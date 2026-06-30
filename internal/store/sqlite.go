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
	var result []model.LanguageStat
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

// QueryFilters 是读取接口支持的通用过滤条件。
type QueryFilters struct {
	Language string
	Platform string
	Topic    string
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

func scanItems(rows *sql.Rows) ([]model.DiscoveryItem, error) {
	var items []model.DiscoveryItem
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
	if err := rows.Scan(
		&item.RepoID, &item.Owner, &item.Name, &item.FullName,
		&description, &homepage, &language, &item.Stars, &item.Forks,
		&item.Watchers, &item.Subscribers, &item.OpenIssues, &ownerAvatar,
		&defaultBranch, &licenseSpdx, &topicsJSON, &platformsJSON,
		&pushedAt, &updatedAt, &createdAt, &archived, &fork,
		&latestReleaseTag, &latestReleaseAt, &latestReleaseURL, &item.ReleaseDownloadCount,
		new(float64), new(float64), new(float64), new(float64), new(float64),
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
	_ = json.Unmarshal([]byte(topicsJSON), &item.Topics)
	_ = json.Unmarshal([]byte(platformsJSON), &item.Platforms)
	item.Signals = buildSignals(item)
	item.Reasons = buildReasons(item)
	return item, nil
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
