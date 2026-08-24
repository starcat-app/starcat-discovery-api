// Package handler 的 Awesome 缓存测试验证命中、失效、容量上限与并发合并。
package handler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

func TestAwesomeResponseCacheReusesAndInvalidatesSource(t *testing.T) {
	cache := NewAwesomeResponseCache(time.Hour, 8, 1<<20)
	var builds atomic.Int32
	build := func(context.Context) (awesomeCachedResponse, error) {
		builds.Add(1)
		return newAwesomeCachedResponse([]string{"cached"}, &model.Meta{Total: 1})
	}
	key := awesomeEntriesCacheKey("awesome-test")

	first, err := cache.getOrBuild(context.Background(), key, build)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.getOrBuild(context.Background(), key, build)
	if err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 1 || first.etag != second.etag {
		t.Fatalf("cache hit builds=%d etags=%q/%q", builds.Load(), first.etag, second.etag)
	}

	cache.InvalidateAwesomeSource("awesome-test")
	if _, err := cache.getOrBuild(context.Background(), key, build); err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 2 {
		t.Fatalf("source invalidation builds=%d", builds.Load())
	}
}

func TestAwesomeResponseCacheCollapsesConcurrentBuilds(t *testing.T) {
	cache := NewAwesomeResponseCache(time.Hour, 8, 1<<20)
	var builds atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	build := func(context.Context) (awesomeCachedResponse, error) {
		builds.Add(1)
		startOnce.Do(func() { close(started) })
		<-release
		return newAwesomeCachedResponse([]string{"cached"}, &model.Meta{Total: 1})
	}

	const requestCount = 12
	var wait sync.WaitGroup
	wait.Add(requestCount)
	errors := make(chan error, requestCount)
	for range requestCount {
		go func() {
			defer wait.Done()
			_, err := cache.getOrBuild(context.Background(), awesomeCatalogCacheKey, build)
			errors <- err
		}()
	}
	<-started
	close(release)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if builds.Load() != 1 {
		t.Fatalf("concurrent builds=%d", builds.Load())
	}
}

func TestAwesomeResponseCacheEvictsLeastRecentlyUsedEntry(t *testing.T) {
	cache := NewAwesomeResponseCache(time.Hour, 2, 1<<20)
	var builds atomic.Int32
	build := func(context.Context) (awesomeCachedResponse, error) {
		builds.Add(1)
		return newAwesomeCachedResponse([]string{"cached"}, &model.Meta{Total: 1})
	}

	for _, key := range []string{"one", "two", "three"} {
		if _, err := cache.getOrBuild(context.Background(), key, build); err != nil {
			t.Fatal(err)
		}
	}
	if cache.lru.Len() != 2 || cache.items["one"] != nil {
		t.Fatalf("unexpected LRU state len=%d hasOne=%v", cache.lru.Len(), cache.items["one"] != nil)
	}
	if _, err := cache.getOrBuild(context.Background(), "one", build); err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 4 {
		t.Fatalf("evicted entry was not rebuilt: builds=%d", builds.Load())
	}
}
