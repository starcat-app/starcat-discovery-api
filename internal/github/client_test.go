package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/dong4j/starcat-discovery-api/internal/tokenpool"
)

func TestClientRetriesNextTokenOnRateLimit(t *testing.T) {
	tokens := tokenpool.New([]string{"github_pat_token_one_123456", "github_pat_token_two_123456"})
	var authHeaders []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		if len(authHeaders) == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "rate limit exceeded"})
			return
		}
		w.Header().Set("X-RateLimit-Remaining", "4000")
		_ = json.NewEncoder(w).Encode(searchResponse{Items: []Repository{{ID: 1, FullName: "acme/repo"}}})
	}))
	defer server.Close()

	client := NewClient(tokens, 50).WithBaseURL(server.URL)
	client.httpClient = server.Client()

	repos, err := client.SearchRepositories(t.Context(), "topic:ai", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].FullName != "acme/repo" {
		t.Fatalf("unexpected repos: %#v", repos)
	}
	if len(authHeaders) != 2 {
		t.Fatalf("want 2 GitHub calls, got %d", len(authHeaders))
	}
	if authHeaders[0] == authHeaders[1] {
		t.Fatalf("want retry with another token, got same header %q", authHeaders[0])
	}
}

func TestClientKeepsSuccessWhenRemainingBelowFloorAndSkipsTokenLater(t *testing.T) {
	tokens := tokenpool.New([]string{"github_pat_token_one_123456", "github_pat_token_two_123456"})
	var authHeaders []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		switch len(authHeaders) {
		case 1:
			w.Header().Set("X-RateLimit-Remaining", "1")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
			_ = json.NewEncoder(w).Encode(searchResponse{Items: []Repository{{ID: 1, FullName: "acme/low"}}})
		default:
			w.Header().Set("X-RateLimit-Remaining", "4000")
			_ = json.NewEncoder(w).Encode(searchResponse{Items: []Repository{{ID: 2, FullName: "acme/next"}}})
		}
	}))
	defer server.Close()

	client := NewClient(tokens, 50).WithBaseURL(server.URL)
	client.httpClient = server.Client()

	first, err := client.SearchRepositories(t.Context(), "topic:ai", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].FullName != "acme/low" {
		t.Fatalf("low-floor success should still be returned, got %#v", first)
	}

	second, err := client.SearchRepositories(t.Context(), "topic:ai", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].FullName != "acme/next" {
		t.Fatalf("unexpected second result: %#v", second)
	}
	if len(authHeaders) != 2 {
		t.Fatalf("want 2 GitHub calls, got %d", len(authHeaders))
	}
	if authHeaders[0] == authHeaders[1] {
		t.Fatalf("token below floor should be skipped on next request, got %q", authHeaders[0])
	}
}
