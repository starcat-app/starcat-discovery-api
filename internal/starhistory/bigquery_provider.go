package starhistory

import (
	"context"
	"fmt"
	"sort"
)

const ghArchiveDailyQuery = `
SELECT
  DATE(created_at) AS event_day,
  COUNT(*) AS watch_events
FROM ` + "`githubarchive.day.20*`" + `
WHERE _TABLE_SUFFIX BETWEEN @start_suffix AND @end_suffix
  AND type = 'WatchEvent'
  AND repo.id = @repo_id
GROUP BY event_day
ORDER BY event_day`

// BigQueryParameter 是 REST / SDK runner 都能实现的最小命名参数。
type BigQueryParameter struct {
	Name  string
	Type  string
	Value string
}

// BigQueryRequest 固化参数化 SQL、dry run 与单次扫描预算。
type BigQueryRequest struct {
	ProjectID          string
	SQL                string
	Parameters         []BigQueryParameter
	MaximumBytesBilled int64
	DryRun             bool
}

// BigQueryResult 只暴露历史 Provider 需要的日聚合与扫描字节。
type BigQueryResult struct {
	Rows                []DailyWatchEvent
	TotalBytesProcessed int64
}

// BigQueryRunner 把认证与 BigQuery HTTP/SDK 细节留在可替换边界内。
type BigQueryRunner interface {
	Run(ctx context.Context, request BigQueryRequest) (BigQueryResult, error)
}

// GHArchiveBigQueryProvider 使用稳定 repo ID 和命名参数查询公开 WatchEvent。
type GHArchiveBigQueryProvider struct {
	projectID string
	runner    BigQueryRunner
}

// NewGHArchiveBigQueryProvider 创建 Provider；缺少项目或 runner 时立即拒绝。
func NewGHArchiveBigQueryProvider(projectID string, runner BigQueryRunner) (*GHArchiveBigQueryProvider, error) {
	if projectID == "" {
		return nil, fmt.Errorf("GCP_PROJECT_ID is required")
	}
	if runner == nil {
		return nil, fmt.Errorf("bigquery runner is required")
	}
	return &GHArchiveBigQueryProvider{projectID: projectID, runner: runner}, nil
}

// DailyWatchEvents 先执行 dry run 再执行真实查询，两个请求都带同一硬预算。
//
// M0 未授权时上层不会调用本方法；实现本身不会在构造阶段产生网络或费用。
func (p *GHArchiveBigQueryProvider) DailyWatchEvents(
	ctx context.Context,
	request HistoryEventRequest,
) ([]DailyWatchEvent, error) {
	if request.RepoID <= 0 {
		return nil, fmt.Errorf("repo_id must be positive")
	}
	if request.StartDate.IsZero() || request.EndDate.IsZero() ||
		request.EndDate.Before(request.StartDate) {
		return nil, fmt.Errorf("valid date range is required")
	}
	if request.MaximumBytesBilled <= 0 {
		return nil, fmt.Errorf("BIGQUERY_MAX_BYTES_BILLED must be positive")
	}

	query := BigQueryRequest{
		ProjectID:          p.projectID,
		SQL:                ghArchiveDailyQuery,
		Parameters:         historyQueryParameters(request),
		MaximumBytesBilled: request.MaximumBytesBilled,
		DryRun:             true,
	}
	result, err := p.runner.Run(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("bigquery dry run: %w", err)
	}
	if result.TotalBytesProcessed > request.MaximumBytesBilled {
		return nil, fmt.Errorf(
			"bigquery dry run exceeds budget: %d > %d",
			result.TotalBytesProcessed,
			request.MaximumBytesBilled,
		)
	}

	query.DryRun = false
	result, err = p.runner.Run(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("bigquery query: %w", err)
	}
	if result.TotalBytesProcessed > request.MaximumBytesBilled {
		return nil, fmt.Errorf(
			"bigquery query exceeded budget: %d > %d",
			result.TotalBytesProcessed,
			request.MaximumBytesBilled,
		)
	}
	rows := append([]DailyWatchEvent(nil), result.Rows...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Date.Before(rows[j].Date) })
	return rows, nil
}

func historyQueryParameters(request HistoryEventRequest) []BigQueryParameter {
	return []BigQueryParameter{
		{Name: "repo_id", Type: "INT64", Value: fmt.Sprintf("%d", request.RepoID)},
		{Name: "start_suffix", Type: "STRING", Value: request.StartDate.UTC().Format("060102")},
		{Name: "end_suffix", Type: "STRING", Value: request.EndDate.UTC().Format("060102")},
	}
}

var _ HistoryEventProvider = (*GHArchiveBigQueryProvider)(nil)
