# Starcat Discovery API

`starcat-discovery-api` 为 Starcat 的「探索」入口提供发现流、热门榜单、新发布榜单，并保留新版趋势候选诊断接口。

本服务独立于 `starcat-trending-api`。旧趋势链路保持不变，新服务只负责 Discovery 相关能力。

## 特性

- Go 标准库 `net/http`
- 统一 envelope 响应
- 普通 API Key 与 Admin API Key 分离
- SQLite 持久化，Fly.io volume 保存数据
- GitHub PAT 池驱动 Search / repo / release ingest
- 开源友好的 README、LICENSE、CONTRIBUTING、SECURITY、Dockerfile、Fly.io 配置

## 快速开始

```bash
cp .env.example .env
# 编辑 .env，填入 API_KEYS、ADMIN_API_KEYS、GITHUB_TOKENS
go run ./cmd/server/
```

默认端口为 `5006`。

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|---|---:|---|---|
| `PORT` | 否 | `5006` | HTTP 端口 |
| `STORE_FILE` | 否 | `./discovery.db` | SQLite 文件路径 |
| `API_KEYS` | 是 | 无 | Starcat 客户端读取接口 key |
| `ADMIN_API_KEYS` | 是 | 无 | `/internal/*` 管理接口 key |
| `GITHUB_TOKENS` | 是 | 无 | GitHub PAT 池，逗号分隔 |
| `SYNC_ENABLED` | 否 | `true` | 是否启动定时同步 |
| `SYNC_CRON` | 否 | `17 */3 * * *` | 轻同步 cron |
| `FULL_SYNC_CRON` | 否 | `23 2 * * *` | 全量同步 cron |
| `CACHE_TTL_SECONDS` | 否 | `900` | `/discovery/bulk` 进程内缓存 TTL |

## API

所有 `/api/v1/*` 接口需要 `Authorization: Bearer <API_KEYS>`。

```text
GET /healthz
GET /api/v1/ping
GET /api/v1/discovery/feed
GET /api/v1/discovery/categories/most-popular
GET /api/v1/discovery/categories/new-releases
GET /api/v1/discovery/summary
GET /api/v1/discovery/bulk
GET /api/v1/discovery/languages
GET /api/v1/discovery/topics
GET /api/v1/discovery/platforms
GET /internal/discovery/trending-candidates
POST /internal/sync/discovery
```

发现 / 热门 / 新发布接口读取 SQLite 预计算结果；`/discovery/bulk` 提供 Starcat 本地优先缓存所需的完整公开 catalog 快照。管理同步入口触发 GitHub ingest 与榜单重建。趋势候选只保留在 `/internal/discovery/trending-candidates`，需要 Admin API Key，不进入 summary / bulk / Starcat UI，客户端当前仍使用既有 `starcat-trending-api`。

## 部署

```bash
fly secrets set \
  API_KEYS="sk-starcat-prodKey" \
  ADMIN_API_KEYS="sk-starcat-adminKey" \
  GITHUB_TOKENS="ghp_token1,ghp_token2" \
  STORE_FILE="/data/discovery.db" \
  -a starcat-discovery-api

fly deploy -a starcat-discovery-api
```

更多说明见 `docs/DEPLOY_FLY.md`。
