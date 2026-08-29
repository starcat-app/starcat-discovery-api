# AGENTS.md — starcat-discovery-api

> **唯一协作规范源**：本仓库根目录 `AGENTS.md` 是本项目协作规范的唯一正文维护源。
> 开工前还必须阅读并遵守上级 [`../AGENTS.md`](../AGENTS.md) 的跨仓规则。

## 项目概述

Starcat 探索发现后端：Discovery feed、热门/新发布榜单、Awesome 精选来源（GFM 解析与持久化同步）、可选公开仓库星标历史缓存（BigQuery）。**独立于** `starcat-trending-api`，不替代旧 Trending 链路。生产经 `starcat-api` 聚合部署。

## 技术栈

- Go 1.25.0 · `net/http`
- `modernc.org/sqlite` · `github.com/robfig/cron/v3`
- `golang.org/x/oauth2`（Star History / GCP）
- `github.com/yuin/goldmark`（Awesome 解析）
- `github.com/starcat-app/starcat-api-kit` v0.3.0

## 关键目录

```
cmd/server/
server/
internal/ingest/      # GitHub Search / release 同步
internal/awesome/     # Awesome 来源解析与 normalize
internal/starhistory/ # BigQuery 星标历史（默认关闭）
internal/store/       # SQLite 持久化
internal/scheduler/
docs/api.md           # API 说明
docs/ranking.md       # 排序规则
Makefile              # VERSION := 0.1.0
```

## 开发与测试命令

```bash
cp .env.example .env          # API_KEYS、ADMIN_API_KEYS、GITHUB_TOKENS 必填
make deps && make run         # PORT=5006
make build
make check                    # fmt-check + vet + test（无 coverage target）
make docker-build
```

CI（`.github/workflows/ci.yml`）：`make deps` · `make check` · `docker build` · `make build`。

环境变量见 `.env.example` 与 README：`STORE_FILE`、`METRICS_STORE_FILE`、`SYNC_*`、`CACHE_TTL_SECONDS`、`STAR_HISTORY_*`（默认 `STAR_HISTORY_ENABLED=false`）。

## 代码与架构约束

- **双 Key**：`API_KEYS` 与 `ADMIN_API_KEYS` 必须分离；**Discovery 不继承 Weekly 的 ADMIN_API_KEYS**。
- **鉴权边界**：
  - `/api/v1/*`：Bearer `API_KEYS`
  - `/internal/stats`、`GET /internal/metrics/*`：Bearer `API_KEYS`（运营只读指标）
  - 使用 `ADMIN_API_KEYS` 的管理接口：
    - **写/触发**：`POST /internal/sync/discovery`；Awesome 来源创建/更新/同步/发布/归档（`POST`/`PATCH /internal/discovery/awesome/*`）
    - **只读**：`GET /internal/sync-runs`、`GET /internal/discovery/trending-candidates`、Awesome 管理侧列表/同步记录查询
  - `/healthz` 公开
- **Star History**：默认关闭；`STAR_HISTORY_ENABLED=true` 时必填 `GCP_PROJECT_ID`、`BIGQUERY_MAX_BYTES_BILLED`、`STAR_HISTORY_DAILY_MAX_BYTES_BILLED`（须先完成 M0 验证与预算配置）。认证为 `GOOGLE_APPLICATION_CREDENTIALS_JSON`（service account JSON）**或**受信任 ADC 二选一，JSON 留空时走 ADC，不得写 JSON 必填。
- **Awesome**：独立 ETag 快照；同步任务持久化，勿破坏已有 migration。
- GitHub 调用走 PAT 池；不接受用户 token 入库。

## 安全与数据边界

- 禁止入库：`.env`、`discovery.db`、`discovery-metrics.db`、`bin/`。
- BigQuery 凭据仅通过 Fly secrets / 环境变量注入，禁止写进代码或 `.env.example` 真实值。
- 日志脱敏 API Key 与 PAT。

## 部署与发布禁令

未经 dong4j 明确授权，禁止：`make release`、`scripts/deploy.sh`、`fly deploy`、`git push`/`git tag`、启用生产 BigQuery 查询。生产 Fly 仅经 `starcat-api`。
