// Linux.do 候选队列和发布快照持久化；网络或解析失败不清空最后一次有效发布结果。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

var ErrLinuxDoConflict = errors.New("Linux.do candidate changed; retry synchronization")

// LinuxDoFeedState 读取上次已提交的采集游标，服务重启后仍能条件请求。
func (s *SQLiteStore) LinuxDoFeedState(ctx context.Context) (model.LinuxDoFeedState, error) {
	var state model.LinuxDoFeedState
	var checked string
	err := s.db.QueryRowContext(ctx, `SELECT etag,last_modified,checked_at,rule_version FROM linuxdo_state WHERE id=1`).Scan(&state.ETag, &state.LastModified, &checked, &state.RuleVersion)
	state.CheckedAt = parseStoredTime(checked)
	return state, err
}

// ObserveLinuxDo 先把首帖和候选落盘再推进 ETag，核验失败的候选可跨重启重试。
// RSS 是滑动窗口：没有看到的帖子/链接不等于删除，不能因此清空历史来源关系。
func (s *SQLiteStore) ObserveLinuxDo(ctx context.Context, observations []model.LinuxDoObservation, state model.LinuxDoFeedState) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := timeString(state.CheckedAt)
	publicChanged := false
	for _, observation := range observations {
		topic := observation.Topic
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM linuxdo_topics t WHERE t.id=? AND (t.title<>? OR t.published_at<>?) AND EXISTS(SELECT 1 FROM linuxdo_repo_topics p WHERE p.topic_id=t.id)`, topic.ID, topic.Title, topic.PublishedAt).Scan(&count); err != nil {
			return err
		}
		publicChanged = publicChanged || count > 0
		_, err = tx.ExecContext(ctx, `INSERT INTO linuxdo_topics(id,url,title,published_at,content_hash,last_seen_at) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET url=excluded.url,title=excluded.title,published_at=excluded.published_at,content_hash=excluded.content_hash,last_seen_at=excluded.last_seen_at`, topic.ID, topic.URL, topic.Title, topic.PublishedAt, topic.ContentHash, now)
		if err != nil {
			return err
		}
		for _, candidate := range observation.Candidates {
			_, err = tx.ExecContext(ctx, `INSERT INTO linuxdo_candidates(topic_id,full_name,content_hash,context,explicit_promotion,rule_version,reason) VALUES(?,?,?,?,?,?,?)
				ON CONFLICT(topic_id,full_name) DO UPDATE SET
				state=CASE WHEN content_hash<>excluded.content_hash OR rule_version<>excluded.rule_version OR explicit_promotion<>excluded.explicit_promotion THEN 'pending' ELSE state END,
				reason=CASE WHEN content_hash<>excluded.content_hash OR rule_version<>excluded.rule_version OR explicit_promotion<>excluded.explicit_promotion THEN excluded.reason ELSE reason END,
				content_hash=excluded.content_hash,context=excluded.context,explicit_promotion=excluded.explicit_promotion,rule_version=excluded.rule_version`, topic.ID, candidate.FullName, candidate.ContentHash, candidate.Context, boolInt(candidate.ExplicitPromotion), candidate.RuleVersion, candidate.Reason)
			if err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE linuxdo_state SET etag=?,last_modified=?,checked_at=?,rule_version=? WHERE id=1`, state.ETag, state.LastModified, now, state.RuleVersion); err != nil {
		return err
	}
	if publicChanged {
		if err := bumpLinuxDoRevision(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const linuxDoCandidateSelect = `SELECT c.id,c.topic_id,c.full_name,c.content_hash,c.context,c.explicit_promotion,c.state,c.reason,c.gh_repo_id,c.license,c.rule_version FROM linuxdo_candidates c`

// LinuxDoCandidates 仅用于自动判定的只读诊断，不提供人工审批入口。
func (s *SQLiteStore) LinuxDoCandidates(ctx context.Context, after int64) ([]model.LinuxDoCandidate, error) {
	rows, err := s.db.QueryContext(ctx, linuxDoCandidateSelect+` WHERE c.id>? ORDER BY c.id LIMIT 100`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinuxDoCandidates(rows)
}

// LinuxDoVerificationQueue 重试未完成候选，并低频重验仍在 RSS 可见范围的公开关系。
// 自动跳过不反复消耗 GitHub 配额；内容或规则变化会由分类写入路径重新置为 pending。
func (s *SQLiteStore) LinuxDoVerificationQueue(ctx context.Context) ([]model.LinuxDoCandidate, error) {
	rows, err := s.db.QueryContext(ctx, linuxDoCandidateSelect+` JOIN linuxdo_topics t ON t.id=c.topic_id
		WHERE c.content_hash=t.content_hash AND c.rule_version=? AND (c.state='pending' OR (c.state='published' AND t.last_seen_at>=? AND EXISTS(
			SELECT 1 FROM linuxdo_repo_topics p WHERE p.topic_id=c.topic_id AND p.gh_repo_id=c.gh_repo_id AND p.verified_at<?)))
		ORDER BY CASE WHEN c.state='pending' THEN 0 ELSE 1 END,c.id LIMIT 100`, model.LinuxDoRuleVersion, timeString(time.Now().Add(-7*24*time.Hour)), timeString(time.Now().Add(-24*time.Hour)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinuxDoCandidates(rows)
}

func scanLinuxDoCandidate(row rowScanner) (model.LinuxDoCandidate, error) {
	var c model.LinuxDoCandidate
	err := row.Scan(&c.ID, &c.TopicID, &c.FullName, &c.ContentHash, &c.Context, &c.ExplicitPromotion, &c.State, &c.Reason, &c.RepoID, &c.License, &c.RuleVersion)
	return c, err
}

func scanLinuxDoCandidates(rows *sql.Rows) ([]model.LinuxDoCandidate, error) {
	result := []model.LinuxDoCandidate{}
	for rows.Next() {
		c, err := scanLinuxDoCandidate(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// LinuxDoStaleObservations 按帖子取旧规则证据，不能只取待处理项后误把多仓库变成单仓库。
// 每轮最多重判 100 篇；仅返回最小上下文，不复制全文，也不冒充这些帖子刚在 RSS 中出现。
func (s *SQLiteStore) LinuxDoStaleObservations(ctx context.Context, version int) ([]model.LinuxDoObservation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.topic_id,c.full_name,c.content_hash,c.context,c.explicit_promotion,c.state,c.reason,c.gh_repo_id,c.license,c.rule_version,t.title
		FROM linuxdo_candidates c JOIN linuxdo_topics t ON t.id=c.topic_id
		WHERE c.content_hash=t.content_hash AND c.topic_id IN (
			SELECT old.topic_id FROM linuxdo_candidates old JOIN linuxdo_topics ot ON ot.id=old.topic_id
			WHERE old.rule_version<? AND old.content_hash=ot.content_hash GROUP BY old.topic_id ORDER BY old.topic_id LIMIT 100
		) ORDER BY c.topic_id,c.id`, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.LinuxDoObservation{}
	indices := map[int64]int{}
	for rows.Next() {
		var c model.LinuxDoCandidate
		var title string
		if err := rows.Scan(&c.ID, &c.TopicID, &c.FullName, &c.ContentHash, &c.Context, &c.ExplicitPromotion, &c.State, &c.Reason, &c.RepoID, &c.License, &c.RuleVersion, &title); err != nil {
			return nil, err
		}
		index, exists := indices[c.TopicID]
		if !exists {
			index = len(result)
			indices[c.TopicID] = index
			result = append(result, model.LinuxDoObservation{Topic: model.LinuxDoTopic{ID: c.TopicID, Title: title}})
		}
		result[index].Candidates = append(result[index].Candidates, c)
	}
	return result, rows.Err()
}

// ReclassifyLinuxDoCandidates 只重置旧判定，不更新 RSS last_seen 或提前改动公共快照。
func (s *SQLiteStore) ReclassifyLinuxDoCandidates(ctx context.Context, candidates []model.LinuxDoCandidate) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range candidates {
		if _, err := tx.ExecContext(ctx, `UPDATE linuxdo_candidates SET explicit_promotion=?,reason=?,rule_version=?,state='pending'
			WHERE id=? AND content_hash=? AND rule_version<? AND EXISTS(
				SELECT 1 FROM linuxdo_topics t WHERE t.id=linuxdo_candidates.topic_id AND t.content_hash=linuxdo_candidates.content_hash
			)`, boolInt(c.ExplicitPromotion), c.Reason, c.RuleVersion, c.ID, c.ContentHash, c.RuleVersion); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ApplyLinuxDoDecision 原子保存机器判定、仓库事实与来源关系；跳过项不进入人工队列。
// 复用 Awesome 的事实 upsert（不改 ranking/目录归属），避免 Linux.do 元数据把原目录分数归零。
func (s *SQLiteStore) ApplyLinuxDoDecision(ctx context.Context, candidate model.LinuxDoCandidate, state, reason string, repo *model.Repository) error {
	if state != "published" && state != "skipped" {
		return fmt.Errorf("invalid Linux.do decision")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentHash, topicHash, currentState string
	var oldID int64
	var ruleVersion int
	err = tx.QueryRowContext(ctx, `SELECT c.content_hash,t.content_hash,c.state,c.gh_repo_id,c.rule_version FROM linuxdo_candidates c JOIN linuxdo_topics t ON t.id=c.topic_id WHERE c.id=?`, candidate.ID).Scan(&currentHash, &topicHash, &currentState, &oldID, &ruleVersion)
	if err != nil {
		return err
	}
	if currentHash != candidate.ContentHash || topicHash != candidate.ContentHash || currentState != candidate.State || ruleVersion != candidate.RuleVersion {
		return ErrLinuxDoConflict
	}
	repoID, license := oldID, candidate.License
	if repo != nil {
		repoID = repo.GhRepoID
		license = repo.LicenseSpdx
	}
	if state == "published" {
		if repo == nil {
			return fmt.Errorf("verified repository required")
		}
		if err := upsertAwesomeRepo(ctx, tx, *repo, time.Now().UTC()); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO linuxdo_repo_topics(gh_repo_id,topic_id,verified_at) VALUES(?,?,?) ON CONFLICT(gh_repo_id,topic_id) DO UPDATE SET verified_at=excluded.verified_at`, repoID, candidate.TopicID, timeString(time.Now().UTC())); err != nil {
			return err
		}
	}
	publicChanged := state == "published"
	if state == "skipped" || (oldID > 0 && oldID != repoID) {
		deleted, err := tx.ExecContext(ctx, `DELETE FROM linuxdo_repo_topics WHERE gh_repo_id=? AND topic_id=?`, oldID, candidate.TopicID)
		if err != nil {
			return err
		}
		count, err := deleted.RowsAffected()
		if err != nil {
			return err
		}
		publicChanged = publicChanged || count > 0
	}
	if _, err = tx.ExecContext(ctx, `UPDATE linuxdo_candidates SET state=?,reason=?,gh_repo_id=?,license=? WHERE id=?`, state, reason, repoID, license, candidate.ID); err != nil {
		return err
	}
	if publicChanged {
		if err := bumpLinuxDoRevision(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func bumpLinuxDoRevision(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE linuxdo_state SET revision=revision+1,updated_at=? WHERE id=1`, timeString(time.Now().UTC()))
	return err
}

// RevokePrivateLinuxDoRepository 根据已确认的 GitHub 数字 ID 原子撤回全部来源关系。
// 只删除本功能的公开关联，不删除共享仓库事实或其它目录的数据。
func (s *SQLiteStore) RevokePrivateLinuxDoRepository(ctx context.Context, repoID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM linuxdo_repo_topics WHERE gh_repo_id=?`, repoID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE linuxdo_candidates SET state='skipped',reason='verified private repository' WHERE gh_repo_id=?`, repoID); err != nil {
		return err
	}
	if count > 0 {
		if err = bumpLinuxDoRevision(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LinuxDoSnapshot 在同一事务读取 revision/目录/来源，防止撤回期间拼接出相互矛盾的快照。
func (s *SQLiteStore) LinuxDoSnapshot(ctx context.Context) (model.LinuxDoSnapshot, error) {
	result := model.LinuxDoSnapshot{Complete: true, Repositories: []model.DiscoveryItem{}, Origins: []model.LinuxDoOrigin{}, Topics: map[int64][]model.LinuxDoTopic{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT revision,updated_at FROM linuxdo_state WHERE id=1`).Scan(&result.Revision, &result.GeneratedAt); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT r.*,0 AS rank,0 AS score FROM repos r WHERE EXISTS(SELECT 1 FROM linuxdo_repo_topics p WHERE p.gh_repo_id=r.gh_repo_id) ORDER BY r.stars DESC,r.gh_repo_id DESC`)
	if err != nil {
		return result, err
	}
	result.Repositories, err = scanItems(rows)
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT p.gh_repo_id,t.id,t.url,t.title,t.published_at,p.verified_at FROM linuxdo_repo_topics p JOIN linuxdo_topics t ON t.id=p.topic_id ORDER BY p.gh_repo_id,t.published_at DESC,t.id DESC`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var id int64
		var topic model.LinuxDoTopic
		if err = rows.Scan(&id, &topic.ID, &topic.URL, &topic.Title, &topic.PublishedAt, &topic.VerifiedAt); err != nil {
			rows.Close()
			return result, err
		}
		result.Topics[id] = append(result.Topics[id], topic)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for _, repo := range result.Repositories {
		topics := result.Topics[repo.RepoID]
		verified := ""
		for _, topic := range topics {
			if topic.VerifiedAt > verified {
				verified = topic.VerifiedAt
			}
		}
		result.Origins = append(result.Origins, model.LinuxDoOrigin{RepoID: repo.RepoID, TopicCount: len(topics), VerifiedAt: verified})
	}
	return result, tx.Commit()
}
