package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestAwesomeSourcesETagReturns304(t *testing.T) {
	service := stubAwesomePublicService{sources: []model.AwesomeSource{{
		ID: "awesome-mac", DisplayName: "Awesome Mac", RepoFullName: "owner/repo",
		RepoURL: "https://github.com/owner/repo", Status: model.AwesomeSourcePublished,
		UpdatedAt: time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC),
	}}}
	handler := NewAwesomeHandler(service)
	first := httptest.NewRecorder()
	handler.HandleSources(first, httptest.NewRequest(http.MethodGet, "/api/v1/discovery/awesome/sources", nil))
	if first.Code != http.StatusOK || first.Header().Get("ETag") == "" {
		t.Fatalf("first response = %d, headers=%v", first.Code, first.Header())
	}
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/awesome/sources", nil)
	secondRequest.Header.Set("If-None-Match", first.Header().Get("ETag"))
	second := httptest.NewRecorder()
	handler.HandleSources(second, secondRequest)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("second response = %d, body=%q", second.Code, second.Body.String())
	}
}

type stubAwesomePublicService struct {
	sources []model.AwesomeSource
}

func (s stubAwesomePublicService) ListPublishedSources(context.Context) ([]model.AwesomeSource, error) {
	return s.sources, nil
}

func (s stubAwesomePublicService) PublishedEntries(context.Context, string) (model.AwesomeEntriesSnapshot, error) {
	return model.AwesomeEntriesSnapshot{}, nil
}
