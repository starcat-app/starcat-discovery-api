package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/github"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"github.com/starcat-app/starcat-discovery-api/internal/starhistory"
)

const (
	starHistoryRetryAfterSeconds = 5
	starHistoryRateRetrySeconds  = 30
)

var repositoryPathPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// StarHistoryService 是 HTTP 层所需的 cache-first 与入队边界。
type StarHistoryService interface {
	Lookup(
		ctx context.Context,
		repoID int64,
		fullName string,
		historyRange model.StarHistoryRange,
	) (starhistory.LookupResult, error)
	Enqueue(ctx context.Context, request model.StarHistoryBuildRequest) (bool, error)
}

// StarHistoryRepositoryClient 只读取公开仓库身份和当前 Star 数。
type StarHistoryRepositoryClient interface {
	GetRepository(ctx context.Context, fullName string) (github.Repository, error)
}

// StarHistoryHandler 提供公开仓库 Star 历史的统一 envelope API。
type StarHistoryHandler struct {
	service    StarHistoryService
	repository StarHistoryRepositoryClient
	enabled    bool
}

// NewStarHistoryHandler 创建 handler；disabled 状态仍注册路由并稳定返回 503。
func NewStarHistoryHandler(
	service StarHistoryService,
	repository StarHistoryRepositoryClient,
	enabled bool,
) *StarHistoryHandler {
	return &StarHistoryHandler{service: service, repository: repository, enabled: enabled}
}

// HandleStarHistory 先返回有效缓存；仅在 miss 时同步校验 GitHub 身份并异步入队。
func (h *StarHistoryHandler) HandleStarHistory(w http.ResponseWriter, r *http.Request) {
	if !h.enabled || h.service == nil || h.repository == nil {
		WriteError(
			w,
			http.StatusServiceUnavailable,
			"HISTORY_PROVIDER_UNAVAILABLE",
			"Star history provider is unavailable.",
			nil,
		)
		return
	}

	owner := strings.TrimSpace(r.PathValue("owner"))
	repo := strings.TrimSpace(r.PathValue("repo"))
	repoID, historyRange, ok := parseStarHistoryRequest(w, r, owner, repo)
	if !ok {
		return
	}
	fullName := owner + "/" + repo

	result, err := h.service.Lookup(r.Context(), repoID, fullName, historyRange)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	switch result.State {
	case starhistory.LookupReady:
		h.writeReady(w, r, repoID, fullName, result)
		return
	case starhistory.LookupBuilding:
		writeStarHistoryBuilding(w)
		return
	case starhistory.LookupFailed:
		WriteError(
			w,
			http.StatusServiceUnavailable,
			"HISTORY_PROVIDER_UNAVAILABLE",
			"Star history provider is temporarily unavailable.",
			nil,
		)
		return
	}

	repository, err := h.repository.GetRepository(r.Context(), fullName)
	if err != nil {
		writeGitHubRepositoryError(w, err)
		return
	}
	if repository.ID != repoID {
		WriteError(
			w,
			http.StatusConflict,
			"REPOSITORY_ID_MISMATCH",
			"Repository ID does not match owner/name.",
			nil,
		)
		return
	}
	if !strings.EqualFold(repository.FullName, fullName) {
		WriteError(
			w,
			http.StatusConflict,
			"REPOSITORY_ID_MISMATCH",
			"Repository owner/name has changed.",
			nil,
		)
		return
	}
	if repository.Private {
		WriteError(
			w,
			http.StatusUnprocessableEntity,
			"PRIVATE_REPOSITORY_UNSUPPORTED",
			"Private repository history is not supported.",
			nil,
		)
		return
	}
	createdAt, err := time.Parse(time.RFC3339, repository.CreatedAt)
	if err != nil {
		WriteError(
			w,
			http.StatusServiceUnavailable,
			"HISTORY_PROVIDER_UNAVAILABLE",
			"Repository metadata is temporarily unavailable.",
			nil,
		)
		return
	}

	claimed, err := h.service.Enqueue(r.Context(), model.StarHistoryBuildRequest{
		GhRepoID:     repository.ID,
		FullName:     repository.FullName,
		CurrentStars: repository.Stargazers,
		CreatedAt:    createdAt,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if !claimed {
		// 另一请求在 metadata 校验期间可能已认领；客户端统一按构建中轮询即可。
		writeStarHistoryBuilding(w)
		return
	}
	writeStarHistoryBuilding(w)
}

func parseStarHistoryRequest(
	w http.ResponseWriter,
	r *http.Request,
	owner string,
	repo string,
) (int64, model.StarHistoryRange, bool) {
	if !repositoryPathPart.MatchString(owner) || !repositoryPathPart.MatchString(repo) {
		WriteError(w, http.StatusBadRequest, "INVALID_REPOSITORY", "Invalid repository path.", nil)
		return 0, "", false
	}
	repoID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("repo_id")), 10, 64)
	if err != nil || repoID <= 0 {
		WriteError(w, http.StatusBadRequest, "INVALID_REPOSITORY", "repo_id is required.", nil)
		return 0, "", false
	}
	historyRange := model.StarHistoryRange(strings.TrimSpace(r.URL.Query().Get("range")))
	if historyRange == "" {
		historyRange = model.StarHistoryRangeOneYear
	}
	switch historyRange {
	case model.StarHistoryRangeThreeMonths,
		model.StarHistoryRangeOneYear,
		model.StarHistoryRangeAll:
		return repoID, historyRange, true
	default:
		WriteError(w, http.StatusBadRequest, "INVALID_REPOSITORY", "Unsupported star history range.", nil)
		return 0, "", false
	}
}

func (h *StarHistoryHandler) writeReady(
	w http.ResponseWriter,
	r *http.Request,
	repoID int64,
	fullName string,
	result starhistory.LookupResult,
) {
	data := model.StarHistoryResponse{
		RepoID:        repoID,
		FullName:      fullName,
		CurrentStars:  result.CurrentStars,
		Range:         result.Series.Range,
		CoverageStart: result.Series.CoverageStart,
		GeneratedAt:   result.Series.GeneratedAt,
		Points:        result.Series.Points,
	}
	etag := starHistoryETag(data)
	maxAge := max(0, result.MaxAgeSeconds)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(maxAge))
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	WriteJSONWithMeta(w, data, &model.Meta{
		Cache:         "hit",
		MaxAgeSeconds: maxAge,
	})
}

func (h *StarHistoryHandler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, starhistory.ErrRepositoryIDMismatch):
		WriteError(w, http.StatusConflict, "REPOSITORY_ID_MISMATCH", "Repository ID does not match owner/name.", nil)
	case errors.Is(err, starhistory.ErrQueueFull):
		w.Header().Set("Retry-After", strconv.Itoa(starHistoryRateRetrySeconds))
		WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Star history build queue is busy.", nil)
	default:
		WriteError(
			w,
			http.StatusServiceUnavailable,
			"HISTORY_PROVIDER_UNAVAILABLE",
			"Star history provider is temporarily unavailable.",
			nil,
		)
	}
}

func writeGitHubRepositoryError(w http.ResponseWriter, err error) {
	var apiError *github.APIError
	if errors.As(err, &apiError) {
		switch apiError.StatusCode {
		case http.StatusNotFound:
			WriteError(w, http.StatusNotFound, "REPOSITORY_NOT_FOUND", "Repository was not found.", nil)
			return
		case http.StatusForbidden, http.StatusTooManyRequests:
			w.Header().Set("Retry-After", strconv.Itoa(starHistoryRateRetrySeconds))
			WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "GitHub rate limit reached.", nil)
			return
		}
	}
	WriteError(
		w,
		http.StatusServiceUnavailable,
		"HISTORY_PROVIDER_UNAVAILABLE",
		"Repository metadata is temporarily unavailable.",
		nil,
	)
}

func writeStarHistoryBuilding(w http.ResponseWriter) {
	w.Header().Set("Retry-After", strconv.Itoa(starHistoryRetryAfterSeconds))
	WriteError(
		w,
		http.StatusAccepted,
		"STAR_HISTORY_BUILDING",
		"Star history is being prepared.",
		nil,
	)
}

func starHistoryETag(data model.StarHistoryResponse) string {
	encoded, _ := json.Marshal(data)
	sum := sha256.Sum256(encoded)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}
