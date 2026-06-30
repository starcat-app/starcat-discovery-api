package model

import "time"

const (
	// AllBucket 是 ranking bucket 的全量哨兵值。
	AllBucket = "__all__"
	// UncategorizedLanguageKey 与 Starcat 客户端已有 Trending 语义保持一致。
	UncategorizedLanguageKey = "__uncategorized__"
)

// TopicDefinition 是发现页左侧主题元数据。
type TopicDefinition struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// PlatformDefinition 是发现页左侧平台元数据。
type PlatformDefinition struct {
	Code       string `json:"code"`
	Label      string `json:"label"`
	SystemName string `json:"system_name,omitempty"`
}

// DefaultTopics 是 Starcat 首期支持的发现主题。
var DefaultTopics = []TopicDefinition{
	{Code: "ai", Label: "人工智能"},
	{Code: "privacy", Label: "隐私"},
	{Code: "networking", Label: "网络"},
	{Code: "media", Label: "媒体"},
	{Code: "social", Label: "社交"},
	{Code: "reading", Label: "阅读"},
	{Code: "tools", Label: "工具"},
}

// DefaultPlatforms 是 Starcat 首期支持的平台筛选。
var DefaultPlatforms = []PlatformDefinition{
	{Code: "macos", Label: "macOS", SystemName: "desktopcomputer"},
	{Code: "ios", Label: "iOS", SystemName: "iphone"},
	{Code: "cli", Label: "CLI", SystemName: "terminal"},
	{Code: "web", Label: "Web", SystemName: "globe"},
	{Code: "server", Label: "Server", SystemName: "server.rack"},
	{Code: "android", Label: "Android", SystemName: "apps.iphone"},
	{Code: "windows", Label: "Windows", SystemName: "pc"},
	{Code: "linux", Label: "Linux", SystemName: "terminal"},
}

// Repository 是 discovery catalog 的核心仓库模型。
type Repository struct {
	GhRepoID             int64
	Owner                string
	Name                 string
	FullName             string
	Description          string
	Homepage             string
	Language             string
	Stars                int
	Forks                int
	Watchers             int
	Subscribers          int
	OpenIssues           int
	OwnerAvatar          string
	DefaultBranch        string
	LicenseSpdx          string
	Topics               []string
	Platforms            []string
	PushedAt             *time.Time
	UpdatedAt            *time.Time
	CreatedAt            *time.Time
	IsArchived           bool
	IsFork               bool
	LatestReleaseTag     string
	LatestReleaseAt      *time.Time
	LatestReleaseURL     string
	ReleaseDownloadCount int
	TrendingScore        float64
	PopularityScore      float64
	ReleaseScore         float64
	DiscoveryScore       float64
	SearchScore          float64
	IndexedAt            time.Time
	EnrichedAt           *time.Time
}

// ReleaseAsset 是 GitHub Release asset 的最小缓存单元。
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url,omitempty"`
	DownloadCount      int    `json:"download_count"`
}

// Release 缓存 GitHub stable / prerelease release 元数据。
//
// new-releases 首屏只使用非 draft、非 prerelease 的 stable release，但 prerelease 仍保存，
// 方便后续观察项目活跃度。
type Release struct {
	GhRepoID      int64
	TagName       string
	Name          string
	HTMLURL       string
	PublishedAt   time.Time
	Draft         bool
	Prerelease    bool
	DownloadCount int
	Assets        []ReleaseAsset
	IndexedAt     time.Time
}

// DailySnapshot 保存每日关键指标，用于后续计算增长和趋势。
type DailySnapshot struct {
	Date                 string
	GhRepoID             int64
	Stars                int
	Forks                int
	Watchers             int
	ReleaseDownloadCount int
	CapturedAt           time.Time
}

// SyncResult 是 admin sync 返回给运维端的结果摘要。
type SyncResult struct {
	RunID         int64  `json:"run_id"`
	Mode          string `json:"mode"`
	Status        string `json:"status"`
	ReposSeen     int    `json:"repos_seen"`
	ReposUpserted int    `json:"repos_upserted"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at,omitempty"`
	ErrorMessage  string `json:"error_message,omitempty"`
}

// DiscoveryItem 是 Starcat 客户端列表和详情页直接消费的卡片模型。
type DiscoveryItem struct {
	RepoID               int64          `json:"repo_id"`
	FullName             string         `json:"full_name"`
	Owner                string         `json:"owner"`
	Name                 string         `json:"name"`
	Description          string         `json:"description,omitempty"`
	Homepage             string         `json:"homepage,omitempty"`
	Language             string         `json:"language,omitempty"`
	Stars                int            `json:"stars"`
	Forks                int            `json:"forks"`
	Watchers             int            `json:"watchers"`
	Subscribers          int            `json:"subscribers"`
	OpenIssues           int            `json:"open_issues"`
	OwnerAvatar          string         `json:"owner_avatar,omitempty"`
	DefaultBranch        string         `json:"default_branch,omitempty"`
	LicenseSpdx          string         `json:"license_spdx,omitempty"`
	Topics               []string       `json:"topics"`
	Platforms            []string       `json:"platforms"`
	PushedAt             string         `json:"pushed_at,omitempty"`
	UpdatedAt            string         `json:"updated_at,omitempty"`
	CreatedAt            string         `json:"created_at,omitempty"`
	IsArchived           bool           `json:"is_archived"`
	IsFork               bool           `json:"is_fork"`
	LatestReleaseTag     string         `json:"latest_release_tag,omitempty"`
	LatestReleaseAt      string         `json:"latest_release_at,omitempty"`
	LatestReleaseURL     string         `json:"latest_release_url,omitempty"`
	ReleaseDownloadCount int            `json:"release_download_count"`
	Rank                 int            `json:"rank,omitempty"`
	Score                float64        `json:"score,omitempty"`
	TrendingScore        float64        `json:"trending_score"`
	PopularityScore      float64        `json:"popularity_score"`
	ReleaseScore         float64        `json:"release_score"`
	DiscoveryScore       float64        `json:"discovery_score"`
	SearchScore          float64        `json:"search_score"`
	Reasons              []string       `json:"reasons"`
	Signals              []Signal       `json:"signals"`
	Categories           []string       `json:"categories"`
	CategoryRanks        map[string]int `json:"category_ranks"`
}

// Signal 是列表和详情中用于解释推荐理由的结构化信号。
type Signal struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Value string `json:"value,omitempty"`
}

// RankingEntry 是 ranking job 写入排名表的最小单元。
type RankingEntry struct {
	RepoID int64
	Rank   int
	Score  float64
}

// Page 是 store 层返回分页结果的通用容器。
type Page[T any] struct {
	Items    []T
	Total    int
	Page     int
	PageSize int
	NextPage *int
}

// LanguageStat 是 /api/v1/discovery/languages 的 data item。
type LanguageStat struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// FacetCount 是 summary 接口返回给 Sidebar 的可计数筛选项。
type FacetCount struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Count      int    `json:"count"`
	SystemName string `json:"system_name,omitempty"`
}

// ModeSummary 描述探索一级子模块的总量和可展示筛选维度。
type ModeSummary struct {
	Mode      string       `json:"mode"`
	Total     int          `json:"total"`
	Topics    []FacetCount `json:"topics,omitempty"`
	Platforms []FacetCount `json:"platforms,omitempty"`
	Languages []FacetCount `json:"languages,omitempty"`
}

// DiscoverySummary 是 /api/v1/discovery/summary 的 data 结构。
type DiscoverySummary struct {
	Modes       []ModeSummary `json:"modes"`
	GeneratedAt string        `json:"generated_at"`
}

// DiscoveryBulk 是 Starcat 客户端本地优先缓存使用的全量快照。
//
// repos 是当前 discovery catalog 的完整公开仓库集合；summary 与同一请求内 repos
// 来自同一个 SQLite 读取时刻，客户端可以在单个 transaction 中一起落盘，避免列表和
// Sidebar 计数分裂。
type DiscoveryBulk struct {
	Repos   []DiscoveryItem  `json:"repos"`
	Summary DiscoverySummary `json:"summary"`
}
