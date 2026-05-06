# 抓取器与文章展示重构 + 阅读模式

**Date:** 2026-05-06
**Status:** Approved
**Owner:** morefreeze

## 1. 问题陈述

当前抓取器只抽取纯文本，丢弃了图片、代码块、表格、列表等结构。前端把存好的纯文本按 `\n{2,}` 切段并以 `<p>` 渲染，导致：

- 技术文章里的代码块变成无格式的散段
- 图片完全缺失
- 标题层级、列表、表格都丢掉了
- 阅读体验和原文相比缺失明显

同时阅读体验固定（白底、字号 15、行高 1.8），无法根据时段/光线调整。

## 2. 目标

- 抓取阶段保留富文本结构（标题、列表、代码块、表格、图片）
- 文章页正确渲染上述结构
- 加入「阅读模式」：纯净布局 + 可调字号、字体族、背景色
- 解决中文站（微信/知乎等）图片 hotlink 防护问题

非目标：
- 跨设备同步阅读偏好
- 文章本地永久存档（图片下载到自有存储）
- 完整 HTML 保真（接受 markdown 表达力为上限）

## 3. 关键决策

| # | 决策 | 选定 | 理由要点 |
|---|------|------|----------|
| 1 | 内容存储格式 | **Markdown** | 与已有 Jina Reader 路径天然对齐；和 AI 摘要/导出共享渲染管道 |
| 2 | 现存数据迁移 | **一次性回填脚本** | 一次性把库里旧文章重新抓取覆写为新格式 |
| 3 | 图片处理 | **后端代理 + 白名单** | 解决微信等站 hotlink；不持久化，依赖浏览器缓存 |
| 4 | 富语法支持 | **`remark-gfm` + `rehype-highlight`** | 表格 + 代码高亮，约 35KB gzip |
| 5 | 阅读模式形态 | **「进入阅读模式」全屏切换** | 显式模式，沉浸感最强 |
| 6 | 阅读模式内容 | **正文 + 折叠 AI 摘要** | 摘要默认折叠不打扰；操作/导航/分享全部隐藏 |
| 7 | 背景色预设 | **白 / 米黄 / 护眼绿 / 浅灰 / 暗色** 五个 | 覆盖白天/午后/夜间场景；不含纯黑（OLED 用户少） |
| 8 | 字号 / 字体 | **A−/A+ 步进 12–24px，默认 16px；Sans / Serif 二选一** | 步进可精细，衬线可选适合长文 |
| 9 | 持久化 | **localStorage；记住上次模式** | 单用户场景，无需服务端同步；上次进出状态作为下次默认 |

## 4. 整体架构

```
[抓取链路]
  RSS Feed → fetcher.go (元数据解析, 不变)
                 │
                 ▼
        content_fetcher.go (重构)
            ├─ 直接路径:  goquery 选区 → html-to-markdown 转换器
            └─ Jina 兜底: r.jina.ai 已经返回 markdown
                 │
                 ▼
        articles.content TEXT (现在装 markdown)

[阅读链路]
  GET /api/articles/:id → JSON (content 是 markdown)
                 │
                 ▼
        ArticlePage.tsx
            ├─ ReaderModeShell (壳, normal vs reading 切换)
            ├─ MarkdownArticle (ReactMarkdown + remark-gfm + rehype-highlight)
            │     └─ <img> 自定义 renderer → /api/proxy/image?url=…
            └─ ReaderSettingsPanel (Aa 浮动按钮; localStorage 持久)

[图片代理]
  GET /api/proxy/image?url=X
      → SSRF 校验 (scheme 白名单 + DNS 解析后 IP 黑名单)
      → 拉源站 (注入合理 Referer 绕过 hotlink)
      → 透传 image/* + 加 Cache-Control → 流式返回

[回填]
  cmd/backfill_content (一次性 CLI)
      速率受控扫库 → ContentFetcher.FetchContent → 覆写 content
```

边界与不变量：
- 抓取/渲染统一以 markdown 为真相源；前端不再做 `\n{2,}` 切段
- 阅读模式是纯前端 UI 状态——不动 schema、不存数据库
- 图片代理无 auth（公共内容）；强 SSRF 防护是必备前提

## 5. 后端改动

### 5.1 依赖

- `github.com/JohannesKaufmann/html-to-markdown/v2`（MIT）

### 5.2 `internal/rss/content.go`

- 替换 `extractText(selection)` 为 `extractMarkdown(selection)`：把 goquery 选区交给 html-to-markdown converter，启用 GFM plugins（表格 / strikethrough / task list）
- 保留主选择器循环 + 200 字符长度阈值：先在 `article` / `main` / `[role=main]` 等选区中找主内容区，再交给转换器，避免把页脚/导航也转出来
- `cleanContent()` 仍跑——清理多余空行、过滤 newsletter / 社交分享类垃圾行
- 50,000 字符上限保留（markdown 也是纯文本）
- `FetchContent()` 总返回 markdown；Jina 兜底路径已经是 markdown，无变化
- `FetchContentFromReader()` 同步改造（用于测试和复用）

### 5.3 `internal/api/proxy.go`（新）

接口：`GET /api/proxy/image?url=<encoded URL>`

校验链：
1. URL 必须是 `http://` 或 `https://`
2. 解析 host → DNS 拿到 IP；若属于 `127.0.0.0/8` / `10.0.0.0/8` / `172.16.0.0/12` / `192.168.0.0/16` / `169.254.0.0/16` / `::1` / `fc00::/7` / `fe80::/10` / metadata IP `169.254.169.254`，拒绝 (403)
3. 设置 `http.Client` timeout 30s

请求头：
- `Referer`: 用 url 的 origin（`https://host/`）——绕过常见 hotlink 防护，对微信公众号尤为关键
- `User-Agent`: 复用 `content_fetcher` 的常规浏览器 UA

响应：
- 拒绝非 `image/*` Content-Type
- 限制大小 10MB（`io.LimitedReader`）
- 透传 `Content-Type` / `ETag`；若源站没给 `Cache-Control`，自加 `Cache-Control: public, max-age=86400, immutable`
- 流式 `io.Copy` 到 `ResponseWriter`，不缓存到内存/磁盘

路由：注册到现有 router，**不挂 auth 中间件**——`<img>` 标签若需 cookie 会引入复杂度且无收益。

### 5.4 `cmd/backfill_content/main.go`（新二进制）

CLI 参数：
- `--batch-size 50`（每批落 DB 提交数）
- `--qps 1`（默认非常温和，避免触发反爬）
- `--feed-id 0`（0 = 全部 feed；指定后只回填该 feed 的文章）
- `--dry-run`（只打日志，不写库）

流程：
1. 选 `articles WHERE content IS NOT NULL AND content != ''`
2. 对每篇调 `ContentFetcher.FetchContent(ctx, article.url)` → 写回 `content`
3. 失败：log 后跳过，不阻塞循环；可重跑（幂等覆写）
4. 进度：每 10 篇打一行 `[123/4567] ✓` 或 `[124/4567] ✗ <url> <err>`

不需要 schema 变更（content TEXT 容量足够）。

### 5.5 测试

- `content_test.go`：HTML fixture 含 `<img>` / `<pre><code>` / `<table>` / `<ul>` / `<h2>` 各一个 → 期望对应 markdown 输出（用 `FetchContentFromReader` 入口）
- `proxy_test.go`：
  - 拒绝 `file://` / `ftp://` scheme
  - 拒绝解析为 RFC1918 / 127/8 / 169.254/16 的 host
  - 验证向源站发出请求时带了 `Referer`
  - 验证非 `image/*` 响应被拒
  - 验证大文件被 `LimitedReader` 截断

## 6. 前端改动

### 6.1 依赖

- `remark-gfm`
- `rehype-highlight` + `highlight.js`（含一个亮主题 + 一个暗主题 CSS）

### 6.2 `src/components/MarkdownArticle.tsx`（新）

- 包 `<ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeHighlight]}>`
- 自定义 `img` renderer：`src` → `/api/proxy/image?url=<encodeURIComponent(src)>`；加 `loading="lazy"` `decoding="async"` `style="max-width:100%; height:auto"`
- 自定义 `a` renderer：`target="_blank" rel="noopener noreferrer"`
- 代码块：rehype-highlight 默认渲染即可

### 6.3 `src/hooks/useReaderSettings.ts`（新）

接口：`{ mode, setMode, fontSize, setFontSize, fontFamily, setFontFamily, bgTheme, setBgTheme }`

localStorage key：`rsspal:reader-settings`
默认值：`{ mode: 'normal', fontSize: 16, fontFamily: 'sans', bgTheme: 'default' }`
取值集合：
- `mode`: `'normal' | 'reading'`
- `fontSize`: `12..24`（步进 1）
- `fontFamily`: `'sans' | 'serif'`
- `bgTheme`: `'default' | 'sepia' | 'green' | 'gray' | 'dark'`

订阅 `storage` 事件以多 tab 同步。

### 6.4 `src/components/ReaderSettingsPanel.tsx`（新）

- Aa 浮动按钮（`position:fixed; right:24px; bottom:24px;`），仅在 `mode === 'reading'` 时挂载
- 点开弹小卡片：
  - 字号 `[A−][16 px][A+]`（受 `fontSize` 控制；当前值显示在中间）
  - 字体 `[Sans][Serif]` 二选一（当前选中高亮）
  - 背景 5 个圆色块（圆环高亮当前选中）
- 关闭交互：点击面板外部 / 按 Esc

### 6.5 `src/pages/ArticlePage.tsx`（改造）

新结构（伪代码）：

```
const { mode, setMode, fontSize, fontFamily, bgTheme } = useReaderSettings()

if (mode === 'reading') {
  // 在 <html> 或 <body> 上加 data-reader-bg={bgTheme} (effect)
  return <ReadingLayout
    article={article}
    fontSize={fontSize}
    fontFamily={fontFamily}
    onExit={() => setMode('normal')}
  />
}

// 普通模式：沿用现有布局，正文渲染换成 <MarkdownArticle>
```

`<ReadingLayout>` 内部：
- 顶部最小条：`← 退出阅读模式`（左）
- 标题 + 元信息（时间·字数·阅读分钟）
- 折叠 AI 摘要：`▶ 查看 AI 摘要` 默认折叠；展开后显示 brief + detailed
- 正文：`<MarkdownArticle>` + inline `style={{ fontSize, fontFamily }}`
- 浮动 `<ReaderSettingsPanel>`
- 隐藏：操作按钮（👍👎⭐）、上下篇/返回、分享、抓取按钮

普通模式按钮区追加 `📖 阅读模式`，点击 → `setMode('reading')`。

正文渲染统一改用 `<MarkdownArticle>`：
- 之前的 `article.content.split(/\n{2,}/).map(...)` 直接删掉
- AI 摘要部分继续用现有 `<ReactMarkdown>` 渲染（无变化）

### 6.6 `src/index.css`

新增：

```css
[data-reader-bg='default'] { --reader-bg:#fff;    --reader-fg:#1a1a1a; --reader-muted:#666; --reader-code-bg:#f3f3f3; }
[data-reader-bg='sepia']   { --reader-bg:#f5edd6; --reader-fg:#3a2f1a; --reader-muted:#7a6f55; --reader-code-bg:#ebe2c8; }
[data-reader-bg='green']   { --reader-bg:#cce8cf; --reader-fg:#1f2e1f; --reader-muted:#456b48; --reader-code-bg:#bcdcbf; }
[data-reader-bg='gray']    { --reader-bg:#ebebeb; --reader-fg:#262626; --reader-muted:#666; --reader-code-bg:#dcdcdc; }
[data-reader-bg='dark']    { --reader-bg:#1a1a1a; --reader-fg:#d4d4d4; --reader-muted:#888; --reader-code-bg:#262626; }

.reading-layout {
  background: var(--reader-bg);
  color: var(--reader-fg);
  min-height: 100vh;
}
.reading-layout .markdown-body { max-width: 720px; margin: 0 auto; line-height: 1.8; }
.reading-layout img { max-width: 100%; height: auto; }
.reading-layout pre { overflow-x: auto; padding: 12px; border-radius: 6px; background: var(--reader-code-bg); }
.reading-layout blockquote { border-left: 3px solid var(--reader-muted); padding-left: 12px; color: var(--reader-muted); }
```

### 6.7 键盘

- 普通模式：保持现有 `n/j/p/k/m/Esc/Backspace`
- 阅读模式：
  - `r` 进出阅读模式（新增；和 `m` 风格一致）
  - `Esc` 退出阅读模式
  - 其他热键暂时禁用，防止阅读时误触

### 6.8 测试

手动验证清单（无前端 unit test 框架）：
- 含图片 / 代码块 / 表格 / 列表 / 多级标题的文章渲染正确
- 微信公众号文章图片可加载（验证代理 Referer 注入）
- 阅读模式：5 个背景色都正确切换；字号 12/16/20/24 都能改；sans/serif 切换有可见区别
- localStorage 持久：刷新页面后设置保留；同源多 tab 设置同步
- 退出阅读模式：操作/导航按钮回归

## 7. 部署 / 回滚

部署顺序：
1. 后端先发：图片代理路由 + content_fetcher markdown 化（旧前端遇到 markdown 仍能按 `\n{2,}` 渲染——markdown 是纯文本超集，无回归）
2. 前端跟进：MarkdownArticle / ReaderSettingsPanel / ArticlePage 改造
3. 回填脚本最后跑：`go run ./cmd/backfill_content --qps 1`（建议 off-peak 跑）

回滚：
- 前端：还原 ArticlePage 即可（旧渲染对 markdown 退化为 plain text 显示，可读但缺图）
- 后端：还原 content_fetcher；现存的 markdown content 用旧渲染管道也能读
- 回填：幂等，可中断；中断时部分文章是新格式部分是旧格式，两者都可读

## 8. 已知风险

- **html-to-markdown 库的边角 case**：复杂嵌套表格、内嵌 `<svg>` 可能转得不完美。缓解：测试固化常见 case；用户可手动「重新抓取」单篇。
- **图片代理放大流量**：单用户场景下不显著；若发现某些图反复 cache miss，再考虑加后端 LRU。
- **SSRF**：必须在 PR review 阶段重点审查 IP 黑名单覆盖度。
- **回填速度**：默认 QPS=1 跑完几千篇可能要 1+ 小时；中断可重跑。

## 9. 文件清单

新增：
- `backend/internal/api/proxy.go`
- `backend/cmd/backfill_content/main.go`
- `frontend/src/components/MarkdownArticle.tsx`
- `frontend/src/components/ReaderSettingsPanel.tsx`
- `frontend/src/components/ReadingLayout.tsx`
- `frontend/src/hooks/useReaderSettings.ts`
- `backend/internal/api/proxy_test.go`

修改：
- `backend/internal/rss/content.go`
- `backend/internal/rss/content_test.go`（新增 cases）
- `backend/internal/api/router.go`（注册 proxy 路由）
- `frontend/src/pages/ArticlePage.tsx`
- `frontend/src/index.css`
- `frontend/package.json`
- `backend/go.mod`
