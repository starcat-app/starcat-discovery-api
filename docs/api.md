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

排序固定使用 `discovery_score`，并记录曝光计数。

### `GET /api/v1/discovery/categories/most-popular`

热门榜单。支持 `page`、`limit`、`language`、`platform`、`topic`，并支持 `sort`:

| sort | 说明 |
|---|---|
| 空 | 综合热门，使用 `popularity_score` |
| `stars` | 按 stars/search 热度 |
| `activity` | 按近期活跃趋势 |

### `GET /api/v1/discovery/categories/new-releases`

新发布榜单。支持 `page`、`limit`、`language`、`platform`、`topic`，并支持 `sort`:

| sort | 说明 |
|---|---|
| 空 | 综合发布质量，使用 `release_score` |
| `stars` | 按 stars/search 热度 |
| `updated` | 按近期更新活跃度 |

### `GET /api/v1/discovery/categories/trending`

新版趋势候选接口。首期用于后端数据质量对比，不替换现有 `starcat-trending-api`。

### `GET /api/v1/discovery/languages`

返回当前 discovery catalog 可用语言及数量。

### `GET /api/v1/discovery/topics`

返回业务主题元数据。

### `GET /api/v1/discovery/platforms`

返回平台元数据。

### `POST /internal/sync/discovery`

需要 admin API key。触发同步任务，`mode=incremental` 轻同步，`mode=full` 全量同步。
