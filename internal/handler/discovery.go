package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dong4j/starcat-discovery-api/internal/model"
	"github.com/dong4j/starcat-discovery-api/internal/store"
)

const (
	categoryMostPopular = "most-popular"
	categoryNewReleases = "new-releases"
	categoryTrending    = "trending"
)

// DiscoveryStore 是 discovery 读取接口依赖的最小 store 契约。
type DiscoveryStore interface {
	ListScoredRepos(ctx context.Context, scoreColumn string, filters store.QueryFilters, page, limit int) (model.Page[model.DiscoveryItem], error)
	ListSortedRepos(ctx context.Context, sortKey string, filters store.QueryFilters, page, limit int) (model.Page[model.DiscoveryItem], error)
	ListCategoryRanking(ctx context.Context, category, bucket string, page, limit int) (model.Page[model.DiscoveryItem], error)
	ListAllRepos(ctx context.Context) ([]model.DiscoveryItem, error)
	ListLanguages(ctx context.Context) ([]model.LanguageStat, error)
	DiscoverySummary(ctx context.Context) (model.DiscoverySummary, error)
	RecordFeedExposure(ctx context.Context, feedKey string, repoIDs []int64) error
}

// DiscoveryHandler 处理发现流和榜单读取接口。
type DiscoveryHandler struct {
	store DiscoveryStore
}

// NewDiscoveryHandler 创建 DiscoveryHandler。
func NewDiscoveryHandler(store DiscoveryStore) *DiscoveryHandler {
	return &DiscoveryHandler{store: store}
}

// HandleFeed 返回发现流。
func (h *DiscoveryHandler) HandleFeed(w http.ResponseWriter, r *http.Request) {
	query, ok := parseListQuery(w, r)
	if !ok {
		return
	}
	page, err := h.listPageBySort(r.Context(), "discovery_score", query)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error(), nil)
		return
	}
	_ = h.store.RecordFeedExposure(r.Context(), feedKey(query.filters), repoIDs(page.Items))
	writePage(w, page, "discovery")
}

// HandleMostPopular 返回热门榜单。
func (h *DiscoveryHandler) HandleMostPopular(w http.ResponseWriter, r *http.Request) {
	sort := strings.TrimSpace(r.URL.Query().Get("sort"))
	h.handleCategory(w, r, categoryMostPopular, scoreColumnForPopular(sort), sort == "" || sort == "popular")
}

// HandleNewReleases 返回新发布榜单。
func (h *DiscoveryHandler) HandleNewReleases(w http.ResponseWriter, r *http.Request) {
	sort := strings.TrimSpace(r.URL.Query().Get("sort"))
	h.handleCategory(w, r, categoryNewReleases, scoreColumnForNewReleases(sort), sort == "" || sort == "release")
}

// HandleTrending 返回新版趋势候选。该接口只做诊断对比，不进入 summary / bulk / Starcat UI。
func (h *DiscoveryHandler) HandleTrending(w http.ResponseWriter, r *http.Request) {
	h.handleCategory(w, r, categoryTrending, "trending_score", false)
}

// HandleSummary 返回探索 Sidebar 所需的模式总量与筛选项计数。
func (h *DiscoveryHandler) HandleSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.store.DiscoverySummary(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error(), nil)
		return
	}
	WriteJSON(w, summary)
}

// HandleBulk 返回 discovery catalog 全量快照。
//
// Starcat 客户端用该快照落本地 SQLite，之后发现 / 热门 / 新发布的排序、筛选、分页都在
// 本地完成；这个端点刻意不接收 query 参数，避免重新退回远端分页语义。
func (h *DiscoveryHandler) HandleBulk(cache *BulkCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if entry, ok := cache.Get(); ok {
			writeBulkResponse(w, r, entry)
			return
		}
		repos, err := h.store.ListAllRepos(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error(), nil)
			return
		}
		summary, err := h.store.DiscoverySummary(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error(), nil)
			return
		}
		env := model.Envelope[model.DiscoveryBulk]{
			SchemaVersion: 1,
			Data: model.DiscoveryBulk{
				Repos:   repos,
				Summary: summary,
			},
			Meta: &model.Meta{
				Total:       len(repos),
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			},
		}
		payload, err := json.Marshal(env)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "ENCODE_ERROR", err.Error(), nil)
			return
		}
		writeBulkResponse(w, r, cache.Set(payload))
	}
}

func (h *DiscoveryHandler) handleCategory(w http.ResponseWriter, r *http.Request, category, scoreColumn string, useRanking bool) {
	query, ok := parseListQuery(w, r)
	if !ok {
		return
	}
	query.filters.Category = category
	page, err := h.categoryPage(r.Context(), category, scoreColumn, query, useRanking)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error(), nil)
		return
	}
	writePage(w, page, category)
}

func (h *DiscoveryHandler) categoryPage(ctx context.Context, category, scoreColumn string, query listQuery, useRanking bool) (model.Page[model.DiscoveryItem], error) {
	bucket, canUseRanking := rankingBucket(query.filters)
	canUseRanking = canUseRanking && useRanking
	if canUseRanking {
		page, err := h.store.ListCategoryRanking(ctx, category, bucket, query.page, query.limit)
		if err == nil && page.Total > 0 {
			return page, nil
		}
		if err != nil {
			return model.Page[model.DiscoveryItem]{}, err
		}
	}
	return h.listPageBySort(ctx, scoreColumn, query)
}

func (h *DiscoveryHandler) listPageBySort(ctx context.Context, defaultScoreColumn string, query listQuery) (model.Page[model.DiscoveryItem], error) {
	switch query.sort {
	case "":
		return h.store.ListScoredRepos(ctx, defaultScoreColumn, query.filters, query.page, query.limit)
	case "recommended":
		return h.store.ListScoredRepos(ctx, "discovery_score", query.filters, query.page, query.limit)
	case "popular":
		return h.store.ListScoredRepos(ctx, "popularity_score", query.filters, query.page, query.limit)
	case "activity":
		return h.store.ListScoredRepos(ctx, "trending_score", query.filters, query.page, query.limit)
	case "release":
		return h.store.ListScoredRepos(ctx, "release_score", query.filters, query.page, query.limit)
	case "stars":
		return h.store.ListSortedRepos(ctx, "stars_desc", query.filters, query.page, query.limit)
	case "stars_asc", "name_asc", "name_desc", "updated_desc", "updated_asc", "created_desc", "created_asc", "release_desc", "release_asc":
		return h.store.ListSortedRepos(ctx, query.sort, query.filters, query.page, query.limit)
	default:
		return model.Page[model.DiscoveryItem]{}, strconv.ErrSyntax
	}
}

// HandleLanguages 返回可用语言及数量。
func (h *DiscoveryHandler) HandleLanguages(w http.ResponseWriter, r *http.Request) {
	languages, err := h.store.ListLanguages(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error(), nil)
		return
	}
	WriteJSONWithMeta(w, languages, &model.Meta{Total: len(languages)})
}

// HandleTopics 返回主题元数据。
func (h *DiscoveryHandler) HandleTopics(w http.ResponseWriter, r *http.Request) {
	WriteJSONWithMeta(w, model.DefaultTopics, &model.Meta{Total: len(model.DefaultTopics)})
}

// HandlePlatforms 返回平台元数据。
func (h *DiscoveryHandler) HandlePlatforms(w http.ResponseWriter, r *http.Request) {
	WriteJSONWithMeta(w, model.DefaultPlatforms, &model.Meta{Total: len(model.DefaultPlatforms)})
}

type listQuery struct {
	page    int
	limit   int
	sort    string
	filters store.QueryFilters
}

func parseListQuery(w http.ResponseWriter, r *http.Request) (listQuery, bool) {
	values := r.URL.Query()
	page, ok := parseBoundedInt(values.Get("page"), 1, 1, 10000)
	if !ok {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "page must be positive", nil)
		return listQuery{}, false
	}
	limit, ok := parseBoundedInt(values.Get("limit"), 20, 1, 50)
	if !ok {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "limit must be between 1 and 50", nil)
		return listQuery{}, false
	}
	return listQuery{
		page:  page,
		limit: limit,
		sort:  strings.TrimSpace(values.Get("sort")),
		filters: store.QueryFilters{
			Language: strings.TrimSpace(values.Get("language")),
			Platform: strings.TrimSpace(values.Get("platform")),
			Topic:    strings.TrimSpace(values.Get("topic")),
		},
	}, true
}

func parseBoundedInt(raw string, fallback, min, max int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < min || value > max {
		return 0, false
	}
	return value, true
}

func writePage(w http.ResponseWriter, page model.Page[model.DiscoveryItem], source string) {
	WriteJSONWithMeta(w, page.Items, &model.Meta{
		Page:     page.Page,
		PageSize: page.PageSize,
		Total:    page.Total,
		NextPage: page.NextPage,
		Source:   source,
	})
}

func rankingBucket(filters store.QueryFilters) (string, bool) {
	if filters.Language != "" && filters.Platform == "" && filters.Topic == "" {
		return "language:" + filters.Language, true
	}
	if filters.Platform != "" && filters.Language == "" && filters.Topic == "" {
		return "platform:" + filters.Platform, true
	}
	if filters.Language == "" && filters.Platform == "" && filters.Topic == "" {
		return model.AllBucket, true
	}
	return "", false
}

func scoreColumnForPopular(sort string) string {
	switch sort {
	case "stars":
		return "search_score"
	case "activity":
		return "trending_score"
	default:
		return "popularity_score"
	}
}

func scoreColumnForNewReleases(sort string) string {
	switch sort {
	case "stars":
		return "search_score"
	case "updated":
		return "trending_score"
	default:
		return "release_score"
	}
}

func feedKey(filters store.QueryFilters) string {
	return "topic:" + emptyAll(filters.Topic) + "|platform:" + emptyAll(filters.Platform) + "|language:" + emptyAll(filters.Language)
}

func emptyAll(value string) string {
	if value == "" {
		return model.AllBucket
	}
	return value
}

func repoIDs(items []model.DiscoveryItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.RepoID)
	}
	return ids
}
