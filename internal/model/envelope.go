// Package model 定义 starcat-discovery-api 的稳定响应契约。
//
// Starcat 自建后端统一使用 envelope，客户端可以沿用同一套解码和错误处理。
package model

// Envelope 是 /api/v1/* 200 响应的顶层包装。
type Envelope[T any] struct {
	SchemaVersion int   `json:"schema_version"`
	Data          T     `json:"data"`
	Meta          *Meta `json:"meta,omitempty"`
}

// Meta 是分页、缓存、来源等可选元数据。
type Meta struct {
	Page          int    `json:"page,omitempty"`
	PageSize      int    `json:"page_size,omitempty"`
	Total         int    `json:"total,omitempty"`
	NextPage      *int   `json:"next_page,omitempty"`
	Source        string `json:"source,omitempty"`
	CacheStatus   string `json:"cache_status,omitempty"`
	Cache         string `json:"cache,omitempty"`
	MaxAgeSeconds int    `json:"max_age_seconds,omitempty"`
	GeneratedAt   string `json:"generated_at,omitempty"`
	FetchedAt     string `json:"fetched_at,omitempty"`
}

// ErrorResponse 是非 2xx 响应中的 error 段。
type ErrorResponse struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
}

// ErrorEnvelope 是所有非 2xx 响应的顶层包装。
type ErrorEnvelope struct {
	SchemaVersion int           `json:"schema_version"`
	Error         ErrorResponse `json:"error"`
}
