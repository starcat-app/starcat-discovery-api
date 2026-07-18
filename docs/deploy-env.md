# 环境变量说明

| 变量 | 必填 | 说明 |
|---|---:|---|
| `PORT` | 否 | HTTP 端口，默认 `5006` |
| `STORE_FILE` | 否 | SQLite 文件路径 |
| `API_KEYS` | 是 | 客户端读取接口 key，逗号分隔 |
| `ADMIN_API_KEYS` | 是 | 管理接口 key，逗号分隔 |
| `GITHUB_TOKENS` | 是 | GitHub PAT 池，逗号分隔 |
| `SYNC_ENABLED` | 否 | 是否启动定时同步 |
| `SYNC_CRON` | 否 | 轻同步 cron |
| `FULL_SYNC_CRON` | 否 | 全量同步 cron |
| `CACHE_TTL_SECONDS` | 否 | `/discovery/bulk` 进程内缓存 TTL |
| `MAX_SEARCH_CALLS_PER_MINUTE` | 否 | GitHub Search API 保护阈值 |
| `RATE_LIMIT_FLOOR` | 否 | token 剩余额度低于该值时停止非必要请求 |
| `FEED_TARGET_SIZE` | 否 | 每轮 GitHub Search 的全局候选预算，默认 `500`，服务端最高限制为 `1600` |
