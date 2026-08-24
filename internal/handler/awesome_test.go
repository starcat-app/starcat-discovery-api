package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestAwesomeSourcesETagReturns304(t *testing.T) {
	service := stubAwesomePublicService{sources: []model.AwesomeSource{{
		ID: "awesome-mac", DisplayName: "Awesome Mac", RepoFullName: "owner/repo",
		RepoURL: "https://github.com/owner/repo", RepoDescription: "GitHub repository description",
		Status: model.AwesomeSourcePublished, SourceStars: 123, SourceForks: 45,
		SourceWatchers: 123, SourceSubscribers: 9, SourceOpenIssues: 7, SourceLanguage: "Swift",
		LanguageBytes: map[string]int{"Swift": 10_000, "Shell": 500},
		UpdatedAt:     time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC),
	}}}
	handler := NewAwesomeHandler(service)
	first := httptest.NewRecorder()
	handler.HandleSources(first, httptest.NewRequest(http.MethodGet, "/api/v1/discovery/awesome/sources", nil))
	if first.Code != http.StatusOK || first.Header().Get("ETag") == "" {
		t.Fatalf("first response = %d, headers=%v", first.Code, first.Header())
	}
	if !strings.Contains(first.Body.String(), `"source_stars":123`) {
		t.Fatalf("source repository stars missing from public catalog: %s", first.Body.String())
	}
	if !strings.Contains(first.Body.String(), `"repo_description":"GitHub repository description"`) {
		t.Fatalf("source repository description missing from public catalog: %s", first.Body.String())
	}
	if !strings.Contains(first.Body.String(), `"source_forks":45`) ||
		!strings.Contains(first.Body.String(), `"source_language":"Swift"`) ||
		!strings.Contains(first.Body.String(), `"language_bytes":{"Shell":500,"Swift":10000}`) {
		t.Fatalf("source repository facts missing from public catalog: %s", first.Body.String())
	}
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/awesome/sources", nil)
	secondRequest.Header.Set("If-None-Match", first.Header().Get("ETag"))
	second := httptest.NewRecorder()
	handler.HandleSources(second, secondRequest)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("second response = %d, body=%q", second.Code, second.Body.String())
	}
}

func TestAwesomeSourcesResponseCacheAvoidsRepeatedServiceReads(t *testing.T) {
	service := &countingAwesomePublicService{sources: []model.AwesomeSource{{
		ID: "awesome-test", DisplayName: "Awesome Test", RepoFullName: "owner/repo",
		RepoURL: "https://github.com/owner/repo", Status: model.AwesomeSourcePublished,
		UpdatedAt: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC),
	}}}
	cache := NewAwesomeResponseCache(time.Hour, 8, 1<<20)
	handler := NewAwesomeHandler(service, cache)

	first := httptest.NewRecorder()
	handler.HandleSources(first, httptest.NewRequest(http.MethodGet, "/api/v1/discovery/awesome/sources", nil))
	second := httptest.NewRecorder()
	handler.HandleSources(second, httptest.NewRequest(http.MethodGet, "/api/v1/discovery/awesome/sources", nil))
	if first.Code != http.StatusOK || second.Code != http.StatusOK || service.sourceCalls != 1 {
		t.Fatalf("cached sources responses=%d/%d serviceCalls=%d", first.Code, second.Code, service.sourceCalls)
	}

	cache.InvalidateAwesomeCatalog()
	third := httptest.NewRecorder()
	handler.HandleSources(third, httptest.NewRequest(http.MethodGet, "/api/v1/discovery/awesome/sources", nil))
	if third.Code != http.StatusOK || service.sourceCalls != 2 {
		t.Fatalf("invalidated sources response=%d serviceCalls=%d", third.Code, service.sourceCalls)
	}
}

func TestAwesomeEntriesAlwaysIncludeRepositoryFacts(t *testing.T) {
	updatedAt := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	service := stubAwesomePublicService{snapshot: model.AwesomeEntriesSnapshot{
		Source: model.AwesomeEntriesSource{ID: "awesome-mac", DisplayName: "Awesome Mac", UpdatedAt: updatedAt},
		Entries: []model.AwesomeEntry{{
			GhRepoID: ptr(int64(42)), FullName: "owner/repo", EntryTitle: "Repo",
			DefaultBranch: "main", UpdatedAt: "2026-08-24T00:00:00Z", CreatedAt: "2020-01-01T00:00:00Z",
			Topics: []string{}, SectionPath: []string{"Apps"}, SourceAnchorURL: "https://example.com#apps",
		}},
	}}
	handler := NewAwesomeHandler(service)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/awesome/sources/awesome-mac/entries", nil)
	request.SetPathValue("source_id", "awesome-mac")
	handler.HandleEntries(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("response = %d, body=%q", response.Code, response.Body.String())
	}
	for _, required := range []string{
		`"stars":0`, `"forks":0`, `"watchers":0`, `"subscribers":0`, `"open_issues":0`,
		`"default_branch":"main"`, `"topics":[]`, `"is_archived":false`, `"is_fork":false`,
		`"updated_at":"2026-08-24T00:00:00Z"`, `"created_at":"2020-01-01T00:00:00Z"`,
	} {
		if !strings.Contains(response.Body.String(), required) {
			t.Fatalf("repository fact %s must remain in the public contract: %s", required, response.Body.String())
		}
	}
}

type stubAwesomePublicService struct {
	sources  []model.AwesomeSource
	snapshot model.AwesomeEntriesSnapshot
}

type countingAwesomePublicService struct {
	sources     []model.AwesomeSource
	sourceCalls int
}

func (s *countingAwesomePublicService) ListPublishedSources(context.Context) ([]model.AwesomeSource, error) {
	s.sourceCalls++
	return s.sources, nil
}

func (s *countingAwesomePublicService) PublishedEntries(context.Context, string) (model.AwesomeEntriesSnapshot, error) {
	return model.AwesomeEntriesSnapshot{}, nil
}

func (s stubAwesomePublicService) ListPublishedSources(context.Context) ([]model.AwesomeSource, error) {
	return s.sources, nil
}

func (s stubAwesomePublicService) PublishedEntries(context.Context, string) (model.AwesomeEntriesSnapshot, error) {
	return s.snapshot, nil
}

func ptr[T any](value T) *T { return &value }
