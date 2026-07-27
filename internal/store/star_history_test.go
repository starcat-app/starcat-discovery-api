package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestStarHistoryDailyBudgetIsAtomicPersistentAndUTCScoped(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "budget.db")
	store, err := NewSQLiteStore(ctx, databasePath)
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// 十个并发预留共享一条 SQLite 记录，只允许其中五个进入每日 500 字节上限。
	now := time.Date(2026, 7, 28, 1, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	var group sync.WaitGroup
	var resultMu sync.Mutex
	successes := 0
	for index := 0; index < 10; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			reserved, reserveErr := store.ReserveStarHistoryDailyBudget(ctx, now, 100, 500)
			if reserveErr != nil {
				t.Errorf("ReserveStarHistoryDailyBudget() error = %v", reserveErr)
				return
			}
			if reserved {
				resultMu.Lock()
				successes++
				resultMu.Unlock()
			}
		}()
	}
	group.Wait()
	if successes != 5 {
		t.Fatalf("successful reservations = %d, want 5", successes)
	}

	var budgetDate string
	var reservedBytes int64
	if err := store.db.QueryRowContext(ctx, `
		SELECT budget_date, reserved_bytes
		FROM star_history_daily_budgets
	`).Scan(&budgetDate, &reservedBytes); err != nil {
		t.Fatalf("query daily budget: %v", err)
	}
	if budgetDate != "2026-07-27" || reservedBytes != 500 {
		t.Fatalf("UTC daily budget = %s/%d, want 2026-07-27/500", budgetDate, reservedBytes)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	store, err = NewSQLiteStore(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore() error = %v", err)
	}
	reserved, err := store.ReserveStarHistoryDailyBudget(ctx, now, 100, 500)
	if err != nil {
		t.Fatalf("reservation after reopen error = %v", err)
	}
	if reserved {
		t.Fatal("reopened store bypassed the exhausted daily budget")
	}

	nextDay := now.Add(24 * time.Hour)
	reserved, err = store.ReserveStarHistoryDailyBudget(ctx, nextDay, 500, 500)
	if err != nil || !reserved {
		t.Fatalf("next-day reservation = %v, %v", reserved, err)
	}
	var dayCount int
	if err := store.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM star_history_daily_budgets",
	).Scan(&dayCount); err != nil {
		t.Fatalf("count daily budgets: %v", err)
	}
	if dayCount != 2 {
		t.Fatalf("daily budget rows = %d, want 2", dayCount)
	}
}

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
