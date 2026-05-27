# 浏览器扩展 adapter 平台 + Twitter 流式抓取 MVP 设计

## 背景

RSS Pal 当前的浏览器扩展（`extension/`）做的事是「单页 push」：用户在一篇文章页面（公众号、博客、付费墙文章…）点扩展图标，扩展把当前页 HTML 送到 `POST /api/bookmarklet/capture`，后端走通用提取或站点专用提取（如 `rss/twitter.go`）入库。

这套模型对**单篇长文**很合适，对**流式短内容**（Twitter timeline、小红书发现页、AI 对话历史）失效：

- 一次只能投一篇，逐条手动点不现实
- 浏览器扩展 popup 是单按钮 UX，没有 source / batch 概念
- 后端 `/api/bookmarklet/capture` 接 `{url, title, html}` 一篇，没有 batch 入口
- 数据模型上 `articles` 表全是「文章」假设（必有 title、长内容），短推塞进去会让标题列像内容预览

OpenCLI（`github.com/jackwener/opencli`）证明了「借用用户已登录的真 Chrome 抓取强反爬内容」这条路是工程上成立的，并且它的 per-site adapter 目录结构（`clis/<site>/<command>.js`）值得参考。

这份 spec 把 rss-pal 扩展从「单 popup 单文件」升级到「per-site adapter 目录」架构，并以 Twitter 作为第一个流式源的样板实现。其它流式源（小红书、付费墙、微信读书、AI 对话历史）按相同模板加 adapter，每个独立 spec。

## 目标与非目标

**目标**

- rss-pal 扩展支持 per-site adapter 注册（`extension/adapters/<site>/<command>.js`），形状刻意与 OpenCLI 对齐，便于后续抄上游 selector 修复
- Twitter 4 个 adapter：
  - **T2 list-tweets** — 寄生在用户打开的 `x.com/i/lists/<id>` tab，提取 list timeline 推文
  - **T3 tweets (user)** — 寄生在用户打开的 `x.com/<user>` profile tab，提取该用户最近推文
  - **T4 bookmarks** — 寄生在 `x.com/i/bookmarks` tab，提取用户书签
  - **T6 thread (push)** — 复用现有 `bookmarklet/capture` + `rss/twitter.go ExtractTweet` 路径，仅补 `kind='tweet'` 标签
- 双触发模式：
  - **R4 寄生**（主路径）— content script 注入 x.com 所有页面，匹配 URL pattern 后自动 extract → 入投递队列
  - **R1 手动补刀**（辅助）— popup 加「刷新 source」下拉，选某 list / 某账号 / bookmarks → 后台 tab 打开 + extract + 关闭
- 后端 batch ingest 入口 `POST /api/extension/ingest`，按 `source_kind` 路由到 normalizer
- 数据模型：`articles.kind`（驱动前端渲染）+ `feeds.kind`（驱动 source 类型分组）；零数据迁移破坏
- 前端：`<TweetCard>` 组件，按 `article.kind` 在列表/详情切渲染
- 维护 SOP：`docs/extension-adapters/upstream-map.md` 对照表 + `scripts/check-upstream-adapters.sh` 月度巡检

**非目标**

- 公众号图片质量问题（C4a/b/c）— 独立 hotfix spec
- 其它流式源（G2 小红书、G3 付费墙、G5 微信读书、G6 AI 对话）— 每个独立 spec
- Twitter 写操作（发推、点赞、回复、关注）— 永远非目标
- 多 x.com 账号切换 — 一个 Chrome profile 一套登录态即可
- Twitter Home timeline 抓取 — 噪音太高，默认关；将来用户可在 options 里开
- 服务端通过 syndication API / Nitter 抓取未登录推文 — 用户没登录就别抓
- 视频/GIF 推文播放 — `<video>` 在 markdown 渲染不支持，沿用现有 `tweetPhoto` 选择器
- 移植 OpenCLI 任何运行时代码（daemon、CDP bridge）— Z 方案核心是「形状对齐，实现独立」

## 架构

### 双路径数据流

```
[R4 寄生 — 主路径]
用户浏览 x.com tab (任何页面)
    └─ rss-pal extension content script 注入 (manifest matches: *://x.com/*)
         └─ adapter registry 查找匹配的 adapter
              │   匹配条件: site === 'twitter' && urlPattern.test(location.pathname)
              ├─ /i/lists/:id          → adapters/twitter/list-tweets.js
              ├─ /i/bookmarks          → adapters/twitter/bookmarks.js
              ├─ /:user (排除 status/) → adapters/twitter/tweets.js
              └─ /:user/status/:id     → 不归本 spec 管 (走现有 bookmarklet path)
                   ↓
              adapter.extract(document) → { items: TweetItem[], source_id, hasMore }
                   ↓
              queue.push(source_kind, source_id, items)
                   ↓
              (滚动事件) 增量 extract → 同一 queue，按 tweet id 去重
                   ↓
              background.js 定时 flush (30s 或 queue >= 20 条)
                   ↓
              POST /api/extension/ingest ──────────────┐
                                                       │
[R1 手动补刀]                                          │
扩展 popup → "刷新 source" 下拉                        │
    └─ 选项: 已注册的 source (list X / @user Y / bookmarks)
         └─ chrome.tabs.create({url, active: false})
              └─ tab 加载完成 → 注入 adapter → extract → queue → flush
                   └─ chrome.tabs.remove(tabId) ───────┘
                                                       │
                                                       ▼
                                后端 IngestHandler.Ingest
                                  └─ 校验 source_kind 合法
                                       └─ feeds.UpsertByKindAndSource(kind, source_id)
                                            └─ for each item:
                                                  twitter.Normalize(item) → model.Article
                                                       └─ FindByOwnerAndURL → 命中 update, 未命中 Create (kind='tweet')
                                                            └─ existing worker pipeline:
                                                                AI summary / classify / rec score
                                                                    └─ frontend ArticleListItem
                                                                         └─ kind === 'tweet'
                                                                              → <TweetCard>
```

### 关键设计原则

- **DOM 提取脚本是 OpenCLI 可共享层** — `extract()` 内部的 `document.querySelector` 逻辑可以从 OpenCLI 对应 adapter 抄过来，因为我们也跑在用户真 Chrome 的 page context
- **基础设施完全独立** — chrome.scripting / chrome.storage / popup / options 都是 Chrome 原生扩展 API，跟 OpenCLI 的 daemon/CDP 体系无关
- **adapter 接口对齐 OpenCLI 形状但不耦合代码** — 我们的 adapter 模块自己 export 一个对象（不依赖 OpenCLI 的 `cli({...})` 注册器），日后 OpenCLI 升级它的注册协议跟我们无关

## 数据模型变更

### Migration 023

```sql
-- backend/migrations/023_add_article_kind.sql
ALTER TABLE articles ADD COLUMN kind TEXT NOT NULL DEFAULT 'article';
CREATE INDEX idx_articles_kind ON articles(kind) WHERE kind != 'article';
```

`kind` 取值：

| 值 | 含义 | 出现来源 |
|---|---|---|
| `article`（默认） | 普通文章 / 公众号 / 博客 / PDF | 现有所有路径 |
| `tweet` | 单条推文 | 新 ingest path（T2/T3/T4）+ 现有 bookmarklet path（T6，迁移） |
| `tweet_thread` | 多条推文聚合（未来用） | 暂留，不在 MVP 实现 |

部分索引（`WHERE kind != 'article'`）避免索引膨胀；普通 article 不需要按 kind 查。

### Migration 024

```sql
-- backend/migrations/024_add_feed_kind.sql
ALTER TABLE feeds ADD COLUMN kind TEXT NOT NULL DEFAULT 'rss';
ALTER TABLE feeds ADD COLUMN provider_source_id TEXT;
CREATE UNIQUE INDEX idx_feeds_kind_source ON feeds(owner_id, kind, provider_source_id)
    WHERE provider_source_id IS NOT NULL;
```

`feeds.kind` 取值：

| 值 | 含义 | provider_source_id |
|---|---|---|
| `rss`（默认） | RSS/Atom 订阅 | NULL（用现有 url 字段） |
| `html` | HTML scraper | NULL |
| `twitter:list` | Twitter list | list 的数字 id |
| `twitter:user` | 某用户的 timeline | handle（小写） |
| `twitter:bookmarks` | 用户的 bookmarks | 用户自己的 numeric id |
| `saved` | 现有 saved feed | NULL |

部分唯一索引保证同一用户 + 同 kind + 同 source 只有一个 feed row。

## 后端组件

### 1. Ingest handler

新文件 `backend/internal/api/extension_ingest.go`：

```go
type ExtensionIngestHandler struct {
    feedRepo    *repository.FeedRepository
    articleRepo *repository.ArticleRepository
    normalizers map[string]Normalizer
}

type IngestRequest struct {
    SourceKind string          `json:"source_kind"` // e.g. "twitter:list"
    SourceID   string          `json:"source_id"`   // list id / handle / "self" for bookmarks
    SourceName string          `json:"source_name"` // human-readable, used when first creating feed
    Items      []json.RawMessage `json:"items"`     // adapter-defined shape, normalizer decodes
}

type IngestResponse struct {
    Accepted int      `json:"accepted"`
    Skipped  int      `json:"skipped"` // duplicates
    Errors   []string `json:"errors"`
}

// POST /api/extension/ingest
func (h *ExtensionIngestHandler) Ingest(c *gin.Context)
```

路由注册：现有 `cmd/server/main.go` 加一行 `r.POST("/api/extension/ingest", ext.Ingest)`，复用现有 JWT auth middleware。

### 2. Normalizer 接口

```go
// backend/internal/extension/normalizer/normalizer.go
type Normalizer interface {
    // SourceKindPrefix returns the kind prefix this normalizer handles, e.g. "twitter:".
    SourceKindPrefix() string

    // Normalize turns one adapter-emitted item into an Article, ready to upsert.
    Normalize(item json.RawMessage, feed *model.Feed) (*model.Article, error)
}
```

注册：在 handler 构造时 `normalizers["twitter:"] = twitter.NewNormalizer()`，按前缀匹配（`twitter:list` / `twitter:user` / `twitter:bookmarks` 都路由到同一个）。

### 3. Twitter normalizer

新文件 `backend/internal/extension/normalizer/twitter.go`：

```go
type TweetItem struct {
    ID          string    `json:"id"`           // numeric tweet id (dedup key)
    Author      string    `json:"author"`       // handle, lowercase
    DisplayName string    `json:"display_name"`
    Text        string    `json:"text"`         // already markdown-ish per extractor
    CreatedAt   time.Time `json:"created_at"`   // RFC3339 from <time datetime=...>
    URL         string    `json:"url"`          // https://x.com/<user>/status/<id>
    MediaURLs   []string  `json:"media_urls"`
    QuotedURL   string    `json:"quoted_url,omitempty"`
    Likes       int       `json:"likes"`
    Retweets    int       `json:"retweets"`
    Replies     int       `json:"replies"`
    Views       int       `json:"views,omitempty"`
}

func (n *TwitterNormalizer) Normalize(raw json.RawMessage, feed *model.Feed) (*model.Article, error)
```

文章字段映射（与现有 `bookmarklet.go` capture path 出来的 article 形态保持一致）：

| `articles` 列 | 来源 |
|---|---|
| `url` | `item.URL`，已是 normalized `x.com/<user>/status/<id>` 形态 |
| `title` | `buildTweetTitle(item)` |
| `content` | `buildTweetContent(item)` —— 第一行是 byline blockquote `> @handle (DisplayName) · YYYY-MM-DD`，后接 text、images、引用链接 |
| `published_at` | `item.CreatedAt` |
| `kind` | `'tweet'` |
| `feed_id` | 入参 feed |
| `is_clip` | `true`（与 bookmarklet path 行为一致） |
| `summary_brief`, `summary_detailed` | NULL（zero value），worker 异步补 |

**`articles` 表没有 author 列** —— author/displayName 信息通过 `buildTweetContent` 嵌入 content 的首行 blockquote，前端 `<TweetCard>` 解析 content 拿 byline 用于头部展示。这与现有 bookmarklet twitter path 的行为完全一致，零数据格式变化。

**关键：抽出 `buildTweet{Title,Author,Content}` 到共享包** —— 这三个函数现在在 `backend/internal/api/bookmarklet.go` 里，挪到 `backend/internal/rss/twitter_format.go`（与 `twitter.go` 同包），让现有 bookmarklet path 和新 ingest path 都用。这是必要的去重，否则两套代码会漂移。

### 4. Feed upsert 逻辑

新方法 `repository.FeedRepository.GetOrCreateByKindAndSource(ownerID int, kind string, sourceID, displayName string) (*model.Feed, error)`：

- 先 `SELECT ... WHERE owner_id=$1 AND kind=$2 AND provider_source_id=$3`
- 命中 → 返回现有 feed
- 未命中 → `INSERT`，`name` 取 `displayName`，`url` 留空字符串（twitter feed 没有 RSS URL），`active=true`

命名风格对齐现有 `GetOrCreateClipFeed`（即 bookmarklet 路径用的那个），保持仓库接口一致。

### Article upsert 行为

`extension_ingest.go` 自己拼装 upsert，复用现有 `articleRepo` 的 `FindByOwnerAndURL` + `Create` + `UpdateContent`（不新增 repo 方法）：

```go
existing, _ := articleRepo.FindByOwnerAndURL(ownerID, item.URL)
if existing != nil {
    // 已有相同 URL 的 article（不区分 kind） → 跳过 or 选择性更新
    // MVP 行为：跳过（即返回 skipped++）。未来可加 force-overwrite
    skipped++
    continue
}
articleRepo.Create(&model.Article{...kind: "tweet", ...})
accepted++
```

理由：MVP 不打算让 R4 寄生路径覆盖用户已有的手动 capture；用户先 popup 投了一篇 tweet，再 R4 扫到同一条，老的就不动，保留用户可能加过的 tag / saved 状态。

### 5. Bookmarklet path 的 kind='tweet' 回灌

修改 `backend/internal/api/bookmarklet.go`：

- 在 `IsTwitterStatusURL && ExtractTweet 成功` 分支，构造 article 时 `kind: "tweet"`（而不是默认 `article`）
- 旧数据迁移：写一个一次性 SQL（直接 inline 在 migration 023 里），把已经匹配 `url ~ '^https://x\.com/[^/]+/status/[0-9]+$'` 的 article 改成 `kind='tweet'`：

```sql
-- 023_add_article_kind.sql 末尾追加
UPDATE articles SET kind = 'tweet'
WHERE url ~ '^https://x\.com/[^/]+/status/[0-9]+$';
```

## 扩展组件

### 1. Adapter registry

新文件 `extension/adapters/registry.js`：

```js
// 每个 adapter 模块在加载时调用 registerAdapter(adapter)
// adapter shape:
// {
//   site: 'twitter',
//   name: 'list-tweets',                  // human-readable
//   sourceKind: 'twitter:list',           // matches backend feeds.kind
//   domain: 'x.com',                      // location.hostname must equal (or subdomain match)
//   urlPattern: /^\/i\/lists\/(\d+)/,    // matches location.pathname; capture group 1 = source_id
//   pullable: true,                       // true: shows in popup "刷新 source" dropdown
//   passive: true,                        // true: extracts on user navigation (R4); false: push-only
//   extract: (document) => ({
//     items: TweetItem[],
//     sourceID: string,                   // from URL or DOM, must match popup re-open
//     sourceName: string,                 // display label, e.g. "AI builders" list name
//     hasMore: boolean,                   // for scroll-loading hint
//   }),
// }
```

`registry.js` 维护 `Map<site, Adapter[]>`，提供 `findAdapter(location) → Adapter | null`。

### 2. Twitter adapters

每个 `extension/adapters/twitter/*.js` 是一个 ES module，import 共享工具，export 一个 adapter 对象，并在文件末尾 `import { registerAdapter } from '../registry.js'; registerAdapter(thisAdapter);`。

**实现策略**：每个 adapter 的 `extract(document)` 内部逻辑**端口 OpenCLI 对应文件**的 `page.evaluate` 里那段 JS（参见 `clis/twitter/list-tweets.js` / `bookmarks.js` / `tweets.js`）。**这是 spec 明确允许的代码复用** —— 这些是站点 DOM 选择器，是 Z 方案"形状对齐 + selector 共享"的核心。

**License & Attribution**：OpenCLI 是 Apache-2.0。Apache-2.0 允许商业/修改/再分发，前提是保留原始 NOTICE、license header、署名。处理方式：

- 新增 `extension/adapters/THIRD_PARTY_NOTICES.md`，列出 OpenCLI 仓库地址、许可证、版权声明
- 每个移植自 OpenCLI 的 adapter 文件**头部加上**：

```js
// extension/adapters/twitter/list-tweets.js
//
// Portions of this file derive from OpenCLI (https://github.com/jackwener/opencli)
//   commit <hash>, file clis/twitter/list-tweets.js, licensed under Apache-2.0.
//   See extension/adapters/THIRD_PARTY_NOTICES.md.
// Last reviewed: 2026-05-26
//
// When OpenCLI updates this file, see docs/extension-adapters/upstream-map.md
//   to decide whether to cherry-pick the diff.
```

每个 adapter 提取的 item 形状要 normalize 到 spec 4.3 的 `TweetItem`（字段名小写下划线，时间 RFC3339）。OpenCLI 内部可能用不同字段名，**adapter 负责改名**，不传染到 ingest payload。

### 3. Content script

修改 `extension/content.js`（注意 MV3 不支持 ES module import，下面是 IIFE 形态）：

```js
(function () {
  'use strict';
  // 上游 adapter 文件已经在 manifest content_scripts 数组里按顺序加载，
  // 每个 adapter 已 self-register 到 window.__rssPalAdapters
  const adapter = window.__rssPalAdapters?.findFor(location);
  if (!adapter || !adapter.passive) return;

  function runExtractAndQueue() {
    const { items, sourceID, sourceName, hasMore } = adapter.extract(document);
    if (!items.length) return;
    window.__rssPalQueue.push({
      source_kind: adapter.sourceKind,
      source_id: sourceID,
      source_name: sourceName,
      items,
    });
  }

  runExtractAndQueue();

  // 增量提取：滚动 / DOM 突变触发，按 item.id 去重在 queue 层处理
  const debounce = (() => {
    let t;
    return (fn, ms) => { clearTimeout(t); t = setTimeout(fn, ms); };
  })();
  new MutationObserver(() => debounce(runExtractAndQueue, 800))
    .observe(document.body, { childList: true, subtree: true });
})();
```

每个 adapter 文件形态：

```js
// extension/adapters/twitter/list-tweets.js
(function () {
  'use strict';
  const adapter = {
    site: 'twitter',
    name: 'list-tweets',
    sourceKind: 'twitter:list',
    domain: 'x.com',
    urlPattern: /^\/i\/lists\/(\d+)/,
    pullable: true,
    passive: true,
    extract: function (document) { /* ported from OpenCLI */ },
  };
  (window.__rssPalAdapters ||= { _all: [], findFor(loc) { /* ... */ } })._all.push(adapter);
})();
```

### 4. Queue

新文件 `extension/queue.js`：

- chrome.storage.local key: `ingestQueue` — `Array<{source_kind, source_id, source_name, items, queued_at}>`
- `pushItems(...)` —— 入队，按 `item.id` 在同 source 内去重；超过 1000 条触发立即 flush
- `flush()` —— 触发条件：① 显式调用 ② alarm 每 30s ③ queue size >= 20 ④ background activation
- 失败处理：
  - 401 → 弹 chrome.notifications + popup badge 红点 + 暂停 flush 直到 options 重设
  - 5xx / 网络错误 → 留在队列，**最长保留 7 天**（超期丢弃 + 控制台 warn）
  - 200 部分成功 → 按 response.errors 标记哪些 item 失败，其余从 queue 移除

### 5. Popup 改造

修改 `extension/popup.html` + `extension/popup.js`：

- 现有「网摘当前页」按钮**保留**，行为不变（走 bookmarklet/capture，公众号/PDF/twitter 单推都还是这条路）
- 新增「同步 Source」区块：
  - 下拉列出已知 sources（每项形如 `Twitter List · AI builders` / `Twitter User · @karpathy` / `Twitter Bookmarks`）
  - **Source 自动发现**：R4 寄生路径每次抓到新 source（`source_kind` + `source_id` 组合此前没见过）就 append 到 `chrome.storage.sync.known_sources`，popup 直接读这个列表；用户**不需要手动添加**
  - 也允许手动添加：粘 list URL 自动解析 `^https://x\.com/i/lists/(\d+)` → `twitter:list`；粘 `@handle` → `twitter:user`；选 "我的 Bookmarks" → `twitter:bookmarks`
  - 按钮「立即同步」→ R1 路径：从 source 重建 URL（list → `https://x.com/i/lists/<id>`；user → `https://x.com/<handle>`；bookmarks → `https://x.com/i/bookmarks`），`chrome.tabs.create({url, active: false})`，等加载完，content script 自动跑（R4 同一路径），等 queue flush 完成，关 tab
  - 30s timeout，超时关 tab + 提示「同步超时」
- options 页加 toggle：「让扩展在我打开 x.com 时自动抓取（R4）」默认开

### 6. Manifest 更新

`extension/manifest.json`:

```json
{
  "version": "1.6.0",
  "permissions": ["activeTab", "storage", "scripting", "alarms", "tabs", "notifications"],
  "host_permissions": ["<all_urls>"],
  "content_scripts": [
    { "matches": ["https://mp.weixin.qq.com/*", "http://mp.weixin.qq.com/*"], "js": ["content.js"], "run_at": "document_idle" },
    { "matches": ["*://*/extension-config"], "js": ["config-receiver.js"], "run_at": "document_idle" },
    { "matches": ["https://x.com/*"], "js": ["content.js"], "run_at": "document_idle" }
  ]
}
```

新加 `tabs` 权限（R1 路径要 `chrome.tabs.create/remove`）、`notifications`（登录态失效提示）。`x.com` 主域名直接写无前缀（`*.x.com` 在 MV3 patterns 里**不**匹配根域名）。

**MV3 content_scripts 不支持 ES module `import`** —— content_scripts 数组里声明的 js 文件被注入时是普通 `<script>`，不是 `<script type=module>`。所以 `content.js` 不能 `import { ... } from './adapters/...'`。可行方案二选一：

1. **打包**（推荐）— 引入 esbuild / rollup 把 `content.js` + 所有 adapter 文件打包成单文件 `dist/content.bundle.js`，manifest 引用 bundle。**`extension/` 目录已有 `package.json`？** 没有的话需要新增 + `bun.lock`（或 npm）。
2. **IIFE 注册**（无构建依赖）— 每个 adapter 文件用 IIFE 包裹，自注册到全局 `window.__rssPalAdapters` Map，manifest content_scripts 数组按顺序列出 registry.js + 所有 adapter 文件 + content.js（执行顺序由 manifest 数组顺序保证）：
   ```json
   "js": ["adapters/registry.js", "adapters/twitter/list-tweets.js", "adapters/twitter/tweets.js", "adapters/twitter/bookmarks.js", "content.js"]
   ```

MVP 选 **方案 2**（IIFE 注册）—— 零构建步骤，每加一个 adapter 只改 manifest 一行。打包是后续 backlog。

## 前端组件

### TweetCard 组件

新文件 `frontend/src/components/TweetCard.tsx`：

- props: `{ article: Article }`，要求 `article.kind === 'tweet'`
- 渲染：
  - header: display name + `@handle` + `published_at` —— 从 `article.content` 第一行 byline blockquote 用正则解析（格式由 `buildTweetContent` 保证）。**MVP 不渲染头像** —— 没有可靠头像 URL 数据源；future 项见非目标。可选简化：用 `https://unavatar.io/twitter/<handle>` 作为占位图，不依赖任何后端字段
  - body: 剥掉首行 byline 后用 `<ReactMarkdown>` 渲染余下 content（已经是 markdown，带 image 行和 quote 行）
  - footer: "在 X 打开 ↗" 跳转 `article.url`。**MVP 不显示互动数** —— 后端 articles 表没有 likes/retweets 列；ingest 收到这些字段但目前丢弃。Future 若要展示，加 `articles.metadata jsonb` 列存原始数字
- 列表项 (`<ArticleListItem>`) 和详情页都判断 `article.kind`：
  - `'tweet'` → `<TweetCard>`
  - `'tweet_thread'` → 暂同 `<TweetCard>`（MVP 不实现）
  - 其它 → 现有组件不变

### Feed 列表分组

`<FeedSidebar>` 按 `feed.kind` 分组显示：

- 📰 RSS Feeds（kind=rss/html）
- 🐦 Twitter Sources（kind=twitter:*）
- 📑 Saved（kind=saved）

按 kind 分组不修改任何现有数据，只是 UI 视觉上更清楚。

## 错误处理

| 失败点 | 检测 | 行为 |
|---|---|---|
| Twitter 登录态失效 | `adapter.extract()` 检测到 `<a href="/login">` 或 `<div data-testid="loggedOut">` 或重定向到 `/i/flow/login` | 扩展 badge 红点 + chrome.notifications + popup 显示「请在 x.com 重新登录」横幅；暂停所有 twitter adapter 的 passive 提取 |
| Selector 失效（UI 改版） | `extract()` 返回 items.length === 0 但 `document.querySelectorAll('article').length > 0` | badge 黄点 + 控制台 warn，**不**报错（不 spam 用户）；fixture 测试会同步红，告诉开发者去更新 |
| Ingest API 401 | rss-pal JWT 过期 | 弹 options 重设 token，queue 暂停 flush |
| Ingest API 5xx / 网络错误 | response.ok === false | 留在 queue，指数退避（30s / 2min / 10min / 30min），7 天后丢弃 |
| 推文重复投递 | 滚动时同一 tweet id 多次出现 | queue 入队时按 `(source_kind, source_id, item.id)` 去重；后端 `FindByOwnerAndURL` 命中即跳过 / 选择性 update 也是兜底 |
| Twitter URL pattern 含 `/with_replies` `/media` | `urlPattern` 不匹配 | adapter 不触发，扩展什么也不做 |
| popup 触发的 tab 加载超时 | 30s 没收到 extract 完成信号 | 关 tab + 提示「该 source 加载超时，重试或检查登录」 |

## 隐私 / opt-in 默认值

- **默认开**：list-tweets、bookmarks（用户**主动打开**这些页面就是已经表态在意里面的内容）
- **默认开**：tweets（profile 页同理）
- **默认关**：Home timeline（`x.com/home`）—— 噪音太高，user 可在 options 单独开
- **永远关**：搜索结果、explore 页、trends —— 不属于"用户在意的源"，spec 不实现

options 页加 per-source 切换：

```
┌─ Twitter ───────────────────────────────┐
│ ☑ 自动抓取 list 时间线 (R4)             │
│ ☑ 自动抓取我打开的用户主页              │
│ ☑ 自动抓取我的 Bookmarks 页             │
│ ☐ 自动抓取 Home Timeline (噪音多，慎开) │
│ ☑ Popup 中显示「同步 Source」入口       │
└─────────────────────────────────────────┘
```

## 测试策略

### Fixture-driven adapter 测试

每个 adapter 旁挂 `__fixtures__/` 目录，至少 2 个 HTML snapshot：

```
extension/adapters/twitter/__fixtures__/
  list-tweets-empty.html
  list-tweets-page1.html          # 实际抓到的 list 页 HTML（去敏感 token）
  bookmarks-page1.html
  tweets-user-page1.html
```

测试运行：`vitest` + `jsdom`，给 fixture 喂给 adapter.extract，断言 items 字段。改版时表现是某个 fixture 重新保存后 test 红 → 开发者修选择器 / 重存 fixture → 绿。

复用现有 `agent-browser` skill 的 `--session-name twitter` 一次登录抓 fixture。Fixture 入库前用脚本（`extension/scripts/sanitize-fixture.sh`）grep 去掉 `<meta>` 里的 csrf / `auth_token` / `ct0` 等敏感字面量。

### Backend normalizer 单测

`backend/internal/extension/normalizer/twitter_test.go`:

- 表驱动：given TweetItem → expected Article (title, author, content, kind, url)
- 覆盖：text-only / image-only / with-quote / 时间为零值的退化

### Handler integration 测试

`backend/internal/api/extension_ingest_test.go`:

- POST /api/extension/ingest body = 包含 3 条 TweetItem
- 断言 feed 自动创建（kind=twitter:list, name=「test list」）
- 断言 3 条 article 入库，kind='tweet'
- 再发一次同 payload，断言 `accepted=0, skipped=3`（dedupe）

### Bookmarklet 回归

修改 `bookmarklet_test.go` 现有 twitter case：

- 断言 created article 的 `kind == 'tweet'`（之前是默认 `article`）
- 断言 `buildTweetContent` 输出未变（共享包提取后行为不变）

### 端到端手测

- 部署到本地 docker-compose
- Chrome 加载扩展未打包版
- 登录 x.com，浏览 `x.com/i/lists/<own-list>`，观察：
  - 滚动几屏，列表自动归档到 rss-pal
  - rss-pal 前端 Feed Sidebar 出现 "🐦 Twitter Sources"，list 名正确
  - 列表项渲染为 TweetCard
- popup 加一个 source「@karpathy」，点「立即同步」，观察后台 tab 一闪而过，归档若干条

## 上游 OpenCLI 维护 SOP

### 对照表

新文件 `docs/extension-adapters/upstream-map.md`:

```markdown
# Extension Adapter Upstream Map

每行：rss-pal adapter ← OpenCLI source @ commit | last reviewed date | reviewer

## Twitter

| rss-pal | OpenCLI | last synced commit | last reviewed |
|---|---|---|---|
| extension/adapters/twitter/list-tweets.js | clis/twitter/list-tweets.js | abc1234 | 2026-05-26 |
| extension/adapters/twitter/tweets.js | clis/twitter/tweets.js | abc1234 | 2026-05-26 |
| extension/adapters/twitter/bookmarks.js | clis/twitter/bookmarks.js | abc1234 | 2026-05-26 |
```

每次新增/更新 adapter 时同步这张表。

### 巡检脚本

新文件 `scripts/check-upstream-adapters.sh`:

```sh
#!/usr/bin/env bash
# Usage: scripts/check-upstream-adapters.sh
# 拉 OpenCLI 主分支，对比 upstream-map 里每条 last-synced commit → latest 的 diff
# 输出哪些 adapter 上游有改动需要 review；不自动 merge。
```

行为：

1. clone (或 fetch) `github.com/jackwener/opencli` 到 `/tmp/opencli-upstream`
2. 解析 `docs/extension-adapters/upstream-map.md` 提取 (rss-pal file, opencli file, last-synced commit)
3. 对每行运行 `git -C /tmp/opencli-upstream log --oneline <commit>..HEAD -- <opencli file>`
4. 任何输出非空就打印一段 banner：「`opencli file` 在 `commit..HEAD` 期间有 N 次改动，建议 review」
5. 不自动写代码、不自动改 map

建议每月跑一次。可加到 `Makefile` 的 `make check-upstream` target。

## 上线 / 部署

### 数据库迁移

按现有惯例（CLAUDE.md memory: "New migrations need manual apply"）：

- `023_add_article_kind.sql` —— 新加 `articles.kind` 列 + 索引 + 回灌已有 twitter URL 的 article kind
- `024_add_feed_kind.sql` —— 新加 `feeds.kind` + `provider_source_id` + 唯一索引

用户**必须**在 docker-compose up 前手动 `psql < migrations/023_*.sql` 和 `024_*.sql`，否则 ingest 入口 500。

### 扩展版本

`extension/manifest.json` version → `1.6.0`。按 memory 中的「Bump extension version on every change」rule。

### 后端环境变量

无新增。

### 回滚

- 后端代码回滚 → 老 article 还在，kind 字段值仍是 'tweet'/'article'，不影响
- 扩展回滚 → 扩展版本退回 1.5.x，新 ingest 入口没人调用就闲置
- 数据库**不**回滚（kind 字段保留无害）

### 灰度

- MVP 单用户，无灰度需求
- 但 R4 默认开 list/bookmarks/tweets 时，第一次激活会立刻开始归档 —— popup 第一次打开时显示一个 onboarding 横幅，说明会自动同步哪些页面，给用户机会去 options 关掉

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| Twitter 改版 selector 失效 | fixture-driven test 红；改选择器 / 重存 fixture |
| 用户在 x.com 退出登录 | `extract` 检测 + badge 红点 + 暂停 passive 提取 |
| ingest payload 太大（一次 100 条推文） | 后端限制 body size 4 MiB（沿用现有），扩展 queue 单次 flush 上限 50 条 |
| 重复扩展（OpenCLI Browser Bridge 同时装着） | 各自跑各自的 content script，互不干扰；都用真 Chrome cookie 没冲突 |
| `articles.kind` 字段新增对现有代码影响 | DEFAULT 'article'，所有现有 INSERT 不写 kind 也工作；SELECT 不带 kind 也工作（pg 自动填默认） |
| 推文文本里含 `>` 被 markdown 当 blockquote | buildTweetContent 输出本来用 `>` 做 byline，推文里出现 `>` 也是合法 markdown 引用块，渲染合理；如果用户嫌烦 `escape` 一下 — 现实中极少，先不做 |
| Twitter list URL 是 numeric id，不易识别 | 第一次抓到时把 list name 存到 `feeds.name`；popup 选项显示 name 不显示 id |
| 用户多 x.com 账号 | 只支持当前 active 账号；如果用户切号，看到的就是新账号视角，归档归到同一个 feed —— 不区分。MVP 不解决多账号 |
| Bookmarks 抓回大量历史（首次几千条） | queue 节流 + 后端 normalizer 串行 + dedup —— 几千条一次性涌入约 5-10 分钟跑完，没死锁就行 |

## 不在本期（给未来）

- 公众号图片质量（C4a/b/c）独立 hotfix
- G2 小红书 adapters（`extension/adapters/xhs/`）
- G3 付费墙文章 — 已有 bookmarklet path 大致能用，但 paywall 检测要单独做
- G5 微信读书 adapters
- G6 ChatGPT/Claude 对话历史 adapters
- Twitter Home Timeline 抓取（用户可在 options 开，但默认关）
- Twitter Search / Lists discovery / Trends
- Twitter 写操作（永远非目标）
- Thread 聚合：T6 现在是单条入 kind='tweet'，未来如果用户想要"把 thread 上下游聚成一篇" → 新 kind='tweet_thread' + 新 adapter
- 视频推文：现在 ImageURLs 不含视频；未来若 markdown 渲染层加 `<video>` 支持再开
- 头像归档：MVP 不显示或用 `unavatar.io` 占位；未来加 `articles.metadata jsonb` 列存 author_avatar_url + likes/retweets/replies/views 等富字段
- ESM 构建管线：MVP 用 IIFE 注册 + manifest 数组按顺序加载；将来 adapter 数量翻倍后引入 esbuild 打包减少 manifest 噪音
