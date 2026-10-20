package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
	_ "modernc.org/sqlite"
)

// 真实旧库包含审核 CHECK 约束；追加迁移必须保留发布关系、稳定 ID 和采集游标，并且只执行一次。
func TestLinuxDoAutomaticMigrationPreservesPublishedDataAndResetsLegacyQueue(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-linuxdo.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		CREATE TABLE linuxdo_state(id INTEGER PRIMARY KEY,revision INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL DEFAULT '',etag TEXT NOT NULL DEFAULT '',last_modified TEXT NOT NULL DEFAULT '',checked_at TEXT NOT NULL DEFAULT '');
		INSERT INTO linuxdo_state VALUES(1,17,'2026-10-14T00:00:00Z','legacy-feed','','2026-10-14T00:00:00Z');
		CREATE TABLE linuxdo_topics(id INTEGER PRIMARY KEY,url TEXT NOT NULL,title TEXT NOT NULL,published_at TEXT NOT NULL,content_hash TEXT NOT NULL,last_seen_at TEXT NOT NULL);
		INSERT INTO linuxdo_topics VALUES(42,'https://linux.do/t/topic/42','Tool','2026-10-14T00:00:00Z','hash','2026-10-14T00:00:00Z');
		CREATE TABLE linuxdo_candidates(id INTEGER PRIMARY KEY AUTOINCREMENT,topic_id INTEGER NOT NULL REFERENCES linuxdo_topics(id),full_name TEXT NOT NULL COLLATE NOCASE,content_hash TEXT NOT NULL,context TEXT NOT NULL,explicit_promotion INTEGER NOT NULL,state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','review','published','rejected','withdrawn')),reason TEXT NOT NULL DEFAULT '',gh_repo_id INTEGER NOT NULL DEFAULT 0,license TEXT NOT NULL DEFAULT '',UNIQUE(topic_id,full_name));
		INSERT INTO linuxdo_candidates VALUES(7,42,'owner/pending','hash','GitHub: owner/pending',0,'review','promotion role requires review',0,'NOASSERTION');
		INSERT INTO linuxdo_candidates VALUES(8,42,'owner/published','hash','项目地址',1,'published','old publication',123,'MIT');
		CREATE TABLE linuxdo_repo_topics(gh_repo_id INTEGER NOT NULL REFERENCES repos(gh_repo_id),topic_id INTEGER NOT NULL REFERENCES linuxdo_topics(id),verified_at TEXT NOT NULL,PRIMARY KEY(gh_repo_id,topic_id));
		INSERT INTO linuxdo_repo_topics VALUES(123,42,'2026-10-14T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.UpsertAwesomeRepositories(ctx, []model.Repository{{GhRepoID: 123, Owner: "owner", Name: "published", FullName: "owner/published", IndexedAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	candidates, err := s.LinuxDoCandidates(ctx, 0)
	if err != nil || len(candidates) != 2 || candidates[0].ID != 7 || candidates[0].State != "pending" || candidates[0].License != "NOASSERTION" || candidates[1].ID != 8 || candidates[1].State != "published" {
		t.Fatalf("migration lost candidates: %+v %v", candidates, err)
	}
	snapshot, err := s.LinuxDoSnapshot(ctx)
	if err != nil || snapshot.Revision != 17 || len(snapshot.Repositories) != 1 || len(snapshot.Origins) != 1 {
		t.Fatalf("migration changed publication: %+v %v", snapshot, err)
	}
	state, err := s.LinuxDoFeedState(ctx)
	if err != nil || state.ETag != "legacy-feed" || state.RuleVersion != 0 {
		t.Fatalf("migration changed RSS checkpoint: %+v %v", state, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE linuxdo_candidates SET state='review' WHERE id=7`); err == nil {
		t.Fatal("new schema still accepts manual review state")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE linuxdo_candidates SET state='skipped',rule_version=? WHERE id=7`, model.LinuxDoRuleVersion); err != nil {
		t.Fatal(err)
	}
	if err := ensureLinuxDoSchema(ctx, s.db); err != nil {
		t.Fatal(err)
	}
	candidates, err = s.LinuxDoCandidates(ctx, 0)
	if err != nil || candidates[0].State != "skipped" || candidates[0].RuleVersion != model.LinuxDoRuleVersion {
		t.Fatalf("migration repeated after restart: %+v %v", candidates, err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO linuxdo_candidates(topic_id,full_name,content_hash,context,explicit_promotion) VALUES(42,'owner/new','hash','GitHub: owner/new',1)`); err != nil {
		t.Fatal(err)
	}
	candidates, err = s.LinuxDoCandidates(ctx, 8)
	if err != nil || len(candidates) != 1 || candidates[0].ID <= 8 {
		t.Fatalf("migration reused candidate IDs: %+v %v", candidates, err)
	}
}

func TestDiscoveryCatalogMembershipUpgradeBackfillsOnlyOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy-discovery.db")
	store, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("create baseline database: %v", err)
	}
	now := time.Now().UTC()
	if err := store.UpsertRepo(ctx, model.Repository{
		GhRepoID: 1, Owner: "discovery", Name: "candidate", FullName: "discovery/candidate",
		DiscoveryScore: 0.8, IndexedAt: now,
	}); err != nil {
		t.Fatalf("seed Discovery repository: %v", err)
	}
	if err := store.UpsertAwesomeRepositories(ctx, []model.Repository{{
		GhRepoID: 2, Owner: "awesome", Name: "only", FullName: "awesome/only", IndexedAt: now,
	}}); err != nil {
		t.Fatalf("seed Awesome repository: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DROP TABLE discovery_catalog_repos`); err != nil {
		t.Fatalf("remove post-upgrade membership table: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM store_migrations WHERE name = ?`, discoveryCatalogMembershipMigration); err != nil {
		t.Fatalf("remove migration marker: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	reopened, err := NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("upgrade legacy database: %v", err)
	}
	var repoID int64
	if err := reopened.db.QueryRowContext(ctx, `SELECT gh_repo_id FROM discovery_catalog_repos`).Scan(&repoID); err != nil {
		t.Fatalf("read backfilled membership: %v", err)
	}
	if repoID != 1 {
		t.Fatalf("backfilled repo id = %d, want Discovery candidate 1", repoID)
	}
	if _, err := reopened.db.ExecContext(ctx, `DELETE FROM discovery_catalog_repos`); err != nil {
		t.Fatalf("clear membership after migration: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close upgraded database: %v", err)
	}

	reopened, err = NewSQLiteStore(ctx, path)
	if err != nil {
		t.Fatalf("reopen upgraded database: %v", err)
	}
	defer reopened.Close()
	var count int
	if err := reopened.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM discovery_catalog_repos`).Scan(&count); err != nil {
		t.Fatalf("count memberships after restart: %v", err)
	}
	if count != 0 {
		t.Fatalf("one-time backfill repeated after restart: count=%d", count)
	}
}

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
