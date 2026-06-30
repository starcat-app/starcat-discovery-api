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

### `GET /api/v1/discovery/categories/trending`

新版趋势候选接口。首期用于后端数据质量对比，不替换现有 `starcat-trending-api`。

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

- 服务端内存缓存 6 小时，GitHub 同步成功后主动失效。
- bulk 读取只查 SQLite，不触发 GitHub。
- `categories` 和 `category_ranks` 来自 `category_rankings(bucket='__all__')`，Starcat 客户端按它们做热门 / 新发布本地过滤。
- 客户端仍可使用分页接口作为诊断和降级路径，但探索主链路应优先 bulk 本地缓存。

### `GET /api/v1/discovery/languages`

返回当前 discovery catalog 可用语言及数量。

### `GET /api/v1/discovery/topics`

返回业务主题元数据。

### `GET /api/v1/discovery/platforms`

返回平台元数据。

### `POST /internal/sync/discovery`

需要 admin API key。触发同步任务，`mode=incremental` 轻同步，`mode=full` 全量同步。
