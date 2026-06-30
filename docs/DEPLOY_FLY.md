# Fly.io 部署

## 首次创建

```bash
fly apps create starcat-discovery-api
fly volumes create starcat_discovery_data --region nrt --size 1 -a starcat-discovery-api
```

## 配置密钥

```bash
fly secrets set \
  API_KEYS="sk-starcat-prodKey" \
  ADMIN_API_KEYS="sk-starcat-adminKey" \
  GITHUB_TOKENS="ghp_token1,ghp_token2" \
  STORE_FILE="/data/discovery.db" \
  -a starcat-discovery-api
```

## 部署

```bash
fly deploy -a starcat-discovery-api
```

## 部署前检查

```bash
go test ./...
go vet ./...
go build ./cmd/server
```

确认 `fly.toml` 中 `STORE_FILE=/data/discovery.db`，且已创建 `starcat_discovery_data` volume。SQLite 数据库必须落在 volume 上，否则机器重建后榜单缓存会丢失。

## 验证

```bash
curl https://starcat-discovery-api.fly.dev/healthz
curl -H "Authorization: Bearer $API_KEY" \
  https://starcat-discovery-api.fly.dev/api/v1/ping
curl -H "Authorization: Bearer $API_KEY" \
  "https://starcat-discovery-api.fly.dev/api/v1/discovery/categories/most-popular?limit=5"
```

## 手动同步

```bash
curl -X POST \
  -H "Authorization: Bearer $ADMIN_API_KEY" \
  "https://starcat-discovery-api.fly.dev/internal/sync/discovery?mode=incremental"
```

`mode=full` 会重新跑全量 seed，GitHub rate limit 消耗更高，只在首次冷启动或需要重建榜单时使用。

## 备份与回滚

部署前可先创建 volume snapshot：

```bash
fly volumes snapshots create vol_xxx -a starcat-discovery-api
```

应用回滚优先使用 Fly release 回滚：

```bash
fly releases -a starcat-discovery-api
fly deploy --image registry.fly.io/starcat-discovery-api:<previous-image-tag> -a starcat-discovery-api
```

如果问题来自同步数据而不是镜像，先暂停 `SYNC_ENABLED` 或移除相关 GitHub token，再从最近的 volume snapshot 恢复数据库。
