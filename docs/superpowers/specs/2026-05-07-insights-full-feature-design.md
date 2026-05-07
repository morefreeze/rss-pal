# 洞察完整功能 — 设计文档

**Date**: 2026-05-07
**Status**: Approved (pending implementation)
**Branch (suggested)**: `feature/insights-full`

## 背景与问题

`/insights` 页面目前是**半残**的：

- `ai.ExtractTopics()` 与 `prefRepo.UpsertTopic()` 在代码库内**从未被调用**，导致 `interest_topics` 表对所有用户永久为空。
- 概念上还存在混淆：现有 `ExtractTopics` 返回 3-5 个关键词，按"标签（many-per-article）"语义工作，但被命名/存储为"主题"。本设计**显式区分主题与标签**两层。
- 后端 `prefRepo.DecayTopics()` 也是无人调用的死代码（话题权重不会衰减）。
- 洞察结果调用一次 AI 后即丢失（每次重新生成都重跑），无持久化、无流式输出。
- 没有调用频率限制，用户可无限触发昂贵 AI 调用。
- 输入 AI 的数据信息密度低（只有"主题列表 + 最近 20 个标题"），无法体现正负信号、时间趋势。
- 标签云无空态引导，无法管理（删除）误打的话题。

本文档描述把 `/insights` 从"按钮多半返回'暂无数据'"变成一个**真正可用的、有持续生命力的功能**所需的端到端设计。

## 概念：主题 vs 标签

| 维度 | 主题 (topic) | 标签 (tag) |
|---|---|---|
| 粒度 | 粗 | 细 |
| 关系 | 一篇文章对应 **1** 个主题（互斥） | 一篇文章对应 **多个**标签 |
| 例子 | `AI`、`金融`、`编程`、`创业` | `OpenAI`、`GPT-5`、`Claude`、`FOMC` |
| 词汇来源 | **半开放**：种子列表 + AI 可扩展（B3） | **自由生成**：AI 按内容自定 |
| 用户画像表 | `interest_topics` (已有) | `interest_tags` (新增) |
| 文章列 | `articles.topic` (TEXT) | `articles.tags` (TEXT[]) |

**词汇自稳定（B3 半开放）**：每次调 AI 时把当前 DB 中最高频的 20 个 topics 喂给 prompt 当推荐列表，AI 见到 9 篇都标了"AI"，第 10 篇也大概率会用"AI"而非"人工智能"。新主题允许扩展但需要现实使用频次累计才能进入推荐列表。

## 目标

1. **接通主题 + 标签提取数据通路**：用户对文章的强信号（like / save / read_duration ≥ 60s）能可靠地累积到 `interest_topics` 与 `interest_tags`。
2. **稳定的洞察生成**：每日凌晨自动为活跃用户生成一次洞察并持久化；用户可手动触发流式重生成（带配额）。
3. **更高质量的 AI 输入**：分层（L1/L2/L3）+ 正负信号区分 + 时间窗口对比 + 主题/标签双层画像，token 预算可降级。
4. **基本的用户控制权**：可删除错误识别的主题与标签；新用户冷启动有引导。
5. **资源受控**：AI 调用次数 ≈ 文章数 + 每日活跃用户数（远小于"信号数 × 用户数"）；主题词汇自稳定无需人工治理。

## 非目标（本版不做）

- 主题/标签点击钻取到文章列表
- 洞察中嵌入推荐订阅源
- 洞察分享 / 导出公开链接
- 不喜欢信号削弱已有权重（仅"不增加"，避免复杂化）
- 跨用户的主题共现 / 聚类
- 主题别名归一化（如 "AI"="人工智能"）；依赖 B3 自稳定机制
- 实时（秒级）反映新信号到 interest_topics（接受最长 1 分钟延迟）

## 需求（拍板汇总）

| 维度 | 选择 |
|---|---|
| 主题/标签提取触发信号 | like、save、read_duration ≥ 60s（A+B+C） |
| 提取执行位置 | worker 周期扫描（每分钟一次） + 信号写入接口里命中缓存即时落库 |
| AI 调用 | **单次调用同时返回 topic + tags（JSON 输出）** |
| 主题词汇 | **B3 半开放**：种子列表 + DB 高频前 20 入推荐 + 允许新建 |
| 标签词汇 | 自由生成 |
| 文章级缓存 | `articles.topic` / `articles.tags`，AI 每篇仅调 1 次 |
| 衰减 | worker 每日 04:00 UTC+8 跑 `interest_topics` 与 `interest_tags` 双双 `weight *= 0.98`（半衰期 ≈ 34 天） |
| 洞察生成 | 每日 04:00 UTC+8 自动 + 用户手动流式 |
| 洞察持久化 | 新表 `user_insights`，追加式写入 |
| 用户配额 | 手动 3/日、100/月；自动不消耗配额；无 topics+tags 用户跳过自动 |
| AI 输入分层 | L1 仅 title / L2 + brief / L3 + detailed；正负信号分组；7d vs 30d；6000 token 预算降级 |
| 扩展项 | 主题/标签手动删除 + 空态引导 |
| 跳过 | 钻取、推荐订阅嵌入、分享按钮、不喜欢削弱权重、别名归一化 |

---

## 1. 架构总览与数据流

```
[用户行为] → [信号写入] → [worker 异步处理] → [洞察可读]

┌─────────────┐                                ┌──────────────────┐
│  Frontend   │   POST /preferences/{like,    │ user_preferences   │
│  /articles  │──save,read-duration}─────────►│   (signal logs)    │
└─────────────┘                                └────────┬───────────┘
   │                                                    │ 周期扫描
   │ 命中 articles.topic 缓存时                          │
   │ 直接 UpsertTopic + UpsertTag（同步、无 AI）         ▼
   │                                          ┌─────────────────────────┐
   │                                          │  Worker (每分钟)         │
   │                                          │  - 拉 RSS（已有）         │
   └─────────────────────────────────────────►│  - 扫描信号 → 调 AI       │
                                              │     单次 JSON 返回        │
                                              │     {topic, tags}         │
                                              │     缓存 articles.topic /  │
                                              │     articles.tags         │
                                              │  - UpsertTopic+UpsertTag  │
                                              └─────────────────────────┘

[每日 04:00 UTC+8]
   ├─ 衰减 interest_topics + interest_tags (weight *= 0.98)
   └─ 为每个非空 topics+tags 用户：调 AI 生成洞察 → user_insights (auto)

┌─────────────┐   GET /insights/latest          ┌──────────────────┐
│  Frontend   │◄──最新一条 + 配额─────────────  │  user_insights    │
│  /insights  │                                 │  (持久化、追加)   │
│             │   POST /insights/generate?      └──────────────────┘
│             │   stream=1 (NDJSON 流)                 ▲
│             │ ─────────────────────────────────────► │ (manual)
└─────────────┘                                        │
                                                  AI Streaming
```

### 关键架构边界

- **API server 不阻塞调 AI 做主题/标签提取**。like/save/read-duration 接口是 O(1)（仅 INSERT + 可选的 cached UpsertTopic/UpsertTag）。
- **Worker 单进程承担四个职责**：(1) RSS 拉取，(2) 主题+标签扫描提取，(3) 每日衰减，(4) 每日洞察生成。
- **AI 调用拓扑**：
  - 提取 = `O(N_articles_with_signals)`（每篇仅 1 次，靠 `articles.topic` 是否为 NULL 去重；返回 topic 与 tags 一次拿齐）
  - 每日洞察 = `O(N_active_users)`（仅 `interest_topics` 或 `interest_tags` 非空的用户）
  - 手动重生 = 受配额硬性 cap 在 `3 × N_users` 每日

---

## 2. 数据库变更

新建 migration 文件：`backend/migrations/008_insights.sql`。

```sql
-- 008_insights.sql

-- 1) articles.topic / articles.tags: 文章级缓存
ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS topic TEXT,        -- 单值，NULL 表示尚未分类
  ADD COLUMN IF NOT EXISTS tags  TEXT[];      -- 多值

-- 部分索引：加速 worker 扫描"未分类"的文章（topic 是 canonical 标记）
CREATE INDEX IF NOT EXISTS idx_articles_no_topic
  ON articles (id) WHERE topic IS NULL;

-- 2) interest_tags: 用户细粒度兴趣画像（与 interest_topics 平行）
CREATE TABLE IF NOT EXISTS interest_tags (
  id                 SERIAL PRIMARY KEY,
  user_id            INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tag                TEXT NOT NULL,
  weight             FLOAT NOT NULL DEFAULT 0,
  last_reinforced_at TIMESTAMP NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, tag)
);

CREATE INDEX idx_interest_tags_user_weight
  ON interest_tags (user_id, weight DESC);

-- 3) user_insights: 每次洞察生成的持久化记录
CREATE TABLE IF NOT EXISTS user_insights (
  id           SERIAL PRIMARY KEY,
  user_id      INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  content      TEXT NOT NULL,
  triggered_by VARCHAR(16) NOT NULL CHECK (triggered_by IN ('auto','manual')),
  model        VARCHAR(64),
  generated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_insights_user_latest
  ON user_insights (user_id, generated_at DESC);

CREATE INDEX idx_user_insights_quota
  ON user_insights (user_id, triggered_by, generated_at);
```

### 关键决策与依据

- `articles.topic`（单值）+ `articles.tags`（多值）反映概念区分；`topic IS NULL` 是 worker "未处理" 的 canonical 判定。
- `interest_tags` 与 `interest_topics` 结构对称，便于代码复用（同一套 Repository 模板）。
- `user_insights` **追加式写入**：最新一条用 `ORDER BY generated_at DESC LIMIT 1`；配额计数 `COUNT(*) WHERE triggered_by='manual' AND generated_at > NOW() - INTERVAL`。
- `triggered_by` CHECK 约束防止脏数据。
- `model` 列方便日后追踪不同 AI 模型对洞察质量的影响。
- 现有 `interest_topics` 表（migration 005 已加 `user_id` + `UNIQUE(user_id, topic)`）无需改动。

---

## 3. 主题+标签提取流水线（worker 扫描）

### 3.1 触发与时机

- **首次提取**：worker 每分钟一次 tick，扫"近 7 天内有强信号、且 `articles.topic IS NULL`" 的文章批量调 AI（一次调用同时返回 topic + tags）。
- **后续命中**：API server 在 like/save/read-duration handler 内，如果 `articles.topic IS NOT NULL`，**直接** UpsertTopic + UpsertTag（多个 tag 各一次 upsert），不调 AI、不阻塞。

### 3.2 SQL 查询要点

```sql
-- FindArticlesNeedingClassification(limit)
SELECT DISTINCT a.id, a.title, a.content
FROM articles a
JOIN user_preferences up ON up.article_id = a.id
WHERE a.topic IS NULL
  AND up.created_at > NOW() - INTERVAL '7 days'
  AND (
    up.signal_type IN ('like','save')
    OR (up.signal_type = 'read_duration' AND up.signal_value >= 60)
  )
LIMIT $1;

-- GetUsersWithStrongSignal(article_id)
-- 每个 user 对该文章的"最强信号强度"，用于权重计算
SELECT user_id,
       MAX(CASE signal_type
         WHEN 'save' THEN 2.0
         WHEN 'like' THEN 1.0
         WHEN 'read_duration' THEN CASE WHEN signal_value>=60 THEN 0.5 ELSE 0 END
       END) AS strength
FROM user_preferences
WHERE article_id = $1
GROUP BY user_id
HAVING MAX(...) > 0;

-- GetTopTopicVocabulary(limit) -- 喂给 AI 的推荐列表（B3）
SELECT topic, COUNT(*) AS freq
FROM articles
WHERE topic IS NOT NULL
GROUP BY topic
ORDER BY freq DESC
LIMIT $1;  -- 例如 20
```

### 3.3 AI 调用与提取后处理

新函数 `ai.ClassifyArticle` 取代原 `ExtractTopics`，返回结构化 JSON：

```go
type Classification struct {
    Topic string   `json:"topic"`
    Tags  []string `json:"tags"`
}

func (s *Summarizer) ClassifyArticle(ctx context.Context,
    title, content string, recommendedTopics []string) (*Classification, error) {
    prompt := fmt.Sprintf(`你是文章分类助手。请分析以下文章并返回 JSON：

{"topic": "...", "tags": ["...", "...", "..."]}

- topic：单选，最贴合的主题。优先从已有主题中选：[%s]，
  如均不贴合可创建新主题（控制在 2-4 字的中文名词）。
- tags：3-5 个具体关键词（人名、产品名、公司、概念）。

仅输出 JSON，无其他内容。

标题：%s

内容：
%s`, strings.Join(recommendedTopics, ", "), title, truncateContent(content))

    raw, err := s.call(ctx, prompt, 200)
    if err != nil { return nil, err }
    var cls Classification
    if err := json.Unmarshal([]byte(extractJSON(raw)), &cls); err != nil {
        return nil, fmt.Errorf("invalid classification JSON: %w", err)
    }
    return &cls, nil
}
```

`extractJSON` 容错：抠出第一个 `{...}` 块，去除 markdown 代码 fence。

```go
func scanAndClassify(ctx context.Context, deps Deps) error {
    candidates := articleRepo.FindArticlesNeedingClassification(ctx, 50)
    if len(candidates) == 0 { return nil }
    
    recommended := articleRepo.GetTopTopicVocabulary(ctx, 20)
    seedTopics := []string{"AI","金融","编程","创业","科技","时事","文化","健康"}
    vocab := unionPreserveOrder(recommended, seedTopics)

    for _, art := range candidates {
        cls, err := summarizer.ClassifyArticle(ctx, art.Title, art.Content, vocab)
        if err != nil { logSkip(art.ID, err); continue }

        // 即使空 tags 也写入空数组，避免反复重试
        articleRepo.SetClassification(ctx, art.ID, cls.Topic, cls.Tags)

        for _, u := range prefRepo.GetUsersWithStrongSignal(art.ID) {
            w := signalToWeight(u.Strength)
            if cls.Topic != "" {
                prefRepo.UpsertTopic(u.UserID, cls.Topic, w)
            }
            for _, t := range cls.Tags {
                prefRepo.UpsertTag(u.UserID, t, w * 0.5) // tag 权重半值
            }
        }
    }
    return nil
}
```

### 3.4 信号 → 权重映射

| 信号 | weightDelta（topic） | weightDelta（每个 tag） |
|---|---|---|
| save | +2.0 | +1.0 |
| like | +1.0 | +0.5 |
| read_duration ≥ 60s | +0.5 | +0.25 |
| dislike | 不增加（也不削弱） | 不增加 |

**Tag 权重 = topic 权重 × 0.5**：tags 数量更多（3-5 个/篇），同等 delta 会让 tag 权重失控；折半后两表数值规模更接近。

### 3.5 信号写入接口的缓存命中分支

伪代码（在 `prefHandler.Like/Save/RecordReadDuration`，写入 `user_preferences` 之后）:

```go
art, _ := articleRepo.GetClassification(articleID)
if art.Topic != "" { // 已分类
    w := signalToWeight(signalStrength)
    prefRepo.UpsertTopic(userID, art.Topic, w)
    for _, t := range art.Tags {
        prefRepo.UpsertTag(userID, t, w * 0.5)
    }
}
// 否则什么都不做，worker 1 分钟后捞起来
```

`read_duration` 接口仅在 `signal_value >= 60` 时执行此分支。

### 3.6 幂等性与失败处理

- `articles.topic IS NOT NULL` 永远跳过 worker 扫描，确保每篇 AI 仅调一次。
- AI 失败 / JSON 解析失败：`articles.topic` 保持 `NULL`，下个 tick 自动重试；连续 3 次失败应记录但不阻塞 batch（用内存计数器，进程重启则重置；非关键路径）。
- 空提取结果（AI 返回空 topic / 空 tags）：写入空字符串 / 空数组，避免无限重试；`UpsertTopic` 仅在非空时执行。

---

## 4. 洞察生成（自动 + 手动流式）

### 4.1 每日 cron（worker 进程内）

启动时计算下次 04:00 UTC+8 的绝对时间，用 `time.AfterFunc` 触发；触发后再排下一次（24h 后）。

```go
func generateDailyInsights(ctx, deps) {
    // 衰减必须在生成洞察"之前"
    prefRepo.DecayAllTopics(0.98)
    prefRepo.DecayAllTags(0.98)

    for _, u := range userRepo.ListAll() {
        topics := prefRepo.GetTopics(u.ID)
        tags   := prefRepo.GetTags(u.ID)
        if len(topics) == 0 && len(tags) == 0 { continue }

        prompt := buildLayeredPrompt(u.ID, topics, tags)
        content, err := summarizer.Call(ctx, prompt, 1500)
        if err != nil { log.Warn(...); continue }

        userInsightsRepo.Insert(u.ID, content, "auto", model)
    }
}
```

`DecayAllTopics` / `DecayAllTags` 各一条 SQL：
```sql
UPDATE interest_topics SET weight = weight * $1 WHERE weight > 0.01;
UPDATE interest_tags   SET weight = weight * $1 WHERE weight > 0.01;
```

### 4.2 端点定义

```
GET    /api/insights/latest
       → 200 {
           insight: {content, generated_at, triggered_by, model} | null,
           remaining_today: int,
           remaining_month: int
         }

POST   /api/insights/generate          (向后兼容，非流式)
       → 200 {insights, message?, remaining_today, remaining_month}
       → 429 {error:"quota_exceeded", remaining_today, remaining_month}

POST   /api/insights/generate?stream=1 (新，NDJSON 流式)
       → 始终 200 application/x-ndjson（错误也用 frame 而非 HTTP code 传递，
         与 generateSummaryStream 保持一致）:

         成功:
           {"type":"delta","text":"..."}\n
           ...
           {"type":"done","full":"...","remaining_today":1,"remaining_month":88}\n

         配额耗尽 / AI 错误:
           {"type":"error","msg":"quota_exceeded","remaining_today":0,"remaining_month":N}\n
         （遇到 error frame 后流即结束，不再 emit done）

GET    /api/preferences/tags                 (新增)
       → 200 [{id, tag, weight, last_reinforced_at}, ...]

DELETE /api/preferences/topics/:id
       → 204 No Content（删除属于当前用户的主题；非本人返回 404）

DELETE /api/preferences/tags/:id
       → 204 No Content（删除属于当前用户的标签；非本人返回 404）
```

### 4.3 配额检查

```go
func checkQuota(userID int) (todayLeft, monthLeft int, ok bool) {
    today := userInsightsRepo.CountManualSince(userID, "1 day")
    month := userInsightsRepo.CountManualSince(userID, "30 days")
    todayLeft = max(0, 3 - today)
    monthLeft = max(0, 100 - month)
    ok = todayLeft > 0 && monthLeft > 0
    return
}
```

写入 `user_insights` 仅在流正常完成（或非流式调用成功）时进行，中途断流不写入半截内容。

### 4.4 分层 Prompt 构造

```go
func buildLayeredPrompt(userID int, topics []InterestTopic, tags []InterestTag) string {
    // L3: top 3 — 7天内 save+like 重合 / 信号最强
    l3 := getStrongest(userID, 3, "7 days")     // 含 detailed
    // L2: top 10 强信号文章（save/like/read≥60s），排除 L3
    l2 := getStrongSignals(userID, 10, "30 days", excludeIDs(l3))  // 含 brief
    // L1: 30 天内交互过的所有文章（排除 L2/L3）
    l1 := getAllInteracted(userID, "30 days", excludeIDs(l3, l2))  // 仅 title

    // 估算 token 数；超 6000 → L3 降级 brief；再超 → L2 降级 title-only
    return chineseTemplate(topics, tags, l1, l2, l3, splitByPolarity, splitByTimeWindow)
}
```

### 4.5 Prompt 模板（中文）

```
基于用户的兴趣画像与多层级阅读行为，请进行个性化洞察分析。

## 用户兴趣主题（粗粒度，按权重，已做时间衰减）
- AI ({weight})
- 编程 ({weight})
...

## 用户关键词（细粒度，top 20，按权重）
- OpenAI ({w}), Claude ({w}), GPT-5 ({w}), FOMC ({w}), ...

## 高强度信号（深度互动）

### 近 7 天 · 收藏+点赞重合
1. [详细] 标题：xxx
   主题：AI · 标签：[OpenAI, GPT-5]
   摘要：xxx (detailed summary)

### 近 30 天 · 强信号
- [收藏] 标题：xxx
  主题：编程 · 标签：[Rust, async]
  要点：xxx (brief)
- [点赞] 标题：xxx
  ...

## 浏览过的文章（仅标题，附主题）
- 近 7 天：[t1 (AI), t2 (金融), ...]
- 近 30 天：[t...]

## 不感兴趣
- [不喜欢] 标题：xxx · 主题：xxx

请用中文 markdown 输出：
1. **核心兴趣领域**（3-5 个，按确定性排序，结合主题与高频标签）
2. **近期偏好变化**（对比 7d vs 30d）
3. **可能的新兴趣点**（弱信号但反复出现）
4. **阅读建议**（结合"不感兴趣"做反向建议）
```

每日 cron 用非流式 `summarizer.Call`；手动用 `summarizer.callStream`，复用 `internal/ai/summarizer.go` 已有 streaming 基础设施。

---

## 5. 前端改动

### 5.1 InsightsPage 状态机

```
INITIAL ─loadLatest()─► HAS_INSIGHT (展示上次结果 + 配额)
                  │
                  └─► EMPTY (无 topics+tags && 无 latest) ─► 空态引导

HAS_INSIGHT ─click 重新生成─► STREAMING (打字机)
                                    │
                                    ├─done──► HAS_INSIGHT (新内容 + 配额-1)
                                    └─error─► toast，保留旧内容
```

页面加载时**并行**调 `GET /preferences/topics`、`GET /preferences/tags`、`GET /insights/latest`；展示主题云、关键词云、最近一条洞察。

### 5.2 空态（无 topics+tags）

```
┌────────────────────────────────────────────────┐
│  💡 还没有足够数据生成洞察                       │
│                                                 │
│  洞察基于你对文章的反应生成。试着：              │
│   • 多阅读一会文章                              │
│   • 给文章点个 ❤️                                │
│   • 收藏感兴趣的文章                             │
│                                                 │
│  [去阅读文章 →]                                  │
└────────────────────────────────────────────────┘
```

判定条件：`topics.length === 0 && tags.length === 0 && latestInsight === null`。点击按钮跳转 `/articles`。

### 5.3 双层标签云布局

```
┌────────────────────────────────────────────────┐
│ 兴趣主题                                        │
│  [AI]   [编程]   [金融]   [创业]                 │ ← 字号大、间距宽
│                                                 │
├────────────────────────────────────────────────┤
│ 关键词                                          │
│  [OpenAI] [Claude] [GPT-5] [FOMC] [Rust] [async]│ ← 字号小、密度高
│  [推理] [大模型] [Anthropic] ...                │
└────────────────────────────────────────────────┘
```

主题云：top 不限（一般个位数），字号按权重缩放 (16-26px)；关键词云：top 30，字号 12-16px。

### 5.4 流式 / 打字机 / API 客户端

复用 `client.ts` 中现有 NDJSON 模式（参考 `generateSummaryStream`）：

```ts
export interface InterestTag {
  id: number
  tag: string
  weight: number
  last_reinforced_at: string
}

export type InsightStreamHandlers = {
  onDelta?: (text: string) => void
  onDone?: (full: string, quota: { remaining_today: number; remaining_month: number }) => void
  onError?: (msg: string) => void
}

export async function generateInsightsStream(
  handlers: InsightStreamHandlers,
  signal?: AbortSignal,
): Promise<void> { /* fetch /api/insights/generate?stream=1, parse NDJSON */ }

export const getLatestInsights = () =>
  api.get<{
    insight: { content: string; generated_at: string; triggered_by: 'auto'|'manual'; model?: string } | null
    remaining_today: number
    remaining_month: number
  }>('/insights/latest').then(r => r.data)

export const getTags = () =>
  api.get<InterestTag[]>('/preferences/tags').then(r => r.data)

export const deleteTopic = (id: number) =>
  api.delete(`/preferences/topics/${id}`)

export const deleteTag = (id: number) =>
  api.delete(`/preferences/tags/${id}`)
```

### 5.5 删除交互（主题与标签同模式）

hover（桌面）或 长按（移动）显示 `×`；点击立即从本地 state 移除（乐观更新），后端失败再回滚 + toast。两层标签云共用一套 `<Chip>` 组件，区别仅在传入的删除回调和样式 props。

### 5.6 按钮配额展示

```tsx
const quotaLabel = remainingToday === 0
  ? '今日已达上限'
  : `重新生成 (今日 ${3 - remainingToday}/3)`

<button disabled={generating || remainingToday === 0}
        title={`今日剩 ${remainingToday} 次 · 本月剩 ${remainingMonth} 次`}>
  {generating ? '分析中...' : quotaLabel}
</button>
```

最近生成时间副标题：`*2 小时前 · 由系统自动生成*` 或 `*5 分钟前 · 你触发的*`。

---

## 6. 验收标准

实现完成时应能验证：

1. **分类通路**：新用户对 ≥3 篇文章点 like/save，1 分钟内 `interest_topics` 与 `interest_tags` 均可见相应记录。
2. **缓存命中**：对同一篇文章再点 like，无新 AI 调用，但 `interest_topics.weight` 与 `interest_tags.weight` 累加。
3. **词汇自稳定**：mock 9 篇 AI 返回 `topic="AI"` 后，第 10 篇即使内容相似度低，prompt 中推荐列表前列也是 "AI"，AI 复用率应高（人工抽样确认）。
4. **衰减**：手动调用 `DecayAllTopics(0.5)` + `DecayAllTags(0.5)` 后，所有用户两表 weight 减半。
5. **每日 cron**：将系统时间调到 04:00 UTC+8 前 1 分钟（或缩短间隔做单元测试），触发后 `user_insights` 中每个非空画像用户多一条 `triggered_by='auto'`。
6. **流式**：前端"重新生成"产生肉眼可见的逐字打字机效果，结束后 DB 多一条 `triggered_by='manual'`。
7. **配额**：连点 3 次"重新生成"后按钮禁用、文案为"今日已达上限"；第 4 次直接调 API 返回 429（非流式）/ error frame（流式）。
8. **空态**：新注册用户打开 `/insights` 看到引导卡片，点击跳到 `/articles`。
9. **删除**：hover 主题/标签都出现 `×`；点击后从对应表删除；刷新页面仍消失。
10. **分层降级**：在 `topics+tags` 数 > 50 且强信号文章 > 20 篇时（mock 数据），prompt 总长度不超过 6000 token；超时降级生效。

## 7. 风险与缓解

| 风险 | 缓解 |
|---|---|
| AI 返回非 JSON / JSON 损坏 | `extractJSON` 容错（抠 `{...}` 块）+ 失败重试 + 3 次失败后写入空值跳过 |
| AI 滥用新主题（创造同义词） | B3 自稳定机制（DB 高频前 20 入推荐）；可选事后人工合并（非本版） |
| Tag 权重失控（数量太多） | tag delta = topic delta × 0.5；衰减保留 |
| 每日 cron 在大量用户时占用 worker 太久 | cron 在独立 goroutine，每用户间 sleep 200ms 限流 |
| Prompt 超出模型上下文 | token 估算 + 分层降级；最坏退到仅 L1 标题 |
| 用户删除主题后，下一次扫描又会因 cached `articles.topic` 重新加回 | 删除是"用户级"操作（删 `interest_topics` 记录），但 `articles.topic` 是文章级缓存仍保留；如果用户再次对同一文章发信号会被加回 —— 这是预期行为 |
| Worker 进程崩溃丢失正在进行的 batch | 全部依赖 DB 状态，重启后自然 resume；无内存队列 |

## 8. 端点变更清单（汇总）

```
新增:
  GET    /api/insights/latest
  POST   /api/insights/generate?stream=1   (NDJSON)
  GET    /api/preferences/tags
  DELETE /api/preferences/topics/:id
  DELETE /api/preferences/tags/:id

兼容修改:
  POST   /api/insights/generate            (响应增加 remaining_today/remaining_month)

不变:
  GET    /api/preferences/topics
```

## 9. 不引入的依赖

- 无新引入第三方库；流式复用 `bufio` 与现有 NDJSON 协议；JSON 解析用标准库 `encoding/json`。
- 无消息队列；调度用 `time.AfterFunc` + worker 内 goroutine。
- 无独立 cron 进程；继续在现有 worker 二进制内承担。
