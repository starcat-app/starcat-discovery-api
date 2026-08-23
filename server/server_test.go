package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/starcat-app/starcat-discovery-api/internal/config"
)

func TestAwesomeRoutesKeepAPIAndAdminAuthenticationSeparated(t *testing.T) {
	service, err := New(config.Config{
		StoreFile: filepath.Join(t.TempDir(), "discovery.db"), APIKeys: []string{"api-key"},
		AdminAPIKeys: []string{"admin-key"}, GitHubTokens: []string{"github_pat_test_token_123456"},
		SyncEnabled: false, CacheTTLSeconds: 60, FeedTargetSize: 10,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer service.Close()

	assertStatus := func(path, token string, want int) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		service.Handler().ServeHTTP(response, request)
		if response.Code != want {
			t.Fatalf("GET %s with %s = %d, want %d; body=%s", path, token, response.Code, want, response.Body.String())
		}
	}
	assertStatus("/api/v1/discovery/awesome/sources", "api-key", http.StatusOK)
	assertStatus("/api/v1/discovery/awesome/sources", "admin-key", http.StatusUnauthorized)
	assertStatus("/internal/discovery/awesome/sources", "admin-key", http.StatusOK)
	assertStatus("/internal/discovery/awesome/sources", "api-key", http.StatusUnauthorized)
}
