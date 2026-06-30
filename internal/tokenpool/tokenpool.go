// Package tokenpool 提供 GitHub PAT 的简单轮询池。
//
// Discovery 同步会连续调用 Search / Repo / Releases，单 token 很容易遇到配额瓶颈；
// 轮询池让服务可以通过多个只读 PAT 分摊请求。
package tokenpool

import (
	"strings"
	"sync"
)

// Pool 保存一组 GitHub token，并按请求轮询返回。
type Pool struct {
	mu     sync.Mutex
	tokens []string
	next   int
}

// New 创建 token pool，空字符串会被忽略。
func New(tokens []string) *Pool {
	cleaned := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token != "" {
			cleaned = append(cleaned, token)
		}
	}
	return &Pool{tokens: cleaned}
}

// Next 返回下一个 token。没有可用 token 时 ok=false。
func (p *Pool) Next() (token string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tokens) == 0 {
		return "", false
	}
	token = p.tokens[p.next%len(p.tokens)]
	p.next = (p.next + 1) % len(p.tokens)
	return token, true
}
