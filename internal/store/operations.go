package store

import (
	"context"
	"database/sql"
)

// OperationalStats 汇总 Discovery 的目录规模、数据新鲜度与后台任务状态。
// 所有查询均返回聚合值，避免运营接口泄露仓库正文或 Star History 明细。
type OperationalStats struct {
	Repositories struct {
		Total       int64  `json:"total"`
		Available   int64  `json:"available"`
		Archived    int64  `json:"archived"`
		Forks       int64  `json:"forks"`
		WithRelease int64  `json:"with_release"`
		LastIndexed string `json:"last_indexed,omitempty"`
	} `json:"repositories"`
	Rankings struct {
		CategoryEntries int64  `json:"category_entries"`
		TopicEntries    int64  `json:"topic_entries"`
		LastGenerated   string `json:"last_generated,omitempty"`
	} `json:"rankings"`
	StarHistory struct {
		Ready         int64 `json:"ready"`
		Building      int64 `json:"building"`
		Failed        int64 `json:"failed"`
		Expired       int64 `json:"expired"`
		ReservedBytes int64 `json:"reserved_bytes"`
	} `json:"star_history"`
	Awesome struct {
		Sources        int64  `json:"sources"`
		Published      int64  `json:"published"`
		ActiveEntries  int64  `json:"active_entries"`
		LastSuccessful string `json:"last_successful,omitempty"`
		ActiveSyncRuns int64  `json:"active_sync_runs"`
		FailedSyncRuns int64  `json:"failed_sync_runs"`
	} `json:"awesome"`
	Sync struct {
		Running    int64  `json:"running"`
		Succeeded  int64  `json:"succeeded"`
		Failed     int64  `json:"failed"`
		LastFinish string `json:"last_finish,omitempty"`
	} `json:"sync"`
}

// SyncRunSummary 是可供控制台展示的受限同步记录，不包含 error_message 原文。
type SyncRunSummary struct {
	ID            int64  `json:"id"`
	Mode          string `json:"mode"`
	Status        string `json:"status"`
	StartedAt     string `json:"started_at"`
	FinishedAt    string `json:"finished_at,omitempty"`
	ReposSeen     int64  `json:"repos_seen"`
	ReposUpserted int64  `json:"repos_upserted"`
	HasError      bool   `json:"has_error"`
}

// OperationalStats 返回固定 SQL 聚合，供 Admin Console 读取。
func (s *SQLiteStore) OperationalStats(ctx context.Context) (OperationalStats, error) {
	var result OperationalStats
	queries := []struct {
		query string
		dest  []any
	}{
		{`SELECT COUNT(*), COALESCE(SUM(CASE WHEN is_archived = 0 AND is_fork = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(is_archived), 0), COALESCE(SUM(is_fork), 0), COALESCE(SUM(CASE WHEN latest_release_at IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(MAX(indexed_at), '') FROM repos`, []any{&result.Repositories.Total, &result.Repositories.Available, &result.Repositories.Archived, &result.Repositories.Forks, &result.Repositories.WithRelease, &result.Repositories.LastIndexed}},
		{`SELECT COUNT(*), (SELECT COUNT(*) FROM topic_rankings), COALESCE(MAX(generated_at), '') FROM category_rankings`, []any{&result.Rankings.CategoryEntries, &result.Rankings.TopicEntries, &result.Rankings.LastGenerated}},
		{`SELECT COALESCE(SUM(CASE WHEN status = 'ready' THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN status = 'building' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN expires_at <= strftime('%Y-%m-%dT%H:%M:%fZ', 'now') THEN 1 ELSE 0 END), 0)
			FROM repo_star_history_cache`, []any{&result.StarHistory.Ready, &result.StarHistory.Building, &result.StarHistory.Failed, &result.StarHistory.Expired}},
		{`SELECT COALESCE(SUM(reserved_bytes), 0) FROM star_history_daily_budgets`, []any{&result.StarHistory.ReservedBytes}},
		{`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'published' THEN 1 ELSE 0 END), 0), COALESCE(MAX(last_synced_at), '') FROM awesome_sources`, []any{&result.Awesome.Sources, &result.Awesome.Published, &result.Awesome.LastSuccessful}},
		{`SELECT COUNT(*) FROM awesome_entries WHERE is_active = 1`, []any{&result.Awesome.ActiveEntries}},
		{`SELECT COALESCE(SUM(CASE WHEN status IN ('queued', 'running') THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0) FROM awesome_sync_runs`, []any{&result.Awesome.ActiveSyncRuns, &result.Awesome.FailedSyncRuns}},
		{`SELECT COALESCE(SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0), COALESCE(MAX(finished_at), '') FROM sync_runs`, []any{&result.Sync.Running, &result.Sync.Succeeded, &result.Sync.Failed, &result.Sync.LastFinish}},
	}
	for _, item := range queries {
		if err := s.db.QueryRowContext(ctx, item.query).Scan(item.dest...); err != nil {
			return OperationalStats{}, err
		}
	}
	return result, nil
}

// ListSyncRuns 返回最近同步记录，错误详情仅暴露布尔状态。
func (s *SQLiteStore) ListSyncRuns(ctx context.Context, limit int) ([]SyncRunSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, mode, status, started_at, finished_at,
		repos_seen, repos_upserted, error_message IS NOT NULL FROM sync_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SyncRunSummary
	for rows.Next() {
		var item SyncRunSummary
		var finished sql.NullString
		if err := rows.Scan(&item.ID, &item.Mode, &item.Status, &item.StartedAt, &finished, &item.ReposSeen, &item.ReposUpserted, &item.HasError); err != nil {
			return nil, err
		}
		item.FinishedAt = finished.String
		result = append(result, item)
	}
	return result, rows.Err()
}
