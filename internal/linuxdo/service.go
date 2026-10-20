// Linux.do 服务编排低频采集、主项目自动识别、GitHub 核验与发布缓存失效。
package linuxdo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	gh "github.com/starcat-app/starcat-discovery-api/internal/github"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"github.com/starcat-app/starcat-discovery-api/internal/store"
)

var ErrDisabled = errors.New("Linux.do collection is disabled pending authorization")
var ErrBusy = errors.New("Linux.do sync is already running")

// 只有 GitHub 明确返回 private 才撤回；403/404/超时不能伪装成私有或删除。
type privateRepositoryError struct{ id int64 }

func (e *privateRepositoryError) Error() string { return "verified repository is private" }

// GitHubClient 复用服务端 Token Pool，绝不使用用户的 GitHub / Linux.do 凭证。
type GitHubClient interface {
	GetRepository(context.Context, string) (gh.Repository, error)
}
type CacheInvalidator interface{ Invalidate() }

// Service 单并发执行采集、规则升级重判与核验，避免重复发布或覆盖另一轮的证据。
type Service struct {
	store     *store.SQLiteStore
	github    GitHubClient
	collector *Collector
	cache     CacheInvalidator
	enabled   bool
	mu        sync.Mutex
}

func NewService(s *store.SQLiteStore, github GitHubClient, enabled bool, cache CacheInvalidator) *Service {
	return &Service{store: s, github: github, collector: NewCollector(), enabled: enabled, cache: cache}
}

// Sync 只保存可见 RSS 内容；304 仍处理持久化待核验队列。失败不替换公共快照。
func (s *Service) Sync(ctx context.Context, mode string) (result model.SyncResult, err error) {
	if !s.enabled {
		return result, ErrDisabled
	}
	if !s.mu.TryLock() {
		return result, ErrBusy
	}
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	id, started, err := s.store.StartSyncRun(ctx, "linuxdo-"+mode)
	if err != nil {
		return result, err
	}
	result = model.SyncResult{RunID: id, Mode: "linuxdo-" + mode, Status: "running", StartedAt: started.Format(time.RFC3339)}
	defer func() {
		result.Status = "succeeded"
		if err != nil {
			result.Status = "failed"
			result.ErrorMessage = "Linux.do sync incomplete; previous publication retained"
		}
		// 请求取消也必须收口运行记录，不把调用方取消传播成永久 running。
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer finishCancel()
		finished, finishErr := s.store.FinishSyncRun(finishCtx, id, result.Status, result.ReposSeen, result.ReposUpserted, result.ErrorMessage)
		result.FinishedAt = finished.Format(time.RFC3339)
		if err == nil {
			err = finishErr
		}
		if s.cache != nil {
			s.cache.Invalidate()
		}
	}()
	state, err := s.store.LinuxDoFeedState(ctx)
	if err != nil {
		return result, err
	}
	observations, state, err := s.collector.Fetch(ctx, state)
	if err != nil {
		return result, err
	}
	if err = s.store.ObserveLinuxDo(ctx, observations, state); err != nil {
		return result, err
	}
	stale, err := s.store.LinuxDoStaleObservations(ctx, model.LinuxDoRuleVersion)
	if err != nil {
		return result, err
	}
	for _, observation := range stale {
		classifyCandidates(observation.Topic.Title, observation.Candidates)
		if err = s.store.ReclassifyLinuxDoCandidates(ctx, observation.Candidates); err != nil {
			return result, err
		}
	}
	candidates, err := s.store.LinuxDoVerificationQueue(ctx)
	if err != nil {
		return result, err
	}
	var incomplete bool
	for _, candidate := range candidates {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		result.ReposSeen++
		if !candidate.ExplicitPromotion {
			if err = s.store.ApplyLinuxDoDecision(ctx, candidate, "skipped", candidate.Reason, nil); err != nil {
				return result, err
			}
			continue
		}
		repo, verifyErr := s.verify(ctx, candidate.FullName)
		if verifyErr != nil {
			var private *privateRepositoryError
			if errors.As(verifyErr, &private) {
				if err = s.store.RevokePrivateLinuxDoRepository(ctx, private.id); err != nil {
					return result, err
				}
				if candidate.RepoID != private.id {
					if err = s.store.ApplyLinuxDoDecision(ctx, candidate, "skipped", "verified private repository", nil); err != nil {
						return result, err
					}
				}
				continue
			}
			incomplete = true
			continue
		}
		// 来源只表达推广关系，不替项目作许可证背书；未知许可证如实保留，不引入人工门禁。
		if err = s.store.ApplyLinuxDoDecision(ctx, candidate, "published", "automatically identified project and verified public repository", &repo); err != nil {
			return result, err
		}
		result.ReposUpserted++
	}
	if incomplete {
		return result, fmt.Errorf("GitHub verification incomplete; candidates remain queued")
	}
	return result, nil
}

func (s *Service) Candidates(ctx context.Context, after int64) ([]model.LinuxDoCandidate, error) {
	return s.store.LinuxDoCandidates(ctx, after)
}
func (s *Service) Snapshot(ctx context.Context) (model.LinuxDoSnapshot, error) {
	return s.store.LinuxDoSnapshot(ctx)
}

// verify 使用 GitHub 的 canonical ID 和真实公开状态；404/限流/私有不推断为已删除。
func (s *Service) verify(ctx context.Context, fullName string) (model.Repository, error) {
	r, err := s.github.GetRepository(ctx, fullName)
	if err != nil {
		return model.Repository{}, fmt.Errorf("GitHub verification unavailable")
	}
	if r.ID > 0 && r.Private {
		return model.Repository{}, &privateRepositoryError{id: r.ID}
	}
	if r.ID <= 0 || r.Owner.Login == "" || r.Name == "" || r.FullName == "" {
		return model.Repository{}, fmt.Errorf("repository is not verified public")
	}
	license := ""
	if r.License != nil {
		license = r.License.SPDXID
	}
	now := time.Now().UTC()
	parse := func(raw string) *time.Time {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil
		}
		return &t
	}
	return model.Repository{GhRepoID: r.ID, Owner: r.Owner.Login, Name: r.Name, FullName: r.FullName, Description: r.Description, Homepage: r.Homepage, Language: r.Language, Stars: r.Stargazers, Forks: r.Forks, Watchers: r.Watchers, Subscribers: r.Subscribers, OpenIssues: r.OpenIssues, OwnerAvatar: r.Owner.AvatarURL, DefaultBranch: r.DefaultBranch, LicenseSpdx: license, Topics: r.Topics, PushedAt: parse(r.PushedAt), UpdatedAt: parse(r.UpdatedAt), CreatedAt: parse(r.CreatedAt), IsArchived: r.Archived, IsFork: r.Fork, IndexedAt: now, EnrichedAt: &now}, nil
}
