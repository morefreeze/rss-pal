# RSS Pal 订阅探索功能设计

## 目标

新增一个类似 X「探索」的一级页面，根据用户已经订阅的来源，持续产出其他值得订阅的公开 RSS/Atom/HTML 来源。页面主体展示候选来源的最近文章，候选来源本身收进自动隐藏的侧栏；用户可以直接阅读候选文章，但只有明确点击后才会创建正式订阅。

首版必须同时满足以下目标：

- 推荐主要由用户已订阅的来源决定，阅读、收藏、点赞和兴趣画像只做轻量排序修正。
- 候选来源由多个公开、持续更新的站点目录自动聚合，并由系统持续校验；管理员不负责补充候选供给。
- 探索文章和正式订阅数据严格分开，阅读探索文章绝不隐式订阅。
- 页面默认可立即打开；推荐计算和联网抓取在后台完成。
- 北京时间 08:00–24:00 的活跃时段内每 3 小时更新一次，00:00–08:00 不更新。
- 多用户的推荐画像、反馈和快照相互隔离；公开候选内容可以安全复用。

## 已确认的产品规则

### 推荐来源与信号

候选池采用两层结构：

1. 自动注册表聚合：持续同步公开 OPML、博客目录、GitHub Awesome 列表和 Reddit 主题流中的外部站点，再统一执行 feed autodiscovery 与校验。
2. 个性化关联发现：从用户相关站点、文章外链和页面中的 RSS 声明继续发现，验证通过后进入公共候选缓存。

首版内置多个相互独立的公开供给源：

- [`plenaryapp/awesome-rss-feeds`](https://github.com/plenaryapp/awesome-rss-feeds) 的分类及国家 OPML；
- [`timqian/chinese-independent-blogs`](https://github.com/timqian/chinese-independent-blogs) 的 `feed.opml`；
- [`ooh.directory`](https://ooh.directory/) 的分类、最近新增和最近更新博客；
- 经现有 RSSHub 访问的 Reddit 主题流，从持续出现的外部链接中发现博客和站点；
- 与用户高权重主题对应的 GitHub Awesome 列表，抽取其中的站点 URL 后执行 RSS autodiscovery。

这些是自动同步的 provider，不是需要管理员逐条维护的推荐清单。任何一个 provider 失效都不能阻断候选供给；系统保留每个 provider 的最后成功结果，并继续使用其他 provider。

排序信号按以下优先级使用：

1. 用户订阅源的标题、站点、分类、主题、标签和近期文章内容。
2. 候选源与上述订阅画像的相似度及对已有主题覆盖的补充价值。
3. 候选源健康度和候选文章新鲜度。
4. 阅读、收藏、点赞和兴趣画像的轻量修正。
5. 用户的显式反馈。`隐藏此源` 是硬过滤；`少推荐这类内容` 对主题施加显著负权重。

显式反馈的权重高于偶然阅读行为。系统不得因为一次点击就大幅改变推荐方向。

### 刷新时间

刷新时区固定为 `Asia/Shanghai`。每日刷新时段为：

- 08:00
- 11:00
- 14:00
- 17:00
- 20:00
- 23:00

每个时段生成一个确定的 `slot_at`。同一用户和同一 `slot_at` 最多生成一次成功快照。worker 在某个时段内晚启动时，可以补做当前时段的快照；00:00–08:00 不补做前一日 23:00 之前的时段。

页面打开、用户新增订阅或提交反馈时不在请求内同步重算。新增订阅和反馈会影响下一个定时快照；当前页面则立即应用已知的隐藏、已订阅和主题降权状态。

### 冷启动

用户没有订阅或订阅信息不足时，探索页直接展示自动注册表中通过校验、持续更新且健康度较高的来源及其最近文章，不要求先完成设置。页面同时提供兴趣标签，用户选择后从下一批快照开始调整推荐。

兴趣标签通过探索反馈存储为显式 `boost_topic`，不混入隐式行为权重。

## 方案选择

### 采用：独立探索缓存

候选来源和候选文章存放在公共探索缓存，用户只保存快照、理由、排名和反馈。明确订阅后才写入现有 `feeds` 和 `articles`。

选择该方案的原因：

- 不把候选来源伪装成隐藏订阅，数据语义与页面语义一致。
- 同一个公开来源只需抓取一次，多用户可以复用公开内容。
- 支持稳定分页、全文阅读、排序和失败回退。
- 推荐画像仍按用户隔离，不会把某位用户订阅某个小众来源这一事实暴露给其他用户。

### 未采用：隐藏订阅

后台为用户创建不可见的 `feeds` 可以少建几张表，但用户尚未确认时数据层已经完成订阅，与产品规则冲突，也会污染正式文章流、未读数、统计和 OPML 导出。

### 未采用：页面请求实时抓取

在 `GET /api/explore` 中实时发现和抓取可以避免持久化，但首屏延迟、错误率、分页稳定性和成本都不可接受，也无法可靠保留上一批可用结果。

## 系统架构

探索功能拆分为五个边界清楚的组件。

### 1. Source Profiler

输入当前用户可见的正式订阅和轻量行为信号，输出订阅画像：

- 高权重主题、标签、分类和域名；
- 已覆盖方向及可能扩展的相邻方向；
- 需要硬排除的来源和需要降权的主题。

Source Profiler 不负责发现 URL，也不直接写推荐快照。

### 2. Registry Aggregator and Source Discoverer

Registry Aggregator 持续同步外部来源清单，Source Discoverer 根据用户画像从聚合结果和关联站点中返回待校验候选。首版实现统一 provider 接口及以下适配器：

- `OPMLRegistryAdapter`：读取 versioned OPML，例如 Awesome RSS Feeds 和中文独立博客列表。
- `DirectoryAdapter`：读取带分类、最近新增或最近更新页面的公开博客目录，例如 ooh.directory。
- `RedditLinkStreamAdapter`：通过现有 RSSHub 读取内置主题 subreddit 流，统计持续出现的外部域名，再执行 feed autodiscovery；不把 Reddit 帖子本身当作博客源。
- `GitHubAwesomeAdapter`：读取与高权重主题对应的 Awesome 列表，抽取站点 URL，再发现其 RSS。
- `RelatedSiteDiscoverer`：检查相关站点首页的 `<link rel="alternate">`，并从近期相关文章的外部链接中选择有限数量的站点，再执行 feed autodiscovery。

provider 清单由版本化的默认 manifest 初始化，内容更新由上游目录持续提供，不需要管理员逐条新增订阅源。管理员只保留紧急禁用 provider、域名或恶意来源的能力，不参与普通来源验证和供给。

Registry Aggregator 在每个用户快照时段前 30 分钟同步一次，使用 ETag / Last-Modified 避免重复下载。provider 连续失败时启用退避和熔断；其最后成功 observation 仍可使用，但超过 7 天未成功同步的 provider 不再单独证明一个新来源可推荐。

首版不依赖新的第三方搜索服务。任何 adapter 只提出候选 URL，不能绕过后续安全和内容校验。

关联发现不得持久化“由哪个用户、哪篇私人文章发现”这类来源信息。公共缓存只记录通用 observation，例如 provider、provider 内的公开 key、首次/最近观察时间和公开分类。

### 3. Source Validator and Cache Fetcher

负责安全校验、RSS 解析、健康探测及公共候选文章缓存。它复用现有 RSS 抓取和正文提取能力，但使用探索专属的仓储。

新来源进入可推荐状态前必须满足：

- URL 仅允许 HTTP/HTTPS，拒绝凭据、回环地址、私网地址、链路本地地址和不安全端口。
- 每次重定向后重新解析并检查目标地址，避免 DNS rebinding 和重定向绕过。
- 请求有连接、响应和总耗时上限，并限制响应体大小。
- 能解析为受支持的 RSS、Atom 或 HTML feed。
- 规范化 URL 后不与现有候选源或用户当前可见订阅重复。
- 至少存在 2 篇可解析文章，且至少 1 篇在最近 90 天内发布；任何 provider 都不能豁免新鲜度和健康检查。
- 标题和文章 URL 合法，抓取未持续失败。

来源还必须具备可解释的外部 observation：来自结构化 OPML、博客目录或高质量 Awesome 列表，或最近 30 天内被两个独立 provider 观察到，或在同一 Reddit/相关链接流中被多个独立帖子重复引用。单次偶然外链不足以让一个来源进入冷启动池；与用户订阅直接关联的 `<link rel="alternate">` 可以作为个性化候选，但仍需通过全部安全、活跃度和内容校验。

校验失败的来源不进入用户快照。已有来源暂时抓取失败时更新健康状态，但保留上一次成功缓存；持续失败后标为不可推荐。

### 4. Explore Ranker

每个刷新时段为每位用户生成最多 12 个候选源。每个候选源最多缓存并参与本批展示最近 5 篇文章。

来源分数由以下部分组成：

- 订阅画像相似度：主权重；
- 相邻主题的覆盖增益：鼓励有依据的拓展，而不是只推荐同质来源；
- 来源健康度与内容新鲜度；
- 轻量行为修正；
- 显式兴趣增强或主题降权。

Ranker 输出候选源分数、主要主题和简短可解释理由。AI 可以辅助归纳订阅画像和生成自然语言理由，但 URL 发现、校验、过滤和最终候选集合不依赖 AI 输出。AI 不可用时，系统使用分类、关键词、域名和健康度进行确定性排序。

### 5. Explore Snapshot Publisher

把完整结果作为新快照一次性发布。生成过程先写 `pending` 批次；来源校验、文章缓存和排名全部成功后，在一个事务中写入批次来源并把批次标为 `done`。

页面只读取最新 `done` 快照。整批失败时记录失败原因并继续展示上一批成功快照，因此探索页不会因为一次 AI、网络或单源错误而变空。

## 数据模型

### `explore_registry_providers`

保存全局 provider 配置与同步状态：

- `id`、稳定 `provider_key`
- `provider_kind`：`opml`、`directory`、`reddit_stream`、`github_awesome`、`related_site`
- `endpoint`
- `topic`，可为空
- `sync_interval_minutes`
- `enabled`
- `etag`、`last_modified`
- `last_sync_at`、`last_success_at`
- `consecutive_failures`、`last_error`
- `created_at`、`updated_at`

默认 provider 由迁移中的版本化 manifest 初始化。`enabled` 用于故障隔离或安全禁用，不把管理员变成内容供给者。

### `explore_source_observations`

记录一个公开候选源被哪些 provider 观察到：

- `id`
- `source_id`，引用 `recommended_feeds`
- `provider_id`，引用 `explore_registry_providers`
- `external_key`，provider 内稳定标识
- `provider_tags`
- `first_seen_at`、`last_seen_at`
- `occurrence_count`
- 唯一键 `(provider_id, external_key, source_id)`

一个来源可以同时来自多个 provider。Ranker 使用 provider 独立性、观察新鲜度和重复出现次数评估供给置信度。

### 扩展 `recommended_feeds`

保留现有表作为全局候选源目录，新增：

| 字段 | 含义 |
| --- | --- |
| `site_url` | 来源站点首页，可为空 |
| `normalized_url` | 用于去重的规范化 feed URL，唯一 |
| `validation_status` | `pending`、`valid`、`invalid` |
| `verified_at` | 最近一次通过完整校验的时间 |
| `last_checked_at` | 最近一次健康检查时间 |
| `last_fetched_at` | 最近一次成功抓取文章时间 |
| `etag` / `last_modified` | 条件请求缓存字段 |
| `health_score` | 0–1 的健康分 |
| `last_error` | 最近错误的裁剪文本 |
| `first_discovered_at` | 首次被任一 provider 或关联发现器观察到的时间 |
| `last_observed_at` | 最近一次外部 observation 时间 |

现有 `is_broken` 字段暂时保留兼容；新逻辑以 `validation_status`、`health_score` 和有效 observation 为准。迁移时只为已有行填充规范化 URL 并把状态设为 `pending`；它们必须被自动 provider 再次观察到并由 worker 完成内容、活跃度和安全校验，不能因为历史人工录入直接成为可推荐来源。

### `explore_articles`

全局公共候选文章缓存：

- `id`
- `source_id`，引用 `recommended_feeds`
- `url`、`normalized_url`
- `title`
- `content`
- `excerpt`，优先使用 feed description，否则取正文安全截断
- `published_at`
- `fetched_at`
- `created_at`、`updated_at`
- 唯一键 `(source_id, normalized_url)`

候选文章不执行 AI 摘要、标签、收藏和分享流程。订阅后才进入正常文章处理管线。

### `explore_batches`

用户刷新批次：

- `id`
- `user_id`
- `slot_at`
- `status`：`pending`、`done`、`failed`
- `source_count`
- `error_message`
- `created_at`、`completed_at`
- 唯一键 `(user_id, slot_at)`

### `explore_batch_sources`

用户批次内的来源排名：

- `id`
- `user_id`
- `batch_id`
- `source_id`
- `rank`
- `score`
- `topic`
- `reason`
- 唯一键 `(batch_id, source_id)`

直接保存 `user_id` 以使用项目标准的 RLS 策略，并校验它与父批次所属用户一致。

### `explore_feedback`

保存可撤销的显式反馈：

- `id`
- `user_id`
- `source_id`，来源反馈时填写
- `topic`，主题反馈时填写
- `feedback_type`：`hide_source`、`dampen_topic`、`boost_topic`
- `created_at`

约束保证来源反馈只填写 `source_id`，主题反馈只填写 `topic`。撤销通过删除对应行完成。唯一索引保证相同用户不会重复创建同一种有效反馈。

### `explore_article_events`

保存轻量行为信号：

- `id`
- `user_id`
- `explore_article_id`
- `event_type`：`exposure`、`click`、`completed_read`
- `occurred_at`

这些事件只用于下一批推荐的低权重修正，不改变正式文章未读数或阅读统计。

### RLS

`explore_batches`、`explore_batch_sources`、`explore_feedback` 和 `explore_article_events` 都以 `user_id` 启用并强制 RLS，使用项目标准策略：

```sql
USING (app_rls_bypass() OR user_id = app_current_user_id())
WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id())
```

它们必须加入 `rls_leak_test.go` 和迁移烟测矩阵。`explore_registry_providers`、`explore_source_observations`、`recommended_feeds` 与 `explore_articles` 是全局公开供给或内容缓存，不保存用户画像，不启用用户 RLS；只能通过已认证 API 读取正文。

### 正式订阅 URL 唯一性

当前 `feeds.url` 是全局唯一，导致两个用户不能分别拥有同一公开来源。探索订阅必须修复这一前置问题：

- 删除 `feeds.url` 的全局唯一约束。
- 建立 `(COALESCE(owner_id, 0), url)` 唯一索引，使同一 owner 内保持幂等，同时允许不同用户订阅同一 URL。
- 现有 `owner_id IS NULL` 的共享 feed 仍保持全局一份。
- 候选过滤把用户当前可见的私有或共享 feed 都视为已订阅，不重复展示。

订阅某个已被其他用户私有订阅的 URL 时，为当前用户创建独立的 owned feed。首版接受由此产生的重复正式抓取；把正式来源和用户订阅彻底拆成多对多模型不在本次范围内。

## API 设计

所有接口都位于现有 JWT 与 RLS 中间件之下。

### `GET /api/explore`

参数：

- `limit`、`offset`
- `sort=published|captured`
- `order=asc|desc`
- `topic`，可选

响应包含：

- 最新成功快照 ID、成功时间和下次计划更新时间；
- 是否正在生成新快照；
- 是否因最近刷新失败而沿用旧快照；
- 候选文章列表；
- 是否还有下一页。

每个列表项包含来源、文章元数据、excerpt、推荐理由、主题以及来源是否已经订阅。

排序复用现有文章列表语义：

- `published` 使用现有“智能发布时间”表达式和升降序规则。
- `captured` 使用严格 `fetched_at` 排序。

实现时提取共享的 order-clause 构造逻辑，避免两处 SQL 漂移。探索页在得到严格排序结果后应用稳定的来源多样性调整：同一来源最多连续展示 2 篇；第三篇及以后移动到下一个不同来源之后。同一来源内部顺序不变。

### `GET /api/explore/sources`

返回当前快照的候选来源、排名、理由、主题、最新文章数量、健康状态、用户选择状态和已订阅状态。

### `GET /api/explore/articles/:id`

返回候选文章全文。只有当该来源在当前用户最近 30 天内的成功探索批次中出现过，或用户已经正式订阅该来源时才允许读取。页面使用独立路由 `/explore/articles/:id`。

候选文章页复用现有安全 Markdown 阅读组件，但不显示依赖正式 `article_id` 的收藏、标签、分享、AI 摘要和播放进度操作。顶部和页尾都提供明确的“订阅此来源”。

### `POST /api/explore/sources/:id/subscribe`

幂等订阅单个候选来源。事务内执行：

1. 验证来源在当前用户可见快照内且仍然有效。
2. 查找当前用户相同 URL 的正式 feed；不存在则创建 owned feed。
3. 把该来源已缓存的候选文章 upsert 到新 feed 的正式 `articles`，使订阅后立即有文章可读。
4. 正常 worker 后续继续抓取、正文补全、分类和摘要。
5. 返回 feed ID、是否新建及复制的文章数量。

若同 URL 是当前用户已可见的共享 feed，直接返回其 ID，不重复创建。

### `POST /api/explore/sources/subscribe-batch`

请求体传入来源 ID 数组。服务端逐项校验后在一个外层事务内创建或复用订阅并复制缓存文章。任一来源无权限、已失效或写入失败时整批回滚，避免 UI 显示一部分成功但无法解释。

### `POST /api/explore/feedback`

创建 `hide_source`、`dampen_topic` 或 `boost_topic`。响应返回反馈 ID，前端立即从当前页面应用结果并显示撤销 toast。

### `DELETE /api/explore/feedback/:id`

仅允许删除当前用户的反馈。撤销后当前页面恢复被移除的候选项；完整排序在下一批快照重新计算。

### `PUT /api/explore/interests`

用主题字符串数组替换当前用户的 `boost_topic` 集合。仅接受服务端允许的兴趣枚举，避免任意文本污染排名输入。

### `POST /api/explore/articles/:id/events`

记录曝光、点击或读完。服务端验证文章对当前用户可见，并对同一用户、文章和短时间窗口内的重复曝光做降噪。

## 前端设计

### 导航

- 新增一级路由 `/explore` 和详情路由 `/explore/articles/:id`。
- 桌面导航新增 `🔭 探索`。
- 移动端底栏用 `探索` 替换 `简报`；`简报` 移入现有“更多”面板。
- 页面标题和详情返回路径都使用“探索”。

### 探索文章流

主体复用现有文章卡片的视觉语言、无限滚动、预取和排序控件。默认使用 `published desc`。

探索工具栏保留：

- 主题筛选；
- 发布 / 抓取排序；
- 升序 / 降序切换。

不显示：

- 仅未读；
- 已保存；
- 全部已读；
- 正式 feed 选择器；
- 正式文章主题分组。

文章卡显示：

- 候选来源标题；
- 发布时间；
- 标题和 excerpt；
- 可用时显示缩略图；
- 推荐理由；
- `⋯` 菜单中的“隐藏此源”和“少推荐这类内容”。

### 候选源抽屉

桌面端在右侧显示收起态把手，标注候选源数量；点击后作为 overlay 抽屉展开。移动端显示悬浮入口，点击后从底部展开。抽屉默认收起，并在点击外部、按 Escape 或完成订阅后自动关闭。

抽屉中每个来源显示标题、主要主题、匹配理由、健康状态和最近文章数，并提供：

- 单个“订阅”按钮；
- 多选 checkbox；
- “订阅已选 N 个”批量按钮。

不提供默认“订阅全部”按钮，避免误操作。单个或批量订阅成功后保持页面位置，按钮变为“已订阅”；该来源的文章可以保留到当前会话结束，下一次快照会排除它。

### 反馈与撤销

提交负反馈后立即从本地列表移除对应来源或主题文章，并显示可撤销 toast。请求失败时恢复本地列表并显示错误。撤销成功后恢复本批快照中的原始位置。

### 页面状态

- 首次冷启动：展示优质源文章和兴趣标签。
- 首次快照生成中：保持冷启动内容，并显示后台优化提示。
- 普通刷新中：继续显示旧快照，不显示阻塞 loading。
- 最近刷新失败：显示非阻塞提示和最后成功时间。
- 当前无结果：解释可能由隐藏反馈或过滤导致，并提供清除筛选/反馈入口。

## 失败处理与运维

- 单个来源失败只影响该来源，不中止整个批次。
- AI 失败使用确定性画像和理由模板。
- 某个 provider 失败时使用其他 provider 和该 provider 的最后成功 observation；全部 provider 暂时失败时继续使用上一次已校验的公共候选缓存。
- 整批发布失败继续读取上一批 `done` 快照。
- 批量订阅使用事务，避免部分成功。
- 同一刷新时段由数据库唯一键防重；抢占失败的 worker 读取现有批次状态，不重复执行。
- 长时间停在 `pending` 的批次可由后续 worker 标为 `failed` 后重试，但仍受同一 slot 的单次成功约束。
- 日志记录批次 ID、用户 ID、slot、候选数量、校验丢弃原因计数和耗时，不记录完整用户画像或正文。

缓存保留策略：

- 每个候选来源保留最近 50 篇且不超过 30 天的探索文章；合法的长期低频来源可保留最新 5 篇，但仍需定期通过健康检查。
- 探索批次保留 30 天。
- 原始探索事件保留 180 天；超期事件删除。
- 删除公共候选文章前，确保它不再被保留期内的可访问批次使用。

## 测试设计

### 后端单元与仓储测试

- `Asia/Shanghai` 六个时段、跨日边界、晚启动补做和 00:00–08:00 禁止更新。
- `(user_id, slot_at)` 幂等、并发抢占、失败批次和旧快照回退。
- 来源画像权重顺序：订阅信号高于行为信号，显式反馈高于二者。
- OPML、Directory、RedditLinkStream、GitHubAwesome 和 RelatedSite adapters；ETag 条件同步、provider 退避、stale provider、`rel=alternate`、相关外链数量限制和重复 URL。
- 多 provider observation 合并、结构化目录单源准入、重复外链门槛和单个 provider 失效时持续供给。
- SSRF：私网 IP、回环、非 HTTP 协议、凭据 URL、重定向到私网和响应过大。
- RSS 校验、新鲜度无豁免、健康度退化和恢复。
- `published` / `captured` 两种排序与现有文章排序 helper 共用；来源多样性调整稳定且不打乱同源内部顺序。
- 单个与批量订阅幂等；缓存文章复制；批量失败整批回滚。
- 两个用户可以分别订阅同一 URL，共享 feed 不重复创建。
- 反馈创建、立即过滤、撤销和所有权校验。

### RLS 与 API 测试

- 四张用户探索表加入私有表泄漏矩阵。
- 用户不能读取另一用户的批次、理由、反馈和事件。
- 候选全文只有最近 30 天内曾推荐给该用户或已正式订阅时可读。
- 所有写接口拒绝越权 source、article、batch 和 feedback ID。
- 列表响应不携带完整正文，详情接口才返回 `content`。

### 前端测试

- `/explore` 和详情路由、页面标题、返回路径。
- 桌面导航新增探索；移动端探索替换简报，简报进入更多。
- 默认排序、排序切换、主题过滤、无限滚动和请求世代隔离。
- 桌面右侧抽屉、移动底部抽屉、Escape/外部点击/订阅后关闭。
- 单个订阅、选择多个、批量成功和批量失败恢复。
- 点击候选文章可阅读全文且不会自动发起订阅请求。
- 隐藏源、少推荐、撤销、请求失败恢复。
- 冷启动、后台刷新、沿用旧快照、空结果和错误提示。

### 完整验证与发布

实现完成后至少运行：

- 后端全量 `go test ./...`；
- 前端 Vitest、legacy 测试和生产构建；
- PostgreSQL 迁移烟测及 RLS 泄漏测试；
- 本地带数据库的探索刷新、文章阅读、单个/批量订阅 smoke test。

遵循仓库交付约定：完成分支验证后合并到 `master`、推送并等待腾讯部署工作流，最后核对远端 revision、容器状态、直接/公网健康、公开前端 bundle 和实际探索页面。

## 成功标准

功能上线后满足：

1. 有订阅的用户打开探索页即可看到由候选来源最近文章组成的稳定信息流。
2. 推荐理由能指出与现有订阅或相邻主题的关系，而不是泛化文案。
3. 用户可阅读全文，但没有任何路径会因阅读而自动订阅。
4. 单个和批量订阅后，来源进入正式订阅且缓存文章立即可见。
5. 隐藏源和降低主题推荐即时生效并可撤销。
6. 每日六个刷新时段准确、幂等；夜间 8 小时不更新。
7. AI、单个 provider、联网发现或单个来源失败时仍有可用结果，普通候选供给不依赖管理员补充。
8. 多用户画像不泄漏，且不同用户可订阅同一个公开 URL。

## 非目标

首版不包含：

- 新增或购买第三方网页搜索服务；
- 社交关注、用户间订阅动态或“谁也订阅了”展示；
- 自动订阅或页面打开时同步生成；
- 付费、登录后或私人 feed 的探索发现；
- 候选文章的 AI 摘要、收藏、标签、分享和播放进度；
- 正式 feed 与用户订阅的完整多对多重构；
- 探索推荐通知或邮件推送；
- 改变现有文章推荐、兴趣分析或简报算法。
