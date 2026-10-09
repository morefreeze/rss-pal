# Reddit 浏览器探索

从插件 1.8.6 起，四个 subreddit 的 week/month top100 榜单由浏览器采集。服务端不再直接抓取这8个种子，仍负责博客 RSS 验证、探索候选去重与文章处理。

1. 安装/重新加载 RSS Pal 1.8.6 插件，配置 HTTPS 服务地址与管理员 bookmarklet token。
2. 在同一浏览器配置中登录 Reddit，确认能正常访问相应 subreddit。
3. 打开插件中的“Reddit 探索 · ≥100 分”，点击“立即探索”。每分钟处理一个榜单，完整一轮约8分钟。
4. 可勾选每6小时自动探索，默认关闭。浏览器退出时无法采集，下次运行继续按持久化状态调度。
5. 状态中的“候选入队”表示外链待RSS验证，不等于最终有效订阅源。403、超时、登录页均显示失败，不能解释为没有候选。

列表固定为 programming、MachineLearning、LocalLLaMA、artificial。分数缺失或低于100跳过；服务端再次执行门槛与外链过滤，管理员可保留更高的既有门槛。没有OAuth令牌或Cookie上传；上传仅含公开帖ID、分数、subreddit、外链及排除标志。Browser读取依赖当前会话和Reddit访问条件，并不代表API授权获批。

接口：`POST /api/extension/reddit-discovery`，Bearer bookmarklet token，必须管理员。请求体最多256KiB，每批最多100帖子，抓取时间有效期12小时。候选、观察、验证队列与成功时间以一个事务保存；同时间或旧批次重传不重复入队。禁用provider返回409。插件暂存失败上传每5分钟重试；切换服务器/Token清除旧身份的待上传批次。

迁移052添加 `explore_registry_providers.browser_only`，把既有8个Reddit provider切换为浏览器运输，保留门槛和启用状态。原050迁移及其只读CLI仍保留供诊断使用。

---

## 早期服务器试跑记录

# Reddit exploration seeds

The worker scans `programming`, `MachineLearning`, `LocalLLaMA`, and `artificial` every six hours. Each has a weekly and monthly top listing, limited to the first 100 posts. These are bounded samples, not a complete Reddit archive.

`reddit_top` requires Reddit's numeric `score >= 100`. Missing scores fail closed. Self posts, videos, galleries, NSFW/promoted posts, Reddit links, common media/repository/paper platforms and unsafe URLs are excluded. Remaining original article URLs enter the existing RSS discovery, recent-content and feed validation pipeline; an external URL is not proof of a personal blog or usable feed. No user subscription is created.

Repeated post IDs are counted once. Multiple qualifying posts pointing to the same article retain the highest-score post's provenance and a real occurrence count. The feed registry subsequently merges canonical RSS sources. A single qualifying post supplies confidence; no fake repeat count is used. Source observations retain the representative Reddit post URL, score and subreddit in their public tags.

The threshold is per provider: the endpoint fragment `#min_score=100` is local adapter configuration and is not sent to Reddit. Only positive integers are accepted. Operators changing the threshold should reset that provider's conditional-fetch validators and schedule it for a fresh sync. The old unscored `reddit-programming` RSSHub seed is disabled; the XML adapter remains for compatibility.

## Read-only trial

From `backend/`:

```sh
go run ./cmd/reddit-explore-trial -min-score 100 -save-dir /tmp/reddit-listings > /tmp/reddit-trial.json
go run ./cmd/reddit-explore-trial -min-score 100 -input-dir /tmp/reddit-listings > /tmp/reddit-replay.json
```

The command does not connect to the database. It uses the production provider client, parser and RSS source validator. It reports per-listing errors and pagination availability, distinct qualifying post IDs, eligible external post IDs, external hosts and verified feed URLs. Distinct counts combine only successfully fetched listings; `successful_listings < 8` makes the run partial and exits nonzero. Zero counters in a wholly failed run do **not** mean no qualifying posts exist. `-validate=false` explicitly skips feed validation.

## Trial on 2026-10-08 (Asia/Shanghai)

- Local production client: 0/8 listings fetched; all returned HTTP 403.
- Tencent worker network: 0/8 listings fetched; all eight timed out after the provider client's 20-second deadline.
- Independent Tencent RSSHub probe: original route returned HTTP 404; direct Reddit JSON through its existing outbound proxy returned HTTP 403.
- OpenCLI probe could not connect to its browser extension; the agent-browser fallback displayed Reddit's network-security block and requested a login or developer token.
- Consequently, the number of posts/sites/feeds discoverable at 100 points is **unknown**, not zero. The user confirmed no Reddit API authorization is currently available.
- Synthetic replay (explicitly not live data): all eight fixture listings processed; scores 99/100/101 yielded two distinct qualifying external posts/sites after cross-list deduplication.

Reddit may require approved API access. Credentials are not embedded, copied from browser sessions or sent to unrelated hosts. Fetch failures remain visible in provider state and use the existing exponential backoff; no fallback silently removes the score requirement.
