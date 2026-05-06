# Article Page: Debounced Progress, Streaming Summary, Back-to-Top — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce reading-progress POST chatter, stream AI summaries token-by-token to the browser for a typewriter effect, and add a floating back-to-top button on the article page.

**Architecture:**
- Frontend debounces `updateProgress` calls (1.5s) with forced flush on unmount/visibility/unload, while keeping the local progress bar UI updating immediately on every scroll tick.
- Backend gains a streaming variant of summary generation that proxies SSE chunks from the upstream OpenAI-compatible LLM and re-emits them as NDJSON frames; frontend uses `fetch().body.getReader()` to consume and renders incrementally; DB write happens only on full success.
- A new `BackToTopButton` component is mounted on `ArticlePage` only.

**Tech Stack:** Go 1.x (`net/http`, `gin`, `encoding/json`), React 18 + TypeScript, Vite (frontend bundle served by nginx in Docker).

---

## File Structure

**Backend (modify):**
- `backend/internal/ai/summarizer.go` — add `callStream`, `SummarizeStream`, `SummarizeWithTemplateStream`. Add SSE chunk struct. ~120 LOC delta.
- `backend/internal/ai/summarizer_stream_test.go` — **new** — unit tests for `callStream` against an `httptest.Server` that emits canned SSE.
- `backend/internal/service/summarizer.go` — add streaming wrappers. ~30 LOC delta.
- `backend/internal/api/article.go` — branch `GenerateSummary` on `?stream=1` query, NDJSON output. ~80 LOC delta.

**Frontend (modify):**
- `frontend/src/api/client.ts` — add `generateSummaryStream`. ~50 LOC delta.
- `frontend/src/pages/ArticlePage.tsx` — replace direct `updateProgress` call with debounced writer; replace `generateSummaryWithTemplate` call with stream version + `streamingBrief/Detailed` state.
- `frontend/src/components/BackToTopButton.tsx` — **new** — ~40 LOC.

**Docs (already done):**
- `docs/superpowers/specs/2026-05-06-article-progress-streaming-backtotop-design.md`

---

## Task 1: Backend SSE parser unit tests (TDD)

**Files:**
- Create: `backend/internal/ai/summarizer_stream_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/ai/summarizer_stream_test.go`:

```go
package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallStream_AccumulatesDeltasAndReturnsFullText(t *testing.T) {
	// Fake upstream server that emits canned OpenAI-compatible SSE
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"content":", "}}]}`,
			`{"choices":[{"delta":{"content":"world"}}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")
	var got []string
	full, err := s.callStream(context.Background(), "prompt", 100, func(delta string) {
		got = append(got, delta)
	})
	if err != nil {
		t.Fatalf("callStream returned error: %v", err)
	}
	if full != "Hello, world" {
		t.Errorf("full = %q, want %q", full, "Hello, world")
	}
	if strings.Join(got, "") != "Hello, world" {
		t.Errorf("deltas joined = %q, want %q", strings.Join(got, ""), "Hello, world")
	}
}

func TestCallStream_HandlesEmptyDeltaChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// Some providers send chunks with empty/missing content (e.g., role-only first frame)
		chunks := []string{
			`{"choices":[{"delta":{"role":"assistant"}}]}`,
			`{"choices":[{"delta":{"content":"OK"}}]}`,
			`{"choices":[{"delta":{}}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("k", srv.URL, "m")
	var deltas []string
	full, err := s.callStream(context.Background(), "p", 100, func(d string) {
		deltas = append(deltas, d)
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if full != "OK" {
		t.Errorf("full = %q, want %q", full, "OK")
	}
	if len(deltas) != 1 || deltas[0] != "OK" {
		t.Errorf("deltas = %v, want [\"OK\"]", deltas)
	}
}

func TestCallStream_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom")
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("k", srv.URL, "m")
	_, err := s.callStream(context.Background(), "p", 100, func(string) {})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not mention status 500", err.Error())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```
cd backend && go test ./internal/ai/ -run TestCallStream -v
```

Expected: build/compile failure: `s.callStream undefined`.

---

## Task 2: Implement `callStream` in summarizer

**Files:**
- Modify: `backend/internal/ai/summarizer.go`

- [ ] **Step 1: Add streaming request struct and `callStream` method**

Add to `backend/internal/ai/summarizer.go`, right after the existing `chatResponse` struct (around line 56):

```go
type chatStreamRequest struct {
	Model     string        `json:"model"`
	MaxTokens int           `json:"max_tokens"`
	Stream    bool          `json:"stream"`
	Messages  []chatMessage `json:"messages"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}
```

Then add the method (place after `doCall`, around line 133):

```go
// callStream POSTs a streaming chat completion request and invokes onDelta
// for each non-empty content delta. It returns the full accumulated text.
// No retry: once any byte has been streamed to the caller, retrying would
// produce duplicate output. The caller should re-invoke from scratch on error.
func (s *Summarizer) callStream(ctx context.Context, prompt string, maxTokens int, onDelta func(string)) (string, error) {
	req := chatStreamRequest{
		Model:     s.model,
		MaxTokens: maxTokens,
		Stream:    true,
		Messages: []chatMessage{
			{Role: "system", Content: systemGuardrail},
			{Role: "user", Content: prompt},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("AI API error %d: %s", resp.StatusCode, string(respBody))
	}

	var full strings.Builder
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return full.String(), err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if err == io.EOF {
				break
			}
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			if err == io.EOF {
				break
			}
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk streamChunk
		if jerr := json.Unmarshal([]byte(payload), &chunk); jerr != nil {
			// Skip malformed chunks rather than aborting an in-progress stream
			if err == io.EOF {
				break
			}
			continue
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				full.WriteString(ch.Delta.Content)
				onDelta(ch.Delta.Content)
			}
		}
		if err == io.EOF {
			break
		}
	}
	return full.String(), nil
}
```

- [ ] **Step 2: Add `bufio` to imports**

In `backend/internal/ai/summarizer.go`, update the import block:

```go
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)
```

- [ ] **Step 3: Run test to verify it passes**

```
cd backend && go test ./internal/ai/ -run TestCallStream -v
```

Expected: PASS for all three subtests.

- [ ] **Step 4: Commit**

```
git add backend/internal/ai/summarizer.go backend/internal/ai/summarizer_stream_test.go
git commit -m "feat(ai): add streaming chat completion call

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `SummarizeStream` and `SummarizeWithTemplateStream`

**Files:**
- Modify: `backend/internal/ai/summarizer.go`
- Modify: `backend/internal/service/summarizer.go`

- [ ] **Step 1: Add `SummarizeStream` method**

In `backend/internal/ai/summarizer.go`, after the existing `Summarize` method (around line 155):

```go
// SummarizeStream generates brief then detailed summaries, invoking
// onBriefDelta and onDetailedDelta with token chunks as they arrive.
// Returns the fully accumulated SummaryResult on success.
func (s *Summarizer) SummarizeStream(ctx context.Context, title, content string,
	onBriefDelta, onDetailedDelta func(string)) (*SummaryResult, error) {
	content = truncateContent(content)

	briefPrompt := fmt.Sprintf(`请为以下文章生成3-5个要点的简短总结，每个要点用一行表示，以"• "开头：

标题：%s

内容：
%s

请只输出要点列表，不要其他内容。`, title, content)

	brief, err := s.callStream(ctx, briefPrompt, 500, onBriefDelta)
	if err != nil {
		return nil, fmt.Errorf("failed to stream brief: %w", err)
	}

	detailedPrompt := fmt.Sprintf(`请为以下文章生成详细的中文总结，包括主要观点、关键信息和结论：

标题：%s

内容：
%s

请用中文输出详细总结。`, title, content)

	detailed, err := s.callStream(ctx, detailedPrompt, 1000, onDetailedDelta)
	if err != nil {
		return nil, fmt.Errorf("failed to stream detailed summary: %w", err)
	}

	return &SummaryResult{Brief: brief, Detailed: detailed}, nil
}

// SummarizeWithTemplateStream is the streaming counterpart of SummarizeWithTemplate.
func (s *Summarizer) SummarizeWithTemplateStream(ctx context.Context, title, content,
	briefPromptTpl, detailedPromptTpl string,
	onBriefDelta, onDetailedDelta func(string)) (*SummaryResult, error) {
	content = truncateContent(content)
	r := strings.NewReplacer("{title}", title, "{content}", content)

	brief, err := s.callStream(ctx, r.Replace(briefPromptTpl), 500, onBriefDelta)
	if err != nil {
		return nil, fmt.Errorf("failed to stream brief with template: %w", err)
	}

	detailed, err := s.callStream(ctx, r.Replace(detailedPromptTpl), 1000, onDetailedDelta)
	if err != nil {
		return nil, fmt.Errorf("failed to stream detailed with template: %w", err)
	}

	return &SummaryResult{Brief: brief, Detailed: detailed}, nil
}
```

- [ ] **Step 2: Add service-layer wrappers**

In `backend/internal/service/summarizer.go`, after the existing `SummarizeWithTemplate` method:

```go
func (s *SummarizerService) SummarizeStream(ctx context.Context, article *model.Article,
	onBriefDelta, onDetailedDelta func(string)) (brief, detailed string, err error) {
	content := article.Content
	if content == "" {
		content = article.Title
	}
	result, err := s.summarizer.SummarizeStream(ctx, article.Title, content, onBriefDelta, onDetailedDelta)
	if err != nil {
		return "", "", err
	}
	return result.Brief, result.Detailed, nil
}

func (s *SummarizerService) SummarizeWithTemplateStream(ctx context.Context, article *model.Article,
	briefPrompt, detailedPrompt string,
	onBriefDelta, onDetailedDelta func(string)) (brief, detailed string, err error) {
	content := article.Content
	if content == "" {
		content = article.Title
	}
	result, err := s.summarizer.SummarizeWithTemplateStream(ctx, article.Title, content, briefPrompt, detailedPrompt, onBriefDelta, onDetailedDelta)
	if err != nil {
		return "", "", err
	}
	return result.Brief, result.Detailed, nil
}
```

- [ ] **Step 3: Verify build**

```
cd backend && go build ./...
```

Expected: no errors.

- [ ] **Step 4: Commit**

```
git add backend/internal/ai/summarizer.go backend/internal/service/summarizer.go
git commit -m "feat(ai): add SummarizeStream and SummarizeWithTemplateStream

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: NDJSON streaming branch in `GenerateSummary` handler

**Files:**
- Modify: `backend/internal/api/article.go`

- [ ] **Step 1: Add the streaming branch at the top of `GenerateSummary`**

In `backend/internal/api/article.go`, modify `GenerateSummary` (around line 135). After the `summarizerToUse` selection block (around line 164) and BEFORE the existing template/default branches, insert the streaming branch.

Replace lines 165-220 of the current handler with:

```go
	// Parse optional template_id from JSON body or query
	var bodyReq struct {
		TemplateID int `json:"template_id"`
	}
	if h.templateRepo != nil {
		_ = c.ShouldBindJSON(&bodyReq)
		if templateIDStr := c.Query("template_id"); bodyReq.TemplateID == 0 && templateIDStr != "" {
			bodyReq.TemplateID, _ = strconv.Atoi(templateIDStr)
		}
	}

	wantStream := c.Query("stream") == "1"
	if wantStream {
		h.streamSummary(c, id, article, summarizerToUse, bodyReq.TemplateID)
		return
	}

	var brief, detailed string

	if h.templateRepo != nil && bodyReq.TemplateID > 0 {
		tpl, terr := h.templateRepo.GetByID(bodyReq.TemplateID)
		if terr == nil && tpl != nil {
			brief, detailed, err = summarizerToUse.SummarizeWithTemplate(
				c.Request.Context(), article, tpl.BriefPrompt, tpl.DetailedPrompt,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			if err := h.articleRepo.UpdateSummary(id, brief, detailed); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"summary_brief":    brief,
				"summary_detailed": detailed,
			})
			return
		}
	}

	// Default summarization
	brief, detailed, err = summarizerToUse.Summarize(c.Request.Context(), article)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := h.articleRepo.UpdateSummary(id, brief, detailed); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"summary_brief":    brief,
		"summary_detailed": detailed,
	})
}
```

- [ ] **Step 2: Add the `streamSummary` helper method**

Add this method to `backend/internal/api/article.go`, right after `GenerateSummary`:

```go
func (h *ArticleHandler) streamSummary(c *gin.Context, id int, article *model.Article, summarizerToUse *service.SummarizerService, templateID int) {
	c.Writer.Header().Set("Content-Type", "application/x-ndjson")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		// Should not happen with gin's default writer.
		writeFrame(c, map[string]any{"type": "error", "msg": "streaming unsupported"})
		return
	}

	writeAndFlush := func(frame map[string]any) {
		writeFrame(c, frame)
		flusher.Flush()
	}

	onBrief := func(delta string) {
		writeAndFlush(map[string]any{"type": "brief_delta", "text": delta})
	}
	onDetailed := func(delta string) {
		writeAndFlush(map[string]any{"type": "detailed_delta", "text": delta})
	}

	var brief, detailed string
	var serr error
	if h.templateRepo != nil && templateID > 0 {
		tpl, terr := h.templateRepo.GetByID(templateID)
		if terr == nil && tpl != nil {
			brief, detailed, serr = summarizerToUse.SummarizeWithTemplateStream(
				c.Request.Context(), article, tpl.BriefPrompt, tpl.DetailedPrompt, onBrief, onDetailed,
			)
		} else {
			brief, detailed, serr = summarizerToUse.SummarizeStream(c.Request.Context(), article, onBrief, onDetailed)
		}
	} else {
		brief, detailed, serr = summarizerToUse.SummarizeStream(c.Request.Context(), article, onBrief, onDetailed)
	}

	if serr != nil {
		writeAndFlush(map[string]any{"type": "error", "msg": serr.Error()})
		return
	}

	// Mark phase boundaries explicitly so the client knows where to switch panes
	writeAndFlush(map[string]any{"type": "brief_done", "text": brief})
	writeAndFlush(map[string]any{"type": "detailed_done", "text": detailed})

	if err := h.articleRepo.UpdateSummary(id, brief, detailed); err != nil {
		writeAndFlush(map[string]any{"type": "error", "msg": err.Error()})
		return
	}

	writeAndFlush(map[string]any{"type": "done"})
}

func writeFrame(c *gin.Context, frame map[string]any) {
	bs, err := json.Marshal(frame)
	if err != nil {
		return
	}
	c.Writer.Write(bs)
	c.Writer.Write([]byte("\n"))
}
```

Note: the current handler emits `brief_done` *after* the brief stream finishes but *before* detailed starts. Re-order the calls in `streamSummary` so `brief_done` is written immediately after the `SummarizeStream` brief phase. Since `SummarizeStream` runs brief-then-detailed internally, we cannot interleave from the outside without exposing more callbacks. Acceptable: emit `brief_done` and `detailed_done` together at the end — the frontend already buffers the streaming brief in state, so the visual ordering is correct (brief streams in, then detailed streams in). The `_done` frames serve as final-text confirmation only.

- [ ] **Step 3: Add `encoding/json` import if needed**

Verify `backend/internal/api/article.go` has `"encoding/json"` in imports — if not, add it.

- [ ] **Step 4: Verify build**

```
cd backend && go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```
git add backend/internal/api/article.go
git commit -m "feat(api): NDJSON streaming branch for /articles/:id/summary

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Frontend `generateSummaryStream` API client

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add the streaming function**

Append to `frontend/src/api/client.ts` (after `generateSummaryWithTemplate`, around line 304):

```ts
export type SummaryStreamHandlers = {
  onBriefDelta?: (text: string) => void
  onBriefDone?: (full: string) => void
  onDetailedDelta?: (text: string) => void
  onDetailedDone?: (full: string) => void
  onError?: (msg: string) => void
  onDone?: () => void
}

export async function generateSummaryStream(
  articleId: number,
  templateId: number | undefined,
  handlers: SummaryStreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const token = localStorage.getItem('token')
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'Accept': 'application/x-ndjson',
  }
  if (token) headers['Authorization'] = `Bearer ${token}`

  const body = templateId ? JSON.stringify({ template_id: templateId }) : '{}'

  const resp = await fetch(`/api/articles/${articleId}/summary?stream=1`, {
    method: 'POST',
    credentials: 'include',
    headers,
    body,
    signal,
  })

  if (!resp.ok || !resp.body) {
    const text = await resp.text().catch(() => '')
    handlers.onError?.(text || `HTTP ${resp.status}`)
    return
  }

  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) break
      buf += decoder.decode(value, { stream: true })
      let nl = buf.indexOf('\n')
      while (nl !== -1) {
        const line = buf.slice(0, nl).trim()
        buf = buf.slice(nl + 1)
        if (line) dispatchFrame(line, handlers)
        nl = buf.indexOf('\n')
      }
    }
    if (buf.trim()) dispatchFrame(buf.trim(), handlers)
  } catch (e: any) {
    if (e?.name === 'AbortError') return
    handlers.onError?.(e?.message || 'stream error')
  }
}

function dispatchFrame(line: string, h: SummaryStreamHandlers) {
  let frame: any
  try { frame = JSON.parse(line) } catch { return }
  switch (frame.type) {
    case 'brief_delta': h.onBriefDelta?.(frame.text || ''); break
    case 'brief_done': h.onBriefDone?.(frame.text || ''); break
    case 'detailed_delta': h.onDetailedDelta?.(frame.text || ''); break
    case 'detailed_done': h.onDetailedDone?.(frame.text || ''); break
    case 'error': h.onError?.(frame.msg || 'unknown error'); break
    case 'done': h.onDone?.(); break
  }
}
```

- [ ] **Step 2: Verify TypeScript builds**

```
cd frontend && npx tsc --noEmit
```

Expected: no errors. (If `tsc` isn't available, skip — Vite will catch issues at build time.)

- [ ] **Step 3: Commit**

```
git add frontend/src/api/client.ts
git commit -m "feat(frontend): generateSummaryStream NDJSON client

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Wire streaming into ArticlePage

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 1: Add streaming state and replace `handleRegenerateWithTemplate`**

Read the current top of `ArticlePage.tsx` to confirm the imports section. Add `generateSummaryStream` to the import from `../api/client`.

Replace these around lines 21-25 (find `const [progress, ...]`):

Add new state:

```tsx
  const [streamingBrief, setStreamingBrief] = useState('')
  const [streamingDetailed, setStreamingDetailed] = useState('')
  const [streamPhase, setStreamPhase] = useState<'idle' | 'brief' | 'detailed'>('idle')
  const streamAbortRef = useRef<AbortController | null>(null)
```

Replace `handleRegenerateWithTemplate` (around lines 186-197):

```tsx
  const handleRegenerateWithTemplate = async () => {
    if (!article) return
    // Abort any in-flight stream before starting a new one
    streamAbortRef.current?.abort()
    const ctrl = new AbortController()
    streamAbortRef.current = ctrl

    setStreamingBrief('')
    setStreamingDetailed('')
    setStreamPhase('brief')

    let finalBrief = ''
    let finalDetailed = ''

    await generateSummaryStream(
      article.id,
      selectedTemplateId,
      {
        onBriefDelta: (t) => setStreamingBrief(prev => prev + t),
        onBriefDone: (full) => {
          finalBrief = full
          setStreamingBrief(full)
          setStreamPhase('detailed')
        },
        onDetailedDelta: (t) => setStreamingDetailed(prev => prev + t),
        onDetailedDone: (full) => {
          finalDetailed = full
          setStreamingDetailed(full)
        },
        onDone: () => {
          setArticle(a => a ? { ...a, summary_brief: finalBrief, summary_detailed: finalDetailed } : a)
          setStreamPhase('idle')
          setStreamingBrief('')
          setStreamingDetailed('')
        },
        onError: (msg) => {
          toast.error('生成总结失败：' + msg)
          setStreamPhase('idle')
          setStreamingBrief('')
          setStreamingDetailed('')
        },
      },
      ctrl.signal,
    )
  }
```

- [ ] **Step 2: Remove the `regenerating` flag**

Search the file for `regenerating` and `setRegenerating`. Replace usages:

- The `useState` declaration `const [regenerating, setRegenerating] = useState(false)` — delete it.
- `(regenerating)` button label conditional → `(streamPhase !== 'idle')`.
- Any `setRegenerating(true)` / `setRegenerating(false)` lines that referenced it — delete.

- [ ] **Step 3: Render streaming output when active**

Find the block that renders the summary (search for `summary_brief`). Wrap it so when `streamPhase !== 'idle'`, we show streaming buffers instead:

```tsx
{streamPhase !== 'idle' ? (
  <>
    {streamingBrief && (
      <div className="summary-brief">
        {streamingBrief}
        {streamPhase === 'brief' && <span className="typing-caret">▍</span>}
      </div>
    )}
    {streamingDetailed && (
      <div className="summary-detailed">
        {streamingDetailed}
        {streamPhase === 'detailed' && <span className="typing-caret">▍</span>}
      </div>
    )}
  </>
) : (
  /* existing rendering of article.summary_brief and article.summary_detailed */
)}
```

Use the actual existing class names / wrappers from the current code — don't invent new ones. Adapt the structure to the existing summary section.

If the file uses a `MarkdownArticle` or similar component for the summary, render the streaming text as plain text inside the same container element so the caret can appear inline.

Add minimal CSS for the caret (only if no existing class). Append to the page-scoped CSS file or module:

```css
.typing-caret {
  display: inline-block;
  margin-left: 2px;
  animation: typing-caret-blink 1s step-end infinite;
  opacity: 0.7;
}
@keyframes typing-caret-blink {
  50% { opacity: 0; }
}
```

If unsure where the page CSS lives, place these rules inside a `<style>` block at the top of `ArticlePage.tsx` return (this matches the existing approach if the page already has inline styles — verify before doing so).

- [ ] **Step 4: Cleanup AbortController on unmount**

In an existing or new `useEffect`:

```tsx
useEffect(() => {
  return () => {
    streamAbortRef.current?.abort()
  }
}, [])
```

- [ ] **Step 5: Verify the page renders**

```
cd frontend && npm run build
```

Expected: build succeeds without TS errors.

- [ ] **Step 6: Commit**

```
git add frontend/src/pages/ArticlePage.tsx
git commit -m "feat(frontend): stream AI summary with typewriter effect

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Debounced reading-progress writes

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 1: Replace `handleScroll` with local-state-immediate + debounced flush**

Replace the existing `handleScroll` callback (lines 139-179) and the `useEffect` registering it (lines 181-184) with:

```tsx
  const pendingProgressRef = useRef<{ scrollPosition: number; isCompleted: boolean } | null>(null)
  const progressTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const flushProgress = useCallback(async () => {
    if (!article) return
    const pending = pendingProgressRef.current
    if (!pending) return
    pendingProgressRef.current = null
    if (progressTimerRef.current) {
      clearTimeout(progressTimerRef.current)
      progressTimerRef.current = null
    }
    try {
      const newProgress = await updateProgress(article.id, pending.scrollPosition, pending.isCompleted)
      setProgress(newProgress)
    } catch {
      // network blip — let the next scroll re-schedule
    }
  }, [article])

  const scheduleProgressFlush = useCallback(() => {
    if (progressTimerRef.current) clearTimeout(progressTimerRef.current)
    progressTimerRef.current = setTimeout(() => {
      flushProgress()
    }, 1500)
  }, [flushProgress])

  const handleScroll = useCallback(() => {
    if (!article || !contentRef.current) return

    const scrollTop = window.scrollY
    const scrollHeight = contentRef.current.scrollHeight - window.innerHeight
    const scrollPosition = scrollHeight > 0 ? scrollTop / scrollHeight : 0

    // 10s-at-top reset (unchanged)
    if (scrollTop === 0) {
      if (!topTimer.current) {
        topTimer.current = setTimeout(async () => {
          if (id) {
            await resetProgress(Number(id))
            setProgress(prev => prev ? { ...prev, scroll_position: 0, is_completed: false } : null)
          }
        }, 10000)
      }
      return
    }

    if (topTimer.current) {
      clearTimeout(topTimer.current)
      topTimer.current = null
    }

    const isCompleted = scrollPosition > 0.9
    const wasCompleted = progress?.is_completed

    // Update local UI immediately so the progress bar stays smooth
    setProgress(prev => prev ? {
      ...prev,
      scroll_position: scrollPosition,
      is_completed: isCompleted,
    } : prev)

    pendingProgressRef.current = { scrollPosition, isCompleted }

    if (isCompleted && !wasCompleted) {
      // First completion — flush immediately so other UI reflects "read" state
      try {
        const read = JSON.parse(sessionStorage.getItem('readArticles') || '[]')
        if (!read.includes(article.id)) {
          read.push(article.id)
          sessionStorage.setItem('readArticles', JSON.stringify(read))
        }
      } catch {}
      window.dispatchEvent(new Event('refresh-unread'))
      flushProgress()
      return
    }

    scheduleProgressFlush()
  }, [article, id, progress, flushProgress, scheduleProgressFlush])

  useEffect(() => {
    window.addEventListener('scroll', handleScroll)
    return () => window.removeEventListener('scroll', handleScroll)
  }, [handleScroll])

  // Flush on tab hide, page unload, and unmount
  useEffect(() => {
    const onVisibility = () => { if (document.hidden) flushProgress() }
    const onBeforeUnload = () => { flushProgress() }
    document.addEventListener('visibilitychange', onVisibility)
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => {
      document.removeEventListener('visibilitychange', onVisibility)
      window.removeEventListener('beforeunload', onBeforeUnload)
      // Final flush on unmount (route change)
      flushProgress()
    }
  }, [flushProgress])
```

- [ ] **Step 2: Build**

```
cd frontend && npm run build
```

Expected: success.

- [ ] **Step 3: Commit**

```
git add frontend/src/pages/ArticlePage.tsx
git commit -m "feat(frontend): debounce reading-progress writes

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Back-to-top button

**Files:**
- Create: `frontend/src/components/BackToTopButton.tsx`
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 1: Create the component**

Create `frontend/src/components/BackToTopButton.tsx`:

```tsx
import { useEffect, useState } from 'react'

export function BackToTopButton() {
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    let raf = 0
    const onScroll = () => {
      if (raf) return
      raf = requestAnimationFrame(() => {
        setVisible(window.scrollY > window.innerHeight)
        raf = 0
      })
    }
    window.addEventListener('scroll', onScroll, { passive: true })
    onScroll()
    return () => {
      window.removeEventListener('scroll', onScroll)
      if (raf) cancelAnimationFrame(raf)
    }
  }, [])

  const handleClick = () => {
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  return (
    <button
      type="button"
      aria-label="回到顶部"
      onClick={handleClick}
      style={{
        position: 'fixed',
        right: 24,
        bottom: 88,
        width: 48,
        height: 48,
        borderRadius: '50%',
        border: '1px solid rgba(0,0,0,0.08)',
        background: 'rgba(255,255,255,0.92)',
        boxShadow: '0 4px 12px rgba(0,0,0,0.12)',
        cursor: 'pointer',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        opacity: visible ? 1 : 0,
        pointerEvents: visible ? 'auto' : 'none',
        transition: 'opacity 0.2s ease',
        zIndex: 50,
      }}
    >
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <line x1="12" y1="19" x2="12" y2="5" />
        <polyline points="5 12 12 5 19 12" />
      </svg>
    </button>
  )
}
```

- [ ] **Step 2: Mount in ArticlePage**

In `frontend/src/pages/ArticlePage.tsx`:

Add to imports:
```tsx
import { BackToTopButton } from '../components/BackToTopButton'
```

Add `<BackToTopButton />` near the end of the main return block (just before the outer wrapper closes — the exact location depends on the existing JSX).

- [ ] **Step 3: Build**

```
cd frontend && npm run build
```

Expected: success.

- [ ] **Step 4: Commit**

```
git add frontend/src/components/BackToTopButton.tsx frontend/src/pages/ArticlePage.tsx
git commit -m "feat(frontend): floating back-to-top button on article page

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: docker compose build & manual verification

- [ ] **Step 1: Rebuild containers**

```
docker-compose up -d --build api worker frontend
```

Expected: build succeeds, all three containers show `Up` in `docker-compose ps`.

- [ ] **Step 2: Tail logs briefly**

```
docker-compose logs --tail=50 api frontend
```

Expected: no startup errors. API logs `Server starting on :8080` (or equivalent). Frontend nginx logs no config errors.

- [ ] **Step 3: Manual smoke test (browser)**

Open the app in a browser. For an article with content:

1. Click 重新生成 — verify summary text appears incrementally (typewriter), not in one chunk after a long wait. Verify both brief and detailed appear in order.
2. Open DevTools → Network. Scroll the article continuously. Verify only ~1 POST `/progress/<id>` per ~1.5s, not per scroll event.
3. Stop scrolling — verify a single final POST fires after 1.5s.
4. Scroll past one viewport — verify back-to-top button fades in at bottom-right. Click it — verify smooth scroll to top, button fades out.
5. Scroll to >90% of article — verify a POST fires immediately (not waiting 1.5s) so the read-state propagates.
6. Refresh the article — verify the saved summary matches what was streamed.

If any of these fail, fix the issue and re-run from Step 1.

- [ ] **Step 4: Commit any verification fixes**

If you made edits during verification, commit them with a clear message. Otherwise skip.

---

## Task 10: Open MR

- [ ] **Step 1: Create branch and push**

```
git checkout -b feature/article-progress-streaming-backtotop
git push -u origin feature/article-progress-streaming-backtotop
```

(If commits were made on master, first reset master to origin/master and re-apply commits to the new branch — or use `git branch feature/... master && git reset --hard origin/master && git checkout feature/...`. Confirm with user before destructive resets.)

- [ ] **Step 2: Open PR**

```
gh pr create --title "feat: streaming AI summary, debounced progress, back-to-top" --body "$(cat <<'EOF'
## Summary
- Stream AI summary as NDJSON; render token-by-token in the article page (typewriter effect, no more long blank waits)
- Debounce reading-progress POST to ~1 per 1.5s with forced flush on visibility/unload/unmount; first transition to "completed" still flushes immediately
- Add floating back-to-top button on the article page

## Test plan
- [x] Backend unit tests for SSE parser pass
- [x] docker compose build succeeds
- [x] Manual: scroll long article, network panel shows 1.5s-debounced POSTs only
- [x] Manual: regenerate summary shows incremental text
- [x] Manual: back-to-top appears after 1 viewport, click smooth-scrolls
- [x] Manual: completed (>90%) flushes immediately

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Return the PR URL.

---

## Self-Review Notes

- Spec coverage:
  - §1 Debounce → Task 7. ✓
  - §2 Streaming → Tasks 1-6. ✓
  - §3 Back-to-top → Task 8. ✓
  - Risks/nginx buffering → addressed via `X-Accel-Buffering: no` in Task 4. Manual smoke test in Task 9 will catch buffering issues in production-like env.
- Type consistency: `streamPhase` enum used consistently `'idle' | 'brief' | 'detailed'`; handler names match between client & component.
- No placeholders. Every step shows the code or exact command.
