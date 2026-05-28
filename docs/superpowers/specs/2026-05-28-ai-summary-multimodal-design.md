# AI 总结支持图片（多模态）— 设计

**Date:** 2026-05-28
**Status:** Design — pending implementation

## 背景

当前 `internal/ai/summarizer.go` 只通过 `chatMessage{Content string}` 发送纯文本，调用默认模型 `glm-4.5`（z.ai / 智谱 OpenAI 兼容端点）。对纯图片文章（如 `/articles/2273` —— 一组 `![](image_url)` 没有正文）摘要无意义。

升级目标：让总结能"看到"文章里的图片，对图文混排或纯图文章生成有信息量的 brief / detailed。

## 设计原则

- **本地化优先**：rss-pal 跑在用户本机，z.ai 服务端不可能反向访问用户的 `localhost` 取图。所以必须先把图下载到本机再 base64 内联到 AI 请求里。
- **临时缓存而非持久存**：为送 AI 下载的图片是**临时缓存**（默认 24 小时 TTL，过期自动清理），跟 PDF clip 那种"文章生命周期持久存"严格分目录。这样反复重新生成 / 短时间重试不重新下载，但磁盘不会无限涨。
- **成本可控**：多模态调用比文本贵；用启发式 + 显式开关精确控制触发面，纯文本场景一律不走多模态。
- **失败软降级**：图片下载失败、vision 调用失败、模型不可用，都 fallback 到现有纯文本总结，不阻塞用户。
- **沿用已有约定**：HTTP 客户端复用 `internal/api/proxy.go` 的 SSRF-guarded transport；并发预算复用现有 `sumSem`。

## 触发判断

| 路径 | 触发条件 |
|---|---|
| Worker backfill | `image_count >= AI_VISION_MIN_IMAGES (=3)` **且** 正文 runes（剥掉所有 `![](...)` 后）`< AI_VISION_MAX_TEXT_CHARS (=2000)` |
| 前端「重新生成」按钮 | 文章 `image_count >= 1` → 强制走 vision；零图 → 退回文本路径 |

正文 runes 用 `utf8.RuneCountInString` 计；`![](...)` 用 regex 简单粗暴抠掉（与现有 `flattenImageAltBlankLines` 风格一致）。

## 图片选择 + 过滤

按 markdown 中 `![](url)` 出现顺序提取所有 URL，按下列规则过滤：

| 过滤项 | 原因 |
|---|---|
| 头像类 URL/alt | 复用 `backend/internal/rss/content.go:isAvatarImg` 的关键词清单（`AVATAR_ATTR_KEYWORDS`, `AVATAR_URL_KEYWORDS`），但暴露一个 URL+alt 字符串版的 helper（现有的 `isAvatarImg` 接 `*goquery.Selection`，DOM-only）。新增 `rss.IsAvatarImageURL(src, alt string) bool` 共享同一清单。 |
| 本地路径 `/api/articles/<id>/images/<idx>.*` | **不丢弃 —— 仍要送给 AI**。但跳过下载步骤：直接把 URL 映射到 `/backups/article_images/<id>/<idx>.<ext>` 读盘 + base64。 |
| 非 `http://` / `https://` / 非上述本地路径 | 不抓 `data:` 内嵌图、未知协议、未知相对路径 |

剩下的取前 `AI_VISION_MAX_IMAGES` (=6) 张，顺序按 markdown 原序。

## 图片本地化（新包 `internal/imagefetch`）

```go
package imagefetch

// FetchAndStore downloads each remote URL through the SSRF-guarded transport
// used by api/proxy.go, resizes oversized images, and writes them to
// cfg.Dir/<articleID>/<idx>.<ext>. Returns local file paths in the same order
// as input (missing/failed entries filtered out).
//
// Local-path inputs (URLs matching /api/articles/<id>/images/<idx>.<ext>) are
// resolved to their on-disk path under cfg.LocalArticleImagesDir and returned
// directly — no copy, no download. Cleanup never touches that dir.
//
// Files already in cfg.Dir are reused (mtime touched so periodic cleanup
// doesn't evict them while still in active use).
func FetchAndStore(
    ctx context.Context,
    articleID int,
    urls []string,
    cfg Config,
) ([]string, error)

// CleanupExpired walks cfg.Dir and deletes any file whose mtime is older than
// cfg.TTL. Empty article-id subdirs are removed afterward. Intended to be
// invoked periodically from cmd/worker (similar cadence to existing backup
// retention sweep).
func CleanupExpired(ctx context.Context, cfg Config) (removed int, err error)

type Config struct {
    Dir                    string        // AI summary image cache root; default /backups/ai_summary_cache
    LocalArticleImagesDir  string        // where PDF clip images live; default /backups/article_images (read-only from this pkg's POV)
    MaxLongSide            int           // resize trigger; default 1024
    TTL                    time.Duration // cache file age limit; default 24h
}
```

**Per-image pipeline:**

1. **Index → path**: `<cfg.Dir>/<articleID>/<idx>.jpg` (index is the input slice position). All cached files are JPEG-normalised, so extension is always `.jpg`.
2. **Stat the target**: exists & non-zero → `os.Chtimes` to refresh mtime, return path (cache hit).
3. **Download** via `proxy.SharedClient` (refactor: extract the `*http.Client` + SSRF check from `internal/api/proxy.go` into `internal/httpx` so both proxy handler and imagefetch share it).
4. **Decode** with stdlib `image` (registers `image/png`, `image/jpeg`, `image/gif` decoders).
5. **Resize** with `golang.org/x/image/draw` (BiLinear) if `max(W, H) > MaxLongSide`, scaling to a longest-side of `MaxLongSide` preserving aspect ratio.
6. **Re-encode** as JPEG quality 85 (always — even if no resize — to normalise; original payload bytes are usually larger than JPEG q85 anyway).
7. **Write** atomically: write to `<path>.tmp`, then `os.Rename`.
8. **On failure** (download timeout, decode error, write error): log warn + return `("", err)` to caller; caller drops the entry from the result slice.

**Cleanup pipeline (`CleanupExpired`):**

1. Walk `cfg.Dir` (skip `LocalArticleImagesDir`).
2. For each regular file with `mtime + cfg.TTL < now`: `os.Remove`.
3. After processing each `<articleID>` subdir: if empty, `os.Remove` the dir.
4. Errors per-file are logged + counted, do not abort the walk.

Worker invokes `CleanupExpired` once per cycle (60s tick) — cheap because it's just a `filepath.Walk` + `Stat`, and most entries skip without I/O if recent.

**Total payload budget**: after building all file paths, caller loads each, accumulates base64 byte counts; once total > `AI_VISION_PAYLOAD_BUDGET_MB * 1024 * 1024 / 0.75` (the /0.75 reverses base64's 33% overhead), stop including more.

## AI 模块改造

Extend `chatMessage.Content` from `string` to `interface{}` so we can emit either plain text or the OpenAI vision content-block array.

```go
type chatMessage struct {
    Role    string      `json:"role"`
    Content interface{} `json:"content"`
}

type contentBlock struct {
    Type     string         `json:"type"`     // "text" | "image_url"
    Text     string         `json:"text,omitempty"`
    ImageURL *imageURLBlock `json:"image_url,omitempty"`
}

type imageURLBlock struct {
    URL string `json:"url"` // e.g. "data:image/jpeg;base64,..."
}
```

**Existing text path is unchanged** — keep passing a `string` for `Content` so legacy non-vision callers don't shift to a different request shape.

**New entry point:**

```go
func (s *Summarizer) SummarizeWithImages(
    ctx context.Context,
    title, content string,
    imagePaths []string,
    template *Template,  // existing param
) (*SummaryResult, error)
```

- Reads each image path from disk, base64-encodes, builds the `[]contentBlock` (one text block + N image_url blocks).
- Uses **`cfg.AI.VisionModel`** for the model field (separate from `DefaultModel`).
- Runs the same brief + detailed two-call pattern. Both calls carry the same images. (2× image cost vs. inventing a combined single-shot path is the simpler win.)
- On vision-call failure (HTTP error, model rejection, timeout): falls back to `Summarize()` (text-only). Logs a single warn line including article id + failure cause.

**Streaming counterpart:**

```go
func (s *Summarizer) SummarizeWithImagesStream(
    ctx, title, content string,
    imagePaths []string,
    template *Template,
    onBriefDelta func(string),
    onDetailedDelta func(string),
) (*SummaryResult, error)
```

Mirrors current `SummarizeStream`. Same NDJSON-deltas-back contract to the HTTP layer.

## Worker integration

`cmd/worker/main.go` `backfillSummaries()` selects an article and, before the existing `Summarize()` call:

```go
useVision := shouldUseVisionAuto(article, cfg.AI)
if useVision {
    paths := selectImageURLsFromMarkdown(article.Content, cfg.AI)
    paths = filterAvatarsAndLocalRefs(paths)
    paths = take(paths, cfg.AI.VisionMaxImages)
    localPaths, _ := imagefetch.FetchAndStore(ctx, article.ID, paths, cfg.AI.ImageFetch())
    if len(localPaths) > 0 {
        result, err := summarizer.SummarizeWithImages(ctx, title, content, localPaths, template)
        // on err -> fall through to text path
    }
}
// existing text-only Summarize() path stays as fallback
```

`shouldUseVisionAuto` lives in `internal/ai` alongside the summarizer (or a new `internal/ai/policy.go` if `summarizer.go` is getting crowded). It reads `imageURLs := extractImageURLs(content)` and applies the heuristic.

**Cache cleanup**: once per fetch cycle (60s tick), `cmd/worker/main.go` calls `imagefetch.CleanupExpired(ctx, cfg.AI.ImageFetch())`. Per-cycle invocation amortises cleanup; nothing is time-critical about TTL accuracy.

## HTTP service path (frontend regen)

`internal/api/article.go` `SummarizeStream`:

- New query param `force_vision=1` (truthy means "use vision path if any images exist"). Front-end's regen button always sets this (server decides if any actual images exist; if none, server falls through to text path — no UI guard needed). Worker backfill never hits this endpoint; backfill runs its own auto heuristic.
- If `force_vision=1` & at least one image URL exists in markdown: route to `SummarizeWithImagesStream` (after `imagefetch.FetchAndStore`).
- Else: existing `SummarizeStream` text path.

Frontend `frontend/src/api/client.ts`:

```ts
export async function* generateSummaryStream(
  articleId: number,
  opts?: { templateId?: number; forceVision?: boolean }
)
```

The article-page summary card sets `forceVision: true` for the user's regenerate click. The `useReaderSettings` / autorun paths don't override — they get text-only.

## Configuration (env)

| Env | Default | Notes |
|---|---|---|
| `AI_VISION_MODEL` | `glm-4v-plus` | sent in chat completion `"model"` field for vision calls |
| `AI_VISION_MAX_IMAGES` | `6` | hard cap per article |
| `AI_VISION_MAX_LONG_SIDE` | `1024` | resize threshold; px |
| `AI_VISION_PAYLOAD_BUDGET_MB` | `4` | total base64 budget; drops tail images on overflow |
| `AI_VISION_MIN_IMAGES` | `3` | auto-trigger image-count floor |
| `AI_VISION_MAX_TEXT_CHARS` | `2000` | auto-trigger text-length ceiling |
| `AI_VISION_CACHE_DIR` | `/backups/ai_summary_cache` | temp cache root; **must not equal `/backups/article_images`** (PDF clip persistent storage) |
| `AI_VISION_CACHE_TTL_HOURS` | `24` | cache file age limit; files older than this are removed by worker sweep |

All wired through `internal/config/config.go` with the same `getenv` / `getenvInt` helpers used today; collected into a single `AIConfig.Vision` struct so call sites don't read env directly.

## Concurrency

Reuse existing `sumSem = make(chan struct{}, maxConcurrentSummary=2)` from `cmd/worker/main.go`. Vision calls are slower but rare (heuristic gate), so the same budget should hold. The existing 6-minute per-article timeout via `context.WithTimeout` covers worst-case multi-image processing.

If post-launch metrics show vision calls starving text calls, split into a separate `visionSumSem` (size 1) — leave that for follow-up.

## Database / schema

No schema change required. `summary_brief` and `summary_detailed` are reused as-is.

**Optional (not in this scope)**: a `summary_provider TEXT` column to tag rows with `text` / `vision`. Skipped unless we discover we need to retro-distinguish. The git-style "current behavior is the default" is good enough for now.

## Backward compat

- Existing summaries are not touched. No batch backfill / re-summarization triggered by this change.
- Articles re-summarized later (template change, fetch-content-then-resummarize, explicit user click) naturally pick up the new heuristic.
- The vision-call → text-fallback path means that even if `AI_VISION_MODEL` is misconfigured or vision is down, summaries still produce successfully via the existing text path.

## Testing

| Surface | Strategy |
|---|---|
| `internal/imagefetch.FetchAndStore` | Table tests with `httptest.Server` returning canned PNG/JPEG/GIF, plus oversize and corrupt cases. Use `t.TempDir()` for storage. Assert cache hit refreshes mtime. |
| `internal/imagefetch.CleanupExpired` | Test with `t.TempDir()`: seed files at varied mtimes; assert age-based removal + empty subdir cleanup; assert `LocalArticleImagesDir` is untouched even when older than TTL. |
| `internal/ai` request shape | Mock OpenAI-compatible server (existing pattern if any, else `httptest.Server`); assert `Content` shape is `string` for legacy callers and `[]contentBlock` for vision callers. |
| `internal/ai.shouldUseVisionAuto` | Pure function table-test (no I/O). |
| Worker integration | Smoke test: feed in synthetic article via existing seed harness, run one cycle, assert summary contains marker phrases from the canned image / text. |

## Failure-mode summary

| Failure | Behavior |
|---|---|
| Image download HTTP error | Log warn, drop image, continue with remaining |
| Image decode error | Log warn, drop image, continue |
| All images dropped | Skip vision path entirely, run text-only `Summarize()` |
| Vision API HTTP error | Log warn (`vision summary failed, falling back to text`), run text-only `Summarize()` |
| Vision model invalid response | Same — fall back to text |
| `imagefetch.FetchAndStore` disk write error | Log warn, drop that one path, continue |
| `imagefetch.CleanupExpired` error on individual file | Log warn, count, continue walking (don't abort) |

## Out of scope

- OCR-only fallback for vision unavailable scenarios. (Article 2273 will still get a useful summary because GLM-4v reads text inside images directly.)
- Batch re-summarization of historical articles. User can manually regen if they want.
- Per-user "use vision" toggle in UI. Frontend regen button just always uses vision when applicable; if user complains, add toggle later.
- Vision summaries for child articles (`is_link_set=true` parents and their children) — they get the same path as any other article; nothing special.
- A dedicated `visionSumSem`. Will revisit if 2-shared starves text calls.
