// Package handler 的 ping 端点已收敛到 starcat-api-kit/httputil。
package handler

import (
	"net/http"

	"github.com/starcat-app/starcat-api-kit/httputil"
)

// HandlePingV1 暴露 GET /api/v1/ping，用于 Starcat 设置页测试连接。
func HandlePingV1(service, serviceVersion string) http.HandlerFunc {
	return httputil.HandlePingV1(service, serviceVersion)
}
