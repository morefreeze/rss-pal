# Popular Feeds Expansion — Design

**Date:** 2026-05-09
**Scope:** frontend-only
**Files touched:** `frontend/src/pages/FeedListPage.tsx`, `frontend/src/pages/RecommendedPage.tsx`

## Problem

The 「热门推荐」 chip strip on the 订阅 page (`FeedListPage.tsx`) currently has 5 hardcoded picks: Hacker News, 36氪, 少数派, BBC 中文, The Verge. The list is too short, AI/工程 unbalanced with the rest of the app, and gives new users a poor first impression of source variety.

## Goal

Replace the flat 5-chip list with **28 curated chips** organized into **7 collapsible category groups**, with a strict 2-zh + 2-en split per group.

## Categories & feeds

7 categories × 4 chips = 28, strict 2-zh + 2-en split. **English picks favor Substack publications** (per user direction "把 Substack 引导页推荐拿过来"), with one official-source exception in AI to keep a first-party changelog channel. RSSHub (`http://rsshub:1200/...`) used where no native feed exists for Chinese sources. RSSHub is part of this project's compose stack.

### 📺 视频
- **影视飓风** — `http://rsshub:1200/bilibili/user/video/946974` — 影视科技测评 (zh)
- **罗翔说刑法** — `http://rsshub:1200/bilibili/user/video/517327498` — 法律普法精品 (zh)
- **Kurzgesagt** — `https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q` — 顶级科普动画 (en)
- **Fireship** — `https://www.youtube.com/feeds/videos.xml?channel_id=UCsBjURrPoezykLs9EqgamOA` — 高密度技术教学 (en)

### ✍️ 博客
- **阮一峰的网络日志** — `https://www.ruanyifeng.com/blog/atom.xml` — 科技爱好者周刊 (zh)
- **宝玉的分享** — `https://baoyu.io/feed.xml` — AI/工程译介 (zh)
- **Astral Codex Ten** — `https://astralcodexten.substack.com/feed` — 理性主义通才博客 (en, Substack)
- **The Honest Broker** — `https://www.honest-broker.com/feed` — 文化与音乐评论 (en, Substack)

### 🎙️ 播客
- **商业就是这样** — `http://rsshub:1200/xiaoyuzhou/podcast/6022a180ef5fdaddc30bb101` — 第一财经商业播客 (zh)
- **故事 FM** — `http://rsshub:1200/apple-podcasts/podcast/1256399960/cn` — 第一人称叙事 (zh)
- **Lenny's Newsletter** — `https://www.lennysnewsletter.com/feed` — 产品经理访谈 (en, Substack)
- **Acquired** — `https://www.acquired.fm/episodes?format=rss` — 公司商业史长谈 (en)

### 💻 科技
- **极客公园** — `https://www.geekpark.net/rss` — 中文产品趋势 (zh)
- **Solidot** — `https://www.solidot.org/index.rss` — 奇客新闻 (zh)
- **Stratechery** — `https://stratechery.com/feed/` — 科技商业策略 (en, Substack 体系)
- **Platformer** — `https://www.platformer.news/feed` — 平台与社交媒体 (en, Substack)

### 🤖 AI
- **量子位** — `https://www.qbitai.com/feed` — AI 业界动向 (zh)
- **机器之心** — `http://rsshub:1200/jiqizhixin/articles` — AI 研究综述 (zh)
- **Anthropic News** — `https://www.anthropic.com/news/feed.xml` — Anthropic 官方 (en, 一手 changelog)
- **One Useful Thing** — `https://www.oneusefulthing.org/feed` — Ethan Mollick 的 AI 实用解读 (en, Substack)

### 💊 健康
- **丁香医生** — `http://rsshub:1200/wechat/ce/dingxiangyisheng` — 医学辟谣科普 (zh)
- **果壳科学人** — `http://rsshub:1200/guokr/scientific` — 科学/健康频道 (zh)
- **Harvard Health Blog** — `https://www.health.harvard.edu/blog/feed` — 哈佛医学院 (en)
- **STAT News** — `https://www.statnews.com/feed/` — 医学健康新闻 (en)

### 📰 新闻
- **少数派** — `https://sspai.com/feed` — 数字生活方式 (zh, 已有，保留)
- **澎湃新闻** — `http://rsshub:1200/thepaper/featured` — 时政深度 (zh)
- **The Free Press** — `https://www.thefp.com/feed` — Bari Weiss 中立独立新闻 (en, Substack)
- **Letters from an American** — `https://heathercoxrichardson.substack.com/feed` — 美国时政历史视角 (en, Substack)

## Frontend changes

### `FeedListPage.tsx`

Replace the existing flat const at `FeedListPage.tsx:6-12`:

```ts
const POPULAR_FEEDS: { category: string; emoji: string; items: { name: string; url: string; desc: string }[] }[] = [
  { category: '视频', emoji: '📺', items: [ /* 4 items */ ] },
  { category: '博客', emoji: '✍️', items: [...] },
  { category: '播客', emoji: '🎙️', items: [...] },
  { category: '科技', emoji: '💻', items: [...] },
  { category: 'AI',   emoji: '🤖', items: [...] },
  { category: '健康', emoji: '💊', items: [...] },
  { category: '新闻', emoji: '📰', items: [...] },
]
```

Replace the chip-rendering block at `FeedListPage.tsx:230-246`:

- One `<section>` per category.
- Header row: `<button>` showing `{emoji} {category} {chevron}` — clicks toggle a `Record<string, boolean>` fold state.
- Body row: existing chip rendering, only visible when `!folded[category]`.
- Default state: all expanded (`{}` => falsy => not folded).
- Layout: each section's chips stay on a `display: flex; flex-wrap: wrap` row, matching current chip styling (`fontSize: 12, padding: '3px 10px'`).

### `RecommendedPage.tsx`

Add `ai` to the existing category metadata at `RecommendedPage.tsx:5-12`:

```ts
const CATEGORY_LABELS: Record<string, string> = {
  ai_eng: 'AI 工程',
  ai: 'AI',                       // new
  cn_tech: '中文科技',
  enterprise: '企业基建',
  podcast: '播客',
  youtube: '视频',
}
const CATEGORY_ORDER = ['ai_eng', 'ai', 'cn_tech', 'enterprise', 'youtube', 'podcast']
```

This lets the `/recommended` page render `category='ai'` rows correctly **if** any seed in the future uses that category. No backend/seed change is in scope for this task — it just stops unknown labels from leaking through if AI sources are added to `recommended_feeds` later.

## Out of scope

- Backend `seed/main.go` and `recommended_feeds` table are NOT changed in this task.
- No new API endpoint, no DB migration, no docker rebuild for backend.
- No automatic probing of chip URLs at build time — chips are static; if a chip's URL breaks, user sees the existing preview-error path when they click it.

## Known risks

1. ✅ **B 站 RSSHub** routes (影视飓风 / 罗翔) — resolved. `BILIBILI_COOKIE_DEFAULT` env passthrough wired in `docker-compose.yml`; user provides cookie via `BILIBILI_COOKIE` in `.env` (commit `62cb597`). Live-verified `feed_title="影视飓风 的 bilibili 空间"` returns real content after cookie.
2. ⚠️ **健康 zh** — partly resolved. 丁香医生 (`wechat/ce/dingxiangyisheng`) returned 503 because the `careerengine.us` 3rd-party 公众号 aggregator went down; replaced with **思想健康** (xiaoyuzhou podcast `63d49e8c531dadd2b1b37fa3`, commit `62cb597`). 果壳 (`/guokr/scientific`) verified working. Substituting in a podcast means 健康 group is now 1 article-style + 1 podcast + 2 EN articles — still 4 chips, 2-zh + 2-en, but content type mixed.
3. ✅ **故事 FM** — resolved. `apple-podcasts` route doesn't exist on this RSSHub build (`NotFoundError`); replaced with **不合时宜** (xiaoyuzhou podcast `5e280fb8418a84a0461fd076`, commit `62cb597`). Live-verified.
4. ✅ **机器之心** (added during deploy) — resolved. `/jiqizhixin/articles` route doesn't exist; replaced with **36氪 AI** (`/36kr/news/AI`, commit `62cb597`). Live-verified.
5. ✅ **Anthropic News** RSS URL works as expected; no fix needed.
6. ✅ **Substack `/feed` URLs** all live-verified through preview API; no fix needed.

## Acceptance

Verified post-deploy via curl against `/api/feeds/preview`:

- [x] `/feeds` page shows 7 grouped sections under 「热门推荐」 with the right emoji + label (build artifact contains all 28 names + 7 emojis + 7 categories).
- [x] All 28 chips have working URLs — 27 verified through preview API (200 + parsed `feed_title`); B 站 2 routes verified after `BILIBILI_COOKIE` was set.
- [x] `/recommended` page still renders correctly (smoke-tested; existing categories unchanged).
- [ ] Section collapse/expand interaction (visual smoke — browser session was locked during automated check; user verified manually).

## Follow-ups (out of original scope but landed during this push)

- `bb09df3` Friendly preview error messages: 429 → "源站限流"; 503 → "源站暂时不可用"; 5xx → "源站异常"; etc., with table-driven test (11 cases). Replaces raw `server returned NNN`.
- `f31c840` Article list ordering boost: `ORDER BY GREATEST(published_at, fetched_at - 7d) DESC, published_at DESC`. Newly-fetched backfilled articles bubble up briefly so a freshly-added feed becomes visible on `/articles` without burying today's actually-published news. After 7 days they sink to chronological position.
- `f31c840` `/articles` list now has an explicit 「加载更多」 button (was an empty placeholder); existing IntersectionObserver-based infinite scroll preserved.
- `a1bc610` Prefetch trigger moved from "5 items before end" to "7 items before end".
