# 自定义域名

生产推荐使用 Fly.io 托管域名或绑定自定义域名。

```bash
fly certs add discovery-api.starcat.dev -a starcat-discovery-api
fly certs show discovery-api.starcat.dev -a starcat-discovery-api
```

DNS 生效后，将 Starcat 客户端默认 endpoint 指向该域名。
