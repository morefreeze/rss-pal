# /recommended 页面改进设计

**日期**: 2026-05-18
**Scope**: `/recommended` 页面的推荐逻辑兜底 + 文案与 UI 增强
**不包含**: link_set 自动 confirm(已记入 memory 作为独立待办)

## 背景与动机

`/recommended` 页面当前有两个 section:

1. **本周精选 link_set 链接** — 由 `GetLinkSetRecommendations`(`backend/internal/repository/article.go:1191`)驱动,展示来自"含链接合集"的父文章(如 Hacker Newsletter)抓取出的子文章。
2. **推荐订阅** — 由 `GET /api/recommended-feeds` 驱动,展示预置高质量 RSS 源,按分类网格呈现。

### 已发现的问题

**问题 A — Link_set section 经常为空**
推荐 query 包含两个强过滤:
- `pref_score` 来自 30 天内的 `user_preferences`,无信号的文章 score IS NULL,排序为 0 但仍可进结果集
- `COALESCE(rp.is_completed, false) = false` 直接过滤已读完的文章

当前数据库里仅有的 10 篇 ready 状态 link_set 子文章(来自 HN Newsletter #791、#793)全部被用户标为 `is_completed=true`,导致此 query 返回 0 行,section 空白。

**问题 B — Link_set section 文案缺失**
section 标题"本周精选 link_set 链接"对终端用户是黑话,没有任何说明文字解释:
- 链接来源(从哪些订阅源展开的)
- 排序依据
- 哪些情况下文章不会出现

**问题 C — 子文章卡片没有父文章入口**
用户看到"Hand Drawn QR Codes"但无从得知它来自哪一期 HN Newsletter,也无法跳回原始 newsletter。

**问题 D — Bestblogs section 文案过于片面**
"以下是从 bestblogs.dev 精选的高质量来源" — 把来源捆绑在单一外站,实际功能是"预置订阅源",更通用的表达更合适。

## 设计

### 1. 后端:推荐 query 兜底逻辑

**核心思想**:推荐分两阶段。Primary 阶段保持现有"按用户偏好排序、过滤已读"的强语义;不足时由 Fallback 阶段以"质量门槛"补足,允许已读文章出现以避免空 section。

#### 1.1 Primary query

在现有 `GetLinkSetRecommendations` 的基础上小幅修改 —— 过滤条件和排序完全保留:
- `processing_state = 'ready'`
- `parent_article_id IS NOT NULL`
- 父文章 `fetched_at` 在 N 天内(默认 7)
- feed 可见性:`f.owner_id IS NULL OR f.owner_id = $user_id`
- `COALESCE(rp.is_completed, false) = false`
- 排序:`(pref_score + prerank_score) DESC, published_at DESC NULLS LAST`

**改动点**:JOIN 父文章 `parent.id, parent.title`,在返回的 article 行里多带 `parent_title` 字段(用于前端"来自《...》"展示)。

#### 1.2 Fallback query(新增)

触发条件:Primary 返回行数 < `limit`。

```sql
SELECT a.id, ..., a.parent_article_id, parent.title AS parent_title
FROM articles a
JOIN articles parent ON a.parent_article_id = parent.id
JOIN feeds f ON a.feed_id = f.id
WHERE a.processing_state = 'ready'
  AND a.parent_article_id IS NOT NULL
  AND parent.fetched_at > NOW() - ($days || ' days')::INTERVAL
  AND (f.owner_id IS NULL OR f.owner_id = $user_id)
  AND a.word_count >= 500              -- 质量门槛
  AND a.summary_brief IS NOT NULL      -- 质量门槛(已成功生成 AI 摘要)
  AND NOT (a.id = ANY($exclude_ids))   -- 排除 primary 已返回的(数组参数,空数组安全)
ORDER BY a.prerank_score DESC NULLS LAST,
         a.published_at DESC NULLS LAST
LIMIT $remaining
```

**关键差异 vs Primary**:
- 去掉 `pref_score` 计算(无 user_preferences LEFT JOIN)
- 去掉 `is_completed=false` 过滤(已读文章可以进 fallback)
- 新增 `word_count >= 500` 和 `summary_brief IS NOT NULL` 两个质量门槛
- 排除 primary 已返回的 id

**为什么质量门槛选这两个字段**:
- 排查实际数据发现 `prerank_score` 仅在 link_set children 上被 worker 默认填为 `0`(402 篇文章里只有 10 篇非 NULL,且全为 0),作为筛选条件没有任何区分度
- `word_count >= 500` 排除明显抓取失败或太短的占位文章
- `summary_brief IS NOT NULL` 排除尚未走完 AI 摘要流水线的文章(确保用户看到的是已完整处理过的内容)

#### 1.3 Go 层组装

```go
func (r *ArticleRepository) GetLinkSetRecommendations(userID, days, limit int) ([]model.Article, error) {
    primary, err := r.queryLinkSetPrimary(userID, days, limit)
    if err != nil { return nil, err }

    // 标记 primary 行不是 fallback
    for i := range primary {
        primary[i].IsFallback = false
    }

    if len(primary) >= limit {
        return primary, nil
    }

    excludeIDs := make([]int, len(primary))
    for i, a := range primary { excludeIDs[i] = a.ID }

    fallback, err := r.queryLinkSetFallback(userID, days, limit-len(primary), excludeIDs)
    if err != nil {
        // 不让 fallback 失败拖垮 primary,记录并返回 primary
        log.Printf("link_set fallback query failed: %v", err)
        return primary, nil
    }
    for i := range fallback {
        fallback[i].IsFallback = true
    }
    return append(primary, fallback...), nil
}
```

**注意**:fallback query 出错不返回 error,只返回 primary 部分。理由:fallback 是"锦上添花",不应阻塞 primary 的成功结果。

**`$exclude_ids` 用 `pq.Array([]int{...})` 传 Postgres int[] 数组**,即使为空数组,`a.id = ANY(ARRAY[]::int[])` 永远返回 false,`NOT` 之后永远 true,不会出现 `NOT IN ()` 语法错误。

### 2. 数据模型

`model.Article` 新增两个字段:

```go
type Article struct {
    // ... 现有字段
    ParentTitle string `json:"parent_title,omitempty" db:"parent_title"`
    IsFallback  bool   `json:"is_fallback,omitempty"`   // 瞬态字段,不入库
}
```

`scanArticleNoFeedTitle` 同步多读一列 `parent_title`(其它调用此 scanner 的 query 也需要 JOIN parent;但 link_set 子文章以外的 article 没有 parent,JOIN 为 LEFT JOIN,parent_title 自然为 NULL 字符串)。

**实现细节**:link_set 之外的 scanner 调用方较多,为避免大面积改动,引入**新的 scanner 函数** `scanArticleWithParentTitle`,只在 `queryLinkSetPrimary` 和 `queryLinkSetFallback` 内使用。现有 `scanArticleNoFeedTitle` 不动。

**无新 migration**:`parent_title` 来自 JOIN,`is_fallback` 是 Go 层瞬态字段,数据库 schema 完全不变。

### 3. 前端:`RecommendedPage.tsx`

#### 3.1 Link_set section 改造

**标题区域**:
```tsx
<div className="flex items-center gap-2 mb-3">
  <h2 className="text-lg font-semibold">本周精选 link_set 链接</h2>
  <button
    onClick={() => setShowHelp(!showHelp)}
    aria-label="说明"
    style={{ background: 'none', border: 'none', cursor: 'pointer', fontSize: 16 }}
  >ℹ️</button>
</div>
{showHelp && (
  <div className="card text-sm" style={{ background: 'var(--surface-hover)', marginBottom: 12 }}>
    <p>这里的文章来自你订阅源里"内含链接合集"的文章(如 Hacker Newsletter)。系统会自动展开链接、抓取正文,按以下规则推荐:</p>
    <ol>
      <li>优先按你的偏好(过去 30 天 like / save / 收听时长加权)排序</li>
      <li>没有偏好数据时按编辑加权 + 发布时间排序,保证质量</li>
      <li>已读完的文章默认不出现,但当合格文章不足时会作为兜底补齐(会标注)</li>
    </ol>
    <p className="text-muted">如果某期 newsletter 没出现,可能是该订阅源还未被系统识别为"含链接合集",或该期所有文章都已读完。</p>
  </div>
)}
```

**卡片改造**(每张子文章卡片底部新增父文章入口 + fallback 提示):
```tsx
{linkSetRecs.map((a) => (
  <div key={a.id} className="card" style={{ cursor: 'pointer' }} onClick={() => navigate(`/articles/${a.id}`)}>
    <div className="text-bold">{a.title}</div>
    {a.summary_brief && (
      <div className="text-muted text-sm mt-1">{a.summary_brief.slice(0, 120)}…</div>
    )}
    {a.feed_title && (
      <div className="text-muted text-sm mt-1" style={{ color: 'var(--accent)' }}>{a.feed_title}</div>
    )}
    {a.parent_title && a.parent_article_id && (
      <div className="text-muted text-sm mt-1">
        来自《
        <span
          onClick={(e) => { e.stopPropagation(); navigate(`/articles/${a.parent_article_id}`) }}
          style={{ color: 'var(--accent)', cursor: 'pointer' }}
        >{a.parent_title}</span>
        》
        {a.is_fallback && (
          <span className="text-muted" style={{ marginLeft: 8, fontSize: 11 }}>
            · 兜底推荐(可能已读过)
          </span>
        )}
      </div>
    )}
  </div>
))}
```

#### 3.2 Bestblogs section 文案

```diff
- 以下是从 bestblogs.dev 精选的高质量来源。点击「订阅」即可添加到你的订阅列表。
+ 以下是预置的优质订阅源,按内容方向分类。点击「订阅」加入你的订阅列表。
```

### 4. API contract

`GET /api/articles/recommended/link_set?days=7&limit=20` 响应保持单数组形状,每个 article 对象**新增**:

- `parent_title?: string` — 父文章标题(omitempty)
- `parent_article_id?: number` — 已有字段,前端需要用
- `is_fallback?: boolean` — 是否来自 fallback(omitempty;不传等同于 false)

旧版本前端忽略这三个字段不会崩。

## 测试

### 后端单元测试

新增 / 扩展 `backend/internal/repository/article_test.go` 中针对 `GetLinkSetRecommendations` 的覆盖:

| 场景 | 设置 | 期望 |
|---|---|---|
| Primary 足够 | 5 篇 ready 子文章 + 用户有 5 个 like 信号,limit=3 | 返回 3 篇,全部 `is_fallback=false`,按 pref_score 排序 |
| Primary 空,fallback 全填 | 5 篇 ready 子文章 + 全部 `is_completed=true` + 无 user_preferences,均满足质量门槛,limit=3 | 返回 3 篇,全部 `is_fallback=true`,按 prerank_score + published_at 排序 |
| Primary 部分 + Fallback 部分 | 2 篇未读 + 3 篇已读(全合格),limit=5 | 返回 5 篇:前 2 篇 `is_fallback=false`,后 3 篇 `is_fallback=true`,无重复 id |
| Fallback 质量门槛 | 3 篇已读但 word_count<500 | fallback 不返回它们 |
| Fallback 排除 primary | 同一篇文章既能进 primary(有 like 信号)又满足 fallback 质量门槛,limit > 1 | 该文章只在 primary 出现一次,不重复 |

> Fallback 内部 SQL 错误的容错路径(`log.Printf` 后返回 primary)由代码审查覆盖,不写专门测试 —— 在不引入 mock 层的情况下很难稳定模拟 `db.Query` 失败。

### 手工验证

- `docker-compose up -d --build frontend && docker-compose restart api`
- 浏览器访问 `/recommended`
- 验证:
  - 标题旁出现 ℹ️,点击切换帮助面板
  - HN 的 10 篇 children 全部出现(标"兜底推荐"),每篇底部有"来自《Hacker Newsletter #793》"链接
  - 点击父文章链接跳转正常,不触发子文章点击事件
  - bestblogs section 文案更新

## 实现影响清单

| 文件 | 改动 |
|---|---|
| `backend/internal/model/model.go`(article struct) | 加 `ParentTitle`、`IsFallback` 字段 |
| `backend/internal/repository/article.go` | 拆 `GetLinkSetRecommendations` → primary + fallback 两阶段;新增 `scanArticleWithParentTitle` |
| `backend/internal/repository/article_test.go` | 5 个新测试场景 |
| `frontend/src/api/client.ts` | `Article` interface 加 `parent_title?`, `is_fallback?` |
| `frontend/src/pages/RecommendedPage.tsx` | help icon + 折叠面板 + 父文章入口 + fallback 提示行 + bestblogs 文案 |

## 风险

- **DB 安全**:无新 migration,无 DML(只读 SELECT),DB 风险 = 0。
- **Frontend Docker 重建**:实现完跑 `docker-compose up -d --build frontend` 验证。
- **API 兼容**:加字段不删字段,客户端 / 扩展不受影响。
- **Fallback 体验**:已读文章混入推荐位是产品取舍,需在 UI 文案("兜底推荐")明确告知用户,避免"为什么推荐我已读的"的困惑。

## 不在 scope

- **link_set 自动 confirm**(`link_set_suggested=true` 的 RSS 文章不自动 promote 为 link_set feed)。这是独立产品决策,见 memory `project_link_set_confirm_ux.md`。
- **`prerank_score` 的回填逻辑**(目前只有 worker 在 link_set children 上写入,且全为 0)。如未来希望它真起到"编辑加权"作用,需要单独设计写入策略。
- **fallback 时间窗扩大**(可以考虑 fallback 用 14 天 / 30 天窗口而非 7 天,以扩大候选池)。当前不做,保持简单。

## 开放问题

无。
