package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/awesome"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

// AwesomePublicService is the read-only contract exposed to Starcat clients.
type AwesomePublicService interface {
	ListPublishedSources(ctx context.Context) ([]model.AwesomeSource, error)
	PublishedEntries(ctx context.Context, sourceID string) (model.AwesomeEntriesSnapshot, error)
}

// AwesomeHandler exposes managed source catalog and verified entries with stable ETags.
type AwesomeHandler struct {
	service AwesomePublicService
	cache   *AwesomeResponseCache
}

func NewAwesomeHandler(service AwesomePublicService, caches ...*AwesomeResponseCache) *AwesomeHandler {
	cache := NewAwesomeResponseCache(defaultAwesomeCacheTTL, defaultAwesomeCacheMaxEntries, defaultAwesomeCacheMaxBytes)
	if len(caches) > 0 && caches[0] != nil {
		cache = caches[0]
	}
	return &AwesomeHandler{service: service, cache: cache}
}

func (h *AwesomeHandler) HandleSources(w http.ResponseWriter, r *http.Request) {
	response, err := h.cache.getOrBuild(r.Context(), awesomeCatalogCacheKey, func(ctx context.Context) (awesomeCachedResponse, error) {
		sources, serviceErr := h.service.ListPublishedSources(ctx)
		if serviceErr != nil {
			return awesomeCachedResponse{}, serviceErr
		}
		cards := make([]model.AwesomeSourceCard, 0, len(sources))
		generatedAt := time.Time{}
		for _, source := range sources {
			cards = append(cards, sourceCard(source))
			if source.UpdatedAt.After(generatedAt) {
				generatedAt = source.UpdatedAt
			}
			if source.LastSyncedAt != nil && source.LastSyncedAt.After(generatedAt) {
				generatedAt = *source.LastSyncedAt
			}
		}
		return newAwesomeCachedResponse(cards, &model.Meta{Total: len(cards), GeneratedAt: generatedAt.Format(time.RFC3339)})
	})
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	writeAwesomeCachedResponse(w, r, response)
}

func (h *AwesomeHandler) HandleEntries(w http.ResponseWriter, r *http.Request) {
	sourceID := r.PathValue("source_id")
	response, err := h.cache.getOrBuild(r.Context(), awesomeEntriesCacheKey(sourceID), func(ctx context.Context) (awesomeCachedResponse, error) {
		snapshot, serviceErr := h.service.PublishedEntries(ctx, sourceID)
		if serviceErr != nil {
			return awesomeCachedResponse{}, serviceErr
		}
		// Public API 的最后一道防线：即使数据库仍残留旧版外链快照，也只允许独立
		// GitHub 仓库离开服务边界，避免缓存或历史数据再次污染客户端。
		snapshot.Entries = publishedGitHubRepositories(snapshot.Entries)
		return newAwesomeCachedResponse(snapshot, &model.Meta{
			Total: len(snapshot.Entries), GeneratedAt: snapshot.Source.UpdatedAt.Format(time.RFC3339),
		})
	})
	if err != nil {
		writeAwesomeError(w, err)
		return
	}
	writeAwesomeCachedResponse(w, r, response)
}

func publishedGitHubRepositories(entries []model.AwesomeEntry) []model.AwesomeEntry {
	filtered := make([]model.AwesomeEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.TargetType == "github_repo" && entry.GhRepoID != nil && entry.FullName != "" {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func sourceCard(source model.AwesomeSource) model.AwesomeSourceCard {
	languageBytes := source.LanguageBytes
	if languageBytes == nil {
		// 公共目录是客户端的稳定契约；无语言数据也必须返回空对象，不能省略字段或编码成 null。
		languageBytes = map[string]int{}
	}
	return model.AwesomeSourceCard{
		ID: source.ID, DisplayName: source.DisplayName, RepoFullName: source.RepoFullName,
		RepoURL: source.RepoURL, RepoDescription: source.RepoDescription,
		ImageURL: source.ImageURL, SummaryZH: source.SummaryZH,
		SummaryEN: source.SummaryEN, Featured: source.Featured, SortOrder: source.SortOrder,
		ParserProfile: source.ParserProfile,
		SourceStars:   source.SourceStars, SourceForks: source.SourceForks,
		SourceWatchers: source.SourceWatchers, SourceSubscribers: source.SourceSubscribers,
		SourceOpenIssues: source.SourceOpenIssues, SourceLanguage: source.SourceLanguage,
		LanguageBytes: languageBytes, GitHubRepoCount: source.GitHubRepoCount,
		// 旧字段继续保留以维持 JSON 契约，但公开目录不再发布任何非仓库条目。
		ExternalEntryCount: 0,
		ResourceEntryCount: 0,
		LastSyncedAt:       source.LastSyncedAt, UpdatedAt: source.UpdatedAt,
	}
}

func writeAwesomeError(w http.ResponseWriter, err error) {
	var encodingErr *awesomeResponseEncodingError
	if errors.As(err, &encodingErr) {
		WriteError(w, http.StatusInternalServerError, "ENCODE_ERROR", "无法编码 Awesome 响应", nil)
		return
	}
	var serviceErr *awesome.ServiceError
	if errors.As(err, &serviceErr) {
		WriteError(w, serviceErr.Status, serviceErr.Code, serviceErr.Message, nil)
		return
	}
	WriteError(w, http.StatusInternalServerError, "STORE_ERROR", "Awesome 服务暂时不可用", nil)
}
