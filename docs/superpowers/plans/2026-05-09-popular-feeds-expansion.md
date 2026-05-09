# Popular Feeds Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the flat 5-chip 「热门推荐」 strip on `/feeds` with 28 chips organized into 7 collapsible category groups (2-zh + 2-en each), and add `ai` to `/recommended` page's category metadata.

**Architecture:** Pure frontend change — no backend / DB / API impact. Two files touched. The chip data shifts from a flat array of `{name,url,desc}` to an array of category groups, each containing 4 items. UI gains per-group collapse via a `Record<string, boolean>` state (default empty = all expanded). Verification is type-check + docker rebuild + manual smoke (no test framework in frontend).

**Tech Stack:** React 18, TypeScript 5, Vite 4. No test framework — `npm run build` (`tsc && vite build`) is the structural check; functional verification is manual in the browser.

---

## File Map

| File | Action | Lines |
|---|---|---|
| `frontend/src/pages/FeedListPage.tsx` | Modify | 6-12 (POPULAR_FEEDS const), ~25 (insert state), 230-246 (render block) |
| `frontend/src/pages/RecommendedPage.tsx` | Modify | 5-12 (CATEGORY_LABELS + CATEGORY_ORDER) |

---

## Task 1: Restructure POPULAR_FEEDS + render grouped collapsible sections

**Files:**
- Modify: `frontend/src/pages/FeedListPage.tsx:6-12` (data const)
- Modify: `frontend/src/pages/FeedListPage.tsx:25` (insert state after `importing` state)
- Modify: `frontend/src/pages/FeedListPage.tsx:230-246` (chip render block)

This task is one atomic commit because the new render shape requires the new data shape — splitting them would break the build mid-way.

- [ ] **Step 1: Replace the `POPULAR_FEEDS` const at lines 6-12**

Find this block (lines 6-12):
```ts
const POPULAR_FEEDS = [
  { name: 'Hacker News', url: 'https://hnrss.org/frontpage', desc: '全球科技社区热帖聚合' },
  { name: '36氪', url: 'https://36kr.com/feed', desc: '中国科技商业资讯聚合' },
  { name: '少数派', url: 'https://sspai.com/feed', desc: '数字生活方式与效率工具' },
  { name: 'BBC 中文', url: 'https://feeds.bbci.co.uk/zhongwen/simp/rss.xml', desc: '国际新闻中文报道' },
  { name: 'The Verge', url: 'https://www.theverge.com/rss/index.xml', desc: '英文科技新闻聚合' },
]
```

Replace with the full grouped structure (28 feeds, 7 groups, 2-zh + 2-en each):
```ts
const POPULAR_FEEDS: { category: string; emoji: string; items: { name: string; url: string; desc: string }[] }[] = [
  {
    category: '视频', emoji: '📺', items: [
      { name: '影视飓风', url: 'http://rsshub:1200/bilibili/user/video/946974', desc: '影视科技测评' },
      { name: '罗翔说刑法', url: 'http://rsshub:1200/bilibili/user/video/517327498', desc: '法律普法精品' },
      { name: 'Kurzgesagt', url: 'https://www.youtube.com/feeds/videos.xml?channel_id=UCsXVk37bltHxD1rDPwtNM8Q', desc: '顶级科普动画' },
      { name: 'Fireship', url: 'https://www.youtube.com/feeds/videos.xml?channel_id=UCsBjURrPoezykLs9EqgamOA', desc: '高密度技术教学' },
    ],
  },
  {
    category: '博客', emoji: '✍️', items: [
      { name: '阮一峰的网络日志', url: 'https://www.ruanyifeng.com/blog/atom.xml', desc: '科技爱好者周刊' },
      { name: '宝玉的分享', url: 'https://baoyu.io/feed.xml', desc: 'AI/工程译介' },
      { name: 'Astral Codex Ten', url: 'https://astralcodexten.substack.com/feed', desc: '理性主义通才博客' },
      { name: 'The Honest Broker', url: 'https://www.honest-broker.com/feed', desc: '文化与音乐评论' },
    ],
  },
  {
    category: '播客', emoji: '🎙️', items: [
      { name: '商业就是这样', url: 'http://rsshub:1200/xiaoyuzhou/podcast/6022a180ef5fdaddc30bb101', desc: '第一财经商业播客' },
      { name: '故事FM', url: 'http://rsshub:1200/apple-podcasts/podcast/1256399960/cn', desc: '第一人称叙事' },
      { name: "Lenny's Newsletter", url: 'https://www.lennysnewsletter.com/feed', desc: '产品经理访谈' },
      { name: 'Acquired', url: 'https://www.acquired.fm/episodes?format=rss', desc: '公司商业史长谈' },
    ],
  },
  {
    category: '科技', emoji: '💻', items: [
      { name: '极客公园', url: 'https://www.geekpark.net/rss', desc: '中文产品趋势' },
      { name: 'Solidot', url: 'https://www.solidot.org/index.rss', desc: '奇客新闻' },
      { name: 'Stratechery', url: 'https://stratechery.com/feed/', desc: '科技商业策略' },
      { name: 'Platformer', url: 'https://www.platformer.news/feed', desc: '平台与社交媒体' },
    ],
  },
  {
    category: 'AI', emoji: '🤖', items: [
      { name: '量子位', url: 'https://www.qbitai.com/feed', desc: 'AI 业界动向' },
      { name: '机器之心', url: 'http://rsshub:1200/jiqizhixin/articles', desc: 'AI 研究综述' },
      { name: 'Anthropic News', url: 'https://www.anthropic.com/news/feed.xml', desc: 'Anthropic 官方' },
      { name: 'One Useful Thing', url: 'https://www.oneusefulthing.org/feed', desc: 'Mollick 的 AI 实用解读' },
    ],
  },
  {
    category: '健康', emoji: '💊', items: [
      { name: '丁香医生', url: 'http://rsshub:1200/wechat/ce/dingxiangyisheng', desc: '医学辟谣科普' },
      { name: '果壳科学人', url: 'http://rsshub:1200/guokr/scientific', desc: '科学/健康频道' },
      { name: 'Harvard Health Blog', url: 'https://www.health.harvard.edu/blog/feed', desc: '哈佛医学院' },
      { name: 'STAT News', url: 'https://www.statnews.com/feed/', desc: '医学健康新闻' },
    ],
  },
  {
    category: '新闻', emoji: '📰', items: [
      { name: '少数派', url: 'https://sspai.com/feed', desc: '数字生活方式' },
      { name: '澎湃新闻', url: 'http://rsshub:1200/thepaper/featured', desc: '时政深度' },
      { name: 'The Free Press', url: 'https://www.thefp.com/feed', desc: 'Bari Weiss 中立独立新闻' },
      { name: 'Letters from an American', url: 'https://heathercoxrichardson.substack.com/feed', desc: '美国时政历史视角' },
    ],
  },
]
```

- [ ] **Step 2: Insert fold-state hook after the `importing` state**

Find this line (line 25):
```ts
  const [importing, setImporting] = useState(false)
```

Insert one new state declaration immediately after it:
```ts
  const [importing, setImporting] = useState(false)
  const [foldedGroups, setFoldedGroups] = useState<Record<string, boolean>>({})
```

`{}` as initial value means every group's `folded` lookup is `undefined` → falsy → expanded by default. Spec requires "all expanded".

- [ ] **Step 3: Replace the chip-rendering block at lines 230-246**

Find this block:
```tsx
        {/* Popular feeds */}
        <div className="mb-2">
          <div className="text-sm text-muted mb-1">热门推荐：</div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            {POPULAR_FEEDS.map(f => (
              <button
                key={f.url}
                className="secondary"
                style={{ fontSize: 12, padding: '3px 10px' }}
                title={f.desc}
                onClick={() => { setNewUrl(f.url); doPreview(f.url) }}
              >
                {f.name}
              </button>
            ))}
          </div>
        </div>
```

Replace with grouped + collapsible render:
```tsx
        {/* Popular feeds — grouped + collapsible */}
        <div className="mb-2">
          <div className="text-sm text-muted mb-1">热门推荐：</div>
          {POPULAR_FEEDS.map(group => {
            const folded = foldedGroups[group.category] === true
            return (
              <div key={group.category} style={{ marginBottom: 6 }}>
                <button
                  type="button"
                  onClick={() => setFoldedGroups(s => ({ ...s, [group.category]: !folded }))}
                  style={{
                    background: 'transparent',
                    border: 'none',
                    padding: '2px 0',
                    fontSize: 12,
                    color: '#666',
                    cursor: 'pointer',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 4,
                  }}
                >
                  <span>{group.emoji}</span>
                  <span>{group.category}</span>
                  <span style={{ fontSize: 10 }}>{folded ? '▸' : '▾'}</span>
                </button>
                {!folded && (
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 2 }}>
                    {group.items.map(f => (
                      <button
                        key={f.url}
                        className="secondary"
                        style={{ fontSize: 12, padding: '3px 10px' }}
                        title={f.desc}
                        onClick={() => { setNewUrl(f.url); doPreview(f.url) }}
                      >
                        {f.name}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )
          })}
        </div>
```

- [ ] **Step 4: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean exit (exit code 0, no output).

If there are errors, the most likely cause is a stray reference to the old flat-array shape. Search the file for `POPULAR_FEEDS` and ensure every reference uses the new grouped shape.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/FeedListPage.tsx
git commit -m "feat(frontend): grouped collapsible 热门推荐 chips on /feeds (28 feeds, 7 categories)"
```

---

## Task 2: Add `ai` to RecommendedPage CATEGORY metadata

**Files:**
- Modify: `frontend/src/pages/RecommendedPage.tsx:5-12`

- [ ] **Step 1: Update the CATEGORY constants**

Find this block (lines 5-12):
```ts
const CATEGORY_LABELS: Record<string, string> = {
  ai_eng: 'AI 工程',
  cn_tech: '中文科技',
  enterprise: '企业基建',
  podcast: '播客',
  youtube: '视频',
}
const CATEGORY_ORDER = ['ai_eng', 'cn_tech', 'enterprise', 'youtube', 'podcast']
```

Replace with (adds `ai` after `ai_eng` in both):
```ts
const CATEGORY_LABELS: Record<string, string> = {
  ai_eng: 'AI 工程',
  ai: 'AI',
  cn_tech: '中文科技',
  enterprise: '企业基建',
  podcast: '播客',
  youtube: '视频',
}
const CATEGORY_ORDER = ['ai_eng', 'ai', 'cn_tech', 'enterprise', 'youtube', 'podcast']
```

This is preventive: no current `recommended_feeds` rows have `category='ai'`. The change makes `/recommended` ready for future seeds without leaking a raw `ai` string into the UI.

- [ ] **Step 2: Type check**

Run: `cd frontend && npx tsc --noEmit`
Expected: clean exit.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/pages/RecommendedPage.tsx
git commit -m "feat(frontend): add 'ai' to /recommended CATEGORY_LABELS/ORDER"
```

---

## Task 3: Build + smoke test

**Files:** none modified — verification only.

Per project memory: frontend is served by nginx in docker as a pre-built bundle. Source changes need `docker-compose up -d --build frontend` to take effect.

- [ ] **Step 1: Rebuild and restart the frontend container**

Run: `docker-compose up -d --build frontend`
Expected: build runs `tsc && vite build`, image rebuilds, container restarts. No build errors.

- [ ] **Step 2: Smoke test `/feeds`**

Open the app in a browser (the URL is whatever is mapped in `docker-compose.yml` for the frontend service — typically `http://localhost`).

Navigate to the 订阅 (subscribe) tab. Under "添加订阅", verify the 「热门推荐」 widget shows:

- 「热门推荐：」 label
- **7 group headers in this order**, each preceded by an emoji and followed by a `▾` chevron:
  - 📺 视频, ✍️ 博客, 🎙️ 播客, 💻 科技, 🤖 AI, 💊 健康, 📰 新闻
- Each group has **4 chip buttons** visible (default expanded)
- Total chip count: **28**

Interactive checks:

- Click a group header (e.g., 视频) → its chips disappear, chevron flips to `▸`
- Click again → chips reappear, chevron returns to `▾`
- Hover any chip → tooltip (browser native) shows the `desc` (e.g., hovering `阮一峰的网络日志` shows `科技爱好者周刊`)
- Click `阮一峰的网络日志` → URL fills the input, preview should succeed (this is the most reliable native RSS in the list)

- [ ] **Step 3: Smoke test `/recommended` for regression**

Navigate to the 推荐 tab. Verify:

- The page still loads
- Existing categories (`AI 工程`, `中文科技`, `企业基建`, `视频`, `播客`) render as before
- No new `AI` section appears (because no `recommended_feeds` row has `category='ai'` yet) — this is correct

- [ ] **Step 4: No commit needed unless regressions found**

If anything looks wrong, fix in a separate commit (NOT amend) and re-verify before declaring done.

---

## Self-Review Notes (for the writer)

Coverage check: every spec item maps to a task —
- 28 feeds with URLs/descs/emojis: Task 1 step 1 ✓
- Collapsible per-group state, default expanded: Task 1 steps 2-3 ✓
- Render structure (header + chip row): Task 1 step 3 ✓
- `ai` in CATEGORY_LABELS + CATEGORY_ORDER: Task 2 step 1 ✓
- Manual acceptance criteria: Task 3 ✓

No placeholders. Type names (`POPULAR_FEEDS`, `foldedGroups`, `setFoldedGroups`) consistent across tasks.
