// RSS 采集与发布分离：只有显式开启授权开关才发起 Linux.do 请求。
package linuxdo

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

const feedURL = "https://linux.do/tag/2234-tag/2234.rss"

// Collector 使用独立、无 Cookie 的客户端，禁止跟随重定向到认证页或其它主机。
type Collector struct {
	client *http.Client
	url    string
}

func NewCollector() *Collector {
	return &Collector{url: feedURL, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Fetch 返回 nil observations 表示 304，不代表有效空目录；调用方必须继续处理持久化待核验队列。
func (c *Collector) Fetch(ctx context.Context, state model.LinuxDoFeedState) ([]model.LinuxDoObservation, model.LinuxDoFeedState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, state, err
	}
	req.Header.Set("User-Agent", "Starcat-Discovery/2.0 (+https://starcat.ink)")
	req.Header.Set("Accept", "application/rss+xml, application/xml")
	if state.ETag != "" {
		req.Header.Set("If-None-Match", state.ETag)
	}
	if state.LastModified != "" {
		req.Header.Set("If-Modified-Since", state.LastModified)
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, state, fmt.Errorf("Linux.do request failed")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		if state.ETag == "" && state.LastModified == "" {
			return nil, state, fmt.Errorf("unexpected Linux.do 304")
		}
		state.CheckedAt = time.Now().UTC()
		return nil, state, nil
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
	return observations, model.LinuxDoFeedState{ETag: response.Header.Get("ETag"), LastModified: response.Header.Get("Last-Modified"), CheckedAt: time.Now().UTC()}, nil
}
