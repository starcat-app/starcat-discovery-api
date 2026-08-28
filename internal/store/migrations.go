package store

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
)

// createSchema 初始化 discovery catalog；CREATE TABLE 负责新库，后续 ensure 方法负责
// 已上线 volume 的追加列，禁止要求生产环境删除数据库重建。
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

		CREATE TABLE IF NOT EXISTS repo_star_history_cache (
			gh_repo_id      INTEGER PRIMARY KEY,
			full_name       TEXT NOT NULL,
			current_stars   INTEGER NOT NULL DEFAULT 0,
			status          TEXT NOT NULL CHECK (status IN ('building', 'ready', 'failed')),
			coverage_start  TEXT,
			points_json     TEXT NOT NULL DEFAULT '[]',
			generated_at    TEXT,
			expires_at      TEXT NOT NULL,
			error_summary   TEXT,
			updated_at      TEXT NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_repo_star_history_cache_expiry
			ON repo_star_history_cache(status, expires_at);

		CREATE TABLE IF NOT EXISTS star_history_daily_budgets (
			budget_date    TEXT PRIMARY KEY,
			reserved_bytes INTEGER NOT NULL DEFAULT 0 CHECK (reserved_bytes >= 0),
			updated_at     TEXT NOT NULL
		);

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

		CREATE TABLE IF NOT EXISTS awesome_sources (
			id                    TEXT PRIMARY KEY,
			repo_full_name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
			display_name          TEXT NOT NULL,
			image_url             TEXT,
			summary_zh            TEXT,
			summary_en            TEXT,
			featured              INTEGER NOT NULL DEFAULT 0,
			sort_order            INTEGER NOT NULL DEFAULT 0,
			parser_profile        TEXT NOT NULL DEFAULT 'generic'
				CHECK (parser_profile IN ('generic', 'external_catalog', 'repository_resources')),
			status                TEXT NOT NULL DEFAULT 'draft'
				CHECK (status IN ('draft', 'ready', 'published', 'archived')),
			revision              INTEGER NOT NULL DEFAULT 1,
			default_branch        TEXT,
			readme_path           TEXT,
			last_successful_sha   TEXT,
			repo_metadata_version INTEGER NOT NULL DEFAULT 1,
			github_repo_count     INTEGER NOT NULL DEFAULT 0,
			external_entry_count  INTEGER NOT NULL DEFAULT 0,
			resource_entry_count  INTEGER NOT NULL DEFAULT 0,
			last_synced_at        TEXT,
			created_at            TEXT NOT NULL,
			updated_at            TEXT NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_awesome_sources_public
			ON awesome_sources(status, sort_order, id);

		CREATE TABLE IF NOT EXISTS awesome_source_languages (
			source_id TEXT NOT NULL REFERENCES awesome_sources(id) ON DELETE CASCADE,
			language  TEXT NOT NULL,
			bytes     INTEGER NOT NULL CHECK (bytes >= 0),
			PRIMARY KEY (source_id, language)
		);

		CREATE TABLE IF NOT EXISTS awesome_entries (
			source_id            TEXT NOT NULL REFERENCES awesome_sources(id) ON DELETE CASCADE,
			target_type          TEXT NOT NULL CHECK (target_type IN ('github_repo', 'external_resource', 'repository_resource')),
			target_key           TEXT NOT NULL,
			gh_repo_id           INTEGER REFERENCES repos(gh_repo_id) ON DELETE SET NULL,
			entry_title          TEXT NOT NULL,
			entry_description    TEXT,
			section_path_json    TEXT NOT NULL DEFAULT '[]',
			raw_url              TEXT NOT NULL,
			source_anchor_url    TEXT NOT NULL,
			entry_order          INTEGER NOT NULL,
			is_active            INTEGER NOT NULL DEFAULT 1,
			first_seen_sha       TEXT NOT NULL,
			last_seen_sha        TEXT NOT NULL,
			created_at           TEXT NOT NULL,
			updated_at           TEXT NOT NULL,
			PRIMARY KEY (source_id, target_key)
		);

		CREATE INDEX IF NOT EXISTS idx_awesome_entries_source_order
			ON awesome_entries(source_id, is_active, entry_order, target_key);
		CREATE INDEX IF NOT EXISTS idx_awesome_entries_repo
			ON awesome_entries(gh_repo_id, source_id) WHERE gh_repo_id IS NOT NULL;

		CREATE TABLE IF NOT EXISTS awesome_sync_runs (
			id                   TEXT PRIMARY KEY,
			source_id            TEXT NOT NULL REFERENCES awesome_sources(id) ON DELETE CASCADE,
			status               TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
			trigger_kind         TEXT NOT NULL CHECK (trigger_kind IN ('manual', 'scheduler')),
			readme_sha           TEXT,
			github_count         INTEGER NOT NULL DEFAULT 0,
			external_count       INTEGER NOT NULL DEFAULT 0,
			resource_count       INTEGER NOT NULL DEFAULT 0,
			extracted_count      INTEGER NOT NULL DEFAULT 0,
			ignored_count        INTEGER NOT NULL DEFAULT 0,
			invalid_count        INTEGER NOT NULL DEFAULT 0,
			duplicate_count      INTEGER NOT NULL DEFAULT 0,
			error_code           TEXT,
			error_message        TEXT,
			started_at           TEXT NOT NULL,
			finished_at          TEXT
		);

		CREATE INDEX IF NOT EXISTS idx_awesome_sync_runs_source
			ON awesome_sync_runs(source_id, started_at DESC);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_awesome_sync_runs_active
			ON awesome_sync_runs(source_id) WHERE status IN ('queued', 'running');
	`)
	if err != nil {
		return err
	}
	if err := ensureAwesomeRepositoryMetadataVersion(ctx, db); err != nil {
		return err
	}
	return ensureAwesomeParserSchema(ctx, db)
}

const awesomeRepositoryMetadataVersion = 1

// ensureAwesomeRepositoryMetadataVersion 只让旧 Awesome 快照重建一次。
//
// 新增的仓库事实来自 GitHub API，但历史来源在 README SHA 未变化时会走 no-op。
// 这里用持久版本标记清空一次旧 SHA，让既有发布来源在下一轮同步完整 enrich；版本写入后，
// 后续进程重启不会再次清空 SHA，也不会破坏正常的远端与本地缓存命中。
func ensureAwesomeRepositoryMetadataVersion(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(awesome_sources)`)
	if err != nil {
		return err
	}
	hasVersionColumn := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if name == "repo_metadata_version" {
			hasVersionColumn = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !hasVersionColumn {
		if _, err := db.ExecContext(ctx, `
			ALTER TABLE awesome_sources
			ADD COLUMN repo_metadata_version INTEGER NOT NULL DEFAULT 0
		`); err != nil {
			return err
		}
	}
	_, err = db.ExecContext(ctx, `
		UPDATE awesome_sources
		SET last_successful_sha = NULL,
		    repo_metadata_version = ?
		WHERE repo_metadata_version < ?
	`, awesomeRepositoryMetadataVersion, awesomeRepositoryMetadataVersion)
	return err
}

// ensureAwesomeParserSchema upgrades the already-deployed Awesome cache without deleting
// source metadata or snapshots. SQLite cannot alter a CHECK constraint in place, so the
// entries table is rebuilt once while preserving every existing row.
func ensureAwesomeParserSchema(ctx context.Context, db *sql.DB) error {
	for _, column := range []struct {
		table      string
		name       string
		definition string
	}{
		{"awesome_sources", "parser_profile", "TEXT NOT NULL DEFAULT 'generic'"},
		{"awesome_sources", "resource_entry_count", "INTEGER NOT NULL DEFAULT 0"},
		{"awesome_sync_runs", "resource_count", "INTEGER NOT NULL DEFAULT 0"},
		{"awesome_sync_runs", "extracted_count", "INTEGER NOT NULL DEFAULT 0"},
		{"awesome_sync_runs", "ignored_count", "INTEGER NOT NULL DEFAULT 0"},
	} {
		exists, err := sqliteColumnExists(ctx, db, column.table, column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := db.ExecContext(ctx, fmt.Sprintf(
				"ALTER TABLE %s ADD COLUMN %s %s", column.table, column.name, column.definition,
			)); err != nil {
				return err
			}
		}
	}

	var entriesSQL string
	if err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'awesome_entries'`).Scan(&entriesSQL); err != nil {
		return err
	}
	if strings.Contains(entriesSQL, "external_resource") {
		return nil
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer db.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE awesome_entries_v2 (
			source_id TEXT NOT NULL REFERENCES awesome_sources(id) ON DELETE CASCADE,
			target_type TEXT NOT NULL CHECK (target_type IN ('github_repo', 'external_resource', 'repository_resource')),
			target_key TEXT NOT NULL,
			gh_repo_id INTEGER REFERENCES repos(gh_repo_id) ON DELETE SET NULL,
			entry_title TEXT NOT NULL,
			entry_description TEXT,
			section_path_json TEXT NOT NULL DEFAULT '[]',
			raw_url TEXT NOT NULL,
			source_anchor_url TEXT NOT NULL,
			entry_order INTEGER NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 1,
			first_seen_sha TEXT NOT NULL,
			last_seen_sha TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (source_id, target_key)
		);
		INSERT INTO awesome_entries_v2
		SELECT source_id,
		       CASE WHEN target_type = 'external' THEN 'external_resource' ELSE target_type END,
		       target_key, gh_repo_id, entry_title, entry_description, section_path_json,
		       raw_url, source_anchor_url, entry_order, is_active, first_seen_sha, last_seen_sha,
		       created_at, updated_at
		FROM awesome_entries;
		DROP TABLE awesome_entries;
		ALTER TABLE awesome_entries_v2 RENAME TO awesome_entries;
		CREATE INDEX idx_awesome_entries_source_order
			ON awesome_entries(source_id, is_active, entry_order, target_key);
		CREATE INDEX idx_awesome_entries_repo
			ON awesome_entries(gh_repo_id, source_id) WHERE gh_repo_id IS NOT NULL;
	`); err != nil {
		return err
	}
	return tx.Commit()
}

func sqliteColumnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
