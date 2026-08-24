// Package handler 中的 Awesome 响应缓存负责复用已经编码的公开目录与条目快照。
//
// SQLite 仍是跨进程重启的持久缓存；本文件只提供有界的进程内加速层，避免热门来源
// 每次请求都重复执行大 JOIN、JSON 编码、gzip 与 ETag 哈希。缓存失效期间同一 key 只允许
// 一个构建任务，防止并发请求同时冲击 SQLite。
package handler

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/model"
)

const (
	defaultAwesomeCacheTTL        = 15 * time.Minute
	defaultAwesomeCacheMaxEntries = 64
	defaultAwesomeCacheMaxBytes   = 64 << 20
	awesomeCatalogCacheKey        = "catalog"
)

type awesomeResponseEncodingError struct {
	cause error
}

func (e *awesomeResponseEncodingError) Error() string { return e.cause.Error() }
func (e *awesomeResponseEncodingError) Unwrap() error { return e.cause }

type awesomeCachedResponse struct {
	payload     []byte
	gzipPayload []byte
	etag        string
}

func (r awesomeCachedResponse) size() int {
	return len(r.payload) + len(r.gzipPayload)
}

type awesomeCacheItem struct {
	key       string
	response  awesomeCachedResponse
	expiresAt time.Time
}

type awesomeCacheBuild struct {
	done     chan struct{}
	response awesomeCachedResponse
	err      error
}

// AwesomeResponseCache 是并发安全的 Awesome 公开响应 LRU。
//
// maxEntries 与 maxBytes 同时约束内存；任一上限到达都会淘汰最久未使用项。
// 构建期间记录 key generation，若同步正好使该 key 失效，旧构建结果可以完成当前请求，
// 但不会重新写回缓存覆盖新数据。
type AwesomeResponseCache struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	maxBytes   int
	usedBytes  int
	now        func() time.Time
	items      map[string]*list.Element
	lru        *list.List
	inflight   map[string]*awesomeCacheBuild
	generation map[string]uint64
}

// NewAwesomeResponseCache 创建有界缓存；非正参数回退到安全默认值。
func NewAwesomeResponseCache(ttl time.Duration, maxEntries, maxBytes int) *AwesomeResponseCache {
	if ttl <= 0 {
		ttl = defaultAwesomeCacheTTL
	}
	if maxEntries <= 0 {
		maxEntries = defaultAwesomeCacheMaxEntries
	}
	if maxBytes <= 0 {
		maxBytes = defaultAwesomeCacheMaxBytes
	}
	return &AwesomeResponseCache{
		ttl:        ttl,
		maxEntries: maxEntries,
		maxBytes:   maxBytes,
		now:        time.Now,
		items:      make(map[string]*list.Element),
		lru:        list.New(),
		inflight:   make(map[string]*awesomeCacheBuild),
		generation: make(map[string]uint64),
	}
}

func awesomeEntriesCacheKey(sourceID string) string {
	return "entries:" + sourceID
}

func (c *AwesomeResponseCache) getOrBuild(
	ctx context.Context,
	key string,
	build func(context.Context) (awesomeCachedResponse, error),
) (awesomeCachedResponse, error) {
	c.mu.Lock()
	if response, ok := c.getLocked(key); ok {
		c.mu.Unlock()
		return response, nil
	}
	if pending, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return awesomeCachedResponse{}, ctx.Err()
		case <-pending.done:
			return pending.response, pending.err
		}
	}
	pending := &awesomeCacheBuild{done: make(chan struct{})}
	startGeneration := c.generation[key]
	c.inflight[key] = pending
	c.mu.Unlock()

	response, err := build(ctx)

	c.mu.Lock()
	pending.response = response
	pending.err = err
	if err == nil && c.generation[key] == startGeneration {
		c.setLocked(key, response)
	}
	delete(c.inflight, key)
	close(pending.done)
	c.mu.Unlock()
	return response, err
}

func (c *AwesomeResponseCache) getLocked(key string) (awesomeCachedResponse, bool) {
	element, ok := c.items[key]
	if !ok {
		return awesomeCachedResponse{}, false
	}
	item := element.Value.(*awesomeCacheItem)
	if !c.now().Before(item.expiresAt) {
		c.removeLocked(element)
		return awesomeCachedResponse{}, false
	}
	c.lru.MoveToFront(element)
	return item.response, true
}

func (c *AwesomeResponseCache) setLocked(key string, response awesomeCachedResponse) {
	if response.size() > c.maxBytes {
		return
	}
	if existing, ok := c.items[key]; ok {
		c.removeLocked(existing)
	}
	item := &awesomeCacheItem{key: key, response: response, expiresAt: c.now().Add(c.ttl)}
	element := c.lru.PushFront(item)
	c.items[key] = element
	c.usedBytes += response.size()
	for c.lru.Len() > c.maxEntries || c.usedBytes > c.maxBytes {
		c.removeLocked(c.lru.Back())
	}
}

func (c *AwesomeResponseCache) removeLocked(element *list.Element) {
	if element == nil {
		return
	}
	item := element.Value.(*awesomeCacheItem)
	delete(c.items, item.key)
	c.usedBytes -= item.response.size()
	c.lru.Remove(element)
}

func (c *AwesomeResponseCache) invalidate(key string) {
	c.mu.Lock()
	c.generation[key]++
	if element, ok := c.items[key]; ok {
		c.removeLocked(element)
	}
	c.mu.Unlock()
}

// InvalidateAwesomeCatalog 使来源目录在下一次请求时从 SQLite 重建。
func (c *AwesomeResponseCache) InvalidateAwesomeCatalog() {
	c.invalidate(awesomeCatalogCacheKey)
}

// InvalidateAwesomeSource 只失效目标来源，避免一个来源同步清空其它热门快照。
func (c *AwesomeResponseCache) InvalidateAwesomeSource(sourceID string) {
	c.invalidate(awesomeEntriesCacheKey(sourceID))
}

func newAwesomeCachedResponse[T any](data T, meta *model.Meta) (awesomeCachedResponse, error) {
	payload, err := json.Marshal(model.Envelope[T]{SchemaVersion: 1, Data: data, Meta: meta})
	if err != nil {
		return awesomeCachedResponse{}, &awesomeResponseEncodingError{cause: err}
	}
	digest := sha256.Sum256(payload)
	return awesomeCachedResponse{
		payload:     payload,
		gzipPayload: gzipEncode(payload),
		etag:        `"` + hex.EncodeToString(digest[:]) + `"`,
	}, nil
}

func writeAwesomeCachedResponse(w http.ResponseWriter, r *http.Request, response awesomeCachedResponse) {
	w.Header().Set("ETag", response.etag)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Vary", "Accept-Encoding")
	if r.Header.Get("If-None-Match") == response.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if strings.Contains(strings.ToLower(r.Header.Get("Accept-Encoding")), "gzip") && len(response.gzipPayload) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(response.gzipPayload)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response.payload)
}
