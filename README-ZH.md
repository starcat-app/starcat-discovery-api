# Starcat Discovery API

<!-- starcat-promo:start -->
<div align="center">
<a href="https://starcat.ink"><img src="https://raw.githubusercontent.com/dong4j/starcat-pro/main/banner.webp" width="100%" alt="Starcat" /></a>

<p><strong>这是 Starcat 探索、热门、新发布仓库 feed 的可自部署支撑服务。</strong></p>
<p>Starcat 是一款原生 macOS 应用，可以把 GitHub Stars 变成可搜索、可整理、可用 AI 理解的知识库。它支持 README 渲染、标签与私有笔记、Release 追踪、仓库健康度、AI 摘要、语义搜索、浏览器插件工作流，并提供多个可自部署 API。</p>

<a href="https://github.com/dong4j/homebrew-starcat"><img src="https://img.shields.io/badge/Install%20with-Homebrew-FBBF24?style=for-the-badge&logo=homebrew&logoColor=white" width="220" alt="Install with Homebrew"/></a>
<br/>
<sub><a href="./README.md">English</a></sub>
</div>

<div align="center">
<a href="https://starcat.ink"><img src="https://img.shields.io/badge/website-starcat.ink-38BDF8?style=flat&color=blue" alt="website"/></a>
<a href="https://github.com/dong4j/starcat-pro"><img src="https://img.shields.io/badge/support-starcat--pro-lightgrey.svg?style=flat&color=blue" alt="support"/></a>
<a href="https://github.com/dong4j/homebrew-starcat"><img src="https://img.shields.io/badge/install-homebrew-lightgrey.svg?style=flat&color=blue" alt="homebrew"/></a>
<a href="https://github.com/dong4j/starcat-localization"><img src="https://img.shields.io/badge/localization-open-lightgrey.svg?style=flat&color=blue" alt="localization"/></a>
</div>

<div align="center">
<img width="900" src="https://raw.githubusercontent.com/dong4j/starcat-pro/main/main.webp" alt="Starcat main window"/>
</div>

**首选 Homebrew 安装：**

```bash
brew tap dong4j/starcat
brew trust dong4j/starcat
brew install --cask starcat
```

**相关链接：**

- 官网: https://starcat.ink
- 下载: https://starcat.ink/downloads/Starcat-1.1.0-arm64.dmg
- 公开支持与发布说明: https://github.com/dong4j/starcat-pro
- Homebrew tap: https://github.com/dong4j/homebrew-starcat
- 浏览器插件: [Chrome](https://github.com/dong4j/starcat-chrome-plugin) / [Safari](https://github.com/dong4j/starcat-safari-plugin)
- 本地化: https://github.com/dong4j/starcat-localization

**Starcat 生态项目：**

- [starcat-sharing-api](https://github.com/dong4j/starcat-sharing-api)
- [starcat-trending-api](https://github.com/dong4j/starcat-trending-api)
- [starcat-weekly-api](https://github.com/dong4j/starcat-weekly-api)
- [starcat-wiki-api](https://github.com/dong4j/starcat-wiki-api)
- [starcat-recommend-api](https://github.com/dong4j/starcat-recommend-api)
- [starcat-discovery-api](https://github.com/dong4j/starcat-discovery-api)
- [starcat-license-api](https://github.com/dong4j/starcat-license-api)

> Starcat 为普通用户提供默认托管服务。这个 API 开源出来，是为了让进阶用户可以审查实现、本地运行，或部署自己的实例。
<!-- starcat-promo:end -->

`starcat-discovery-api` 为 Starcat 的「探索」入口提供发现流、热门榜单、新发布榜单，并保留新版趋势候选诊断接口。

本服务独立于 `starcat-trending-api`。旧趋势链路保持不变，新服务只负责 Discovery 相关能力。

## 特性

- Go 标准库 `net/http`
- 统一 envelope 响应
- 普通 API Key 与 Admin API Key 分离
- SQLite 持久化，Fly.io volume 保存数据
- GitHub PAT 池驱动 Search / repo / release ingest
- 开源友好的 README、LICENSE、CONTRIBUTING、SECURITY、Dockerfile、Fly.io 配置

## 快速开始

```bash
cp .env.example .env
# 编辑 .env，填入 API_KEYS、ADMIN_API_KEYS、GITHUB_TOKENS
go run ./cmd/server/
```

默认端口为 `5006`。

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|---|---:|---|---|
| `PORT` | 否 | `5006` | HTTP 端口 |
| `STORE_FILE` | 否 | `./discovery.db` | SQLite 文件路径 |
| `API_KEYS` | 是 | 无 | Starcat 客户端读取接口 key |
| `ADMIN_API_KEYS` | 是 | 无 | `/internal/*` 管理接口 key |
| `GITHUB_TOKENS` | 是 | 无 | GitHub PAT 池，逗号分隔 |
| `SYNC_ENABLED` | 否 | `true` | 是否启动定时同步 |
| `SYNC_CRON` | 否 | `17 */3 * * *` | 轻同步 cron |
| `FULL_SYNC_CRON` | 否 | `23 2 * * *` | 全量同步 cron |
| `CACHE_TTL_SECONDS` | 否 | `900` | `/discovery/bulk` 进程内缓存 TTL |

## API

所有 `/api/v1/*` 接口需要 `Authorization: Bearer <API_KEYS>`。

```text
GET /healthz
GET /api/v1/ping
GET /api/v1/discovery/feed
GET /api/v1/discovery/categories/most-popular
GET /api/v1/discovery/categories/new-releases
GET /api/v1/discovery/summary
GET /api/v1/discovery/bulk
GET /api/v1/discovery/languages
GET /api/v1/discovery/topics
GET /api/v1/discovery/platforms
GET /internal/discovery/trending-candidates
POST /internal/sync/discovery
```

发现 / 热门 / 新发布接口读取 SQLite 预计算结果；`/discovery/bulk` 提供 Starcat 本地优先缓存所需的完整公开 catalog 快照。管理同步入口触发 GitHub ingest 与榜单重建。趋势候选只保留在 `/internal/discovery/trending-candidates`，需要 Admin API Key，不进入 summary / bulk / Starcat UI，客户端当前仍使用既有 `starcat-trending-api`。

## 部署

```bash
fly secrets set \
  API_KEYS="sk-starcat-prodKey" \
  ADMIN_API_KEYS="sk-starcat-adminKey" \
  GITHUB_TOKENS="ghp_token1,ghp_token2" \
  STORE_FILE="/data/discovery.db" \
  -a starcat-discovery-api

fly deploy -a starcat-discovery-api
```

更多说明见 `docs/DEPLOY_FLY.md`。
