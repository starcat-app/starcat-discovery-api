// Package tokenpool 提供 GitHub PAT 多 token 池管理。
//
// 实现已收敛到 starcat-api-kit；本包保留原 import 路径供 github / ingest 使用。
package tokenpool

import kittokenpool "github.com/starcat-app/starcat-api-kit/tokenpool"

// TokenState 单个 PAT 的运行时状态。
type TokenState = kittokenpool.TokenState

// Pool GitHub Token 池。
type Pool = kittokenpool.Pool

// New 从 token 字符串列表创建 Pool。
func New(tokenValues []string) *Pool {
	return kittokenpool.New(tokenValues)
}
