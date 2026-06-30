package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dong4j/starcat-discovery-api/internal/model"
	"github.com/dong4j/starcat-discovery-api/internal/store"
)

func TestDiscoveryHandlerMostPopular(t *testing.T) {
	sqliteStore := newHandlerStore(t)
	now := time.Now().UTC()
	if err := sqliteStore.UpsertRepo(context.Background(), model.Repository{
		GhRepoID:        10,
		Owner:           "owner",
		Name:            "repo",
		FullName:        "owner/repo",
		Language:        "Go",
		Stars:           1000,
		Topics:          []string{"tools"},
		Platforms:       []string{"cli"},
		PopularityScore: 0.8,
		DiscoveryScore:  0.7,
		IndexedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	if err := sqliteStore.ReplaceCategoryRanking(context.Background(), "most-popular", "__all__", []model.RankingEntry{
		{RepoID: 10, Rank: 1, Score: 0.8},
	}); err != nil {
		t.Fatalf("ReplaceCategoryRanking() error = %v", err)
	}

	handler := NewDiscoveryHandler(sqliteStore)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/categories/most-popular", nil)
	rec := httptest.NewRecorder()
	handler.HandleMostPopular(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope model.Envelope[[]model.DiscoveryItem]
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Data) != 1 || envelope.Data[0].Rank != 1 {
		t.Fatalf("unexpected data: %+v", envelope.Data)
	}
}

func TestDiscoveryHandlerMostPopularExplicitSortBypassesRanking(t *testing.T) {
	sqliteStore := newHandlerStore(t)
	now := time.Now().UTC()
	for _, repo := range []model.Repository{
		{
			GhRepoID:        11,
			Owner:           "ranked",
			Name:            "low-stars",
			FullName:        "ranked/low-stars",
			Language:        "Go",
			Stars:           10,
			PopularityScore: 0.9,
			DiscoveryScore:  0.7,
			IndexedAt:       now,
		},
		{
			GhRepoID:        12,
			Owner:           "ranked",
			Name:            "high-stars",
			FullName:        "ranked/high-stars",
			Language:        "Go",
			Stars:           1000,
			PopularityScore: 0.1,
			DiscoveryScore:  0.6,
			IndexedAt:       now,
		},
		{
			GhRepoID:        13,
			Owner:           "ranked",
			Name:            "not-popular",
			FullName:        "ranked/not-popular",
			Language:        "Go",
			Stars:           500,
			PopularityScore: 0.1,
			DiscoveryScore:  0.4,
			IndexedAt:       now,
		},
	} {
		if err := sqliteStore.UpsertRepo(context.Background(), repo); err != nil {
			t.Fatalf("UpsertRepo() error = %v", err)
		}
	}
	if err := sqliteStore.ReplaceCategoryRanking(context.Background(), "most-popular", "__all__", []model.RankingEntry{
		{RepoID: 11, Rank: 1, Score: 0.9},
		{RepoID: 12, Rank: 2, Score: 0.1},
	}); err != nil {
		t.Fatalf("ReplaceCategoryRanking() error = %v", err)
	}

	handler := NewDiscoveryHandler(sqliteStore)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/categories/most-popular?sort=stars", nil)
	rec := httptest.NewRecorder()
	handler.HandleMostPopular(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope model.Envelope[[]model.DiscoveryItem]
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Data) != 2 || envelope.Data[0].RepoID != 12 {
		t.Fatalf("explicit sort should bypass rank order, data=%+v", envelope.Data)
	}
}

func TestDiscoveryHandlerEmptyMostPopularReturnsArray(t *testing.T) {
	sqliteStore := newHandlerStore(t)
	handler := NewDiscoveryHandler(sqliteStore)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/categories/most-popular?limit=5", nil)
	rec := httptest.NewRecorder()

	handler.HandleMostPopular(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"data":[]`) {
		t.Fatalf("empty list must be encoded as JSON array, body=%s", rec.Body.String())
	}
	var envelope model.Envelope[[]model.DiscoveryItem]
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Data == nil {
		t.Fatalf("data slice must be non-nil")
	}
}

func TestDiscoveryHandlerBulk(t *testing.T) {
	sqliteStore := newHandlerStore(t)
	now := time.Now().UTC()
	if err := sqliteStore.UpsertRepo(context.Background(), model.Repository{
		GhRepoID:        50,
		Owner:           "bulk",
		Name:            "repo",
		FullName:        "bulk/repo",
		Language:        "Swift",
		Stars:           42,
		Topics:          []string{"tools"},
		Platforms:       []string{"macos"},
		PopularityScore: 0.8,
		ReleaseScore:    0.6,
		DiscoveryScore:  0.9,
		IndexedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	if err := sqliteStore.ReplaceCategoryRanking(context.Background(), "most-popular", "__all__", []model.RankingEntry{
		{RepoID: 50, Rank: 1, Score: 0.8},
	}); err != nil {
		t.Fatalf("ReplaceCategoryRanking(popular) error = %v", err)
	}
	if err := sqliteStore.ReplaceCategoryRanking(context.Background(), "new-releases", "__all__", []model.RankingEntry{
		{RepoID: 50, Rank: 2, Score: 0.6},
	}); err != nil {
		t.Fatalf("ReplaceCategoryRanking(new-releases) error = %v", err)
	}

	handler := NewDiscoveryHandler(sqliteStore)
	cache := NewBulkCache(15 * time.Minute)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/bulk", nil)
	rec := httptest.NewRecorder()
	handler.HandleBulk(cache)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope model.Envelope[model.DiscoveryBulk]
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Data.Repos) != 1 || envelope.Data.Repos[0].DiscoveryScore == 0 {
		t.Fatalf("unexpected bulk repos: %+v", envelope.Data.Repos)
	}
	if envelope.Data.Repos[0].CategoryRanks["popular"] != 1 || envelope.Data.Repos[0].CategoryRanks["new_releases"] != 2 {
		t.Fatalf("bulk repos should include category membership: %+v", envelope.Data.Repos[0])
	}
	if len(envelope.Data.Summary.Modes) != 3 {
		t.Fatalf("unexpected bulk summary: %+v", envelope.Data.Summary)
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatalf("bulk response should include ETag")
	}
}

func TestDiscoveryHandlerItemArrayFieldsAreAlwaysPresent(t *testing.T) {
	sqliteStore := newHandlerStore(t)
	now := time.Now().UTC()
	if err := sqliteStore.UpsertRepo(context.Background(), model.Repository{
		GhRepoID:       20,
		Owner:          "owner",
		Name:           "empty-arrays",
		FullName:       "owner/empty-arrays",
		Language:       "Go",
		IndexedAt:      now,
		DiscoveryScore: 0.5,
	}); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}

	handler := NewDiscoveryHandler(sqliteStore)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/feed", nil)
	rec := httptest.NewRecorder()
	handler.HandleFeed(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope model.Envelope[[]model.DiscoveryItem]
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("unexpected data: %+v", envelope.Data)
	}
	item := envelope.Data[0]
	if item.Topics == nil || item.Platforms == nil || item.Reasons == nil || item.Signals == nil {
		t.Fatalf("array fields must be present as empty arrays, item=%+v body=%s", item, rec.Body.String())
	}
}

func TestDiscoveryHandlerSummary(t *testing.T) {
	sqliteStore := newHandlerStore(t)
	now := time.Now().UTC()
	if err := sqliteStore.UpsertRepo(context.Background(), model.Repository{
		GhRepoID:       40,
		Owner:          "summary",
		Name:           "repo",
		FullName:       "summary/repo",
		Language:       "Swift",
		Topics:         []string{"tools"},
		Platforms:      []string{"macos"},
		IndexedAt:      now,
		DiscoveryScore: 0.5,
	}); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}

	handler := NewDiscoveryHandler(sqliteStore)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/summary", nil)
	rec := httptest.NewRecorder()
	handler.HandleSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var envelope model.Envelope[model.DiscoverySummary]
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if len(envelope.Data.Modes) != 3 {
		t.Fatalf("expected 3 discovery modes, got %+v", envelope.Data.Modes)
	}
	discover := findSummaryMode(t, envelope.Data, "discover")
	if discover.Total != 1 || len(discover.Topics) == 0 || len(discover.Platforms) == 0 {
		t.Fatalf("unexpected discover summary: %+v", discover)
	}
}

func newHandlerStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore(context.Background(), filepath.Join(t.TempDir(), "discovery.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	return sqliteStore
}

func findSummaryMode(t *testing.T, summary model.DiscoverySummary, mode string) model.ModeSummary {
	t.Helper()
	for _, item := range summary.Modes {
		if item.Mode == mode {
			return item
		}
	}
	t.Fatalf("mode %q not found in %+v", mode, summary.Modes)
	return model.ModeSummary{}
}
