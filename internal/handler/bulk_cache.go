package handler

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BulkCache 缓存 discovery bulk endpoint 的完整 JSON 响应。
//
// bulk 不分 query bucket，只有一份全量公开 catalog；用单 entry 可以避免主动刷新或
// 多客户端并发时反复扫描 SQLite、marshal JSON 和 gzip。
type BulkCache struct {
	mu    sync.RWMutex
	entry *bulkCacheEntry
	ttl   time.Duration
}

type bulkCacheEntry struct {
	payload      []byte
	gzipPayload  []byte
	etag         string
	lastModified time.Time
	builtAt      time.Time
}

const defaultBulkCacheTTL = 15 * time.Minute

// NewBulkCache 创建空 bulk cache。
//
// ttl 来自 CACHE_TTL_SECONDS。这里不再硬编码 6 小时，是为了让运维配置和实际行为一致；
// 非法值退回 15 分钟，避免缓存永久失效或意外长期持有旧 catalog。
func NewBulkCache(ttl time.Duration) *BulkCache {
	if ttl <= 0 {
		ttl = defaultBulkCacheTTL
	}
	return &BulkCache{ttl: ttl}
}

// Get 返回未过期 entry。
func (c *BulkCache) Get() (*bulkCacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.entry == nil {
		return nil, false
	}
	if time.Since(c.entry.builtAt) > c.ttl {
		return nil, false
	}
	return c.entry, true
}

// Set 写入新的 immutable entry。
func (c *BulkCache) Set(payload []byte) *bulkCacheEntry {
	now := time.Now()
	entry := &bulkCacheEntry{
		payload:      payload,
		gzipPayload:  gzipEncode(payload),
		etag:         computeBulkWeakETag(payload),
		lastModified: now,
		builtAt:      now,
	}
	c.mu.Lock()
	c.entry = entry
	c.mu.Unlock()
	return entry
}

// Invalidate 主动清空 cache，供同步完成后调用。
func (c *BulkCache) Invalidate() {
	c.mu.Lock()
	c.entry = nil
	c.mu.Unlock()
}

func writeBulkResponse(w http.ResponseWriter, r *http.Request, entry *bulkCacheEntry) {
	w.Header().Set("ETag", entry.etag)
	w.Header().Set("Last-Modified", entry.lastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Vary", "Accept-Encoding")

	if match := r.Header.Get("If-None-Match"); match != "" && match == entry.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	acceptsGzip := strings.Contains(strings.ToLower(r.Header.Get("Accept-Encoding")), "gzip")
	if acceptsGzip && len(entry.gzipPayload) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(entry.gzipPayload); err != nil {
			log.Printf("[handler] write gzipped discovery bulk payload: %v", err)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(entry.payload); err != nil {
		log.Printf("[handler] write discovery bulk payload: %v", err)
	}
}

func computeBulkWeakETag(payload []byte) string {
	sum := sha256.Sum256(payload)
	return `W/"` + hex.EncodeToString(sum[:8]) + `"`
}

func gzipEncode(payload []byte) []byte {
	var buf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil
	}
	if _, err := gw.Write(payload); err != nil {
		_ = gw.Close()
		return nil
	}
	if err := gw.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}
