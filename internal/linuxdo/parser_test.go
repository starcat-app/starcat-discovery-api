// 所有采集用例使用合成 RSS，不对 Linux.do 发起请求。
package linuxdo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
		`<p>源码 <a href="https://github.com/A/B">A</a><a href="https://github.com/C/D">B</a></p>`,
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

func TestParseFeedRejectsIncompleteOrWrongSource(t *testing.T) {
	valid := syntheticFeed(`<a href="https://github.com/a/b">repo</a>`)
	for _, body := range []string{"<html>challenge</html>", valid[:len(valid)-6], strings.ReplaceAll(valid, "2234", "9999"), strings.ReplaceAll(valid, "topic/42", "topic/42/3"), strings.Repeat("x", maxFeedBytes+1)} {
		if _, err := ParseFeed(strings.NewReader(body)); err == nil {
			t.Fatal("invalid feed accepted")
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
