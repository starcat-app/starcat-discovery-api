package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dong4j/starcat-discovery-api/internal/model"
)

// SQLiteStore 是 discovery 服务的持久化入口。
//
// SQLite 使用 WAL + 单写连接，避免后台同步和 HTTP 读取互相造成长时间锁等待。
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore 打开 SQLite 并创建 schema。
func NewSQLiteStore(ctx context.Context, dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dsn+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := createSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	log.Printf("[store] sqlite opened at %s", dsn)
	return &SQLiteStore{db: db}, nil
}

// Close 关闭 SQLite 连接。
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// UpsertRepo 写入或更新一个仓库，同时刷新主题和平台映射表。
func (s *SQLiteStore) UpsertRepo(ctx context.Context, repo model.Repository) error {
	if repo.GhRepoID <= 0 {
		return fmt.Errorf("gh_repo_id must be positive")
	}
	if repo.FullName == "" || repo.Owner == "" || repo.Name == "" {
		return fmt.Errorf("owner/name/full_name are required")
	}
	if repo.IndexedAt.IsZero() {
		repo.IndexedAt = time.Now().UTC()
	}

	topicsJSON, err := json.Marshal(repo.Topics)
	if err != nil {
		return err
	}
	platformsJSON, err := json.Marshal(repo.Platforms)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO repos (
			gh_repo_id, owner, name, full_name, description, homepage, language,
			stars, forks, watchers, subscribers, open_issues, owner_avatar,
			default_branch, license_spdx, topics_json, platforms_json,
			pushed_at, updated_at, created_at, is_archived, is_fork,
			latest_release_tag, latest_release_at, latest_release_url, release_download_count,
			trending_score, popularity_score, release_score, discovery_score, search_score,
			indexed_at, enriched_at
		) VALUES (
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?
		)
		ON CONFLICT(gh_repo_id) DO UPDATE SET
			owner = excluded.owner,
			name = excluded.name,
			full_name = excluded.full_name,
			description = excluded.description,
			homepage = excluded.homepage,
			language = excluded.language,
			stars = excluded.stars,
			forks = excluded.forks,
			watchers = excluded.watchers,
			subscribers = excluded.subscribers,
			open_issues = excluded.open_issues,
			owner_avatar = excluded.owner_avatar,
			default_branch = excluded.default_branch,
			license_spdx = excluded.license_spdx,
			topics_json = excluded.topics_json,
			platforms_json = excluded.platforms_json,
			pushed_at = excluded.pushed_at,
			updated_at = excluded.updated_at,
			created_at = excluded.created_at,
			is_archived = excluded.is_archived,
			is_fork = excluded.is_fork,
			latest_release_tag = excluded.latest_release_tag,
			latest_release_at = excluded.latest_release_at,
			latest_release_url = excluded.latest_release_url,
			release_download_count = excluded.release_download_count,
			trending_score = excluded.trending_score,
			popularity_score = excluded.popularity_score,
			release_score = excluded.release_score,
			discovery_score = excluded.discovery_score,
			search_score = excluded.search_score,
			indexed_at = excluded.indexed_at,
			enriched_at = excluded.enriched_at
	`, repo.GhRepoID, repo.Owner, repo.Name, repo.FullName, nullable(repo.Description), nullable(repo.Homepage), nullable(repo.Language),
		repo.Stars, repo.Forks, repo.Watchers, repo.Subscribers, repo.OpenIssues, nullable(repo.OwnerAvatar),
		nullable(repo.DefaultBranch), nullable(repo.LicenseSpdx), string(topicsJSON), string(platformsJSON),
		timePtrString(repo.PushedAt), timePtrString(repo.UpdatedAt), timePtrString(repo.CreatedAt), boolInt(repo.IsArchived), boolInt(repo.IsFork),
		nullable(repo.LatestReleaseTag), timePtrString(repo.LatestReleaseAt), nullable(repo.LatestReleaseURL), repo.ReleaseDownloadCount,
		repo.TrendingScore, repo.PopularityScore, repo.ReleaseScore, repo.DiscoveryScore, repo.SearchScore,
		timeString(repo.IndexedAt), timePtrString(repo.EnrichedAt))
	if err != nil {
		return err
	}

	if err := replaceCodes(ctx, tx, "repo_topic_codes", repo.GhRepoID, repo.Topics); err != nil {
		return err
	}
	if err := replaceCodes(ctx, tx, "repo_platform_codes", repo.GhRepoID, repo.Platforms); err != nil {
		return err
	}

	return tx.Commit()
}

// UpsertRelease 写入一个 GitHub Release 及其 asset 摘要。
func (s *SQLiteStore) UpsertRelease(ctx context.Context, release model.Release) error {
	if release.GhRepoID <= 0 {
		return fmt.Errorf("gh_repo_id must be positive")
	}
	if release.TagName == "" {
		return fmt.Errorf("tag_name is required")
	}
	if release.PublishedAt.IsZero() {
		return fmt.Errorf("published_at is required")
	}
	if release.IndexedAt.IsZero() {
		release.IndexedAt = time.Now().UTC()
	}
	if release.Assets == nil {
		release.Assets = []model.ReleaseAsset{}
	}
	assetsJSON, err := json.Marshal(release.Assets)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO repo_releases (
			gh_repo_id, tag_name, name, html_url, published_at, draft, prerelease,
			download_count, assets_json, indexed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(gh_repo_id, tag_name) DO UPDATE SET
			name = excluded.name,
			html_url = excluded.html_url,
			published_at = excluded.published_at,
			draft = excluded.draft,
			prerelease = excluded.prerelease,
			download_count = excluded.download_count,
			assets_json = excluded.assets_json,
			indexed_at = excluded.indexed_at
	`, release.GhRepoID, release.TagName, nullable(release.Name), nullable(release.HTMLURL),
		timeString(release.PublishedAt), boolInt(release.Draft), boolInt(release.Prerelease),
		release.DownloadCount, string(assetsJSON), timeString(release.IndexedAt))
	return err
}

// RecordDailySnapshot 保存每日指标快照。
func (s *SQLiteStore) RecordDailySnapshot(ctx context.Context, snapshot model.DailySnapshot) error {
	if snapshot.Date == "" {
		snapshot.Date = time.Now().UTC().Format("2006-01-02")
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO repo_daily_snapshots (
			date, gh_repo_id, stars, forks, watchers, release_download_count, captured_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(date, gh_repo_id) DO UPDATE SET
			stars = excluded.stars,
			forks = excluded.forks,
			watchers = excluded.watchers,
			release_download_count = excluded.release_download_count,
			captured_at = excluded.captured_at
	`, snapshot.Date, snapshot.GhRepoID, snapshot.Stars, snapshot.Forks, snapshot.Watchers,
		snapshot.ReleaseDownloadCount, timeString(snapshot.CapturedAt))
	return err
}

// RecordFeedExposure 记录全局曝光，用于后续发现流冷却和去重。
func (s *SQLiteStore) RecordFeedExposure(ctx context.Context, feedKey string, repoIDs []int64) error {
	if strings.TrimSpace(feedKey) == "" || len(repoIDs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO feed_exposure(feed_key, gh_repo_id, exposed_at, exposure_count)
		VALUES (?, ?, ?, 1)
		ON CONFLICT(feed_key, gh_repo_id) DO UPDATE SET
			exposed_at = excluded.exposed_at,
			exposure_count = feed_exposure.exposure_count + 1
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, repoID := range repoIDs {
		if repoID <= 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, feedKey, repoID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceCategoryRanking 原子替换某个 category/bucket 的排名。
func (s *SQLiteStore) ReplaceCategoryRanking(ctx context.Context, category, bucket string, entries []model.RankingEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM category_rankings WHERE category = ? AND bucket = ?`, category, bucket); err != nil {
		return err
	}
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO category_rankings(category, bucket, gh_repo_id, rank, score, generated_at) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, entry := range entries {
		if _, err := stmt.ExecContext(ctx, category, bucket, entry.RepoID, entry.Rank, entry.Score, generatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ReplaceTopicRanking 原子替换某个 topic/platform 的发现排名。
func (s *SQLiteStore) ReplaceTopicRanking(ctx context.Context, topic, platform string, entries []model.RankingEntry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM topic_rankings WHERE topic = ? AND platform = ?`, topic, platform); err != nil {
		return err
	}
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO topic_rankings(topic, platform, gh_repo_id, rank, score, generated_at) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, entry := range entries {
		if _, err := stmt.ExecContext(ctx, topic, platform, entry.RepoID, entry.Rank, entry.Score, generatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TopRankingEntries 返回指定 score 和过滤条件下的前 N 个 repo，用于 ranking job 写表。
func (s *SQLiteStore) TopRankingEntries(ctx context.Context, scoreColumn string, filters QueryFilters, limit int) ([]model.RankingEntry, error) {
	if !allowedScoreColumn(scoreColumn) {
		return nil, fmt.Errorf("unsupported score column %s", scoreColumn)
	}
	if limit <= 0 {
		limit = 100
	}
	where, args := buildFilterWhere(filters)
	query := "SELECT r.gh_repo_id, r." + scoreColumn + " FROM repos r" + where +
		" ORDER BY r." + scoreColumn + " DESC, r.stars DESC, r.gh_repo_id DESC LIMIT ?"
	if filters.Category == "new-releases" && scoreColumn == "release_score" {
		query = "SELECT r.gh_repo_id, r." + scoreColumn + " FROM repos r" + where +
			" ORDER BY COALESCE(r.latest_release_at, '') DESC, r.release_score DESC, r.stars DESC, r.gh_repo_id DESC LIMIT ?"
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []model.RankingEntry
	rank := 1
	for rows.Next() {
		var entry model.RankingEntry
		if err := rows.Scan(&entry.RepoID, &entry.Score); err != nil {
			return nil, err
		}
		entry.Rank = rank
		rank++
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// StartSyncRun 记录同步任务开始。
func (s *SQLiteStore) StartSyncRun(ctx context.Context, mode string) (int64, time.Time, error) {
	startedAt := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_runs(mode, status, started_at)
		VALUES (?, 'running', ?)
	`, mode, timeString(startedAt))
	if err != nil {
		return 0, time.Time{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, time.Time{}, err
	}
	return id, startedAt, nil
}

// FinishSyncRun 记录同步任务结束。
func (s *SQLiteStore) FinishSyncRun(ctx context.Context, runID int64, status string, reposSeen, reposUpserted int, errorMessage string) (time.Time, error) {
	finishedAt := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE sync_runs SET
			status = ?,
			finished_at = ?,
			repos_seen = ?,
			repos_upserted = ?,
			error_message = ?
		WHERE id = ?
	`, status, timeString(finishedAt), reposSeen, reposUpserted, nullable(errorMessage), runID)
	return finishedAt, err
}

// ListCategoryRanking 读取预计算排名表。
func (s *SQLiteStore) ListCategoryRanking(ctx context.Context, category, bucket string, page, limit int) (model.Page[model.DiscoveryItem], error) {
	page, limit = normalizePage(page, limit)
	offset := (page - 1) * limit
	total, err := s.count(ctx, `SELECT COUNT(*) FROM category_rankings WHERE category = ? AND bucket = ?`, category, bucket)
	if err != nil {
		return model.Page[model.DiscoveryItem]{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.*, cr.rank, cr.score
		FROM category_rankings cr
		JOIN repos r ON r.gh_repo_id = cr.gh_repo_id
		WHERE cr.category = ? AND cr.bucket = ?
		ORDER BY cr.rank ASC
		LIMIT ? OFFSET ?
	`, category, bucket, limit, offset)
	if err != nil {
		return model.Page[model.DiscoveryItem]{}, err
	}
	defer rows.Close()
	items, err := scanItems(rows)
	if err != nil {
		return model.Page[model.DiscoveryItem]{}, err
	}
	return makePage(items, total, page, limit), nil
}

// ListScoredRepos 按仓库 score 直接读取列表，供 ranking 表尚未生成时兜底。
func (s *SQLiteStore) ListScoredRepos(ctx context.Context, scoreColumn string, filters QueryFilters, page, limit int) (model.Page[model.DiscoveryItem], error) {
	if !allowedScoreColumn(scoreColumn) {
		return model.Page[model.DiscoveryItem]{}, fmt.Errorf("unsupported score column %s", scoreColumn)
	}
	page, limit = normalizePage(page, limit)
	where, args := buildFilterWhere(filters)
	countQuery := "SELECT COUNT(*) FROM repos r" + where
	total, err := s.count(ctx, countQuery, args...)
	if err != nil {
		return model.Page[model.DiscoveryItem]{}, err
	}

	query := "SELECT r.*, 0 AS rank, r." + scoreColumn + " AS score FROM repos r" + where +
		" ORDER BY r." + scoreColumn + " DESC, r.stars DESC, r.gh_repo_id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, (page-1)*limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return model.Page[model.DiscoveryItem]{}, err
	}
	defer rows.Close()
	items, err := scanItems(rows)
	if err != nil {
		return model.Page[model.DiscoveryItem]{}, err
	}
	return makePage(items, total, page, limit), nil
}

// ListSortedRepos 按非 score 类通用排序读取列表。
//
// 这个方法只接收枚举 sortKey，不接收调用方拼好的 SQL 片段；ORDER BY 必须在
// orderClauseForSortKey 白名单中生成，避免把 query 参数带进 SQL。
func (s *SQLiteStore) ListSortedRepos(ctx context.Context, sortKey string, filters QueryFilters, page, limit int) (model.Page[model.DiscoveryItem], error) {
	orderClause, ok := orderClauseForSortKey(sortKey)
	if !ok {
		return model.Page[model.DiscoveryItem]{}, fmt.Errorf("unsupported sort key %s", sortKey)
	}
	page, limit = normalizePage(page, limit)
	where, args := buildFilterWhere(filters)
	total, err := s.count(ctx, "SELECT COUNT(*) FROM repos r"+where, args...)
	if err != nil {
		return model.Page[model.DiscoveryItem]{}, err
	}

	query := "SELECT r.*, 0 AS rank, 0 AS score FROM repos r" + where +
		" ORDER BY " + orderClause + " LIMIT ? OFFSET ?"
	args = append(args, limit, (page-1)*limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return model.Page[model.DiscoveryItem]{}, err
	}
	defer rows.Close()
	items, err := scanItems(rows)
	if err != nil {
		return model.Page[model.DiscoveryItem]{}, err
	}
	return makePage(items, total, page, limit), nil
}

// ListAllRepos 返回 discovery catalog 的完整公开仓库快照，供 bulk endpoint 使用。
//
// 这里不接受 filter / sort 参数：客户端拿到全量后在本地做筛选、排序和分页，保持与
// Weekly bulk 同款的本地优先体验。
func (s *SQLiteStore) ListAllRepos(ctx context.Context) ([]model.DiscoveryItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.*, 0 AS rank, r.discovery_score AS score
		FROM repos r
		ORDER BY r.discovery_score DESC, r.stars DESC, r.gh_repo_id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanItems(rows)
	if err != nil {
		return nil, err
	}
	return s.attachCategoryMemberships(ctx, items)
}

// ListLanguages 聚合 discovery catalog 中可用语言。
func (s *SQLiteStore) ListLanguages(ctx context.Context) ([]model.LanguageStat, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(language, ''), ?) AS key, COUNT(*) AS count
		FROM repos
		GROUP BY key
		ORDER BY CASE WHEN key = ? THEN 1 ELSE 0 END, count DESC, key ASC
	`, model.UncategorizedLanguageKey, model.UncategorizedLanguageKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.LanguageStat, 0)
	for rows.Next() {
		var item model.LanguageStat
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return nil, err
		}
		if item.Key == model.UncategorizedLanguageKey {
			item.Label = "Uncategorized"
		} else {
			item.Label = item.Key
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// DiscoverySummary 汇总探索 Sidebar 所需的模式总量与筛选项计数。
func (s *SQLiteStore) DiscoverySummary(ctx context.Context) (model.DiscoverySummary, error) {
	totalRepos, err := s.count(ctx, `SELECT COUNT(*) FROM repos`)
	if err != nil {
		return model.DiscoverySummary{}, err
	}
	discoverTopics, err := s.topicFacetCounts(ctx)
	if err != nil {
		return model.DiscoverySummary{}, err
	}
	discoverPlatforms, err := s.platformFacetCounts(ctx)
	if err != nil {
		return model.DiscoverySummary{}, err
	}
	popularTotal, err := s.categoryTotal(ctx, "most-popular")
	if err != nil {
		return model.DiscoverySummary{}, err
	}
	popularLanguages, err := s.categoryLanguageFacetCounts(ctx, "most-popular")
	if err != nil {
		return model.DiscoverySummary{}, err
	}
	newReleaseTotal, err := s.categoryTotal(ctx, "new-releases")
	if err != nil {
		return model.DiscoverySummary{}, err
	}
	newReleaseLanguages, err := s.categoryLanguageFacetCounts(ctx, "new-releases")
	if err != nil {
		return model.DiscoverySummary{}, err
	}
	return model.DiscoverySummary{
		Modes: []model.ModeSummary{
			{
				Mode:      "discover",
				Total:     totalRepos,
				Topics:    discoverTopics,
				Platforms: discoverPlatforms,
			},
			{
				Mode:      "popular",
				Total:     popularTotal,
				Languages: popularLanguages,
			},
			{
				Mode:      "new_releases",
				Total:     newReleaseTotal,
				Languages: newReleaseLanguages,
			},
		},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// QueryFilters 是读取接口支持的通用过滤条件。
type QueryFilters struct {
	Language     string
	Platform     string
	Topic        string
	Category     string
	MinReleaseAt string
}

func (s *SQLiteStore) categoryTotal(ctx context.Context, category string) (int, error) {
	return s.count(ctx, `
		SELECT COUNT(*)
		FROM category_rankings
		WHERE category = ? AND bucket = ?
	`, category, model.AllBucket)
}

func (s *SQLiteStore) categoryLanguageFacetCounts(ctx context.Context, category string) ([]model.FacetCount, error) {
	total, err := s.count(ctx, `
		SELECT COUNT(*)
		FROM category_rankings
		WHERE category = ? AND bucket = ?
	`, category, model.AllBucket)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return []model.FacetCount{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(NULLIF(r.language, ''), ?) AS key, COUNT(*) AS count
		FROM category_rankings cr
		JOIN repos r ON r.gh_repo_id = cr.gh_repo_id
		WHERE cr.category = ? AND cr.bucket = ?
		GROUP BY key
		ORDER BY CASE WHEN key = ? THEN 1 ELSE 0 END, count DESC, key ASC
	`, model.UncategorizedLanguageKey, category, model.AllBucket, model.UncategorizedLanguageKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]model.FacetCount, 0)
	for rows.Next() {
		var item model.FacetCount
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return nil, err
		}
		if item.Key == model.UncategorizedLanguageKey {
			item.Label = "Uncategorized"
		} else {
			item.Label = item.Key
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) topicFacetCounts(ctx context.Context) ([]model.FacetCount, error) {
	counts, err := s.codeCounts(ctx, "repo_topic_codes")
	if err != nil {
		return nil, err
	}
	result := make([]model.FacetCount, 0, len(model.DefaultTopics))
	for _, topic := range model.DefaultTopics {
		result = append(result, model.FacetCount{
			Key:   topic.Code,
			Label: topic.Label,
			Count: counts[topic.Code],
		})
	}
	return result, nil
}

func (s *SQLiteStore) platformFacetCounts(ctx context.Context) ([]model.FacetCount, error) {
	counts, err := s.codeCounts(ctx, "repo_platform_codes")
	if err != nil {
		return nil, err
	}
	result := make([]model.FacetCount, 0, len(model.DefaultPlatforms))
	for _, platform := range model.DefaultPlatforms {
		result = append(result, model.FacetCount{
			Key:        platform.Code,
			Label:      platform.Label,
			Count:      counts[platform.Code],
			SystemName: platform.SystemName,
		})
	}
	return result, nil
}

func (s *SQLiteStore) codeCounts(ctx context.Context, table string) (map[string]int, error) {
	if table != "repo_topic_codes" && table != "repo_platform_codes" {
		return nil, fmt.Errorf("unsupported code table %s", table)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT code, COUNT(*) AS count
		FROM `+table+`
		GROUP BY code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var code string
		var count int
		if err := rows.Scan(&code, &count); err != nil {
			return nil, err
		}
		result[code] = count
	}
	return result, rows.Err()
}

func languageStatsToFacets(languages []model.LanguageStat) []model.FacetCount {
	result := make([]model.FacetCount, 0, len(languages))
	for _, language := range languages {
		result = append(result, model.FacetCount{
			Key:   language.Key,
			Label: language.Label,
			Count: language.Count,
		})
	}
	return result
}

func replaceCodes(ctx context.Context, tx *sql.Tx, table string, repoID int64, codes []string) error {
	if table != "repo_topic_codes" && table != "repo_platform_codes" {
		return fmt.Errorf("unsupported code table %s", table)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE gh_repo_id = ?", repoID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO "+table+"(gh_repo_id, code) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	seen := map[string]bool{}
	for _, code := range codes {
		code = strings.TrimSpace(strings.ToLower(code))
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		if _, err := stmt.ExecContext(ctx, repoID, code); err != nil {
			return err
		}
	}
	return nil
}

func buildFilterWhere(filters QueryFilters) (string, []interface{}) {
	var clauses []string
	args := []interface{}{}
	appendCategoryEligibility(&clauses, &args, filters)
	if filters.Language == model.UncategorizedLanguageKey {
		clauses = append(clauses, "(r.language IS NULL OR r.language = '')")
	} else if filters.Language != "" && filters.Language != model.AllBucket {
		clauses = append(clauses, "r.language = ?")
		args = append(args, filters.Language)
	}
	if filters.Platform != "" && filters.Platform != model.AllBucket {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM repo_platform_codes rpc WHERE rpc.gh_repo_id = r.gh_repo_id AND rpc.code = ?)")
		args = append(args, filters.Platform)
	}
	if filters.Topic != "" && filters.Topic != model.AllBucket {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM repo_topic_codes rtc WHERE rtc.gh_repo_id = r.gh_repo_id AND rtc.code = ?)")
		args = append(args, filters.Topic)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func appendCategoryEligibility(clauses *[]string, args *[]interface{}, filters QueryFilters) {
	switch filters.Category {
	case "most-popular":
		*clauses = append(*clauses,
			"r.is_archived = 0",
			"r.is_fork = 0",
			"(r.stars >= 1000 OR r.popularity_score >= 0.65)",
		)
	case "new-releases":
		minReleaseAt := time.Now().UTC().AddDate(0, 0, -180).Format(time.RFC3339)
		if strings.TrimSpace(filters.MinReleaseAt) != "" {
			minReleaseAt = strings.TrimSpace(filters.MinReleaseAt)
		}
		*clauses = append(*clauses,
			"r.is_archived = 0",
			"r.is_fork = 0",
			"r.latest_release_at IS NOT NULL",
			"r.latest_release_at >= ?",
			`EXISTS (
				SELECT 1 FROM repo_releases rr
				WHERE rr.gh_repo_id = r.gh_repo_id
				  AND rr.draft = 0
				  AND rr.prerelease = 0
				  AND rr.published_at >= ?
				  AND COALESCE(rr.assets_json, '[]') <> '[]'
			)`,
		)
		*args = append(*args, minReleaseAt, minReleaseAt)
	}
}

func (s *SQLiteStore) attachCategoryMemberships(ctx context.Context, items []model.DiscoveryItem) ([]model.DiscoveryItem, error) {
	if len(items) == 0 {
		return items, nil
	}
	indexByRepoID := make(map[int64]int, len(items))
	for index := range items {
		indexByRepoID[items[index].RepoID] = index
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT gh_repo_id, category, rank
		FROM category_rankings
		WHERE bucket = ?
		ORDER BY gh_repo_id ASC, category ASC
	`, model.AllBucket)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var repoID int64
		var category string
		var rank int
		if err := rows.Scan(&repoID, &category, &rank); err != nil {
			return nil, err
		}
		index, ok := indexByRepoID[repoID]
		if !ok {
			continue
		}
		mode := categoryMode(category)
		if mode == "" {
			continue
		}
		items[index].Categories = append(items[index].Categories, mode)
		if items[index].CategoryRanks == nil {
			items[index].CategoryRanks = map[string]int{}
		}
		items[index].CategoryRanks[mode] = rank
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range items {
		normalizeDiscoveryItemSlices(&items[index])
	}
	return items, nil
}

func categoryMode(category string) string {
	switch category {
	case "most-popular":
		return "popular"
	case "new-releases":
		return "new_releases"
	default:
		// 旧库里可能残留 category='trending' 的候选排名。Starcat 正式 discovery
		// bulk 只暴露热门 / 新发布归属，未知 category 必须忽略，避免污染本地缓存。
		return ""
	}
}

func scanItems(rows *sql.Rows) ([]model.DiscoveryItem, error) {
	items := make([]model.DiscoveryItem, 0)
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanItem(rows *sql.Rows) (model.DiscoveryItem, error) {
	var item model.DiscoveryItem
	var topicsJSON, platformsJSON string
	var description, homepage, language, ownerAvatar, defaultBranch, licenseSpdx sql.NullString
	var pushedAt, updatedAt, createdAt, latestReleaseTag, latestReleaseAt, latestReleaseURL sql.NullString
	var indexedAt string
	var enrichedAt sql.NullString
	var archived, fork int
	var trendingScore, popularityScore, releaseScore, discoveryScore, searchScore float64
	if err := rows.Scan(
		&item.RepoID, &item.Owner, &item.Name, &item.FullName,
		&description, &homepage, &language, &item.Stars, &item.Forks,
		&item.Watchers, &item.Subscribers, &item.OpenIssues, &ownerAvatar,
		&defaultBranch, &licenseSpdx, &topicsJSON, &platformsJSON,
		&pushedAt, &updatedAt, &createdAt, &archived, &fork,
		&latestReleaseTag, &latestReleaseAt, &latestReleaseURL, &item.ReleaseDownloadCount,
		&trendingScore, &popularityScore, &releaseScore, &discoveryScore, &searchScore,
		&indexedAt, &enrichedAt, &item.Rank, &item.Score,
	); err != nil {
		return model.DiscoveryItem{}, err
	}
	item.Description = nullString(description)
	item.Homepage = nullString(homepage)
	item.Language = nullString(language)
	item.OwnerAvatar = nullString(ownerAvatar)
	item.DefaultBranch = nullString(defaultBranch)
	item.LicenseSpdx = nullString(licenseSpdx)
	item.PushedAt = nullString(pushedAt)
	item.UpdatedAt = nullString(updatedAt)
	item.CreatedAt = nullString(createdAt)
	item.LatestReleaseTag = nullString(latestReleaseTag)
	item.LatestReleaseAt = nullString(latestReleaseAt)
	item.LatestReleaseURL = nullString(latestReleaseURL)
	item.IsArchived = archived == 1
	item.IsFork = fork == 1
	item.TrendingScore = trendingScore
	item.PopularityScore = popularityScore
	item.ReleaseScore = releaseScore
	item.DiscoveryScore = discoveryScore
	item.SearchScore = searchScore
	_ = json.Unmarshal([]byte(topicsJSON), &item.Topics)
	_ = json.Unmarshal([]byte(platformsJSON), &item.Platforms)
	item.Signals = buildSignals(item)
	item.Reasons = buildReasons(item)
	normalizeDiscoveryItemSlices(&item)
	return item, nil
}

func normalizeDiscoveryItemSlices(item *model.DiscoveryItem) {
	if item.Topics == nil {
		item.Topics = []string{}
	}
	if item.Platforms == nil {
		item.Platforms = []string{}
	}
	if item.Reasons == nil {
		item.Reasons = []string{}
	}
	if item.Signals == nil {
		item.Signals = []model.Signal{}
	}
	if item.Categories == nil {
		item.Categories = []string{}
	}
	if item.CategoryRanks == nil {
		item.CategoryRanks = map[string]int{}
	}
}

func buildSignals(item model.DiscoveryItem) []model.Signal {
	var signals []model.Signal
	if item.Stars > 0 {
		signals = append(signals, model.Signal{Code: "stars", Label: "Stars", Value: fmt.Sprintf("%d", item.Stars)})
	}
	if item.ReleaseDownloadCount > 0 {
		signals = append(signals, model.Signal{Code: "downloads", Label: "Downloads", Value: fmt.Sprintf("%d", item.ReleaseDownloadCount)})
	}
	if item.LatestReleaseAt != "" {
		signals = append(signals, model.Signal{Code: "release", Label: "Recently released", Value: item.LatestReleaseAt})
	}
	return signals
}

func buildReasons(item model.DiscoveryItem) []string {
	var reasons []string
	if item.LatestReleaseAt != "" {
		reasons = append(reasons, "recent_release")
	}
	if item.ReleaseDownloadCount > 0 {
		reasons = append(reasons, "release_downloads")
	}
	if item.Stars >= 1000 {
		reasons = append(reasons, "popular")
	}
	return reasons
}

func (s *SQLiteStore) count(ctx context.Context, query string, args ...interface{}) (int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func normalizePage(page, limit int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	return page, limit
}

func makePage[T any](items []T, total, page, limit int) model.Page[T] {
	if items == nil {
		items = make([]T, 0)
	}
	var next *int
	if page*limit < total {
		value := page + 1
		next = &value
	}
	return model.Page[T]{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: limit,
		NextPage: next,
	}
}

func allowedScoreColumn(column string) bool {
	switch column {
	case "discovery_score", "popularity_score", "release_score", "trending_score", "search_score":
		return true
	default:
		return false
	}
}

func orderClauseForSortKey(sortKey string) (string, bool) {
	switch sortKey {
	case "name_asc":
		return "lower(r.full_name) ASC, r.gh_repo_id DESC", true
	case "name_desc":
		return "lower(r.full_name) DESC, r.gh_repo_id DESC", true
	case "stars_desc":
		return "r.stars DESC, r.gh_repo_id DESC", true
	case "stars_asc":
		return "r.stars ASC, r.gh_repo_id DESC", true
	case "updated_desc":
		return "COALESCE(r.updated_at, r.pushed_at, r.created_at, '') DESC, r.gh_repo_id DESC", true
	case "updated_asc":
		return "COALESCE(r.updated_at, r.pushed_at, r.created_at, '') ASC, r.gh_repo_id DESC", true
	case "created_desc":
		return "COALESCE(r.created_at, '') DESC, r.gh_repo_id DESC", true
	case "created_asc":
		return "COALESCE(r.created_at, '') ASC, r.gh_repo_id DESC", true
	case "release_desc":
		return "COALESCE(r.latest_release_at, '') DESC, r.release_score DESC, r.gh_repo_id DESC", true
	case "release_asc":
		return "COALESCE(r.latest_release_at, '') ASC, r.release_score DESC, r.gh_repo_id DESC", true
	default:
		return "", false
	}
}

func nullable(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func timeString(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}

func timePtrString(value *time.Time) interface{} {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}
