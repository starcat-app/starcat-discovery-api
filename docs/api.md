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

### `POST /internal/sync/discovery`

需要 admin API key。骨架阶段返回占位 accepted，后续接入实际同步任务。

## 待接入

- `GET /api/v1/discovery/feed`
- `GET /api/v1/discovery/categories/most-popular`
- `GET /api/v1/discovery/categories/new-releases`
- `GET /api/v1/discovery/categories/trending`
- `GET /api/v1/discovery/languages`
- `GET /api/v1/discovery/topics`
- `GET /api/v1/discovery/platforms`
