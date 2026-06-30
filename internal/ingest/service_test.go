package ingest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dong4j/starcat-discovery-api/internal/github"
	"github.com/dong4j/starcat-discovery-api/internal/model"
	"github.com/dong4j/starcat-discovery-api/internal/store"
)

func TestServiceSync(t *testing.T) {
	sqliteStore, err := store.NewSQLiteStore(context.Background(), filepath.Join(t.TempDir(), "discovery.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = sqliteStore.Close() })

	client := &fakeGitHubClient{}
	service := NewService(sqliteStore, client, 10)
	service.now = func() time.Time { return time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC) }

	result, err := service.Sync(context.Background(), "test")
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if result.Status != "success" || result.ReposUpserted == 0 {
		t.Fatalf("unexpected sync result: %+v", result)
	}

	page, err := sqliteStore.ListCategoryRanking(context.Background(), "most-popular", "__all__", 1, 20)
	if err != nil {
		t.Fatalf("ListCategoryRanking() error = %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatalf("expected ranking items")
	}
}

func TestServiceSyncPrunesStaleReposOnlyForFullMode(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name       string
		mode       string
		wantPruned int
		wantTotal  int
	}{
		{name: "light sync keeps stale repos", mode: "scheduled-light", wantPruned: 0, wantTotal: 2},
		{name: "full sync prunes stale repos", mode: "scheduled-full", wantPruned: 1, wantTotal: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			sqliteStore, err := store.NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "discovery.db"))
			if err != nil {
				t.Fatalf("NewSQLiteStore() error = %v", err)
			}
			t.Cleanup(func() { _ = sqliteStore.Close() })
			if err := sqliteStore.UpsertRepo(ctx, model.Repository{
				GhRepoID:       999,
				Owner:          "stale",
				Name:           "old",
				FullName:       "stale/old",
				DiscoveryScore: 0.1,
				IndexedAt:      now,
			}); err != nil {
				t.Fatalf("UpsertRepo(stale) error = %v", err)
			}

			service := NewService(sqliteStore, &fakeGitHubClient{}, 10)
			service.now = func() time.Time { return now }
			result, err := service.Sync(ctx, testCase.mode)
			if err != nil {
				t.Fatalf("Sync() error = %v", err)
			}
			if result.ReposPruned != testCase.wantPruned {
				t.Fatalf("ReposPruned = %d, want %d; result=%+v", result.ReposPruned, testCase.wantPruned, result)
			}
			page, err := sqliteStore.ListScoredRepos(ctx, "discovery_score", store.QueryFilters{}, 1, 20)
			if err != nil {
				t.Fatalf("ListScoredRepos() error = %v", err)
			}
			if page.Total != testCase.wantTotal {
				t.Fatalf("total repos = %d, want %d; items=%+v", page.Total, testCase.wantTotal, page.Items)
			}
		})
	}
}

type fakeGitHubClient struct{}

func (f *fakeGitHubClient) SearchRepositories(ctx context.Context, query string, perPage int) ([]github.Repository, error) {
	return []github.Repository{{
		ID:       100,
		FullName: "openclaw/openclaw",
	}}, nil
}

func (f *fakeGitHubClient) GetRepository(ctx context.Context, fullName string) (github.Repository, error) {
	return github.Repository{
		ID:            100,
		Name:          "openclaw",
		FullName:      "openclaw/openclaw",
		Description:   "Local AI assistant",
		Language:      "TypeScript",
		Stargazers:    4200,
		Forks:         210,
		Watchers:      4200,
		Subscribers:   120,
		DefaultBranch: "main",
		Topics:        []string{"llm", "cli"},
		PushedAt:      "2026-06-20T12:00:00Z",
		UpdatedAt:     "2026-06-20T12:00:00Z",
		CreatedAt:     "2025-01-01T12:00:00Z",
		Owner: github.Owner{
			Login:     "openclaw",
			AvatarURL: "https://example.com/avatar.png",
		},
	}, nil
}

func (f *fakeGitHubClient) ListReleases(ctx context.Context, fullName string, perPage int) ([]github.Release, error) {
	return []github.Release{{
		TagName:     "v1.0.0",
		Name:        "Stable",
		HTMLURL:     "https://github.com/openclaw/openclaw/releases/tag/v1.0.0",
		PublishedAt: "2026-06-25T12:00:00Z",
		Assets: []github.Asset{
			{Name: "openclaw-darwin.dmg", DownloadCount: 300},
			{Name: "openclaw-linux.AppImage", DownloadCount: 200},
		},
	}}, nil
}
