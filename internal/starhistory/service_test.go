package starhistory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestServiceBuildsReadyHistoryAndReturnsDownsampledCache(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store := newMemoryCacheStore()
	store.snapshots[9001] = []model.StarHistoryPoint{{
		Date:      "2026-07-27",
		Count:     11,
		Source:    model.StarHistorySourceDiscoverySnapshot,
		Precision: model.StarHistorySnapshot,
	}}
	provider := &fakeHistoryProvider{events: []DailyWatchEvent{{
		Date:  time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
		Count: 5,
	}}}
	service, err := NewService(store, provider, testServiceConfig())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return now }
	t.Cleanup(service.Close)

	request := model.StarHistoryBuildRequest{
		GhRepoID:     9001,
		FullName:     "octo/history",
		CurrentStars: 12,
	}
	claimed, err := service.Enqueue(t.Context(), request)
	if err != nil || !claimed {
		t.Fatalf("Enqueue() = %v, %v", claimed, err)
	}
	waitForCacheStatus(t, store, request.GhRepoID, model.StarHistoryReady)

	result, err := service.Lookup(
		t.Context(),
		request.GhRepoID,
		request.FullName,
		model.StarHistoryRangeAll,
	)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if result.State != LookupReady || result.CurrentStars != 12 || len(result.Series.Points) != 2 {
		t.Fatalf("unexpected lookup result: %+v", result)
	}
	if result.Series.Points[1].Count != 11 ||
		result.Series.Points[1].Precision != model.StarHistorySnapshot {
		t.Fatalf("exact snapshot was not merged: %+v", result.Series.Points)
	}
	if provider.lastRequest.MaximumBytesBilled != testServiceConfig().MaximumBytesBilled {
		t.Fatalf("provider budget = %d", provider.lastRequest.MaximumBytesBilled)
	}
}

func TestServiceAppliesDailyBudgetBeforeCallingProvider(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	store := newMemoryCacheStore()
	provider := &fakeHistoryProvider{}
	config := testServiceConfig()
	config.DailyMaximumBytes = config.MaximumBytesBilled
	service, err := NewService(store, provider, config)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	service.now = func() time.Time { return now }
	t.Cleanup(service.Close)

	first := model.StarHistoryBuildRequest{GhRepoID: 1, FullName: "octo/one", CurrentStars: 1}
	if claimed, enqueueErr := service.Enqueue(t.Context(), first); enqueueErr != nil || !claimed {
		t.Fatalf("first Enqueue() = %v, %v", claimed, enqueueErr)
	}
	waitForCacheStatus(t, store, first.GhRepoID, model.StarHistoryReady)

	second := model.StarHistoryBuildRequest{GhRepoID: 2, FullName: "octo/two", CurrentStars: 2}
	if claimed, enqueueErr := service.Enqueue(t.Context(), second); enqueueErr != nil || !claimed {
		t.Fatalf("second Enqueue() = %v, %v", claimed, enqueueErr)
	}
	cache := waitForCacheStatus(t, store, second.GhRepoID, model.StarHistoryFailed)
	if cache.ErrorSummary != "daily query budget exhausted" {
		t.Fatalf("unexpected negative cache error = %q", cache.ErrorSummary)
	}
	if provider.callCount != 1 {
		t.Fatalf("provider call count = %d, want 1", provider.callCount)
	}
}

func testServiceConfig() ServiceConfig {
	return ServiceConfig{
		CacheTTL:           24 * time.Hour,
		NegativeCacheTTL:   10 * time.Minute,
		BuildTimeout:       time.Second,
		WorkerConcurrency:  1,
		QueueCapacity:      2,
		MaximumPoints:      500,
		MaximumBytesBilled: 100,
		DailyMaximumBytes:  1_000,
	}
}

type fakeHistoryProvider struct {
	mu          sync.Mutex
	events      []DailyWatchEvent
	lastRequest HistoryEventRequest
	callCount   int
}

func (p *fakeHistoryProvider) DailyWatchEvents(
	_ context.Context,
	request HistoryEventRequest,
) ([]DailyWatchEvent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRequest = request
	p.callCount++
	return append([]DailyWatchEvent(nil), p.events...), nil
}

type memoryCacheStore struct {
	mu        sync.Mutex
	caches    map[int64]model.StarHistoryCache
	snapshots map[int64][]model.StarHistoryPoint
	changed   chan struct{}
}

func newMemoryCacheStore() *memoryCacheStore {
	return &memoryCacheStore{
		caches:    make(map[int64]model.StarHistoryCache),
		snapshots: make(map[int64][]model.StarHistoryPoint),
		changed:   make(chan struct{}, 16),
	}
}

func (s *memoryCacheStore) GetStarHistoryCache(
	_ context.Context,
	repoID int64,
) (model.StarHistoryCache, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache, found := s.caches[repoID]
	return cache, found, nil
}

func (s *memoryCacheStore) ClaimStarHistoryBuild(
	_ context.Context,
	request model.StarHistoryBuildRequest,
	now time.Time,
	leaseExpiresAt time.Time,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, found := s.caches[request.GhRepoID]; found && existing.ExpiresAt.After(now) {
		return false, nil
	}
	s.caches[request.GhRepoID] = model.StarHistoryCache{
		GhRepoID:     request.GhRepoID,
		FullName:     request.FullName,
		CurrentStars: request.CurrentStars,
		Status:       model.StarHistoryBuilding,
		ExpiresAt:    leaseExpiresAt,
		UpdatedAt:    now,
	}
	s.notify()
	return true, nil
}

func (s *memoryCacheStore) SaveStarHistoryReady(
	_ context.Context,
	cache model.StarHistoryCache,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caches[cache.GhRepoID] = cache
	s.notify()
	return nil
}

func (s *memoryCacheStore) SaveStarHistoryFailed(
	_ context.Context,
	cache model.StarHistoryCache,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caches[cache.GhRepoID] = cache
	s.notify()
	return nil
}

func (s *memoryCacheStore) ListStarHistorySnapshots(
	_ context.Context,
	repoID int64,
) ([]model.StarHistoryPoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.StarHistoryPoint(nil), s.snapshots[repoID]...), nil
}

func (s *memoryCacheStore) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func waitForCacheStatus(
	t *testing.T,
	store *memoryCacheStore,
	repoID int64,
	status model.StarHistoryCacheStatus,
) model.StarHistoryCache {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		cache, found, err := store.GetStarHistoryCache(t.Context(), repoID)
		if err != nil {
			t.Fatalf("GetStarHistoryCache() error = %v", err)
		}
		if found && cache.Status == status {
			return cache
		}
		select {
		case <-store.changed:
		case <-timer.C:
			t.Fatalf("timed out waiting for status %s, last cache = %+v", status, cache)
		}
	}
}
