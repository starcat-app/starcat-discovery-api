package store

import (
	"context"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestStarHistoryCacheIsIndependentAndDeduplicatesBuilds(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	request := model.StarHistoryBuildRequest{
		GhRepoID:     9001,
		FullName:     "outside/catalog",
		CurrentStars: 123,
	}

	claimed, err := store.ClaimStarHistoryBuild(ctx, request, now, now.Add(5*time.Minute))
	if err != nil || !claimed {
		t.Fatalf("first ClaimStarHistoryBuild() = %v, %v", claimed, err)
	}
	claimed, err = store.ClaimStarHistoryBuild(ctx, request, now.Add(time.Minute), now.Add(6*time.Minute))
	if err != nil {
		t.Fatalf("duplicate ClaimStarHistoryBuild() error = %v", err)
	}
	if claimed {
		t.Fatal("unexpired building task must be deduplicated")
	}

	var catalogCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repos").Scan(&catalogCount); err != nil {
		t.Fatalf("count catalog repos: %v", err)
	}
	if catalogCount != 0 {
		t.Fatalf("star history polluted discovery catalog: %d rows", catalogCount)
	}

	claimed, err = store.ClaimStarHistoryBuild(
		ctx,
		request,
		now.Add(6*time.Minute),
		now.Add(11*time.Minute),
	)
	if err != nil || !claimed {
		t.Fatalf("expired build was not reclaimable: claimed=%v err=%v", claimed, err)
	}
}

func TestStarHistoryCacheRoundTripsReadyAndFailedStates(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	generatedAt := now.Add(-time.Minute)
	ready := model.StarHistoryCache{
		GhRepoID:      9002,
		FullName:      "octo/history",
		CurrentStars:  42,
		Status:        model.StarHistoryReady,
		CoverageStart: "2026-07-01",
		Points: []model.StarHistoryPoint{{
			Date:      "2026-07-01",
			Count:     10,
			Source:    model.StarHistorySourceGHArchive,
			Precision: model.StarHistoryEstimated,
		}},
		GeneratedAt: &generatedAt,
		ExpiresAt:   now.Add(24 * time.Hour),
		UpdatedAt:   now,
	}
	if err := store.SaveStarHistoryReady(ctx, ready); err != nil {
		t.Fatalf("SaveStarHistoryReady() error = %v", err)
	}
	got, found, err := store.GetStarHistoryCache(ctx, ready.GhRepoID)
	if err != nil || !found {
		t.Fatalf("GetStarHistoryCache() = found %v, err %v", found, err)
	}
	if got.Status != model.StarHistoryReady || got.CoverageStart != ready.CoverageStart ||
		len(got.Points) != 1 || got.Points[0] != ready.Points[0] {
		t.Fatalf("unexpected ready cache: %+v", got)
	}

	failed := model.StarHistoryCache{
		GhRepoID:     ready.GhRepoID,
		FullName:     ready.FullName,
		CurrentStars: ready.CurrentStars,
		Status:       model.StarHistoryFailed,
		ExpiresAt:    now.Add(10 * time.Minute),
		ErrorSummary: "provider unavailable",
		UpdatedAt:    now,
	}
	if err := store.SaveStarHistoryFailed(ctx, failed); err != nil {
		t.Fatalf("SaveStarHistoryFailed() error = %v", err)
	}
	got, found, err = store.GetStarHistoryCache(ctx, ready.GhRepoID)
	if err != nil || !found {
		t.Fatalf("GetStarHistoryCache(failed) = found %v, err %v", found, err)
	}
	if got.Status != model.StarHistoryFailed || got.ErrorSummary != failed.ErrorSummary ||
		len(got.Points) != 0 {
		t.Fatalf("unexpected failed cache: %+v", got)
	}
}

func TestStarHistoryCacheRejectsUnknownStatus(t *testing.T) {
	store := newTestStore(t)
	_, err := store.db.ExecContext(context.Background(), `
		INSERT INTO repo_star_history_cache (
			gh_repo_id, full_name, current_stars, status, points_json, expires_at, updated_at
		) VALUES (9003, 'octo/invalid', 0, 'queued', '[]', ?, ?)
	`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err == nil {
		t.Fatal("schema accepted an unknown star history status")
	}
}
