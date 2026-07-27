package model

import "time"

// StarHistoryCacheStatus 是单仓历史构建的持久化状态。
type StarHistoryCacheStatus string

const (
	StarHistoryBuilding StarHistoryCacheStatus = "building"
	StarHistoryReady    StarHistoryCacheStatus = "ready"
	StarHistoryFailed   StarHistoryCacheStatus = "failed"
)

// Valid 防止应用层绕过数据库 CHECK 写入未定义状态。
func (status StarHistoryCacheStatus) Valid() bool {
	switch status {
	case StarHistoryBuilding, StarHistoryReady, StarHistoryFailed:
		return true
	default:
		return false
	}
}

// StarHistorySource 区分估算事件、Discovery 精确快照和客户端本机精确快照。
type StarHistorySource string

const (
	StarHistorySourceGHArchive         StarHistorySource = "gh_archive"
	StarHistorySourceDiscoverySnapshot StarHistorySource = "discovery_snapshot"
	StarHistorySourceLocalSnapshot     StarHistorySource = "local_snapshot"
)

// StarHistoryPrecision 明确曲线是否可以被解释为精确值。
type StarHistoryPrecision string

const (
	StarHistoryEstimated StarHistoryPrecision = "estimated"
	StarHistorySnapshot  StarHistoryPrecision = "snapshot"
)

// StarHistoryPoint 是 API 与缓存共用的日级星标历史点。
type StarHistoryPoint struct {
	Date      string               `json:"date"`
	Count     int                  `json:"count"`
	Source    StarHistorySource    `json:"source"`
	Precision StarHistoryPrecision `json:"precision"`
}

// StarHistoryCache 独立于 Discovery catalog 保存任意公开仓库的构建结果。
//
// ExpiresAt 在 building 状态下表示任务租约，在 ready / failed 状态下分别表示缓存 TTL
// 与 negative cache TTL。这样服务重启后无需额外内存状态即可重新认领过期任务。
type StarHistoryCache struct {
	GhRepoID      int64
	FullName      string
	CurrentStars  int
	Status        StarHistoryCacheStatus
	CoverageStart string
	Points        []StarHistoryPoint
	GeneratedAt   *time.Time
	ExpiresAt     time.Time
	ErrorSummary  string
	UpdatedAt     time.Time
}

// StarHistoryBuildRequest 是认领构建任务所需的最小公开仓库身份。
type StarHistoryBuildRequest struct {
	GhRepoID     int64
	FullName     string
	CurrentStars int
}

// StarHistoryRange 是 API 支持的固定观察范围。
type StarHistoryRange string

const (
	StarHistoryRangeThreeMonths StarHistoryRange = "3m"
	StarHistoryRangeOneYear     StarHistoryRange = "1y"
	StarHistoryRangeAll         StarHistoryRange = "all"
)

// StarHistorySeries 是按范围降采样后的 API 领域结果。
type StarHistorySeries struct {
	Range         StarHistoryRange   `json:"range"`
	CoverageStart string             `json:"coverage_start,omitempty"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Points        []StarHistoryPoint `json:"points"`
}

// StarHistoryResponse 是客户端稳定消费的公开仓库历史契约。
type StarHistoryResponse struct {
	RepoID        int64              `json:"repo_id"`
	FullName      string             `json:"full_name"`
	CurrentStars  int                `json:"current_stars"`
	Range         StarHistoryRange   `json:"range"`
	CoverageStart string             `json:"coverage_start,omitempty"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Points        []StarHistoryPoint `json:"points"`
}
