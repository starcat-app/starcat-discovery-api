# Contributing

感谢你关注 Starcat Discovery API。

## 开发流程

1. 复制 `.env.example` 为 `.env`。
2. 执行 `go mod tidy` 安装依赖。
3. 执行 `make check` 跑格式、vet 和测试。
4. 提交 PR 前确认没有提交 `.env`、数据库文件和 token。

## 代码约定

- HTTP 响应统一走 envelope。
- `/api/v1/*` 使用普通 API key，`/internal/*` 使用 admin key。
- 不在请求路径实时调用 GitHub，GitHub 数据只由后台同步写入 SQLite。
- 不采集 Starcat 用户行为或用户 GitHub token。
