// Linux.do HTTP 契约复用现有有界响应缓存；与 Awesome 使用不同实例，互不失效。
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/linuxdo"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"github.com/starcat-app/starcat-discovery-api/internal/store"
)

// LinuxDoResponseCache 在发布后换代。旧请求只能完成自己的响应，不能回填新一代缓存。
type LinuxDoResponseCache struct {
	mu      sync.RWMutex
	current *AwesomeResponseCache
	ttl     time.Duration
}

func NewLinuxDoResponseCache(ttl time.Duration) *LinuxDoResponseCache {
	c := &LinuxDoResponseCache{ttl: ttl}
	c.Invalidate()
	return c
}
func (c *LinuxDoResponseCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = NewAwesomeResponseCache(c.ttl, 64, 32<<20)
}
func (c *LinuxDoResponseCache) cache() *AwesomeResponseCache {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// LinuxDoHandler 只向客户端暴露审核后的公共目录，不公开候选证据或运行错误详情。
type LinuxDoHandler struct {
	service *linuxdo.Service
	cache   *LinuxDoResponseCache
}

func NewLinuxDoHandler(service *linuxdo.Service, cache *LinuxDoResponseCache) *LinuxDoHandler {
	return &LinuxDoHandler{service: service, cache: cache}
}

// HandlePublic 的三个视图共享相同 revision，客户端可拒绝乱序旧响应。
func (h *LinuxDoHandler) HandlePublic(w http.ResponseWriter, r *http.Request) {
	repoID := int64(0)
	if raw := r.PathValue("gh_repo_id"); raw != "" {
		var err error
		repoID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || repoID <= 0 {
			WriteError(w, 400, "INVALID_REPO_ID", "Invalid repository ID", nil)
			return
		}
	}
	response, err := h.cache.cache().getOrBuild(r.Context(), r.URL.Path, func(ctx context.Context) (awesomeCachedResponse, error) {
		snapshot, err := h.service.Snapshot(ctx)
		if err != nil {
			return awesomeCachedResponse{}, err
		}
		data := map[string]any{"revision": snapshot.Revision, "generated_at": snapshot.GeneratedAt, "complete": true}
		if repoID > 0 {
			topics := snapshot.Topics[repoID]
			if topics == nil {
				topics = []model.LinuxDoTopic{}
			}
			data["topics"] = topics
			data["repo_id"] = repoID
		} else if r.URL.Path == "/api/v1/discovery/linuxdo/provenance" {
			data["origins"] = snapshot.Origins
		} else {
			data["repositories"] = snapshot.Repositories
			// 推广时间不是 GitHub 创建时间；独立扩展字段避免污染共享仓库事实。
			promotions := map[int64]string{}
			for id, topics := range snapshot.Topics {
				if len(topics) > 0 {
					promotions[id] = topics[0].PublishedAt
				}
			}
			data["promotion_dates"] = promotions
		}
		return newAwesomeCachedResponse(data, nil)
	})
	if err != nil {
		WriteError(w, 503, "LINUXDO_UNAVAILABLE", "Linux.do snapshot unavailable", nil)
		return
	}
	writeAwesomeCachedResponse(w, r, response)
}

func (h *LinuxDoHandler) HandleSync(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Sync(r.Context(), "manual")
	if err != nil {
		writeLinuxDoError(w, err)
		return
	}
	WriteJSON(w, result)
}
func (h *LinuxDoHandler) HandleCandidates(w http.ResponseWriter, r *http.Request) {
	after := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || after < 0 {
			WriteError(w, 400, "INVALID_CURSOR", "Invalid cursor", nil)
			return
		}
	}
	items, err := h.service.Candidates(r.Context(), after)
	if err != nil {
		writeLinuxDoError(w, err)
		return
	}
	WriteJSON(w, items)
}

// HandleReview 严格限制请求大小/字段，避免误把旧证据或任意仓库元数据直接写入发布表。
func (h *LinuxDoHandler) HandleReview(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("candidate_id"), 10, 64)
	if err != nil || id <= 0 {
		WriteError(w, 400, "INVALID_CANDIDATE", "Invalid candidate ID", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request linuxdo.ReviewRequest
	if err = decoder.Decode(&request); err != nil {
		WriteError(w, 400, "INVALID_REVIEW", "Invalid review body", nil)
		return
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		WriteError(w, 400, "INVALID_REVIEW", "Expected one review object", nil)
		return
	}
	if err = h.service.Review(r.Context(), id, request); err != nil {
		writeLinuxDoError(w, err)
		return
	}
	WriteJSON(w, map[string]bool{"updated": true})
}

func writeLinuxDoError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusBadGateway, "LINUXDO_OPERATION_FAILED", "Linux.do operation failed; previous snapshot retained"
	switch {
	case errors.Is(err, linuxdo.ErrDisabled):
		status = 503
		code = "LINUXDO_COLLECTION_DISABLED"
		message = "Collection requires explicit authorization"
	case errors.Is(err, linuxdo.ErrBusy), errors.Is(err, store.ErrLinuxDoConflict):
		status = 409
		code = "LINUXDO_CONFLICT"
		message = "Reload before retrying"
	case errors.Is(err, sql.ErrNoRows):
		status = 404
		code = "LINUXDO_NOT_FOUND"
		message = "Candidate not found"
	}
	WriteError(w, status, code, message, nil)
}
