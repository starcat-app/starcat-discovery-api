// Package middleware 提供 CORS 跨域中间件。
//
// 使 _local-admin 面板能从浏览器直接调用 Fly.io 上的 API，
// 处理 OPTIONS 预检请求并注入跨域响应头。
package middleware

import "net/http"

// CORS 返回一个 http.Handler，为所有响应注入跨域头，
// 并拦截 OPTIONS 预检请求返回 204。
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
