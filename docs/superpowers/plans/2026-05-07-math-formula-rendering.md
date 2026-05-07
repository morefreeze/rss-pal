# Math Formula Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render LaTeX math in articles via KaTeX, and strip the Unicode "shadow" duplicate that Jina Reader appends after each `$math$` expression so it doesn't appear next to the rendered formula.

**Architecture:** A single character-walking algorithm (`stripJinaMathShadow` / `stripMathShadow`) implemented twice — once in Go for the server-side Jina post-process and once in TS for client-side render-time cleanup of legacy data. KaTeX is wired into the existing react-markdown pipeline via `remark-math` + `rehype-katex`.

**Tech Stack:** Go 1.24 (server), React 18 + react-markdown 10 + remark-math + rehype-katex + katex (frontend).

**Spec:** `docs/superpowers/specs/2026-05-07-math-formula-rendering-design.md`

---

## File Structure

**Backend (Go):**
- Modify: `backend/internal/rss/content.go` — add `stripJinaMathShadow` plus three private helpers (`mathBodyQualifies`, `scanMathShadow`, `isMathSignalRune`); call from `jinaRequest`.
- Modify: `backend/internal/rss/content_test.go` — add `TestStripJinaMathShadow`.

**Frontend (TS):**
- Create: `frontend/src/util/mathShadow.ts` — exports `stripMathShadow(md: string): string`.
- Modify: `frontend/src/components/MarkdownArticle.tsx` — apply `stripMathShadow` to `source`; add `remark-math` + `rehype-katex` plugins; import KaTeX CSS.
- Modify: `frontend/package.json` (via `npm install`) — add `remark-math`, `rehype-katex`, `katex`.

No new files server-side. One new file frontend-side. No DB changes.

---

## Detection Algorithm Reference

This algorithm is implemented twice (Go + TS). Both implementations share these exact rules:

1. **Find a math span.** Walk the string. When a `$` is encountered, scan forward (without crossing `\n`) for the next `$`. If found, the body is the slice between them.
2. **Qualify.** The body must contain at least one of `\` `{` `}` `_` `^`. Otherwise treat the `$` as a literal character (advance one) — this protects prose like `$5 burger`.
3. **Emit `$body$` unchanged** to the output.
4. **Scan shadow** starting at the position after the closing `$`:
   - Stop at `\n` or end of string.
   - On an ASCII letter: peek the consecutive ASCII-letter run. If length ≥ 3, stop (English word). Else consume the whole run.
   - Track whether any consumed character is a "signal rune" (see ranges below).
   - Otherwise consume the character.
5. **Trim trailing space/tab** from the consumed range.
6. **Decide:** if the consumed range contained at least one signal rune, drop it from the output. Otherwise keep it (a pure-ASCII trailing string like `x=3` is left alone — KaTeX renders the formula and the duplicate text remains as a small cosmetic redundancy, but no English prose is lost).

**Signal rune ranges (any one qualifies):**

| Range | Purpose |
|---|---|
| U+200B | Zero-width space (Jina's fraction/radical artefact) |
| U+2212 | Unicode minus sign |
| U+00A0–U+00FF | Latin-1 supplement (`±`, `·`, `÷`, `×`, `°`, etc.) |
| U+2200–U+23FF | Mathematical Operators block (`≠`, `≤`, `≥`, `⇒`, `√`, …) |
| U+2A00–U+2AFF | Supplemental Mathematical Operators |
| U+2070–U+209F | Sub/superscripts |
| U+0391–U+03C9 | Greek letters |

---

## Task 1: `stripJinaMathShadow` + tests (Go)

**Files:**
- Modify: `backend/internal/rss/content.go` (append helpers and main function)
- Modify: `backend/internal/rss/content_test.go` (append table test)

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/rss/content_test.go`:

```go
func TestStripJinaMathShadow(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no math",
			in:   "Just plain text with no math.",
			want: "Just plain text with no math.",
		},
		{
			name: "price not math",
			in:   "a $5 burger costs $5",
			want: "a $5 burger costs $5",
		},
		{
			name: "shadow with unicode minus",
			in:   "consider $x - 1$x−1 must also satisfy",
			want: "consider $x - 1$ must also satisfy",
		},
		{
			name: "shadow with zero-width space",
			in:   "so $\\sqrt{3 + 7} = \\sqrt{10}$3+7​=10​,3-1=2\nnext line",
			want: "so $\\sqrt{3 + 7} = \\sqrt{10}$\nnext line",
		},
		{
			name: "shadow before end of line",
			in:   "and $\\sqrt{10} \\neq 2$10​=2\nmore",
			want: "and $\\sqrt{10} \\neq 2$\nmore",
		},
		{
			name: "pure ascii shadow kept",
			in:   "we get $x = 3$x=3 is the answer",
			want: "we get $x = 3$x=3 is the answer",
		},
		{
			name: "fraction shadow",
			in:   "result $x = \\frac{3 \\pm \\sqrt{33}}{2}$x=2 3±33​​\nend",
			want: "result $x = \\frac{3 \\pm \\sqrt{33}}{2}$\nend",
		},
		{
			name: "no closing dollar",
			in:   "stray $5 dollar bill",
			want: "stray $5 dollar bill",
		},
		{
			name: "newline inside dollars",
			in:   "$x \nstuff$ shadow",
			want: "$x \nstuff$ shadow",
		},
		{
			name: "multiple math on one line",
			in:   "$x \\geq 1$x≥1, valid for $x = 3$x=3.",
			want: "$x \\geq 1$, valid for $x = 3$x=3.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stripJinaMathShadow(tc.in)
			if got != tc.want {
				t.Errorf("stripJinaMathShadow(%q)\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test, confirm it fails**

```
docker run --rm -v /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images:/work -w /work/backend golang:1.24 go test ./internal/rss/ -run TestStripJinaMathShadow -v
```

Expected: FAIL with `undefined: stripJinaMathShadow`.

- [ ] **Step 3: Append implementation to `backend/internal/rss/content.go`**

```go
// stripJinaMathShadow removes the Unicode "shadow" that Jina Reader appends
// immediately after each LaTeX math span in scraped markdown. See
// docs/superpowers/specs/2026-05-07-math-formula-rendering-design.md for the
// detection rules. Pure function: idempotent and safe on inputs without math.
func stripJinaMathShadow(md string) string {
	r := []rune(md)
	var b strings.Builder
	b.Grow(len(md))
	i := 0
	for i < len(r) {
		if r[i] != '$' {
			b.WriteRune(r[i])
			i++
			continue
		}
		// Look for a closing $ on the same line.
		j := i + 1
		for j < len(r) && r[j] != '$' && r[j] != '\n' {
			j++
		}
		if j >= len(r) || r[j] == '\n' {
			b.WriteRune(r[i])
			i++
			continue
		}
		body := r[i+1 : j]
		if !mathBodyQualifies(body) {
			b.WriteRune(r[i])
			i++
			continue
		}
		// Emit $body$ verbatim.
		b.WriteString(string(r[i : j+1]))
		i = j + 1
		// Scan and possibly drop the shadow that follows.
		end, hasSignal := scanMathShadow(r, i)
		if hasSignal {
			i = end
		}
	}
	return b.String()
}

func mathBodyQualifies(body []rune) bool {
	for _, c := range body {
		switch c {
		case '\\', '{', '}', '_', '^':
			return true
		}
	}
	return false
}

func scanMathShadow(r []rune, start int) (end int, hasSignal bool) {
	end = start
	for end < len(r) {
		c := r[end]
		if c == '\n' {
			break
		}
		if isAsciiLetterRune(c) {
			k := end
			for k < len(r) && isAsciiLetterRune(r[k]) {
				k++
			}
			if k-end >= 3 {
				break
			}
			end = k
			continue
		}
		if isMathSignalRune(c) {
			hasSignal = true
		}
		end++
	}
	for end > start && (r[end-1] == ' ' || r[end-1] == '\t') {
		end--
	}
	return end, hasSignal
}

func isAsciiLetterRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isMathSignalRune(r rune) bool {
	switch {
	case r == 0x200B,
		r == 0x2212:
		return true
	case r >= 0x00A0 && r <= 0x00FF,
		r >= 0x2200 && r <= 0x23FF,
		r >= 0x2A00 && r <= 0x2AFF,
		r >= 0x2070 && r <= 0x209F,
		r >= 0x0391 && r <= 0x03C9:
		return true
	}
	return false
}
```

(`strings` is already imported at the top of `content.go`. No new imports.)

- [ ] **Step 4: Run test, confirm it passes**

```
docker run --rm -v /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images:/work -w /work/backend golang:1.24 go test ./internal/rss/ -run TestStripJinaMathShadow -v
```

Expected: PASS — all 10 sub-tests green. Then run the full package to confirm no regression:

```
docker run --rm -v /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images:/work -w /work/backend golang:1.24 go test ./internal/rss/ -v 2>&1 | tail -30
```

Expected: every test passes including the existing `TestIsAvatarImg`, `TestStripAvatars`, `TestFetchContentFromReader_*`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/rss/content.go backend/internal/rss/content_test.go
git commit -m "feat(rss): stripJinaMathShadow drops Unicode rendering after \$math\$

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Wire `stripJinaMathShadow` into `jinaRequest` (Go)

**Files:**
- Modify: `backend/internal/rss/content.go` (`jinaRequest`, around line 213)

- [ ] **Step 1: Locate the current trailing block of `jinaRequest`**

The current code (around line 211–221) reads:

```go
	body, err := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(string(body))
	if len(content) > 50000 {
		content = content[:50000] + "..."
	}
	return content, nil
}
```

- [ ] **Step 2: Replace it with**

```go
	body, err := io.ReadAll(io.LimitReader(resp.Body, 200_000))
	if err != nil {
		return "", err
	}

	content := stripJinaMathShadow(strings.TrimSpace(string(body)))
	if len(content) > 50000 {
		content = content[:50000] + "..."
	}
	return content, nil
}
```

(One-line change: wrap `strings.TrimSpace(string(body))` with `stripJinaMathShadow(...)`. The length cap follows the strip so the truncation accounts for the shorter post-strip content.)

- [ ] **Step 3: Run the rss package tests**

```
docker run --rm -v /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images:/work -w /work/backend golang:1.24 go test ./internal/rss/ -v 2>&1 | tail -20
```

Expected: PASS. (No new test for this wiring — `stripJinaMathShadow` is unit-tested; the integration path through `jinaRequest` requires hitting the real Jina service, which is out of scope.)

- [ ] **Step 4: Commit**

```bash
git add backend/internal/rss/content.go
git commit -m "feat(rss): apply stripJinaMathShadow to fetchViaJina output

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `stripMathShadow` util (TS)

**Files:**
- Create: `frontend/src/util/mathShadow.ts`

No frontend test runner is configured — verification is by mirroring the Go algorithm exactly and visual smoke testing in Task 5.

- [ ] **Step 1: Verify util directory state**

```bash
ls /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images/frontend/src/util/ 2>/dev/null || echo "dir absent"
```

If absent, the Write tool will create the directory automatically when the file is written.

- [ ] **Step 2: Create the file**

Write `frontend/src/util/mathShadow.ts` with the following exact content:

```ts
// Mirror of backend/internal/rss/content.go::stripJinaMathShadow.
// Drops the Unicode "shadow" Jina Reader appends after each $math$ span.
// Kept here for client-side cleanup of articles already stored in the DB
// before the server-side strip was added. See:
//   docs/superpowers/specs/2026-05-07-math-formula-rendering-design.md

const SIGNAL_RANGES: ReadonlyArray<readonly [number, number]> = [
  [0x200b, 0x200b],
  [0x2212, 0x2212],
  [0x00a0, 0x00ff],
  [0x2200, 0x23ff],
  [0x2a00, 0x2aff],
  [0x2070, 0x209f],
  [0x0391, 0x03c9],
]

function isSignalCp(cp: number): boolean {
  for (const [lo, hi] of SIGNAL_RANGES) {
    if (cp >= lo && cp <= hi) return true
  }
  return false
}

function isAsciiLetterCp(cp: number): boolean {
  return (cp >= 0x41 && cp <= 0x5a) || (cp >= 0x61 && cp <= 0x7a)
}

function mathBodyQualifies(body: string): boolean {
  for (const c of body) {
    if (c === '\\' || c === '{' || c === '}' || c === '_' || c === '^') {
      return true
    }
  }
  return false
}

export function stripMathShadow(md: string): string {
  const r = Array.from(md)
  let out = ''
  let i = 0
  while (i < r.length) {
    if (r[i] !== '$') {
      out += r[i]
      i++
      continue
    }
    let j = i + 1
    while (j < r.length && r[j] !== '$' && r[j] !== '\n') j++
    if (j >= r.length || r[j] === '\n') {
      out += r[i]
      i++
      continue
    }
    const body = r.slice(i + 1, j).join('')
    if (!mathBodyQualifies(body)) {
      out += r[i]
      i++
      continue
    }
    out += r.slice(i, j + 1).join('')
    i = j + 1
    let end = i
    let hasSignal = false
    while (end < r.length) {
      const c = r[end]
      if (c === '\n') break
      const cp = c.codePointAt(0)!
      if (isAsciiLetterCp(cp)) {
        let k = end
        while (k < r.length && isAsciiLetterCp(r[k].codePointAt(0)!)) k++
        if (k - end >= 3) break
        end = k
        continue
      }
      if (isSignalCp(cp)) hasSignal = true
      end++
    }
    while (end > i && (r[end - 1] === ' ' || r[end - 1] === '\t')) end--
    if (hasSignal) i = end
  }
  return out
}
```

- [ ] **Step 3: Type-check**

```bash
cd /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images/frontend && npx tsc --noEmit
```

Expected: exit code 0, no errors.

- [ ] **Step 4: Commit**

```bash
cd /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images
git add frontend/src/util/mathShadow.ts
git commit -m "feat(frontend): stripMathShadow util mirrors server-side algorithm

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: KaTeX integration in `MarkdownArticle.tsx`

**Files:**
- Modify: `frontend/package.json` (via `npm install`)
- Modify: `frontend/package-lock.json` (auto-updated by npm)
- Modify: `frontend/src/components/MarkdownArticle.tsx`

- [ ] **Step 1: Install dependencies**

```bash
cd /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images/frontend
npm install remark-math@^6 rehype-katex@^7 katex@^0.16
```

Expected: clean install, no peer-dep warnings about react-markdown incompatibility. (react-markdown 10 supports remark-math 6 + rehype-katex 7.)

- [ ] **Step 2: Replace `frontend/src/components/MarkdownArticle.tsx`**

Write the full file contents (overwriting current avatar-skip version):

```tsx
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import rehypeHighlight from 'rehype-highlight'
import rehypeKatex from 'rehype-katex'
import 'highlight.js/styles/github.css'
import 'katex/dist/katex.min.css'
import { stripMathShadow } from '../util/mathShadow'

type Props = {
  source: string
}

const AVATAR_ATTR_KEYWORDS = [
  'avatar', 'gravatar', 'profile', 'author',
  'user-pic', 'userpic', 'headshot',
]
const AVATAR_URL_KEYWORDS = [
  'gravatar.com', '/avatar/', '/avatars/',
]

// isAvatarImg mirrors the server-side detector (Signal 1 only — class/id/width
// /height attributes don't survive markdown round-trip, so dimension matching
// is unreachable client-side). Returns true if the image's URL or alt text
// contains any avatar keyword.
function isAvatarImg(src: string | undefined, alt: string | undefined): boolean {
  const url = (src ?? '').toLowerCase()
  for (const kw of AVATAR_URL_KEYWORDS) {
    if (url.includes(kw)) return true
  }
  const altLower = (alt ?? '').toLowerCase()
  if (!altLower) return false
  for (const kw of AVATAR_ATTR_KEYWORDS) {
    if (altLower.includes(kw)) return true
  }
  return false
}

// Rewrites <img src="..."> to go through the backend proxy so hotlink-
// protected sites (WeChat, Zhihu) actually render. Author/profile avatars
// are dropped entirely (see isAvatarImg). LaTeX math via remark-math +
// rehype-katex; Jina Reader's shadow duplicate is removed via stripMathShadow
// before parsing. External links open in a new tab.
export default function MarkdownArticle({ source }: Props) {
  const cleaned = stripMathShadow(source)
  return (
    <div className="markdown-body">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeHighlight, rehypeKatex]}
        components={{
          img: ({ src, alt, ...rest }) => {
            if (isAvatarImg(src, alt)) return null
            const proxied = src
              ? `/api/proxy/image?url=${encodeURIComponent(src)}`
              : undefined
            return (
              <img
                src={proxied}
                alt={alt ?? ''}
                loading="lazy"
                decoding="async"
                style={{ maxWidth: '100%', height: 'auto' }}
                {...rest}
              />
            )
          },
          a: ({ href, children, ...rest }) => (
            <a href={href} target="_blank" rel="noopener noreferrer" {...rest}>
              {children}
            </a>
          ),
        }}
      >
        {cleaned}
      </ReactMarkdown>
    </div>
  )
}
```

- [ ] **Step 3: Type-check**

```bash
cd /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images/frontend && npx tsc --noEmit
```

Expected: exit code 0, no errors.

- [ ] **Step 4: Build (smoke compile of bundle)**

```bash
cd /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images/frontend && npm run build 2>&1 | tail -20
```

Expected: Vite build succeeds; output bundle includes katex CSS. Watch for "Cannot find module" errors that would indicate a missed import.

- [ ] **Step 5: Commit**

```bash
cd /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images
git add frontend/package.json frontend/package-lock.json frontend/src/components/MarkdownArticle.tsx
git commit -m "feat(frontend): KaTeX rendering for LaTeX math + shadow strip wiring

Adds remark-math + rehype-katex to MarkdownArticle and applies
stripMathShadow to the source before rendering, so legacy articles
already in the DB also benefit from the cleanup.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Docker rebuild + smoke verify

**Files:** none modified — verification only.

- [ ] **Step 1: Rebuild api/worker/frontend from the worktree**

```bash
cd /Users/bytedance/mygit/rss-pal/.worktrees/feature/skip-avatar-images
docker-compose -p rss-pal up -d --build api worker frontend 2>&1 | tail -10
```

Expected: build completes, containers recreated, all services Up. (Postgres + rsshub continue running unchanged.)

- [ ] **Step 2: Verify services are healthy**

```bash
docker ps --format 'table {{.Names}}\t{{.Status}}'
```

Expected: `rss-pal-api-1`, `rss-pal-worker-1`, `rss-pal-frontend-1`, `rss-pal-postgres-1`, `rss-pal-rsshub-1` all `Up`.

- [ ] **Step 3: Open article 1107 in the browser**

Navigate to `https://localhost/articles/1107`. (Self-signed cert — accept the warning.) Expected:

- Algebra examples (e.g. `$x = \frac{3 \pm \sqrt{33}}{2}$`) render as proper KaTeX formulas.
- The Unicode shadow text (`x=2 3±33​​`, `x−1`, `3+7​=10​`, etc.) is gone from those lines.
- Paragraph prose around the formulas (`must also satisfy`, `is the only candidate`, etc.) is preserved.
- Avatar-skip behaviour from the previous feature still works — open DevTools → Network, filter `proxy/image`, and confirm no `gravatar` / `/avatar/` URLs are requested.

- [ ] **Step 4: Verify a fresh Jina-scraped article**

Either trigger the worker to re-fetch a math-bearing article, or wait for the cron pass. Open the new article; confirm the shadow is absent in the stored markdown:

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -tAc "SELECT substring(content, 1, 1000) FROM articles ORDER BY id DESC LIMIT 1;" | grep -E '\$|−|​' | head -5
```

If the latest article happens not to contain math, run this against article 1107 to confirm the *client-side* strip is working on legacy data:

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -tAc "SELECT substring(content, 1, 200) FROM articles WHERE id=1107;"
```

The DB content for 1107 will still contain shadows (server-side strip only fires on new fetches) — that's expected; the client renders cleanly because `stripMathShadow` runs at view time.

- [ ] **Step 5: Report findings**

Summarize:
- KaTeX rendering visible? Y/N + which article tested.
- Shadow successfully stripped at view time? Y/N.
- Any prose lost / false-positive regressions? List.
- Avatar-skip regressions? Y/N.

If clean, the feature is ready for merge. Use `superpowers:finishing-a-development-branch` to wrap up.

---

## Self-Review

**Spec coverage:**
- Detection rule (qualify-via-LaTeX-command, signal-rune-required strip) → Task 1.
- Server-side wiring in `fetchViaJina` (specifically the lower-level `jinaRequest`) → Task 2.
- Client-side mirror util → Task 3.
- KaTeX integration in MarkdownArticle.tsx → Task 4.
- Verification: backend test (Task 1 step 4), tsc (Task 3 + 4), build (Task 4), browser smoke (Task 5).

**Placeholder scan:** All steps contain concrete code, exact paths, exact commands. No "TODO" or "implement later" text.

**Type consistency:**
- Go: `stripJinaMathShadow(string) string`, `mathBodyQualifies([]rune) bool`, `scanMathShadow([]rune, int) (int, bool)`, `isAsciiLetterRune(rune) bool`, `isMathSignalRune(rune) bool` — used consistently.
- TS: `stripMathShadow(md: string): string`, helpers private — used as imported in Task 4.
- The TS helpers are local-scoped to `mathShadow.ts`; the Go helpers are package-scoped under `rss/`. Both modules share zero state but identical algorithm shape — tests live with the Go implementation only.
