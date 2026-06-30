// Package github 封装 Discovery 同步所需的 GitHub REST API。
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dong4j/starcat-discovery-api/internal/tokenpool"
)

const defaultBaseURL = "https://api.github.com"

// Client 是 GitHub REST API 客户端。
type Client struct {
	baseURL        string
	httpClient     *http.Client
	tokens         *tokenpool.Pool
	rateLimitFloor int
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
	}
}

// WithBaseURL 覆盖 API base URL，主要用于测试。
func (c *Client) WithBaseURL(baseURL string) *Client {
	c.baseURL = strings.TrimRight(baseURL, "/")
	return c
}

// SearchRepositories 调 GitHub Search API 获取候选仓库。
func (c *Client) SearchRepositories(ctx context.Context, query string, perPage int) ([]Repository, error) {
	if perPage <= 0 {
		perPage = 30
	}
	if perPage > 50 {
		perPage = 50
	}
	values := url.Values{}
	values.Set("q", query)
	values.Set("sort", "stars")
	values.Set("order", "desc")
	values.Set("per_page", strconv.Itoa(perPage))

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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "starcat-discovery-api")
	if token, ok := c.tokens.Next(); ok {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		value, _ := strconv.Atoi(remaining)
		if c.rateLimitFloor > 0 && value > 0 && value < c.rateLimitFloor {
			return fmt.Errorf("github rate limit remaining below floor: %d", value)
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return fmt.Errorf("github api %s returned %d: %v", path, resp.StatusCode, body["message"])
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
