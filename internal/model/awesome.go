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
	GitHubRepoCount    int                 `json:"github_repo_count"`
	ExternalEntryCount int                 `json:"external_entry_count"`
	LastSyncedAt       *time.Time          `json:"last_synced_at,omitempty"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

// AwesomeEntry 是 README 中一条可审计的来源事实。
//
// 外部链接也持久化用于运营统计，但公共 entries API 只读取 GitHub Repo 条目。
type AwesomeEntry struct {
	SourceID         string   `json:"source_id,omitempty"`
	TargetType       string   `json:"target_type,omitempty"`
	TargetKey        string   `json:"target_key,omitempty"`
	GhRepoID         *int64   `json:"gh_repo_id,omitempty"`
	Owner            string   `json:"owner,omitempty"`
	Name             string   `json:"name,omitempty"`
	FullName         string   `json:"full_name,omitempty"`
	Description      string   `json:"description,omitempty"`
	OwnerAvatar      string   `json:"owner_avatar,omitempty"`
	Language         string   `json:"language,omitempty"`
	Stars            int      `json:"stars,omitempty"`
	IsArchived       bool     `json:"is_archived,omitempty"`
	EntryTitle       string   `json:"entry_title"`
	EntryDescription string   `json:"entry_description,omitempty"`
	SectionPath      []string `json:"section_path"`
	RawURL           string   `json:"raw_url,omitempty"`
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
	Source  AwesomeSource  `json:"source"`
	Entries []AwesomeEntry `json:"entries"`
}
