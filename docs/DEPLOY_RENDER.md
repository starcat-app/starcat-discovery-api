# Render 部署

Render 可用于临时验证，但生产推荐 Fly.io，因为 Discovery 服务需要持久化 SQLite volume。

## 基础设置

- Runtime: Docker
- Health Check Path: `/healthz`
- Environment:
  - `PORT=5006`
  - `API_KEYS`
  - `ADMIN_API_KEYS`
  - `GITHUB_TOKENS`
  - `STORE_FILE`

如果没有持久磁盘，服务重启后 SQLite 数据会丢失，只适合调试。
