package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

var (
	// ErrAwesomeSourceNotFound 表示来源不存在，handler 会稳定映射为 404。
	ErrAwesomeSourceNotFound = errors.New("awesome source not found")
	// ErrAwesomeSourceRevisionConflict 表示运营台提交了过期 revision。
	ErrAwesomeSourceRevisionConflict = errors.New("awesome source revision conflict")
	// ErrAwesomeSyncInProgress 表示同一来源已有 queued/running 任务。
	ErrAwesomeSyncInProgress = errors.New("awesome sync already in progress")
)

// CreateAwesomeSource 新建草稿来源；GitHub 核验由 service 在进入 Store 前完成。
func (s *SQLiteStore) CreateAwesomeSource(ctx context.Context, source model.AwesomeSource) (model.AwesomeSource, error) {
	now := time.Now().UTC()
	source.Status = model.AwesomeSourceDraft
	source.Revision = 1
	source.CreatedAt = now
	source.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO awesome_sources (
			id, repo_full_name, display_name, image_url, summary_zh, summary_en,
			featured, sort_order, status, revision, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, source.ID, source.RepoFullName, source.DisplayName, nullable(source.ImageURL),
		nullable(source.SummaryZH), nullable(source.SummaryEN), boolInt(source.Featured),
		source.SortOrder, source.Status, source.Revision, timeString(now), timeString(now))
	if err != nil {
		return model.AwesomeSource{}, err
	}
	return s.GetAwesomeSource(ctx, source.ID)
}

// UpdateAwesomeSource 使用 revision 做乐观并发，避免旧运营表单覆盖较新的内容。
func (s *SQLiteStore) UpdateAwesomeSource(ctx context.Context, source model.AwesomeSource, expectedRevision int) (model.AwesomeSource, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE awesome_sources SET
			repo_full_name = ?, display_name = ?, image_url = ?, summary_zh = ?, summary_en = ?,
			featured = ?, sort_order = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND revision = ?
	`, source.RepoFullName, source.DisplayName, nullable(source.ImageURL), nullable(source.SummaryZH),
		nullable(source.SummaryEN), boolInt(source.Featured), source.SortOrder, timeString(now),
		source.ID, expectedRevision)
	if err != nil {
		return model.AwesomeSource{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return model.AwesomeSource{}, err
	}
	if rows == 0 {
		if _, getErr := s.GetAwesomeSource(ctx, source.ID); errors.Is(getErr, ErrAwesomeSourceNotFound) {
			return model.AwesomeSource{}, ErrAwesomeSourceNotFound
		}
		return model.AwesomeSource{}, ErrAwesomeSourceRevisionConflict
	}
	return s.GetAwesomeSource(ctx, source.ID)
}

// GetAwesomeSource 返回单一来源的完整运营状态。
func (s *SQLiteStore) GetAwesomeSource(ctx context.Context, id string) (model.AwesomeSource, error) {
	row := s.db.QueryRowContext(ctx, awesomeSourceSelect+` WHERE id = ?`, id)
	source, err := scanAwesomeSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AwesomeSource{}, ErrAwesomeSourceNotFound
	}
	return source, err
}

// ListAwesomeSources 返回全部运营来源，按稳定顺序排列。
func (s *SQLiteStore) ListAwesomeSources(ctx context.Context) ([]model.AwesomeSource, error) {
	return s.listAwesomeSources(ctx, "", nil)
}

// ListPublishedAwesomeSources 只返回客户端可见来源。
func (s *SQLiteStore) ListPublishedAwesomeSources(ctx context.Context) ([]model.AwesomeSource, error) {
	return s.listAwesomeSources(ctx, " WHERE status = ?", []any{model.AwesomeSourcePublished})
}

func (s *SQLiteStore) listAwesomeSources(ctx context.Context, where string, args []any) ([]model.AwesomeSource, error) {
	rows, err := s.db.QueryContext(ctx, awesomeSourceSelect+where+` ORDER BY sort_order ASC, id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.AwesomeSource, 0)
	for rows.Next() {
		source, scanErr := scanAwesomeSource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, source)
	}
	return result, rows.Err()
}

// SetAwesomeSourceStatus 执行已由 service 校验过的状态迁移。
func (s *SQLiteStore) SetAwesomeSourceStatus(ctx context.Context, id string, status model.AwesomeSourceStatus) (model.AwesomeSource, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE awesome_sources
		SET status = ?, revision = revision + 1, updated_at = ?
		WHERE id = ?
	`, status, timeString(time.Now().UTC()), id)
	if err != nil {
		return model.AwesomeSource{}, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return model.AwesomeSource{}, err
	}
	if rows == 0 {
		return model.AwesomeSource{}, ErrAwesomeSourceNotFound
	}
	return s.GetAwesomeSource(ctx, id)
}

// StartAwesomeSyncRun 原子创建 active run；partial unique index 是并发下的最终防线。
func (s *SQLiteStore) StartAwesomeSyncRun(ctx context.Context, run model.AwesomeSyncRun) (model.AwesomeSyncRun, error) {
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.Status == "" {
		run.Status = "queued"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO awesome_sync_runs (id, source_id, status, trigger_kind, started_at)
		VALUES (?, ?, ?, ?, ?)
	`, run.ID, run.SourceID, run.Status, run.Trigger, timeString(run.StartedAt))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
			return model.AwesomeSyncRun{}, ErrAwesomeSyncInProgress
		}
		return model.AwesomeSyncRun{}, err
	}
	return run, nil
}

// FinishAwesomeSyncRun 写入成功或失败统计；错误文本必须由 service 先脱敏和限长。
func (s *SQLiteStore) FinishAwesomeSyncRun(ctx context.Context, run model.AwesomeSyncRun) error {
	finishedAt := time.Now().UTC()
	if run.FinishedAt != nil {
		finishedAt = run.FinishedAt.UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE awesome_sync_runs SET
			status = ?, readme_sha = ?, github_count = ?, external_count = ?,
			invalid_count = ?, duplicate_count = ?, error_code = ?, error_message = ?, finished_at = ?
		WHERE id = ?
	`, run.Status, nullable(run.ReadmeSHA), run.GitHubCount, run.ExternalCount,
		run.InvalidCount, run.DuplicateCount, nullable(run.ErrorCode), nullable(run.ErrorMessage),
		timeString(finishedAt), run.ID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("awesome sync run %q not found", run.ID)
	}
	return nil
}

// ListAwesomeSyncRuns 返回最近的任务，运营台刷新后可恢复观察状态。
func (s *SQLiteStore) ListAwesomeSyncRuns(ctx context.Context, sourceID string, limit int) ([]model.AwesomeSyncRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_id, status, trigger_kind, readme_sha, github_count, external_count,
		       invalid_count, duplicate_count, error_code, error_message, started_at, finished_at
		FROM awesome_sync_runs WHERE source_id = ? ORDER BY started_at DESC LIMIT ?
	`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]model.AwesomeSyncRun, 0)
	for rows.Next() {
		run, scanErr := scanAwesomeSyncRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

const awesomeSourceSelect = `
	SELECT id, repo_full_name, display_name, image_url, summary_zh, summary_en,
	       featured, sort_order, status, revision, default_branch, readme_path,
	       last_successful_sha, github_repo_count, external_entry_count,
	       last_synced_at, created_at, updated_at
	FROM awesome_sources`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAwesomeSource(row rowScanner) (model.AwesomeSource, error) {
	var source model.AwesomeSource
	var imageURL, summaryZH, summaryEN, defaultBranch, readmePath, sha sql.NullString
	var lastSyncedAt sql.NullString
	var featured int
	var createdAt, updatedAt string
	err := row.Scan(&source.ID, &source.RepoFullName, &source.DisplayName, &imageURL, &summaryZH, &summaryEN,
		&featured, &source.SortOrder, &source.Status, &source.Revision, &defaultBranch, &readmePath,
		&sha, &source.GitHubRepoCount, &source.ExternalEntryCount, &lastSyncedAt, &createdAt, &updatedAt)
	if err != nil {
		return model.AwesomeSource{}, err
	}
	source.ImageURL = imageURL.String
	source.SummaryZH = summaryZH.String
	source.SummaryEN = summaryEN.String
	source.Featured = featured != 0
	source.DefaultBranch = defaultBranch.String
	source.ReadmePath = readmePath.String
	source.LastSuccessfulSHA = sha.String
	source.RepoURL = "https://github.com/" + source.RepoFullName
	source.LastSyncedAt = parseOptionalTime(lastSyncedAt)
	source.CreatedAt = parseStoredTime(createdAt)
	source.UpdatedAt = parseStoredTime(updatedAt)
	return source, nil
}

func scanAwesomeSyncRun(row rowScanner) (model.AwesomeSyncRun, error) {
	var run model.AwesomeSyncRun
	var readmeSHA, errorCode, errorMessage, finishedAt sql.NullString
	var startedAt string
	err := row.Scan(&run.ID, &run.SourceID, &run.Status, &run.Trigger, &readmeSHA,
		&run.GitHubCount, &run.ExternalCount, &run.InvalidCount, &run.DuplicateCount,
		&errorCode, &errorMessage, &startedAt, &finishedAt)
	if err != nil {
		return model.AwesomeSyncRun{}, err
	}
	run.ReadmeSHA = readmeSHA.String
	run.ErrorCode = errorCode.String
	run.ErrorMessage = errorMessage.String
	run.StartedAt = parseStoredTime(startedAt)
	run.FinishedAt = parseOptionalTime(finishedAt)
	return run, nil
}

func parseStoredTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func parseOptionalTime(value sql.NullString) *time.Time {
	if !value.Valid || value.String == "" {
		return nil
	}
	parsed := parseStoredTime(value.String)
	return &parsed
}

// sectionPathJSON 保持 Store 写入时只接受 JSON 数组，避免调用方拼接 SQL 文本。
func sectionPathJSON(path []string) (string, error) {
	if path == nil {
		path = []string{}
	}
	encoded, err := json.Marshal(path)
	return string(encoded), err
}
