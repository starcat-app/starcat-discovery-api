package handler

import (
	"net/http"
	"time"
)

type syncResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	QueuedAt  string `json:"queued_at"`
	Mode      string `json:"mode"`
	StoreFile string `json:"store_file,omitempty"`
}

// HandleAdminSyncDiscovery 是 /internal/sync/discovery 的管理入口。
//
// 骨架阶段先返回明确占位状态。下一闭环接入 SQLite store 和 ingest service 后，
// 这里会改为触发后台同步并返回 run id。
func HandleAdminSyncDiscovery(storeFile string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, syncResponse{
			Status:    "accepted",
			Message:   "sync endpoint is wired; ingest worker will be attached in the next implementation step",
			QueuedAt:  time.Now().UTC().Format(time.RFC3339),
			Mode:      "manual",
			StoreFile: storeFile,
		})
	}
}
