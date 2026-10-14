// Linux.do 查询与管理使用分离的鉴权，并验证默认关闭时不会启动采集。
package server

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/starcat-app/starcat-discovery-api/internal/config"
)

func TestLinuxDoRoutesAndDisabledCollection(t *testing.T) {
	s, err := New(config.Config{StoreFile: filepath.Join(t.TempDir(), "discovery.db"), APIKeys: []string{"reader"}, AdminAPIKeys: []string{"admin"}, GitHubTokens: []string{"test-pat"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.linuxDoScheduler != nil {
		t.Fatal("collection enabled by default")
	}
	for _, test := range []struct {
		method, path, token string
		status              int
	}{
		{"GET", "/api/v1/discovery/linuxdo/bulk", "reader", 200},
		{"GET", "/api/v1/discovery/linuxdo/provenance", "admin", 401},
		{"GET", "/api/v1/discovery/linuxdo/repos/123/topics", "reader", 200},
		{"GET", "/internal/discovery/linuxdo/candidates", "reader", 401},
		{"GET", "/internal/discovery/linuxdo/candidates", "admin", 200},
		{"POST", "/internal/sync/linuxdo", "admin", 503},
		{"POST", "/internal/discovery/linuxdo/candidates/1/review", "reader", 401},
	} {
		r := httptest.NewRequest(test.method, test.path, nil)
		r.Header.Set("Authorization", "Bearer "+test.token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != test.status {
			t.Errorf("%s %s: %d != %d", test.method, test.path, w.Code, test.status)
		}
	}
}
