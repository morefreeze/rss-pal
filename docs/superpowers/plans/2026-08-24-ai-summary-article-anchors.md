# AI Summary Article Anchors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add sparse, grouped links in newly generated detailed summaries that scroll to stable locations in the rendered article body.

**Architecture:** A small line-oriented Markdown annotator exists in Go and TypeScript with matching fixtures. The backend adds canonical anchor markers only to detailed-summary model input and appends a system-owned citation instruction; the frontend converts the same markers into invisible DOM targets and handles only strict `#article-section-NNN` summary links as local navigation.

**Tech Stack:** Go 1.25, React 18, TypeScript, react-markdown, Vitest, Testing Library

---

## File Structure

- Create `backend/internal/ai/article_anchors.go`: canonical block-start detection, sequential IDs, AI-only source annotation, and detailed-summary instruction.
- Create `backend/internal/ai/article_anchors_test.go`: table tests for block selection and prompt annotation.
- Modify `backend/internal/ai/summarizer.go`: route all default, streaming, template, and vision detailed prompts through the anchor preparation helpers while leaving brief prompts unchanged.
- Modify `backend/internal/ai/summarizer_stream_test.go`: assert streamed default/template prompt separation.
- Modify `backend/internal/ai/summarizer_vision_test.go`: assert vision detailed prompts contain anchors and brief prompts do not.
- Create `frontend/src/util/articleAnchors.ts`: matching Markdown annotation and strict article-anchor parsing.
- Create `frontend/src/util/articleAnchors.test.ts`: fixture parity tests for headings, paragraphs, lists, links, images, fences, and empty content.
- Modify `frontend/src/components/MarkdownArticle.tsx`: render injected internal markers as invisible anchor targets.
- Modify `frontend/src/components/SummaryMarkdown.tsx`: handle valid local article anchors without changing external links.
- Create `frontend/src/components/SummaryMarkdown.test.tsx`: navigation, missing-target, and external-link tests.
- Modify `frontend/src/index.css`: target highlight and reduced-motion styles.

### Task 1: Backend Canonical Anchor Annotation

**Files:**
- Create: `backend/internal/ai/article_anchors.go`
- Create: `backend/internal/ai/article_anchors_test.go`

- [ ] **Step 1: Write failing annotation tests**

Add table tests with literal expected output, including this representative case:

```go
func TestAnnotateArticleForSummary(t *testing.T) {
	input := "# 标题\n\n第一段有[链接](https://example.com)。\n续行。\n\n- 第一项\n- ![](image.jpg)\n\n```go\nignored()\n```"
	want := "[正文锚点: article-section-001]\n# 标题\n\n[正文锚点: article-section-002]\n第一段有[链接](https://example.com)。\n续行。\n\n[正文锚点: article-section-003]\n- 第一项\n- ![](image.jpg)\n\n```go\nignored()\n```"
	if got := annotateArticleForSummary(input); got != want {
		t.Fatalf("annotateArticleForSummary() = %q, want %q", got, want)
	}
}
```

Also assert blank content yields no markers, image-only blocks do not consume IDs, link-only paragraphs do consume IDs, ordered and nested list items remain deterministic, blockquotes are treated as paragraphs, and fenced-code contents do not consume IDs.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `cd backend && go test ./internal/ai -run 'TestAnnotateArticleForSummary|TestArticleAnchor' -count=1`

Expected: FAIL because `annotateArticleForSummary` does not exist.

- [ ] **Step 3: Implement the minimal pure annotator**

Create a focused helper with these public-in-package contracts:

```go
const articleAnchorPrefix = "article-section-"

func articleAnchorID(index int) string {
	return fmt.Sprintf("%s%03d", articleAnchorPrefix, index)
}

func annotateArticleForSummary(content string) string
func hasAddressableArticleBlock(content string) bool
```

The scanner tracks fenced-code state, recognizes ATX headings, paragraph starts, blockquote text, and list items, skips blank/image-only/separator blocks, and inserts `[正文锚点: article-section-NNN]` immediately before each addressable block. Continuation lines remain part of the current block. Keep the logic line-oriented and dependency-free so the TypeScript twin can be exact.

- [ ] **Step 4: Run focused and package tests and verify GREEN**

Run: `cd backend && go test ./internal/ai -run 'TestAnnotateArticleForSummary|TestArticleAnchor' -count=1 && go test ./internal/ai`

Expected: PASS.

- [ ] **Step 5: Commit the backend annotator**

```bash
git add backend/internal/ai/article_anchors.go backend/internal/ai/article_anchors_test.go
git commit -m "feat(ai): annotate article summary anchors"
```

### Task 2: Add Anchors to Every Detailed-Summary Prompt Path

**Files:**
- Modify: `backend/internal/ai/article_anchors.go`
- Modify: `backend/internal/ai/summarizer.go`
- Modify: `backend/internal/ai/summarizer_stream_test.go`
- Modify: `backend/internal/ai/summarizer_vision_test.go`

- [ ] **Step 1: Write failing prompt-contract tests**

Add direct helper tests and extend the existing HTTP test servers to capture request messages. Assert:

```go
func TestBuildDetailedArticlePromptInput(t *testing.T) {
	content, instruction := buildDetailedArticlePromptInput("第一段。\n\n第二段。")
	if !strings.Contains(content, "[正文锚点: article-section-001]") ||
		!strings.Contains(content, "[正文锚点: article-section-002]") {
		t.Fatalf("missing canonical markers: %s", content)
	}
	if !strings.Contains(instruction, "按原文顺序") ||
		!strings.Contains(instruction, "不要每段都添加") ||
		!strings.Contains(instruction, "单一主题") {
		t.Fatalf("missing grouping rules: %s", instruction)
	}
}
```

For `SummarizeStream`, `SummarizeWithTemplate`, `SummarizeWithTemplateStream`, `buildVisionDetailedPrompt`, and image streaming, assert detailed prompts contain canonical markers plus the system-owned rule, while captured brief prompts contain neither. Assert empty/image-only content gets no citation instruction.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `cd backend && go test ./internal/ai -run 'DetailedArticlePrompt|SummaryAnchor|Template.*Anchor|Vision.*Anchor' -count=1`

Expected: FAIL because detailed calls still receive unannotated content.

- [ ] **Step 3: Implement one shared detailed-input helper**

Add:

```go
const detailedArticleAnchorInstruction = `请尽量按原文顺序总结，并按大意合并相邻内容。仅在确有助于定位原文的总结段落末尾添加一个 [查看原文](#article-section-NNN) 链接，NNN 必须来自正文中已有的锚点。不要每段都添加跳转；短文或整篇只讲一件事时可以完全不添加。`

func buildDetailedArticlePromptInput(content string) (annotated, instruction string) {
	annotated = annotateArticleForSummary(content)
	if !hasAddressableArticleBlock(content) {
		return content, ""
	}
	return annotated, detailedArticleAnchorInstruction
}
```

Apply `truncateContent` before this helper. Use it only for detailed prompts. Append `instruction` after user-owned custom detailed templates so templates cannot accidentally omit the navigation contract. Update `buildVisionDetailedPrompt` to accept/use annotated content and instruction; do not change `buildVisionBriefPrompt`.

- [ ] **Step 4: Run backend tests and verify GREEN**

Run: `cd backend && go test ./internal/ai -count=1 && go test ./...`

Expected: all packages PASS.

- [ ] **Step 5: Commit prompt integration**

```bash
git add backend/internal/ai/article_anchors.go backend/internal/ai/summarizer.go backend/internal/ai/summarizer_stream_test.go backend/internal/ai/summarizer_vision_test.go
git commit -m "feat(ai): cite article anchors in detailed summaries"
```

### Task 3: Render Matching Article Targets

**Files:**
- Create: `frontend/src/util/articleAnchors.ts`
- Create: `frontend/src/util/articleAnchors.test.ts`
- Modify: `frontend/src/components/MarkdownArticle.tsx`

- [ ] **Step 1: Write failing TypeScript parity tests**

Use the same literal fixtures and expected IDs as Task 1:

```ts
it('assigns stable IDs to addressable blocks only', () => {
  const source = '# 标题\n\n第一段有[链接](https://example.com)。\n续行。\n\n- 第一项\n- ![](image.jpg)'
  expect(annotateArticleMarkdown(source)).toContain('[rss-pal-anchor](#article-section-001) # 标题')
  expect(extractArticleAnchorIds(annotateArticleMarkdown(source))).toEqual([
    'article-section-001',
    'article-section-002',
    'article-section-003',
  ])
})
```

Assert headings, paragraphs, blockquotes, ordered/unordered/nested lists, inline links, link-only paragraphs, image-only blocks, fenced code, CRLF input, and empty input match the Go contract.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `cd frontend && npm test -- src/util/articleAnchors.test.ts`

Expected: FAIL because `articleAnchors.ts` does not exist.

- [ ] **Step 3: Implement the TypeScript annotator and marker renderer**

Create:

```ts
export const ARTICLE_ANCHOR_LABEL = 'rss-pal-anchor'
export const ARTICLE_ANCHOR_RE = /^#article-section-\d{3,}$/

export function annotateArticleMarkdown(source: string): string
export function parseArticleAnchor(href: string | undefined): string | null
```

Mirror the Go scanner exactly. Inject `[rss-pal-anchor](#article-section-NNN)` at the beginning of each addressable Markdown block. In `MarkdownArticle`, run this transform after existing cleanup. Update `ArticleLink` so the exact internal marker label renders as `<span id="article-section-NNN" className="article-section-anchor" aria-hidden="true" />`; all real article links continue through the existing reader-action and external-link logic.

- [ ] **Step 4: Run focused tests and verify GREEN**

Run: `cd frontend && npm test -- src/util/articleAnchors.test.ts && npm test`

Expected: parity tests and the complete Vitest suite PASS.

- [ ] **Step 5: Commit article target rendering**

```bash
git add frontend/src/util/articleAnchors.ts frontend/src/util/articleAnchors.test.ts frontend/src/components/MarkdownArticle.tsx
git commit -m "feat(reader): render stable article anchors"
```

### Task 4: Handle Summary Anchor Navigation Accessibly

**Files:**
- Modify: `frontend/src/components/SummaryMarkdown.tsx`
- Create: `frontend/src/components/SummaryMarkdown.test.tsx`
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Write failing interaction tests**

Render `SummaryMarkdown` with a valid local link and install a target element. Assert the click prevents navigation, calls `scrollIntoView`, adds the highlight class, and removes it on `animationend`. Add separate tests proving a missing target is ignored and `https://example.com` retains `target="_blank"` and `rel="noopener noreferrer"`.

```tsx
render(<SummaryMarkdown source="结论。[查看原文](#article-section-002)" />)
const target = document.createElement('p')
target.id = 'article-section-002'
target.scrollIntoView = vi.fn()
document.body.append(target)
await user.click(screen.getByRole('link', { name: '查看原文' }))
expect(target.scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' })
expect(target).toHaveClass('article-anchor-highlight')
```

- [ ] **Step 2: Run the component test and verify RED**

Run: `cd frontend && npm test -- src/components/SummaryMarkdown.test.tsx`

Expected: FAIL because summary links have no custom navigation component.

- [ ] **Step 3: Implement strict local navigation and styles**

Give `SummaryMarkdown` a link override. For `parseArticleAnchor(href)` matches, look up `document.getElementById`, prevent default, and scroll with `behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'`. Apply `article-anchor-highlight`, remove it after `animationend` plus a timeout fallback, and add `tabIndex={-1}` only to targets that are not naturally focusable before focusing for keyboard-triggered activation. External links keep the current new-tab security attributes.

Add CSS:

```css
@keyframes article-anchor-highlight {
  0%, 100% { background-color: transparent; }
  20%, 70% { background-color: color-mix(in srgb, var(--accent) 18%, transparent); }
}

.article-anchor-highlight {
  animation: article-anchor-highlight 1.6s ease-out;
  border-radius: 4px;
}

@media (prefers-reduced-motion: reduce) {
  .article-anchor-highlight { animation: none; }
}
```

- [ ] **Step 4: Run frontend tests, legacy checks, and build**

Run: `cd frontend && npm run check && npm run build`

Expected: 0 failing Vitest/legacy tests and successful TypeScript/Vite build.

- [ ] **Step 5: Commit navigation behavior**

```bash
git add frontend/src/components/SummaryMarkdown.tsx frontend/src/components/SummaryMarkdown.test.tsx frontend/src/index.css
git commit -m "feat(reader): navigate summary links to article sections"
```

### Task 5: Full Verification and Requirement Audit

**Files:**
- Verify only; modify prior files only if a failing test exposes a defect.

- [ ] **Step 1: Run the full backend suite**

Run: `cd backend && go test ./...`

Expected: all Go packages PASS.

- [ ] **Step 2: Run the full frontend suite and production build**

Run: `cd frontend && npm run check && npm run build`

Expected: all Vitest and legacy tests PASS; TypeScript and Vite build exit 0.

- [ ] **Step 3: Inspect the final diff and scope**

Run: `git diff --check HEAD~4..HEAD && git status --short && git log --oneline --max-count=6`

Expected: no whitespace errors, only planned source/test/style/doc files changed, and no user backup or unrelated worktree files included.

- [ ] **Step 4: Audit acceptance requirements**

Confirm from tests and source that only detailed summaries receive anchor instructions; custom, streaming, and vision paths are covered; anchors follow source order; link grouping and single-theme omission are explicit; missing anchors fail safely; and external links remain unchanged.

- [ ] **Step 5: Request code review**

Invoke `requesting-code-review`, address any high-confidence issues using a fresh RED/GREEN cycle, then rerun Steps 1-3 before reporting completion.
