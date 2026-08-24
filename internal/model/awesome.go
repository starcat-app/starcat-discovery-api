package model

import "time"

// AwesomeSourceStatus 是精选来源从草稿到公开发布的稳定状态机。
type AwesomeSourceStatus string

const (
	AwesomeSourceDraft     AwesomeSourceStatus = "draft"
	AwesomeSourceReady     AwesomeSourceStatus = "ready"
	AwesomeSourcePublished AwesomeSourceStatus = "published"
	AwesomeSourceArchived  AwesomeSourceStatus = "archived"
)

// AwesomeSource 保存运营字段、同步新鲜度和发布门禁所需事实。
type AwesomeSource struct {
	ID                 string              `json:"id"`
	RepoFullName       string              `json:"repo_full_name"`
	RepoURL            string              `json:"repo_url,omitempty"`
	DisplayName        string              `json:"display_name"`
	ImageURL           string              `json:"image_url,omitempty"`
	SummaryZH          string              `json:"summary_zh,omitempty"`
	SummaryEN          string              `json:"summary_en,omitempty"`
	Featured           bool                `json:"featured"`
	SortOrder          int                 `json:"sort_order"`
	Status             AwesomeSourceStatus `json:"status"`
	Revision           int                 `json:"revision"`
	DefaultBranch      string              `json:"default_branch,omitempty"`
	ReadmePath         string              `json:"readme_path,omitempty"`
	LastSuccessfulSHA  string              `json:"last_successful_sha,omitempty"`
	SourceStars        int                 `json:"source_stars"`
	GitHubRepoCount    int                 `json:"github_repo_count"`
	ExternalEntryCount int                 `json:"external_entry_count"`
	LastSyncedAt       *time.Time          `json:"last_synced_at,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

// AwesomeSourceCard is the public source-catalog contract consumed by Starcat.
type AwesomeSourceCard struct {
	ID                 string     `json:"id"`
	DisplayName        string     `json:"display_name"`
	RepoFullName       string     `json:"repo_full_name"`
	RepoURL            string     `json:"repo_url"`
	ImageURL           string     `json:"image_url,omitempty"`
	SummaryZH          string     `json:"summary_zh,omitempty"`
	SummaryEN          string     `json:"summary_en,omitempty"`
	Featured           bool       `json:"featured"`
	SortOrder          int        `json:"sort_order"`
	SourceStars        int        `json:"source_stars"`
	GitHubRepoCount    int        `json:"github_repo_count"`
	ExternalEntryCount int        `json:"external_entry_count"`
	LastSyncedAt       *time.Time `json:"last_synced_at,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// AwesomeEntry 是 README 中一条可审计的来源事实。
//
// 外部链接也持久化用于运营统计，但公共 entries API 只读取 GitHub Repo 条目。
type AwesomeEntry struct {
	SourceID         string   `json:"-"`
	TargetType       string   `json:"-"`
	TargetKey        string   `json:"-"`
	GhRepoID         *int64   `json:"gh_repo_id,omitempty"`
	Owner            string   `json:"owner,omitempty"`
	Name             string   `json:"name,omitempty"`
	FullName         string   `json:"full_name,omitempty"`
	Description      string   `json:"description,omitempty"`
	OwnerAvatar      string   `json:"owner_avatar,omitempty"`
	Homepage         string   `json:"homepage,omitempty"`
	Language         string   `json:"language,omitempty"`
	Stars            int      `json:"stars"`
	Forks            int      `json:"forks"`
	Watchers         int      `json:"watchers"`
	Subscribers      int      `json:"subscribers"`
	OpenIssues       int      `json:"open_issues"`
	DefaultBranch    string   `json:"default_branch"`
	LicenseSpdx      string   `json:"license_spdx,omitempty"`
	Topics           []string `json:"topics"`
	IsArchived       bool     `json:"is_archived"`
	IsFork           bool     `json:"is_fork"`
	PushedAt         string   `json:"pushed_at,omitempty"`
	UpdatedAt        string   `json:"updated_at"`
	CreatedAt        string   `json:"created_at"`
	EntryTitle       string   `json:"entry_title"`
	EntryDescription string   `json:"entry_description,omitempty"`
	SectionPath      []string `json:"section_path"`
	RawURL           string   `json:"-"`
	SourceAnchorURL  string   `json:"source_anchor_url"`
	EntryOrder       int      `json:"entry_order"`
	FirstSeenSHA     string   `json:"-"`
	LastSeenSHA      string   `json:"-"`
}

// AwesomeSyncRun 记录持久化同步任务，避免运营页面关闭后丢失任务状态。
type AwesomeSyncRun struct {
	ID             string     `json:"id"`
	SourceID       string     `json:"source_id"`
	Status         string     `json:"status"`
	Trigger        string     `json:"trigger"`
	ReadmeSHA      string     `json:"readme_sha,omitempty"`
	GitHubCount    int        `json:"github_count"`
	ExternalCount  int        `json:"external_count"`
	InvalidCount   int        `json:"invalid_count"`
	DuplicateCount int        `json:"duplicate_count"`
	ErrorCode      string     `json:"error_code,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// AwesomeEntriesSnapshot 是单一来源公开快照的响应主体。
type AwesomeEntriesSnapshot struct {
	Source  AwesomeEntriesSource `json:"source"`
	Entries []AwesomeEntry       `json:"entries"`
}

// AwesomeEntriesSource keeps the entries response source header intentionally small.
type AwesomeEntriesSource struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"display_name"`
	UpdatedAt   time.Time `json:"updated_at"`
}
