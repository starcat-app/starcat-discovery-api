// 所有采集用例使用合成 RSS，不对 Linux.do 发起请求。
package linuxdo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func syntheticFeed(body string) string {
	return `<rss version="2.0"><channel><link>https://linux.do/tag/2234-tag/2234</link><item><title>My project</title><link>https://linux.do/t/topic/42</link><pubDate>Wed, 14 Oct 2026 00:00:00 +0000</pubDate><description><![CDATA[` + body + `]]></description></item></channel></rss>`
}

func TestCanonicalRepository(t *testing.T) {
	for _, raw := range []string{"https://github.com/Owner/Repo.git?x=1#readme", "https://github.com/Owner/Repo/releases/latest", "https://github.com/Owner/Repo/tree/main"} {
		if got, ok := CanonicalRepository(raw); !ok || got != "owner/repo" {
			t.Errorf("%s: %s %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"https://github.com.evil/a/b", "https://github.com@evil/a/b", "https://github.com/topics/swift", "https://github.com/owner", "https://github.com/a/../b", "https://github.com/a/b/unknown", "javascript:alert(1)", "https://github.com:443/a/b"} {
		if _, ok := CanonicalRepository(raw); ok {
			t.Errorf("accepted %s", raw)
		}
	}
}

func TestParseFeedPromotionsAndNoise(t *testing.T) {
	observations, err := ParseFeed(strings.NewReader(syntheticFeed(`<p>项目地址 <a href="https://github.com/Me/App">GitHub</a> <a href="https://github.com/Me/App/releases">download</a></p><blockquote><a href="https://github.com/Other/Repo">quoted</a></blockquote><pre><a href="https://github.com/Code/Repo">code</a></pre><aside class="onebox"><a href="https://github.com/Me/App">box</a></aside>`)))
	if err != nil {
		t.Fatal(err)
	}
	c := observations[0].Candidates
	if len(c) != 1 || c[0].FullName != "me/app" || !c[0].ExplicitPromotion {
		t.Fatalf("unexpected candidates: %+v", c)
	}
	for _, body := range []string{
		`<p>基于 <a href="https://github.com/Other/Repo">项目地址</a></p>`,
		`<p>源码 <a href="https://github.com/A/B">A</a></p><p>依赖 <a href="https://github.com/A/B">A</a></p>`,
	} {
		obs, err := ParseFeed(strings.NewReader(syntheticFeed(body)))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range obs[0].Candidates {
			if c.ExplicitPromotion {
				t.Fatalf("ambiguous candidate auto-approved: %+v", c)
			}
		}
	}
}

// 覆盖真实帖子常见表达；多个仓库按各自角色判断，不让依赖借用主项目的标签。
func TestParseFeedAutomaticallyIdentifiesPromotedProjects(t *testing.T) {
	for _, test := range []struct {
		name, title, body string
		projects          map[string]bool
	}{
		{"github_label", "Codex Proxy", `<p>GitHub： <a href="https://github.com/zyycn/codex-proxy-rs">仓库</a></p>`, map[string]bool{"zyycn/codex-proxy-rs": true}},
		{"address_label", "MuMuAINovel", `<p>地址: <a href="https://github.com/ddys9621/MuMuAINovel/releases">下载</a></p>`, map[string]bool{"ddys9621/mumuainovel": true}},
		{"plain_url", "小工具", `<p>https://github.com/me/tool</p>`, map[string]bool{"me/tool": true}},
		{"plain_urls_with_separate_roles", "小工具", `<p>项目地址 https://github.com/me/tool，基于 https://github.com/vendor/library 实现。</p>`, map[string]bool{"me/tool": true, "vendor/library": false}},
		{"nested_link_label", "项目分享", `<p><strong>GitHub：</strong><span><a href="https://github.com/me/tool">tool</a></span></p><p>依赖 <a href="https://github.com/vendor/library">library</a></p>`, map[string]bool{"me/tool": true, "vendor/library": false}},
		{"single_link_without_keywords", "邮件管理工具", `<p><a href="https://github.com/yuetc/y-mail">A modern unified email manager</a></p>`, map[string]bool{"yuetc/y-mail": true}},
		{"single_supporting_tool_is_not_the_project", "我的 Agent 客户端", `<p>通过 <a href="https://github.com/vendor/adapter">adapter</a> 使用模型服务。</p>`, map[string]bool{"vendor/adapter": false}},
		{"project_usage_is_not_a_dependency", "Browser Helper 项目分享", `<p>通过 <a href="https://github.com/me/browser-helper">Browser Helper</a> 完成浏览器任务。</p>`, map[string]bool{"me/browser-helper": true}},
		{"preview_source", "我的工具", `<aside class="onebox allowlistedgeneric" data-onebox-src="https://github.com/me/tool"><h3><a href="https://github.com/me/tool">me/tool</a></h3><p>Dependencies <a href="https://github.com/vendor/library">library</a></p></aside>`, map[string]bool{"me/tool": true}},
		{"preview_heading", "我的工具", `<aside class="onebox"><header>GitHub</header><article><h3><a href="https://github.com/me/tool">me/tool</a></h3></article></aside>`, map[string]bool{"me/tool": true}},
		{"main_and_dependency_same_paragraph", "我的工具", `<p>项目地址 <a href="https://github.com/me/tool">tool</a>，基于 <a href="https://github.com/vendor/library">library</a> 实现。</p>`, map[string]bool{"me/tool": true, "vendor/library": false}},
		{"dependency_heading_before_preview", "我的工具", `<h2>依赖</h2><aside class="onebox" data-onebox-src="https://github.com/vendor/library"><h3><a href="https://github.com/vendor/library">library</a></h3></aside>`, map[string]bool{"vendor/library": false}},
		{"promoted_heading_before_link", "项目分享", `<h2>项目地址</h2><p><a href="https://github.com/me/tool">tool</a></p><h2>参考项目</h2><p><a href="https://github.com/other/reference">reference</a></p>`, map[string]bool{"me/tool": true, "other/reference": false}},
		{"title_selects_main", "HiRoute 本地 AI 网关", `<p><a href="https://github.com/higress-group/hiroute">HiRoute</a></p><p>我也是 Higress 维护者。<a href="https://github.com/higress-group/community">Community</a></p>`, map[string]bool{"higress-group/hiroute": true, "higress-group/community": false}},
		{"separate_frontend_and_backend", "项目分享", `<p>前端源码 <a href="https://github.com/me/frontend">frontend</a></p><p>后端源码 <a href="https://github.com/me/backend">backend</a></p>`, map[string]bool{"me/frontend": true, "me/backend": true}},
		{"shared_source_label", "项目分享", `<p>源码 <a href="https://github.com/me/frontend">frontend</a> <a href="https://github.com/me/backend">backend</a></p>`, map[string]bool{"me/frontend": true, "me/backend": true}},
		{"multiple_unattributed_links", "项目分享", `<p><a href="https://github.com/one/tool">tool</a></p><p><a href="https://github.com/two/other">other</a></p>`, map[string]bool{"one/tool": false, "two/other": false}},
		{"title_word_boundary", "Foobar 项目分享", `<p><a href="https://github.com/one/foobar">Foobar</a></p><p><a href="https://github.com/two/foo">Foo</a></p>`, map[string]bool{"one/foobar": true, "two/foo": false}},
		{"external_preview_does_not_promote_embedded_repository", "项目分享", `<aside class="onebox" data-onebox-src="https://example.com/article"><a href="https://github.com/vendor/library">library</a></aside>`, map[string]bool{}},
		{"preview_body_without_target_is_not_evidence", "项目分享", `<aside class="onebox"><p>README <a href="https://github.com/vendor/library">library</a></p></aside>`, map[string]bool{}},
		{"quotes_code_and_badges", "项目分享", `<blockquote><a href="https://github.com/one/tool">tool</a></blockquote><pre>https://github.com/two/code</pre><p><a href="https://github.com/three/badge"><img src="badge.svg"></a></p>`, map[string]bool{}},
		{"duplicate_role_conflict", "项目分享", `<p>源码 <a href="https://github.com/me/tool">tool</a></p><p>依赖 <a href="https://github.com/me/tool">tool</a></p>`, map[string]bool{"me/tool": false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			feed := strings.Replace(syntheticFeed(test.body), "My project", test.title, 1)
			observations, err := ParseFeed(strings.NewReader(feed))
			if err != nil {
				t.Fatal(err)
			}
			candidates := observations[0].Candidates
			if len(candidates) != len(test.projects) {
				t.Fatalf("unexpected candidates: %+v", candidates)
			}
			for _, candidate := range candidates {
				promoted, exists := test.projects[candidate.FullName]
				if !exists || candidate.ExplicitPromotion != promoted {
					t.Errorf("unexpected project classification: %+v", candidate)
				}
			}
		})
	}
}

func TestParseFeedRejectsIncompleteOrWrongSource(t *testing.T) {
	valid := syntheticFeed(`<a href="https://github.com/a/b">repo</a>`)
	for _, body := range []string{"<html>challenge</html>", valid[:len(valid)-6], strings.ReplaceAll(valid, "2234", "9999"), strings.ReplaceAll(valid, "topic/42", "topic/42/3"), strings.Repeat("x", maxFeedBytes+1)} {
		if _, err := ParseFeed(strings.NewReader(body)); err == nil {
			t.Fatal("invalid feed accepted")
		}
	}
}

// Discourse 的 Atom 元数据与 RSS link 同名；插入顺序不能改变频道来源校验。
func TestParseFeedPreservesChannelLinkWithAtomMetadata(t *testing.T) {
	const atomLinks = `<atom:link href="https://linux.do/tag/2234-tag/2234.rss" rel="self" type="application/rss+xml"/><atom:link href="https://linux.do/tag/2234-tag/2234.md" rel="alternate" type="text/markdown"/>`
	for _, test := range []struct{ name, marker string }{
		{"before_rss_link", "<link>"},
		{"after_rss_link", "<item>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			feed := strings.Replace(syntheticFeed(`<p>源码 <a href="https://github.com/Me/App">GitHub</a></p>`), `<rss version="2.0">`, `<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">`, 1)
			feed = strings.Replace(feed, test.marker, atomLinks+test.marker, 1)
			observations, err := ParseFeed(strings.NewReader(feed))
			if err != nil {
				t.Fatal(err)
			}
			if len(observations) != 1 || len(observations[0].Candidates) != 1 || observations[0].Candidates[0].FullName != "me/app" {
				t.Fatalf("unexpected observations: %+v", observations)
			}
		})
	}
}

// Atom / 其它命名空间不能冒充 RSS 来源，也不能接受多个互相覆盖的频道 link。
func TestParseFeedRequiresSingleRSSChannelLink(t *testing.T) {
	const channelLink = `<link>https://linux.do/tag/2234-tag/2234</link>`
	feed := strings.Replace(syntheticFeed(`<a href="https://github.com/a/b">repo</a>`), `<rss version="2.0">`, `<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">`, 1)
	for _, replacement := range []string{
		`<atom:link>https://linux.do/tag/2234-tag/2234</atom:link>`,
		channelLink + channelLink,
	} {
		if _, err := ParseFeed(strings.NewReader(strings.Replace(feed, channelLink, replacement, 1))); err == nil {
			t.Fatal("ambiguous or namespaced channel link accepted")
		}
	}
}

func TestCollectorConditionalRequestAndChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected credentials")
		}
		if r.Header.Get("If-None-Match") == `"one"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"one"`)
		fmt.Fprint(w, syntheticFeed(`<p>源码 <a href="https://github.com/a/b">repo</a></p>`))
	}))
	defer server.Close()
	c := NewCollector()
	c.url = server.URL
	obs, state, err := c.Fetch(context.Background(), model.LinuxDoFeedState{})
	if err != nil || len(obs) != 1 {
		t.Fatalf("%v %+v", err, obs)
	}
	obs, _, err = c.Fetch(context.Background(), state)
	if err != nil || obs != nil {
		t.Fatalf("304 not preserved: %v", err)
	}
}

// 同一份 RSS 的 ETag 不能遮住识别规则升级，否则旧跳过项永远看不到新的解析结果。
func TestCollectorReparsesWhenRulesChange(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("old rule version sent conditional validators")
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("ETag", `"unchanged"`)
		fmt.Fprint(w, syntheticFeed(`<p>GitHub： <a href="https://github.com/me/tool">tool</a></p>`))
	}))
	defer upstream.Close()
	collector := NewCollector()
	collector.url = upstream.URL
	observations, state, err := collector.Fetch(context.Background(), model.LinuxDoFeedState{ETag: `"unchanged"`, LastModified: "Wed, 14 Oct 2026 00:00:00 GMT", RuleVersion: model.LinuxDoRuleVersion - 1})
	if err != nil || len(observations) != 1 || !observations[0].Candidates[0].ExplicitPromotion || state.RuleVersion != model.LinuxDoRuleVersion {
		t.Fatalf("rule upgrade did not reparse RSS: %+v %+v %v", observations, state, err)
	}
}

// 站方的冷却可以长于 5 小时随机上限；不能靠手动同步或重抽间隔提前发请求。
func TestCollectorRespectsRetryAfter(t *testing.T) {
	for _, format := range []string{"seconds", "http_date"} {
		t.Run(format, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Second)
			deadline := now.Add(6 * time.Hour)
			header := "21600"
			if format == "http_date" {
				header = deadline.Format(http.TimeFormat)
			}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.Header().Set("Retry-After", header)
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.Header().Set("Content-Type", "application/rss+xml")
				w.Header().Set("ETag", `"new"`)
				fmt.Fprint(w, syntheticFeed(`<p>源码 <a href="https://github.com/me/app">app</a></p>`))
			}))
			defer upstream.Close()
			collector := NewCollector()
			collector.url = upstream.URL
			state := model.LinuxDoFeedState{ETag: `"old"`, CheckedAt: now, RuleVersion: model.LinuxDoRuleVersion}
			for range 3 {
				observations, gotState, err := collector.Fetch(context.Background(), state)
				if err == nil || observations != nil || gotState != state {
					t.Fatalf("rate limit advanced feed state: %+v %+v %v", observations, gotState, err)
				}
			}
			if calls.Load() != 1 || collector.notBefore.Before(deadline) {
				t.Fatalf("ignored Retry-After: calls=%d deadline=%s", calls.Load(), collector.notBefore)
			}
			// 只模拟冷却到期，不真的等待六小时或修改生产时钟。
			collector.notBefore = time.Now().Add(-time.Second)
			observations, gotState, err := collector.Fetch(context.Background(), state)
			if err != nil || len(observations) != 1 || gotState.ETag != `"new"` || calls.Load() != 2 {
				t.Fatalf("collector did not resume after cooldown: %+v %+v %v", observations, gotState, err)
			}
		})
	}
}

func TestRetryAfterDeadline(t *testing.T) {
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		header string
		want   time.Time
	}{
		{"600", now.Add(10 * time.Minute)},
		{" 0 ", now},
		{now.Add(6 * time.Hour).Format(http.TimeFormat), now.Add(6 * time.Hour)},
		{now.Add(-time.Hour).Format(http.TimeFormat), time.Time{}},
		{"", time.Time{}},
		{"-1", time.Time{}},
		{"1.5", time.Time{}},
		{"999999999999999999999999", time.Time{}},
		{"not a deadline", time.Time{}},
	} {
		if got := retryAfterDeadline(test.header, now); !got.Equal(test.want) {
			t.Errorf("%q: got %s, want %s", test.header, got, test.want)
		}
	}
}
