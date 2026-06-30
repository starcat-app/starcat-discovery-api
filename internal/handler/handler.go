// Package handler 提供 HTTP handler 公共工具。
package handler

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/dong4j/starcat-discovery-api/internal/model"
)

// WriteJSON 将任意 data 包装成 envelope 后输出。
func WriteJSON[T any](w http.ResponseWriter, data T) {
	WriteJSONWithMeta(w, data, nil)
}

// WriteJSONWithMeta 将 data 与 meta 包装成 envelope 后输出。
func WriteJSONWithMeta[T any](w http.ResponseWriter, data T, meta *model.Meta) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	env := model.Envelope[T]{
		SchemaVersion: 1,
		Data:          data,
		Meta:          meta,
	}
	if err := json.NewEncoder(w).Encode(env); err != nil {
		log.Printf("[handler] failed to encode envelope: %v", err)
	}
}

// WriteError 输出统一错误 envelope。
func WriteError(w http.ResponseWriter, status int, code, message string, details interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	env := model.ErrorEnvelope{
		SchemaVersion: 1,
		Error: model.ErrorResponse{
			Code:    code,
			Message: message,
			Details: details,
		},
	}
	if err := json.NewEncoder(w).Encode(env); err != nil {
		log.Printf("[handler] failed to encode error envelope: %v", err)
	}
}
