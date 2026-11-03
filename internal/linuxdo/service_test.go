// 自动发布、跳过、重试和撤回在临时 SQLite 中验证，不接触生产数据库。
package linuxdo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	var license *gh.License
	if f.license != "" {
		license = &gh.License{SPDXID: f.license}
	}
	return gh.Repository{ID: 123, Name: "app", FullName: "owner/app", Owner: gh.Owner{Login: "owner"}, Private: f.private, License: license}, nil
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
	stale := queued[0]
	stale.ContentHash = "stale"
	if err := db.ApplyLinuxDoDecision(ctx, stale, "skipped", "outdated classification", nil); !errors.Is(err, store.ErrLinuxDoConflict) {
		t.Fatal("accepted stale automatic decision")
	}
}

func TestDefaultDisabledDoesNotMakeRequests(t *testing.T) {
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
}

// 窗口只决定本轮能观察到什么，不是历史目录的替换清单；重启后仍保留唯一仓库与多个回链。
func TestSyncDeduplicatesAndPersistsProjectsOutsideRSSWindow(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "retained.db")
	db, err := store.NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	type response struct {
		status int
		body   string
	}
	body := `<p>项目地址 <a href="https://github.com/Owner/App">app</a> <a href="https://github.com/owner/app/releases/latest">下载</a></p>`
	feed := syntheticFeed(body)
	var next atomic.Value
	next.Store(response{http.StatusOK, feed})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := next.Load().(response)
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"feed"`)
		if value.status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "21600")
		}
		w.WriteHeader(value.status)
		fmt.Fprint(w, value.body)
	}))
	defer upstream.Close()
	github := &fakeGitHub{license: "MIT"}
	service := NewService(db, github, true, nil)
	service.collector.url = upstream.URL
	for range 3 {
		if _, err := service.Sync(ctx, "test"); err != nil {
			t.Fatal(err)
		}
	}
	before, err := service.Snapshot(ctx)
	if err != nil || len(before.Repositories) != 1 || len(before.Topics[123]) != 1 || github.calls != 1 {
		t.Fatalf("repeated input duplicated publication: %+v calls=%d err=%v", before, github.calls, err)
	}
	// 同一个项目被第二篇帖子推广，应累积回链而不是重复创建仓库或移除第一篇回链。
	next.Store(response{http.StatusOK, strings.ReplaceAll(feed, "topic/42", "topic/43")})
	if _, err := service.Sync(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	before, err = service.Snapshot(ctx)
	if err != nil || len(before.Repositories) != 1 || len(before.Origins) != 1 || before.Origins[0].TopicCount != 2 || len(before.Topics[123]) != 2 {
		t.Fatalf("cross-topic repository deduplication failed: %+v %v", before, err)
	}
	next.Store(response{http.StatusOK, strings.ReplaceAll(syntheticFeed(`<p>本帖没有直接仓库链接。</p>`), "topic/42", "topic/44")})
	if _, err := service.Sync(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	// 完整但不含旧项目的窗口、304、空窗口、坏 XML 和 HTTP 失败都不能删除已发布关系。
	for _, value := range []response{
		{http.StatusNotModified, ""},
		{http.StatusOK, `<rss><channel><link>https://linux.do/tag/2234-tag/2234</link></channel></rss>`},
		{http.StatusOK, "<rss>"},
		{http.StatusForbidden, "challenge"},
		{http.StatusInternalServerError, "unavailable"},
		{http.StatusTooManyRequests, "rate limited"},
	} {
		after, err := service.Snapshot(ctx)
		if err != nil || after.Revision != before.Revision || len(after.Repositories) != 1 || len(after.Topics[123]) != 2 {
			t.Fatalf("rolling window withdrew publication: %+v %v", after, err)
		}
		next.Store(value)
		_, syncErr := service.Sync(ctx, "test")
		if value.status == http.StatusNotModified && syncErr != nil {
			t.Fatal(syncErr)
		}
		if value.status != http.StatusNotModified && syncErr == nil {
			t.Fatalf("accepted failed input: %+v", value)
		}
	}
	// 关闭并重开同一 SQLite 文件，证明数据不是只存活在服务的内存缓存中。
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := db.LinuxDoSnapshot(ctx)
	if err != nil || !after.Complete || after.Revision != before.Revision || len(after.Repositories) != 1 || len(after.Origins) != 1 || len(after.Topics[123]) != 2 {
		t.Fatalf("restart lost retained catalog: %+v %v", after, err)
	}
	candidates, err := db.LinuxDoCandidates(ctx, 0)
	if err != nil || len(candidates) != 2 || candidates[0].State != "published" || candidates[1].State != "published" {
		t.Fatalf("duplicate or lost candidates after restart: %+v %v", candidates, err)
	}
}

// 未知许可证是元数据状态，不得把正常项目挂起等待无人处理的人工审批。
func TestSyncAutomaticallyPublishesUnknownLicense(t *testing.T) {
	for _, test := range []struct {
		name, license string
		private       bool
	}{
		{"unknown", "NOASSERTION", false},
		{"missing", "", false},
		{"custom", "BUSL-1.1", false},
		{"private", "MIT", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			db, err := store.NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "automatic.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/rss+xml")
				fmt.Fprint(w, syntheticFeed(`<p>GitHub：<a href="https://github.com/owner/app">owner/app</a></p>`))
			}))
			defer upstream.Close()
			service := NewService(db, &fakeGitHub{license: test.license, private: test.private}, true, nil)
			service.collector.url = upstream.URL
			if _, err := service.Sync(ctx, "test"); err != nil {
				t.Fatal(err)
			}
			snapshot, err := service.Snapshot(ctx)
			count, state := 1, "published"
			if test.private {
				count, state = 0, "skipped"
			}
			if err != nil || len(snapshot.Repositories) != count {
				t.Fatalf("unexpected publication: %+v %v", snapshot, err)
			}
			candidates, err := service.Candidates(ctx, 0)
			if err != nil || len(candidates) != 1 || candidates[0].State != state || (!test.private && candidates[0].License != test.license) {
				t.Fatalf("unexpected automatic decision: %+v %v", candidates, err)
			}
		})
	}
}

// RSS 没变化或旧帖子已滑出窗口，都不应让升级前的候选继续等待人工处理。
func TestSyncReclassifiesLegacyEvidenceAfter304(t *testing.T) {
	ctx := context.Background()
	db, err := store.NewSQLiteStore(ctx, filepath.Join(t.TempDir(), "legacy-evidence.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	observation := model.LinuxDoObservation{
		Topic: model.LinuxDoTopic{ID: 42, Title: "Old Tool", URL: "https://linux.do/t/topic/42", PublishedAt: "2026-10-14T00:00:00Z", ContentHash: "legacy"},
		Candidates: []model.LinuxDoCandidate{
			{TopicID: 42, FullName: "owner/app", ContentHash: "legacy", Context: "GitHub：owner/app"},
			{TopicID: 42, FullName: "vendor/library", ContentHash: "legacy", Context: "依赖 vendor/library"},
		},
	}
	if err := db.ObserveLinuxDo(ctx, []model.LinuxDoObservation{observation}, model.LinuxDoFeedState{ETag: `"feed"`, CheckedAt: time.Now().UTC(), RuleVersion: model.LinuxDoRuleVersion}); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `"feed"` {
			t.Error("expected current RSS conditional request")
		}
		w.WriteHeader(304)
	}))
	defer upstream.Close()
	github := &fakeGitHub{license: "NOASSERTION"}
	service := NewService(db, github, true, nil)
	service.collector.url = upstream.URL
	for i := 0; i < 2; i++ {
		if _, err := service.Sync(ctx, "test"); err != nil {
			t.Fatal(err)
		}
	}
	candidates, err := service.Candidates(ctx, 0)
	if err != nil || len(candidates) != 2 || candidates[0].State != "published" || candidates[1].State != "skipped" || candidates[1].Reason == "" || github.calls != 1 {
		t.Fatalf("legacy queue not automatically resolved: %+v calls=%d err=%v", candidates, github.calls, err)
	}
	for _, candidate := range candidates {
		if candidate.RuleVersion != model.LinuxDoRuleVersion {
			t.Fatalf("old classification left behind: %+v", candidate)
		}
	}
	// 帖子内容变化时，之前的自动跳过决定必须能够自行恢复，不需要任何审批接口。
	observation.Topic.ContentHash = "edited"
	observation.Candidates = []model.LinuxDoCandidate{{TopicID: 42, FullName: "vendor/library", ContentHash: "edited", Context: "项目地址 vendor/library", ExplicitPromotion: true, RuleVersion: model.LinuxDoRuleVersion}}
	if err := db.ObserveLinuxDo(ctx, []model.LinuxDoObservation{observation}, model.LinuxDoFeedState{ETag: `"feed"`, CheckedAt: time.Now().UTC(), RuleVersion: model.LinuxDoRuleVersion}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	candidates, _ = service.Candidates(ctx, 0)
	if candidates[1].State != "published" || github.calls != 2 {
		t.Fatalf("edited project did not retry: %+v calls=%d", candidates, github.calls)
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
		repo, err := service.verify(ctx, candidate.FullName)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.ApplyLinuxDoDecision(ctx, candidate, "published", "automatic project identification", &repo); err != nil {
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
		if candidate.State != "skipped" {
			t.Fatalf("private candidate can republish: %+v", candidate)
		}
	}
}
