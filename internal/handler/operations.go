package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/starcat-app/starcat-discovery-api/internal/store"
)

// OperationsStore 约束运营接口仅能读取聚合统计和受限任务记录。
type OperationsStore interface {
	OperationalStats(context.Context) (store.OperationalStats, error)
	ListSyncRuns(context.Context, int) ([]store.SyncRunSummary, error)
}

// HandleOperationalStats 返回 Discovery 数据目录状态。
func HandleOperationalStats(repository OperationsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats, err := repository.OperationalStats(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load discovery statistics", nil)
			return
		}
		WriteJSON(w, stats)
	}
}

// HandleSyncRuns 返回最近同步记录；limit 由 Store 再次限制，避免无界读取。
func HandleSyncRuns(repository OperationsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		runs, err := repository.ListSyncRuns(r.Context(), limit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to load sync runs", nil)
			return
		}
		WriteJSONWithMeta(w, runs, nil)
	}
}
