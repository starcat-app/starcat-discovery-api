# Ranking 设计

Discovery 排序由后台任务预计算，HTTP 请求路径只读 SQLite 或内存缓存。

## 首期分数

- `popularity_score`: stars、forks、release downloads、活跃度、质量门禁。
- `release_score`: latest stable release 时间、asset 平台完整度、stars 兜底。
- `discovery_score`: 近期活跃、主题匹配、平台匹配、多样性。
- `trending_score`: 预留给后续新版趋势对比，不替换当前 `starcat-trending-api`。

## 质量门禁

- archived repo 降权或排除。
- fork repo 默认不进入新发布首屏。
- prerelease / draft release 不作为新发布主信号。
- 无实质 release asset 的项目降低新发布权重。
