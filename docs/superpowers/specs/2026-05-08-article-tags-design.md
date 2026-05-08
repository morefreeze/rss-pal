# 文章 Tag 与「我的收藏」页设计

**日期**：2026-05-08
**状态**：Spec — 待实施

## 背景与动机

收藏的文章数量增长后，用户难以从一长串列表中快速区分内容。需要一种轻量的归类机制，让用户能：

1. 在文章页给文章打标签（tag）以便归类
2. 自动按来源（feed）给文章打 tag，相同网站的文章天然归为一类
3. 在专门的「我的收藏」页用 tag 浏览/筛选收藏内容

设计采用「**手动 tag + 来源 tag（派生）+ AI 建议 tag（来自现有分类缓存，采纳后转手动）**」三类协同的方案。

## 范围

- Tag 是文章本身的属性，所有文章都可以打 tag；UI 主推「我的收藏」页（专页），普通文章列表只在卡片上展示已绑定的手动 tag
- 用户私有：手动 tag 与「忽略 AI 建议」状态都是 per-user
- AI 建议候选**直接复用现有的 `articles.tags TEXT[]`**（已由 worker 写入），所有用户共享同一份候选

## 现有相关基础设施（不在本期改动）

代码里已经存在与「tag」相关的设施，本设计**复用**它们而非另起炉灶：

| 名字 | 类型 | 作用 |
|---|---|---|
| `articles.topic TEXT` | 列 | AI 给文章打的主题（单选） |
| `articles.tags TEXT[]` | 列 | AI 给文章打的 3-5 个 tag（候选池） |
| `interest_tags` | 表 | per-user 的 tag 权重，用于推荐打分（隐式信号，**不是**用户主动打的 tag）|
| `cmd/worker/classify.go` | 进程 | 扫「有强信号但 `topic IS NULL`」的文章，调 `ClassifyArticle` 写回 `articles.topic` + `articles.tags` |
| `applyCachedClassification` | 函数 | 收藏/点赞后把缓存的 topic+tags 喂进 `interest_topics` / `interest_tags` 加权 |

收藏（`save` signal）是 classify worker 的强信号触发器之一。这意味着：**用户收藏一篇老文章时会自动排队分类，几分钟内 `articles.tags` 就会被填上**。本设计的「AI 建议」直接读这个字段，无需新表也无需独立 backfill 脚本。

## 命名

为避免与现有的 `articles.tags`（列）和 `interest_tags`（表）混淆，本期新增的表统一带 `user_` 前缀：

- `user_tags` —— 用户手动创建的 tag 字典（per-user）
- `article_user_tags` —— 文章与手动 tag 的多对多绑定
- `tag_suggestion_dismissals` —— 用户「忽略」某文章的某条 AI 建议

## 设计概览

| 类别 | 显示样式 | 数据来源 | 用户能否编辑 |
|---|---|---|---|
| **来源 tag** | `📡 ` 前缀 + 灰底 chip | 派生自 `articles.feed_id → feeds.title`，**不入表** | 否（feed 改名自动跟） |
| **手动 tag** | 彩色 chip（按 tag 名 hash 配色），可点 ✕ 删除 | `user_tags` + `article_user_tags` | 是 |
| **AI 建议 tag** | 虚线 chip + ⊕ 前缀 | `articles.tags` 数组（现有）减去用户已采纳/已忽略的 | 「采纳」升级为手动 tag；「忽略」写入 dismissal |

## 数据模型

新增 2 张表（不再有 `ai_tag_suggestions`），迁移文件 `backend/migrations/016_user_tags.sql`：

```sql
-- 用户自定义 tag 字典（per-user）
CREATE TABLE user_tags (
    id          SERIAL PRIMARY KEY,
    user_id     INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        VARCHAR(64) NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW(),
    UNIQUE (user_id, name)
);

-- 文章 ↔ 手动 tag 多对多
CREATE TABLE article_user_tags (
    article_id  INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    tag_id      INT NOT NULL REFERENCES user_tags(id) ON DELETE CASCADE,
    user_id     INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (article_id, tag_id)
);
CREATE INDEX idx_article_user_tags_user_tag ON article_user_tags(user_id, tag_id);
CREATE INDEX idx_article_user_tags_user_article ON article_user_tags(user_id, article_id);

-- 用户对某文章某条 AI 建议（即 articles.tags 数组中的一项）的「忽略」
CREATE TABLE tag_suggestion_dismissals (
    article_id INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (article_id, user_id, name)
);
```

注意：
- `VARCHAR(64)` 在 PostgreSQL 中是字符数，可放下 64 个中文字符
- 来源 tag **不进表**，全部派生
- AI 建议「采纳」状态由 `article_user_tags` 是否存在该 tag 来表达，不另存
- AI 建议候选直接读 `articles.tags`，不复制到任何新表

### 「该用户在某文章上待显示的 AI 建议」查询

```sql
SELECT t AS name
FROM unnest((SELECT tags FROM articles WHERE id = $2)) AS t
WHERE NOT EXISTS (
    SELECT 1 FROM tag_suggestion_dismissals
    WHERE article_id = $2 AND user_id = $1 AND name = t
)
AND NOT EXISTS (
    SELECT 1 FROM user_tags ut
    JOIN article_user_tags aut ON aut.tag_id = ut.id
    WHERE ut.user_id = $1 AND ut.name = t AND aut.article_id = $2
)
LIMIT 5;
```

## 后端架构

层级遵循现有约定：`api/` → `service/` → `repository/` → `model/`。

### 新文件

- `internal/model/usertag.go` — `UserTag`, `ArticleUserTag`, `TagSuggestionDismissal` 模型
- `internal/repository/user_tag.go` — tag CRUD、article-tag 绑定、AI 建议读取/dismiss
- `internal/api/user_tag.go` — HTTP handlers，挂在 `/api/tags/*` 与 `/api/articles/:id/tags`
- `internal/api/saved.go` — `GET /api/saved`

**Worker 不需要改动。** 现有 `cmd/worker/classify.go` 已经在维护 `articles.tags`。

### API 路由

URL 路径仍然用 `tags` 字眼（用户可见的 URL，更自然），表名内部用 `user_tags`。

**Tag 字典：**

| Method | Path | 用途 |
|---|---|---|
| `GET` | `/api/tags` | 列出当前用户所有手动 tag，附带每个 tag 在收藏中的文章数（用于边栏计数） |
| `POST` | `/api/tags` | `{name}` → 创建 tag（仅创建，不绑定文章） |
| `PATCH` | `/api/tags/:id` | `{name}` → 重命名（同名冲突 409） |
| `DELETE` | `/api/tags/:id` | 删除 tag（级联清理 `article_user_tags`） |

**文章 tag 绑定：**

| Method | Path | 用途 |
|---|---|---|
| `GET` | `/api/articles/:id/tags` | 返回 `{source: {feedId, title}, manual: [{id, name}], suggestions: [{name}]}` |
| `POST` | `/api/articles/:id/tags` | `{name}` → 绑定（tag 不存在则同时创建；幂等） |
| `DELETE` | `/api/articles/:id/tags/:tagId` | 解绑 |

**AI 建议（注意：建议没有自己的 ID，用 name 标识）：** 采纳一条建议直接调 `POST /api/articles/:id/tags`，不需要单独接口；只需要新增「忽略」端点。

| Method | Path | 用途 |
|---|---|---|
| `POST` | `/api/articles/:id/suggestions/dismiss` | `{name}` → 写 `tag_suggestion_dismissals` 一行（幂等） |

**收藏列表：**

| Method | Path | 用途 |
|---|---|---|
| `GET` | `/api/saved` | 收藏文章列表，参数：`tag_ids`（逗号分隔，0 个=全部）、`mode`（`and` / `or`，仅多 tag 时有效）、`untagged=true`（找无手动 tag 的收藏，与 `tag_ids` 互斥）、`source_feed_id`、`limit/offset` |

`/api/saved` 的 AND 模式 SQL 草图：

```sql
SELECT a.* FROM articles a
JOIN user_preferences p
  ON p.article_id = a.id AND p.user_id = $1 AND p.signal_type = 'save'
WHERE EXISTS (
    SELECT 1 FROM article_user_tags aut
    WHERE aut.article_id = a.id
      AND aut.user_id = $1
      AND aut.tag_id = ANY($2::int[])
    GROUP BY aut.article_id
    HAVING COUNT(DISTINCT aut.tag_id) = $3   -- 选中的 tag 数
)
ORDER BY a.published_at DESC LIMIT $4 OFFSET $5;
```

OR 模式去掉 `HAVING` 条件即可。`untagged=true` 时改为 `NOT EXISTS (SELECT 1 FROM article_user_tags WHERE ...)`.

### Worker 与回填

**新文章：** 现有 classify worker 会在文章被收藏（强信号）后排队跑 `ClassifyArticle`，写入 `articles.tags`，无需新增逻辑。

**老文章：** 既有 worker 已经在跑——任何被收藏过且 `topic IS NULL` 的老文章都会被覆盖。**不需要独立 backfill 脚本**。

如果上线后发现 worker 的 batch 速度跟不上、用户打开收藏老文章时 AI 建议为空，再单独写一次性回填脚本（不在本期范围）。

## 前端

### 文章页（`pages/ArticlePage.tsx`）

在标题/元信息下方加 **Tag Bar**：

```
[📡 阮一峰的网络日志]   [前端]✕  [周报]✕  [+ 添加]
                       ↳ 来源 tag (灰底，不可关)
                                      ↳ 手动 tag
                                              ↳ 输入框：自动补全已有 tag

AI 建议:  [⊕ LLM 推理]  [⊕ 论文笔记]  [⊕ 推理优化]   [全部忽略]
```

- 「+ 添加」点击后展开输入框；下拉显示该用户已有 tag（按使用频率排序）；新名直接创建
- AI 建议行最多 5 条；采纳/忽略后实时消失
- 该行没有手动 tag 也没有 AI 建议时，仍展示来源 tag

### 「我的收藏」页（`/saved` → `pages/SavedPage.tsx`）

布局：左侧 220px tag 边栏 + 右侧文章列表。

**左侧边栏：**

```
⭐ 我的收藏 (124)

□ 多选模式     ← checkbox；勾选后 tag 项前出现 checkbox，底部出现 [AND] [OR] 切换

— Tags —
  · 全部                      124
  · (无 tag)                   18
  · 前端                       42
  · 论文笔记                   31

— 来源 —
  · 📡 阮一峰的网络日志        24
  · 📡 Hacker News             18
```

- 单选模式（默认）：点击任意一行直接切换筛选；不显示 AND/OR
- 多选模式（开关打开）：tag 项出现 checkbox；底部出现 `[AND] [OR]` 单选切换（默认 AND）；解锁多选
- 关闭多选开关时回到单选并保留当前最后一个选中
- 「(无 tag)」是特殊筛选，找出没有任何手动 tag 的收藏（用来「补 tag」）；与「全部」互斥；与具体 tag 互斥

**右侧文章列表：** 复用 `ArticleListPage` 的 article card 组件（提取为公共组件 `components/ArticleCard.tsx`）；不再展示「未读 / feed 下拉」筛选器。每张卡显示标题、来源 tag、手动 tag。

### 入口

- `Layout.tsx` 顶部菜单在「文章」「Insights」之间插入「⭐ 收藏」链接到 `/saved`
- `ArticleListPage` 现有的「收藏」checkbox 保留，与 `/saved` 并行

### 视觉与配色

- **来源 tag**：`bg-slate-100 text-slate-600`，`📡 ` 前缀永远显示
- **手动 tag**：根据 tag 名 hash 自动分配 8 色调色板之一（`rose / amber / emerald / sky / violet / pink / lime / indigo`），`bg-{c}-100 text-{c}-700`；同名 tag 在任何位置永远是同一种颜色
- **AI 建议 chip**：`border-dashed border-slate-300 text-slate-500`，hover 变实心；带 ⊕ 前缀

## 实施 Phases

为保持 PR 可控，分 3 个 phase。每个 phase 自成可发布单元。

### Phase 1（核心 MVP）

- 迁移 `016_user_tags.sql`（创建 `user_tags`, `article_user_tags`, `tag_suggestion_dismissals`）
- 后端：tag/article_user_tags 的 CRUD 接口（不含 AI 建议字段）
  - `GET/POST/PATCH/DELETE /api/tags`
  - `GET/POST/DELETE /api/articles/:id/tags`（返回值不含 `suggestions` 字段）
- 前端：
  - `ArticlePage` Tag Bar（手动 tag + 来源 tag，无 AI 建议区域）
  - `ArticleCard` 提取 + 显示手动 tag

### Phase 2（收藏页）

- 后端 `GET /api/saved`，先支持单 tag + `untagged` + `source_feed_id`，再加 `mode=and|or` 多 tag
- 前端 `pages/SavedPage.tsx`，先单选模式上线，再加多选 + AND/OR 开关
- `Layout.tsx` 加入口

### Phase 3（AI 建议）

- `GET /api/articles/:id/tags` 返回值加 `suggestions` 字段（从 `articles.tags` 派生）
- `POST /api/articles/:id/suggestions/{accept,dismiss}` 接口
- ArticlePage 建议 chip 区域

**Worker 与 backfill 在本期不需要改动。** 上线后若发现老收藏 AI 建议覆盖不全（取决于 classify worker 速度），再单独评估是否要写一次性回填脚本。

## 边界情况

1. **Feed 改名**：来源 tag 自动跟随（派生）
2. **Feed 删除（unsubscribe）**：文章随 `ON DELETE CASCADE` 删除，`article_user_tags` 级联删
3. **手动 tag 与 AI 建议同名**：建议查询通过 `NOT EXISTS user_tags + article_user_tags` 过滤，已采纳的不再展示
4. **采纳建议时 tag 已存在但未绑定文章**：upsert tag，`INSERT ... ON CONFLICT DO NOTHING` 写 `article_user_tags`，幂等
5. **删除手动 tag 后又创建同名**：是新的 `user_tags.id`；旧 `tag_suggestion_dismissals` 行按 `name` 索引仍然生效（用户曾经忽略过这条建议，意图依然存在）
6. **`articles.tags` 还没被 classify worker 写入**：建议查询返回空数组；前端不渲染 AI 建议区域；用户可正常打手动 tag
7. **未登录用户访问 `/saved`**：路由前置 auth 检查，未登录跳 `/login`

## 测试

- **后端单测**：
  - `user_tag_repository_test.go` — CRUD + 边界（同名冲突、删除级联、untagged 查询）
  - `saved_handler_test.go` — AND / OR / untagged / source_feed_id 四种模式
  - `suggestion_filter_test.go` — `articles.tags` 减去 `user_tags` 与 `tag_suggestion_dismissals` 的查询正确性
- **集成测试**：`/api/saved` 走真实 DB，沿用现有 integration 模式
- **前端**：现项目无前端测试基建，跟随既有约定不引入

## 不在本期范围

- tag 改色、tag 改名后的批量同步（前者由 hash 配色解决；后者天然 follow，因为查询是按 `tag_id` 而非 `name`）
- 跨用户共享 tag、社区 tag
- 同义词归并（「LLM」/「大模型」/「大语言模型」）—— 留待观察实际使用后决定
- 「为这篇文章按需重新生成 AI 建议」按钮
- 一次性 AI 建议回填脚本（取决于上线后 worker 覆盖率，必要时再做）
