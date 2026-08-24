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
		RepoURL: "https://github.com/owner/repo", Status: model.AwesomeSourcePublished, SourceStars: 123,
		UpdatedAt: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC),
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
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/awesome/sources", nil)
	secondRequest.Header.Set("If-None-Match", first.Header().Get("ETag"))
	second := httptest.NewRecorder()
	handler.HandleSources(second, secondRequest)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("second response = %d, body=%q", second.Code, second.Body.String())
	}
}

func TestAwesomeEntriesAlwaysIncludeArchivedState(t *testing.T) {
	updatedAt := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	service := stubAwesomePublicService{snapshot: model.AwesomeEntriesSnapshot{
		Source: model.AwesomeEntriesSource{ID: "awesome-mac", DisplayName: "Awesome Mac", UpdatedAt: updatedAt},
		Entries: []model.AwesomeEntry{{
			GhRepoID: ptr(int64(42)), FullName: "owner/repo", EntryTitle: "Repo",
			SectionPath: []string{"Apps"}, SourceAnchorURL: "https://example.com#apps",
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
	if !strings.Contains(response.Body.String(), `"is_archived":false`) {
		t.Fatalf("is_archived=false must remain in the public contract: %s", response.Body.String())
	}
}

type stubAwesomePublicService struct {
	sources  []model.AwesomeSource
	snapshot model.AwesomeEntriesSnapshot
}

func (s stubAwesomePublicService) ListPublishedSources(context.Context) ([]model.AwesomeSource, error) {
	return s.sources, nil
}

func (s stubAwesomePublicService) PublishedEntries(context.Context, string) (model.AwesomeEntriesSnapshot, error) {
	return s.snapshot, nil
}

func ptr[T any](value T) *T { return &value }
