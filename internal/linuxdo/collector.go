// RSS 采集与发布分离：只有显式开启授权开关才发起 Linux.do 请求。
package linuxdo

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

const feedURL = "https://linux.do/tag/2234-tag/2234.rss"

// Collector 使用独立、无 Cookie 的客户端，禁止跟随重定向到认证页或其它主机。
// Fetch 由 Service.mu 串行调用；冷却同时约束后台与手动触发，不占用服务线程睡眠。
type Collector struct {
	client    *http.Client
	url       string
	notBefore time.Time
}

func NewCollector() *Collector {
	return &Collector{url: feedURL, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Fetch 返回 nil observations 表示 304，不代表有效空目录；调用方必须继续处理持久化待核验队列。
func (c *Collector) Fetch(ctx context.Context, state model.LinuxDoFeedState) ([]model.LinuxDoObservation, model.LinuxDoFeedState, error) {
	if time.Now().Before(c.notBefore) {
		return nil, state, fmt.Errorf("Linux.do rate limited until %s", c.notBefore.UTC().Format(time.RFC3339))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, state, err
	}
	req.Header.Set("User-Agent", "Starcat-Discovery/2.0 (+https://starcat.ink)")
	req.Header.Set("Accept", "application/rss+xml, application/xml")
	// 规则变化必须重新读取同一页 RSS；不能让旧 ETag 的 304 永久跳过新识别逻辑。
	conditional := state.RuleVersion == model.LinuxDoRuleVersion
	if conditional && state.ETag != "" {
		req.Header.Set("If-None-Match", state.ETag)
	}
	if conditional && state.LastModified != "" {
		req.Header.Set("If-Modified-Since", state.LastModified)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, state, fmt.Errorf("Linux.do request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		if !conditional || (state.ETag == "" && state.LastModified == "") {
			return nil, state, fmt.Errorf("unexpected Linux.do 304")
		}
		state.CheckedAt = time.Now().UTC()
		return nil, state, nil
	}
	if response.StatusCode == http.StatusTooManyRequests {
		// 随机等待不能覆盖站方要求的更长冷却；冷却期内连手动同步也不发送请求。
		c.notBefore = retryAfterDeadline(response.Header.Get("Retry-After"), time.Now())
	}
	if response.StatusCode != http.StatusOK {
		return nil, state, fmt.Errorf("Linux.do HTTP %d", response.StatusCode)
	}
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if contentType != "application/rss+xml" && contentType != "application/xml" && contentType != "text/xml" {
		return nil, state, fmt.Errorf("unexpected Linux.do content type")
	}
	observations, err := ParseFeed(response.Body)
	if err != nil {
		return nil, state, err
	}
	return observations, model.LinuxDoFeedState{ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified"), CheckedAt: time.Now().UTC(), RuleVersion: model.LinuxDoRuleVersion}, nil
}

// retryAfterDeadline 支持秒数和 HTTP 日期；无效或过期值不覆盖正常随机等待。
// 秒数按 uint32 读取，避免不可信超大值在转换 time.Duration 时溢出。
func retryAfterDeadline(value string, now time.Time) time.Time {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if deadline, err := http.ParseTime(value); err == nil && deadline.After(now) {
		return deadline
	}
	return time.Time{}
}
