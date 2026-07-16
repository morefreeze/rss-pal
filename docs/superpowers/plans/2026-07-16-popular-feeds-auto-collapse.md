# Popular Feeds Auto-Collapse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the subscriptions page keep Popular Feeds expanded for the first 72 hours seen in a browser, then default the whole section to a manually reversible collapsed row.

**Architecture:** Put the timestamp parsing and storage fallback policy in a small pure TypeScript utility so every time boundary and storage failure can be tested without a browser. `FeedListPage` will use that utility as a lazy state initializer and conditionally render the existing category groups beneath one accessible section toggle; the existing category state and feed-preview flow stay untouched.

**Tech Stack:** React 18, TypeScript, browser `localStorage`, existing no-dependency Node/TypeScript test pattern, Vite, Docker Compose

---

## File structure

| File | Change | Responsibility |
| --- | --- | --- |
| `frontend/src/utils/popularFeedsVisibility.ts` | Create | Own the storage key, 72-hour threshold, timestamp validation, and safe initial expanded/collapsed decision. |
| `frontend/test/popularFeedsVisibility.test.ts` | Create | Cover new, recent, expired, exact-boundary, invalid, future, and failing-storage cases. |
| `frontend/test/popularFeedsSection.test.cjs` | Create | Guard the `FeedListPage` state initializer, accessible whole-section toggle, conditional rendering, and preserved category/preview wiring. |
| `frontend/src/pages/FeedListPage.tsx` | Modify | Add whole-section state and toggle while leaving the existing category folds and recommendation actions intact. |

No backend, database, Worker, RSSHub, recommendation-data, or CSS file changes are needed.

### Task 1: Implement and test the visibility policy

**Files:**
- Create: `frontend/test/popularFeedsVisibility.test.ts`
- Create: `frontend/src/utils/popularFeedsVisibility.ts`

- [ ] **Step 1: Write the failing policy test**

Create `frontend/test/popularFeedsVisibility.test.ts` with the complete boundary and failure matrix:

```ts
import {
  getInitialPopularFeedsExpanded,
  POPULAR_FEEDS_AUTO_COLLAPSE_MS,
  POPULAR_FEEDS_FIRST_SEEN_KEY,
} from '../src/utils/popularFeedsVisibility'

function assertEqual<T>(actual: T, expected: T, label: string) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)}, got ${String(actual)}`)
  }
}

class MemoryStorage {
  private values = new Map<string, string>()

  constructor(initialValue?: string) {
    if (initialValue !== undefined) {
      this.values.set(POPULAR_FEEDS_FIRST_SEEN_KEY, initialValue)
    }
  }

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
  }
}

const now = 2_000_000_000_000

const newBrowser = new MemoryStorage()
assertEqual(
  getInitialPopularFeedsExpanded(now, newBrowser),
  true,
  'new browser starts expanded',
)
assertEqual(
  newBrowser.getItem(POPULAR_FEEDS_FIRST_SEEN_KEY),
  String(now),
  'new browser records first seen time',
)

const recent = new MemoryStorage(String(now - 71 * 60 * 60 * 1000))
assertEqual(getInitialPopularFeedsExpanded(now, recent), true, '71 hours stays expanded')
assertEqual(
  recent.getItem(POPULAR_FEEDS_FIRST_SEEN_KEY),
  String(now - 71 * 60 * 60 * 1000),
  'valid first seen time is not overwritten',
)

const exactBoundary = new MemoryStorage(String(now - POPULAR_FEEDS_AUTO_COLLAPSE_MS))
assertEqual(
  getInitialPopularFeedsExpanded(now, exactBoundary),
  false,
  'exactly 72 hours starts collapsed',
)

const expired = new MemoryStorage(String(now - 73 * 60 * 60 * 1000))
assertEqual(getInitialPopularFeedsExpanded(now, expired), false, '73 hours starts collapsed')

for (const invalidValue of ['not-a-number', '0', '-1', String(now + 1)]) {
  const invalid = new MemoryStorage(invalidValue)
  assertEqual(
    getInitialPopularFeedsExpanded(now, invalid),
    true,
    `invalid value ${invalidValue} resets expanded`,
  )
  assertEqual(
    invalid.getItem(POPULAR_FEEDS_FIRST_SEEN_KEY),
    String(now),
    `invalid value ${invalidValue} records a new first seen time`,
  )
}

const unreadableStorage = {
  getItem(): string | null {
    throw new Error('storage blocked')
  },
  setItem(): void {},
}
assertEqual(
  getInitialPopularFeedsExpanded(now, unreadableStorage),
  true,
  'read failure falls back to expanded',
)

const unwritableStorage = {
  getItem(): string | null {
    return null
  },
  setItem(): void {
    throw new Error('storage quota exceeded')
  },
}
assertEqual(
  getInitialPopularFeedsExpanded(now, unwritableStorage),
  true,
  'write failure falls back to expanded',
)

console.log('popularFeedsVisibility tests passed')
```

- [ ] **Step 2: Run the policy test to verify it fails**

Run from `frontend/`:

```bash
./node_modules/.bin/tsc --module commonjs --target ES2020 --moduleResolution node --skipLibCheck --outDir /tmp/rss-pal-popular-feeds-test test/popularFeedsVisibility.test.ts src/utils/popularFeedsVisibility.ts
```

Expected: FAIL because `src/utils/popularFeedsVisibility.ts` does not exist yet (for example, `TS6053: File ... not found`).

- [ ] **Step 3: Implement the minimal policy utility**

Create `frontend/src/utils/popularFeedsVisibility.ts`:

```ts
export const POPULAR_FEEDS_FIRST_SEEN_KEY = 'rsspal:popular-feeds:first-seen-at'
export const POPULAR_FEEDS_AUTO_COLLAPSE_MS = 3 * 24 * 60 * 60 * 1000

export interface PopularFeedsStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

export function getInitialPopularFeedsExpanded(
  now = Date.now(),
  storage?: PopularFeedsStorage,
): boolean {
  try {
    const targetStorage = storage ?? window.localStorage
    const rawFirstSeenAt = targetStorage.getItem(POPULAR_FEEDS_FIRST_SEEN_KEY)

    if (rawFirstSeenAt === null) {
      targetStorage.setItem(POPULAR_FEEDS_FIRST_SEEN_KEY, String(now))
      return true
    }

    const firstSeenAt = Number(rawFirstSeenAt)
    if (!Number.isFinite(firstSeenAt) || firstSeenAt <= 0 || firstSeenAt > now) {
      targetStorage.setItem(POPULAR_FEEDS_FIRST_SEEN_KEY, String(now))
      return true
    }

    return now - firstSeenAt < POPULAR_FEEDS_AUTO_COLLAPSE_MS
  } catch {
    return true
  }
}
```

The storage lookup stays inside `try`, so browsers that deny access to `localStorage` also take the safe expanded fallback.

- [ ] **Step 4: Run the policy test and verify it passes**

Run from `frontend/`:

```bash
./node_modules/.bin/tsc --module commonjs --target ES2020 --moduleResolution node --skipLibCheck --outDir /tmp/rss-pal-popular-feeds-test test/popularFeedsVisibility.test.ts src/utils/popularFeedsVisibility.ts
node /tmp/rss-pal-popular-feeds-test/test/popularFeedsVisibility.test.js
```

Expected final line:

```text
popularFeedsVisibility tests passed
```

- [ ] **Step 5: Verify the utility under the application TypeScript configuration**

Run from `frontend/`:

```bash
npm run build
```

Expected: `tsc` and `vite build` exit 0 and Vite reports a completed production build.

- [ ] **Step 6: Commit the tested policy utility**

```bash
git add frontend/src/utils/popularFeedsVisibility.ts frontend/test/popularFeedsVisibility.test.ts
git commit -m "feat: derive popular feeds visibility age"
```

Expected: one commit containing only the utility and its test.

### Task 2: Wire the whole-section toggle into the subscriptions page

**Files:**
- Create: `frontend/test/popularFeedsSection.test.cjs`
- Modify: `frontend/src/pages/FeedListPage.tsx:1-4`
- Modify: `frontend/src/pages/FeedListPage.tsx:85-87`
- Modify: `frontend/src/pages/FeedListPage.tsx:446-487`

- [ ] **Step 1: Write the failing page-wiring guard**

Create `frontend/test/popularFeedsSection.test.cjs`:

```js
const { readFileSync } = require('node:fs')
const { resolve } = require('node:path')

const feedListPage = readFileSync(resolve('src/pages/FeedListPage.tsx'), 'utf8')

for (const [expected, label] of [
  [
    "import { getInitialPopularFeedsExpanded } from '../utils/popularFeedsVisibility'",
    'page should import the visibility initializer',
  ],
  [
    'useState(getInitialPopularFeedsExpanded)',
    'page should lazily derive the initial whole-section state',
  ],
  [
    'aria-expanded={popularFeedsExpanded}',
    'whole-section button should expose its expanded state',
  ],
  [
    'setPopularFeedsExpanded(expanded => !expanded)',
    'whole-section button should toggle only session state',
  ],
  [
    '{popularFeedsExpanded && (',
    'category groups should render only while the whole section is expanded',
  ],
  [
    'setFoldedGroups(s => ({ ...s, [group.category]: !folded }))',
    'existing per-category folding should remain wired',
  ],
  [
    'onClick={() => { setNewUrl(f.url); doPreview(f.url) }}',
    'existing recommendation preview action should remain wired',
  ],
]) {
  if (!feedListPage.includes(expected)) {
    throw new Error(label)
  }
}

console.log('popularFeedsSection wiring test passed')
```

- [ ] **Step 2: Run the wiring guard to verify it fails**

Run from `frontend/`:

```bash
node test/popularFeedsSection.test.cjs
```

Expected: FAIL with `page should import the visibility initializer`.

- [ ] **Step 3: Add the lazy whole-section state**

In `frontend/src/pages/FeedListPage.tsx`, add the utility import after the toast import:

```ts
import { getInitialPopularFeedsExpanded } from '../utils/popularFeedsVisibility'
```

Add the whole-section state immediately before the existing `foldedGroups` state:

```ts
const [popularFeedsExpanded, setPopularFeedsExpanded] = useState(getInitialPopularFeedsExpanded)
const [foldedGroups, setFoldedGroups] = useState<Record<string, boolean>>({})
```

Passing the function to `useState` makes it a lazy initializer, so storage is read only when this page instance initializes. Manual toggles update only React state and never rewrite the first-seen timestamp.

- [ ] **Step 4: Replace the static label with the whole-section toggle**

Replace the current Popular Feeds block in `frontend/src/pages/FeedListPage.tsx` with:

```tsx
{/* Popular feeds — whole section auto-collapses after 3 days; groups remain independently collapsible */}
<div className="mb-2">
  <button
    type="button"
    onClick={() => setPopularFeedsExpanded(expanded => !expanded)}
    className="btn-text btn-sm text-muted mb-1"
    aria-expanded={popularFeedsExpanded}
    aria-controls="popular-feeds-groups"
    style={{
      padding: '2px 0',
      display: 'flex',
      alignItems: 'center',
      gap: 4,
    }}
  >
    <span style={{ fontSize: 10 }}>{popularFeedsExpanded ? '▾' : '▸'}</span>
    <span>热门推荐：</span>
  </button>
  {popularFeedsExpanded && (
    <div id="popular-feeds-groups">
      {POPULAR_FEEDS.map(group => {
        const folded = foldedGroups[group.category] === true
        return (
          <div key={group.category} style={{ marginBottom: 6 }}>
            <button
              type="button"
              onClick={() => setFoldedGroups(s => ({ ...s, [group.category]: !folded }))}
              className="btn-text btn-sm"
              style={{
                padding: '2px 0',
                display: 'flex',
                alignItems: 'center',
                gap: 4,
              }}
            >
              <span>{group.emoji}</span>
              <span>{group.category}</span>
              <span style={{ fontSize: 10 }}>{folded ? '▸' : '▾'}</span>
            </button>
            {!folded && (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, marginTop: 2 }}>
                {group.items.map(f => (
                  <button
                    key={f.url}
                    type="button"
                    className="secondary"
                    style={{ fontSize: 12, padding: '3px 10px' }}
                    title={f.desc}
                    onClick={() => { setNewUrl(f.url); doPreview(f.url) }}
                  >
                    {f.name}
                  </button>
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )}
</div>
```

Do not persist `popularFeedsExpanded` in the click handler. This preserves the specified behavior: refreshing before 72 hours defaults open again, while refreshing after 72 hours defaults closed again.

- [ ] **Step 5: Run the focused policy and wiring tests**

Run from `frontend/`:

```bash
./node_modules/.bin/tsc --module commonjs --target ES2020 --moduleResolution node --skipLibCheck --outDir /tmp/rss-pal-popular-feeds-test test/popularFeedsVisibility.test.ts src/utils/popularFeedsVisibility.ts
node /tmp/rss-pal-popular-feeds-test/test/popularFeedsVisibility.test.js
node test/popularFeedsSection.test.cjs
```

Expected final lines:

```text
popularFeedsVisibility tests passed
popularFeedsSection wiring test passed
```

- [ ] **Step 6: Run the full frontend production build**

Run from `frontend/`:

```bash
npm run build
```

Expected: `tsc` and `vite build` exit 0 with no unused import or JSX/type errors.

- [ ] **Step 7: Commit the page interaction**

```bash
git add frontend/src/pages/FeedListPage.tsx frontend/test/popularFeedsSection.test.cjs
git commit -m "feat: auto-collapse popular feed suggestions"
```

Expected: one commit containing the page wiring and its source guard.

### Task 3: Rebuild and verify the local application

**Files:**
- Verify only; no source changes expected.

- [ ] **Step 1: Rebuild and restart the frontend container**

Run from the repository root:

```bash
docker compose up -d --build frontend
```

Expected: the frontend image builds successfully and the `frontend` service returns to `running` state; existing API, Worker, database, and RSSHub services remain running.

- [ ] **Step 2: Verify the local HTTP endpoint and service state**

```bash
docker compose ps frontend api worker postgres rsshub
curl -fsS -o /dev/null -w '%{http_code}\n' http://localhost/
```

Expected: listed services are running (Postgres remains healthy) and curl prints `200`.

- [ ] **Step 3: Verify a new browser gets an expanded section and a timestamp**

Open `http://localhost/feeds` in the existing signed-in browser. In its developer console run:

```js
localStorage.removeItem('rsspal:popular-feeds:first-seen-at')
location.reload()
```

Expected after reload:

- `热门推荐：` shows `▾`.
- All seven categories are visible.
- `localStorage.getItem('rsspal:popular-feeds:first-seen-at')` returns a numeric millisecond timestamp close to the current time.

- [ ] **Step 4: Verify both sides of the 72-hour boundary**

Set the timestamp to 71 hours ago and reload:

```js
localStorage.setItem('rsspal:popular-feeds:first-seen-at', String(Date.now() - 71 * 60 * 60 * 1000))
location.reload()
```

Expected: the whole section remains expanded.

Then set it to 73 hours ago and reload:

```js
localStorage.setItem('rsspal:popular-feeds:first-seen-at', String(Date.now() - 73 * 60 * 60 * 1000))
location.reload()
```

Expected: only the `▸ 热门推荐：` title row is visible; no category rows or recommendation chips render.

- [ ] **Step 5: Verify manual and existing nested interactions**

From the 73-hour collapsed state:

1. Click `热门推荐：`; expect all categories to appear and the indicator to become `▾`.
2. Click the `视频` category; expect only its four chips to hide and the other categories to remain visible.
3. Click `视频` again; expect its four chips to return.
4. Click one recommendation chip; expect the existing feed Preview flow to start.
5. Click `热门推荐：` again; expect the whole module to collapse.
6. Reload; expect it to default collapsed again because manual toggling did not overwrite the 73-hour timestamp.

- [ ] **Step 6: Verify invalid and future timestamps reset safely**

Run each snippet separately and reload after it:

```js
localStorage.setItem('rsspal:popular-feeds:first-seen-at', 'invalid')
location.reload()
```

```js
localStorage.setItem('rsspal:popular-feeds:first-seen-at', String(Date.now() + 60_000))
location.reload()
```

Expected for both: the section is expanded and the stored value is replaced with a numeric timestamp close to reload time.

- [ ] **Step 7: Run final automated and repository checks**

Run from the repository root:

```bash
cd frontend
./node_modules/.bin/tsc --module commonjs --target ES2020 --moduleResolution node --skipLibCheck --outDir /tmp/rss-pal-popular-feeds-test test/popularFeedsVisibility.test.ts src/utils/popularFeedsVisibility.ts
node /tmp/rss-pal-popular-feeds-test/test/popularFeedsVisibility.test.js
node test/popularFeedsSection.test.cjs
npm run build
cd ..
git diff --check HEAD
git status --short
```

Expected:

- Both focused tests print their pass messages.
- The production build exits 0.
- `git diff --check HEAD` prints nothing.
- `git status --short` shows no tracked modifications; the repository's pre-existing untracked backup files and `rss-pal-course/` may remain and must not be added or deleted.

## Final review checklist

- The first-seen timestamp is written only when missing or invalid.
- Less than 72 hours expands; exactly or more than 72 hours collapses.
- Read and write failures safely render the section expanded.
- The whole-section click handler changes React state only.
- Existing category-level folds and recommendation Preview clicks remain wired.
- No backend, database, Worker, RSSHub, recommendation list, or styling scope was added.
- Local frontend serves the rebuilt production bundle with HTTP 200.
