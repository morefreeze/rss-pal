# Article Page: Debounced Progress, Streaming Summary, Back-to-Top

Date: 2026-05-06
Status: Approved (brainstorming)
Scope: `frontend/src/pages/ArticlePage.tsx`, `frontend/src/api/client.ts`, `frontend/src/components/`, `backend/internal/ai/summarizer.go`, `backend/internal/service/summarizer.go`, `backend/internal/api/article.go`

## Problem

Three pain points on the article reading page:

1. **Progress POSTs are too chatty.** `handleScroll` fires on every scroll event and awaits `updateProgress` each time. Long reading sessions produce hundreds of redundant requests.
2. **AI summary generation often "breaks" mid-flight.** The endpoint is fully blocking — frontend sends POST and waits for both `brief` and `detailed` to finish. Long upstream LLM calls hit timeouts, the user sees nothing until success/failure, and intermittent failures lose the whole result.
3. **No back-to-top.** Long articles force the user to manually scroll back.

## Solution Overview

- Debounce progress writes to the network (1.5s after scroll stop), with forced flush on unmount/visibility/unload.
- Stream AI summary from upstream LLM through the backend to the browser as NDJSON chunks; render incrementally for a typewriter effect; persist to DB only on full success.
- Add a floating "back to top" button visible after one viewport of scroll.

---

## 1. Debounced Progress Updates

### Frontend changes (`frontend/src/pages/ArticlePage.tsx`)

- Replace the current `handleScroll` body's network call with a local-state-first + debounced-network pattern:
  - Local state (`progress`) updates immediately on every scroll tick so the top progress bar stays smooth.
  - Network call (`updateProgress`) is scheduled via a debounce timer (1500ms after last scroll event).
- Implement as an inline `useDebouncedProgressWriter` (lives in `ArticlePage.tsx`; no new file unless it grows). API:
  ```ts
  const writer = useDebouncedProgressWriter(articleId, {
    delayMs: 1500,
    onFlushed: (p) => setProgress(p),
  })
  writer.schedule({ scrollPosition, isCompleted })
  writer.flush() // imperative, returns Promise
  ```
  Internals:
  - Holds latest `{ scrollPosition, isCompleted }` in a ref.
  - `schedule` sets a `setTimeout`; subsequent `schedule` calls reset the timer.
  - `flush` clears timer and POSTs immediately.
- Forced flush points:
  - `useEffect` cleanup when `articleId` changes / component unmounts.
  - `document.visibilitychange` → when `document.hidden` becomes true.
  - `window.beforeunload` (best-effort; browsers may not await async).
- Special-case **first transition to `isCompleted = true`**: bypass debounce (call `flush` immediately) so the article-list "read" state propagates without delay.
- "10s at top resets progress" logic stays as-is (independent timer).

### No backend changes for progress.

The existing `updateProgress` endpoint is fine; we just call it less.

### Acceptance criteria

- Continuously scrolling for 30 seconds produces at most ~1 POST every 1.5s (network panel).
- Stopping scroll → exactly 1 POST 1.5s later.
- Closing the tab / navigating away mid-read fires exactly one final POST.
- Crossing the 90% completion threshold fires immediately (not 1.5s later).
- Top progress bar UI continues to update smoothly during scroll (driven by local state, not the network).

---

## 2. Streaming AI Summary (NDJSON)

### Wire format

`POST /api/articles/:id/summary?stream=1` (or `Accept: application/x-ndjson`).
Body unchanged (`{ "template_id": <id?> }`).
Response: `Content-Type: application/x-ndjson`, one JSON object per line, flushed immediately.

```
{"type":"brief_delta","text":"• "}
{"type":"brief_delta","text":"要点 1"}
{"type":"brief_done","text":"<full brief>"}
{"type":"detailed_delta","text":"本文..."}
{"type":"detailed_done","text":"<full detailed>"}
{"type":"done"}
```

On error, a single `{"type":"error","msg":"..."}` frame is emitted, then the stream closes. **No DB write happens on error** — the previous `summary_brief` / `summary_detailed` stays.

### Backend changes

**`backend/internal/ai/summarizer.go`**
- Add `(s *Summarizer) callStream(ctx, prompt, maxTokens, onDelta func(string)) (full string, err error)`:
  - Marshals request body with `"stream": true`.
  - POSTs to `s.baseURL + "/chat/completions"`.
  - Reads response line-by-line as SSE:
    - Lines beginning with `data: ` are JSON OpenAI-style chunks; parse, extract `choices[0].delta.content`, call `onDelta(chunk)`, append to `full`.
    - `data: [DONE]` ends the loop.
  - Returns the accumulated `full` string.
  - Retry semantics: same 3-attempt retry shell as `call`, but only retries if no bytes have been streamed to the caller yet (otherwise we'd duplicate output).
- Add `SummarizeStream(ctx, title, content, onBriefDelta, onDetailedDelta) (*SummaryResult, error)`: runs brief streaming first, then detailed.
- Add `SummarizeWithTemplateStream(ctx, title, content, briefTpl, detailedTpl, onBriefDelta, onDetailedDelta) (*SummaryResult, error)`: same shape, with template substitution like the existing non-stream version.

**`backend/internal/service/summarizer.go`**
- Add `SummarizeStream` and `SummarizeWithTemplateStream` thin wrappers, mirroring the existing pair.

**`backend/internal/api/article.go`** (`GenerateSummary`)
- Branch on `c.Query("stream") == "1"` (or `Accept: application/x-ndjson`):
  - Set headers: `Content-Type: application/x-ndjson`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`, `Connection: keep-alive`.
  - Helper `writeFrame(c, frame any)`: marshal JSON, write `frame + "\n"`, `c.Writer.Flush()`.
  - Pick streaming method (template or default), pass two `onDelta` callbacks that write `brief_delta` / `detailed_delta` frames.
  - On success of both phases: write `brief_done` (after brief stream), `detailed_done` (after detailed stream), then call `articleRepo.UpdateSummary(id, brief, detailed)`, then write `done`.
  - On any error: write `{"type":"error","msg":...}`, return without DB write.
  - Use `summarizerToUse` selection logic identical to the existing non-stream path (user AI config / global config).
- Existing non-stream JSON path remains intact for the worker / weekly digest / any other callers.

### Frontend changes

**`frontend/src/api/client.ts`**
- Add `generateSummaryStream(articleId, templateId | undefined, handlers)`:
  ```ts
  type StreamHandlers = {
    onBriefDelta?: (text: string) => void
    onBriefDone?: (full: string) => void
    onDetailedDelta?: (text: string) => void
    onDetailedDone?: (full: string) => void
    onError?: (msg: string) => void
    onDone?: () => void
  }
  ```
  Implementation: `fetch(`/api/articles/${id}/summary?stream=1`, { method: 'POST', credentials: 'include', headers: {...}, body: JSON.stringify(...), signal })`. Use a `TextDecoder` and a buffer; split on `\n`; each non-empty line `JSON.parse` → dispatch by `type`. Returns an `AbortController`-aware promise so callers can cancel.

**`frontend/src/pages/ArticlePage.tsx`**
- New state: `streamingBrief: string`, `streamingDetailed: string`, `streaming: 'idle' | 'brief' | 'detailed'`.
- Replace `handleRegenerateWithTemplate`'s call to `generateSummaryWithTemplate` with `generateSummaryStream`.
  - On `onBriefDelta(t)` → `setStreamingBrief(prev => prev + t)`; ensure `streaming === 'brief'`.
  - On `onBriefDone(full)` → `setStreamingBrief(full)` (replace, ensures consistency).
  - Switch `streaming` to `'detailed'`; same pattern for detailed.
  - On `onDone` → `setArticle(a => ({ ...a, summary_brief: brief, summary_detailed: detailed }))`, clear streaming buffers, `streaming = 'idle'`.
  - On `onError(msg)` → toast error, clear streaming buffers, leave `article.summary_*` unchanged.
- Render logic:
  - When `streaming !== 'idle'`: show `streamingBrief` (with a blinking caret while `streaming === 'brief'`), and once detailed starts, show `streamingDetailed` below it (with caret while `streaming === 'detailed'`).
  - Otherwise: show `article.summary_brief` / `article.summary_detailed` as today.
- Cancellation: store an `AbortController` ref; abort on unmount or if the user clicks "重新生成" while streaming.
- Replace the existing `regenerating` flag with `streaming !== 'idle'` (button label: `生成中...` while non-idle).

### Acceptance criteria

- Clicking "生成总结" / "重新生成" produces visible incremental text within ~2 seconds (no 30s wait staring at empty box).
- If the upstream LLM cuts off mid-detailed, the brief is still visible on screen but DB is not updated; clicking again re-generates from scratch.
- On full success the article reload shows the same summary as what was streamed.
- Existing non-stream `POST /articles/:id/summary` continues to work (worker, tests).

---

## 3. Back-to-Top Button

**New component `frontend/src/components/BackToTopButton.tsx`**

- Visible state: `scrollY > window.innerHeight`. Updated via a `scroll` listener with `requestAnimationFrame` coalescing.
- Render: `position: fixed; right: 24px; bottom: 88px; width: 48px; height: 48px;` rounded button. Up-arrow inline SVG. `aria-label="回到顶部"`.
- Visibility transition: `opacity .2s`; `pointer-events: none` when hidden so it can't accidentally be clicked.
- Color: reuse the same neutral surface used by `ReaderSettingsPanel` (read CSS variables / class names from there to stay consistent).
- Click: `window.scrollTo({ top: 0, behavior: 'smooth' })`.

**Mount point**

- Render `<BackToTopButton />` once inside `ArticlePage.tsx` (top-level of its return tree). Not added to other pages.

### Acceptance criteria

- Button is invisible at top of article; appears after scrolling more than one viewport.
- Click smoothly scrolls to top; button hides again automatically once near top.
- Doesn't overlap with the existing reader settings panel.

---

## Out of Scope

- WebSocket transport.
- Auto-resume of a streamed summary after disconnect.
- Saving partial summaries to DB.
- Back-to-top on non-article pages.
- Keyboard shortcut for back-to-top.
- Visual redesign of the summary section.

## Risks / Notes

- **Nginx buffering**: in production the frontend is served by nginx. Need to ensure the streaming endpoint's response is not buffered. The `X-Accel-Buffering: no` header tells nginx not to buffer; verify `nginx.conf` doesn't override `proxy_buffering` for this path. If it does, add a path-specific `proxy_buffering off;` and `proxy_request_buffering off;`.
- **`beforeunload` async**: browsers do not reliably await async POSTs. Use `navigator.sendBeacon` as a fallback for the final flush if practical, otherwise accept best-effort — the next mount will record progress as soon as the user re-opens the article.
- **Z.AI / GLM-4.5 streaming format**: assumed OpenAI-compatible SSE (`data: {...}` lines, `[DONE]` terminator). If the upstream sends a different shape, `callStream` will need adjustment — verify with a one-off curl during implementation.
