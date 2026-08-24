# Summary Anchor Link Label Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Display `跳转原文` for every valid article-section summary link immediately and make newly generated detailed summaries store the same label.

**Architecture:** Normalize only the visible children of links accepted by `parseArticleAnchor`; keep their href, icon, scrolling, focus, and highlight behavior intact. Independently replace the label in the backend's existing detailed-summary anchor instruction, without rewriting stored summaries or changing anchor eligibility.

**Tech Stack:** React 18, TypeScript, React Markdown, Testing Library, Vitest, Go, standard Go testing

---

## File structure

- Modify `frontend/src/components/SummaryMarkdown.tsx`: render the canonical visible label for valid article anchors.
- Modify `frontend/src/components/SummaryMarkdown.test.tsx`: cover old/new/arbitrary stored labels and update valid-anchor accessible-name queries.
- Modify `backend/internal/ai/article_anchors.go`: use `跳转原文` in the active instruction and example.
- Modify `backend/internal/ai/summarizer_anchor_prompt_test.go`: enforce the new prompt label and reject stale active wording.

### Task 1: Normalize existing summary link labels at render time

**Files:**
- Modify: `frontend/src/components/SummaryMarkdown.tsx`
- Test: `frontend/src/components/SummaryMarkdown.test.tsx`

- [ ] **Step 1: Add failing compatibility coverage**

Replace the current valid-link decoration test with a parameterized test plus the existing external/invalid isolation assertions:

```tsx
it.each([
  ['old stored label', '查看原文'],
  ['new stored label', '跳转原文'],
  ['arbitrary stored label', 'Jump'],
])('normalizes %s for a valid article anchor', (_case, storedLabel) => {
  render(<SummaryMarkdown source={`[${storedLabel}](#article-section-001)`} />)

  const articleLink = screen.getByRole('link', { name: '跳转原文' })
  expect(articleLink.textContent).toBe('跳转原文⌖')
  expect(articleLink.querySelectorAll('.summary-article-link-icon')).toHaveLength(1)
  expect(articleLink.querySelector('.summary-article-link-icon')?.getAttribute('aria-hidden')).toBe('true')
})

it('does not normalize non-article links', () => {
  render(<SummaryMarkdown source={[
    '[External](https://example.com)',
    '[Fragment](#other-section)',
    '[Invalid](#article-section-01)',
  ].join('\n\n')} />)

  for (const name of ['External', 'Fragment', 'Invalid']) {
    const link = screen.getByRole('link', { name })
    expect(link.classList.contains('summary-article-link')).toBe(false)
    expect(link.querySelector('.summary-article-link-icon')).toBeNull()
  }
})
```

- [ ] **Step 2: Update existing valid-anchor selectors to the canonical accessible name**

In every test that renders a valid `#article-section-NNN` link, replace queries such as:

```tsx
screen.getByRole('link', { name: 'Jump' })
screen.getByRole('link', { name: 'Missing' })
```

with:

```tsx
screen.getByRole('link', { name: '跳转原文' })
```

Do not change the baseline ordinary-link test: its external, ordinary-fragment, and malformed-fragment labels must remain unchanged.

- [ ] **Step 3: Run the focused frontend test and verify RED**

Run from `frontend/`:

```bash
npm test -- --run src/components/SummaryMarkdown.test.tsx
```

Expected: FAIL because valid article anchors still expose their stored labels instead of `跳转原文`.

- [ ] **Step 4: Implement the minimal visible-label override**

In `SummaryMarkdown.tsx`, define the canonical label near the other module constants:

```tsx
const ARTICLE_LINK_LABEL = '跳转原文'
```

In `SummaryLink`, replace only the rendered children:

```tsx
    <a href={href} onClick={handleClick} onAuxClick={handleAuxClick} {...rest} className={className}>
      {targetID ? ARTICLE_LINK_LABEL : children}
      {targetID && (
        <span className="summary-article-link-icon" aria-hidden="true">⌖</span>
      )}
    </a>
```

Do not change target parsing, event handlers, href, icon markup, or highlight timing.

- [ ] **Step 5: Run focused and related frontend tests**

Run from `frontend/`:

```bash
npm test -- --run src/components/SummaryMarkdown.test.tsx src/util/articleAnchors.test.ts
```

Expected: both files PASS; all valid anchors have the canonical accessible name and strict parsing remains unchanged.

- [ ] **Step 6: Inspect and commit Task 1**

```bash
git diff --check
git diff -- frontend/src/components/SummaryMarkdown.tsx frontend/src/components/SummaryMarkdown.test.tsx
git add frontend/src/components/SummaryMarkdown.tsx frontend/src/components/SummaryMarkdown.test.tsx
git commit -m "feat(reader): rename article jump links"
```

Expected: only the label normalization and its tests are committed.

### Task 2: Update the default detailed-summary prompt label

**Files:**
- Modify: `backend/internal/ai/article_anchors.go`
- Test: `backend/internal/ai/summarizer_anchor_prompt_test.go`

- [ ] **Step 1: Change prompt expectations and add a stale-label guard**

Replace both active expected Markdown examples in `summarizer_anchor_prompt_test.go`:

```go
"[跳转原文](#article-section-NNN)",
```

After the required-substring loop in `TestDetailedArticleAnchorInstructionRequiresBoundedLinks`, add:

```go
if strings.Contains(detailedArticleAnchorInstruction, "[查看原文](") {
    t.Fatalf("instruction contains stale article-link label:\n%s", detailedArticleAnchorInstruction)
}
```

- [ ] **Step 2: Run the focused backend test and verify RED**

Run from `backend/`:

```bash
go test ./internal/ai -run 'TestBuildDetailedArticlePromptInputAnnotatesAddressableContentAndExplainsLinkRules|TestDetailedArticleAnchorInstructionRequiresBoundedLinks' -count=1
```

Expected: FAIL because the instruction and example still contain `查看原文`.

- [ ] **Step 3: Replace the active prompt label and example**

In `article_anchors.go`, change:

```go
detailedArticleAnchorLinkExample = "示例：[跳转原文](#article-section-003)"
```

Within `detailedArticleAnchorInstruction`, replace the active Markdown label with:

```text
[跳转原文](#article-section-NNN)
```

Do not change the numerical bounds, eligibility rules, grouping language, or `fmt.Sprintf` arguments.

- [ ] **Step 4: Run focused and package backend tests**

Run from `backend/`:

```bash
go test ./internal/ai -count=1
```

Expected: `internal/ai` PASS.

- [ ] **Step 5: Inspect and commit Task 2**

```bash
gofmt -w internal/ai/article_anchors.go internal/ai/summarizer_anchor_prompt_test.go
git diff --check
git diff -- internal/ai/article_anchors.go internal/ai/summarizer_anchor_prompt_test.go
git add internal/ai/article_anchors.go internal/ai/summarizer_anchor_prompt_test.go
git commit -m "fix(ai): rename detailed summary jump links"
```

Expected: only active prompt wording and its tests change.

### Task 3: Full regression verification

**Files:**
- Verify: `frontend/src/components/SummaryMarkdown.tsx`
- Verify: `frontend/src/components/SummaryMarkdown.test.tsx`
- Verify: `backend/internal/ai/article_anchors.go`
- Verify: `backend/internal/ai/summarizer_anchor_prompt_test.go`

- [ ] **Step 1: Run the complete frontend test suite**

Run from `frontend/`:

```bash
npm test -- --run
```

Expected: all frontend test files and tests PASS.

- [ ] **Step 2: Build the frontend**

```bash
npm run build
```

Expected: TypeScript and Vite finish with exit code 0.

- [ ] **Step 3: Run the complete backend test suite**

Run from `backend/`:

```bash
go test ./...
```

Expected: all backend packages PASS.

- [ ] **Step 4: Verify scope and repository state**

From the worktree root:

```bash
git status --short
git diff --check master..HEAD
git diff --name-only master..HEAD
```

Expected: clean worktree; no whitespace errors; changes are limited to the design/plan and four implementation/test files listed above.

- [ ] **Step 5: Request final review before integration**

Review `master..HEAD` against `docs/superpowers/specs/2026-08-24-summary-anchor-link-label-design.md`. Fix all Critical and Important findings, rerun focused frontend/backend tests plus the frontend build, and commit corrections before requesting integration.
