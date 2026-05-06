# 书签文章重抓 + 重复检测 UX

**Date:** 2026-05-06
**Status:** Approved
**Owner:** morefreeze

## 1. 问题陈述

两个相关的小问题：

1. **书签文章被"重新抓取"按钮破坏内容。** 文章页的"重新抓取"按钮调用后端 HTTP GET 文章 URL 重抓——对登录墙 / JS 渲染 / 反爬站，得到的内容比原来书签捕获的差得多（实测 BestBlogs 文章被替换成了登录墙文案）。
2. **书签捕获重复 URL 时，今天的"长内容覆盖短内容"是隐式行为。** 用户希望在新内容不是明显改进时显式选择：覆盖还是保留。

## 2. 目标

- 书签文章上的"重新抓取"按钮变成有用的入口（打开原网页 + 提示用户点书签），而不是维持破坏性行为或简单藏掉。
- 书签捕获 URL 重复时，在内容不是明显改进的情况下交给用户决定，避免静默错误覆盖。

非目标：
- 不为同 URL 保留多版本快照（"另存为"被排除——schema 不变）
- 不影响 RSS 自动同步的去重逻辑（无人在场，prompt 无意义）
- 不试图自动应用书签 JS 到外部站（浏览器跨源限制，做不到）

## 3. 关键决策

| # | 决策 | 选定 | 备注 |
|---|------|------|------|
| 1 | 书签文章的"重新抓取"按钮 | **保留按钮，改文字 + 改行为：打开原网页提示用户点书签** | 比"藏按钮"更有用 |
| 2 | 书签来源识别 | **靠 `feeds.feed_type='saved'` 推断** | 不加 schema 列 |
| 3 | 重复时的选项 | **覆盖 / 保留旧内容**（不要"另存"） | 保留 `UNIQUE(feed_id, url)` |
| 4 | 弹窗触发条件 | **新内容 < 1.5 × 旧内容** | ≥ 1.5× 静默覆盖 |
| 5 | force overwrite 机制 | **请求 body `force: true`** | 比 query string 干净 |

## 4. 整体架构

### 场景 1 · 文章页"重新抓取"

```
GET /api/articles/:id
  → 服务端 join feeds.feed_type
  → 响应增加 derived 字段 from_bookmarklet: bool

ArticlePage.tsx 读 article.from_bookmarklet:
  ├─ false → 沿用原"重新抓取"按钮（POST /api/articles/:id/content）
  └─ true  → 渲染"🔁 通过书签重新抓取"按钮
            → 点击 confirm() → window.open(article.url, '_blank') + toast 提示
            → 用户在新 tab 点 RSS Pal 书签 → 走标准书签流（含场景 2）
```

### 场景 2 · 书签捕获遇到同 URL

```
POST /api/bookmarklet/capture {url, title, html, force?:bool}
                    ↓
  服务端 FindByOwnerAndURL:
    ├─ 不存在 → Create + status:"created"
    └─ 存在
        ├─ force=true                              → Update + status:"updated"
        ├─ newLen >= 1.5 × oldLen                  → Update + status:"updated"
        └─ 其他（newLen < 1.5×oldLen, force=false) → 不写库, status:"duplicate"
                                                     + {existing_length, new_length, article_id}
                    ↓
  bookmarklet-receiver.html:
    duplicate 状态 → 渲染 "现有 N 字 / 新内容 M 字 [覆盖] [保留旧内容]"
      [覆盖]    → 重 POST 带 force:true → status:"updated"
      [保留]    → 关闭页面，DB 无改动
```

### 边界与不变量

- `articles` schema 不变；`UNIQUE(feed_id, url)` 保留
- `from_bookmarklet` 仅在 GET 响应中存在，不进 model.Article 持久化字段
- duplicate 返回 `200 OK`（业务态，非 HTTP 错误）
- "force" 字段只对书签 capture 生效；其他端点忽略
- 旧的"`newLen <= oldLen` → unchanged"分支被新逻辑取代（unchanged 状态不再出现）

## 5. 后端改动

### 5.1 `internal/api/article.go` —— GetByID 响应加 `from_bookmarklet`

在 article handler 的 `GetByID`（或 service 层 `GetByID`）里 join feeds 拿 `feed_type`。组装响应时追加 `from_bookmarklet: feed.FeedType == "saved"`。

不修改 `model.Article` 结构。响应组装层（gin.H 或专门的 response struct）多写一个字段即可。

### 5.2 `internal/api/bookmarklet.go` —— 重写 duplicate 分支

请求 body decode 增加：
```go
var req struct {
    URL   string `json:"url"`
    Title string `json:"title"`
    HTML  string `json:"html"`
    Force bool   `json:"force"`  // 新增
}
```

替换现有 `if existing != nil { if len(content) <= len(existing.Content) {...} ... update }` 块为：

```go
if existing != nil {
    newLen, oldLen := len(content), len(existing.Content)
    if !req.Force && oldLen > 0 && float64(newLen) < 1.5*float64(oldLen) {
        c.JSON(http.StatusOK, gin.H{
            "status":          "duplicate",
            "article_id":      existing.ID,
            "existing_length": oldLen,
            "new_length":      newLen,
            "message":         fmt.Sprintf("已有内容 %d 字 / 新内容 %d 字", oldLen, newLen),
        })
        return
    }
    // force=true 或 newLen >= 1.5*oldLen 走原 update 路径
    wc, rm := rss.ComputeMetrics(content)
    if err := h.articleRepo.UpdateContent(existing.ID, content, wc, rm); err != nil { ... }
    if err := h.articleRepo.UpdateSummary(existing.ID, "", ""); err != nil { ... }
    c.JSON(http.StatusOK, gin.H{
        "status":     "updated",
        "article_id": existing.ID,
        "message":    "已更新文章: " + existing.Title,
    })
    return
}
```

`oldLen > 0` 守卫：旧内容是空字符串（极端 edge case）的话视作无内容，永远静默覆盖。

### 5.3 测试 `internal/api/bookmarklet_test.go`

三个核心 case：

- `TestCapture_Duplicate_TriggersPrompt`：existing.Content="A"*1000；POST 带 html 抽出来约 1200 chars → 期望 status:"duplicate"，DB 未更新
- `TestCapture_Duplicate_AutoUpdateOnLargeIncrease`：existing 1000 chars，new 2000 chars → 期望 status:"updated"，DB 已更新
- `TestCapture_Duplicate_ForceOverwrites`：existing 1000，new 500，body 带 `"force": true` → 期望 status:"updated"，DB 已更新到新 500-char 内容

为这些 test 注入一个 stub user/feed/article（用 in-memory sqlite 或 testcontainers postgres，按现有 test 套路）。如现有 bookmarklet 没 test 套路，先用 httptest + 模拟 repo（依赖注入或 interface mock）。

## 6. 前端改动

### 6.1 `src/api/client.ts` —— Article 类型加 `from_bookmarklet?`

```ts
export type Article = {
  id: number
  // ...existing fields
  from_bookmarklet?: boolean
}
```

可选字段（向后兼容旧响应）。

### 6.2 `src/pages/ArticlePage.tsx`

定位现有"重新抓取"按钮（"原文内容"卡片右上角）。改为按 `article.from_bookmarklet` 分支：

```tsx
{article.from_bookmarklet ? (
  <button onClick={handleRescrapeViaBookmarklet}>🔁 通过书签重新抓取</button>
) : (
  <button onClick={handleFetchContent} disabled={fetchingContent}>
    {fetchingContent ? '获取中...' : '重新抓取'}
  </button>
)}
```

新增 handler：

```tsx
const handleRescrapeViaBookmarklet = () => {
  if (!article) return
  const ok = window.confirm(
    `重新抓取需要在原网页运行书签。\n` +
    `会打开 ${article.url}，请到新标签后点你 bookmark bar 上的 RSS Pal 书签来抓取最新内容。\n\n` +
    `继续？`
  )
  if (!ok) return
  window.open(article.url, '_blank', 'noopener,noreferrer')
  toast.info('已打开原网页 — 在新标签里点你的 RSS Pal 书签')
}
```

用 `window.confirm`（无新 modal 组件依赖）。toast 走现有 `'../utils/toast'`。

### 6.3 `frontend/public/bookmarklet-receiver.html` —— 加 duplicate 分支

把现有的 fetch 调用提取成局部函数 `postCapture(force)`：

```js
function postCapture(force) {
  return fetch('/api/bookmarklet/capture', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer ' + d.token,
    },
    body: JSON.stringify({
      url: d.url,
      title: d.title || '',
      html: d.html,
      force: !!force,
    }),
  }).then(function (r) {
    return r.json().then(function (j) { return { ok: r.ok, j: j, status: r.status }; });
  });
}
```

在 `.then(function (x) { ... })` 里，`if (x.ok)` 分支增加 `phase === 'duplicate'` 处理：

```js
if (phase === 'duplicate') {
  box.innerHTML =
    '<h1 class="info">⚠️ 已有同 URL 文章</h1>'
    + '<p>现有内容 ' + x.j.existing_length + ' 字 / 新内容 ' + x.j.new_length + ' 字</p>'
    + '<p>新内容看起来不是明显改进，怎么处理？</p>'
    + '<div style="margin-top:14px;display:flex;gap:8px;justify-content:center;">'
    +   '<button id="ow" style="padding:8px 18px;background:#2563eb;color:#fff;border:none;border-radius:6px;cursor:pointer;">覆盖</button>'
    +   '<button id="kp" style="padding:8px 18px;background:#f3f4f6;border:1px solid #ddd;border-radius:6px;cursor:pointer;">保留旧内容</button>'
    + '</div>'
  document.getElementById('ow').onclick = function () {
    box.innerHTML = '<h1 class="info"><span class="spinner"></span>正在覆盖…</h1>'
    postCapture(true).then(handleResponse).catch(function (err) { fail('网络错误: ' + err.message); })
  }
  document.getElementById('kp').onclick = function () {
    render('info', 'ℹ️ 已保留旧内容', '可关闭此页面', false)
  }
  return
}
```

把现有的 `then(function (x) { if (x.ok) { ...switch phase... } else { fail(...) } })` 也抽到 `handleResponse(x)` 函数，被两次调用复用。

最终结构：

```js
function handleResponse(x) {
  if (!x.ok) { fail(...); return }
  var phase = x.j.status
  if (phase === 'duplicate') { /* 渲染两按钮，绑定回调 */ return }
  // existing: created / updated / unchanged 渲染
}

postCapture(false).then(handleResponse).catch(...)
```

## 7. 部署 / 回滚

部署顺序：
1. 后端先发：duplicate 状态 + force 字段（旧前端遇到 duplicate 状态会落到 default switch 分支显示 "✅ 抓取成功"——不准确但不破坏 UX；可接受过渡）
2. 前端跟进：ArticlePage 按钮分流 + receiver.html 的 duplicate 分支 + Article 类型字段

回滚：
- 后端单独回滚：前端遇到不再返回 duplicate 的服务端 → 退回 created/updated/unchanged，无回归
- 前端单独回滚：服务端仍可返回 duplicate，旧 receiver.html 走 default 分支显示 ✅，体验略差但可读

## 8. 已知风险

- **1.5× 阈值**：拍的，可能误拦"原文小幅增补"或漏放"原文小幅缩水"。后续如果发现误判太多可调 / 加配置。
- **`window.confirm`**：浏览器原生确认框，样式不可控，UX 一般。如果以后有了通用 Modal 组件可以替换，但此功能用不上专门的视觉投入。
- **新 tab toast**：toast 在 RSS Pal 这个 tab 提示，用户已经切到外部 tab 可能看不到。提示文字也写在 confirm 框里作为 fallback。
- **跨源限制**：明确无法自动应用书签 JS——这是设计前提，不是 bug。

## 9. 文件清单

修改：
- `backend/internal/api/article.go`（GetByID 响应加 from_bookmarklet）
- `backend/internal/api/bookmarklet.go`（force 字段 + duplicate 分支）
- `frontend/src/pages/ArticlePage.tsx`（按钮分流 + handler）
- `frontend/src/api/client.ts`（Article 类型加可选字段）
- `frontend/public/bookmarklet-receiver.html`（duplicate 分支 + postCapture 函数化）

新增：
- `backend/internal/api/bookmarklet_test.go`（如不存在）+ 三个 case
