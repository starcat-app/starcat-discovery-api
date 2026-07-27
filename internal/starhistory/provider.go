// Package starhistory 负责公开仓库 Star 历史的事件查询、归一化、降采样与异步构建。
package starhistory

import (
	"context"
	"time"
)

// DailyWatchEvent 是 GH Archive 中某个 UTC 日期的 WatchEvent 聚合数。
type DailyWatchEvent struct {
	Date  time.Time
	Count int64
}

// HistoryEventRequest 使用稳定 GitHub repo ID 查询有界日期范围。
type HistoryEventRequest struct {
	RepoID             int64
	StartDate          time.Time
	EndDate            time.Time
	MaximumBytesBilled int64
}

// HistoryEventProvider 隔离外部分析引擎，单元测试和 worker 不依赖 BigQuery SDK。
type HistoryEventProvider interface {
	DailyWatchEvents(ctx context.Context, request HistoryEventRequest) ([]DailyWatchEvent, error)
}
