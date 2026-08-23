package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestAwesomeSourceCreateUpdateAndPublishedFilter(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	created, err := store.CreateAwesomeSource(ctx, model.AwesomeSource{
		ID:           "awesome-mac",
		RepoFullName: "jaywcjlove/awesome-mac",
		DisplayName:  "Awesome Mac",
		SummaryZH:    "macOS 清单",
		Featured:     true,
		SortOrder:    10,
	})
	if err != nil {
		t.Fatalf("CreateAwesomeSource() error = %v", err)
	}
	if created.Status != model.AwesomeSourceDraft || created.Revision != 1 {
		t.Fatalf("created source = %+v", created)
	}

	created.DisplayName = "Awesome macOS"
	updated, err := store.UpdateAwesomeSource(ctx, created, 1)
	if err != nil {
		t.Fatalf("UpdateAwesomeSource() error = %v", err)
	}
	if updated.DisplayName != "Awesome macOS" || updated.Revision != 2 {
		t.Fatalf("updated source = %+v", updated)
	}
	if _, err := store.UpdateAwesomeSource(ctx, created, 1); !errors.Is(err, ErrAwesomeSourceRevisionConflict) {
		t.Fatalf("stale update error = %v", err)
	}

	published, err := store.SetAwesomeSourceStatus(ctx, created.ID, model.AwesomeSourcePublished)
	if err != nil {
		t.Fatalf("SetAwesomeSourceStatus() error = %v", err)
	}
	if published.Status != model.AwesomeSourcePublished {
		t.Fatalf("published status = %q", published.Status)
	}
	publicSources, err := store.ListPublishedAwesomeSources(ctx)
	if err != nil || len(publicSources) != 1 || publicSources[0].ID != created.ID {
		t.Fatalf("published sources = %+v, err = %v", publicSources, err)
	}
}

func TestAwesomeSourceCanonicalRepoIsUniqueIgnoringCase(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, err := store.CreateAwesomeSource(ctx, model.AwesomeSource{
		ID: "one", RepoFullName: "Owner/Repo", DisplayName: "One",
	})
	if err != nil {
		t.Fatalf("first CreateAwesomeSource() error = %v", err)
	}
	_, err = store.CreateAwesomeSource(ctx, model.AwesomeSource{
		ID: "two", RepoFullName: "owner/repo", DisplayName: "Two",
	})
	if err == nil {
		t.Fatal("expected canonical repo conflict")
	}
}

func TestAwesomeSyncRunAllowsOnlyOneActiveRunPerSource(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, err := store.CreateAwesomeSource(ctx, model.AwesomeSource{
		ID: "awesome-go", RepoFullName: "avelino/awesome-go", DisplayName: "Awesome Go",
	})
	if err != nil {
		t.Fatalf("CreateAwesomeSource() error = %v", err)
	}
	run := model.AwesomeSyncRun{
		ID: "run-1", SourceID: "awesome-go", Status: "running", Trigger: "manual", StartedAt: time.Now().UTC(),
	}
	if _, err := store.StartAwesomeSyncRun(ctx, run); err != nil {
		t.Fatalf("StartAwesomeSyncRun() error = %v", err)
	}
	run.ID = "run-2"
	if _, err := store.StartAwesomeSyncRun(ctx, run); !errors.Is(err, ErrAwesomeSyncInProgress) {
		t.Fatalf("second active run error = %v", err)
	}
	run.ID = "run-1"
	run.Status = "succeeded"
	if err := store.FinishAwesomeSyncRun(ctx, run); err != nil {
		t.Fatalf("FinishAwesomeSyncRun() error = %v", err)
	}
	run.ID = "run-2"
	run.Status = "running"
	if _, err := store.StartAwesomeSyncRun(ctx, run); err != nil {
		t.Fatalf("new run after finish error = %v", err)
	}
}
