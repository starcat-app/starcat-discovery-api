package handler

import (
	"context"
	"net/http"

	"github.com/dong4j/starcat-discovery-api/internal/model"
)

// SyncService 是 admin handler 依赖的同步服务接口。
type SyncService interface {
	Sync(ctx context.Context, mode string) (model.SyncResult, error)
}

// HandleAdminSyncDiscovery 是 /internal/sync/discovery 的管理入口。
func HandleAdminSyncDiscovery(syncer SyncService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		result, err := syncer.Sync(r.Context(), mode)
		if err != nil {
			WriteError(w, http.StatusBadGateway, "SYNC_FAILED", err.Error(), result)
			return
		}
		WriteJSON(w, result)
	}
}
