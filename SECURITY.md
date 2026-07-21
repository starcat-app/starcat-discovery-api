# Security Policy

## Supported Versions

当前项目处于 0.x 开发期，只维护最新版本。

## Reporting a Vulnerability

请通过 [GitHub Security Advisories](https://github.com/starcat-app/starcat-discovery-api/security/advisories/new) 私下报告，不要在公开 issue 中贴出 API key、GitHub PAT、数据库内容或线上日志。

## Runtime Secrets

- `API_KEYS`
- `ADMIN_API_KEYS`
- `GITHUB_TOKENS`

以上密钥必须通过环境变量或 Fly.io secrets 注入，不允许提交到仓库。
