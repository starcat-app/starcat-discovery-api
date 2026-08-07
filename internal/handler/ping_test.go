package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

// TestHandlePingV1 验证 ping 在原有 envelope 中返回服务名和构建版本。
func TestHandlePingV1(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	rr := httptest.NewRecorder()

	HandlePingV1("discovery", "1.2.3").ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	type pingData struct {
		Service string `json:"service"`
		Version string `json:"version"`
		OK      bool   `json:"ok"`
	}
	var env model.Envelope[pingData]
	if err := json.NewDecoder(rr.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", env.SchemaVersion)
	}
	if env.Data.Service != "discovery" || !env.Data.OK {
		t.Fatalf("unexpected data: %+v", env.Data)
	}
	if env.Data.Version != "1.2.3" {
		t.Fatalf("version = %q, want %q", env.Data.Version, "1.2.3")
	}
}
