package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestAwesomeRepositoryMetadataUpgradeInvalidatesLegacySHAOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-awesome.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		CREATE TABLE awesome_sources (
			id TEXT PRIMARY KEY, repo_full_name TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL,
			image_url TEXT, summary_zh TEXT, summary_en TEXT, featured INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL, revision INTEGER NOT NULL DEFAULT 1,
			default_branch TEXT, readme_path TEXT, last_successful_sha TEXT,
			github_repo_count INTEGER NOT NULL DEFAULT 0, external_entry_count INTEGER NOT NULL DEFAULT 0,
			last_synced_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		);
		INSERT INTO awesome_sources (
			id, repo_full_name, display_name, status, last_successful_sha, created_at, updated_at
		) VALUES ('awesome-test', 'acme/awesome', 'Awesome Test', 'published', 'legacy-sha', '2026-08-24T00:00:00Z', '2026-08-24T00:00:00Z');
	`)
	if err != nil {
		t.Fatalf("seed legacy database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("upgrade legacy database: %v", err)
	}
	var version int
	var sha sql.NullString
	if err := store.db.QueryRowContext(ctx, `
		SELECT repo_metadata_version, last_successful_sha
		FROM awesome_sources WHERE id = 'awesome-test'
	`).Scan(&version, &sha); err != nil {
		t.Fatalf("read upgraded source: %v", err)
	}
	if version != awesomeRepositoryMetadataVersion || sha.Valid {
		t.Fatalf("upgraded source version=%d sha=%q", version, sha.String)
	}
	if _, err := store.db.ExecContext(ctx, `
		UPDATE awesome_sources SET last_successful_sha = 'current-sha' WHERE id = 'awesome-test'
	`); err != nil {
		t.Fatalf("write refreshed SHA: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close upgraded store: %v", err)
	}

	reopened, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen upgraded database: %v", err)
	}
	defer reopened.Close()
	if err := reopened.db.QueryRowContext(ctx, `
		SELECT last_successful_sha FROM awesome_sources WHERE id = 'awesome-test'
	`).Scan(&sha); err != nil {
		t.Fatalf("read reopened source: %v", err)
	}
	if !sha.Valid || sha.String != "current-sha" {
		t.Fatalf("metadata upgrade repeated after restart: sha=%q", sha.String)
	}
}

func TestAwesomeSourceLanguagesUpgradeRecreatesMissingTable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-awesome-languages.db")
	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("create baseline database: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DROP TABLE awesome_source_languages`); err != nil {
		t.Fatalf("drop source languages table: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close baseline database: %v", err)
	}

	reopened, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("upgrade legacy database: %v", err)
	}
	defer reopened.Close()
	var tableName string
	if err := reopened.db.QueryRowContext(ctx, `
		SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'awesome_source_languages'
	`).Scan(&tableName); err != nil {
		t.Fatalf("source languages table was not recreated: %v", err)
	}
}
