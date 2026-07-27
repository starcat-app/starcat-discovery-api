package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/github"
	"github.com/starcat-app/starcat-discovery-api/internal/middleware"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"github.com/starcat-app/starcat-discovery-api/internal/starhistory"
)

func TestStarHistoryHandlerReturnsReadyEnvelopeAndSupportsETag(t *testing.T) {
	generatedAt := time.Date(2026, 7, 27, 8, 30, 0, 0, time.UTC)
	service := &stubStarHistoryService{lookup: starhistory.LookupResult{
		State:         starhistory.LookupReady,
		CurrentStars:  42,
		MaxAgeSeconds: 3600,
		Series: model.StarHistorySeries{
			Range:         model.StarHistoryRangeOneYear,
			CoverageStart: "2025-07-27",
			GeneratedAt:   generatedAt,
			Points: []model.StarHistoryPoint{{
				Date:      "2026-07-27",
				Count:     42,
				Source:    model.StarHistorySourceGHArchive,
				Precision: model.StarHistoryEstimated,
			}},
		},
	}}
	repository := &stubRepositoryClient{}
	handler := NewStarHistoryHandler(service, repository, true)

	first := serveStarHistoryRequest(handler, "/api/v1/repos/octo/history/star-history?repo_id=9", "test-api-key", "")
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", first.Code, first.Body.String())
	}
	if first.Header().Get("Cache-Control") != "private, max-age=3600" ||
		first.Header().Get("ETag") == "" {
		t.Fatalf("missing cache headers: %+v", first.Header())
	}
	var envelope model.Envelope[model.StarHistoryResponse]
	if err := json.Unmarshal(first.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode ready envelope: %v", err)
	}
	if envelope.SchemaVersion != 1 || envelope.Data.RepoID != 9 ||
		envelope.Data.Range != model.StarHistoryRangeOneYear ||
		envelope.Meta == nil || envelope.Meta.Cache != "hit" ||
		envelope.Meta.MaxAgeSeconds != 3600 {
		t.Fatalf("unexpected ready envelope: %+v", envelope)
	}
	if repository.calls != 0 {
		t.Fatal("ready cache unexpectedly requested GitHub metadata")
	}

	second := serveStarHistoryRequest(
		handler,
		"/api/v1/repos/octo/history/star-history?repo_id=9",
		"test-api-key",
		first.Header().Get("ETag"),
	)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional response = %d body=%s", second.Code, second.Body.String())
	}
}

func TestStarHistoryHandlerValidatesPublicRepositoryBeforeEnqueue(t *testing.T) {
	service := &stubStarHistoryService{lookup: starhistory.LookupResult{State: starhistory.LookupMiss}}
	repository := &stubRepositoryClient{repository: github.Repository{
		ID:         9,
		FullName:   "octo/history",
		Stargazers: 42,
	}}
	handler := NewStarHistoryHandler(service, repository, true)

	response := serveStarHistoryRequest(
		handler,
		"/api/v1/repos/octo/history/star-history?repo_id=9&range=all",
		"test-api-key",
		"",
	)
	if response.Code != http.StatusAccepted ||
		response.Header().Get("Retry-After") != "5" {
		t.Fatalf("status = %d headers=%+v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if service.enqueue.GhRepoID != 9 || service.enqueue.CurrentStars != 42 ||
		service.enqueue.FullName != "octo/history" {
		t.Fatalf("unexpected build request: %+v", service.enqueue)
	}
	assertErrorCode(t, response, "STAR_HISTORY_BUILDING")
}

func TestStarHistoryHandlerMapsStableErrorSemantics(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		apiKey     string
		enabled    bool
		lookup     starhistory.LookupResult
		lookupErr  error
		repository github.Repository
		repoErr    error
		enqueueErr error
		status     int
		code       string
		retryAfter string
	}{
		{
			name: "unauthorized", url: "/api/v1/repos/octo/history/star-history?repo_id=9",
			enabled: true, status: http.StatusUnauthorized, code: "UNAUTHORIZED",
		},
		{
			name: "invalid repo id", url: "/api/v1/repos/octo/history/star-history",
			apiKey: "test-api-key", enabled: true, status: http.StatusBadRequest, code: "INVALID_REPOSITORY",
		},
		{
			name: "repository not found", url: "/api/v1/repos/octo/history/star-history?repo_id=9",
			apiKey: "test-api-key", enabled: true, lookup: starhistory.LookupResult{State: starhistory.LookupMiss},
			repoErr: &github.APIError{StatusCode: http.StatusNotFound},
			status:  http.StatusNotFound, code: "REPOSITORY_NOT_FOUND",
		},
		{
			name: "repository id mismatch", url: "/api/v1/repos/octo/history/star-history?repo_id=9",
			apiKey: "test-api-key", enabled: true, lookup: starhistory.LookupResult{State: starhistory.LookupMiss},
			repository: github.Repository{ID: 10, FullName: "octo/history"},
			status:     http.StatusConflict, code: "REPOSITORY_ID_MISMATCH",
		},
		{
			name: "private repository", url: "/api/v1/repos/octo/history/star-history?repo_id=9",
			apiKey: "test-api-key", enabled: true, lookup: starhistory.LookupResult{State: starhistory.LookupMiss},
			repository: github.Repository{ID: 9, FullName: "octo/history", Private: true},
			status:     http.StatusUnprocessableEntity, code: "PRIVATE_REPOSITORY_UNSUPPORTED",
		},
		{
			name: "github rate limited", url: "/api/v1/repos/octo/history/star-history?repo_id=9",
			apiKey: "test-api-key", enabled: true, lookup: starhistory.LookupResult{State: starhistory.LookupMiss},
			repoErr: &github.APIError{StatusCode: http.StatusTooManyRequests},
			status:  http.StatusTooManyRequests, code: "RATE_LIMITED", retryAfter: "30",
		},
		{
			name: "queue full", url: "/api/v1/repos/octo/history/star-history?repo_id=9",
			apiKey: "test-api-key", enabled: true, lookup: starhistory.LookupResult{State: starhistory.LookupMiss},
			repository: github.Repository{ID: 9, FullName: "octo/history"},
			enqueueErr: starhistory.ErrQueueFull,
			status:     http.StatusTooManyRequests, code: "RATE_LIMITED", retryAfter: "30",
		},
		{
			name: "negative cache", url: "/api/v1/repos/octo/history/star-history?repo_id=9",
			apiKey: "test-api-key", enabled: true,
			lookup: starhistory.LookupResult{State: starhistory.LookupFailed},
			status: http.StatusServiceUnavailable, code: "HISTORY_PROVIDER_UNAVAILABLE",
		},
		{
			name: "provider disabled", url: "/api/v1/repos/octo/history/star-history?repo_id=9",
			apiKey: "test-api-key", enabled: false,
			status: http.StatusServiceUnavailable, code: "HISTORY_PROVIDER_UNAVAILABLE",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &stubStarHistoryService{
				lookup:     test.lookup,
				lookupErr:  test.lookupErr,
				enqueueErr: test.enqueueErr,
			}
			repository := &stubRepositoryClient{
				repository: test.repository,
				err:        test.repoErr,
			}
			response := serveStarHistoryRequest(
				NewStarHistoryHandler(service, repository, test.enabled),
				test.url,
				test.apiKey,
				"",
			)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, test.status, response.Body.String())
			}
			if response.Header().Get("Retry-After") != test.retryAfter {
				t.Fatalf("Retry-After = %q, want %q", response.Header().Get("Retry-After"), test.retryAfter)
			}
			assertErrorCode(t, response, test.code)
		})
	}
}

func serveStarHistoryRequest(
	handler *StarHistoryHandler,
	url string,
	apiKey string,
	ifNoneMatch string,
) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	auth := middleware.NewBearerAuth("test", []string{"test-api-key"})
	mux.Handle(
		"GET /api/v1/repos/{owner}/{repo}/star-history",
		auth.Wrap(http.HandlerFunc(handler.HandleStarHistory)),
	)
	request := httptest.NewRequest(http.MethodGet, url, nil)
	if apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if ifNoneMatch != "" {
		request.Header.Set("If-None-Match", ifNoneMatch)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func assertErrorCode(t *testing.T, response *httptest.ResponseRecorder, expected string) {
	t.Helper()
	var envelope model.ErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, response.Body.String())
	}
	if envelope.SchemaVersion != 1 || envelope.Error.Code != expected {
		t.Fatalf("error envelope = %+v, want code %s", envelope, expected)
	}
}

type stubStarHistoryService struct {
	lookup     starhistory.LookupResult
	lookupErr  error
	enqueue    model.StarHistoryBuildRequest
	enqueueErr error
}

func (s *stubStarHistoryService) Lookup(
	_ context.Context,
	_ int64,
	_ string,
	_ model.StarHistoryRange,
) (starhistory.LookupResult, error) {
	return s.lookup, s.lookupErr
}

func (s *stubStarHistoryService) Enqueue(
	_ context.Context,
	request model.StarHistoryBuildRequest,
) (bool, error) {
	s.enqueue = request
	return s.enqueueErr == nil, s.enqueueErr
}

type stubRepositoryClient struct {
	repository github.Repository
	err        error
	calls      int
}

func (c *stubRepositoryClient) GetRepository(
	_ context.Context,
	_ string,
) (github.Repository, error) {
	c.calls++
	return c.repository, c.err
}
