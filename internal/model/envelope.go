// Package model 定义 starcat-discovery-api 的稳定响应契约。
//
// 类型来自 starcat-api-kit/envelope，本文件只做别名以保持现有 import 稳定。
package model

import "github.com/starcat-app/starcat-api-kit/envelope"

// Envelope 是 /api/v1/* 200 响应的顶层包装。
type Envelope[T any] = envelope.Envelope[T]

// Meta 是分页、缓存、来源等可选元数据。
type Meta = envelope.Meta

// ErrorResponse 是非 2xx 响应中的 error 段。
type ErrorResponse = envelope.ErrorResponse

// ErrorEnvelope 是所有非 2xx 响应的顶层包装。
type ErrorEnvelope = envelope.ErrorEnvelope
