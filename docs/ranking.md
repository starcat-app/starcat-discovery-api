# Ranking 设计

Discovery 排序由后台任务预计算，HTTP 请求路径只读 SQLite 或内存缓存。

候选池在排名前生成：默认全局预算为 500，按 8 个主题均分，再拆为头部 50%、近 30 天活跃 30%、近一年新兴 20%。三路结果按 GitHub repo id 去重；全量同步裁剪未命中项目，因此候选池会滚动变化但不会无限增长。

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

## 分类资格

`category_rankings` 不是同一批 repo 按不同 score 排序，而是先判断 repo 是否有资格进入某个分类，再写入该分类榜单。

- `most-popular`: 排除 archived / fork；要求 stars 足够高或 `popularity_score` 达到热门阈值。
- `new-releases`: 排除 archived / fork；要求 180 天内 stable release、非 draft、非 prerelease，且 release 包含真实 asset。
- 新版趋势候选不写入 `category_rankings`，只保留诊断读取；Starcat 客户端趋势仍使用 `starcat-trending-api`。

`bulk` 响应中的 `categories` / `category_ranks` 从 `category_rankings(bucket='__all__')` 回填，Starcat 客户端按这些字段做本地过滤。
