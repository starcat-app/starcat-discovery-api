package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

// ClaimStarHistoryBuild 原子认领首次 miss 或已过期缓存的构建任务。
//
// 未过期的 building / ready / failed 都返回 claimed=false：前者用于同仓去重，后两者
// 分别保护正常 TTL 与 negative cache。表不引用 repos，避免用户查看任意公开仓库时
// 污染 Discovery catalog。
func (s *SQLiteStore) ClaimStarHistoryBuild(
	ctx context.Context,
	request model.StarHistoryBuildRequest,
	now time.Time,
	leaseExpiresAt time.Time,
) (bool, error) {
	if request.GhRepoID <= 0 {
		return false, fmt.Errorf("gh_repo_id must be positive")
	}
	if strings.TrimSpace(request.FullName) == "" {
		return false, fmt.Errorf("full_name is required")
	}
	if request.CurrentStars < 0 {
		return false, fmt.Errorf("current_stars must not be negative")
	}
	if now.IsZero() || !leaseExpiresAt.After(now) {
		return false, fmt.Errorf("valid build lease is required")
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO repo_star_history_cache (
			gh_repo_id, full_name, current_stars, status, coverage_start,
			points_json, generated_at, expires_at, error_summary, updated_at
		) VALUES (?, ?, ?, 'building', NULL, '[]', NULL, ?, NULL, ?)
		ON CONFLICT(gh_repo_id) DO UPDATE SET
			full_name = excluded.full_name,
			current_stars = excluded.current_stars,
			status = 'building',
			expires_at = excluded.expires_at,
			error_summary = NULL,
			updated_at = excluded.updated_at
		WHERE repo_star_history_cache.expires_at <= excluded.updated_at
	`, request.GhRepoID, strings.TrimSpace(request.FullName), request.CurrentStars,
		timeString(leaseExpiresAt), timeString(now))
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

// SaveStarHistoryReady 保存已完成的完整序列并开始 ready TTL。
func (s *SQLiteStore) SaveStarHistoryReady(
	ctx context.Context,
	cache model.StarHistoryCache,
) error {
	if err := validateStarHistoryCache(cache, model.StarHistoryReady); err != nil {
		return err
	}
	pointsJSON, err := json.Marshal(cache.Points)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO repo_star_history_cache (
			gh_repo_id, full_name, current_stars, status, coverage_start,
			points_json, generated_at, expires_at, error_summary, updated_at
		) VALUES (?, ?, ?, 'ready', ?, ?, ?, ?, NULL, ?)
		ON CONFLICT(gh_repo_id) DO UPDATE SET
			full_name = excluded.full_name,
			current_stars = excluded.current_stars,
			status = 'ready',
			coverage_start = excluded.coverage_start,
			points_json = excluded.points_json,
			generated_at = excluded.generated_at,
			expires_at = excluded.expires_at,
			error_summary = NULL,
			updated_at = excluded.updated_at
	`, cache.GhRepoID, strings.TrimSpace(cache.FullName), cache.CurrentStars,
		nullable(cache.CoverageStart), string(pointsJSON), timePtrString(cache.GeneratedAt),
		timeString(cache.ExpiresAt), timeString(cache.UpdatedAt))
	return err
}

// SaveStarHistoryFailed 写入可过期的失败摘要；不保存 provider 原始响应或用户凭据。
func (s *SQLiteStore) SaveStarHistoryFailed(
	ctx context.Context,
	cache model.StarHistoryCache,
) error {
	if err := validateStarHistoryCache(cache, model.StarHistoryFailed); err != nil {
		return err
	}
	summary := strings.TrimSpace(cache.ErrorSummary)
	if len(summary) > 512 {
		summary = summary[:512]
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO repo_star_history_cache (
			gh_repo_id, full_name, current_stars, status, coverage_start,
			points_json, generated_at, expires_at, error_summary, updated_at
		) VALUES (?, ?, ?, 'failed', NULL, '[]', NULL, ?, ?, ?)
		ON CONFLICT(gh_repo_id) DO UPDATE SET
			full_name = excluded.full_name,
			current_stars = excluded.current_stars,
			status = 'failed',
			coverage_start = NULL,
			points_json = '[]',
			generated_at = NULL,
			expires_at = excluded.expires_at,
			error_summary = excluded.error_summary,
			updated_at = excluded.updated_at
	`, cache.GhRepoID, strings.TrimSpace(cache.FullName), cache.CurrentStars,
		timeString(cache.ExpiresAt), nullable(summary), timeString(cache.UpdatedAt))
	return err
}

// GetStarHistoryCache 返回单仓持久化状态；损坏 JSON 视为存储错误，不能伪装成 miss。
func (s *SQLiteStore) GetStarHistoryCache(
	ctx context.Context,
	repoID int64,
) (model.StarHistoryCache, bool, error) {
	var cache model.StarHistoryCache
	var status string
	var coverageStart, generatedAt, errorSummary sql.NullString
	var pointsJSON, expiresAt, updatedAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT gh_repo_id, full_name, current_stars, status, coverage_start,
			points_json, generated_at, expires_at, error_summary, updated_at
		FROM repo_star_history_cache
		WHERE gh_repo_id = ?
	`, repoID).Scan(
		&cache.GhRepoID, &cache.FullName, &cache.CurrentStars, &status, &coverageStart,
		&pointsJSON, &generatedAt, &expiresAt, &errorSummary, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return model.StarHistoryCache{}, false, nil
	}
	if err != nil {
		return model.StarHistoryCache{}, false, err
	}
	cache.Status = model.StarHistoryCacheStatus(status)
	if !cache.Status.Valid() {
		return model.StarHistoryCache{}, false, fmt.Errorf("invalid star history status %q", status)
	}
	cache.CoverageStart = coverageStart.String
	cache.ErrorSummary = errorSummary.String
	if err := json.Unmarshal([]byte(pointsJSON), &cache.Points); err != nil {
		return model.StarHistoryCache{}, false, fmt.Errorf("decode star history points: %w", err)
	}
	if cache.Points == nil {
		cache.Points = []model.StarHistoryPoint{}
	}
	cache.ExpiresAt, err = time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return model.StarHistoryCache{}, false, fmt.Errorf("parse star history expiry: %w", err)
	}
	cache.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return model.StarHistoryCache{}, false, fmt.Errorf("parse star history update: %w", err)
	}
	if generatedAt.Valid {
		parsed, parseErr := time.Parse(time.RFC3339, generatedAt.String)
		if parseErr != nil {
			return model.StarHistoryCache{}, false, fmt.Errorf("parse star history generation: %w", parseErr)
		}
		cache.GeneratedAt = &parsed
	}
	return cache, true, nil
}

// ListStarHistorySnapshots 返回 Discovery catalog 已经采集到的精确每日快照。
//
// 星标历史缓存允许任意公开仓库，因此这里查不到数据是正常结果；worker 只把已有
// catalog 快照作为精确锚点，不会为了补快照额外请求 GitHub。
func (s *SQLiteStore) ListStarHistorySnapshots(
	ctx context.Context,
	repoID int64,
) ([]model.StarHistoryPoint, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT date, stars
		FROM repo_daily_snapshots
		WHERE gh_repo_id = ?
		ORDER BY date
	`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]model.StarHistoryPoint, 0)
	for rows.Next() {
		var point model.StarHistoryPoint
		if err := rows.Scan(&point.Date, &point.Count); err != nil {
			return nil, err
		}
		point.Source = model.StarHistorySourceDiscoverySnapshot
		point.Precision = model.StarHistorySnapshot
		points = append(points, point)
	}
	return points, rows.Err()
}

func validateStarHistoryCache(
	cache model.StarHistoryCache,
	expectedStatus model.StarHistoryCacheStatus,
) error {
	if cache.GhRepoID <= 0 {
		return fmt.Errorf("gh_repo_id must be positive")
	}
	if strings.TrimSpace(cache.FullName) == "" {
		return fmt.Errorf("full_name is required")
	}
	if cache.CurrentStars < 0 {
		return fmt.Errorf("current_stars must not be negative")
	}
	if cache.Status != expectedStatus {
		return fmt.Errorf("status must be %s", expectedStatus)
	}
	if cache.ExpiresAt.IsZero() || cache.UpdatedAt.IsZero() {
		return fmt.Errorf("expires_at and updated_at are required")
	}
	if expectedStatus == model.StarHistoryReady && cache.GeneratedAt == nil {
		return fmt.Errorf("generated_at is required for ready cache")
	}
	return nil
}
