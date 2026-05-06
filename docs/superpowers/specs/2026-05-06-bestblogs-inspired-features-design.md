# bestblogs.dev 借鉴特性 — 设计文档

**日期:** 2026-05-06
**范围:** 微信源支持(via 自部署 RSSHub)、字数 / 阅读时长、周刊聚合视图、推荐订阅库 + 种子来源。
**触发:** 调研 https://www.bestblogs.dev Issue #93 内容来源后,挑选 3 类高 ROI 借鉴点。

## 1. 目标与非目标

### 目标
1. 在 RSS Pal 引入"推荐订阅"和种子来源机制,管理员账号开箱即用 12 个高质量源(覆盖 OpenAI、Anthropic、Cloudflare、量子位、宝玉、YouTube 频道、Substack、小宇宙播客、1 个微信公众号)。
2. 文章列表 / 详情 / 周刊统一显示「字数 · 阅读分钟」。
3. 新增 `/weekly` 页面:近 7 天 Top 10 文章 + AI 生成的中文主题导语,支持上一周/下一周切换。
4. 抓取过程中暴露的真实问题(失效的 RSSHub 路由、YouTube/Podcast 无正文)即时修复或显式标记。

### 非目标
- 邮件推送、周刊聚类分组、文章级 taxonomy、推荐分算法重写、Pro 付费、X/Twitter 集成。
- 替换/重写已有 OPML 导入、Jina Reader fallback、AI 摘要管线。

## 2. 整体架构

```
postgres ── api ── frontend
          │
          └─ worker ──▶ rsshub:1200(新容器,内网 only)
                         │
                         └─▶ 微信 / 小宇宙 等无原生 RSS 的源
```

新组件:
- **RSSHub 容器**(`diygod/rsshub`,内网,`CACHE_TYPE=memory`)。
- **Migration 007**:articles 加列 + recommended_feeds 表 + weekly_digests 表。
- **后端 API**:`/api/recommended-feeds`(GET / POST :id/subscribe)、`/api/weekly-digest`。
- **前端页面**:`/recommended`、`/weekly`。

复用:
- `feeds.feed_type` 已存在(rss / webpage),本设计扩展 `youtube` / `podcast` 两值。
- `ContentFetcher` 的 Jina Reader fallback 用于微信文章正文。
- AI 摘要管线复用,周刊导语是 insights prompt 的变体。

## 3. 数据模型(Migration 007)

```sql
-- 文章阅读指标
ALTER TABLE articles ADD COLUMN word_count INT DEFAULT 0;
ALTER TABLE articles ADD COLUMN reading_minutes INT DEFAULT 0;

-- 推荐订阅库(catalog)
CREATE TABLE recommended_feeds (
    id SERIAL PRIMARY KEY,
    url VARCHAR(2048) NOT NULL UNIQUE,
    title VARCHAR(500) NOT NULL,
    description TEXT,
    category VARCHAR(100) NOT NULL,
    language VARCHAR(10) NOT NULL,
    feed_type VARCHAR(20) DEFAULT 'rss',
    sort_order INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW()
);
-- category 取值:'ai_eng' | 'cn_tech' | 'enterprise' | 'podcast' | 'youtube'
-- language 取值:'zh' | 'en'
-- feed_type 取值:'rss' | 'webpage' | 'youtube' | 'podcast'

-- 周刊导语缓存
CREATE TABLE weekly_digests (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    week_start DATE NOT NULL,
    intro_text TEXT NOT NULL,
    article_ids INT[] NOT NULL,
    generated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(user_id, week_start)
);
```

### 推荐库 + 种子语义(选项 D 的落地)

`feeds.url` 全局 UNIQUE,所以:
- **种子(A)**:12 条 feed 以 `owner_id=NULL`(公共,所有用户可见)写入 `feeds`,同时写入 `recommended_feeds` catalog。管理员登录即看到。
- **库(B)**:`/recommended` 读 `recommended_feeds`,LEFT JOIN `feeds` on url 判断 `subscribed`。已存在则按钮置「✓ 已订阅」;不存在(将来扩库新增项)显示「订阅」按钮,点击 INSERT 到 `feeds`(owner_id=当前用户 id)。

## 4. 字数 / 阅读时长

算法(Go 工具函数 `internal/rss/metrics.go`):

```
chineseChars   = count(rune in [一-鿿])
englishWords   = words after stripping Chinese chars and splitting on whitespace
wordCount      = chineseChars + englishWords
readingMinutes = max(1, round(chineseChars/300 + englishWords/250))
```

写入时机:
- worker 在 `content` 第一次写入或更新时(包括 webpage 全文抓取、Jina fallback、短内容重抓)同步计算并 UPDATE。
- `feed_type='youtube'` / `'podcast'` 用 RSS description 计算,不触发深抓。
- 一次性回填脚本(`backend/cmd/backfill_metrics`)给现存 articles 补齐。

API:文章 list / detail JSON 增加 `word_count` / `reading_minutes` 字段(零值由前端不显示)。
前端:新组件 `<ReadingMeta>`,在 `ArticleListPage` / `ArticlePage` / `WeeklyPage` 卡片底部显示「📖 1,234 字 · 5 分钟」。

## 5. RSSHub + 来源清单

### docker-compose 新增

```yaml
rsshub:
  image: diygod/rsshub:latest
  restart: unless-stopped
  environment:
    - NODE_ENV=production
    - CACHE_TYPE=memory
    - CACHE_EXPIRE=3600
    - REQUEST_TIMEOUT=15000
  # 不暴露端口,worker 通过 http://rsshub:1200 内网访问
```

### 12 个种子源(Issue #93 精选 + 用户筛选)

| # | 来源 | category | lang | URL | 备注 |
|---|------|----------|------|-----|------|
| 1 | OpenAI Blog | ai_eng | en | `https://openai.com/news/rss.xml` | 原生 RSS |
| 2 | Anthropic Engineering | ai_eng | en | 实施时验证 anthropic.com 是否提供原生 RSS,否则 RSSHub | 中风险 |
| 3 | Cloudflare Blog | enterprise | en | `https://blog.cloudflare.com/rss/` | 原生 |
| 4 | InfoQ 英文 | enterprise | en | `https://feed.infoq.com/` | 原生 |
| 5 | 宝玉的分享 (baoyu.io) | ai_eng | zh | `https://baoyu.io/feed.xml` | 原生 |
| 6 | 量子位 | cn_tech | zh | 优先 qbitai.com 原生,否则 RSSHub `/qbitai` | 实施时确认 |
| 7 | Sequoia Capital YouTube | ai_eng | en | `https://www.youtube.com/feeds/videos.xml?channel_id=...` | YouTube 原生 RSS,需查 channel_id |
| 8 | Y Combinator YouTube | ai_eng | en | YouTube 原生 RSS | 同上 |
| 9 | AI Engineer YouTube | ai_eng | en | YouTube 原生 RSS | 同上 |
| 10 | Addy Osmani Substack | ai_eng | en | `https://addyo.substack.com/feed` | Substack 原生 |
| 11 | 腾讯技术工程 公众号 | cn_tech | zh | RSSHub 微信路由 | **高风险**:RSSHub 微信路由历史不稳定 |
| 12 | 小宇宙播客(Agent 史相关) | podcast | zh | RSSHub `/xiaoyuzhou/podcast/:id` | 中风险 |

(已从 bestblogs Issue #93 的腾讯云开发者 / 腾讯科技 / 阿里云开发者 / InfoQ 中文这 4 个微信号中**剔除**,微信只保留 #11。)

### 失败降级

种子脚本(`backend/cmd/seed`)对每个 URL 做 GET 探活:
- 成功 → INSERT 到 `feeds`(owner_id=NULL)+ `recommended_feeds`,文章后续由 worker 抓取。
- 失败 → 只 INSERT 到 `recommended_feeds`,前端卡片显示「⚠ 当前路由不可用」徽标(条件:`subscribed=false && feed_type 属于易失效类`)。
- 输出 JSON 报告供后续修复。

## 6. YouTube / Podcast 处理

worker 在调用 `ContentFetcher.FetchContent` 前判断 `feed_type`:
- `webpage` / `rss` 走原路径(深抓 + Jina fallback + 短内容重抓循环)。
- `youtube` / `podcast` **跳过**深抓与短内容重抓,使用 RSS 自带的 description 作为 content,字数 / 阅读时长由 description 计算。

避免现有"短内容 (<300) 反复重抓"逻辑把视频/播客文章拉进死循环。

## 7. API

| Method | Path | 说明 |
|--------|------|------|
| GET | `/api/recommended-feeds` | 列出 catalog,按 `category, sort_order` 排序;每条带 `subscribed: bool` |
| POST | `/api/recommended-feeds/:id/subscribe` | 已订阅(target URL 已在 `feeds` 表内,无论 owner 是 NULL 还是当前用户)→ 返回 200 幂等;否则 INSERT(owner_id=当前用户 id)。url UNIQUE 冲突按 200 处理。 |
| GET | `/api/weekly-digest?week=YYYY-MM-DD` | 返回 `{week_start, intro_text, articles: [...]}`;`week` 缺省=本周一(`Asia/Shanghai` 时区,与 AI 摘要中文受众一致);缓存不存在则现场生成并写入 `weekly_digests` |

文章 list / detail 接口的 JSON **新增** `word_count` / `reading_minutes` 字段,不破坏现有调用方。

## 8. 前端

新页面:
- **`/recommended`** "推荐订阅":卡片网格,按 category 分 section(AI 工程 / 中文科技 / 企业基建 / 视频 / 播客)。每张卡片:title + description + 语言徽标 + 「订阅 / ✓ 已订阅 / ⚠ 不可用」按钮。
- **`/weekly`** "本周精选":顶部 AI 主题导语,下方 10 篇文章卡片(复用现有卡片样式),右上角「‹ 上一周 / 下一周 ›」。AI 调用失败时显示空导语 + 文章列表(容错)。

全站组件:
- 新 `<ReadingMeta wordCount={...} minutes={...} />`,在 `ArticleListPage` / `ArticlePage` / `WeeklyPage` 卡片角落显示「📖 字数 · X 分钟」(`wordCount > 0` 才渲染)。

导航:`App.tsx` 顶部菜单加「推荐订阅」「本周精选」入口,位置紧跟现有「文章」「Insights」之后。

## 9. 周刊导语生成

- 触发:首次访问 `/weekly?week=X` 时按需生成,后续命中缓存。
- 选文逻辑:`week_start ≤ published_at < week_start + 7d`,按推荐分排序(沿用现有 `service` 评分;若用户暂无偏好信号,按 `published_at desc` 兜底)取 Top 10。
- AI Prompt(中文,放 `internal/ai/weekly_digest.go`):"以下是本周 10 篇精选文章的标题和简要摘要…用 150-200 字提炼本周技术圈的核心主题、给读者『为什么这周值得关注』的视角"。
- 缓存:写入 `weekly_digests`(user_id, week_start UNIQUE)后永久保留,后续直接读。
- 失败模式:AI 调用失败,API 返回 200 + `intro_text=""` + 文章列表;前端正常渲染文章,导语区位置显示空白(无错误弹窗)。

## 10. 实施顺序

每步独立可验证,顺序如下:

1. **Migration 007** — 加列、加表(空)。验证:`docker-compose up`,`\d articles` / `\d recommended_feeds` / `\d weekly_digests` 都在。
2. **字数 / 阅读时长** — Go 工具函数、worker hook、回填脚本、文章 API、前端 `<ReadingMeta>`。验证:回填后前端显示。
3. **RSSHub 容器** — docker-compose 加,`docker exec api curl http://rsshub:1200/test/1` 通。
4. **种子脚本** — `backend/cmd/seed`,逐 URL 探活、写表、输出报告。**实施这步会真实暴露失效路由,即时排查或换路由。**
5. **YouTube / Podcast feed_type 分支** — worker 跳过深抓。验证:订阅一个 YouTube 频道无报错。
6. **推荐订阅 API + 前端页面** — 卡片视图、订阅按钮幂等。
7. **周刊 API + 前端页面** — 选文、AI 导语、缓存、上一周/下一周切换。

每步完成跑 `go build ./...`、前端 `npm run build`、手动验证。

## 11. 风险登记

| 风险 | 影响 | 处理 |
|------|------|------|
| RSSHub 微信路由失效 | #11 抓不到 | seed 探活时不阻塞;UI 标 ⚠;实施时尝试多个候选路由 |
| RSSHub 容器异常拖慢 worker | 抓取阻塞 | worker 已有 30s timeout;RSSHub `REQUEST_TIMEOUT=15000` |
| YouTube/Podcast content 短被反复重抓 | 死循环 | feed_type 分支跳过短内容重抓 |
| 字数算法对代码块、表格不准 | 显示偏差 | 接受;阅读时长本就估算 |
| 周刊 AI 调用失败 | 页面挂掉 | 前端容错(空 intro + 文章列表正常) |
| `feeds.url` 冲突 | seed/订阅失败 | seed 用 `ON CONFLICT DO NOTHING`;订阅 API 同样幂等 |
| 微信文章正文抓取失败 | content 为空 | 复用已落地的 Jina Reader fallback |

## 12. 验证清单(End-to-End)

- [ ] `docker-compose up -d --build` 全部容器健康(postgres / api / worker / frontend / rsshub)
- [ ] `/recommended` 看到 12 张卡片,正确分类,管理员账号显示「✓ 已订阅」
- [ ] `/feeds` 看到 12 个种子源,worker 日志显示在抓取
- [ ] OpenAI / Cloudflare / baoyu.io / 量子位 / Substack / YouTube × 3 至少各有 1 篇文章入库
- [ ] 微信源(腾讯技术工程)若可用则有文章入库;不可用则 catalog 显示 ⚠
- [ ] 文章列表卡片显示「字数 · X 分钟」
- [ ] `/weekly` 显示中文导语 + 10 篇文章,「上一周」按钮工作
- [ ] 旧用户 / 旧文章不受影响(回填脚本跑过、API 字段向后兼容)

## 13. YAGNI(明确不做)

- 邮件推送(SMTP)
- 周刊文章按主题聚类分组
- 文章级固定分类 taxonomy
- 推荐分算法重写
- Pro 付费等级
- X/Twitter 集成
