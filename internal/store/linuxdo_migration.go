// Linux.do 使用追加迁移，不修改正式版已有表结构与目录归属。
package store

import (
	"context"
	"database/sql"
)

// ensureLinuxDoSchema 在事务中追加独立来源表；可安全重入且保留已有用户服务数据。
func ensureLinuxDoSchema(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS linuxdo_state (
			id INTEGER PRIMARY KEY CHECK(id = 1), revision INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL DEFAULT '', etag TEXT NOT NULL DEFAULT '',
			last_modified TEXT NOT NULL DEFAULT '', checked_at TEXT NOT NULL DEFAULT ''
		);
		INSERT OR IGNORE INTO linuxdo_state(id) VALUES (1);
		CREATE TABLE IF NOT EXISTS linuxdo_topics (
			id INTEGER PRIMARY KEY, url TEXT NOT NULL, title TEXT NOT NULL,
			published_at TEXT NOT NULL, content_hash TEXT NOT NULL, last_seen_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS linuxdo_candidates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			topic_id INTEGER NOT NULL REFERENCES linuxdo_topics(id), full_name TEXT NOT NULL COLLATE NOCASE,
			content_hash TEXT NOT NULL, context TEXT NOT NULL, explicit_promotion INTEGER NOT NULL,
			state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','review','published','rejected','withdrawn')),
			reason TEXT NOT NULL DEFAULT '', gh_repo_id INTEGER NOT NULL DEFAULT 0, license TEXT NOT NULL DEFAULT '',
			UNIQUE(topic_id, full_name)
		);
		CREATE INDEX IF NOT EXISTS idx_linuxdo_candidates_queue ON linuxdo_candidates(state, id);
		CREATE TABLE IF NOT EXISTS linuxdo_repo_topics (
			gh_repo_id INTEGER NOT NULL REFERENCES repos(gh_repo_id),
			topic_id INTEGER NOT NULL REFERENCES linuxdo_topics(id),
			verified_at TEXT NOT NULL, PRIMARY KEY(gh_repo_id, topic_id)
		);
		INSERT OR IGNORE INTO store_migrations(name, applied_at)
		VALUES ('2026-10-15-linuxdo-provenance', strftime('%Y-%m-%dT%H:%M:%SZ','now'));
	`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
