# PDF 网摘设计

**日期**：2026-05-25
**作者**：morefreeze + Claude
**状态**：Design approved，待 writing-plans 出实现计划

## 背景

当前"网摘"功能仅支持 HTML 页面：Chrome 扩展抓取 `document.documentElement.outerHTML`，POST 到 `/api/bookmarklet/capture`，服务端用 goquery 解析提取主内容。PDF 文件不在支持范围内——Chrome 内置 PDF viewer 不是普通 HTML 文档，扩展抓到的 `outerHTML` 没有可用正文，服务端也只能处理 HTML 输入。

## 目标

扩展"网摘"能力以覆盖 PDF：

1. **场景 A**：浏览器里打开的 PDF（`https://…/foo.pdf`）
2. **场景 B**：本地 PDF 文件（`file:///…/foo.pdf`）
3. **场景 C**：直接给一个远程 PDF 链接，服务端下载并解析

支持：文字提取、图片抽取（每篇上限 100 张去重后图片）、扫描版 PDF 的 OCR 降级。

## 非目标

- 不存原始 PDF 文件
- 不为 PDF 建独立 feed 类型 / 独立列表
- 不改 50,000 字符上限
- 不抽视频 / 音频
- 不拖拽上传、不批量上传
- 不引入对象存储

## 数据模型

PDF 网摘完全复用现有"网摘"数据通路：

- 落到 `GetOrCreateClipFeed(user.ID)` 这条"网摘" feed 下，`is_clip=true`
- 不新增表
- Migration 028 新增字段：
  - `articles.processing_status TEXT DEFAULT 'ready'` — 取值 `ready` / `processing` / `failed`
  - `articles.processing_error TEXT` — 失败时人类可读原因
- 内容字段：按页分节的 Markdown（详见 §排版）
- 字符上限沿用 50,000，超过截断 + `...`

## 处理流程

### 同步尝试 + 异步 OCR 降级

**同步段（≤ 2 秒）**：

1. `pdftotext -layout -nopgbrk input.pdf -` 提取纯文本，按 `\f`（form feed）拆页
2. 若所有页文字合计 ≥ 200 字符 → 视为数字版 PDF
   - 同步抽图（`pdfimages`）、写文件、装配 markdown
   - `processing_status='ready'`
   - 返回 `{status: "created"|"updated", article_id}`
3. 否则 → 视为扫描版 PDF
   - 建空 article（`processing_status='processing'`，标题/URL 已知，content 为空字符串）
   - 返回 `{status: "processing", article_id}`
   - 由 worker 异步完成 OCR

**异步段（worker）**：

- 复用现有 worker 轮询模型，每分钟扫一遍 `processing_status='processing'` 的 article，并发 1（OCR CPU 密集，不并发）
- `pdftoppm -r 300 input.pdf /tmp/page` 把每页渲染成 300 dpi PNG
- 每页 PNG 喂给 `tesseract /tmp/page-N.png - -l chi_sim+eng`
- 抽图（同 §图片抽取与去重）、按页号装配 markdown
- 成功：`UpdateContent` + 清摘要触发现有 backfillSummaries 重算 + `processing_status='ready'`
- 部分失败（某些页 OCR 出错）：把成功页的结果拼起来正常入库，每个失败页在 markdown 里写 `> [第 N 页 OCR 失败：<err>]` 占位，整篇仍标 `ready`
- 全部失败 / pipeline 异常：`processing_status='failed'` + `processing_error` 写人类可读原因；前端能看到错误条目

**为什么这条路径**：

- 数字版 PDF 是绝大多数场景，`pdftotext` 亚秒级，用户立刻拿到结果
- 扫描版 PDF Tesseract 1–3 秒/页，100 页就是几分钟，扩展 popup 不可能挂这么久
- 两条路径都不阻塞用户，且数字版完全无须 worker 介入

## PDF 解析包 `internal/pdfextract`

对外两个函数：

```go
package pdfextract

type Result struct {
    Title              string         // PDF metadata /Title → 文件名 → URL
    Pages              []PageContent  // {Text, Images []ImageRef}
    Markdown           string         // 装配好的最终内容
    TotalImagesOriginal int           // 去重前的图数量
    TotalImagesKept    int            // 实际写入的图数量
}

type PageContent struct {
    Text   string
    Images []ImageRef
}

type ImageRef struct {
    Idx      int     // 在该 article 中的唯一序号
    Bytes    []byte  // 图片二进制
    Format   string  // "png" | "jpg"
    SHA256   string  // 用于去重 + ETag
    Width    int
    Height   int
}

// 同步快通路：仅文字 + 图片，不做 OCR
func ExtractFast(pdfBytes []byte) (Result, error)

// 异步深通路：失败回落到 OCR
func ExtractWithOCR(pdfBytes []byte) (Result, error)
```

所有外部依赖（`pdftotext`、`pdfimages`、`pdftoppm`、`tesseract`、`pdfinfo`）通过 `exec.Command` 调用。包内不直接持有这些工具的 Go 绑定，方便后续替换。

## 图片抽取与去重

PDF 经常每页都嵌同一个 logo / 页眉。`pdfimages` 抽完是一堆重复字节。处理顺序：

1. `pdfimages -all input.pdf /tmp/img` 抽出所有图片，自带页号信息
2. 对每张图算 SHA-256
3. 重复 SHA 复用第一个出现的 idx，markdown 里只引用一次
4. 去重后的图按"页号、页内出现顺序"排序，取前 100 张
5. 超过 100 张：丢弃后续，在 markdown 文末加：
   ```
   > 注：原 PDF 共 X 张图（去重后 Y 张），超出 100 张限制，已省略后 (Y-100) 张。
   ```

## Markdown 排版

按页分节：

```markdown
## 第 1 页

<第 1 页正文>

![](/api/articles/123/images/0.png)
![](/api/articles/123/images/1.png)

## 第 2 页

<第 2 页正文>

![](/api/articles/123/images/2.png)
```

- 空页（无文字也无图）：直接跳过，不出 `## 第 N 页` 标题
- 一页只有文字 / 只有图：保留 `## 第 N 页` 标题 + 内容
- 图片放在该页正文之后

## HTTP 端点

### `POST /api/bookmarklet/capture-pdf`

- Content-Type: `multipart/form-data`
- 字段：
  - `url` (string，必填) — 原 PDF 的 URL，可以是 `https://…`、`file:///…`
  - `title` (string，可选) — 浏览器 tab title，若 PDF metadata 没有 title 时用
  - `file` (binary，必填) — PDF 二进制
- 鉴权：`Authorization: Bearer <bookmarklet_token>`（沿用现有 bookmarklet 端点的鉴权方式）
- 大小限制：32 MiB（PDF 比 HTML 大，比现有的 4 MiB 大；超过 413）
- 用于：扩展（A / B）

### `POST /api/bookmarklet/capture-pdf-url`

- Content-Type: `application/json`
- Body: `{"url": "https://…"}`
- 鉴权：同上
- 服务端 `http.Get(url)` 拿 PDF（10 秒超时，32 MiB 上限），再走同一处理函数
- 用于：(C) 场景；前端"添加 feed → type=pdf" 调用

### 内部统一函数

两个 capture 端点都调：

```go
func (h *BookmarkletHandler) processPDFCapture(
    user *model.User, url, browserTitle string, pdfBytes []byte,
) (response, error)
```

里面：

1. `pdfextract.ExtractFast` 同步尝试
2. 按 §处理流程 分支
3. 查重（`(user_id, normalized_url)` 唯一），命中走更新 + 清摘要；未命中走 Create
4. 图片写到 `${BACKUP_DIR}/article_images/<article_id>/`
5. 触发 backup snapshot（如已配置）
6. 返回前端约定结构

### `GET /api/articles/:id/images/:idx`

- 鉴权：JWT（要求 article 属于该用户）
- 响应：
  - `Content-Type: image/png` 或 `image/jpeg`
  - `Cache-Control: public, max-age=31536000, immutable`（极长，因图片 bytes 永不变）
  - `ETag: "<sha256[:16]>"`
  - 支持 `If-None-Match` → 304
- 文件路径：`${BACKUP_DIR}/article_images/<article_id>/<idx>.<ext>`

## 图片存储

- 路径：`${BACKUP_DIR}/article_images/<article_id>/<idx>.{png,jpg}`
- 创建 article 时按需 mkdir
- 删除 article 时清理整个目录（在 `articleRepo.Delete` 里加 hook）
- 不在数据库存图（避免行体积膨胀）
- 不存 SHA 到 DB——服务端响应时即时 `os.ReadFile` + 算 SHA 当 ETag，配上极长缓存反正几乎不重算

## 前端

### `MarkdownArticle.tsx` 改动

当前所有 `<img src>` 都被包成 `/api/proxy/image?url=...`。需 short-circuit：

```ts
const proxied = src
  ? src.startsWith('/api/articles/')      // 自家图片，直接渲染
    ? src
    : `/api/proxy/image?url=${encodeURIComponent(src)}`
  : undefined
```

保持模块作用域 plugins + `React.memo`，避免重新触发 `<img>` 重 mount。

### 添加 feed 对话框

- type 选择器加新选项 `📄 PDF 链接（单篇网摘）`
- 选中后：
  - URL 输入框文案变 "PDF 文件链接"
  - 隐藏 `expand_links`、`fetch_full_content` 等 RSS 专属字段
  - 提交按钮变 "抓取并加入网摘"
- 提交调 `POST /api/bookmarklet/capture-pdf-url`
- 成功 toast：
  - `ready` → `✅ 已加入网摘：《标题》` + "打开文章" 链接
  - `processing` → `⏳ 已入库，OCR 处理中` + "打开文章" 链接（点过去看到处理中条目）

### 文章列表

- `processing_status='processing'` → 小角标 `⏳ 处理中`
- `processing_status='failed'` → 小角标 `❌ 处理失败`，hover 显示 `processing_error`
- `ready` → 普通条目

## 扩展（v1.5.0）

### `manifest.json`

- `version`: 1.4.0 → **1.5.0**
- `host_permissions` 已经是 `<all_urls>`，够用
- 给 `options.html` 加一行说明：local PDF 需在 `chrome://extensions` 该扩展详情页手动勾选"允许访问文件 URL"

### `popup.js`

进入 popup 时判断当前 tab 是否 PDF：

```js
function isPDFTab(tab) {
  if (!tab?.url) return false
  const url = tab.url.split('?')[0].split('#')[0]
  return url.endsWith('.pdf')
}
```

兜底用 `fetch(tab.url, { method: 'HEAD' })` 看 `Content-Type: application/pdf`。

- 是 PDF：按钮文字变 "网摘此 PDF"；点击后：
  - `await fetch(tab.url)` 拿 Blob
  - 拼 `FormData`：`url=tab.url`, `title=tab.title`, `file=blob`
  - POST 到 `/api/bookmarklet/capture-pdf`
- 不是 PDF：原有 HTML 流程，**零回归**
- 响应处理沿用现有 success / duplicate / error UI，额外加 `processing` 状态：
  - `⏳ PDF 已入库，OCR 处理中（约 1–3 秒/页）`

## Docker

`backend/Dockerfile` 加：

```dockerfile
RUN apt-get update && apt-get install -y --no-install-recommends \
    poppler-utils \
    tesseract-ocr \
    tesseract-ocr-eng \
    tesseract-ocr-chi-sim \
    && rm -rf /var/lib/apt/lists/*
```

镜像增大 ~60 MB。

## Migration

`backend/migrations/028_article_processing_status.sql`：

```sql
ALTER TABLE articles
  ADD COLUMN IF NOT EXISTS processing_status TEXT NOT NULL DEFAULT 'ready',
  ADD COLUMN IF NOT EXISTS processing_error TEXT;

CREATE INDEX IF NOT EXISTS idx_articles_processing_status
  ON articles (processing_status)
  WHERE processing_status <> 'ready';
```

按照项目惯例，docker-entrypoint-initdb.d 只在空 volume 上跑一次，**新 migration 需手动 `psql < migrations/028_*.sql`** 应用到现有 DB。

## 测试

### 单元测试 `internal/pdfextract`

`testdata/` 准备：

- `digital.pdf` — 数字原生文字 PDF（短，便于 CI 跑快）
- `scanned.pdf` — 扫描影印 PDF
- `mixed.pdf` — 部分页带文字、部分扫描
- `image_heavy.pdf` — > 100 张图、含重复 logo（验证去重和上限）
- `corrupt.pdf` — 截断 / 损坏的 PDF
- `knuth-1980.pdf` — 从 https://gwern.net/doc/design/typography/1980-knuth.pdf 下载（经典数字版印刷研究 PDF，用作真实样本回归测试）

断言：

- `ExtractFast` 在 `digital.pdf` 和 `knuth-1980.pdf` 上返回非空 markdown、`TotalImagesKept > 0`、`Pages` 数符合 PDF 真实页数
- `ExtractFast` 在 `scanned.pdf` 上返回 `< 200` 字符（触发 OCR 降级）
- `ExtractWithOCR` 在 `scanned.pdf` 上能跑通（不强求 OCR 准确率，只验证不报错）
- `image_heavy.pdf` 的 `TotalImagesOriginal > TotalImagesKept`（去重生效），最终 `TotalImagesKept ≤ 100`
- `corrupt.pdf` 返回明确错误，不 panic

### API handler 测试 `api/bookmarklet_pdf_test.go`

`httptest` 起 handler，mock `pdfextract`：

- multipart 上传成功路径
- 缺 `file` / `url` → 400
- 超过 32 MiB → 413
- 无效 token → 401
- duplicate 流程（更新 vs 提示）
- URL fetch 端点：404、超时、非 PDF Content-Type 都要明确错误
- `/api/articles/:id/images/:idx` 端点：200 + 304（带 `If-None-Match`）+ 404 + 跨用户访问 403

### 手动验收清单

- A：浏览器打开 `arxiv.org/pdf/<id>` → 点扩展 → 数字版立即入库 + 摘要生成
- A：浏览器打开扫描版 PDF（找一份影印资料） → 点扩展 → 处理中状态 → 等几秒看 OCR 结果
- B：本地 `~/Downloads/foo.pdf` → 浏览器打开 → 扩展弹窗显示"PDF 模式" → 点击 → 文章入库
- B：本地扫描版 PDF → 同上 → 看到处理中、后续 OCR 完成
- C：feed 对话框选 PDF → 粘贴 `https://gwern.net/doc/design/typography/1980-knuth.pdf` → toast 成功 → 列表里能搜到、详情页能看到图

## 回滚

所有改动都是新增：

- 端点：删 handler 注册即可
- 前端 type 选项：UI 删除即可
- 扩展 PDF 分支：popup.js 注释掉判断即可
- DB 字段：保留无害，不需要 down migration

不需要数据迁移。

## 已知限制

- 50,000 字符上限会截断长 PDF（百页教材类）
- 图片只能按页粒度对齐，做不到段内对齐（PDF 元数据本身不提供此信息）
- OCR 仅支持中简 + 英；繁/日/韩 PDF 会 OCR 出近乎乱码
- 不存原图，远程 PDF 删除后该网摘条目里的图片链接（指向 `/api/articles/…/images/…`）仍然有效，但用户若想看 PDF 原文需自己保留 URL
- B 场景 `file://` URL 存到 `article.url` 后无法从其它设备点开（路径属于用户本机）；用 `NormalizeURLKeepFragment` 时按完整路径去重，跨设备同一 PDF 算两条
- 32 MiB 上传上限会拒掉超大书籍 PDF（如全本扫描书），用户能在错误信息里看到限制并自行拆页
