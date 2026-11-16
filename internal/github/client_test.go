package github

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/tokenpool"
)

func TestClientGetREADMEDecodesGitHubContent(t *testing.T) {
	tokens := tokenpool.New([]string{"github_pat_token_one_123456"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/awesome/readme" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(readmeResponse{
			Path: "README.md", SHA: "sha-1", HTMLURL: "https://github.com/acme/awesome/blob/main/README.md",
			Encoding: "base64", Content: base64.StdEncoding.EncodeToString([]byte("# Awesome")),
		})
	}))
	defer server.Close()
	client := NewClient(tokens, 50).WithBaseURL(server.URL)
	client.httpClient = server.Client()
	readme, err := client.GetREADME(t.Context(), "acme/awesome")
	if err != nil {
		t.Fatalf("GetREADME() error = %v", err)
	}
	if string(readme.Content) != "# Awesome" || readme.SHA != "sha-1" || readme.Path != "README.md" {
		t.Fatalf("README = %+v", readme)
	}
}

func TestClientGetREADMEReadsLargeContentsThroughImmutableBlob(t *testing.T) {
	// Contents API 对大于 1 MiB 的 README 返回 encoding=none；按其 SHA 读取
	// Git blob，才能在默认分支继续更新时仍保留同一份 README 的证据。
	content := strings.Repeat("x", 1024*1024+1)
	const readmeSHA = "90186230ab6381d426c2d812e856a4ee9d2c44a8"
	tokens := tokenpool.New([]string{"github_pat_token_one_123456"})
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/repos/acme/awesome/readme":
			_ = json.NewEncoder(w).Encode(readmeResponse{
				Path: "README.md", SHA: readmeSHA, HTMLURL: "https://github.com/acme/awesome/blob/main/README.md",
				Encoding: "none", Size: len(content),
			})
		case "/repos/acme/awesome/git/blobs/" + readmeSHA:
			_ = json.NewEncoder(w).Encode(readmeResponse{
				SHA: readmeSHA, Encoding: "base64", Content: base64.StdEncoding.EncodeToString([]byte(content)),
			})
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := NewClient(tokens, 50).WithBaseURL(server.URL)
	client.httpClient = server.Client()
	readme, err := client.GetREADME(t.Context(), "acme/awesome")
	if err != nil || string(readme.Content) != content || readme.SHA != readmeSHA ||
		readme.Path != "README.md" || readme.HTMLURL != "https://github.com/acme/awesome/blob/main/README.md" {
		t.Fatalf("large README metadata/content mismatch: path=%q sha=%q bytes=%d error=%v", readme.Path, readme.SHA, len(readme.Content), err)
	}
	if len(paths) != 2 {
		t.Fatalf("request paths = %v, want metadata and one immutable blob", paths)
	}
}

func TestClientGetREADMERejectsOversizedBlobBeforeDownloading(t *testing.T) {
	tokens := tokenpool.New([]string{"github_pat_token_one_123456"})
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(readmeResponse{SHA: "large-sha", Encoding: "none", Size: 2*1024*1024 + 1})
	}))
	defer server.Close()
	client := NewClient(tokens, 50).WithBaseURL(server.URL)
	client.httpClient = server.Client()
	if _, err := client.GetREADME(t.Context(), "acme/awesome"); err == nil {
		t.Fatal("expected oversized README to be rejected")
	}
	if requests != 1 {
		t.Fatalf("oversized README triggered %d requests, want metadata only", requests)
	}
}

func TestClientGetRepositoryLanguagesPreservesGitHubByteCounts(t *testing.T) {
	tokens := tokenpool.New([]string{"github_pat_token_one_123456"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/awesome/languages" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("X-RateLimit-Remaining", "4000")
		_ = json.NewEncoder(w).Encode(map[string]int{"Swift": 12_000, "Shell": 800})
	}))
	defer server.Close()

	client := NewClient(tokens, 50).WithBaseURL(server.URL)
	client.httpClient = server.Client()
	languages, err := client.GetRepositoryLanguages(t.Context(), "acme/awesome")
	if err != nil {
		t.Fatalf("GetRepositoryLanguages() error = %v", err)
	}
	if languages["Swift"] != 12_000 || languages["Shell"] != 800 || len(languages) != 2 {
		t.Fatalf("languages = %#v", languages)
	}
}

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

	repos, err := client.SearchRepositories(t.Context(), RepositorySearchOptions{
		Query:   "topic:ai",
		PerPage: 1,
	})
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

	first, err := client.SearchRepositories(t.Context(), RepositorySearchOptions{
		Query:   "topic:ai",
		PerPage: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].FullName != "acme/low" {
		t.Fatalf("low-floor success should still be returned, got %#v", first)
	}

	second, err := client.SearchRepositories(t.Context(), RepositorySearchOptions{
		Query:   "topic:ai",
		PerPage: 1,
	})
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

func TestClientWaitsForTemporaryTokenCooldown(t *testing.T) {
	tokens := tokenpool.New([]string{"github_pat_token_one_123456"})
	token := tokens.PickBest()
	tokens.DisableUntil(token, time.Now().Add(50*time.Millisecond), "test cooldown")

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("X-RateLimit-Remaining", "4000")
		_ = json.NewEncoder(w).Encode(searchResponse{Items: []Repository{{ID: 1, FullName: "acme/recovered"}}})
	}))
	defer server.Close()

	client := NewClient(tokens, 50).WithBaseURL(server.URL)
	client.httpClient = server.Client()
	client.maxTokenWait = time.Second

	repos, err := client.SearchRepositories(t.Context(), RepositorySearchOptions{
		Query:   "topic:ai",
		PerPage: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].FullName != "acme/recovered" {
		t.Fatalf("unexpected repos after cooldown: %#v", repos)
	}
	if requestCount != 1 {
		t.Fatalf("want one GitHub call after cooldown, got %d", requestCount)
	}
}

func TestClientSearchRepositoriesUsesCandidateStrategyOptions(t *testing.T) {
	tokens := tokenpool.New([]string{"github_pat_token_one_123456"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("q") != "topic:llm pushed:>=2026-06-18" {
			t.Fatalf("q = %q", query.Get("q"))
		}
		if query.Get("sort") != "updated" || query.Get("order") != "desc" {
			t.Fatalf("unexpected ordering: %s %s", query.Get("sort"), query.Get("order"))
		}
		if query.Get("per_page") != "100" {
			t.Fatalf("per_page = %q, want client cap 100", query.Get("per_page"))
		}
		w.Header().Set("X-RateLimit-Remaining", "4000")
		_ = json.NewEncoder(w).Encode(searchResponse{})
	}))
	defer server.Close()

	client := NewClient(tokens, 50).WithBaseURL(server.URL)
	client.httpClient = server.Client()
	_, err := client.SearchRepositories(t.Context(), RepositorySearchOptions{
		Query:   "topic:llm pushed:>=2026-06-18",
		Sort:    "updated",
		Order:   "desc",
		PerPage: 150,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientPreservesGitHubStatusForRepositoryErrors(t *testing.T) {
	tokens := tokenpool.New([]string{"github_pat_token_one_123456"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	}))
	defer server.Close()

	client := NewClient(tokens, 50).WithBaseURL(server.URL)
	client.httpClient = server.Client()
	_, err := client.GetRepository(t.Context(), "octo/missing")
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotFound {
		t.Fatalf("GetRepository() error = %#v", err)
	}
}
