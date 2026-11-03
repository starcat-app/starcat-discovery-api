// Linux.do 空目录也是可缓存的完整快照；失败和 304 必须保持不同语义。
package handler

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/linuxdo"
	"github.com/starcat-app/starcat-discovery-api/internal/store"
)

func TestLinuxDoCacheETagGzipAndInvalidation(t *testing.T) {
	db, err := store.NewSQLiteStore(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cache := NewLinuxDoResponseCache(time.Hour)
	service := linuxdo.NewService(db, nil, false, cache)
	h := NewLinuxDoHandler(service, cache)
	request := httptest.NewRequest("GET", "/api/v1/discovery/linuxdo/provenance", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	response := httptest.NewRecorder()
	h.HandlePublic(response, request)
	if response.Code != 200 || response.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal(response)
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reader)
	reader.Close()
	var envelope struct {
		Data struct {
			Complete bool  `json:"complete"`
			Origins  []any `json:"origins"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.Data.Complete || envelope.Data.Origins == nil {
		t.Fatalf("invalid empty snapshot: %s", body)
	}
	request.Header.Set("If-None-Match", response.Header().Get("ETag"))
	response = httptest.NewRecorder()
	h.HandlePublic(response, request)
	if response.Code != 304 || response.Body.Len() != 0 {
		t.Fatal("conditional GET failed")
	}
	old := cache.cache()
	cache.Invalidate()
	if old == cache.cache() {
		t.Fatal("cache generation was reused")
	}
}
