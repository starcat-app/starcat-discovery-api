package awesome

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	gh "github.com/starcat-app/starcat-discovery-api/internal/github"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"github.com/starcat-app/starcat-discovery-api/internal/store"
)

func TestServiceManagedSourceLifecycleAndFailureKeepsSnapshot(t *testing.T) {
	ctx := context.Background()
	sqliteStore, err := store.NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "awesome.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer sqliteStore.Close()
	fake := &fakeGitHubClient{
		repos: map[string]gh.Repository{
			"acme/awesome": sourceRepository(),
			"example/alpha": {
				ID: 2, FullName: "Example/Alpha", Name: "Alpha", DefaultBranch: "main",
				Owner:       gh.Owner{Login: "Example", AvatarURL: "https://avatars.example/alpha"},
				Description: "Useful repository", Homepage: "https://alpha.example", Language: "Go",
				Stargazers: 42, Forks: 7, Watchers: 8, Subscribers: 9, OpenIssues: 3,
				Topics: []string{"awesome", "tooling"}, Fork: true,
				License: &gh.License{SPDXID: "MIT"}, PushedAt: "2026-08-23T11:00:00Z",
				UpdatedAt: "2026-08-23T12:34:56Z", CreatedAt: "2020-01-02T03:04:05Z",
			},
			"alias/alpha": {
				ID: 2, FullName: "Example/Alpha", Name: "Alpha", DefaultBranch: "main",
				Owner: gh.Owner{Login: "Example"}, Stargazers: 42, UpdatedAt: "2026-08-23T12:34:56Z",
			},
		},
		readme: gh.README{
			Path: "README.md", SHA: "sha-1", HTMLURL: "https://github.com/acme/awesome/blob/main/README.md",
			Content: []byte("## Tools\n\n- [Alpha](https://github.com/Example/Alpha) - Useful tool.\n- [Alias](https://github.com/alias/alpha) - Same GitHub ID.\n- [Site](https://example.com) - External.\n"),
		},
	}
	invalidation := &recordingAwesomeInvalidator{}
	service := NewService(sqliteStore, fake, invalidation)
	created, err := service.CreateSource(ctx, model.AwesomeSource{
		ID: "awesome-test", RepoFullName: "acme/awesome", DisplayName: "Awesome Test",
	})
	if err != nil {
		t.Fatalf("CreateSource() error = %v", err)
	}
	if created.Status != model.AwesomeSourceDraft {
		t.Fatalf("created status = %q", created.Status)
	}
	run, err := service.SyncSource(ctx, created.ID, "manual")
	if err != nil || run.Status != "succeeded" || run.GitHubCount != 1 || run.ExternalCount != 1 || run.DuplicateCount != 1 {
		t.Fatalf("SyncSource() = %+v, %v", run, err)
	}
	ready, err := sqliteStore.GetAwesomeSource(ctx, created.ID)
	if err != nil || ready.Status != model.AwesomeSourceReady || ready.SourceStars != 321 || ready.GitHubRepoCount != 1 {
		t.Fatalf("ready source = %+v, %v", ready, err)
	}
	if _, err := service.PublishSource(ctx, created.ID); err != nil {
		t.Fatalf("PublishSource() error = %v", err)
	}
	snapshot, err := service.PublishedEntries(ctx, created.ID)
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].FullName != "Example/Alpha" {
		t.Fatalf("published snapshot = %+v, %v", snapshot, err)
	}
	if snapshot.Entries[0].UpdatedAt != "2026-08-23T12:34:56Z" {
		t.Fatalf("published repository updated_at = %q", snapshot.Entries[0].UpdatedAt)
	}
	entry := snapshot.Entries[0]
	if entry.Forks != 7 || entry.Watchers != 8 || entry.Subscribers != 9 || entry.OpenIssues != 3 ||
		entry.Homepage != "https://alpha.example" || entry.LicenseSpdx != "MIT" ||
		entry.DefaultBranch != "main" || entry.PushedAt != "2026-08-23T11:00:00Z" ||
		entry.CreatedAt != "2020-01-02T03:04:05Z" || !entry.IsFork ||
		len(entry.Topics) != 2 || entry.Topics[0] != "awesome" {
		t.Fatalf("published repository metadata is incomplete: %+v", entry)
	}
	if ready.LastSyncedAt == nil || !snapshot.Source.UpdatedAt.Equal(*ready.LastSyncedAt) {
		t.Fatalf("snapshot updated_at = %s, last_synced_at = %v", snapshot.Source.UpdatedAt, ready.LastSyncedAt)
	}

	updatedSource := fake.repos["acme/awesome"]
	updatedSource.Stargazers = 654
	fake.repos["acme/awesome"] = updatedSource
	if _, err := service.SyncSource(ctx, created.ID, "manual"); err != nil {
		t.Fatalf("unchanged README metadata refresh error = %v", err)
	}
	refreshed, err := sqliteStore.GetAwesomeSource(ctx, created.ID)
	if err != nil || refreshed.SourceStars != 654 {
		t.Fatalf("source stars after unchanged README refresh = %d, %v", refreshed.SourceStars, err)
	}

	fake.readme.SHA = "sha-2"
	fake.readme.Content = []byte("- [Missing](https://github.com/example/missing)\n")
	if _, err := service.SyncSource(ctx, created.ID, "manual"); err == nil {
		t.Fatal("expected second sync failure")
	}
	snapshot, err = service.PublishedEntries(ctx, created.ID)
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].FullName != "Example/Alpha" {
		t.Fatalf("failed sync replaced previous snapshot: %+v, %v", snapshot, err)
	}
	runs, err := service.SyncRuns(ctx, created.ID)
	if err != nil || len(runs) != 3 || runs[0].Status != "failed" || runs[1].Status != "succeeded" || runs[2].Status != "succeeded" {
		t.Fatalf("sync runs = %+v, %v", runs, err)
	}
	if _, err := service.ArchiveSource(ctx, created.ID); err != nil {
		t.Fatalf("ArchiveSource() error = %v", err)
	}
	if sources, err := service.ListPublishedSources(ctx); err != nil || len(sources) != 0 {
		t.Fatalf("archived public sources = %+v, %v", sources, err)
	}
	if invalidation.catalogCount < 6 || len(invalidation.sourceIDs) < 4 {
		t.Fatalf("response cache invalidations = catalog:%d sources:%v", invalidation.catalogCount, invalidation.sourceIDs)
	}
}

func TestServicePublishRequiresSuccessfulNonEmptySync(t *testing.T) {
	ctx := context.Background()
	sqliteStore, err := store.NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "awesome.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	fake := &fakeGitHubClient{repos: map[string]gh.Repository{"acme/awesome": sourceRepository()}, readme: gh.README{Path: "README.md", SHA: "sha", Content: []byte("# empty")}}
	service := NewService(sqliteStore, fake)
	created, err := service.CreateSource(ctx, model.AwesomeSource{ID: "empty", RepoFullName: "acme/awesome", DisplayName: "Empty"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishSource(ctx, created.ID); err == nil {
		t.Fatal("expected publish gate")
	}
}

func TestServiceReturnsExistingActiveRun(t *testing.T) {
	ctx := context.Background()
	sqliteStore, err := store.NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "awesome.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	service := NewService(sqliteStore, &fakeGitHubClient{repos: map[string]gh.Repository{"acme/awesome": sourceRepository()}})
	created, err := service.CreateSource(ctx, model.AwesomeSource{ID: "active", RepoFullName: "acme/awesome", DisplayName: "Active"})
	if err != nil {
		t.Fatal(err)
	}
	active := model.AwesomeSyncRun{ID: "run-active", SourceID: created.ID, Status: "running", Trigger: "manual", StartedAt: time.Now().UTC()}
	if _, err := sqliteStore.StartAwesomeSyncRun(ctx, active); err != nil {
		t.Fatal(err)
	}

	got, err := service.SyncSource(ctx, created.ID, "manual")
	if err != nil || got.ID != active.ID || got.Status != "running" {
		t.Fatalf("SyncSource() active run = %+v, %v", got, err)
	}
}

func TestServiceRecoversAfterGitHubTransientFailures(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ctx := context.Background()
			sqliteStore, err := store.NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "awesome.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer sqliteStore.Close()
			fake := &fakeGitHubClient{
				repos: map[string]gh.Repository{
					"acme/awesome":  sourceRepository(),
					"example/alpha": {ID: 2, FullName: "example/alpha", Name: "alpha", Owner: gh.Owner{Login: "example"}},
				},
				errors: map[string]error{"example/alpha": &gh.APIError{StatusCode: status, Message: "transient"}},
				readme: gh.README{Path: "README.md", SHA: "sha", Content: []byte("- [Alpha](https://github.com/example/alpha)\n")},
			}
			service := NewService(sqliteStore, fake)
			created, err := service.CreateSource(ctx, model.AwesomeSource{ID: "retry", RepoFullName: "acme/awesome", DisplayName: "Retry"})
			if err != nil {
				t.Fatal(err)
			}
			failed, err := service.SyncSource(ctx, created.ID, "manual")
			if err == nil || failed.Status != "failed" {
				t.Fatalf("first SyncSource() = %+v, %v", failed, err)
			}
			delete(fake.errors, "example/alpha")
			recovered, err := service.SyncSource(ctx, created.ID, "manual")
			if err != nil || recovered.Status != "succeeded" || recovered.GitHubCount != 1 {
				t.Fatalf("recovered SyncSource() = %+v, %v", recovered, err)
			}
		})
	}
}

type fakeGitHubClient struct {
	repos  map[string]gh.Repository
	errors map[string]error
	readme gh.README
}

type recordingAwesomeInvalidator struct {
	catalogCount int
	sourceIDs    []string
}

func (r *recordingAwesomeInvalidator) InvalidateAwesomeCatalog() {
	r.catalogCount++
}

func (r *recordingAwesomeInvalidator) InvalidateAwesomeSource(sourceID string) {
	r.sourceIDs = append(r.sourceIDs, sourceID)
}

func (f *fakeGitHubClient) GetRepository(_ context.Context, fullName string) (gh.Repository, error) {
	if err := f.errors[fullName]; err != nil {
		return gh.Repository{}, err
	}
	if repo, ok := f.repos[fullName]; ok {
		return repo, nil
	}
	return gh.Repository{}, &gh.APIError{StatusCode: 404, Path: "/repos/" + fullName, Message: "Not Found"}
}

func (f *fakeGitHubClient) GetREADME(_ context.Context, _ string) (gh.README, error) {
	return f.readme, nil
}

func sourceRepository() gh.Repository {
	return gh.Repository{ID: 1, FullName: "acme/awesome", Name: "awesome", DefaultBranch: "main", Stargazers: 321, Owner: gh.Owner{Login: "acme"}}
}
