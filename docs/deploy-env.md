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
| `CACHE_TTL_SECONDS` | 否 | `/discovery/bulk` 进程内缓存 TTL，默认 `10800` 秒（3 小时） |
| `MAX_SEARCH_CALLS_PER_MINUTE` | 否 | GitHub Search API 保护阈值 |
| `RATE_LIMIT_FLOOR` | 否 | token 剩余额度低于该值时停止非必要请求 |
| `FEED_TARGET_SIZE` | 否 | 每轮 GitHub Search 的全局候选预算，默认 `500`，服务端最高限制为 `1600` |
| `STAR_HISTORY_ENABLED` | 否 | 星标历史总开关，默认 `false` |
| `STAR_HISTORY_CACHE_TTL_SECONDS` | 否 | 成功缓存 TTL，默认 `86400` 秒 |
| `STAR_HISTORY_NEGATIVE_TTL_SECONDS` | 否 | 失败负缓存 TTL，默认 `600` 秒 |
| `STAR_HISTORY_BUILD_TIMEOUT_SECONDS` | 否 | worker 单次构建超时，默认 `300` 秒 |
| `STAR_HISTORY_WORKER_CONCURRENCY` | 否 | 固定 worker 数量，默认 `1` |
| `STAR_HISTORY_QUEUE_CAPACITY` | 否 | 有界队列容量，默认 `32` |
| `STAR_HISTORY_MAX_POINTS` | 否 | 单序列返回上限，默认 `500` |
| `GCP_PROJECT_ID` | 开启时 | BigQuery 查询计费项目 |
| `GOOGLE_APPLICATION_CREDENTIALS_JSON` | 否 | service account JSON；留空使用 ADC，只能通过 secret 注入 |
| `BIGQUERY_MAX_BYTES_BILLED` | 开启时 | 单仓查询扫描字节硬上限，必须为正整数 |
| `STAR_HISTORY_DAILY_MAX_BYTES_BILLED` | 开启时 | 按 UTC 日期持久化的每日保守预算，服务重启后继续累计，且必须不小于单次上限 |

## 星标历史启用门禁

`STAR_HISTORY_ENABLED` 默认必须保持 `false`。只有 GH Archive / BigQuery M0 已完成表结构、覆盖范围、扫描字节、耗时和费用验证，并获得单独上线授权后，才可在对应环境配置 GCP 项目、凭据与两个预算值。普通开发、测试或部署不得用猜测值开启。

单仓首次构建的查询起点取 `max(repository.created_at, GH Archive 覆盖起点)`；M0 预算必须按裁剪后的真实 dry run 扫描量制定，不能继续按所有仓库统一扫描完整归档历史估算。

服务不会接收或记录用户 GitHub token、Star 列表或仓库访问顺序；GitHub 校验和 BigQuery 查询只使用服务端凭据。
