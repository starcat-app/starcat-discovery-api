package starhistory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

var (
	// ErrQueueFull 表示服务已达到有界队列容量，调用方应按限流语义稍后重试。
	ErrQueueFull = errors.New("star history build queue is full")
	// ErrServiceClosed 防止服务关闭后写入新的 building 租约。
	ErrServiceClosed = errors.New("star history service is closed")
	// ErrRepositoryIDMismatch 防止 repo ID 被另一条 owner/name 路径复用。
	ErrRepositoryIDMismatch = errors.New("repository id does not match full name")
)

const ghArchiveCoverageStart = "2011-02-12"

// CacheStore 是异步构建所需的最小持久化边界。
type CacheStore interface {
	GetStarHistoryCache(ctx context.Context, repoID int64) (model.StarHistoryCache, bool, error)
	ClaimStarHistoryBuild(
		ctx context.Context,
		request model.StarHistoryBuildRequest,
		now time.Time,
		leaseExpiresAt time.Time,
	) (bool, error)
	SaveStarHistoryReady(ctx context.Context, cache model.StarHistoryCache) error
	SaveStarHistoryFailed(ctx context.Context, cache model.StarHistoryCache) error
	ListStarHistorySnapshots(ctx context.Context, repoID int64) ([]model.StarHistoryPoint, error)
}

// ServiceConfig 固化 worker、缓存和费用护栏；所有值都必须是正数。
type ServiceConfig struct {
	CacheTTL           time.Duration
	NegativeCacheTTL   time.Duration
	BuildTimeout       time.Duration
	WorkerConcurrency  int
	QueueCapacity      int
	MaximumPoints      int
	MaximumBytesBilled int64
	DailyMaximumBytes  int64
}

// LookupState 是 HTTP 层可直接映射的缓存状态。
type LookupState string

const (
	LookupMiss     LookupState = "miss"
	LookupBuilding LookupState = "building"
	LookupReady    LookupState = "ready"
	LookupFailed   LookupState = "failed"
)

// LookupResult 是 cache-first 查询结果；只有 ready 状态包含 Series。
type LookupResult struct {
	State         LookupState
	Series        model.StarHistorySeries
	CurrentStars  int
	MaxAgeSeconds int
	ErrorSummary  string
}

// Service 管理星标历史 cache-first 查询和有界异步构建。
//
// HTTP 请求只做缓存读取与任务入队，GH Archive 全历史查询始终由固定数量 worker
// 执行。每日预算按单次最大扫描量保守预留，服务重启最多丢失内存计数，不会绕过
// BigQuery 自身的 maximumBytesBilled 硬限制。
type Service struct {
	store    CacheStore
	provider HistoryEventProvider
	config   ServiceConfig
	queue    chan model.StarHistoryBuildRequest
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	now      func() time.Time

	budgetMu   sync.Mutex
	budgetDay  string
	budgetUsed int64
}

// NewService 校验全部护栏并立即启动固定数量 worker。
func NewService(
	store CacheStore,
	provider HistoryEventProvider,
	config ServiceConfig,
) (*Service, error) {
	if store == nil || provider == nil {
		return nil, fmt.Errorf("star history store and provider are required")
	}
	if config.CacheTTL <= 0 || config.NegativeCacheTTL <= 0 || config.BuildTimeout <= 0 {
		return nil, fmt.Errorf("star history TTL and timeout must be positive")
	}
	if config.WorkerConcurrency <= 0 || config.QueueCapacity <= 0 || config.MaximumPoints <= 0 {
		return nil, fmt.Errorf("star history worker, queue and point limits must be positive")
	}
	if config.MaximumBytesBilled <= 0 || config.DailyMaximumBytes < config.MaximumBytesBilled {
		return nil, fmt.Errorf("star history query and daily budgets are invalid")
	}

	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		store:    store,
		provider: provider,
		config:   config,
		queue:    make(chan model.StarHistoryBuildRequest, config.QueueCapacity),
		ctx:      ctx,
		cancel:   cancel,
		now:      time.Now,
	}
	for index := 0; index < config.WorkerConcurrency; index++ {
		service.wg.Add(1)
		go service.worker()
	}
	return service, nil
}

// Close 停止 worker；正在执行的 provider 请求会收到 context cancellation。
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
}

// Lookup 读取仍有效的状态，并仅在 ready 时做范围降采样。
func (s *Service) Lookup(
	ctx context.Context,
	repoID int64,
	fullName string,
	historyRange model.StarHistoryRange,
) (LookupResult, error) {
	cache, found, err := s.store.GetStarHistoryCache(ctx, repoID)
	if err != nil {
		return LookupResult{}, err
	}
	if !found || !cache.ExpiresAt.After(s.now().UTC()) {
		return LookupResult{State: LookupMiss}, nil
	}
	if !strings.EqualFold(strings.TrimSpace(cache.FullName), strings.TrimSpace(fullName)) {
		return LookupResult{}, ErrRepositoryIDMismatch
	}

	result := LookupResult{
		CurrentStars:  cache.CurrentStars,
		MaxAgeSeconds: max(0, int(cache.ExpiresAt.Sub(s.now().UTC()).Seconds())),
	}
	switch cache.Status {
	case model.StarHistoryBuilding:
		result.State = LookupBuilding
	case model.StarHistoryFailed:
		result.State = LookupFailed
		result.ErrorSummary = cache.ErrorSummary
	case model.StarHistoryReady:
		if cache.GeneratedAt == nil {
			return LookupResult{}, fmt.Errorf("ready star history cache has no generated_at")
		}
		result.State = LookupReady
		result.Series, err = DownsampleHistory(
			cache.Points,
			historyRange,
			s.now().UTC(),
			*cache.GeneratedAt,
			s.config.MaximumPoints,
		)
		if err != nil {
			return LookupResult{}, err
		}
	default:
		return LookupResult{}, fmt.Errorf("unsupported star history cache status %q", cache.Status)
	}
	return result, nil
}

// Enqueue 原子认领任务并非阻塞写入有界队列。
//
// 队列恰好在认领后满载时会写入短期失败缓存，避免留下直到 build lease 到期的
// building 假状态；HTTP 层将 ErrQueueFull 映射为 429。
func (s *Service) Enqueue(
	ctx context.Context,
	request model.StarHistoryBuildRequest,
) (bool, error) {
	select {
	case <-s.ctx.Done():
		return false, ErrServiceClosed
	default:
	}

	now := s.now().UTC()
	claimed, err := s.store.ClaimStarHistoryBuild(
		ctx,
		request,
		now,
		now.Add(s.config.BuildTimeout),
	)
	if err != nil || !claimed {
		return claimed, err
	}

	select {
	case s.queue <- request:
		return true, nil
	default:
		_ = s.saveFailed(context.Background(), request, ErrQueueFull, now)
		return false, ErrQueueFull
	}
}

func (s *Service) worker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case request := <-s.queue:
			s.build(request)
		}
	}
}

func (s *Service) build(request model.StarHistoryBuildRequest) {
	startedAt := s.now().UTC()
	if !s.reserveDailyBudget(startedAt, s.config.MaximumBytesBilled) {
		_ = s.saveFailed(context.Background(), request, errors.New("daily query budget exhausted"), startedAt)
		return
	}

	ctx, cancel := context.WithTimeout(s.ctx, s.config.BuildTimeout)
	defer cancel()
	startDate, _ := time.Parse("2006-01-02", ghArchiveCoverageStart)
	events, err := s.provider.DailyWatchEvents(ctx, HistoryEventRequest{
		RepoID:             request.GhRepoID,
		StartDate:          startDate,
		EndDate:            startedAt,
		MaximumBytesBilled: s.config.MaximumBytesBilled,
	})
	if err != nil {
		_ = s.saveFailed(context.Background(), request, err, s.now().UTC())
		return
	}
	estimated, err := NormalizeWatchEvents(events, request.CurrentStars)
	if err != nil {
		_ = s.saveFailed(context.Background(), request, err, s.now().UTC())
		return
	}
	exact, err := s.store.ListStarHistorySnapshots(ctx, request.GhRepoID)
	if err != nil {
		_ = s.saveFailed(context.Background(), request, err, s.now().UTC())
		return
	}
	points, err := MergeExactSnapshots(estimated, exact)
	if err != nil {
		_ = s.saveFailed(context.Background(), request, err, s.now().UTC())
		return
	}

	generatedAt := s.now().UTC()
	coverageStart := ""
	if len(points) > 0 {
		coverageStart = points[0].Date
	}
	_ = s.store.SaveStarHistoryReady(context.Background(), model.StarHistoryCache{
		GhRepoID:      request.GhRepoID,
		FullName:      request.FullName,
		CurrentStars:  request.CurrentStars,
		Status:        model.StarHistoryReady,
		CoverageStart: coverageStart,
		Points:        points,
		GeneratedAt:   &generatedAt,
		ExpiresAt:     generatedAt.Add(s.config.CacheTTL),
		UpdatedAt:     generatedAt,
	})
}

func (s *Service) saveFailed(
	ctx context.Context,
	request model.StarHistoryBuildRequest,
	buildErr error,
	now time.Time,
) error {
	return s.store.SaveStarHistoryFailed(ctx, model.StarHistoryCache{
		GhRepoID:     request.GhRepoID,
		FullName:     request.FullName,
		CurrentStars: request.CurrentStars,
		Status:       model.StarHistoryFailed,
		ExpiresAt:    now.Add(s.config.NegativeCacheTTL),
		ErrorSummary: buildErr.Error(),
		UpdatedAt:    now,
	})
}

func (s *Service) reserveDailyBudget(now time.Time, amount int64) bool {
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()
	day := now.UTC().Format("2006-01-02")
	if s.budgetDay != day {
		s.budgetDay = day
		s.budgetUsed = 0
	}
	if s.budgetUsed+amount > s.config.DailyMaximumBytes {
		return false
	}
	s.budgetUsed += amount
	return true
}
