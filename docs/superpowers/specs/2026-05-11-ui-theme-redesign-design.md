# UI Theme Redesign — Reading-Focused Themes & Layout Cleanup

**Date:** 2026-05-11
**Status:** Approved (pending user review of this spec)

## Goal

Replace the current generic SaaS-blue look with a reading-focused visual system. Provide four switchable themes (Paper / Quiet / Pearl / Night), tidy up buttons into a small consistent set, restructure the Settings page into sub-tabs, and fold the existing reader-mode palette toggle into the single global theme so the reader view inherits whichever theme is active.

The redesign is visual and structural-CSS only. No new features, no backend changes.

## Themes

Four themes, switchable in Settings, persisted to `localStorage` under key `rsspal_theme`. Default for new users: **Paper**.

| Theme  | `--bg`    | `--fg`     | `--fg-muted` | `--accent`  | `--border`  | Notes                          |
|--------|-----------|------------|--------------|-------------|-------------|--------------------------------|
| Paper  | `#f5edd6` | `#3a2f1a`  | `#7a6f55`    | `#5b3b00`   | `#d9cdaa`   | Sepia notebook; default        |
| Quiet  | `#fafaf7` | `#1f2933`  | `#5a6473`    | `#3a5a40`   | `#e3e3de`   | Calm off-white, forest accent  |
| Pearl  | `#ebebeb` | `#262626`  | `#666666`    | `#0066cc`   | `#d4d4d4`   | Neutral daylight, retains blue |
| Night  | `#1a1a1a` | `#d4d4d4`  | `#888888`    | `#7eb6ff`   | `#333333`   | Low-light                      |

Each theme is selected via `body[data-theme='paper|quiet|pearl|night']` and exposes the full token set:

```
--bg --surface --surface-hover --fg --fg-muted --accent --accent-soft
--accent-fg --border --code-bg --link
```

`--surface` is the row/card background (slightly lifted from `--bg`); `--accent-soft` is a tinted accent used for focus rings, active nav backgrounds, and progress bar tracks.

No hard-coded hex colors are permitted outside the four theme blocks (in `index.css`). Existing inline `style={{ background: '#...' }}` usages are migrated to either tokens (via classnames) or `var(--token)`.

### Reader-mode integration

The existing `body[data-reader-bg='default|sepia|green|gray|dark']` system in `index.css` is removed. `ReadingLayout` no longer overrides the palette — it reads `--bg`/`--fg`/etc from whichever global theme is active. The `ReaderSettingsPanel` keeps the font-size and content-width controls but drops the background-palette selector.

The existing `useReaderSettings` hook continues to manage reader-specific knobs (font size, width); the new theme picker is a separate piece of state.

## Settings page sub-tabs

The Settings page becomes a tabbed view. Tab state is reflected in the URL via `#appearance | #account | #ai | #tools` so a user can refresh and stay on the same tab.

| Tab           | Hash           | Sections (existing unless marked NEW)                              |
|---------------|----------------|--------------------------------------------------------------------|
| 外观          | `#appearance`  | **NEW** theme picker (4 swatches + live preview); 阅读体验 (font size, width) |
| 账号          | `#account`     | 修改密码; 邀请码管理 (admin only)                                  |
| AI            | `#ai`          | 我的 AI 配置; 摘要模板                                             |
| 工具          | `#tools`       | 工具; 浏览器抓取 (bookmarklet); 备份与恢复                         |

Default tab if no hash: `#appearance`.

The tab strip is a horizontal row of buttons under the page `<h2>`, using the new tertiary button style with an underline indicator on the active tab. On mobile, the strip scrolls horizontally if it overflows.

No section content is removed. Existing helper components (`BackupSection`, `BookmarkletSection`, `PromptField`) and their state are reused as-is; only the surrounding `<div className="card">` parents and their visual order are restructured.

### Theme picker UI

Inside the 外观 tab:

- Four large swatch buttons in a row (wrap on mobile). Each swatch shows a thumbnail of the theme's `--bg`/`--fg`/`--accent` plus the theme name underneath.
- The active theme has an `--accent` outline.
- Clicking a swatch immediately switches the theme (applies `data-theme` to `<body>`, writes to `localStorage`, dispatches a custom event so any listening component re-reads).
- Below the swatches, a small live-preview block containing a heading, a paragraph, a button, and a code span — to show what the theme looks like without leaving the page.

## Layout & typography

### Top nav

The top header in `Layout.tsx` is restyled but **not restructured** — it stays a top bar, not a side rail. Changes:

- Title `RSS Pal` uses the serif heading family with `--accent` color.
- Nav links use new token-based active/hover styles.
- Header sits inside the centered content column (no full-width background bar).

### Content column

`#root` keeps `max-width: 1200px` to leave room for wide screens like Stats, but a new `.content` wrapper provides a 760px reading-optimal centered column for list/article pages. Pages opt into the narrow column by wrapping their root in `<div className="content">`; pages that genuinely need full width (Stats, FeedHealth) skip it.

### Density

- Body: 16px / line-height 1.65 (currently 15/1.5).
- List rows: switch from white-card-with-shadow to hair-thin separators. New `.row` class: `padding: 12px 0; border-bottom: 1px solid var(--border)`.
- Cards (`.card`) keep their box style but use `--surface` instead of white, and `--border` instead of shadow on most variants. Existing `box-shadow` retained only where elevation matters (toaster, mini-player, modals).

### Typography

- Body family: `Inter, "Source Han Sans SC", system-ui, sans-serif` (unchanged for body; Chinese fallback added).
- Heading family: `"Source Han Serif SC", "Noto Serif SC", "Crimson Pro", Georgia, serif` — applied to `h1, h2, h3, .nav-brand`.
- Markdown body content keeps sans for readability.

## Buttons

All buttons consolidate into three classes plus standard `<button>` reset. No more inline `style={{ padding, background, color }}` on buttons.

| Class          | Visual                                                         | Use                                  |
|----------------|----------------------------------------------------------------|--------------------------------------|
| (default)      | filled `--accent` bg, `--accent-fg` text                       | Primary action per screen            |
| `.btn-ghost`   | transparent bg, `1px solid --border`, `--fg` text              | Secondary actions                    |
| `.btn-text`    | transparent bg, no border, `--accent` text                     | Tertiary/inline actions              |

Shared rules:

- Height 32px desktop / 36px mobile, 14px font, weight 600, 6px radius.
- Hover: `--surface-hover` background overlay on ghost/text; `--accent-soft` outline on primary.
- Focus: `outline: 2px solid var(--accent-soft); outline-offset: 2px`.
- Disabled: 45% opacity, `cursor: not-allowed`, hover suppressed.

The current `button.secondary` selector is kept as an alias for `.btn-ghost` so existing call sites work without churn, and is gradually migrated. New code uses `.btn-ghost`.

Per-call-site sweep:

- `ArticleCard.tsx`: media-indicator pills become ghost mini-buttons (`.btn-ghost .btn-sm`).
- `RecommendationsCard.tsx`: feedback buttons become `.btn-text` icon-only.
- `ArticlePlayerCard.tsx`: play/pause becomes primary; transport controls ghost.
- `Layout.tsx`: logout, mobile menu toggle → ghost.
- `SettingsPage.tsx`: AI 润色, 使用润色版 → ghost; primary save remains default.
- `FeedHealthTable.tsx`, `SavedTagSidebar.tsx`, `TagBar.tsx`: inline-style buttons → ghost/text variants.

## CSS architecture

`frontend/src/index.css` is rewritten with this structure:

```
1. Reset & root box-sizing
2. Theme blocks (body[data-theme=...]) — token definitions
3. Element defaults using tokens (body bg/fg, a, input, button)
4. Button classes (.btn-ghost, .btn-text)
5. Layout primitives (.content, .row, .card, .flex, .flex-between, gap utils)
6. Component-scoped sections (nav-link, rec-panel, progress-bar, tag-chip,
   saved-row, mode-toggle, markdown-body, reading-layout, ai-marker,
   confetti, typing-caret)
7. Responsive overrides (@media max-width: 640px)
```

Total expected size: similar to current (~540 lines) — old reader-bg blocks (~10 lines) and most hard-coded colors (~50 sites) are replaced with token references, partially offset by the new theme blocks (~50 lines) and button variants (~30 lines).

## Theme switcher mechanics

A small module at `frontend/src/util/theme.ts` (new file) owns:

```ts
type Theme = 'paper' | 'quiet' | 'pearl' | 'night'

export function getTheme(): Theme            // reads localStorage, defaults 'paper'
export function setTheme(t: Theme): void     // writes <body data-theme>, localStorage, dispatches event
export function useTheme(): [Theme, (t: Theme) => void]  // React hook
```

`main.tsx` calls `setTheme(getTheme())` once at boot, before React mounts, so the initial paint is correct (no flash of wrong theme).

The theme picker in Settings calls `setTheme()`. Anywhere else that wants to react to theme changes (none currently) can listen for the `rsspal-theme-change` `CustomEvent` on `window`.

## Files changed

| File                                          | Action                                          |
|-----------------------------------------------|-------------------------------------------------|
| `frontend/src/index.css`                      | Rewrite — theme tokens, button classes, .row    |
| `frontend/src/main.tsx`                       | Apply initial theme before mount                |
| `frontend/src/util/theme.ts`                  | **NEW** — theme state module                    |
| `frontend/src/components/Layout.tsx`          | Restyle top nav (no structural change)          |
| `frontend/src/pages/SettingsPage.tsx`         | Sub-tab structure; **NEW** AppearanceTab section |
| `frontend/src/components/ReadingLayout.tsx`   | Drop palette overrides; inherit global theme    |
| `frontend/src/components/ReaderSettingsPanel.tsx` | Drop bg-palette selector                    |
| `frontend/src/components/ArticleCard.tsx`     | Migrate inline-style buttons → classes          |
| `frontend/src/components/RecommendationsCard.tsx` | Migrate inline-style buttons → classes      |
| `frontend/src/components/ArticlePlayerCard.tsx` | Migrate inline-style buttons → classes        |
| `frontend/src/components/FeedHealthTable.tsx` | Migrate inline-style buttons → classes          |
| `frontend/src/components/SavedTagSidebar.tsx` | Migrate inline-style buttons → classes          |
| `frontend/src/components/TagBar.tsx`          | Migrate inline-style buttons → classes          |
| `frontend/src/hooks/useReaderSettings.ts`     | Drop the bg-palette field; keep font/width      |

All other pages (FeedListPage, ArticleListPage, ArticlePage, InsightsPage, StatsPage, WeeklyPage, RecommendedPage, SavedPage, FeedHealthPage, SharePage, LoginPage, RegisterPage) inherit the new look via shared classes and need no per-page edits, unless a sweep turns up an inline color hardcode that should switch to a token. Such sweeps are in-scope but small.

## Out of scope

- Side-rail navigation (deferred; top nav restyled in place).
- Reordering or removing any Settings sections.
- Backend / API changes.
- New animations beyond what already exists (typing caret, AI marker pulse, confetti).
- Per-page redesigns beyond classname migration.

## Verification

After implementation:

1. Open the app, confirm Paper theme is the default for a fresh user (clear `localStorage` first).
2. In Settings → 外观, click each of the four swatches; confirm immediate global re-paint with no flash, and the choice persists across reload.
3. Enter the article reader view; confirm its background matches the active global theme. Confirm `ReaderSettingsPanel` no longer offers a background palette.
4. Spot-check each page (Feeds, Articles, Article detail, Insights, Stats, Weekly, Recommended, Saved, Settings, Login) under each theme — looking for:
   - No hard-coded white/black/blue backgrounds.
   - Buttons have one consistent height per breakpoint.
   - Focus rings visible on tab navigation.
5. Confirm Settings sub-tab hash navigation: `/settings#ai` deep-links to the AI tab.
6. Mobile breakpoint (≤640px): nav still collapses to hamburger; Settings tab strip still scrolls horizontally; buttons hit 36px height.

## Risks & mitigations

- **Flash of wrong theme on first paint** — mitigated by applying `data-theme` in `main.tsx` before React mounts.
- **Reader-mode users who liked sepia even when the rest of the app was light** — folded into the global theme intentionally; if a user wants reader-only sepia, they'd pick the Paper theme globally. Accepted simplification.
- **Inline-style buttons spread across many files** — sweep is mechanical but could miss one; we'll grep `style={{[^}]*background` after the migration to catch stragglers.
- **CSS variable inheritance into third-party rendered content (highlight.js, katex)** — those rely on their own stylesheets; we override only the surrounding `pre`/`code` backgrounds via the `markdown-body` rules, which already exist.
