# Changelog

## 2.0.0 - 2026-08-07

### Changed
- 导出可装配 `server` 包；依赖 `starcat-api-kit`。
- `GetRepository` 改走 kit `github.GetRepo`（Search / Releases 仍本地实现）。
- `/api/v1/ping` 改用 kit `httputil.HandlePingV1`。

## 0.1.0 - 2026-06-30

- 初始化 `starcat-discovery-api` 服务骨架。
- 新增健康检查、客户端 ping、admin sync 占位入口。
- 新增 Dockerfile、Fly.io 配置和开源项目基础文档。
