// Package tokenpool 提供 GitHub PAT 多 token 池管理。
//
// Discovery 同步会连续调用 Search / Repo / Releases。单个 token 进入低额度或
// GitHub 返回限流时，不应该打断整轮同步；本包维护每个 token 的运行时状态，让
// 调用方可以在同一次请求内换下一个 token 重试，并按 GitHub reset 时间自动恢复。
package tokenpool

import (
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultTemporaryDisable = 60 * time.Second

// TokenState 单个 PAT 的运行时状态。
type TokenState struct {
	Value               string
	Remaining           int
	ResetAt             time.Time
	DisabledUntil       time.Time
	Dead                bool
	LastUsedAt          time.Time
	ConsecutiveFailures int
}

// Pool 保存一组 GitHub token 及其运行时配额状态。
type Pool struct {
	mu     sync.Mutex
	tokens []*TokenState
}

// New 创建 token pool，空字符串会被忽略。
func New(tokenValues []string) *Pool {
	tokens := make([]*TokenState, 0, len(tokenValues))
	for _, token := range tokenValues {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		tokens = append(tokens, &TokenState{
			Value:     token,
			Remaining: -1,
		})
	}
	log.Printf("[token-pool] loaded %d tokens from GITHUB_TOKENS env", len(tokens))
	return &Pool{tokens: tokens}
}

// Count 返回池中 token 总数，用于限制单个 GitHub 请求的最大换 token 次数。
func (p *Pool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.tokens)
}

// PickBest 选择当前最合适的 token。
//
// 到达 DisabledUntil 的 token 会在这里懒恢复；如果 reset 已过，remaining 会回到
// 未知状态，让下一次真实 GitHub 响应重新校准配额。
func (p *Pool) PickBest() *TokenState {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	var alive []*TokenState
	for _, token := range p.tokens {
		if token.Dead {
			continue
		}
		p.recoverIfReady(token, now)
		if token.DisabledUntil.After(now) {
			continue
		}
		if token.Remaining == 0 && token.ResetAt.After(now) {
			continue
		}
		alive = append(alive, token)
	}
	if len(alive) == 0 {
		return nil
	}

	var unknowns []*TokenState
	for _, token := range alive {
		if token.Remaining == -1 {
			unknowns = append(unknowns, token)
		}
	}
	if len(unknowns) > 0 {
		return unknowns[rand.Intn(len(unknowns))]
	}

	best := alive[0]
	for _, token := range alive[1:] {
		if token.Remaining > best.Remaining {
			best = token
		}
	}
	return best
}

// UpdateFromResponse 从 GitHub 响应头同步 token 状态。
func (p *Pool) UpdateFromResponse(token *TokenState, resp *http.Response) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if token == nil || resp == nil {
		return
	}
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		token.Remaining, _ = strconv.Atoi(remaining)
	}
	if reset := resp.Header.Get("X-RateLimit-Reset"); reset != "" {
		ts, _ := strconv.ParseInt(reset, 10, 64)
		token.ResetAt = time.Unix(ts, 0)
	}
	token.LastUsedAt = time.Now()

	if token.Remaining == 0 && token.ResetAt.After(token.LastUsedAt) {
		p.disableUntilLocked(token, token.ResetAt, "quota exhausted")
	}

	if resp.StatusCode == http.StatusUnauthorized {
		token.Dead = true
		log.Printf("[token-pool] %s marked DEAD (401)", maskToken(token.Value))
	}
	if resp.StatusCode >= 500 {
		token.ConsecutiveFailures++
		if token.ConsecutiveFailures >= 5 {
			token.Dead = true
			log.Printf("[token-pool] %s marked DEAD (5 consecutive 5xx)", maskToken(token.Value))
		}
	} else if resp.StatusCode < 500 {
		token.ConsecutiveFailures = 0
	}
}

// DisableUntil 临时禁用 token。until 为空或已过期时，使用短暂 backoff。
func (p *Pool) DisableUntil(token *TokenState, until time.Time, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.disableUntilLocked(token, until, reason)
}

// EarliestAvailable 返回最早可恢复的时间，用于所有 token 都不可用时生成可解释错误。
func (p *Pool) EarliestAvailable() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()

	var earliest time.Time
	now := time.Now()
	for _, token := range p.tokens {
		if token.Dead {
			continue
		}
		p.recoverIfReady(token, now)
		candidate := token.DisabledUntil
		if candidate.IsZero() && token.Remaining == 0 && token.ResetAt.After(now) {
			candidate = token.ResetAt
		}
		if candidate.IsZero() {
			return time.Time{}
		}
		if earliest.IsZero() || candidate.Before(earliest) {
			earliest = candidate
		}
	}
	return earliest
}

func (p *Pool) disableUntilLocked(token *TokenState, until time.Time, reason string) {
	if token == nil || token.Dead {
		return
	}
	now := time.Now()
	if until.IsZero() || !until.After(now) {
		until = now.Add(defaultTemporaryDisable)
	}
	if until.After(token.DisabledUntil) {
		token.DisabledUntil = until
		log.Printf("[token-pool] %s disabled until %s (%s)", maskToken(token.Value), until.Format(time.RFC3339), reason)
	}
}

func (p *Pool) recoverIfReady(token *TokenState, now time.Time) {
	if token.DisabledUntil.IsZero() || token.DisabledUntil.After(now) {
		return
	}
	token.DisabledUntil = time.Time{}
	if token.ResetAt.IsZero() || !token.ResetAt.After(now) {
		token.Remaining = -1
	}
}

func maskToken(key string) string {
	if len(key) < 16 {
		return "****"
	}
	return key[:7] + "****" + key[len(key)-4:]
}
