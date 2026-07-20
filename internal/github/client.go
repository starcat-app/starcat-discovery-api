// Package github 封装 Discovery 同步所需的 GitHub REST API。
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/tokenpool"
)

const (
	defaultBaseURL           = "https://api.github.com"
	maxTokenAvailabilityWait = 90 * time.Second
)

// RepositorySearchOptions 描述一次候选仓库搜索。
//
// Discovery 需要同时拉取头部、近期活跃和新兴项目，因此 sort 不能再由 client 固定为 stars。
// 调用方仍只请求单页；候选规模由 ingest 按不同策略分配，避免无界分页拖垮 GitHub 配额。
type RepositorySearchOptions struct {
	Query   string
	Sort    string
	Order   string
	PerPage int
}

// Client 是 GitHub REST API 客户端。
type Client struct {
	baseURL        string
	httpClient     *http.Client
	tokens         *tokenpool.Pool
	rateLimitFloor int
	maxTokenWait   time.Duration
}

// NewClient 创建 GitHub client。
func NewClient(tokens *tokenpool.Pool, rateLimitFloor int) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		tokens:         tokens,
		rateLimitFloor: rateLimitFloor,
		maxTokenWait:   maxTokenAvailabilityWait,
	}
}

// WithBaseURL 覆盖 API base URL，主要用于测试。
func (c *Client) WithBaseURL(baseURL string) *Client {
	c.baseURL = strings.TrimRight(baseURL, "/")
	return c
}

// SearchRepositories 调 GitHub Search API 获取候选仓库。
func (c *Client) SearchRepositories(ctx context.Context, options RepositorySearchOptions) ([]Repository, error) {
	if options.PerPage <= 0 {
		options.PerPage = 30
	}
	if options.PerPage > 100 {
		options.PerPage = 100
	}
	if strings.TrimSpace(options.Sort) == "" {
		options.Sort = "stars"
	}
	if strings.TrimSpace(options.Order) == "" {
		options.Order = "desc"
	}
	values := url.Values{}
	values.Set("q", options.Query)
	values.Set("sort", options.Sort)
	values.Set("order", options.Order)
	values.Set("per_page", strconv.Itoa(options.PerPage))

	var response searchResponse
	if err := c.get(ctx, "/search/repositories?"+values.Encode(), &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

// GetRepository 拉取单仓库完整信息。
func (c *Client) GetRepository(ctx context.Context, fullName string) (Repository, error) {
	var repo Repository
	if err := c.get(ctx, "/repos/"+fullName, &repo); err != nil {
		return Repository{}, err
	}
	return repo, nil
}

// ListReleases 拉取仓库最近 release。
func (c *Client) ListReleases(ctx context.Context, fullName string, perPage int) ([]Release, error) {
	if perPage <= 0 {
		perPage = 5
	}
	values := url.Values{}
	values.Set("per_page", strconv.Itoa(perPage))
	var releases []Release
	if err := c.get(ctx, "/repos/"+fullName+"/releases?"+values.Encode(), &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (c *Client) get(ctx context.Context, path string, out interface{}) error {
	maxAttempts := c.tokens.Count()
	if maxAttempts == 0 {
		return fmt.Errorf("no GitHub tokens configured")
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; {
		token := c.tokens.PickBest()
		if token == nil {
			if err := c.waitForAvailableToken(ctx); err != nil {
				return err
			}
			continue
		}
		attempt++

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "starcat-discovery-api")
		req.Header.Set("Authorization", "Bearer "+token.Value)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		c.tokens.UpdateFromResponse(token, resp)
		remainingBelowFloor := c.disableBelowFloor(token, resp)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(out)
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()

		switch resp.StatusCode {
		case http.StatusUnauthorized:
			lastErr = fmt.Errorf("github api %s unauthorized with current token", path)
			continue
		case http.StatusForbidden, http.StatusTooManyRequests:
			c.disableRateLimited(token, resp)
			lastErr = fmt.Errorf("github api %s rate limited: %s", path, strings.TrimSpace(string(body)))
			continue
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			lastErr = fmt.Errorf("github api %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
			continue
		default:
			if remainingBelowFloor {
				lastErr = fmt.Errorf("github api %s returned %d with token below floor: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
				continue
			}
			return fmt.Errorf("github api %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
		}
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("github api %s failed: no token attempts completed", path)
}

// waitForAvailableToken 等待 Search API 的分钟级额度恢复。
//
// 候选发现会连续执行多路 Search；当所有 token 都刚好低于保护阈值时，直接失败会让整轮
// 同步丢弃。等待严格限制在 90 秒内并服从请求 context，避免 Core API 的小时级 reset
// 把管理请求和定时任务长期挂住。
func (c *Client) waitForAvailableToken(ctx context.Context) error {
	earliest := c.tokens.EarliestAvailable()
	if earliest.IsZero() {
		return fmt.Errorf("no GitHub token available")
	}
	wait := time.Until(earliest)
	if wait <= 0 {
		return nil
	}
	if wait > c.maxTokenWait {
		return fmt.Errorf("no GitHub token available until %s", earliest.Format(time.RFC3339))
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) disableBelowFloor(token *tokenpool.TokenState, resp *http.Response) bool {
	if c.rateLimitFloor <= 0 {
		return false
	}
	remaining := resp.Header.Get("X-RateLimit-Remaining")
	if remaining == "" {
		return false
	}
	value, err := strconv.Atoi(remaining)
	if err != nil || value <= 0 || value >= c.rateLimitFloor {
		return false
	}
	c.tokens.DisableUntil(token, resetAt(resp), fmt.Sprintf("remaining below floor: %d", value))
	return true
}

func (c *Client) disableRateLimited(token *tokenpool.TokenState, resp *http.Response) {
	until := resetAt(resp)
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
			retryUntil := time.Now().Add(time.Duration(seconds) * time.Second)
			if retryUntil.After(until) {
				until = retryUntil
			}
		}
	}
	c.tokens.DisableUntil(token, until, fmt.Sprintf("rate limited status %d", resp.StatusCode))
}

func resetAt(resp *http.Response) time.Time {
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		if ts, err := strconv.ParseInt(reset, 10, 64); err == nil {
			return time.Unix(ts, 0)
		}
	}
	return time.Time{}
}

type searchResponse struct {
	Items []Repository `json:"items"`
}

// Repository 是 GitHub repo API 的必要字段子集。
type Repository struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	FullName      string   `json:"full_name"`
	Description   string   `json:"description"`
	Homepage      string   `json:"homepage"`
	Language      string   `json:"language"`
	Stargazers    int      `json:"stargazers_count"`
	Forks         int      `json:"forks_count"`
	Watchers      int      `json:"watchers_count"`
	Subscribers   int      `json:"subscribers_count"`
	OpenIssues    int      `json:"open_issues_count"`
	DefaultBranch string   `json:"default_branch"`
	Topics        []string `json:"topics"`
	Archived      bool     `json:"archived"`
	Fork          bool     `json:"fork"`
	PushedAt      string   `json:"pushed_at"`
	UpdatedAt     string   `json:"updated_at"`
	CreatedAt     string   `json:"created_at"`
	Owner         Owner    `json:"owner"`
	License       *License `json:"license"`
}

// Owner 是 GitHub owner 字段子集。
type Owner struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// License 是 GitHub license 字段子集。
type License struct {
	SPDXID string `json:"spdx_id"`
}

// Release 是 GitHub release 字段子集。
type Release struct {
	TagName     string  `json:"tag_name"`
	Name        string  `json:"name"`
	HTMLURL     string  `json:"html_url"`
	Draft       bool    `json:"draft"`
	Prerelease  bool    `json:"prerelease"`
	PublishedAt string  `json:"published_at"`
	Assets      []Asset `json:"assets"`
}

// Asset 是 GitHub release asset 字段子集。
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	DownloadCount      int    `json:"download_count"`
}
