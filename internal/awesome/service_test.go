package awesome

import (
	"context"
	"path/filepath"
	"testing"

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
			"acme/awesome":  sourceRepository(),
			"example/alpha": {ID: 2, FullName: "Example/Alpha", Name: "Alpha", DefaultBranch: "main", Owner: gh.Owner{Login: "Example"}, Stargazers: 42},
		},
		readme: gh.README{
			Path: "README.md", SHA: "sha-1", HTMLURL: "https://github.com/acme/awesome/blob/main/README.md",
			Content: []byte("## Tools\n\n- [Alpha](https://github.com/Example/Alpha) - Useful tool.\n- [Site](https://example.com) - External.\n"),
		},
	}
	service := NewService(sqliteStore, fake)
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
	if err != nil || run.Status != "succeeded" || run.GitHubCount != 1 || run.ExternalCount != 1 {
		t.Fatalf("SyncSource() = %+v, %v", run, err)
	}
	ready, err := sqliteStore.GetAwesomeSource(ctx, created.ID)
	if err != nil || ready.Status != model.AwesomeSourceReady || ready.GitHubRepoCount != 1 {
		t.Fatalf("ready source = %+v, %v", ready, err)
	}
	if _, err := service.PublishSource(ctx, created.ID); err != nil {
		t.Fatalf("PublishSource() error = %v", err)
	}
	snapshot, err := service.PublishedEntries(ctx, created.ID)
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].FullName != "Example/Alpha" {
		t.Fatalf("published snapshot = %+v, %v", snapshot, err)
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
	if err != nil || len(runs) != 2 || runs[0].Status != "failed" || runs[1].Status != "succeeded" {
		t.Fatalf("sync runs = %+v, %v", runs, err)
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

type fakeGitHubClient struct {
	repos  map[string]gh.Repository
	readme gh.README
}

func (f *fakeGitHubClient) GetRepository(_ context.Context, fullName string) (gh.Repository, error) {
	if repo, ok := f.repos[fullName]; ok {
		return repo, nil
	}
	return gh.Repository{}, &gh.APIError{StatusCode: 404, Path: "/repos/" + fullName, Message: "Not Found"}
}

func (f *fakeGitHubClient) GetREADME(_ context.Context, _ string) (gh.README, error) {
	return f.readme, nil
}

func sourceRepository() gh.Repository {
	return gh.Repository{ID: 1, FullName: "acme/awesome", Name: "awesome", DefaultBranch: "main", Owner: gh.Owner{Login: "acme"}}
}
