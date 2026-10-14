// Linux.do 服务编排低频采集、GitHub 核验、人工审核与发布缓存失效。
package linuxdo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	gh "github.com/starcat-app/starcat-discovery-api/internal/github"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"github.com/starcat-app/starcat-discovery-api/internal/store"
)

var ErrDisabled = errors.New("Linux.do collection is disabled pending authorization")
var ErrBusy = errors.New("Linux.do sync or review is already running")

// 只有 GitHub 明确返回 private 才撤回；403/404/超时不能伪装成私有或删除。
type privateRepositoryError struct{ id int64 }

func (e *privateRepositoryError) Error() string { return "verified repository is private" }

// GitHubClient 复用服务端 Token Pool，绝不使用用户的 GitHub / Linux.do 凭证。
type GitHubClient interface {
	GetRepository(context.Context, string) (gh.Repository, error)
}
type CacheInvalidator interface{ Invalidate() }

// Service 单并发执行采集/审核，避免重复核验和审核结果被后台任务覆盖。
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
		repo, verifyErr := s.verify(ctx, candidate.FullName)
		if verifyErr != nil {
			var private *privateRepositoryError
			if errors.As(verifyErr, &private) {
				if err = s.store.RevokePrivateLinuxDoRepository(ctx, private.id); err != nil {
					return result, err
				}
				if candidate.RepoID == 0 {
					if err = s.store.ReviewLinuxDo(ctx, candidate, "review", "repository is private", nil); err != nil {
						return result, err
					}
				}
				continue
			}
			incomplete = true
			continue
		}
		state, reason := "review", "promotion role requires review"
		if !knownOpenLicense(repo.LicenseSpdx) {
			reason = "license requires review"
		} else if candidate.ExplicitPromotion || candidate.State == "published" {
			state = "published"
			reason = "explicit promotion and verified public repository"
		}
		if err = s.store.ReviewLinuxDo(ctx, candidate, state, reason, &repo); err != nil {
			return result, err
		}
		if state == "published" {
			result.ReposUpserted++
		}
	}
	if incomplete {
		return result, fmt.Errorf("GitHub verification incomplete; candidates remain queued")
	}
	return result, nil
}

// ReviewRequest 的 hash 与明确许可确认使审核决定可追溯，不能用裸 repo ID 越过核验。
type ReviewRequest struct {
	ContentHash     string `json:"content_hash"`
	Decision        string `json:"decision"`
	LicenseReviewed bool   `json:"license_reviewed"`
	Reason          string `json:"reason"`
}

func (s *Service) Review(ctx context.Context, id int64, request ReviewRequest) error {
	if !s.mu.TryLock() {
		return ErrBusy
	}
	defer s.mu.Unlock()
	candidate, err := s.store.LinuxDoCandidate(ctx, id)
	if err != nil {
		return err
	}
	if request.ContentHash != candidate.ContentHash {
		return store.ErrLinuxDoConflict
	}
	var repo *model.Repository
	state := request.Decision
	switch state {
	case "publish":
		verified, err := s.verify(ctx, candidate.FullName)
		if err != nil {
			return err
		}
		if !knownOpenLicense(verified.LicenseSpdx) && (!request.LicenseReviewed || strings.TrimSpace(request.Reason) == "") {
			return fmt.Errorf("explicit license review and reason required")
		}
		repo = &verified
		state = "published"
	case "reject":
		state = "rejected"
	case "withdraw":
		state = "withdrawn"
	default:
		return fmt.Errorf("invalid review decision")
	}
	if len(request.Reason) > 1000 {
		return fmt.Errorf("review reason too long")
	}
	if err = s.store.ReviewLinuxDo(ctx, candidate, state, request.Reason, repo); err != nil {
		return err
	}
	if s.cache != nil {
		s.cache.Invalidate()
	}
	return nil
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

// 保守白名单只决定自动发布；未知或自定义许可证保留给有权限的人工审核。
func knownOpenLicense(spdx string) bool {
	switch spdx {
	case "MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "GPL-2.0", "GPL-3.0", "GPL-2.0-only", "GPL-3.0-only", "GPL-2.0-or-later", "GPL-3.0-or-later", "AGPL-3.0", "AGPL-3.0-only", "AGPL-3.0-or-later", "LGPL-2.1", "LGPL-3.0", "MPL-2.0", "ISC", "Unlicense":
		return true
	}
	return false
}
