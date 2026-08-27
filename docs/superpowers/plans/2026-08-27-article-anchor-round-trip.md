# Article Anchor Round-Trip Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render summary references as icon-only anchors and provide an icon-only return anchor at the destination without changing URL hash or browser history.

**Architecture:** `SummaryMarkdown` keeps strict article-link parsing and forward highlighting. A focused DOM controller owns one active source/target pair, renders a fixed semantic return anchor over the target upper-right, and intercepts both directions to use `scrollIntoView`.

**Tech Stack:** React 19, TypeScript, React Markdown, Vitest, Testing Library, CSS.

---

## File Map

- Create `frontend/src/util/articleAnchorRoundTrip.ts`: active trip, return anchor, positioning, focus, and cleanup.
- Create `frontend/src/util/articleAnchorRoundTrip.test.ts`: controller behavior.
- Modify `frontend/src/components/SummaryMarkdown.tsx`: stable source IDs, icon-only source, controller integration.
- Modify `frontend/src/components/SummaryMarkdown.test.tsx`: rendering and round-trip integration.
- Modify `frontend/src/index.css`: compact source and return-anchor styles.

### Task 1: Add the round-trip controller

**Files:**
- Create: `frontend/src/util/articleAnchorRoundTrip.ts`
- Test: `frontend/src/util/articleAnchorRoundTrip.test.ts`

- [ ] **Step 1: Write the failing controller tests**

```ts
import { afterEach, describe, expect, it, vi } from "vitest"
import { clearArticleAnchorRoundTrip, startArticleAnchorRoundTrip } from "./articleAnchorRoundTrip"

afterEach(() => {
  clearArticleAnchorRoundTrip()
  vi.restoreAllMocks()
  document.body.replaceChildren()
})

it("creates a semantic return anchor and returns without changing hash", () => {
  history.replaceState(null, "", "/articles/1")
  const source = document.body.appendChild(document.createElement("a"))
  source.id = "summary-article-source-1"
  const target = document.body.appendChild(document.createElement("p"))
  Object.defineProperty(source, "scrollIntoView", { value: vi.fn() })

  startArticleAnchorRoundTrip(source, target)
  const back = document.querySelector<HTMLAnchorElement>(".article-anchor-return-link")!
  expect(back.textContent).toBe("↩⌖")
  expect(back.getAttribute("href")).toBe("#summary-article-source-1")
  back.click()
  expect(source.scrollIntoView).toHaveBeenCalledWith({ behavior: "smooth", block: "center" })
  expect(location.hash).toBe("")
  expect(document.querySelector(".article-anchor-return-link")).toBeNull()
})
```

- [ ] **Step 2: Run the test and verify RED**

Run: `cd frontend && npm test -- --run src/util/articleAnchorRoundTrip.test.ts`

Expected: FAIL because the module does not exist.

- [ ] **Step 3: Implement the controller**

Expose exactly:

```ts
export function startArticleAnchorRoundTrip(source: HTMLAnchorElement, target: HTMLElement): void
export function clearArticleAnchorRoundTrip(source?: HTMLAnchorElement): void
```

On start, clear the prior trip; create `<a class="article-anchor-return-link" href={"#" + source.id}>↩⌖</a>` with `aria-label` and `title` equal to `跳回 AI 总结`; append it to `document.body`; and position it from `target.getBoundingClientRect()`. Update on passive scroll, resize, and optional `ResizeObserver`, hiding it outside the viewport. On unmodified primary click, call `preventDefault()`, scroll the connected source with reduced-motion-aware `{ behavior, block: "center" }`, focus it for keyboard activation, then clean up. Consume modifier and auxiliary activations without navigation. `clearArticleAnchorRoundTrip(source)` must only clear when that source owns the active trip.

- [ ] **Step 4: Run the controller tests and verify GREEN**

Run: `cd frontend && npm test -- --run src/util/articleAnchorRoundTrip.test.ts`

Expected: PASS with no warnings.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/util/articleAnchorRoundTrip.ts frontend/src/util/articleAnchorRoundTrip.test.ts
git commit -m "feat(frontend): add article anchor round trip"
```

### Task 2: Integrate icon-only anchors

**Files:**
- Modify: `frontend/src/components/SummaryMarkdown.tsx`
- Modify: `frontend/src/components/SummaryMarkdown.test.tsx`
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Change tests first**

Replace the current text expectation and add the round trip:

```ts
const source = screen.getByRole("link", { name: "跳转原文" })
expect(source.textContent).toBe("⌖")
expect(source.id).toMatch(/^summary-article-source-/)
expect(source.getAttribute("href")).toBe("#article-section-001")

const beforeLength = history.length
fireEvent.click(source, { detail: 1 })
const back = screen.getByRole("link", { name: "跳回 AI 总结" })
expect(back.textContent).toBe("↩⌖")
expect(back.getAttribute("href")).toBe(`#${source.id}`)
expect(location.hash).toBe("")
expect(history.length).toBe(beforeLength)
```

Keep existing coverage for missing targets, modifiers, reduced motion, keyboard focus, highlight cleanup, and non-article links. Add consecutive-forward and unmount cleanup assertions so only one return anchor exists.

- [ ] **Step 2: Run SummaryMarkdown tests and verify RED**

Run: `cd frontend && npm test -- --run src/components/SummaryMarkdown.test.tsx`

Expected: FAIL because the source still renders `跳转原文⌖` and creates no return anchor.

- [ ] **Step 3: Implement the React integration**

Import `useEffect`, `useId`, `useRef`, and the controller functions. In `SummaryLink`, create `summary-article-source-${useId().replace(/:/g, "")}` only for strict article links, attach it and a ref to the anchor, and clear only that source trip on unmount. After the existing successful forward scroll and highlight, call `startArticleAnchorRoundTrip(sourceRef.current, target)`. Render strict links with `aria-label="跳转原文"`, `title="跳转原文"`, and only `<span className="summary-article-link-icon" aria-hidden="true">⌖</span>`. Preserve children and attributes for every non-article link.

- [ ] **Step 4: Add compact CSS**

```css
.summary-article-link {
  display: inline-grid; place-items: center;
  width: 1.5em; height: 1.5em; margin-left: .25em;
  border: 1px solid color-mix(in srgb, currentColor 45%, transparent);
  border-radius: 5px; vertical-align: .1em;
}
.summary-article-link-icon { font-size: .82em; line-height: 1; }
.article-anchor-return-link {
  position: fixed; z-index: 30; display: inline-grid; place-items: center;
  min-width: 2.1em; height: 1.75em; padding: 0 .35em;
  border: 1px solid color-mix(in srgb, var(--accent) 55%, var(--border));
  border-radius: 5px; background: var(--surface); color: var(--accent);
  box-shadow: 0 2px 8px rgba(0,0,0,.12); font-size: 12px; line-height: 1;
}
```

- [ ] **Step 5: Run focused integration tests**

Run: `cd frontend && npm test -- --run src/util/articleAnchorRoundTrip.test.ts src/components/SummaryMarkdown.test.tsx test/ArticleVideoDedupe.test.tsx test/TweetCardAnchors.test.tsx`

Expected: all selected files PASS with no DOM-nesting warnings.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/SummaryMarkdown.tsx frontend/src/components/SummaryMarkdown.test.tsx frontend/src/index.css
git commit -m "feat(frontend): add reversible article preview anchors"
```

### Task 3: Full verification

**Files:**
- Verify the five files listed in the File Map.

- [ ] **Step 1: Run all frontend tests**

Run: `cd frontend && npm test -- --run`

Expected: every frontend test passes with no unhandled errors.

- [ ] **Step 2: Build production assets**

Run: `cd frontend && npm run build`

Expected: TypeScript and Vite finish successfully and emit `frontend/dist`.

- [ ] **Step 3: Check the diff**

Run: `git diff --check HEAD~2..HEAD && git status --short`

Expected: no whitespace errors; unrelated backup files remain untouched.

- [ ] **Step 4: Verify the rendered interaction when authenticated article data is available**

Check desktop and mobile widths: `⌖` aligns with summary prose; `↩⌖` sits at the target upper-right; both directions center-scroll; URL and history stay unchanged. If authenticated data is unavailable, report visual verification as partial.

- [ ] **Step 5: Report evidence**

Report focused tests, full-suite count, build result, commit SHAs, exact files changed, and any live-UI boundary. Do not push or deploy unless separately requested.
