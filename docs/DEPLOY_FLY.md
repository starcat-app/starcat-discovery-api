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

## 验证

```bash
curl https://starcat-discovery-api.fly.dev/healthz
curl -H "Authorization: Bearer $API_KEY" \
  https://starcat-discovery-api.fly.dev/api/v1/ping
```
