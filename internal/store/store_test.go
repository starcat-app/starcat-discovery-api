package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dong4j/starcat-discovery-api/internal/model"
)

func TestSQLiteStoreUpsertAndListScoredRepos(t *testing.T) {
	store := newTestStore(t)
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)

	err := store.UpsertRepo(context.Background(), model.Repository{
		GhRepoID:             1,
		Owner:                "openclaw",
		Name:                 "openclaw",
		FullName:             "openclaw/openclaw",
		Description:          "Personal AI assistant",
		Language:             "TypeScript",
		Stars:                4200,
		Forks:                200,
		Topics:               []string{"ai", "tools"},
		Platforms:            []string{"macos", "linux"},
		LatestReleaseTag:     "v1.0.0",
		LatestReleaseAt:      &now,
		ReleaseDownloadCount: 9000,
		PopularityScore:      0.91,
		DiscoveryScore:       0.87,
		IndexedAt:            now,
	})
	if err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}

	page, err := store.ListScoredRepos(context.Background(), "discovery_score", QueryFilters{
		Topic:    "ai",
		Platform: "macos",
	}, 1, 20)
	if err != nil {
		t.Fatalf("ListScoredRepos() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("expected one item, total=%d len=%d", page.Total, len(page.Items))
	}
	if got := page.Items[0].FullName; got != "openclaw/openclaw" {
		t.Fatalf("FullName = %q", got)
	}
	if page.Items[0].Signals == nil || len(page.Items[0].Signals) == 0 {
		t.Fatalf("expected explanatory signals")
	}
}

func TestSQLiteStoreCategoryRanking(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	if err := store.UpsertRepo(context.Background(), model.Repository{
		GhRepoID:        2,
		Owner:           "godotengine",
		Name:            "godot",
		FullName:        "godotengine/godot",
		Language:        "C++",
		Stars:           113000,
		Topics:          []string{"tools"},
		Platforms:       []string{"android", "linux"},
		PopularityScore: 0.99,
		IndexedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	if err := store.ReplaceCategoryRanking(context.Background(), "most-popular", "language:C++", []model.RankingEntry{
		{RepoID: 2, Rank: 1, Score: 0.99},
	}); err != nil {
		t.Fatalf("ReplaceCategoryRanking() error = %v", err)
	}
	page, err := store.ListCategoryRanking(context.Background(), "most-popular", "language:C++", 1, 20)
	if err != nil {
		t.Fatalf("ListCategoryRanking() error = %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Rank != 1 {
		t.Fatalf("unexpected ranking page: %+v", page)
	}
}

func TestSQLiteStoreUpsertRelease(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	if err := store.UpsertRepo(context.Background(), model.Repository{
		GhRepoID:  5,
		Owner:     "release",
		Name:      "app",
		FullName:  "release/app",
		IndexedAt: now,
	}); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	if err := store.UpsertRelease(context.Background(), model.Release{
		GhRepoID:      5,
		TagName:       "v1.0.0",
		Name:          "First stable release",
		HTMLURL:       "https://github.com/release/app/releases/tag/v1.0.0",
		PublishedAt:   now,
		DownloadCount: 42,
		Assets: []model.ReleaseAsset{
			{Name: "app-macos.zip", BrowserDownloadURL: "https://example.com/app.zip", DownloadCount: 42},
		},
		IndexedAt: now,
	}); err != nil {
		t.Fatalf("UpsertRelease() error = %v", err)
	}
}

func TestSQLiteStoreLanguages(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	for _, repo := range []model.Repository{
		{GhRepoID: 3, Owner: "a", Name: "swift", FullName: "a/swift", Language: "Swift", IndexedAt: now},
		{GhRepoID: 4, Owner: "b", Name: "none", FullName: "b/none", IndexedAt: now},
	} {
		if err := store.UpsertRepo(context.Background(), repo); err != nil {
			t.Fatalf("UpsertRepo() error = %v", err)
		}
	}
	languages, err := store.ListLanguages(context.Background())
	if err != nil {
		t.Fatalf("ListLanguages() error = %v", err)
	}
	if len(languages) != 2 {
		t.Fatalf("expected 2 languages, got %d", len(languages))
	}
	if languages[1].Key != model.UncategorizedLanguageKey {
		t.Fatalf("uncategorized should be last, got %+v", languages)
	}
}

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
