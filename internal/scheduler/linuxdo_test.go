// Linux.do 调度使用虚拟时钟验证小时级等待，不访问论坛，也不让测试真的休眠数小时。
package scheduler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

// linuxDoSyncFunc 让测试精确控制一轮同步的耗时、失败和取消。
type linuxDoSyncFunc func(context.Context, string) (model.SyncResult, error)

func (f linuxDoSyncFunc) Sync(ctx context.Context, mode string) (model.SyncResult, error) {
	return f(ctx, mode)
}

func TestLinuxDoRandomDelayBounds(t *testing.T) {
	if linuxDoMinDelay != 5*time.Minute || linuxDoMaxDelay != 5*time.Hour {
		t.Fatal("Linux.do collection interval changed")
	}
	seen := map[time.Duration]bool{}
	for range 1000 {
		delay := randomLinuxDoDelay()
		if delay < linuxDoMinDelay || delay > linuxDoMaxDelay {
			t.Fatalf("delay outside permitted range: %s", delay)
		}
		seen[delay] = true
	}
	if len(seen) < 2 {
		t.Fatal("collection interval is fixed instead of randomized")
	}
}

// 成功和失败都从本轮完成时重新计时，不能在同步期间提前安排下一轮。
func TestLinuxDoSchedulerWaitsAfterEachRun(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{{"success", nil}, {"failure", errors.New("upstream unavailable")}} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var startsMu sync.Mutex
				var starts []time.Time
				startTimes := func() []time.Time {
					startsMu.Lock()
					defer startsMu.Unlock()
					return append([]time.Time(nil), starts...)
				}
				syncer := linuxDoSyncFunc(func(ctx context.Context, mode string) (model.SyncResult, error) {
					if mode != "scheduled" {
						t.Errorf("unexpected sync mode: %s", mode)
					}
					startsMu.Lock()
					starts = append(starts, time.Now())
					startsMu.Unlock()
					timer := time.NewTimer(2 * time.Minute)
					defer timer.Stop()
					select {
					case <-ctx.Done():
						return model.SyncResult{}, ctx.Err()
					case <-timer.C:
						return model.SyncResult{}, test.err
					}
				})
				scheduler := NewLinuxDo(syncer)
				var draws atomic.Int32
				scheduler.delay = func() time.Duration {
					if draws.Add(1) == 1 {
						return 5 * time.Minute
					}
					return 5 * time.Hour
				}
				scheduler.Start()
				scheduler.Start() // 重复启动不能创建第二个采集循环。
				defer scheduler.Stop()
				synctest.Wait()
				time.Sleep(5*time.Minute - time.Nanosecond)
				synctest.Wait()
				if len(startTimes()) != 0 || draws.Load() != 1 {
					t.Fatal("collector ran before the initial randomized wait")
				}
				time.Sleep(time.Nanosecond)
				synctest.Wait()
				if len(startTimes()) != 1 || draws.Load() != 1 {
					t.Fatal("next wait was selected before the active run completed")
				}
				time.Sleep(2 * time.Minute)
				synctest.Wait()
				if draws.Load() != 2 {
					t.Fatal("completed run did not select a new random interval")
				}
				time.Sleep(5*time.Hour - time.Nanosecond)
				synctest.Wait()
				if len(startTimes()) != 1 {
					t.Fatal("collector ignored the second interval")
				}
				time.Sleep(time.Nanosecond)
				synctest.Wait()
				times := startTimes()
				if len(times) != 2 || times[1].Sub(times[0]) != 5*time.Hour+2*time.Minute {
					t.Fatalf("unexpected collection times: %v", times)
				}
			})
		})
	}
}

func TestLinuxDoSchedulerStopCancelsWaitAndAllowsRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		scheduler := NewLinuxDo(linuxDoSyncFunc(func(context.Context, string) (model.SyncResult, error) {
			calls.Add(1)
			return model.SyncResult{}, nil
		}))
		scheduler.delay = func() time.Duration { return 5 * time.Minute }
		scheduler.Stop() // 从未启动时停止也不能阻塞。
		scheduler.Start()
		defer scheduler.Stop()
		synctest.Wait()
		scheduler.Stop()
		scheduler.Stop()
		time.Sleep(6 * time.Hour)
		synctest.Wait()
		if calls.Load() != 0 {
			t.Fatal("stopped wait still triggered collection")
		}
		scheduler.Start()
		synctest.Wait()
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatalf("restart created an unexpected number of loops: %d", calls.Load())
		}
	})
}

func TestLinuxDoSchedulerStopWaitsForCanceledRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		var canceled atomic.Bool
		scheduler := NewLinuxDo(linuxDoSyncFunc(func(ctx context.Context, _ string) (model.SyncResult, error) {
			calls.Add(1)
			<-ctx.Done()
			canceled.Store(true)
			return model.SyncResult{}, ctx.Err()
		}))
		scheduler.delay = func() time.Duration { return 5 * time.Minute }
		scheduler.Start()
		defer scheduler.Stop()
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatal("sync did not start")
		}
		scheduler.Stop()
		if !canceled.Load() {
			t.Fatal("stop returned before the active sync exited")
		}
		time.Sleep(6 * time.Hour)
		synctest.Wait()
		if calls.Load() != 1 {
			t.Fatal("collector restarted after shutdown")
		}
	})
}
