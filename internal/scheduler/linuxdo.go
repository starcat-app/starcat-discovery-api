// Linux.do 使用串行、可取消的随机等待循环；普通 Discovery/Awesome 的 cron 保持不变。
package scheduler

import (
	"context"
	"log"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	linuxDoMinDelay = 5 * time.Minute
	linuxDoMaxDelay = 5 * time.Hour
)

// LinuxDoScheduler 从每轮完成时开始等待，避免长任务与下一轮重叠或停机后的补偿突发。
// 间隔不是访问许可或风控绕过机制；请求失败与上游冷却由采集器处理，不在这里立即重试。
type LinuxDoScheduler struct {
	syncer SyncService
	delay  func() time.Duration // 仅供同包测试替换；生产不开放额外调度配置。
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewLinuxDo 只在服务装配确认采集开关后创建；首轮也随机等待，不在启动时请求论坛。
func NewLinuxDo(syncer SyncService) *LinuxDoScheduler {
	return &LinuxDoScheduler{syncer: syncer, delay: randomLinuxDoDelay}
}

// randomLinuxDoDelay 在闭区间内均匀抽样，不把一次抽到的间隔永久复用为固定周期。
func randomLinuxDoDelay() time.Duration {
	return linuxDoMinDelay + time.Duration(rand.Int64N(int64(linuxDoMaxDelay-linuxDoMinDelay)+1))
}

// Start 幂等启动一个循环；重启重新抽取等待时间，已收录项目由 SQLite 独立持久化。
func (s *LinuxDoScheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	go func() {
		defer close(done)
		s.loop(ctx)
	}()
}

// Stop 取消定时器和当前同步，并等待退出后才允许关闭 SQLite 或重新启动。
// 锁覆盖等待，避免另一线程在旧循环尚未退出时启动第二个循环；loop 不获取此锁。
func (s *LinuxDoScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.done
	s.cancel, s.done = nil, nil
	log.Println("[scheduler] Linux.do collector stopped")
}

// loop 顺序执行等待与同步；日志只记录计划和汇总，不记录凭据或论坛正文。
func (s *LinuxDoScheduler) loop(ctx context.Context) {
	for {
		delay := s.delay()
		log.Printf("[scheduler] Linux.do next sync in %s at %s", delay.Round(time.Second), time.Now().Add(delay).UTC().Format(time.RFC3339))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		// 取消与到期可能同时就绪，不让停机路径再启动一轮同步。
		if ctx.Err() != nil {
			return
		}
		result, err := s.syncer.Sync(ctx, "scheduled")
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("[scheduler] Linux.do sync failed: %v", err)
		} else {
			log.Printf("[scheduler] Linux.do sync success: repos_seen=%d repos_upserted=%d", result.ReposSeen, result.ReposUpserted)
		}
	}
}
