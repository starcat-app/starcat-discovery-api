# API 契约

所有成功响应统一为:

```json
{
  "schema_version": 1,
  "data": {},
  "meta": {}
}
```

错误响应统一为:

```json
{
  "schema_version": 1,
  "error": {
    "code": "UNAUTHORIZED",
    "message": "invalid API key"
  }
}
```

## 已实现

### `GET /healthz`

公开健康检查，返回 `ok`。

### `GET /api/v1/ping`

需要普通 API key。

### `GET /api/v1/discovery/feed`

发现流。支持分页和筛选:

| 参数 | 说明 |
|---|---|
| `page` | 页码，默认 `1` |
| `limit` | 每页数量，默认 `20`，最大 `50` |
| `topic` | 主题 code，例如 `ai`、`privacy` |
| `platform` | 平台 code，例如 `macos`、`linux` |
| `language` | 语言名或 `__uncategorized__` |
| `sort` | 可选排序，默认使用发现推荐分 |

默认使用 `discovery_score`，并记录曝光计数。显式排序值见下方通用 sort 表。

### `GET /api/v1/discovery/categories/most-popular`

热门榜单。支持 `page`、`limit`、`language`、`platform`、`topic`，并支持 `sort`:

| sort | 说明 |
|---|---|
| 空 / `popular` | 综合热门，优先读取预计算排名；ranking 缺失时按热门资格过滤后使用 `popularity_score` |
| `recommended` | 发现推荐分 |
| `activity` | 近期活跃趋势分 |
| `release` | 发布质量分 |
| `stars` / `stars_asc` | Stars 高到低 / 低到高 |
| `updated_desc` / `updated_asc` | 更新时间近到远 / 远到近 |
| `created_desc` / `created_asc` | 创建时间近到远 / 远到近 |
| `release_desc` / `release_asc` | 最新 Release 近到远 / 远到近 |
| `name_asc` / `name_desc` | 仓库名 A-Z / Z-A |

### `GET /api/v1/discovery/categories/new-releases`

新发布榜单。支持 `page`、`limit`、`language`、`platform`、`topic`，并支持 `sort`:

| sort | 说明 |
|---|---|
| 空 / `release` | 新发布默认排序，优先读取预计算排名；ranking 缺失时按新发布资格过滤后使用 `latest_release_at DESC, release_score DESC` |
| `recommended` | 发现推荐分 |
| `popular` | 综合热门分 |
| `activity` | 近期活跃趋势分 |
| `stars` / `stars_asc` | Stars 高到低 / 低到高 |
| `updated_desc` / `updated_asc` | 更新时间近到远 / 远到近 |
| `created_desc` / `created_asc` | 创建时间近到远 / 远到近 |
| `release_desc` / `release_asc` | 最新 Release 近到远 / 远到近 |
| `name_asc` / `name_desc` | 仓库名 A-Z / Z-A |

### `GET /internal/discovery/trending-candidates`

新版趋势候选诊断接口。该接口需要 Admin API Key，仅用于后端数据质量对比，不替换现有 `starcat-trending-api`，也不进入 Starcat 正式客户端链路。

### `GET /api/v1/discovery/summary`

探索 Sidebar 汇总接口。一次返回发现 / 热门 / 新发布 3 个 discovery 模式的 repo 总量和筛选项计数，客户端不需要为了左侧数字分别请求列表分页。趋势数量不来自本接口，Starcat 继续使用 `starcat-trending-api` 的语言聚合。

响应示例:

```json
{
  "schema_version": 1,
  "data": {
    "generated_at": "2026-06-30T10:00:00Z",
    "modes": [
      {
        "mode": "discover",
        "total": 1200,
        "topics": [
          { "key": "ai", "label": "人工智能", "count": 320 }
        ],
        "platforms": [
          { "key": "macos", "label": "macOS", "count": 180, "system_name": "desktopcomputer" }
        ]
      },
      {
        "mode": "popular",
        "total": 500,
        "languages": [
          { "key": "TypeScript", "label": "TypeScript", "count": 80 }
        ]
      },
      {
        "mode": "new_releases",
        "total": 300,
        "languages": [
          { "key": "Go", "label": "Go", "count": 44 }
        ]
      }
    ]
  }
}
```

说明:

- `discover` 返回 `topics` 和 `platforms`。
- `popular`、`new_releases` 返回 `languages`。
- Starcat 当前“趋势”中栏与左栏趋势语言计数仍使用 `starcat-trending-api`，不消费 discovery summary。
- 读取路径只查 SQLite 聚合，不触发 GitHub。

### `GET /api/v1/discovery/bulk`

Starcat 本地优先缓存用的全量公开 catalog 快照。该接口不接收筛选参数，返回所有 discovery repo 与 summary，客户端落本地 SQLite 后在本地完成发现 / 热门 / 新发布的筛选、排序和分页。

响应头:

| Header | 说明 |
|---|---|
| `ETag` | 弱 ETag，客户端可用 `If-None-Match` 做 304 校验 |
| `Last-Modified` | bulk 响应构建时间 |
| `Cache-Control` | `private, max-age=0, must-revalidate` |
| `Content-Encoding` | 当请求 `Accept-Encoding: gzip` 时返回 gzip |

响应 `data`:

```json
{
  "repos": [
    {
      "repo_id": 123,
      "full_name": "owner/repo",
      "stars": 12000,
      "trending_score": 72.5,
      "popularity_score": 91.4,
      "release_score": 81.2,
      "discovery_score": 88.9,
      "categories": ["popular", "new_releases"],
      "category_ranks": {
        "popular": 1,
        "new_releases": 12
      }
    }
  ],
  "summary": {
    "generated_at": "2026-06-30T10:00:00Z",
    "modes": []
  }
}
```

说明:

- 服务端内存缓存 TTL 由 `CACHE_TTL_SECONDS` 控制，默认 10800 秒（3 小时）；GitHub 同步成功后主动失效。
- bulk 读取只查 SQLite，不触发 GitHub。
- `categories` 和 `category_ranks` 来自 `category_rankings(bucket='__all__')`，Starcat 客户端按它们做热门 / 新发布本地过滤。
- 客户端仍可使用分页接口作为诊断和降级路径，但探索主链路应优先 bulk 本地缓存。

### `GET /api/v1/discovery/languages`

返回当前 discovery catalog 可用语言及数量。

### `GET /api/v1/discovery/topics`

返回业务主题元数据。

### `GET /api/v1/discovery/platforms`

返回平台元数据。

### `GET /api/v1/discovery/awesome/sources`

返回已发布的 Starcat 精选 Awesome 来源目录，按 `sort_order ASC, id ASC` 排序。需要普通 API Key，不接受用户身份或订阅参数。

响应字段包括稳定 `id`、`display_name`、canonical `repo_full_name` / `repo_url`、GitHub 仓库真实 `repo_description`、HTTPS `image_url`、中英文精选介绍、`featured`、排序、来源仓库 `source_stars`、GitHub / 外部条目计数以及内容和同步时间。`repo_description` 与 `source_stars` 均复用公共 `repos` 缓存，不在 `awesome_sources` 重复保存；每轮来源同步都会更新来源仓库 GitHub 元数据，即使 README SHA 未变化也会刷新这些字段。响应带 `ETag` 和 `Cache-Control`；`If-None-Match` 命中时直接返回 `304`，不附带 envelope。

来源目录与单来源 entries 都以 SQLite 快照作为可跨重启复用的持久缓存，并额外使用进程内响应缓存复用已编码 JSON、gzip 和 ETag。该缓存 TTL 由 `CACHE_TTL_SECONDS` 控制，默认 10800 秒；采用 64 条 / 64 MiB 双上限 LRU，同 key 并发 miss 只重建一次，来源 CRUD、同步、发布和下架会精确失效相关 key。公开响应的 `Cache-Control` 为 5 分钟，进程重启只会丢失加速层，不会丢失 Awesome 快照。

### `GET /api/v1/discovery/awesome/sources/{source_id}/entries`

返回单一已发布来源的完整 GitHub Repo 快照。外部链接和 GitHub 非 Repo 链接不进入公共响应；每条 Repo 保留 README 原始标题、描述、章节路径、顺序和安全的来源锚点。`is_archived` 是必返布尔字段，`false` 不得省略。响应带独立 `ETag`，来源不存在、未发布或从未形成可用快照时返回 `404 AWESOME_SOURCE_NOT_FOUND`。

```json
{
  "schema_version": 1,
  "data": {
    "source": {
      "id": "awesome-mac",
      "display_name": "Awesome Mac",
      "updated_at": "2026-08-24T08:00:00Z"
    },
    "entries": [
      {
        "gh_repo_id": 123456,
        "owner": "example",
        "name": "project",
        "full_name": "example/project",
        "entry_title": "Project",
        "entry_description": "Original README description",
        "section_path": ["Utilities", "File Transfer"],
        "entry_order": 42,
        "source_anchor_url": "https://github.com/example/awesome/blob/main/README.md#file-transfer"
      }
    ]
  },
  "meta": {"total": 1, "generated_at": "2026-08-24T08:00:00Z"}
}
```

### Awesome Admin API

以下接口全部要求 Admin API Key，不能使用普通 API Key：

```text
GET    /internal/discovery/awesome/sources
POST   /internal/discovery/awesome/sources
PATCH  /internal/discovery/awesome/sources/{source_id}
POST   /internal/discovery/awesome/sources/{source_id}/sync
POST   /internal/discovery/awesome/sources/{source_id}/publish
POST   /internal/discovery/awesome/sources/{source_id}/archive
GET    /internal/discovery/awesome/sources/{source_id}/sync-runs
```

- 创建时核验 kebab-case ID、HTTPS 图片、canonical GitHub Repo、公开未归档状态、默认分支和 README。
- PATCH 必须提交当前 `revision`；旧 revision 返回 `409 AWESOME_SOURCE_CONFLICT`。
- 状态机为 `draft -> ready -> published -> archived`；同步成功且至少存在一个 GitHub Repo 才能发布，已下架的成功快照可以重新发布。
- 同一来源仅允许一个 `queued/running` run；重复触发复用 active run。任务状态持久化，管理页面关闭不会取消任务。
- 成功同步在一个 SQLite 事务内 upsert Repo、替换来源关系、更新统计并完成 run；失败只记录脱敏错误，继续提供上次成功快照。
- 常规 light sync cron 会刷新全部 published 来源；管理端也可手动触发。

Awesome 稳定错误码：`AWESOME_SOURCE_INVALID`、`AWESOME_SOURCE_NOT_FOUND`、`AWESOME_SOURCE_CONFLICT`、`AWESOME_SOURCE_NOT_READY`、`AWESOME_README_UNSUPPORTED`、`GITHUB_RATE_LIMITED`、`AWESOME_SYNC_UNAVAILABLE`。

### `GET /api/v1/repos/{owner}/{repo}/star-history`

返回公开仓库星标历史。需要普通 API key，且服务端必须已显式开启 `STAR_HISTORY_ENABLED`。

查询参数：

| 参数 | 必填 | 说明 |
|---|---:|---|
| `repo_id` | 是 | 稳定 GitHub repository ID，必须为正整数 |
| `range` | 否 | `3m`、`1y` 或 `all`，默认 `1y` |

服务端会先读独立 SQLite 缓存。只有首次 miss 或缓存过期时才使用服务端 GitHub PAT 校验公开仓库、`repo_id` 与 `owner/repo`，并读取可信的 `created_at`。BigQuery 查询从 `max(repository.created_at, GH Archive 覆盖起点)` 开始，避免扫描仓库创建前的日表；随后任务进入有界 worker queue，HTTP handler 不直接执行历史查询。

缓存命中响应：

```http
HTTP/1.1 200 OK
ETag: "a1b2..."
Cache-Control: private, max-age=86400
```

```json
{
  "schema_version": 1,
  "data": {
    "repo_id": 123456,
    "full_name": "owner/repo",
    "current_stars": 42810,
    "range": "1y",
    "coverage_start": "2011-02-12",
    "generated_at": "2026-07-27T08:30:00Z",
    "points": [
      {
        "date": "2026-07-27",
        "count": 42810,
        "source": "gh_archive",
        "precision": "estimated"
      }
    ]
  },
  "meta": {
    "cache": "hit",
    "max_age_seconds": 86400
  }
}
```

客户端带匹配的 `If-None-Match` 时返回无 body 的 `304`。首次有效 miss 和已有构建任务统一返回：

```http
HTTP/1.1 202 Accepted
Retry-After: 5
```

```json
{
  "schema_version": 1,
  "error": {
    "code": "STAR_HISTORY_BUILDING",
    "message": "Star history is being prepared."
  }
}
```

固定错误语义：

| HTTP | code | 说明 |
|---:|---|---|
| `400` | `INVALID_REPOSITORY` | path、`repo_id` 或 `range` 非法 |
| `401` | `UNAUTHORIZED` | 缺少或无效 Bearer API key |
| `404` | `REPOSITORY_NOT_FOUND` | GitHub 找不到目标仓库 |
| `409` | `REPOSITORY_ID_MISMATCH` | repo ID 与 owner/name 不一致或仓库已改名 |
| `422` | `PRIVATE_REPOSITORY_UNSUPPORTED` | 私有仓库不进入公共历史链路 |
| `429` | `RATE_LIMITED` | GitHub 限流或有界构建队列已满；读取 `Retry-After` |
| `503` | `HISTORY_PROVIDER_UNAVAILABLE` | 功能未开启、负缓存命中或 Provider 暂不可用 |

`3m` 按日、`1y` 按 ISO 周、`all` 按月降采样，最多返回 `STAR_HISTORY_MAX_POINTS` 个点。`gh_archive + estimated` 是估算历史，`discovery_snapshot + snapshot` 是精确快照；两者不能混写语义。当前 M0 未获查询授权，因此部署环境必须保持 `STAR_HISTORY_ENABLED=false`，不能把代码就绪描述为真实数据链路已验收。

### `POST /internal/sync/discovery`

需要 admin API key。触发同步任务，`mode=incremental` 轻同步，`mode=full` 全量同步。
