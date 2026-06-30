package store

import (
	"context"
	"database/sql"
	"log"
)

// createSchema 初始化 discovery catalog。
//
// 本项目尚未上线，schema 直接按当前设计创建，不保留旧字段兼容或迁移逻辑。
func createSchema(ctx context.Context, db *sql.DB) error {
	log.Println("[migrate] create discovery schema")
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS repos (
			gh_repo_id             INTEGER PRIMARY KEY,
			owner                  TEXT NOT NULL,
			name                   TEXT NOT NULL,
			full_name              TEXT NOT NULL UNIQUE,
			description            TEXT,
			homepage               TEXT,
			language               TEXT,
			stars                  INTEGER NOT NULL DEFAULT 0,
			forks                  INTEGER NOT NULL DEFAULT 0,
			watchers               INTEGER NOT NULL DEFAULT 0,
			subscribers            INTEGER NOT NULL DEFAULT 0,
			open_issues            INTEGER NOT NULL DEFAULT 0,
			owner_avatar           TEXT,
			default_branch         TEXT,
			license_spdx           TEXT,
			topics_json            TEXT NOT NULL DEFAULT '[]',
			platforms_json         TEXT NOT NULL DEFAULT '[]',
			pushed_at              TEXT,
			updated_at             TEXT,
			created_at             TEXT,
			is_archived            INTEGER NOT NULL DEFAULT 0,
			is_fork                INTEGER NOT NULL DEFAULT 0,
			latest_release_tag     TEXT,
			latest_release_at      TEXT,
			latest_release_url     TEXT,
			release_download_count INTEGER NOT NULL DEFAULT 0,
			trending_score         REAL NOT NULL DEFAULT 0,
			popularity_score       REAL NOT NULL DEFAULT 0,
			release_score          REAL NOT NULL DEFAULT 0,
			discovery_score        REAL NOT NULL DEFAULT 0,
			search_score           REAL NOT NULL DEFAULT 0,
			indexed_at             TEXT NOT NULL,
			enriched_at            TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_repos_language ON repos(language);
		CREATE INDEX IF NOT EXISTS idx_repos_popularity ON repos(popularity_score DESC, gh_repo_id DESC);
		CREATE INDEX IF NOT EXISTS idx_repos_release ON repos(latest_release_at DESC, release_score DESC, gh_repo_id DESC);
		CREATE INDEX IF NOT EXISTS idx_repos_discovery ON repos(discovery_score DESC, gh_repo_id DESC);
		CREATE INDEX IF NOT EXISTS idx_repos_trending ON repos(trending_score DESC, gh_repo_id DESC);
		CREATE INDEX IF NOT EXISTS idx_repos_search ON repos(search_score DESC, gh_repo_id DESC);
		CREATE INDEX IF NOT EXISTS idx_repos_stars ON repos(stars DESC, gh_repo_id DESC);
		CREATE INDEX IF NOT EXISTS idx_repos_updated ON repos(updated_at DESC, pushed_at DESC, created_at DESC, gh_repo_id DESC);
		CREATE INDEX IF NOT EXISTS idx_repos_created ON repos(created_at DESC, gh_repo_id DESC);
		CREATE INDEX IF NOT EXISTS idx_repos_full_name_lower ON repos(lower(full_name), gh_repo_id DESC);

		CREATE TABLE IF NOT EXISTS repo_releases (
			gh_repo_id     INTEGER NOT NULL REFERENCES repos(gh_repo_id) ON DELETE CASCADE,
			tag_name       TEXT NOT NULL,
			name           TEXT,
			html_url       TEXT,
			published_at   TEXT NOT NULL,
			draft          INTEGER NOT NULL DEFAULT 0,
			prerelease     INTEGER NOT NULL DEFAULT 0,
			download_count INTEGER NOT NULL DEFAULT 0,
			assets_json    TEXT NOT NULL DEFAULT '[]',
			indexed_at     TEXT NOT NULL,
			PRIMARY KEY (gh_repo_id, tag_name)
		);

		CREATE INDEX IF NOT EXISTS idx_repo_releases_repo
			ON repo_releases(gh_repo_id, published_at DESC);
		CREATE INDEX IF NOT EXISTS idx_repo_releases_stable
			ON repo_releases(published_at DESC, download_count DESC)
			WHERE draft = 0 AND prerelease = 0;

		CREATE TABLE IF NOT EXISTS repo_topic_codes (
			gh_repo_id INTEGER NOT NULL REFERENCES repos(gh_repo_id) ON DELETE CASCADE,
			code       TEXT NOT NULL,
			PRIMARY KEY (gh_repo_id, code)
		);

		CREATE INDEX IF NOT EXISTS idx_repo_topic_codes_code ON repo_topic_codes(code, gh_repo_id);

		CREATE TABLE IF NOT EXISTS repo_platform_codes (
			gh_repo_id INTEGER NOT NULL REFERENCES repos(gh_repo_id) ON DELETE CASCADE,
			code       TEXT NOT NULL,
			PRIMARY KEY (gh_repo_id, code)
		);

		CREATE INDEX IF NOT EXISTS idx_repo_platform_codes_code ON repo_platform_codes(code, gh_repo_id);

		CREATE TABLE IF NOT EXISTS category_rankings (
			category     TEXT NOT NULL,
			bucket       TEXT NOT NULL,
			gh_repo_id   INTEGER NOT NULL REFERENCES repos(gh_repo_id) ON DELETE CASCADE,
			rank         INTEGER NOT NULL,
			score        REAL NOT NULL DEFAULT 0,
			generated_at TEXT NOT NULL,
			PRIMARY KEY (category, bucket, gh_repo_id)
		);

		CREATE INDEX IF NOT EXISTS idx_category_rankings_lookup
			ON category_rankings(category, bucket, rank);
		CREATE INDEX IF NOT EXISTS idx_category_rankings_bucket_repo
			ON category_rankings(bucket, gh_repo_id);

		CREATE TABLE IF NOT EXISTS topic_rankings (
			topic        TEXT NOT NULL,
			platform     TEXT NOT NULL,
			gh_repo_id   INTEGER NOT NULL REFERENCES repos(gh_repo_id) ON DELETE CASCADE,
			rank         INTEGER NOT NULL,
			score        REAL NOT NULL DEFAULT 0,
			generated_at TEXT NOT NULL,
			PRIMARY KEY (topic, platform, gh_repo_id)
		);

		CREATE INDEX IF NOT EXISTS idx_topic_rankings_lookup
			ON topic_rankings(topic, platform, rank);

		CREATE TABLE IF NOT EXISTS repo_daily_snapshots (
			date                   TEXT NOT NULL,
			gh_repo_id             INTEGER NOT NULL REFERENCES repos(gh_repo_id) ON DELETE CASCADE,
			stars                  INTEGER NOT NULL DEFAULT 0,
			forks                  INTEGER NOT NULL DEFAULT 0,
			watchers               INTEGER NOT NULL DEFAULT 0,
			release_download_count INTEGER NOT NULL DEFAULT 0,
			captured_at            TEXT NOT NULL,
			PRIMARY KEY (date, gh_repo_id)
		);

		CREATE INDEX IF NOT EXISTS idx_repo_daily_snapshots_repo
			ON repo_daily_snapshots(gh_repo_id, date DESC);

		CREATE TABLE IF NOT EXISTS feed_exposure (
			feed_key       TEXT NOT NULL,
			gh_repo_id     INTEGER NOT NULL REFERENCES repos(gh_repo_id) ON DELETE CASCADE,
			exposed_at     TEXT NOT NULL,
			exposure_count INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (feed_key, gh_repo_id)
		);

		CREATE INDEX IF NOT EXISTS idx_feed_exposure_lookup
			ON feed_exposure(feed_key, exposed_at DESC);

		CREATE TABLE IF NOT EXISTS sync_runs (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			mode           TEXT NOT NULL,
			status         TEXT NOT NULL,
			started_at     TEXT NOT NULL,
			finished_at    TEXT,
			repos_seen     INTEGER NOT NULL DEFAULT 0,
			repos_upserted INTEGER NOT NULL DEFAULT 0,
			error_message   TEXT
		);
	`)
	return err
}
