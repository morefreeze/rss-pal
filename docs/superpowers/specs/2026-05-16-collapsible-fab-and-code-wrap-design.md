# Collapsible action FABs & Code-block wrap toggle

Date: 2026-05-16
Status: approved

## Motivation

Two small reader-UX requests:

1. The 📥 批量抓取 / 💡 转为 link_set floating action buttons sit in the same
   bottom-right slot above the mini-player and tab bar. On mobile they
   eat real estate but the user can't dismiss them. Want a way to retract
   them to the right edge until needed, with per-article memory.
2. Long lines in fenced code blocks force horizontal scroll. Want a
   global "auto-wrap" toggle in reader settings, plus a per-block override
   for the cases where wrapping mangles indentation.

## Feature 1: Collapsible action FABs

### Scope

Applies to two existing fixed-position buttons in `frontend/src/pages/ArticlePage.tsx`:

- `📥 批量抓取` — rendered when `article.links_extendable === true`.
- `💡 转为 link_set` — rendered when `article.link_set_suggested === true`.

Both pin to `position: fixed; right: 24px; bottom: 152px`. At most one is
visible per article. They share the same screen slot, so they share the
same collapse state for any given article id.

### Behavior

**Expanded state** (current pill, plus an inline ✕):

- Add a small ✕ button on the right edge of the pill, inside the pill's
  padding. Tap → collapse.
- On touch devices, the whole pill is horizontally draggable. Drag right
  past 50% of the pill width and release → collapse. Drag less, snap back.
- Down-press without drag still fires the primary action (open batch-fetch
  modal / confirm link_set).

**Collapsed state** — a thin vertical strip flush to the right edge:

- ~8px wide, ~40px tall, anchored at the same `bottom: 152px` as the pill.
- Shows the icon (📥 or 💡) centered just above the strip — visually like a
  small tab sticking out.
- Tap → expand back into the pill. Touch drag-left also expands.
- Same accent color/border as the original pill so the affordance reads
  the same.

**Animation** — 150ms `transform` transition on expand/collapse, no
rebound. Pill translates right off-screen except for the strip; strip
translates left back into the pill. Reduce-motion respects
`prefers-reduced-motion: reduce` and skips the transition.

### Persistence

- `localStorage` key per article: `rsspal:fab-collapsed:<articleId>`,
  value `"1"` for collapsed, absent for expanded.
- Both FABs share the same key for a given article id (only one is shown
  at a time anyway).
- No TTL or eviction. Each entry is a few bytes; storage growth is
  bounded by articles actually visited with one of these CTAs.

### Implementation

- Extract the FAB into a new `frontend/src/components/CollapsibleFab.tsx`:

  ```ts
  type Props = {
    articleId: number
    icon: string         // "📥" or "💡"
    label: string        // "批量抓取" / "转为 link_set"
    variant: 'primary' | 'outline'  // primary = solid accent; outline = bordered
    onActivate: () => void           // pill click + tap-not-drag
    title?: string
  }
  ```

- ArticlePage's two inline `<button>` blocks become two `<CollapsibleFab>`
  usages.
- Drag detection via touch events on the pill: track `touchstart.clientX`,
  set `translateX` on `touchmove`, decide collapse vs snap-back on
  `touchend`. Suppress click after a drag larger than ~6px to avoid
  triggering `onActivate`.
- Persistence helper `getFabCollapsed(id)` / `setFabCollapsed(id, b)` lives
  in the same file — keeps the localStorage key in one place.

### Edge cases

- Logout clears the FAB-collapsed entries the same way it clears other
  reader state. Add a `localStorage` key prefix sweep to `client.ts:logout`.
- If `articleId` is 0 / undefined, skip persistence and always render
  expanded.

## Feature 2: Code-block wrap toggle

### Scope

Affects rendered fenced code blocks inside `MarkdownArticle`. Reader
Settings panel gains one new toggle. Inline `<code>` is unchanged.

### Global setting

`ReaderSettings` (in `frontend/src/hooks/useReaderSettings.ts`) gains:

```ts
codeWrap: boolean   // default false (preserves current overflow-scroll)
```

Persisted under existing `rsspal:reader-settings` key. Surface in
`ReaderSettingsPanel` as "代码块自动换行" with the same control style as the
font toggle.

### Wiring

`MarkdownArticle` is currently memoized with a module-scoped `COMPONENTS`
override map. That's important — re-introducing per-render inline
components would unmount/remount `<img>` and resurrect the 3-fetch bug
(commit `ec395e5`).

Plan:

- Create `frontend/src/components/CodeWrapContext.tsx` exporting
  `CodeWrapContext = React.createContext<boolean>(false)`.
- `ArticlePage` and `ReadingLayout` wrap the `<MarkdownArticle>` render
  site in `<CodeWrapContext.Provider value={reader.codeWrap}>`.
- A new module-scoped `CodeBlock` component is registered as
  `COMPONENTS.pre`. It reads `CodeWrapContext` via `useContext`. Because
  `MarkdownArticle` is `React.memo`'d on props, context updates don't
  re-render the memoized wrapper — only the `CodeBlock` instances
  subscribe and re-render. The COMPONENTS object reference stays stable.

### Per-block override

Each `CodeBlock` keeps a local `useState<boolean | null>(null)`:

- `null` → follow context (global).
- `true` / `false` → forced wrapped / unwrapped, ignoring global.

The override is session-only and tied to the specific React element
instance — reloading the article, navigating away and back, or even
switching reading mode all reset the override to `null`. Persisting
per-block needs stable code-block IDs which don't exist in markdown
source, so we don't try.

UI:

- Small icon button anchored top-right inside the `<pre>` wrapper, with
  small inset (8px from top and right).
- Hover-only opacity on desktop (`:hover` on the wrapper bumps the button
  to opacity 1); always-visible on touch devices (media query
  `(hover: none)`).
- Icon: `↵` when current effective state is wrapped, `→` when not.
- aria-label: "切换换行" plus state.

### Styling

```css
.code-block-wrap { position: relative; }
.code-block-wrap pre {
  white-space: pre;
  overflow-x: auto;
  word-break: normal;
}
.code-block-wrap[data-wrap="true"] pre {
  white-space: pre-wrap;
  overflow-x: hidden;
  word-break: break-word;
}
```

The wrapper's `data-wrap` attribute is set from the effective boolean.
Putting it in CSS keeps the `<pre>` itself reusable and inherits
`rehype-highlight`'s `.hljs` class untouched.

### Edge cases

- `<pre>` not produced by a fenced code block (rare, e.g. an HTML `<pre>`
  in the source) still gets the wrapper — that's acceptable; markdown
  raw HTML is uncommon in our articles and the wrap toggle is harmless.
- KaTeX renders into `<span class="katex">`, not `<pre>` — unaffected.

## Out of scope

- Persisting per-block override (would need stable IDs in markdown).
- A keyboard shortcut for wrap toggle.
- Wrap toggle for ai-summary code blocks rendered via plain
  `<ReactMarkdown>` without `MarkdownArticle` (summaries rarely contain
  fenced code; can be added later if needed).
- Animating the strip with a spring; flat 150ms is fine.

## Test plan

Manual, on the live container after frontend rebuild:

1. Open an article with `links_extendable=true` — verify the pill shows
   with a ✕. Click ✕ → collapses to the right-edge strip. Refresh → still
   collapsed. Tap the strip → expands. Refresh → still expanded.
2. Same flow on an article with `link_set_suggested=true`.
3. Mobile / iOS Chrome (DevTools touch emulation works as a fallback):
   touch-drag the pill to the right past half its width → collapses.
   Drag less → snaps back.
4. Open an article with a long-line code block. Reader Settings →
   toggle 代码块自动换行 on. Lines wrap, horizontal scroll gone. Toggle off →
   scrolls again.
5. With global wrap off, tap a single code block's `↵` button → that block
   wraps; other blocks unchanged. Reload → block back to global default.
6. Verify image-fetch behavior is unchanged (no 3× regression) by
   reopening an image-heavy article and watching network: each `<img>`
   should fire exactly one `/api/proxy/image` request.
