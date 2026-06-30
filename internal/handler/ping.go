package handler

import "net/http"

type pingResponse struct {
	Service string `json:"service"`
	OK      bool   `json:"ok"`
}

// HandlePingV1 暴露 GET /api/v1/ping，用于 Starcat 设置页测试连接。
func HandlePingV1(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, pingResponse{
			Service: service,
			OK:      true,
		})
	}
}
