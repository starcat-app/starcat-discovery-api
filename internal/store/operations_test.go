package store

import (
	"context"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestOperationalStatsAndSyncRuns(t *testing.T) {
	repository := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := repository.UpsertRepo(ctx, model.Repository{
		GhRepoID: 1, Owner: "acme", Name: "project", FullName: "acme/project",
		Stars: 10, IndexedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	runID, _, err := repository.StartSyncRun(ctx, "incremental")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.FinishSyncRun(ctx, runID, "succeeded", 3, 1, ""); err != nil {
		t.Fatal(err)
	}
	stats, err := repository.OperationalStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Repositories.Total != 1 || stats.Repositories.Available != 1 || stats.Sync.Succeeded != 1 {
		t.Fatalf("unexpected operational stats: %#v", stats)
	}
	runs, err := repository.ListSyncRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "succeeded" || runs[0].HasError {
		t.Fatalf("unexpected sync runs: %#v", runs)
	}
}

func TestOperationalStatsSupportsEmptyDatabase(t *testing.T) {
	stats, err := newTestStore(t).OperationalStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Repositories.Total != 0 || stats.StarHistory.Ready != 0 || stats.Awesome.Sources != 0 {
		t.Fatalf("unexpected empty stats: %#v", stats)
	}
}
