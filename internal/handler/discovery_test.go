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

func newHandlerStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	sqliteStore, err := store.NewSQLiteStore(context.Background(), filepath.Join(t.TempDir(), "discovery.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })
	return sqliteStore
}
