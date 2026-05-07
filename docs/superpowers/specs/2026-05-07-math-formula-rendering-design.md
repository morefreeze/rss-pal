# Math Formula Rendering

**Date:** 2026-05-07
**Branch:** `feature/skip-avatar-images` (stacked on top of avatar-skip work)

## Problem

Articles scraped via Jina Reader contain LaTeX math expressions that render as raw `$...$` in the reader, plus an immediately-following Unicode "shadow" of the rendered formula (Jina captures both the source LaTeX and the page's pre-rendered text representation, glued together with no separator).

Example from article 1107 (`https://openai.com/index/gpt-5-5-instant`):

```
- Since $\sqrt{x + 7} \geq 0$x+7​≥0 the right side, $x - 1$x−1 must also satisfy
- $x = - 2$x=−2 gets rejected because it makes $x - 1$x−1 negative.
$x = \frac{3 \pm \sqrt{33}}{2}$x=2 3±33​​
```

Each formula appears twice (LaTeX + shadow) with no whitespace between them, and the LaTeX never renders.

## Goal

1. Render `$inline$` and `$$display$$` LaTeX as math via KaTeX.
2. Strip the duplicate Unicode shadow that follows each formula in Jina-sourced content, server-side AND client-side.

## Out of Scope

- Math support for non-Jina extraction paths. `fetchDirect` produces clean HTML→markdown; if a source contains `<span>$x$</span>` directly, it'll round-trip without shadow contamination — KaTeX will render it. The shadow problem is exclusively Jina's.
- Server-side migration of existing articles. Client-side strip handles legacy data; future Jina scrapes are clean.
- MathML / MathJax / non-LaTeX dialects.

## Detection

### Qualifying as a math span

A `$...$` qualifies as math (and thus triggers shadow detection) only if its body contains at least one of: `\` `{` `}` `_` `^`. This excludes prose like *"a $5 burger"* or *"earn $N per hour"* from being mangled.

### Shadow boundary algorithm

Starting at the position immediately after the closing `$` of a qualifying math span, walk forward consuming characters until a stop condition triggers. Drop the consumed range; everything before and after stays untouched.

**Consume (allowed in shadow):**
- Digits `[0-9]`
- Single ASCII letter (i.e. one letter not part of a longer word — see stop condition)
- Unicode math operator blocks: U+2200–U+23FF, U+2A00–U+2AFF
- U+00B7–U+00FF (Latin-1 supplement; covers `±`, `·`, `÷`, `×`, etc.)
- U+200B (zero-width space — Jina's spacing artefact in fractions/radicals)
- Common math punctuation: `.` `,` `=` `:` `−` (Unicode minus U+2212)
- Whitespace `[ \t]`

**Stop (do NOT consume; end of shadow):**
- `\n` (newline)
- End of string
- A run of 3+ consecutive ASCII letters (looks like an English word). Implementation: when peeking past whitespace or directly, if the next non-whitespace char starts a sequence of ≥3 ASCII letters, stop before any whitespace/letter we'd otherwise consume.
- A character not in the consume list (e.g. `(`, `[`, `*`, etc.) — stop without consuming it. Note that `.` and `,` ARE consumed; this is a known minor: a shadow ending in a sentence period is consumed, dropping the period along with the shadow. Prose punctuation following the shadow text (e.g. `must`) is preserved because the word triggers the 3-letter stop first.

### Examples (validated)

| Input | After strip |
|---|---|
| `$x - 1$x−1 must also satisfy` | `$x - 1$ must also satisfy` |
| `$x = 3$x=3.` | `$x = 3$` (note: trailing `.` consumed) |
| `$\sqrt{...}$3+7​=10​,3−1=2\n` | `$\sqrt{...}$\n` |
| `a $5 burger` | unchanged (no LaTeX command in body) |
| `$x \geq 1$x≥1` | `$x \geq 1$` |
| `$\frac{3 + \sqrt{33}}{2}$2 3+33​​\n` | `$\frac{3 + \sqrt{33}}{2}$\n` |

The minor known case (eaten trailing period) is acceptable. Math expressions ending a sentence are uncommon enough, and the alternative — a more elaborate "look ahead for sentence-end punctuation" — adds complexity for marginal gain.

## A. Server-side (Go)

### New helper

In `backend/internal/rss/content.go`:

```go
// stripJinaMathShadow removes the Unicode "shadow" that Jina Reader appends
// after each LaTeX math span in scraped markdown. See the design doc for
// detection rules.
func stripJinaMathShadow(md string) string
```

### Wiring

Call from `fetchViaJina` after the response body is read and trimmed, before the length cap and return. (Specifically: in `jinaRequest`, replace the `content := strings.TrimSpace(string(body))` / length-cap block with `content := stripJinaMathShadow(strings.TrimSpace(string(body)))`, then apply the existing length cap.)

This is the only Jina entry point — `fetchViaJina` calls `jinaRequest` with optional retry — applying the strip at the lowest level covers both pathways.

### Test

`backend/internal/rss/content_test.go`: a table-driven `TestStripJinaMathShadow` with at least the rows from the Examples table above plus a no-op case (markdown without `$`).

## B. Client-side (TS)

### New util

`frontend/src/util/mathShadow.ts`:

```ts
export function stripMathShadow(md: string): string
```

Mirrors the server algorithm character-for-character. No new types, no test runner — accuracy is verified manually via the same fixtures.

### Wiring

In `frontend/src/components/MarkdownArticle.tsx`, apply `stripMathShadow(source)` before passing to `<ReactMarkdown>`. The avatar-skip behaviour added earlier this session is preserved.

## C. KaTeX integration (frontend)

### Dependencies

Add to `frontend/package.json` via `npm install`:

- `remark-math` (^6.0.0)
- `rehype-katex` (^7.0.0)
- `katex` (^0.16.x) — peer dep of rehype-katex

These are React-friendly, actively maintained, and tree-shake cleanly.

### Renderer config

In `MarkdownArticle.tsx`:

- Add to `remarkPlugins`: `remarkMath`
- Add to `rehypePlugins`: `rehypeKatex`
- Import `katex/dist/katex.min.css` at the top of the file (it's compact, no per-formula network fetch).

## Acceptance Criteria

1. `cd backend && go test ./internal/rss/...` passes including the new `TestStripJinaMathShadow` with all rows green.
2. `cd frontend && npx tsc --noEmit` passes.
3. After Docker rebuild, opening `https://localhost/articles/1107` renders the algebra examples as proper KaTeX, with no Unicode shadow text remaining.
4. A prose article containing literal `$5` (or any `$<digits>` pattern lacking LaTeX commands) is rendered unchanged.
5. Existing avatar-skip tests still pass; existing `TestFetchContentFromReader_PreservesImage` still passes.
