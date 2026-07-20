// Package scheduler 管理 discovery 后台同步任务。
package scheduler

import (
	"context"
	"log"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

// SyncService 是 scheduler 依赖的同步服务接口。
type SyncService interface {
	Sync(ctx context.Context, mode string) (model.SyncResult, error)
}

// CacheInvalidator 是后台同步完成后要主动失效的缓存。
type CacheInvalidator interface {
	Invalidate()
}

// Scheduler 封装 cron 任务生命周期。
type Scheduler struct {
	cron        *cron.Cron
	invalidator CacheInvalidator
}

// New 创建同步调度器。
func New(syncer SyncService, syncSpec, fullSyncSpec string, invalidator CacheInvalidator) *Scheduler {
	c := cron.New()
	mustAdd(c, syncSpec, func() {
		run(syncer, "scheduled-light", invalidator)
	})
	mustAdd(c, fullSyncSpec, func() {
		run(syncer, "scheduled-full", invalidator)
	})
	return &Scheduler{cron: c, invalidator: invalidator}
}

// Start 启动 cron。
func (s *Scheduler) Start() {
	s.cron.Start()
	log.Println("[scheduler] discovery cron started")
}

// Stop 停止 cron 并等待正在执行的任务退出。
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("[scheduler] discovery cron stopped")
}

func mustAdd(c *cron.Cron, spec string, fn func()) {
	if _, err := c.AddFunc(spec, fn); err != nil {
		log.Fatalf("[scheduler] invalid cron spec %q: %v", spec, err)
	}
}

func run(syncer SyncService, mode string, invalidator CacheInvalidator) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	result, err := syncer.Sync(ctx, mode)
	if err != nil {
		log.Printf("[scheduler] sync %s failed: %v result=%+v", mode, err, result)
		return
	}
	if invalidator != nil {
		invalidator.Invalidate()
	}
	log.Printf("[scheduler] sync %s success: repos_seen=%d repos_upserted=%d", mode, result.ReposSeen, result.ReposUpserted)
}
