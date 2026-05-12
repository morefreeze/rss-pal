# Mobile Adaptation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Adapt the RSS Pal frontend for iPhone + iPad (iOS Chrome priority), with bottom-tab navigation, reading-mode chrome auto-hide, MiniPlayer safe-area stacking, and per-page responsive layouts.

**Architecture:** Three breakpoints (`phone ≤640`, `tablet 641–1024`, `desktop >1024`) driven mostly by CSS. A small `useBreakpoint()` hook handles the few cases where DOM structure differs (SavedPage sidebar↔chip, ReaderSettings popover↔sheet). A `useReadingChrome()` hook owns chrome visibility in reading mode via body classes.

**Tech Stack:** React 18 (no test framework), TypeScript (strict), Vite, handwritten CSS (no Tailwind), `matchMedia`, `env(safe-area-inset-*)`.

**Spec:** `docs/superpowers/specs/2026-05-12-mobile-adaptation-design.md`

**Verification convention (project has no unit tests):**
- After each code change: `cd frontend && npx tsc --noEmit` must pass.
- After each task: commit on `feature/mobile-adaptation`.
- After all code tasks: `docker-compose up -d --build frontend` + agent-browser screenshot at 375/768/1280 viewports.

**Branch:** Already on `feature/mobile-adaptation` (draft PR #21).

---

## File Structure

### New files
- `frontend/src/hooks/useBreakpoint.ts` — matchMedia-based breakpoint hook.
- `frontend/src/hooks/useReadingChrome.ts` — body-class toggler for reading mode chrome visibility (tap + scroll direction).
- `frontend/src/components/MobileTabBar.tsx` — bottom tab bar (phone + tablet).
- `frontend/src/components/MoreSheet.tsx` — bottom sheet for `⋯ 更多` tab content (订阅 / 周刊 / 洞察 / 统计 / 设置 / 登出).
- `frontend/src/components/SavedTagChipBar.tsx` — horizontal scrolling tag/source chip strip for SavedPage on phone.

### Modified files
- `frontend/index.html` — viewport meta gets `viewport-fit=cover`.
- `frontend/src/index.css` — global iOS WebKit defaults + new `≤1024px` media block + refined `≤640px` media block + reading-chrome transitions.
- `frontend/src/components/Layout.tsx` — mount `MobileTabBar` on non-desktop; set `--bottom-chrome` CSS variable.
- `frontend/src/components/MiniPlayer.tsx` — `bottom: calc(56px + env(safe-area-inset-bottom))` on non-desktop; collapse skip/speed behind `⋯` button when width < 360 px.
- `frontend/src/components/ReaderSettingsPanel.tsx` — bottom-sheet variant on `phone` breakpoint.
- `frontend/src/components/ReadingLayout.tsx` — wire into `useReadingChrome`; make article body a tap target.
- `frontend/src/pages/SavedPage.tsx` — branch to `SavedTagChipBar` on `phone`.
- `frontend/src/pages/ArticlePage.tsx` — call `useReadingChrome` when in reading mode.
- `frontend/src/pages/LoginPage.tsx`, `frontend/src/pages/RegisterPage.tsx` — full-width card and `min-height: 44px` submit button (rest handled by global CSS).

---

## Task 1: Global iOS WebKit defaults

**Files:**
- Modify: `frontend/index.html`
- Modify: `frontend/src/index.css` (head section + body rule)

- [ ] **Step 1: Update viewport meta to enable safe-area-inset**

Replace `frontend/index.html` line 6:

```html
<meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover" />
```

- [ ] **Step 2: Add iOS WebKit defaults to `index.css`**

In `frontend/src/index.css`, replace the existing reset/tokens block (lines 1–16) with:

```css
/* === Reset === */
*, *::before, *::after { box-sizing: border-box; }
* { margin: 0; padding: 0; }

html {
  -webkit-text-size-adjust: 100%;
}

/* === Tokens === */
:root {
  --font-sans: Inter, "Source Han Sans SC", system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;
  --font-serif: "Source Han Serif SC", "Noto Serif SC", "Crimson Pro", Georgia, "Songti SC", serif;
  --font-mono: Menlo, Monaco, "Courier New", monospace;
  --radius: 6px;
  --content-max: 760px;
  --bottom-chrome: 16px;

  font-family: var(--font-sans);
  font-weight: 400;
  line-height: 1.65;
}
```

- [ ] **Step 3: Update body rule for safe-area + dvh**

Replace `index.css` lines 93–99 (the `body { min-height: 100vh; ... }` block) with:

```css
body {
  min-height: 100vh;
  min-height: 100dvh;
  background: var(--bg);
  color: var(--fg);
  transition: background-color 0.15s ease, color 0.15s ease;
  padding: env(safe-area-inset-top) env(safe-area-inset-right) 0 env(safe-area-inset-left);
}
```

- [ ] **Step 4: Add tap-highlight + touch-action to buttons**

In `index.css`, find the `button { ... }` block (around line 138) and add two declarations at the top:

```css
button {
  -webkit-tap-highlight-color: transparent;
  touch-action: manipulation;
  cursor: pointer;
  padding: 0 14px;
  height: 32px;
  /* ... rest unchanged ... */
}
```

- [ ] **Step 5: Add `main { padding-bottom: var(--bottom-chrome) }` rule**

Add this rule in `index.css` after the existing `#root { ... }` block (around line 105):

```css
main {
  padding-bottom: var(--bottom-chrome);
}
```

- [ ] **Step 6: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors.

```bash
git add frontend/index.html frontend/src/index.css
git commit -m "$(cat <<'EOF'
feat(mobile): iOS WebKit defaults — viewport-fit + safe-area + tap-highlight

- viewport meta: viewport-fit=cover (enables env(safe-area-inset-*))
- html: -webkit-text-size-adjust: 100% (no auto-zoom on rotate)
- body: min-height: 100dvh fallback to 100vh; safe-area inset on padding
- button: -webkit-tap-highlight-color transparent; touch-action: manipulation
- --bottom-chrome CSS var (default 16px) for main padding

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: `useBreakpoint` hook

**Files:**
- Create: `frontend/src/hooks/useBreakpoint.ts`

- [ ] **Step 1: Create the hook**

Write `frontend/src/hooks/useBreakpoint.ts`:

```ts
import { useEffect, useState } from 'react'

export type Breakpoint = 'phone' | 'tablet' | 'desktop'

// Phone ≤ 640px, Tablet 641–1024px, Desktop > 1024px. SSR-safe default:
// if `window` is missing we return 'desktop' to mirror legacy markup.
function compute(): Breakpoint {
  if (typeof window === 'undefined') return 'desktop'
  if (window.matchMedia('(max-width: 640px)').matches) return 'phone'
  if (window.matchMedia('(max-width: 1024px)').matches) return 'tablet'
  return 'desktop'
}

export function useBreakpoint(): Breakpoint {
  const [bp, setBp] = useState<Breakpoint>(() => compute())

  useEffect(() => {
    const phone = window.matchMedia('(max-width: 640px)')
    const tablet = window.matchMedia('(max-width: 1024px)')
    const update = () => setBp(compute())
    phone.addEventListener('change', update)
    tablet.addEventListener('change', update)
    return () => {
      phone.removeEventListener('change', update)
      tablet.removeEventListener('change', update)
    }
  }, [])

  return bp
}
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useBreakpoint.ts
git commit -m "$(cat <<'EOF'
feat(mobile): useBreakpoint hook (phone/tablet/desktop)

matchMedia subscription with SSR-safe default. Used only where DOM
structure differs across breakpoints; visual tweaks stay in CSS.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `useReadingChrome` hook

**Files:**
- Create: `frontend/src/hooks/useReadingChrome.ts`

- [ ] **Step 1: Create the hook**

Write `frontend/src/hooks/useReadingChrome.ts`:

```ts
import { useEffect, useState, useCallback, useRef } from 'react'

// useReadingChrome owns the "chrome visible" state in reading mode.
// Adds `reading-mode-active` to body on enable; toggles
// `reading-chrome-visible` based on user input.
//
// Reveal triggers: tap article center (callback exposed to caller),
// scroll up ≥ THRESHOLD, page near top.
// Hide triggers: scroll down ≥ THRESHOLD.
//
// `enabled=false` removes both classes and unbinds listeners.
const THRESHOLD = 30
const TOP_REVEAL = 40

export function useReadingChrome(enabled: boolean): {
  chromeVisible: boolean
  toggle: () => void
} {
  const [chromeVisible, setChromeVisible] = useState(false)
  const lastY = useRef(0)
  const accum = useRef(0)

  useEffect(() => {
    if (!enabled) {
      document.body.classList.remove('reading-mode-active')
      document.body.classList.remove('reading-chrome-visible')
      return
    }
    document.body.classList.add('reading-mode-active')
    setChromeVisible(false)
    lastY.current = window.scrollY
    accum.current = 0

    const onScroll = () => {
      const y = window.scrollY
      const dy = y - lastY.current
      lastY.current = y
      if (y < TOP_REVEAL) {
        setChromeVisible(true)
        accum.current = 0
        return
      }
      // Accumulate same-direction delta; reset on direction change
      if ((accum.current > 0 && dy < 0) || (accum.current < 0 && dy > 0)) {
        accum.current = dy
      } else {
        accum.current += dy
      }
      if (accum.current >= THRESHOLD) {
        setChromeVisible(false)
        accum.current = 0
      } else if (accum.current <= -THRESHOLD) {
        setChromeVisible(true)
        accum.current = 0
      }
    }

    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      document.body.classList.remove('reading-mode-active')
      document.body.classList.remove('reading-chrome-visible')
    }
  }, [enabled])

  useEffect(() => {
    if (!enabled) return
    document.body.classList.toggle('reading-chrome-visible', chromeVisible)
  }, [chromeVisible, enabled])

  const toggle = useCallback(() => {
    setChromeVisible(v => !v)
  }, [])

  return { chromeVisible, toggle }
}
```

- [ ] **Step 2: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/hooks/useReadingChrome.ts
git commit -m "$(cat <<'EOF'
feat(mobile): useReadingChrome hook (tap + scroll chrome toggle)

Body classes:
- reading-mode-active: set whenever reading mode is on
- reading-chrome-visible: toggles based on tap (via toggle()) or
  scroll direction (down hides, up reveals, top always reveals).
Threshold 30px accumulates same-direction delta to avoid jitter.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Reading-mode chrome CSS transitions

**Files:**
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Replace the dead `display: none` rule**

In `frontend/src/index.css`, find this existing line (around line 550):

```css
/* Hide app chrome (header) when reading mode is active. */
body.reading-mode-active #root > div > header { display: none; }
```

Replace with:

```css
/* === Reading mode chrome === */
/* Header + bottom tab slide off when chrome hidden. */
#root > div > header,
.mobile-tab-bar {
  transition: transform 0.22s ease;
}
body.reading-mode-active:not(.reading-chrome-visible) #root > div > header {
  transform: translateY(-110%);
  pointer-events: none;
}
body.reading-mode-active:not(.reading-chrome-visible) .mobile-tab-bar {
  transform: translateY(calc(100% + env(safe-area-inset-bottom)));
  pointer-events: none;
}
```

- [ ] **Step 2: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/index.css
git commit -m "$(cat <<'EOF'
feat(mobile): reading-mode chrome slides via transform transition

Replaces dead display:none rule with translateY transitions on the
top header and (forthcoming) .mobile-tab-bar. Keyed on body classes
managed by useReadingChrome.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: `MoreSheet` component

**Files:**
- Create: `frontend/src/components/MoreSheet.tsx`

- [ ] **Step 1: Create the component**

Write `frontend/src/components/MoreSheet.tsx`:

```tsx
import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'

type SheetItem = { icon: string; label: string; to?: string; action?: 'logout' }

const ITEMS: SheetItem[] = [
  { icon: '📡', label: '订阅',     to: '/feeds' },
  { icon: '📅', label: '周刊',     to: '/weekly' },
  { icon: '💡', label: '洞察',     to: '/insights' },
  { icon: '📊', label: '统计',     to: '/stats' },
  { icon: '⚙️', label: '设置',     to: '/settings' },
  { icon: '🚪', label: '登出',     action: 'logout' },
]

interface Props {
  open: boolean
  onClose: () => void
  onLogout: () => void
}

export default function MoreSheet({ open, onClose, onLogout }: Props) {
  const navigate = useNavigate()

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])

  if (!open) return null

  const onItem = (item: SheetItem) => {
    onClose()
    if (item.action === 'logout') { onLogout(); return }
    if (item.to) navigate(item.to)
  }

  return (
    <>
      <div
        onClick={onClose}
        style={{
          position: 'fixed', inset: 0,
          background: 'rgba(0,0,0,0.35)',
          zIndex: 1200,
        }}
      />
      <div
        role="dialog"
        aria-label="更多"
        style={{
          position: 'fixed',
          left: 0, right: 0, bottom: 0,
          background: 'var(--surface)',
          borderTop: '1px solid var(--border)',
          borderTopLeftRadius: 16,
          borderTopRightRadius: 16,
          padding: '8px 8px calc(env(safe-area-inset-bottom) + 16px) 8px',
          zIndex: 1201,
          boxShadow: '0 -4px 16px rgba(0,0,0,0.18)',
        }}
      >
        <div
          aria-hidden="true"
          style={{
            width: 36, height: 4, borderRadius: 2,
            background: 'var(--border)',
            margin: '8px auto 12px',
          }}
        />
        {ITEMS.map(item => (
          <button
            key={item.label}
            type="button"
            onClick={() => onItem(item)}
            style={{
              display: 'flex', alignItems: 'center', gap: 12,
              width: '100%',
              padding: '14px 16px',
              height: 'auto',
              minHeight: 44,
              background: 'transparent',
              color: 'var(--fg)',
              border: 'none',
              borderRadius: 8,
              fontSize: 16,
              fontWeight: 500,
              textAlign: 'left',
            }}
          >
            <span style={{ fontSize: 20, width: 24, textAlign: 'center' }}>{item.icon}</span>
            <span>{item.label}</span>
          </button>
        ))}
      </div>
    </>
  )
}
```

- [ ] **Step 2: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/components/MoreSheet.tsx
git commit -m "$(cat <<'EOF'
feat(mobile): MoreSheet bottom sheet for secondary nav destinations

订阅 / 周刊 / 洞察 / 统计 / 设置 / 登出. Tap row -> navigate + close.
Backdrop tap or Esc dismisses. Safe-area-inset padding for bottom.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: `MobileTabBar` component

**Files:**
- Create: `frontend/src/components/MobileTabBar.tsx`

- [ ] **Step 1: Create the component**

Write `frontend/src/components/MobileTabBar.tsx`:

```tsx
import { useState } from 'react'
import { NavLink } from 'react-router-dom'
import MoreSheet from './MoreSheet'

type Tab = { to: string; icon: string; label: string; showUnread?: boolean }

const TABS: Tab[] = [
  { to: '/articles',    icon: '📰', label: '文章', showUnread: true },
  { to: '/saved',       icon: '⭐', label: '网摘' },
  { to: '/recommended', icon: '✨', label: '推荐' },
]

interface Props {
  unreadCount: number
  onLogout: () => void
}

export default function MobileTabBar({ unreadCount, onLogout }: Props) {
  const [moreOpen, setMoreOpen] = useState(false)

  const tabStyle = (active: boolean): React.CSSProperties => ({
    flex: 1,
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 2,
    padding: '6px 0',
    background: 'transparent',
    border: 'none',
    color: active ? 'var(--accent)' : 'var(--fg-muted)',
    fontSize: 11,
    fontWeight: 500,
    height: 'auto',
    textDecoration: 'none',
    minHeight: 44,
  })

  return (
    <>
      <nav
        className="mobile-tab-bar"
        aria-label="主导航"
        style={{
          position: 'fixed',
          left: 0, right: 0, bottom: 0,
          height: 'calc(56px + env(safe-area-inset-bottom))',
          paddingBottom: 'env(safe-area-inset-bottom)',
          background: 'var(--surface)',
          borderTop: '1px solid var(--border)',
          display: 'flex',
          zIndex: 1000,
        }}
      >
        {TABS.map(tab => (
          <NavLink
            key={tab.to}
            to={tab.to}
            className="mobile-tab-link"
            style={({ isActive }) => tabStyle(isActive)}
          >
            <span style={{ fontSize: 22, lineHeight: 1, position: 'relative' }}>
              {tab.icon}
              {tab.showUnread && unreadCount > 0 && (
                <span
                  className="unread-badge"
                  style={{ position: 'absolute', top: -4, right: -10 }}
                >
                  {unreadCount > 99 ? '99+' : unreadCount}
                </span>
              )}
            </span>
            <span>{tab.label}</span>
          </NavLink>
        ))}
        <button
          type="button"
          onClick={() => setMoreOpen(true)}
          aria-label="更多"
          style={tabStyle(moreOpen)}
        >
          <span style={{ fontSize: 22, lineHeight: 1 }}>⋯</span>
          <span>更多</span>
        </button>
      </nav>
      <MoreSheet open={moreOpen} onClose={() => setMoreOpen(false)} onLogout={onLogout} />
    </>
  )
}
```

- [ ] **Step 2: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/components/MobileTabBar.tsx
git commit -m "$(cat <<'EOF'
feat(mobile): MobileTabBar component (📰/⭐/✨/⋯)

Fixed-bottom 56px + safe-area. Active tab styled in accent color.
Unread badge on 📰 文章. ⋯ opens MoreSheet for secondary destinations.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Wire `MobileTabBar` into Layout

**Files:**
- Modify: `frontend/src/components/Layout.tsx`

- [ ] **Step 1: Import hooks and new component**

Open `frontend/src/components/Layout.tsx`. Replace the import block (lines 1–6) with:

```tsx
import { useState, useEffect, useRef, useCallback } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { logout, getUnreadCount } from '../api/client'
import Toaster from './Toaster'
import { PlayerProvider, usePlayer } from '../player/PlayerContext'
import MiniPlayer from './MiniPlayer'
import MobileTabBar from './MobileTabBar'
import { useBreakpoint } from '../hooks/useBreakpoint'
```

- [ ] **Step 2: Move main render into an inner component that can read player context**

The current `Layout` wraps everything in `<PlayerProvider>` so `usePlayer()` is not available at this level. Extract the rendered tree into a child component `LayoutInner`.

Replace the final `return ( ... )` block of `Layout` (lines 158–218 of the existing file) with this structure:

```tsx
  return (
    <PlayerProvider>
      <LayoutInner
        user={user}
        unreadCount={unreadCount}
        onLogout={handleLogout}
        renderNavLabel={renderNavLabel}
        navLinkClass={navLinkClass}
        menuOpen={menuOpen}
        setMenuOpen={setMenuOpen}
      />
    </PlayerProvider>
  )
}

interface LayoutInnerProps {
  user: { id: number; username: string; is_admin: boolean } | null
  unreadCount: number
  onLogout: () => void
  renderNavLabel: (item: NavItem) => React.ReactNode
  navLinkClass: (s: { isActive: boolean }) => string
  menuOpen: boolean
  setMenuOpen: (v: boolean) => void
}

function LayoutInner({
  user, unreadCount, onLogout, renderNavLabel, navLinkClass, menuOpen, setMenuOpen,
}: LayoutInnerProps) {
  const bp = useBreakpoint()
  const player = usePlayer()

  // --bottom-chrome = tab-bar height (if shown) + mini-player height (if active)
  // + safe-area-inset-bottom + 16px gutter. Reads on <body> so any deep main
  // can pad correctly.
  useEffect(() => {
    const tabH = bp === 'desktop' ? 0 : 56
    const playerH = player.articleId !== null ? 64 : 0
    const chrome = `calc(${tabH + playerH + 16}px + env(safe-area-inset-bottom))`
    document.body.style.setProperty('--bottom-chrome', chrome)
    return () => {
      document.body.style.removeProperty('--bottom-chrome')
    }
  }, [bp, player.articleId])

  return (
    <div>
      <header style={{ marginBottom: 20 }}>
        <div className="flex-between">
          <h1 className="nav-brand">RSS Pal</h1>

          <nav className="flex gap-2 desktop-nav" style={{ alignItems: 'center' }}>
            {NAV_ITEMS.map(item => (
              <NavLink key={item.to} to={item.to} className={navLinkClass}>
                {renderNavLabel(item)}
              </NavLink>
            ))}
            {user && <UserMenu username={user.username} onLogout={onLogout} />}
          </nav>

          <button
            className="btn-ghost btn-sm mobile-menu-btn"
            onClick={() => setMenuOpen(!menuOpen)}
            aria-label="菜单"
          >
            {menuOpen ? '✕' : '☰'}
          </button>
        </div>

        {menuOpen && (
          <nav className="mobile-nav" style={{
            marginTop: 8,
            padding: '8px 0',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 8,
          }}>
            {NAV_ITEMS.map(item => (
              <NavLink
                key={item.to}
                to={item.to}
                className={navLinkClass}
                onClick={() => setMenuOpen(false)}
                style={{ display: 'block', padding: '10px 16px', borderBottom: '1px solid var(--border)', borderRadius: 0 }}
              >
                {renderNavLabel(item)}
              </NavLink>
            ))}
            <div style={{ padding: '10px 16px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span className="text-muted text-sm">👤 {user?.username}</span>
              <button className="btn-ghost btn-sm" onClick={onLogout}>
                🚪 登出
              </button>
            </div>
          </nav>
        )}
      </header>
      <main>
        <Outlet />
      </main>
      <Toaster />
      <MiniPlayer />
      {bp !== 'desktop' && (
        <MobileTabBar unreadCount={unreadCount} onLogout={onLogout} />
      )}
    </div>
  )
}
```

Note: replace the old `<main style={{ paddingBottom: 80 }}>` with the plain `<main>` shown above — `main` now reads `padding-bottom` from `--bottom-chrome` via the CSS rule added in Task 1.

- [ ] **Step 3: Hide legacy mobile hamburger on non-desktop (we now have bottom tab)**

In `frontend/src/index.css`, find the existing `@media (max-width: 640px)` block (line 766+). Update the `.mobile-menu-btn` rule inside it:

Replace:
```css
.mobile-menu-btn { display: none; }
.mobile-nav { display: none; }

@media (max-width: 640px) {
  .desktop-nav { display: none !important; }
  .mobile-menu-btn { display: block; }
  .mobile-nav { display: block; }
  ...
}
```

With:
```css
.mobile-menu-btn { display: none; }
.mobile-nav { display: none; }

@media (max-width: 1024px) {
  .desktop-nav { display: none !important; }
  /* mobile-menu-btn intentionally remains hidden: we use MobileTabBar */
  #root > div > header { margin-bottom: 12px; }
}

@media (max-width: 640px) {
  #root { padding: 12px; }
  .card { padding: 12px; }
  button:not(.btn-sm):not(.settings-tab):not(.theme-swatch):not(.tag-chip-remove):not(.tag-add-btn):not(.reading-summary-toggle):not(.rec-feedback-btn):not(.rec-panel-header):not(.saved-row):not(.tag-suggest-dropdown button):not(.saved-mode-toggle button) { min-height: 44px; }
}
```

The legacy `mobile-menu-btn` and `mobile-nav` blocks/markup in Layout.tsx stay (they're now dead but kept for desktop > 1024px where they were also hidden). They could be removed but are out of scope.

- [ ] **Step 4: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/components/Layout.tsx frontend/src/index.css
git commit -m "$(cat <<'EOF'
feat(mobile): mount MobileTabBar in Layout; manage --bottom-chrome

LayoutInner consumes usePlayer + useBreakpoint to compute --bottom-chrome.
Desktop nav hidden on ≤1024px (was ≤640px). MobileTabBar shown on
phone + tablet. Button min-height bumped to 44px on phone.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: MiniPlayer mobile adaptations

**Files:**
- Modify: `frontend/src/components/MiniPlayer.tsx`

- [ ] **Step 1: Read breakpoint, offset above tab bar, collapse controls when narrow**

Replace the entire body of `frontend/src/components/MiniPlayer.tsx` with:

```tsx
import { useEffect, useRef, useState } from 'react'
import { usePlayer } from '../player/PlayerContext'
import Spinner from './Spinner'
import { useBreakpoint } from '../hooks/useBreakpoint'

const SPEEDS = [1, 1.25, 1.5, 1.75, 2] as const

function fmt(sec: number): string {
  if (!isFinite(sec) || sec < 0) return '--:--'
  const total = Math.floor(sec)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
}

export default function MiniPlayer() {
  const p = usePlayer()
  const bp = useBreakpoint()
  const [dragValue, setDragValue] = useState<number | null>(null)
  const [extraOpen, setExtraOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const [narrow, setNarrow] = useState(false)

  useEffect(() => {
    if (!ref.current) return
    const el = ref.current
    const ro = new ResizeObserver(entries => {
      for (const entry of entries) {
        setNarrow(entry.contentRect.width < 480)
      }
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [p.articleId])

  if (p.articleId === null) return null

  const sliderValue = dragValue ?? p.position
  const bottomOffset = bp === 'desktop'
    ? '0'
    : 'calc(56px + env(safe-area-inset-bottom))'

  return (
    <div
      ref={ref}
      role="region"
      aria-label="Podcast player"
      style={{
        position: 'fixed',
        bottom: bottomOffset,
        left: 0,
        right: 0,
        height: 64,
        background: 'var(--surface)',
        borderTop: '1px solid var(--border)',
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '0 12px',
        boxShadow: '0 -2px 8px rgba(0,0,0,0.08)',
        zIndex: 999,
      }}
    >
      <button
        onClick={p.toggle}
        aria-label={p.loading ? '加载中' : p.playing ? '暂停' : '播放'}
        disabled={p.loading && !p.playing}
        style={{ fontSize: 20, padding: '4px 10px', minWidth: 40, display: 'inline-flex', alignItems: 'center', justifyContent: 'center' }}
      >
        {p.loading ? <Spinner size={16} /> : p.playing ? '⏸' : '▶'}
      </button>

      {!narrow && (
        <>
          <button onClick={() => p.skip(-5)} aria-label="后退5秒" style={{ padding: '4px 8px' }}>⏪5</button>
          <button onClick={() => p.skip(10)} aria-label="前进10秒" style={{ padding: '4px 8px' }}>⏩10</button>
        </>
      )}

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
        <div style={{ fontSize: 13, fontWeight: 600, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {p.title}
          {p.feedTitle && <span style={{ color: 'var(--fg-muted)', fontWeight: 400 }}> · {p.feedTitle}</span>}
        </div>
        <input
          type="range"
          min={0}
          max={p.duration || 0}
          value={sliderValue}
          onChange={e => setDragValue(Number(e.target.value))}
          onMouseUp={() => {
            if (dragValue !== null) { p.seek(dragValue); setDragValue(null) }
          }}
          onTouchEnd={() => {
            if (dragValue !== null) { p.seek(dragValue); setDragValue(null) }
          }}
          onKeyUp={() => {
            if (dragValue !== null) { p.seek(dragValue); setDragValue(null) }
          }}
          style={{ width: '100%' }}
          aria-label="播放进度"
        />
      </div>

      {!narrow && (
        <span style={{ fontSize: 12, color: 'var(--fg-muted)', whiteSpace: 'nowrap' }}>
          {fmt(sliderValue)} / {fmt(p.duration)}
        </span>
      )}

      {!narrow && (
        <select
          value={p.speed}
          onChange={e => p.setSpeed(Number(e.target.value) as 1 | 1.25 | 1.5 | 1.75 | 2)}
          aria-label="播放速度"
          style={{ fontSize: 13 }}
        >
          {SPEEDS.map(s => <option key={s} value={s}>{s}x</option>)}
        </select>
      )}

      {narrow && (
        <button
          onClick={() => setExtraOpen(o => !o)}
          aria-label="更多控制"
          style={{ padding: '4px 8px' }}
        >⋯</button>
      )}

      <button onClick={p.close} aria-label="关闭播放器" style={{ padding: '4px 8px' }}>✕</button>

      {p.error && <span style={{ color: '#c00', fontSize: 12 }}>{p.error}</span>}

      {narrow && extraOpen && (
        <div
          style={{
            position: 'absolute',
            right: 8,
            bottom: 'calc(100% + 4px)',
            background: 'var(--surface)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            padding: 8,
            display: 'flex',
            gap: 6,
            alignItems: 'center',
            boxShadow: '0 4px 12px rgba(0,0,0,0.18)',
            zIndex: 1001,
          }}
        >
          <button onClick={() => p.skip(-5)} style={{ padding: '4px 8px' }}>⏪5</button>
          <button onClick={() => p.skip(10)} style={{ padding: '4px 8px' }}>⏩10</button>
          <select
            value={p.speed}
            onChange={e => p.setSpeed(Number(e.target.value) as 1 | 1.25 | 1.5 | 1.75 | 2)}
            aria-label="播放速度"
            style={{ fontSize: 13 }}
          >
            {SPEEDS.map(s => <option key={s} value={s}>{s}x</option>)}
          </select>
          <span style={{ fontSize: 12, color: 'var(--fg-muted)', whiteSpace: 'nowrap' }}>
            {fmt(sliderValue)} / {fmt(p.duration)}
          </span>
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/components/MiniPlayer.tsx
git commit -m "$(cat <<'EOF'
feat(mobile): MiniPlayer stacks above tab bar; collapses on narrow

bottom: calc(56px + safe-area-inset) on phone/tablet; 0 on desktop.
When player width < 480px, hide skip/speed/time inline and surface
them behind a ⋯ popover. Width tracked via ResizeObserver.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: `SavedTagChipBar` component

**Files:**
- Create: `frontend/src/components/SavedTagChipBar.tsx`

- [ ] **Step 1: Create the component**

Write `frontend/src/components/SavedTagChipBar.tsx`:

```tsx
import { UserTag } from '../api/client'
import { SavedSelection, SavedSourceRow } from './SavedTagSidebar'

interface Props {
  tags: UserTag[]
  sources: SavedSourceRow[]
  selection: SavedSelection
  onSelect: (sel: SavedSelection) => void
}

function isActive(sel: SavedSelection, target: SavedSelection): boolean {
  if (sel.kind !== target.kind) return false
  if (sel.kind === 'all' || sel.kind === 'untagged') return true
  if (sel.kind === 'tag' && target.kind === 'tag') return sel.id === target.id
  if (sel.kind === 'source' && target.kind === 'source') return sel.key === target.key
  return false
}

export default function SavedTagChipBar({ tags, sources, selection, onSelect }: Props) {
  return (
    <div
      style={{
        position: 'sticky',
        top: 0,
        zIndex: 50,
        background: 'var(--bg)',
        margin: '0 -12px 12px',
        padding: '8px 12px',
        borderBottom: '1px solid var(--border)',
        overflowX: 'auto',
        WebkitOverflowScrolling: 'touch',
      }}
    >
      <div style={{ display: 'flex', gap: 8, flexWrap: 'nowrap', minWidth: 'min-content' }}>
        <Chip active={isActive(selection, { kind: 'all' })} onClick={() => onSelect({ kind: 'all' })}>
          全部
        </Chip>
        <Chip active={isActive(selection, { kind: 'untagged' })} onClick={() => onSelect({ kind: 'untagged' })}>
          无 tag
        </Chip>
        {tags.length > 0 && <Divider />}
        {tags.map(t => (
          <Chip
            key={`tag-${t.id}`}
            active={isActive(selection, { kind: 'tag', id: t.id })}
            onClick={() => onSelect({ kind: 'tag', id: t.id })}
          >
            {t.name}
            <span style={{ opacity: 0.6, marginLeft: 4, fontSize: 11 }}>{t.article_count}</span>
          </Chip>
        ))}
        {sources.length > 0 && <Divider />}
        {sources.map(s => (
          <Chip
            key={`src-${s.key}`}
            active={isActive(selection, { kind: 'source', key: s.key, title: s.title })}
            onClick={() => onSelect({ kind: 'source', key: s.key, title: s.title })}
            title={s.title}
          >
            {s.title}
            <span style={{ opacity: 0.6, marginLeft: 4, fontSize: 11 }}>{s.count}</span>
          </Chip>
        ))}
      </div>
    </div>
  )
}

function Chip({
  active, onClick, children, title,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
  title?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      style={{
        flexShrink: 0,
        padding: '6px 12px',
        height: 'auto',
        minHeight: 32,
        borderRadius: 999,
        border: '1px solid ' + (active ? 'var(--accent)' : 'var(--border)'),
        background: active ? 'var(--accent-soft)' : 'var(--surface)',
        color: active ? 'var(--accent)' : 'var(--fg)',
        fontSize: 13,
        fontWeight: active ? 600 : 400,
        whiteSpace: 'nowrap',
      }}
    >
      {children}
    </button>
  )
}

function Divider() {
  return (
    <div
      aria-hidden="true"
      style={{
        flexShrink: 0,
        width: 1,
        margin: '4px 4px',
        background: 'var(--border)',
      }}
    />
  )
}
```

- [ ] **Step 2: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/components/SavedTagChipBar.tsx
git commit -m "$(cat <<'EOF'
feat(mobile): SavedTagChipBar for SavedPage on phone

Horizontal scrolling sticky chip strip. Sections (all/untagged | tags
| sources) separated by 1px dividers. Active chip highlighted with
accent-soft background.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: SavedPage breakpoint branching

**Files:**
- Modify: `frontend/src/pages/SavedPage.tsx`

- [ ] **Step 1: Import the new component + hook**

Open `frontend/src/pages/SavedPage.tsx`. Replace the import block (lines 1–17) with:

```tsx
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  GetSavedParams,
  SavedItem,
  SavedListResponse,
  UserTag,
  getSaved,
  listTags,
} from '../api/client'
import ArticleCard from '../components/ArticleCard'
import SavedTagSidebar, {
  SavedSelection,
  SavedSourceRow,
} from '../components/SavedTagSidebar'
import SavedTagChipBar from '../components/SavedTagChipBar'
import { usePlayer } from '../player/PlayerContext'
import { reportClick } from '../hooks/useExposureTracking'
import { useBreakpoint } from '../hooks/useBreakpoint'
```

- [ ] **Step 2: Branch render on breakpoint**

Find the final `return ( ... )` block in `SavedPage.tsx` (lines 214–277). Add `const bp = useBreakpoint()` near the top of the component body (right after `const navigate = useNavigate()` is fine — but search for an existing line `const navigate = useNavigate()` and add right after it):

```tsx
  const navigate = useNavigate()
  const bp = useBreakpoint()
```

Then replace the entire `return ( ... )` block with:

```tsx
  if (bp === 'phone') {
    return (
      <div>
        <SavedTagChipBar
          tags={tags}
          sources={sources}
          selection={selection}
          onSelect={handleSelect}
        />
        <div className="flex-between mb-2">
          <h2 style={{ margin: 0, fontSize: 18 }}>{headerLabel}</h2>
          <span className="text-muted text-sm">共 {total} 篇</span>
        </div>
        {renderList()}
      </div>
    )
  }

  return (
    <div
      style={{
        display: 'flex',
        gap: 0,
        alignItems: 'flex-start',
        minHeight: 'calc(100vh - 120px)',
      }}
    >
      <SavedTagSidebar
        tags={tags}
        sources={sources}
        selection={selection}
        onSelect={handleSelect}
      />
      <section style={{ flex: 1, minWidth: 0, paddingLeft: 16 }}>
        <div className="flex-between mb-2">
          <h2 style={{ margin: 0 }}>{headerLabel}</h2>
          <span className="text-muted text-sm">共 {total} 篇</span>
        </div>
        {renderList()}
      </section>
    </div>
  )
```

- [ ] **Step 3: Extract `renderList()` helper**

Both branches reuse the list rendering. Add this helper just before the `return` statements, inside the component body:

```tsx
  const renderList = () => {
    if (loading) return <div className="card">Loading...</div>
    if (items.length === 0) return <div className="card text-muted">暂无收藏文章</div>
    return (
      <>
        {items.map((it, idx) => (
          <ArticleCard
            key={it.id}
            article={it}
            manualTags={it.manual_tags}
            isRead={!!it.is_read}
            isFocused={focusedIdx === idx}
            idx={idx}
            onPlay={player.playArticle}
            formatDate={formatDate}
            stripMarkdown={stripMarkdown}
            onOpen={openArticle}
            onFocus={setFocusedIdx}
            sourceLabel={it.effective_source?.title}
          />
        ))}
        {hasMore && (
          <div style={{ textAlign: 'center', padding: 12 }}>
            <button
              className="secondary"
              onClick={loadMore}
              disabled={loadingMore}
              style={{ fontSize: 13, padding: '6px 16px' }}
            >
              {loadingMore ? '加载中...' : '加载更多'}
            </button>
          </div>
        )}
        {!hasMore && items.length > 0 && (
          <div style={{ textAlign: 'center', padding: 16, color: 'var(--fg-muted)', fontSize: 13 }}>
            — 已加载全部收藏 —
          </div>
        )}
      </>
    )
  }
```

(This consolidates code duplicated between the two return branches.)

- [ ] **Step 4: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/pages/SavedPage.tsx
git commit -m "$(cat <<'EOF'
feat(mobile): SavedPage uses chip bar on phone, sidebar otherwise

useBreakpoint branches between SavedTagChipBar (horizontal sticky chips
on phone) and SavedTagSidebar (left column on tablet/desktop).
Extracted renderList() helper to avoid duplicating the list JSX.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Reading mode chrome wiring

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`
- Modify: `frontend/src/components/ReadingLayout.tsx`

- [ ] **Step 1: Call `useReadingChrome` from ArticlePage**

In `frontend/src/pages/ArticlePage.tsx`, add the hook import near the existing hook imports:

```tsx
import { useReadingChrome } from '../hooks/useReadingChrome'
```

Then in the `ArticlePage` component body, find where `reader = useReaderSettings()` (or similar) is called and add right after:

```tsx
  const { toggle: toggleReadingChrome } = useReadingChrome(reader.mode === 'reading')
```

This activates body classes only when in reading mode.

- [ ] **Step 2: Pass toggle into ReadingLayout**

Find the existing `<ReadingLayout ... />` call (around line 619). Add the `onTapBody` prop:

```tsx
return (
  <ReadingLayout
    article={{ ... }}
    fontSize={reader.fontSize}
    fontFamily={reader.fontFamily}
    onExit={() => reader.setMode('normal')}
    onFontSize={reader.setFontSize}
    onFontFamily={reader.setFontFamily}
    onTapBody={toggleReadingChrome}
  />
)
```

- [ ] **Step 3: Accept and wire `onTapBody` in ReadingLayout**

Open `frontend/src/components/ReadingLayout.tsx`. Replace the `Props` type and the `ReadingLayout` function with:

```tsx
type Props = {
  article: ArticleLite
  fontSize: number
  fontFamily: ReaderFontFamily
  onExit: () => void
  onFontSize: (n: number) => void
  onFontFamily: (f: ReaderFontFamily) => void
  onTapBody?: () => void
}

export default function ReadingLayout(props: Props) {
  const { article, fontSize, fontFamily, onExit, onTapBody } = props

  const [summaryOpen, setSummaryOpen] = useState(false)

  const fmtDate = (s: string | null) => s ? new Date(s).toLocaleString('zh-CN') : ''
  const ff = fontFamily === 'serif'
    ? 'var(--font-serif)'
    : 'var(--font-sans)'

  // Tap on the article body (excluding links / buttons / inputs / details)
  // toggles chrome via the parent-supplied callback.
  const handleArticleClick: React.MouseEventHandler<HTMLElement> = (e) => {
    if (!onTapBody) return
    const target = e.target as HTMLElement
    if (target.closest('a, button, input, textarea, select, summary, [role="button"]')) return
    onTapBody()
  }

  return (
    <div className="reading-layout" style={{ fontFamily: ff }}>
      <div className="reading-toolbar">
        <button className="reading-exit" onClick={onExit} title="退出阅读模式 (Esc / r)">← 退出阅读模式</button>
      </div>

      <article
        className="reading-article"
        style={{ fontSize }}
        onClick={handleArticleClick}
      >
        <h1 className="reading-title">{article.title}</h1>
        <div className="reading-meta">
          <span>{fmtDate(article.published_at)}</span>
          {article.word_count > 0 && <span> · {article.word_count} 字</span>}
          {article.reading_minutes > 0 && <span> · 约 {article.reading_minutes} 分钟</span>}
          <span> · </span>
          <a href={article.url} target="_blank" rel="noopener noreferrer">原文链接</a>
        </div>

        {(article.summary_brief || article.summary_detailed) && (
          <div className="reading-summary">
            <button className="reading-summary-toggle" onClick={() => setSummaryOpen(o => !o)}>
              {summaryOpen ? '▼' : '▶'} AI 摘要
            </button>
            {summaryOpen && (
              <div className="reading-summary-body">
                {article.summary_brief && <ReactMarkdown>{article.summary_brief}</ReactMarkdown>}
                {article.summary_detailed && (
                  <>
                    <hr />
                    <ReactMarkdown>{article.summary_detailed}</ReactMarkdown>
                  </>
                )}
              </div>
            )}
          </div>
        )}

        {article.content
          ? <MarkdownArticle source={article.content} />
          : <div className="text-muted">暂无内容</div>
        }
      </article>

      <ReaderSettingsPanel
        fontSize={fontSize}
        fontFamily={fontFamily}
        onFontSize={props.onFontSize}
        onFontFamily={props.onFontFamily}
      />
    </div>
  )
}
```

- [ ] **Step 4: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/pages/ArticlePage.tsx frontend/src/components/ReadingLayout.tsx
git commit -m "$(cat <<'EOF'
feat(mobile): wire reading-mode chrome (tap toggle + scroll direction)

ArticlePage calls useReadingChrome when reader.mode === 'reading'.
ReadingLayout's article body is now a tap target that calls the
hook's toggle (excluding links/buttons/inputs).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: ReaderSettingsPanel bottom-sheet variant

**Files:**
- Modify: `frontend/src/components/ReaderSettingsPanel.tsx`

- [ ] **Step 1: Branch on breakpoint**

Replace the entire body of `frontend/src/components/ReaderSettingsPanel.tsx` with:

```tsx
import { useEffect, useRef, useState } from 'react'
import type { ReaderFontFamily } from '../hooks/useReaderSettings'
import { useBreakpoint } from '../hooks/useBreakpoint'

type Props = {
  fontSize: number
  fontFamily: ReaderFontFamily
  onFontSize: (n: number) => void
  onFontFamily: (f: ReaderFontFamily) => void
}

export default function ReaderSettingsPanel({
  fontSize, fontFamily, onFontSize, onFontFamily,
}: Props) {
  const [open, setOpen] = useState(false)
  const bp = useBreakpoint()
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setOpen(false) }
    document.addEventListener('mousedown', onClick)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onClick)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const body = (
    <>
      <div className="text-sm text-muted" style={{ marginBottom: 6 }}>字号</div>
      <div className="flex gap-2" style={{ marginBottom: 14 }}>
        <button className="btn-ghost btn-sm" onClick={() => onFontSize(fontSize - 1)} disabled={fontSize <= 12} style={{ flex: 1 }}>A−</button>
        <div style={{ flex: 2, padding: 4, textAlign: 'center', border: '1px solid var(--border)', borderRadius: 4, fontSize: 12 }}>
          {fontSize} px
        </div>
        <button className="btn-ghost btn-sm" onClick={() => onFontSize(fontSize + 1)} disabled={fontSize >= 24} style={{ flex: 1, fontSize: 14 }}>A+</button>
      </div>

      <div className="text-sm text-muted" style={{ marginBottom: 6 }}>字体</div>
      <div className="flex gap-2">
        <button
          className={fontFamily === 'sans' ? 'btn-sm' : 'btn-ghost btn-sm'}
          onClick={() => onFontFamily('sans')}
          style={{ flex: 1 }}
        >Sans</button>
        <button
          className={fontFamily === 'serif' ? 'btn-sm' : 'btn-ghost btn-sm'}
          onClick={() => onFontFamily('serif')}
          style={{ flex: 1, fontFamily: 'var(--font-serif)' }}
        >Serif</button>
      </div>
    </>
  )

  const fab = (
    <button
      onClick={() => setOpen(o => !o)}
      aria-label="阅读设置"
      title="阅读设置"
      style={{ width: 48, height: 48, borderRadius: '50%', padding: 0, fontWeight: 700 }}
    >Aa</button>
  )

  if (bp === 'phone') {
    return (
      <div ref={ref} style={{
        position: 'fixed',
        right: 16,
        bottom: 'calc(56px + env(safe-area-inset-bottom) + 16px)',
        zIndex: 1100,
      }}>
        {fab}
        {open && (
          <>
            <div
              onClick={() => setOpen(false)}
              style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.35)', zIndex: 1199 }}
            />
            <div
              role="dialog"
              aria-label="阅读设置"
              style={{
                position: 'fixed',
                left: 0, right: 0, bottom: 0,
                background: 'var(--surface)',
                borderTop: '1px solid var(--border)',
                borderTopLeftRadius: 16,
                borderTopRightRadius: 16,
                padding: '8px 16px calc(env(safe-area-inset-bottom) + 16px)',
                zIndex: 1200,
                boxShadow: '0 -4px 16px rgba(0,0,0,0.18)',
              }}
            >
              <div
                aria-hidden="true"
                style={{
                  width: 36, height: 4, borderRadius: 2,
                  background: 'var(--border)',
                  margin: '8px auto 14px',
                }}
              />
              {body}
            </div>
          </>
        )}
      </div>
    )
  }

  return (
    <div ref={ref} style={{ position: 'fixed', right: 24, bottom: 24, zIndex: 1100 }}>
      {open && (
        <div style={{
          position: 'absolute',
          right: 0,
          bottom: 64,
          width: 240,
          background: 'var(--surface)',
          color: 'var(--fg)',
          border: '1px solid var(--border)',
          borderRadius: 10,
          padding: 14,
          boxShadow: '0 8px 24px rgba(0,0,0,0.18)',
        }}>
          {body}
        </div>
      )}
      {fab}
    </div>
  )
}
```

- [ ] **Step 2: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/components/ReaderSettingsPanel.tsx
git commit -m "$(cat <<'EOF'
feat(mobile): ReaderSettingsPanel uses bottom sheet on phone

Phone: FAB at fixed right above MiniPlayer; tap opens a bottom-edge
sheet with backdrop. Tablet/desktop: existing right-anchored popover.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Login + Register input sizing

**Files:**
- Modify: `frontend/src/pages/LoginPage.tsx`
- Modify: `frontend/src/pages/RegisterPage.tsx`

- [ ] **Step 1: Read existing LoginPage to identify the card + submit selectors**

```bash
cd /Users/bytedance/mygit/rss-pal && grep -n "submit\|button\|className=\"card" frontend/src/pages/LoginPage.tsx frontend/src/pages/RegisterPage.tsx
```

Expected: a `<div className="card">` wrapper with `maxWidth: 400` style (or similar) and one or two `<button type="submit">`.

- [ ] **Step 2: Make card full-width on phone**

For both `LoginPage.tsx` and `RegisterPage.tsx`, find the main wrapper `style={{ maxWidth: 400, margin: '40px auto' }}` (exact values may differ; locate the centered card wrapper). Update to:

```tsx
style={{ maxWidth: 400, margin: '40px auto', padding: '0 16px', width: '100%' }}
```

If the wrapper doesn't already have explicit width handling, the addition is `padding: '0 16px'` for phone-side gutters.

- [ ] **Step 3: Submit button height (no per-page change required)**

The global rule added in Task 7 already sets `min-height: 44px` on standard buttons at ≤640 px. Verify the submit button class is NOT in the exclusion list (`.btn-sm`, `.settings-tab`, etc.) — for a plain `<button type="submit">` with no special class, it inherits the 44 px rule.

No code change needed beyond Step 2.

- [ ] **Step 4: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/pages/LoginPage.tsx frontend/src/pages/RegisterPage.tsx
git commit -m "$(cat <<'EOF'
feat(mobile): LoginPage + RegisterPage phone gutters

Card wrapper gets padding: 0 16px and width: 100% so it doesn't hug
the edge on small viewports. Input/button sizing handled by global
CSS rules (16px font, 44px min-height).

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Global fallback CSS for non-priority pages

**Files:**
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Add `≤ 1024px` shared block**

Find the existing `@media (max-width: 1024px)` block from Task 7 (the one with `.desktop-nav { display: none !important }`). Replace it with:

```css
@media (max-width: 1024px) {
  .desktop-nav { display: none !important; }
  #root > div > header { margin-bottom: 12px; }

  /* Inputs: 16px font prevents iOS Safari auto-zoom on focus. */
  input, textarea, select { font-size: 16px; }

  /* Generic toolbar reflow: anything with .flex-between or inline-style
     flex toolbars wraps instead of overflowing. */
  .flex-between { flex-wrap: wrap; gap: 8px; }

  /* Settings-style tabs were already overflow-x:auto; reinforce that here
     so unrelated horizontal tab strips inherit the same behavior. */
  .settings-tabs { overflow-x: auto; }
}
```

- [ ] **Step 2: Refine `≤ 640px` block**

Find the existing `@media (max-width: 640px)` block. Replace it with:

```css
@media (max-width: 640px) {
  #root { padding: 12px; }
  .card { padding: 12px; }

  /* Touch targets: bump non-chip buttons to 44px. */
  button:not(.btn-sm):not(.settings-tab):not(.theme-swatch):not(.tag-chip-remove):not(.tag-add-btn):not(.reading-summary-toggle):not(.rec-feedback-btn):not(.rec-panel-header):not(.saved-row):not(.tag-suggest-dropdown button):not(.saved-mode-toggle button):not(.mobile-tab-link) { min-height: 44px; }

  /* Single-column reflow for auto-fit grids that have explicit minmax > 1fr. */
  .theme-grid { grid-template-columns: 1fr 1fr; }

  /* Reading article: tighter horizontal padding. */
  .reading-article { padding: 8px 16px 96px; }
  .reading-toolbar { padding: 12px 16px; }
}
```

- [ ] **Step 3: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/index.css
git commit -m "$(cat <<'EOF'
feat(mobile): fallback CSS for non-priority pages (≤1024 + ≤640)

≤1024px: input/textarea/select at 16px (prevent iOS focus-zoom),
generic flex-between wraps, settings-tabs overflow-x.
≤640px: 44px touch targets (with chip-style exclusions), theme-grid
2-col, reading article tighter padding.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: ArticleListPage + ArticlePage action row wrap

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx`
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 1: ArticleListPage — confirm toolbar uses `.flex-between`**

```bash
cd /Users/bytedance/mygit/rss-pal && grep -n "flex-between\|toolbar\|filter" frontend/src/pages/ArticleListPage.tsx | head -10
```

If the top-of-page toolbar uses `className="flex-between"` (or similar inline flex styling), the global `≤1024px` rule from Task 14 already makes it wrap. No per-page change needed.

If the toolbar uses inline styles only (e.g., `style={{ display: 'flex', gap: 8 }}`), locate that container and add a className `mobile-wrap`:

```tsx
<div className="mobile-wrap" style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
```

The key addition is `flexWrap: 'wrap'` in the inline style. (Add this only if needed after inspection.)

- [ ] **Step 2: ArticlePage — wrap the actions row**

Open `frontend/src/pages/ArticlePage.tsx` and find the actions row containing the read/unread + 阅读模式 buttons (around line 700–765, look for the `<div>` immediately wrapping `📖 阅读模式` and `↩ 标记未读` / `✓ 标记已读`).

Locate the parent `<div style={{ ... }}>` that contains those buttons. Add `flexWrap: 'wrap'` and `gap: 8` to its inline style:

```tsx
<div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
```

(Exact existing style may include `justifyContent: 'flex-end'` or similar — preserve all existing keys; only add `flexWrap: 'wrap'` and ensure `gap` is set to at least 8.)

- [ ] **Step 3: Type-check + commit**

```bash
cd frontend && npx tsc --noEmit
```

```bash
git add frontend/src/pages/ArticleListPage.tsx frontend/src/pages/ArticlePage.tsx
git commit -m "$(cat <<'EOF'
feat(mobile): wrap toolbar + action rows on narrow viewports

Inline flex containers in ArticleListPage toolbar and ArticlePage
action row get flexWrap:'wrap' so controls reflow instead of
overflowing horizontally on phone/tablet.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 16: Full build + Docker rebuild

**Files:** none new

- [ ] **Step 1: Full build (catches anything `tsc --noEmit` missed)**

```bash
cd /Users/bytedance/mygit/rss-pal/frontend && npm run build
```

Expected: builds with no errors. Warnings about unused vars OK to fix if any surface.

- [ ] **Step 2: Rebuild the Docker frontend container so nginx serves new assets**

```bash
cd /Users/bytedance/mygit/rss-pal && docker-compose up -d --build frontend
```

Expected: image rebuilds, container restarts. Verify with:

```bash
docker-compose ps frontend
docker-compose logs --tail=30 frontend
```

- [ ] **Step 3: Smoke-check by curling the served HTML**

```bash
curl -s http://localhost:5173 | grep -o 'viewport-fit=cover' || echo "MISSING viewport-fit"
```

Expected: prints `viewport-fit=cover`. (Port may differ; check `docker-compose.yml` if 5173 doesn't respond.)

---

## Task 17: Visual verification via agent-browser

**Files:** none

- [ ] **Step 1: Determine frontend URL**

Read `docker-compose.yml` and `frontend/nginx.conf` to find the served port. Default port in this project is exposed via the `frontend` service.

```bash
cd /Users/bytedance/mygit/rss-pal && grep -A3 "frontend:" docker-compose.yml | grep -E "ports|published"
```

- [ ] **Step 2: Login session capture (manual one-time)**

Run agent-browser headed to log in once and save the session. The plan executor should:

```bash
agent-browser --session-name rsspal --headed
# After login, close the browser
```

(If the user already has a session, skip.)

- [ ] **Step 3: Capture screenshots at three viewports**

For each viewport size in `375x667` (iPhone), `768x1024` (iPad portrait), `1280x800` (desktop), navigate the priority routes and capture screenshots:

- `/login`
- `/articles`
- `/articles/<some-id>` (any article from the list)
- `/saved`
- Reading mode toggle inside the article page

Use agent-browser:

```bash
agent-browser --session-name rsspal --viewport 375x667 \
  --url http://localhost:5173/articles \
  --screenshot /tmp/rsspal-mobile-articles.png

# repeat for the other URLs and viewports
```

- [ ] **Step 4: Visual acceptance review**

For each screenshot, verify against the spec's acceptance criteria:

1. No horizontal scroll at 375 px.
2. Bottom tab bar visible on 375 + 768; hidden on 1280.
3. Tap targets visibly ≥ 44 px on 375 (estimate from screenshot).
4. Reading mode: top header AND bottom tab hidden on entry.
5. MiniPlayer (if visible) does not overlap the tab bar — it sits above it.
6. SavedPage: chip bar on 375, sidebar on 768 and 1280.

If any criterion fails, note the issue and create a fix task. If all pass, mark this task complete and proceed.

- [ ] **Step 5: Commit screenshots (optional)**

If reviewer wants to attach to PR, copy screenshots into `docs/superpowers/specs/screenshots/2026-05-12-mobile/` and commit. Otherwise skip — visual sign-off is enough.

---

## Task 18: Mark PR ready + final summary

**Files:** none

- [ ] **Step 1: Push final commits**

```bash
cd /Users/bytedance/mygit/rss-pal && git push
```

Expected: all task commits push to `origin/feature/mobile-adaptation`.

- [ ] **Step 2: Mark PR ready for review (un-draft)**

```bash
gh pr ready 21
```

Expected: PR #21 transitions from Draft to Ready.

- [ ] **Step 3: Final summary**

Print a short summary to the user including:
- PR URL
- Number of commits on the branch
- Notable risks observed during implementation (e.g., any new dependencies, any deviations from spec).

---

## Self-Review

**Spec coverage:**
- §3 Breakpoints → Task 2 (`useBreakpoint`), Task 14 (CSS blocks).
- §4.1 New CSS strategy → Task 14.
- §4.2 `useBreakpoint` hook → Task 2.
- §4.3 `useReadingChrome` hook → Task 3, wired in Task 11.
- §4.4 `MobileTabBar` + `MoreSheet` → Tasks 5, 6, 7.
- §4.5 Reading mode chrome behavior → Tasks 3, 4, 11.
- §4.6 MiniPlayer stacking → Task 8 + `--bottom-chrome` in Task 7.
- §5.1 Priority pages: ArticleListPage (Task 15) / ArticlePage (Tasks 11, 15) / SavedPage (Task 10) / Login + Register (Task 13) / Reading mode (Tasks 4, 11) / MiniPlayer (Task 8).
- §5.2 Fallback pages → Task 14 (generic rules).
- §6 iOS WebKit defaults → Task 1.
- §7 Data flow → No backend, covered by Tasks 2, 3, 7.
- §8 Testing → Tasks 16 (build + Docker), 17 (visual verification).
- §9 Risks → covered by hook implementation choices (Task 2 SSR-safe default; Task 3 30 px threshold; Task 1 100dvh fallback).

**Placeholder scan:** No TBD/TODO entries. Step 2 of Task 15 says "Locate the parent `<div>`" — this is unavoidable because the exact line is one of several similar containers; the step describes the unambiguous landmark (read/unread + 阅读模式 buttons). Step 1 of Task 15 has an optional branch ("only if needed after inspection") which is acceptable for a conditional cleanup.

**Type consistency:**
- `Breakpoint` type used identically across hook + consumers.
- `SavedSelection`, `SavedSourceRow` imported from existing `SavedTagSidebar` and reused in `SavedTagChipBar` (no new type).
- `useReadingChrome` returns `{ chromeVisible, toggle }`; consumers use `toggle` only.
- `MoreSheet` accepts `{ open, onClose, onLogout }`; `MobileTabBar` passes through to `MoreSheet` correctly.
- CSS `.mobile-tab-bar` class on the `<nav>` in `MobileTabBar.tsx` matches the selector used in Task 4's transition rule.

Plan is consistent and complete.
