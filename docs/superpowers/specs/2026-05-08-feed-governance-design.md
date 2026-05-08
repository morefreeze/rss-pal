# Feed 治理（Phase 1）— 设计文档

**日期：** 2026-05-08
**范围：** 行为打点修复 + feed 健康度仪表盘 + 自动剪荐建议 + feed 状态管理
**触发：** 阅读 BestBlogs 2.0 内测公告（article id 1572）后的方向梳理。在自用语境下提炼"核心竞争力 = 让 rss-pal 慢慢学懂我自己的判断标准"。
**分阶段：** 本 spec 只覆盖 Phase 1（feed 治理），Phase 2（早报 + verdict + 反馈闭环）单独 spec。

---

## 1. 背景与定位

### 1.1 问题陈述
当前 rss-pal 已有 19 个活跃 feed、1752 篇文章，但作者打开列表时仍持续遭遇四个痛点：

1. 列表太多，不知道今天先看哪几条（入口判断成本高）
2. 点开后才发现不值得读完（缺 verdict）
3. 读完很快忘了，没沉淀回路
4. feed 越来越多、质量良莠不齐，不知道哪些可以剪掉（订阅源治理盲区）

这四个痛点其实是同一条数据闭环上的四段：
```
feeds → articles → verdict → reading → 沉淀/反馈
  ↑                                       │
  └───────────── 调整 verdict + feed 治理 ──┘
```
痛点 1+2 是输出面，3 是反馈采样，4 是输入侧调优。

### 1.2 核心竞争力（自用语境）
> **让 rss-pal 从「能聚合 RSS 的阅读器」变成「会慢慢学懂我自己判断标准的阅读副驾」。**
>
> 通用 RSS 阅读器没有作者的长期反馈数据，通用 LLM 没有作者的阅读历史 —— 两者结合才是不可替代点。

### 1.3 实施排序
- **Phase 1（本 spec）**：feed 治理（输入侧）。先把行为数据基础打牢，再把噪音源头剪干净。
- **Phase 2（后续 spec）**：早报 + verdict + 反馈闭环（输出面 + 采样 + 学习）。

排序理由：Phase 1 把行为打点修对，Phase 2 verdict 学习有可信 ground truth；如果直接做 Phase 2，verdict 准确度评估会建在歪数据上。

---

## 2. 当前数据基础与缺陷

| 资产 | 状况 |
|------|------|
| `feeds` (19 行) | 有 `is_active` bool，无状态机；无优先级权重 |
| `articles` (1752 行) | 有 word_count / reading_minutes / topic / tags |
| `reading_progress` (1742 行) | **`is_completed` 99.8% = true**，失真严重 |
| `user_preferences` | read_duration 107 / like 13 / dislike 7 / save 1 — 稀疏 |

**关键问题**：
- 完读阈值 `scrollPosition > 0.9`（`frontend/src/pages/ArticlePage.tsx:190`）短文章一打开就过 → `is_completed` 失真
- 没有曝光事件 → 算不出某 feed 文章在我面前出现过几次
- 没有点击事件 → 没法区分"曝光但没点开"
- read_duration 覆盖率 6% → 行为信号稀疏

---

## 3. 范围与非目标

### 3.1 In-scope（Phase 1）
1. 行为打点修复：完读阈值修正 + 曝光（≥10s 入视窗）+ 点击事件
2. feed 健康度指标计算：6 个核心指标 + 综合价值得分
3. `/feeds/health` 仪表盘页：顶部 KPI + 表格 + 行尾 warning
4. 自动剪荐建议：5 类判据 + 抽屉式 review
5. feed 状态管理：active / paused / archived 三态 + priority_weight

### 3.2 Out-of-scope（Phase 2 或更后）
- 早报、verdict、个性化推荐打分
- AI 伴读
- 历史阅读全文搜索
- 阅读后沉淀（笔记/复盘）
- feed 健康度的定时/邮件提醒
- 阈值的用户配置 UI（写死在 `internal/config`）

### 3.3 硬约束
- **曝光定义**：文章卡片进入视窗 ≥ 10s 才算曝光
- **分支策略**：从最新 `master` 拉新分支开发，不延续 `feature/transcript-summary`
- **建议而非自动**：所有处置都是用户点击后才生效，不自动归档

---

## 4. 数据模型（新 migration）

### 4.1 新表：`article_events`
事件型采样表，记录用户与文章的交互信号。

```sql
CREATE TABLE article_events (
    id           BIGSERIAL PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    article_id   INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    event_type   VARCHAR(32) NOT NULL,
    occurred_at  TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_article_events_user_time ON article_events (user_id, occurred_at DESC);
CREATE INDEX idx_article_events_article ON article_events (article_id);
CREATE INDEX idx_article_events_type ON article_events (event_type);
```

`event_type` 取值：
- `exposure` — 文章卡片进入视窗 ≥ 10s（同一文章在同一 session 内只记一次）
- `click` — 用户从列表点开文章（导航跳转前打点）
- `completed_read` — 完读判定通过时（见 4.2）

去重原则：
- exposure / click 在同一 session（浏览器会话）内对同一 article_id 只记一次
- completed_read 全局只记一次（已读不再触发）

### 4.2 修改：`reading_progress` 完读判定
完读阈值改为 `scrollPosition > 0.9 AND 累计停留时长 ≥ MIN(30s, reading_minutes × 60 × 0.5)`。

实现：前端在记录 `is_completed=true` 时，先检查累计停留时长（已有的 read_duration 累计可复用）。低于门槛则只更新 scroll_position，不翻 is_completed。

**历史数据回填**：不回填。已有 `is_completed=true` 的 1739 条保留现状（前端列表"已读"标记继续读 `reading_progress.is_completed`，不受影响）。Feed 健康度指标只看 `article_events.event_type='completed_read'`，自打点修复上线日起累计 —— 上线前的历史数据自然不进指标，无需任何"legacy"标注。

### 4.3 修改：`feeds` 加 status + priority_weight
```sql
ALTER TABLE feeds ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active';
-- status 取值：'active' | 'paused' | 'archived'
ALTER TABLE feeds ADD COLUMN priority_weight DOUBLE PRECISION NOT NULL DEFAULT 1.0;

-- 迁移：保守地把已 inactive 的迁到 paused（不直接 archived）
UPDATE feeds SET status = 'paused' WHERE is_active = false;
UPDATE feeds SET status = 'active' WHERE is_active = true;
```

**`is_active` 字段保留**：现有 SQL 大量用 `is_active`，迁移期间双写（status 写入时同步 is_active）。新读取 query 改用 `status = 'active'`。后续 Phase 2 之后再 drop `is_active`。

`priority_weight` 在 Phase 1 不参与查询逻辑，只是数据通道；Phase 2 verdict 打分会读它。

---

## 5. 行为打点修复（Component 1）

### 5.1 后端
新增 endpoint：
```
POST /api/events
Body: { article_id: int, event_type: "exposure" | "click" }
```
`completed_read` 事件由后端在 `POST /api/progress/:id` 翻 `is_completed=true` 时自动写入，不走前端。

去重：前端按 sessionStorage 维护已上报集合（per session, per article_id, per event_type），避免重复打。

### 5.2 前端 — 曝光打点
- 列表页（`/articles`、`/feeds/:id`、未来的早报）每个文章卡片用 `IntersectionObserver`
- threshold = 0.5（卡片一半可见即开始计时）
- 计时 ≥ 10s 不间断 → 触发一次曝光事件
- 离开视窗中断计时；重新入视窗重新计时
- 同一文章 session 内只上报一次

### 5.3 前端 — 点击打点
- 列表卡片点击跳转前 `await postEvent(articleId, 'click')`，再 `navigate`
- bookmarklet / share token 进入路径不算点击（属于直达，不是从列表选出来的）

### 5.4 前端 — 完读修复
- `ArticlePage.tsx:190` 处增加停留时长门槛
- 新增局部累计计时器：`activeReadSeconds`（仅页面可见 + scroll/click 在 60s 内）
- 完读条件：`scrollPosition > 0.9 AND activeReadSeconds >= MIN(30, reading_minutes * 30)`
- 触发完读时调一次 `POST /api/progress/:id`（已有），后端在翻 false→true 时写 `article_events.completed_read`

---

## 6. 健康度指标（Component 2）

每个 feed 在窗口期 W ∈ {30d, 90d} 内计算：

| 指标 | 公式 | 用途 |
|------|------|------|
| `produced` | 该 feed 在 W 内抓回的文章数 | feed 是否还在更 |
| `ctr` | `count(click) / count(exposure)` | 标题 → 打开决策 |
| `read_completion` | `count(completed_read) / count(click)` | 打开 → 读完转化 |
| `avg_duration` | 该 feed 文章 read_duration 中位数（分钟） | 投入度 |
| `last_active_at` | 最后一次该 feed 文章的 click 或 completed_read 时间 | 沉睡判定 |
| `value_score` | 见下 | 单一可排序数字 |

### 6.1 价值得分公式
```
value_score = 0.35 × ctr
            + 0.35 × read_completion
            + 0.20 × normalize(avg_duration, target=10min)
            + 0.10 × normalize(feedback_density, target=5)
```

- `normalize(x, target)` = `min(x / target, 1.0)`
- `feedback_density` = `like + save - dislike`（W 期内 user_preferences 信号汇总）
- `avg_duration` 无样本时该项视为 0（不让它阻止 value_score 计算）；前端在指标列单独显示 `—`

**冷启动**：曝光 < 10 时 `value_score` 设为 `null`（前端显示"样本不足"），不参与排序，也不进入剪荐候选。

**注意**：当前 `read_duration` 覆盖率仅 6%。如打点修复上线后覆盖率仍 < 50%，`avg_duration` 仅作参考列展示，不参与 `value_score`（系数 0.20 临时挪到 ctr 和 read_completion 各加 0.10）。这是上线后第一次回看时的应急切换，不在初版实现里。

### 6.2 计算实现
- 后端 `internal/service/feed_health.go` 新增 `ComputeHealth(userID int, window time.Duration) []FeedHealth`
- 一次 SQL 聚合所有 feed 指标（CTE + LEFT JOIN article_events / user_preferences）
- 不缓存：19 个 feed 量小，每次进入 `/feeds/health` 实时算（< 200ms 预期）
- 如未来扩到 100+ feed，再加 5min memory cache

---

## 7. 仪表盘 UI（Component 3）

### 7.1 路由与导航
- 新增 `/feeds/health`
- 现有 `/feeds` 顶部加按钮 `健康度 →`，互相挂跳转
- 现有 `/feeds` 不变（"管理订阅源"职责）

### 7.2 页面结构
```
┌─────────────────────────────────────────┐
│ [30d ▼]  ⚠️ N 个 feed 建议处理 [展开]    │
├─────────────────────────────────────────┤
│  KPI:  [总19] [健康12] [沉睡5] [我读了X篇] │
├─────────────────────────────────────────┤
│ Feed             产出 CTR 完读 时长 最近  分  动作  ⚠ │
│ Anthropic Blog    8   0.6  0.7   12m  2d ago 0.65 [...] │
│ ...                                                     │
├─────────────────────────────────────────┤
│ ▸ 已归档 (3)                            │
└─────────────────────────────────────────┘
```

### 7.3 交互细节
- **顶部时间窗口**：30d 默认，可切 90d；选择存 localStorage
- **KPI 卡片**：4 个数字，无图表
  - 总 = 所有 status='active' 的 feed
  - 健康 = value_score >= 0.3
  - 沉睡 = 命中"沉睡型"剪荐判据
  - 我读了 = W 内 completed_read 事件总数
- **表格**：默认按 value_score 降序；任意列点击切排序
- **行尾 warning icon** ⚠：命中任一剪荐判据时显示，hover 显示原因
- **行尾动作**：3 个图标按钮 `暂停 / 归档 / 降权`（降权弹小窗输入新 priority_weight）
- **行点击**：进入 `/feeds/:id`（既有页面，列出该 feed 文章）
- **底部"已归档"**：折叠区，列 status='archived' 的 feed，每行一个"恢复"按钮

### 7.4 移动端
- 表格 `overflow-x-auto` 横滑
- KPI 卡片改为 2×2 网格
- Phase 1 不做卡片视图 fallback（YAGNI，作者主桌面）

---

## 8. 自动剪荐（Component 4）

### 8.1 判据（按优先级）
所有判据基于 30d 窗口（不是用户切换的窗口；剪荐固定 30d 视角，避免切到 90d 时所有候选消失）。

| ID | 类型 | 判据 | 推荐处置 |
|----|------|------|---------|
| R1 | 完全失效 | 90d 0 文章 AND 90d 0 点击 | 归档 |
| R2 | 沉睡型 | 30d 0 点击 AND 30d 文章数 ≥ 3（≥3 是为了和 R3 区分；feed 没出文章不能算我"沉睡"） | 归档 |
| R3 | 死源型 | 30d feed 抓回 0 篇文章 | 暂停 + 检查源 |
| R4 | 低价值 | value_score < 0.1 AND 30d 文章数 ≥ 10 | 归档 / 降权 |
| R5 | 过水型 | 30d 文章数 > 100 AND read_completion < 0.05 | 降权 |

判据互斥优先级：R1 > R2 > R3 > R4 > R5（一个 feed 命中多条只显示最严重的）。

### 8.2 阈值配置
全部硬编码在 `internal/config/feedhealth.go`：
```go
type FeedHealthConfig struct {
    DormantClickWindow         time.Duration // 30d
    DeadFeedArticleWindow      time.Duration // 30d
    FullyDeadWindow            time.Duration // 90d
    LowValueScoreThreshold     float64       // 0.1
    LowValueMinSampleSize      int           // 10
    HighVolumeArticleCount     int           // 100
    HighVolumeMinCompletionRate float64      // 0.05
    ColdStartMinExposures      int           // 10
}
```
不暴露给前端，不做 UI；以后想调改常量重启。

### 8.3 UI
- 顶部 banner：`⚠️ N 个 feed 建议处理 [展开]`
- 展开抽屉：列出所有候选 feed
  ```
  ┌──────────────────────────────┐
  │ Feed Foo  [沉睡型]  归档 暂停 暂不│
  │ 原因：30d 内你 0 次点击该 feed   │
  │                              │
  │ Feed Bar  [低价值] 归档 降权 暂不│
  │ 原因：value=0.06，30d 共 12 篇 │
  └──────────────────────────────┘
  ```
- 三种动作：
  - **归档** → status='archived'
  - **暂停** → status='paused'
  - **降权** → priority_weight 设 0.5
  - **暂不处理** → 仅本次会话内隐藏（不入库），刷新后重新出现

---

## 9. 状态管理（Component 5）

### 9.1 三态语义
| 状态 | worker 抓取 | 显示在 /articles | 显示在 /feeds | 显示在 /feeds/health 主表 |
|------|------------|-----------------|--------------|-------------------------|
| active | ✓ | ✓ | ✓ | ✓ |
| paused | ✗ | ✓（已有文章） | ✓（带 paused badge） | ✓（带状态标记） |
| archived | ✗ | ✗ | ✗ | 折叠区"已归档" |

### 9.2 priority_weight
- Phase 1：仅作为字段存储，不参与任何查询
- Phase 2：verdict 打分时 `final_score = base_score × feed.priority_weight`
- 取值：默认 1.0；降权动作设 0.5；用户可手动设 0.0–2.0（详情页提供 slider）

### 9.3 API
新增：
```
PATCH /api/feeds/:id/status   Body: { status: "paused" | "active" | "archived" }
PATCH /api/feeds/:id/weight   Body: { priority_weight: float }
```

worker 启动循环里 `WHERE status = 'active'` 替换 `WHERE is_active = true`。

---

## 10. 实施顺序与里程碑

### M1 — 数据 schema + 后端基础（1.5d）
1. Migration: `article_events` 表 + `feeds.status` + `feeds.priority_weight`
2. `internal/repository/event.go` + 事件去重逻辑
3. `POST /api/events` endpoint + auth middleware
4. worker 切到 `status='active'`

### M2 — 前端打点（1d）
1. `useExposureTracking` hook（IntersectionObserver + 10s 计时）
2. 列表卡片接入曝光 / 点击打点
3. `ArticlePage.tsx` 完读判定加停留时长门槛

### M3 — 健康度指标计算 + API（1d）
1. `service/feed_health.go` 聚合 SQL
2. `GET /api/feeds/health?window=30d|90d` endpoint

### M4 — `/feeds/health` 页面（1.5d）
1. 路由、页面骨架、KPI 卡片
2. 表格组件 + 排序 + warning icon
3. localStorage 时间窗口偏好

### M5 — 剪荐建议 + 状态管理 UI（1d）
1. 顶部 banner + 抽屉
2. 行尾动作按钮 + 状态变更 API 接入
3. "已归档"折叠区 + 恢复

### M6 — 验证（0.5d）
1. 在真实数据上跑一遍：检查 19 个 feed 的指标合理性
2. 制造一个明显低质量的 feed 验证剪荐判据触发
3. 切窗口 30d/90d 验证

**总工作量预估**：约 6.5 工作日

---

## 11. 验证标准

Phase 1 完成的判据：
1. 打开 `/feeds/health` 后 5 秒内能完成"今天哪些 feed 我应该剪掉"的判断
2. 至少有 1 个 feed 被实际归档或降权（来自剪荐建议）
3. 4 周后回看：`is_completed` 不再是 99.8%，而是反映真实读完率（预期 30-60%）
4. 4 周后回看：`article_events` 累计 ≥ 500 条曝光、≥ 50 条点击 → 为 Phase 2 verdict 学习提供 ground truth

---

## 12. 与 Phase 2 的衔接

Phase 1 产出对 Phase 2 必要：
- `article_events` 是 verdict 学习的 ground truth（曝光→点击 = 标题判断；点击→完读 = 内容判断）
- `feeds.priority_weight` 是 verdict 打分的乘数项
- 健康面板上的 feed 排序逻辑可直接复用为 verdict 中"feed 信誉分"的初始值
- 修复后的 `is_completed` 是衡量 verdict 准确度的标尺

Phase 2 在 Phase 1 上线后至少 4 周开始（让数据攒起来），然后启动单独 spec。
