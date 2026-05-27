# link_set 内联标记重设计

**Date:** 2026-05-27
**Status:** Design — pending implementation

## 背景

当前 link_set 功能（见 `2026-05-12-link-set-design.md`）的交互流程：

1. Worker 后台对 RSS 文章做启发式扫描，符合"一行一链接 ≥11 条"的设 `link_set_suggested=true`，候选缓存到 `link_set_candidates` 表。
2. 文章页若 `link_set_suggested=true` 且 `links_extendable=false`，显示「💡 转为 link_set」按钮 → 翻转 `links_extendable=true` → 立刻打开 `BatchFetchModal`（全量候选 + 复选框）。
3. 文章页若 `links_extendable=true`，常驻悬浮按钮「📥 批量抓取」→ 也打开同一个 Modal。
4. 用户在 Modal 内全选 / 反选 / 勾选若干 → 「开始抓取」→ 后端排队 → worker 抓取 → 子文章入库。

体验痛点：Modal 把"选哪些"和"确认抓取"两个心智阶段合在一起，全屏遮罩 + 长列表 + 多种选择手势（点击、shift 多选、工具栏批量），用户从文章正文上下文中被强行抽离。重新打开时也无法回到原本看链接的位置。

## 目标

把"选哪些"放回正文：用户在文章正文里直接给候选链接打小图标标记，「批量抓取」按钮只承担最后的"确认 + 开始"。

## 设计原则

- **不改后端、不改数据库**：所有变化集中在前端
- **沿用 `link_set_candidates` 候选库**：worker 的检测逻辑、候选缓存、batch_fetch / confirm_link_set API 全保留
- **状态门不变**：仅 `links_extendable=true` 的文章才显示标记图标和「📥 批量抓取」按钮；`link_set_suggested=true` 但未启用的文章保留小按钮入口
- **持久化沿用**：选中集合继续走 `localStorage[rsspal_batch_sel_<articleId>]`，TTL 1 天

## 用户流程

### 流程 A — Suggested 状态文章首次启用

1. 用户打开 RSS 列表型文章，文章页顶部出现「💡 转为 link_set」按钮（同当前 UI）
2. 点击按钮 → 调用 `POST /articles/:id/confirm_link_set`（已存在）→ 后端置 `links_extendable=true`
3. **不再开 Modal**。前端拉取候选列表后立即在正文中给候选链接加上小图标 ⬇
4. 「💡」按钮消失，「📥 批量抓取」悬浮按钮出现

### 流程 B — 已启用文章的标记 + 抓取

1. 用户阅读正文，看到感兴趣的候选链接，点其尾部的 ⬇ 图标 → 图标变实心蓝，URL 写入 `markedURLs`，同步到 localStorage
2. 用户继续滚动 / 切换文章 → 标记保留
3. 用户点击悬浮的「📥 批量抓取」 → 打开 `BatchFetchConfirmDialog`，**只列出已标记的链接**（标题 + 域名 + editor_note）
4. 对话框内可：
   - 工具栏 全选 / 反选 / 取消全选：操作显示行的勾选状态
   - 每行 ✕：从 `markedURLs` 彻底移除该条（关闭对话框后正文图标也会取消）
   - 每行复选框：本次是否提交抓取
5. 点击「开始抓取（N）」 → 调用现有 `POST /articles/:id/batch_fetch` → 后端入队 → worker 抓取 → 子文章入库 → 子文章卡片轮询显示
6. 成功后 `markedURLs` 清空（沿用当前清 localStorage 的语义）

### 流程 C — 已抓取过的候选

- 候选 API 已返回 `already_fetched: true`
- 正文中这些链接的图标永远显示为 ✅ 灰色，不可点击切换
- 对话框中如果被显示（因为之前曾标记过），也是灰色禁用 + "已抓取"徽章，不计入开始抓取数量

## 组件设计

### 状态拥有者：`ArticlePage.tsx`

```ts
const [candidates, setCandidates] = useState<CandidateView[] | null>(null)
const [markedURLs, setMarkedURLs] = useState<Set<string>>(new Set())
const [confirmOpen, setConfirmOpen] = useState(false)

// 在 article.links_extendable 由 false→true 时 / 文章 ID 变化时拉候选
useEffect(() => {
  if (!article?.links_extendable) { setCandidates(null); setMarkedURLs(new Set()); return }
  getArticleCandidates(article.id).then(setCandidates)
  setMarkedURLs(new Set(loadSavedURLs(article.id)))  // 沿用现有 helper
}, [article?.id, article?.links_extendable])

// markedURLs 写入 localStorage（沿用现有 saveSelectedURLs）
useEffect(() => {
  if (!article) return
  saveSelectedURLs(article.id, Array.from(markedURLs))
}, [markedURLs, article?.id])

const candidateURLSet = useMemo(() => new Set((candidates ?? []).map(c => normalizeURL(c.url, articleBaseURL))), [candidates, articleBaseURL])

const toggleMark = useCallback((url: string) => {
  setMarkedURLs(prev => {
    const next = new Set(prev)
    next.has(url) ? next.delete(url) : next.add(url)
    return next
  })
}, [])
```

向下传：
- `<MarkdownArticle candidateURLs={candidateURLSet} markedURLs={markedURLs} alreadyFetchedURLs={alreadyFetchedSet} onToggleMark={toggleMark} ... />`
- `<BatchFetchConfirmDialog open={confirmOpen} candidates={candidates} markedURLs={markedURLs} onToggleMark={toggleMark} onClose={...} onFetched={...} />`

### `MarkdownArticle.tsx` 改动

仅在 `a` 组件 override 里增加：

```tsx
a: ({ href, children, ...rest }) => {
  const normalized = href ? normalizeURL(href, props.articleBaseURL) : null
  const isCandidate = normalized && props.candidateURLs?.has(normalized)
  return (
    <span style={{ display: 'inline' }}>
      <a {...rest} href={href} target="_blank" rel="noopener noreferrer">{children}</a>
      {isCandidate && (
        <LinkSetMarkIcon
          marked={props.markedURLs?.has(normalized)}
          alreadyFetched={props.alreadyFetchedURLs?.has(normalized)}
          onToggle={(e) => { e.preventDefault(); e.stopPropagation(); props.onToggleMark?.(normalized) }}
        />
      )}
    </span>
  )
}
```

`<LinkSetMarkIcon>` 是一个小 `<button>` 组件：
- 默认：14px 高、`margin-left: 4px`、`vertical-align: baseline`、`color: var(--fg-muted)`、`opacity: 0.5`、SVG 下载图标 ⬇
- hover：`opacity: 1`
- `marked=true`：`color: var(--accent)`、`opacity: 1`、实心 ⬇
- `alreadyFetched=true`：✅ icon、`color: var(--success-fg)`、`opacity: 0.5`、`cursor: not-allowed`、`onClick` no-op

### `BatchFetchConfirmDialog.tsx`（替换 `BatchFetchModal.tsx`）

Props：
```ts
{
  open: boolean
  articleId: number
  candidates: CandidateView[]
  markedURLs: Set<string>
  onToggleMark: (url: string) => void  // ✕ 调用
  onClose: () => void
  onFetched?: (count: number) => void
}
```

内部状态：
```ts
const [checkedURLs, setCheckedURLs] = useState<Set<string>>(new Set())
// open 变 true 时初始化 = markedURLs ∩ {!already_fetched}
```

显示规则：
```ts
const rows = candidates.filter(c => markedURLs.has(normalize(c.url)))
// 按 candidates 原序（= position 顺序）
```

工具栏：
- **全选**：`setCheckedURLs(new Set(rows.filter(r => !r.already_fetched).map(r => r.url)))`
- **反选**：取选中集合相对"可勾选行集合"的补集
- **取消全选**：`setCheckedURLs(new Set())`

每行：
- ✕ 按钮 → `onToggleMark(url)` → markedURLs 收缩 → 该行随之消失
- 复选框（`already_fetched=true` 时禁用 + 灰）→ 切换 `checkedURLs`
- 标题 + 域名 + editor_note（如果有），样式沿用当前 Modal 行布局

底部：
- 「开始抓取（{checkedURLs.size}）」按钮，size=0 时禁用 → 调用 `batchFetchCandidates(articleId, [...])` → 成功后通过 `onFetched(result.inserted)` 回调通知父组件 + `onClose()`；父组件 `ArticlePage` 的 `onFetched` 实现里 `setMarkedURLs(new Set())`（localStorage 由 useEffect 自动同步为空 → 命中 `loadSavedURLs`/`saveSelectedURLs` 中 `urls.length === 0` 即移除 key 的现有分支）

### URL 归一化

候选 API 返回的 `url` 已是绝对 URL（worker 抽取时做了归一化）。`<a href>` 在 markdown 渲染时可能是相对 URL（被 `2026-05-26` 推特修复处理过的根相对、或 markdown 自带的相对路径）。

归一化函数：
```ts
function normalizeURL(href: string, base?: string): string {
  try {
    return new URL(href, base).toString()
  } catch {
    return href  // 不合法的 href 原样返回（不会命中候选）
  }
}
```

base 取自 `article.url`。

## 兼容性 / 删除项

**前端**：
- 删除：`BatchFetchModal.tsx`（被新的 `BatchFetchConfirmDialog.tsx` 取代）
- 删除：`ArticlePage.tsx` 中「💡 转为 link_set」按钮点击后调用 `setBatchModalOpen(true)` 的代码路径
- 删除：旧 Modal 的全量候选列表、shift 多选区间、Select-all-then-fetch 心智模型

**后端 / 数据库**：
- 无改动
- API 不变：`GET /articles/:id/candidates`、`POST /articles/:id/batch_fetch`、`POST /articles/:id/confirm_link_set`、`GET /articles/:id`（含 children）

**worker**：
- 无改动

## 边界场景

| 场景 | 行为 |
|---|---|
| 候选 URL 在正文中出现多次 | 每个 `<a>` 实例独立渲染图标，但状态由同一 URL 共享 → 点任一处都改全部 |
| 候选 URL 在候选库但 markdown 渲染时被剪裁掉（不可见） | 用户无法 inline 标记，永远不会出现在确认对话框；不影响整体功能（candidates 库本来就由 worker 抓 raw HTML，可能多于 markdown 渲染结果） |
| 候选 `already_fetched=true` 的 URL 在 `markedURLs` 里残留（来自旧 localStorage） | 对话框显示但禁用；用户须主动 ✕ 才能去掉；下次开始抓取时被过滤掉 |
| 用户在对话框内 ✕ 掉所有行 | rows 变空 → 「开始抓取」置灰；用户可关闭对话框 |
| 文章 `links_extendable=true` 但候选库为空 | 「📥 批量抓取」按钮仍显示（沿用现状），点开后对话框显示"未找到可抓取的链接"提示 |
| 用户在 Suggested 状态点击「💡 转为 link_set」后候选 API 失败 | `links_extendable=true` 已生效但 candidates=null；不渲染图标，「📥 批量抓取」按钮显示空对话框 + 重试入口 |

## 测试要点

实现完成后必须验证：

1. RSS 列表型文章打开 → 「💡」可见，点击后立即出现内联图标（无 Modal 弹出）
2. 点正文内联 ⬇ 图标 → 图标实心化，再点 → 恢复淡色
3. 多个候选标记 → 「📥」打开对话框 → 仅显示已标记的，按候选顺序排列
4. 工具栏 全选 / 反选 / 取消全选 → 复选框状态正确翻转
5. ✕ 按钮 → 该行立即消失，关闭对话框后正文图标也变淡
6. 「开始抓取」→ 子文章入队 → 标记清空、对话框关闭、子文章卡片显示 "处理中"
7. 已抓取过的候选 → 正文 ✅ 灰、不可点击；对话框中显示但置灰不可勾
8. localStorage 持久化：标记后刷新页面，标记仍在；TTL 过期或开始抓取成功后清空
9. 关闭对话框（不点开始抓取）→ markedURLs 保留
10. `link_set_suggested=true` 且 `links_extendable=false` 的文章 → 不显示图标也不显示 fab

## 实现顺序提示

1. 提取 `normalizeURL` 工具到 `frontend/src/utils/url.ts`（或现有等价位置）
2. 提取 / 复用 localStorage helper（`loadSavedURLs` / `saveSelectedURLs` 当前在 BatchFetchModal 内）到 `frontend/src/utils/linkSetSelection.ts`
3. 实现 `<LinkSetMarkIcon>` 组件
4. 改造 `MarkdownArticle.tsx`，接受候选 props 并条件渲染图标
5. 在 `ArticlePage.tsx` 提升状态（candidates + markedURLs）+ 拉取候选 + 传给 MarkdownArticle
6. 新增 `BatchFetchConfirmDialog.tsx`，并把 ArticlePage 中所有引用从 BatchFetchModal 切过来
7. 调整「💡 转为 link_set」点击行为：去掉打开 Modal 的副作用
8. 删除 `BatchFetchModal.tsx`
9. 手动测试 10 项验证清单
10. 提交 + docker rebuild frontend
