// 发布、重试、审核和撤回在临时 SQLite 中验证，不接触生产数据库。
package linuxdo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gh "github.com/starcat-app/starcat-discovery-api/internal/github"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"github.com/starcat-app/starcat-discovery-api/internal/store"
)

type fakeGitHub struct {
	calls   int
	fail    bool
	private bool
	license string
}

func (f *fakeGitHub) GetRepository(context.Context, string) (gh.Repository, error) {
	f.calls++
	if f.fail {
		return gh.Repository{}, errors.New("unavailable")
	}
	return gh.Repository{ID: 123, Name: "app", FullName: "owner/app", Owner: gh.Owner{Login: "owner"}, Private: f.private, License: &gh.License{SPDXID: f.license}}, nil
}

func TestSyncPersistsQueueAnd304RetriesWithoutLosingPublication(t *testing.T) {
	ctx := context.Background()
	db, err := store.NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"feed"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"feed"`)
		fmt.Fprint(w, syntheticFeed(`<p>项目地址 <a href="https://github.com/owner/app">GitHub</a></p>`))
	}))
	defer upstream.Close()
	ghClient := &fakeGitHub{fail: true, license: "MIT"}
	service := NewService(db, ghClient, true, nil)
	service.collector.url = upstream.URL
	if _, err := service.Sync(ctx, "test"); err == nil {
		t.Fatal("expected verification failure")
	}
	queued, _ := service.Candidates(ctx, 0)
	if len(queued) != 1 || queued[0].State != "pending" {
		t.Fatalf("queue lost: %+v", queued)
	}
	ghClient.fail = false
	if _, err := service.Sync(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Snapshot(ctx)
	if err != nil || len(snapshot.Repositories) != 1 || len(snapshot.Origins) != 1 {
		t.Fatalf("publication missing: %+v %v", snapshot, err)
	}
	regular, err := db.ListAllRepos(ctx)
	if err != nil || len(regular) != 0 {
		t.Fatal("Linux.do polluted Discovery")
	}
	if _, err := db.PruneReposNotIn(ctx, []int64{999}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.Snapshot(ctx)
	if err != nil || len(snapshot.Repositories) != 1 {
		t.Fatal("ordinary sync pruned Linux.do")
	}
	queued, _ = service.Candidates(ctx, 0)
	if err := service.Review(ctx, queued[0].ID, ReviewRequest{ContentHash: "stale", Decision: "withdraw"}); !errors.Is(err, store.ErrLinuxDoConflict) {
		t.Fatal("accepted stale review")
	}
	if err := service.Review(ctx, queued[0].ID, ReviewRequest{ContentHash: queued[0].ContentHash, Decision: "withdraw"}); err != nil {
		t.Fatal(err)
	}
	withdrawn, _ := service.Snapshot(ctx)
	if len(withdrawn.Origins) != 0 || withdrawn.Revision <= snapshot.Revision || !withdrawn.Complete {
		t.Fatal("invalid empty retraction snapshot")
	}
	if _, err := service.Sync(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	withdrawn, _ = service.Snapshot(ctx)
	if len(withdrawn.Origins) != 0 {
		t.Fatal("sync reversed withdrawal")
	}
}

func TestDefaultDisabledAndUnknownLicenseReview(t *testing.T) {
	ctx := context.Background()
	db, err := store.NewSQLiteStore(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	github := &fakeGitHub{license: "NOASSERTION"}
	service := NewService(db, github, false, nil)
	if _, err := service.Sync(ctx, "manual"); !errors.Is(err, ErrDisabled) || github.calls != 0 {
		t.Fatal("disabled collector made request")
	}
	if knownOpenLicense("NOASSERTION") || knownOpenLicense("BUSL-1.1") || knownOpenLicense("") {
		t.Fatal("unknown license auto-approved")
	}
}

// 失败保留最后发布结果；只有明确 private 才撤回该仓库在所有帖子中的公开关系。
func TestVerifiedPrivateRepositoryRetractsAllTopicsButTransientFailureDoesNot(t *testing.T) {
	ctx := context.Background()
	db, err := store.NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	body := `<p>项目地址 <a href="https://github.com/owner/app">GitHub</a></p>`
	for _, id := range []string{"42", "43"} {
		feed := strings.ReplaceAll(syntheticFeed(body), "topic/42", "topic/"+id)
		observations, err := ParseFeed(strings.NewReader(feed))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.ObserveLinuxDo(ctx, observations, model.LinuxDoFeedState{CheckedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	github := &fakeGitHub{license: "MIT"}
	service := NewService(db, github, true, nil)
	candidates, err := service.Candidates(ctx, 0)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("candidate setup: %+v %v", candidates, err)
	}
	for _, candidate := range candidates {
		if err := service.Review(ctx, candidate.ID, ReviewRequest{ContentHash: candidate.ContentHash, Decision: "publish"}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := service.Snapshot(ctx)
	if err != nil || len(before.Topics[123]) != 2 {
		t.Fatalf("publication setup: %+v %v", before, err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, syntheticFeed(body+"<p>Updated release</p>"))
	}))
	defer upstream.Close()
	service.collector.url = upstream.URL
	github.fail = true
	if _, err := service.Sync(ctx, "test"); err == nil {
		t.Fatal("expected temporary verification failure")
	}
	preserved, err := service.Snapshot(ctx)
	if err != nil || len(preserved.Topics[123]) != 2 || preserved.Revision != before.Revision {
		t.Fatalf("temporary failure withdrew publication: %+v %v", preserved, err)
	}
	github.fail, github.private = false, true
	if _, err := service.Sync(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	retracted, err := service.Snapshot(ctx)
	if err != nil || !retracted.Complete || len(retracted.Repositories) != 0 || len(retracted.Origins) != 0 || len(retracted.Topics) != 0 || retracted.Revision <= preserved.Revision {
		t.Fatalf("incomplete private retraction: %+v %v", retracted, err)
	}
	candidates, err = service.Candidates(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.State != "withdrawn" {
			t.Fatalf("private candidate can republish: %+v", candidate)
		}
	}
}
