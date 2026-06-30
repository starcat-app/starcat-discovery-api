# Starcat Discovery API

`starcat-discovery-api` 为 Starcat 的「探索」入口提供发现流、热门榜单、新发布榜单和未来新版趋势候选。

本服务独立于 `starcat-trending-api`。旧趋势链路保持不变，新服务只负责 Discovery 相关能力。

## 特性

- Go 标准库 `net/http`
- 统一 envelope 响应
- 普通 API Key 与 Admin API Key 分离
- SQLite 持久化，Fly.io volume 保存数据
- GitHub PAT 池供后续 ingest 使用
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
| `CACHE_TTL_SECONDS` | 否 | `900` | 读取接口缓存 TTL |

## API

所有 `/api/v1/*` 接口需要 `Authorization: Bearer <API_KEYS>`。

```text
GET /healthz
GET /api/v1/ping
GET /api/v1/discovery/feed
GET /api/v1/discovery/categories/most-popular
GET /api/v1/discovery/categories/new-releases
GET /api/v1/discovery/categories/trending
GET /api/v1/discovery/languages
GET /api/v1/discovery/topics
GET /api/v1/discovery/platforms
POST /internal/sync/discovery
```

当前骨架已实现 `healthz`、`ping` 与管理同步入口。发现 / 榜单读取接口将在后续闭环接入 SQLite 与 ranking。

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
