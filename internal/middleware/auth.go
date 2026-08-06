// Package middleware 提供 Bearer Token 鉴权与 CORS。
//
// discovery 区分 api / admin 两套 key；实现收敛到 starcat-api-kit，本包保留两参 NewBearerAuth 签名。
package middleware

import (
	"net/http"

	kitauth "github.com/starcat-app/starcat-api-kit/auth"
	kitcors "github.com/starcat-app/starcat-api-kit/cors"
)

// BearerAuth 是 kit auth 的类型别名。
type BearerAuth = kitauth.BearerAuth

// NewBearerAuth 创建 Bearer 鉴权中间件；name 仅用于日志前缀（api / admin）。
func NewBearerAuth(name string, keys []string) *BearerAuth {
	return kitauth.NewNamedBearerAuth(name, keys)
}

// CORS 注入跨域响应头并处理 OPTIONS。
func CORS(next http.Handler) http.Handler {
	return kitcors.Handler(next)
}
