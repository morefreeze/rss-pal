# Mobile Adaptation (iPhone + iPad) — Design

**Date:** 2026-05-12
**Status:** Design

## 1. Goal

Adapt the entire RSS Pal frontend for mobile and tablet usage, prioritizing iOS Chrome / Safari WebKit. Deliver a polished experience on the reading-core surface (article list, article detail, saved, login, reading mode, MiniPlayer) and ensure all other pages remain usable (no overflow, controls reachable) on small screens without deep customization.

## 2. Non-Goals

- Native iOS app or PWA install flow (out of scope).
- Android-specific tuning (we ship the same CSS; iOS WebKit is the design target).
- Desktop layout changes (> 1024 px experience is unchanged).
- Adding a CSS framework (Tailwind, etc.) — we extend the existing handwritten CSS.
- New features. This is purely UX/layout adaptation of existing functionality.

## 3. Breakpoints

Three tiers, driven by `matchMedia`:

| Tier      | Range            | Layout                                        |
| --------- | ---------------- | --------------------------------------------- |
| `phone`   | ≤ 640 px         | Single column, bottom tab nav, tight spacing  |
| `tablet`  | 641 – 1024 px    | Bottom tab nav, can use 2-col where it helps  |
| `desktop` | > 1024 px        | Current desktop layout — top nav, multi-col   |

iPad in landscape (≥ 1180 px) lands in `desktop`. iPad in portrait (768–834 px) lands in `tablet` and gets the bottom-tab "App style".

## 4. Architecture

### 4.1 New CSS strategy

Extend `frontend/src/index.css` with two media blocks (in addition to the existing `≤ 640 px` block):

```css
@media (max-width: 1024px) { /* tablet + phone shared rules */ }
@media (max-width: 640px)  { /* phone-only refinements */ }
```

Rule of thumb: most layout adjustments (single-column reflow, larger touch targets, full-width inputs) go in the shared `≤ 1024px` block. Only iPhone-specific tightening (smaller paddings, hiding non-essential chrome) goes in the `≤ 640px` block.

### 4.2 New hook: `useBreakpoint()`

`frontend/src/hooks/useBreakpoint.ts` — returns `'phone' | 'tablet' | 'desktop'`. Subscribes to two `matchMedia` queries; updates on resize and orientation change. Used **only** where the React tree differs structurally (not for visual tweaks — those stay in CSS).

```ts
export function useBreakpoint(): 'phone' | 'tablet' | 'desktop'
```

Used in:
- `Layout.tsx` — render `<MobileTabBar />` when not desktop
- `SavedPage` — render `<SavedTagChipBar />` instead of `<SavedTagSidebar />` on phone
- `ArticlePage` — show `ReaderSettingsPanel` as a bottom sheet (phone) vs popover (tablet/desktop)
- `ArticleListPage` — show the secondary filter set as a bottom sheet on phone

### 4.3 New hook: `useReadingChrome()`

`frontend/src/hooks/useReadingChrome.ts` — manages chrome visibility inside reading mode.

State: `chromeVisible: boolean` (default `false` on entry).

Behavior:
- **Tap article center** (single click on the `.reading-article` body, not on a link/button) → toggle visible.
- **Scroll up by ≥ 30 px** → show. **Scroll down by ≥ 30 px** → hide.
- **Reach top of page** → show (always reveal at top).
- Adds/removes `body.reading-mode-active` and `body.reading-chrome-visible` classes; CSS uses those to slide header + bottom tab in/out with `transform: translateY()`.

The hook owns the body classes. The existing dead CSS rule `body.reading-mode-active #root > div > header { display: none }` is replaced with a smoother `transform` transition keyed on `body.reading-mode-active:not(.reading-chrome-visible)`.

### 4.4 New component: `MobileTabBar`

`frontend/src/components/MobileTabBar.tsx`. Rendered inside `Layout.tsx` for `phone` and `tablet` breakpoints only.

Visual: fixed bottom, full width, height 56 px, with `padding-bottom: env(safe-area-inset-bottom)` so the iOS home indicator does not overlap.

Items:

| Icon | Label | Route               |
| ---- | ----- | ------------------- |
| 📰    | 文章   | `/articles`         |
| ⭐    | 网摘   | `/saved`            |
| ✨    | 推荐   | `/recommended`      |
| ⋯    | 更多   | opens `<MoreSheet>` |

"更多" opens a new `<MoreSheet>` component — a full-height bottom sheet listing: 订阅 / 周刊 / 洞察 / 统计 / 设置 / 登出. (No new route; the sheet is a `position: fixed` overlay anchored to the bottom, with a translucent backdrop. Tapping a row navigates and closes.)

Active tab: shown by `aria-current="page"`, styled with `color: var(--accent)` + a small top bar.

Unread badge: piped through from `Layout`'s existing `getUnreadCount` polling — shown on the `文章` tab (same logic as the desktop nav).

### 4.5 Reading mode chrome behavior

Reading mode is triggered when `useReaderSettings().mode === 'reading'`. In that mode:

1. `useReadingChrome()` sets `body.reading-mode-active` on enter, clears on exit.
2. CSS hides the top header AND the bottom tab bar via `transform: translateY()` when not `reading-chrome-visible`.
3. Header gets `transform: translateY(-100%)`, tab bar gets `transform: translateY(calc(100% + env(safe-area-inset-bottom)))`.
4. `MiniPlayer` stays visible (anchored to bottom, above the hidden tab bar position).
5. Reader settings FAB (`ReaderSettingsPanel`'s round button) stays visible — pinned to right edge with safe-area padding.

### 4.6 MiniPlayer stacking

When `MobileTabBar` is visible:
- Tab bar: `bottom: 0`, height `56 + safe-area-inset-bottom`.
- MiniPlayer: `bottom: calc(56px + env(safe-area-inset-bottom))`, height 64.
- Main content: `padding-bottom: calc(56px + 64px + safe-area-inset-bottom + 16px)` when player active, else `calc(56px + safe-area-inset-bottom + 16px)`.

To avoid hand-computing this, a CSS custom property `--bottom-chrome` is set on the `<body>` from `Layout.tsx` (using `useEffect` keyed on `player.articleId` and breakpoint). `main` reads `padding-bottom: var(--bottom-chrome, 16px)`.

When `MiniPlayer` is narrower than ~360 px (very small phones), drop the `⏪5` and `⏩10` skip buttons and the speed selector behind a "⋯" inline button that opens a small popover. This keeps title + play/pause + progress always visible.

## 5. Per-page changes

### 5.1 Priority pages (deep adaptation)

#### ArticleListPage
- Toolbar (filter chips + sort + view mode + group-by + 网摘开关): switch to wrap-flex on `≤ 1024px`. Secondary filters (`group-by`, `restrict-feed`) get moved behind a "更多筛选" button on `phone`, which opens a bottom sheet.
- `ArticleCard` already stacks vertically; trim horizontal padding on `phone`.
- Inline source-feed dropdown (currently `width: 200`) → full-width on phone.

#### ArticlePage
- Article header (title + metadata + actions): actions row wraps; primary actions (save, like, reading-mode) stay first, secondary (share, rescrape) move to a "⋯" overflow on `phone`.
- `ReaderSettingsPanel`: on `phone`, replace the right-anchored popover with a bottom sheet (slides up from below). The round `Aa` FAB stays, but on tap opens the sheet instead.
- Comments / link-set / recommendations cards: full width, single column. No structural change.

#### SavedPage
- On `phone`: replace `SavedTagSidebar` (left column) with a new `SavedTagChipBar` (horizontal scrolling sticky bar at top of the page). Chip = `{label, count}`; selected chip highlighted; sections separated by visual dividers within the same scrolling strip.
- On `tablet`: keep sidebar but narrower (180 px) and let content fill the rest.
- On `desktop`: unchanged.

#### LoginPage / RegisterPage
- Card: full width minus 32 px gutter on `phone`, max 400 px.
- All inputs: `font-size: 16px` minimum (prevents iOS WebKit auto-zoom on focus).
- Submit button: `height: 44px` on `phone`.

#### Reading mode (ReadingLayout)
- `padding: 8px 16px 96px` on `phone` (was `8px 24px 96px`).
- `max-width: 720px` retained (already responsive via `max-width`).
- `font-size` baseline raised to 17 px on `phone` (current default 16; readers tend to set higher anyway).
- Hide the toolbar's "← 退出阅读模式" button when chrome is hidden; surface via the top-chrome reveal (tap / scroll up).

#### MiniPlayer (already covered in 4.6)

### 5.2 Fallback pages (best-effort adaptation)

Apply the following generic rules from the global `@media (max-width: 1024px)` block — no per-page tuning required:

- All toolbars get `flex-wrap: wrap` and `gap: 8px`.
- All `width: 200px` (or similar fixed widths) inputs/selects become `width: 100%; max-width: 320px`.
- All multi-column grids reflow via `grid-template-columns: 1fr` on `phone` (existing `auto-fit` grids already cope on tablet).
- All buttons (except chip-style, settings-tab, theme-swatch, etc — the existing CSS exclusion list) get `min-height: 44px` on `phone`.
- Page padding: `#root` at 12 px (existing); cards at 12 px (existing); add `main { padding-inline: 4px }` only on phone.

Affected (no individual design notes — they fall through to generic rules):
- FeedListPage
- InsightsPage
- StatsPage
- WeeklyPage
- RecommendedPage (priority is the bottom-tab destination, but layout itself is just a card list)
- FeedHealthPage
- SettingsPage
- SharePage

## 6. iOS WebKit defaults (global)

Added once to `index.css` and `index.html`:

1. `index.html` viewport: change to `<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover" />`. The `viewport-fit=cover` is required for `env(safe-area-inset-*)` to return non-zero values on devices with a notch / home indicator.

2. Global CSS additions:
   - `html { -webkit-text-size-adjust: 100%; }` — prevent text auto-zoom on orientation change.
   - `body { min-height: 100dvh; }` with a `100vh` fallback for older WebKit.
   - `button { -webkit-tap-highlight-color: transparent; touch-action: manipulation; }` — remove the gray tap highlight; remove the 300 ms tap delay.
   - `input, textarea, select { font-size: 16px; }` on `≤ 1024px` (raise from 14 px to prevent iOS focus-zoom). Visual size on tablet/desktop remains 14 px (unchanged ≥ 1025 px).
   - `body { padding: env(safe-area-inset-top) env(safe-area-inset-right) 0 env(safe-area-inset-left); }` — top + sides safe-area for notched phones; bottom is handled by `--bottom-chrome`.

3. `#root` padding: change to `padding: 12px 16px` on `phone` (slightly more horizontal room for cards), `24px` on desktop (unchanged).

## 7. Data flow

No backend changes. No new API calls. All adaptation is presentational.

State touched:
- `useBreakpoint()` — new, hook-local state.
- `useReadingChrome()` — new, hook-local state; writes to `document.body.classList` for CSS hooks.
- `Layout.tsx` — new effect: sets `--bottom-chrome` CSS variable on `document.body`, keyed on breakpoint and `player.articleId`.
- `useReaderSettings` — unchanged.

### New files
- `frontend/src/hooks/useBreakpoint.ts`
- `frontend/src/hooks/useReadingChrome.ts`
- `frontend/src/components/MobileTabBar.tsx`
- `frontend/src/components/MoreSheet.tsx`
- `frontend/src/components/SavedTagChipBar.tsx`

### Modified files
- `frontend/index.html` — viewport meta `viewport-fit=cover`.
- `frontend/src/index.css` — global iOS WebKit defaults + new `≤ 1024px` and refined `≤ 640px` media blocks + reading-mode chrome transitions.
- `frontend/src/components/Layout.tsx` — mount `MobileTabBar`, set `--bottom-chrome`.
- `frontend/src/components/MiniPlayer.tsx` — bottom-offset by tab-bar height; collapse skip/speed on narrow viewports.
- `frontend/src/components/ReaderSettingsPanel.tsx` — bottom-sheet variant on `phone`.
- `frontend/src/components/ReadingLayout.tsx` — hook into `useReadingChrome`; tap-to-toggle target.
- `frontend/src/pages/SavedPage.tsx` — branch to `SavedTagChipBar` on `phone`.
- `frontend/src/pages/ArticlePage.tsx` — wrap action row, move secondary actions behind `⋯`.
- `frontend/src/pages/ArticleListPage.tsx` — wrap toolbar, move secondary filters behind `更多筛选` sheet on `phone`.
- `frontend/src/pages/LoginPage.tsx`, `RegisterPage.tsx` — input `font-size: 16px`, button height 44 px on `phone`.

## 8. Testing

Manual (the user owns the visual sign-off):
- iPhone (Safari + Chrome iOS): SE 1st gen 320 px width, iPhone 13 mini 375 px, iPhone 14 Pro Max 430 px.
- iPad (Safari + Chrome iPadOS): iPad mini portrait 744 px, iPad 11" portrait 834 px, iPad 11" landscape 1194 px.
- Browser DevTools device emulation as a first pass before any real device.

Automated:
- `agent-browser` headless run hitting `localhost:8080` (after `docker-compose up -d --build frontend`) at viewport sizes 375×667, 768×1024, 1280×800. For each, navigate the priority routes (`/login`, `/articles`, `/articles/:id`, `/saved`) and capture screenshots. Compare visually.
- No new unit tests — the changes are CSS + small hooks; existing test surface remains.

Acceptance criteria:
1. On a 375 px viewport in iOS Chrome, the user can: log in, see the article list, open an article, enter and exit reading mode, save the article, switch tabs via the bottom bar, and play audio — all without horizontal scroll, all tap targets ≥ 44 px.
2. Bottom tab bar does not overlap the iOS home indicator (verified via `env(safe-area-inset-bottom)` rendering on a notched device).
3. Reading mode hides both top header and bottom tab; tapping the article center toggles them; scrolling up reveals them.
4. On a 768 px iPad portrait viewport: same flow works, SavedPage retains a (narrower) sidebar, bottom tab is shown.
5. On a 1280 px viewport: layout is identical to today (no regression).

## 9. Risks

- **`useBreakpoint()` hydration / first-paint flash.** On first render the hook reads `window.matchMedia` synchronously, so there should be no flash. We will SSR-disable the hook by reading inside `useState(() => ...)` initializer, with a desktop default if `window` is undefined (defensive only — this app is SPA, no SSR).
- **`100dvh` not supported in Safari < 15.4.** Fallback to `100vh` via `@supports not (height: 100dvh)` is included.
- **Bottom sheet overlap with iOS soft keyboard.** When inputs are inside a sheet (e.g., `BatchFetchModal`), iOS Chrome may scroll the sheet under the keyboard. Fix: use `visualViewport` API to add bottom padding to the active sheet equal to the keyboard height when an input is focused. Only applied to sheets that contain inputs.
- **Scroll-direction chrome toggle vs. inertial scroll.** iOS WebKit fires scroll events during inertial scroll; we debounce by accumulating delta with a 30 px threshold (matches typical UI behavior in iOS apps) to avoid jitter.

## 10. Out-of-scope follow-ups (not in this spec)

- 2-column card grid on tablet for `ArticleListPage`.
- Pull-to-refresh.
- Swipe-back gesture on article detail.
- A dedicated `/more` route (currently a sheet — if the menu grows, promote to a route).
