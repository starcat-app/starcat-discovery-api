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
	if err := store.UpsertRepo(context.Background(), model.Repository{
		GhRepoID:        22,
		Owner:           "toy",
		Name:            "small",
		FullName:        "toy/small",
		Language:        "C++",
		Stars:           20,
		Topics:          []string{"tools"},
		Platforms:       []string{"linux"},
		PopularityScore: 0.2,
		IndexedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertRepo(small) error = %v", err)
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
	entries, err := store.TopRankingEntries(context.Background(), "popularity_score", QueryFilters{Language: "C++"}, 10)
	if err != nil {
		t.Fatalf("TopRankingEntries() error = %v", err)
	}
	if len(entries) != 2 || entries[0].RepoID != 2 || entries[1].RepoID != 22 {
		t.Fatalf("unexpected top ranking entries: %+v", entries)
	}
	qualified, err := store.TopRankingEntries(context.Background(), "popularity_score", QueryFilters{
		Language: "C++",
		Category: "most-popular",
	}, 10)
	if err != nil {
		t.Fatalf("TopRankingEntries(popular) error = %v", err)
	}
	if len(qualified) != 1 || qualified[0].RepoID != 2 {
		t.Fatalf("popular eligibility should exclude small repos: %+v", qualified)
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

func TestSQLiteStoreSnapshotAndExposure(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	if err := store.UpsertRepo(context.Background(), model.Repository{
		GhRepoID:  6,
		Owner:     "snapshot",
		Name:      "app",
		FullName:  "snapshot/app",
		IndexedAt: now,
	}); err != nil {
		t.Fatalf("UpsertRepo() error = %v", err)
	}
	if err := store.RecordDailySnapshot(context.Background(), model.DailySnapshot{
		Date:                 "2026-06-30",
		GhRepoID:             6,
		Stars:                10,
		Forks:                2,
		Watchers:             10,
		ReleaseDownloadCount: 5,
		CapturedAt:           now,
	}); err != nil {
		t.Fatalf("RecordDailySnapshot() error = %v", err)
	}
	if err := store.RecordFeedExposure(context.Background(), "feed:all", []int64{6}); err != nil {
		t.Fatalf("RecordFeedExposure() error = %v", err)
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

func TestSQLiteStoreDiscoverySummary(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	repos := []model.Repository{
		{
			GhRepoID:        30,
			Owner:           "ai",
			Name:            "assistant",
			FullName:        "ai/assistant",
			Language:        "TypeScript",
			Topics:          []string{"ai", "tools"},
			Platforms:       []string{"macos", "linux"},
			PopularityScore: 0.9,
			ReleaseScore:    0.8,
			TrendingScore:   0.7,
			DiscoveryScore:  0.95,
			IndexedAt:       now,
		},
		{
			GhRepoID:        31,
			Owner:           "privacy",
			Name:            "proxy",
			FullName:        "privacy/proxy",
			Language:        "Go",
			Topics:          []string{"privacy", "networking"},
			Platforms:       []string{"server", "linux"},
			PopularityScore: 0.6,
			ReleaseScore:    0.4,
			TrendingScore:   0.5,
			DiscoveryScore:  0.7,
			IndexedAt:       now,
		},
	}
	for _, repo := range repos {
		if err := store.UpsertRepo(context.Background(), repo); err != nil {
			t.Fatalf("UpsertRepo() error = %v", err)
		}
	}
	if err := store.ReplaceCategoryRanking(context.Background(), "most-popular", model.AllBucket, []model.RankingEntry{
		{RepoID: 30, Rank: 1, Score: 0.9},
	}); err != nil {
		t.Fatalf("ReplaceCategoryRanking(popular) error = %v", err)
	}
	if err := store.ReplaceCategoryRanking(context.Background(), "new-releases", model.AllBucket, []model.RankingEntry{
		{RepoID: 31, Rank: 1, Score: 0.8},
	}); err != nil {
		t.Fatalf("ReplaceCategoryRanking(new-releases) error = %v", err)
	}

	summary, err := store.DiscoverySummary(context.Background())
	if err != nil {
		t.Fatalf("DiscoverySummary() error = %v", err)
	}
	if len(summary.Modes) != 3 {
		t.Fatalf("expected 3 discovery modes, got %+v", summary.Modes)
	}
	discover := findMode(t, summary, "discover")
	if discover.Total != 2 {
		t.Fatalf("discover total = %d", discover.Total)
	}
	if countForFacet(discover.Topics, "ai") != 1 || countForFacet(discover.Platforms, "linux") != 2 {
		t.Fatalf("unexpected discover facets: topics=%+v platforms=%+v", discover.Topics, discover.Platforms)
	}
	popular := findMode(t, summary, "popular")
	if popular.Total != 1 || countForFacet(popular.Languages, "TypeScript") != 1 {
		t.Fatalf("unexpected popular summary: %+v", popular)
	}
	newReleases := findMode(t, summary, "new_releases")
	if newReleases.Total != 1 || countForFacet(newReleases.Languages, "Go") != 1 {
		t.Fatalf("unexpected new releases summary: %+v", newReleases)
	}

	allRepos, err := store.ListAllRepos(context.Background())
	if err != nil {
		t.Fatalf("ListAllRepos() error = %v", err)
	}
	for _, repo := range allRepos {
		if repo.RepoID == 30 && repo.CategoryRanks["popular"] != 1 {
			t.Fatalf("expected popular category membership on repo 30, repo=%+v", repo)
		}
		if repo.RepoID == 31 && repo.CategoryRanks["new_releases"] != 1 {
			t.Fatalf("expected new_releases category membership on repo 31, repo=%+v", repo)
		}
	}
}

func TestSQLiteStoreNewReleaseEligibilityRequiresStableAssetRelease(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	recent := now.AddDate(0, 0, -7)
	for _, repo := range []model.Repository{
		{
			GhRepoID:        40,
			Owner:           "release",
			Name:            "qualified",
			FullName:        "release/qualified",
			Language:        "Swift",
			Stars:           1000,
			LatestReleaseAt: &recent,
			ReleaseScore:    0.2,
			IndexedAt:       now,
		},
		{
			GhRepoID:        41,
			Owner:           "release",
			Name:            "no-assets",
			FullName:        "release/no-assets",
			Language:        "Swift",
			Stars:           2000,
			LatestReleaseAt: &recent,
			ReleaseScore:    0.9,
			IndexedAt:       now,
		},
	} {
		if err := store.UpsertRepo(context.Background(), repo); err != nil {
			t.Fatalf("UpsertRepo() error = %v", err)
		}
	}
	if err := store.UpsertRelease(context.Background(), model.Release{
		GhRepoID:      40,
		TagName:       "v1.0.0",
		HTMLURL:       "https://github.com/release/qualified/releases/tag/v1.0.0",
		PublishedAt:   recent,
		DownloadCount: 10,
		Assets: []model.ReleaseAsset{
			{Name: "qualified.zip", DownloadCount: 10},
		},
		IndexedAt: now,
	}); err != nil {
		t.Fatalf("UpsertRelease(qualified) error = %v", err)
	}
	if err := store.UpsertRelease(context.Background(), model.Release{
		GhRepoID:    41,
		TagName:     "v1.0.0",
		HTMLURL:     "https://github.com/release/no-assets/releases/tag/v1.0.0",
		PublishedAt: recent,
		IndexedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertRelease(no-assets) error = %v", err)
	}

	entries, err := store.TopRankingEntries(context.Background(), "release_score", QueryFilters{
		Category:     "new-releases",
		MinReleaseAt: now.AddDate(0, 0, -180).Format(time.RFC3339),
	}, 10)
	if err != nil {
		t.Fatalf("TopRankingEntries(new-releases) error = %v", err)
	}
	if len(entries) != 1 || entries[0].RepoID != 40 {
		t.Fatalf("new release eligibility should require stable release assets: %+v", entries)
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

func findMode(t *testing.T, summary model.DiscoverySummary, mode string) model.ModeSummary {
	t.Helper()
	for _, item := range summary.Modes {
		if item.Mode == mode {
			return item
		}
	}
	t.Fatalf("mode %q not found in %+v", mode, summary.Modes)
	return model.ModeSummary{}
}

func countForFacet(facets []model.FacetCount, key string) int {
	for _, facet := range facets {
		if facet.Key == key {
			return facet.Count
		}
	}
	return 0
}
