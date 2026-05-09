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

1. **B 站 RSSHub** routes (影视飓风 / 罗翔) need `BILIBILI_COOKIE_*` env vars on the rsshub container. If absent, content may be empty. Verify post-deploy.
2. **健康 zh** (丁香医生 公众号 + 果壳 官方 RSS 已停服 → RSSHub) is the weakest pair. If both 404 in production, fall back to one chip + accept this group having 3 entries until a better candidate is found.
3. **故事 FM** uses RSSHub `apple-podcasts` route — uncommon. If broken, swap to its `xiaoyuzhou/podcast/<id>` route.
4. **Anthropic News** RSS URL comes from the existing project seed (`backend/cmd/seed/main.go:33`), trusted as known-working in this project's environment despite some external fetch tools reporting 404 (CDN UA blocks).
5. **Substack `/feed` URLs** mostly return free-tier post summaries even when the publication is paywalled — that's the expected behavior, not a bug.

## Acceptance

Verify by docker-rebuilding frontend (`docker-compose up -d --build frontend`) and:

- [ ] `/feeds` page shows 7 grouped sections under 「热门推荐」 with the right emoji + label.
- [ ] Each section header collapses/expands its chips on click.
- [ ] All 28 chips render with the correct title; tooltip shows `desc`.
- [ ] Clicking each chip fills the URL input and triggers preview.
- [ ] At least one chip per group successfully previews (full coverage probe is a follow-up — risks above explain why we don't gate on 100%).
- [ ] `/recommended` page still renders correctly (no regression from the CATEGORY_LABELS addition).
