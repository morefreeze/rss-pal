# UI Theme Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current generic SaaS-blue UI with four switchable reading-focused themes (Paper / Quiet / Pearl / Night), unify buttons into 3 variants, and add a sub-tabbed Settings page housing the theme picker.

**Architecture:** A new `util/theme.ts` module owns the global theme as `body[data-theme='paper|quiet|pearl|night']` plus localStorage. The CSS is rewritten around CSS-variable tokens (`--bg --surface --fg --accent --border --link --code-bg`) so every theme is a single block. The existing reader-mode `data-reader-bg` system is folded into the global theme so there is no double toggle. Settings page becomes tabbed (外观/账号/AI/工具) with URL-hash routing.

**Tech Stack:** React 18, React Router 6, Vite, TypeScript. No new dependencies. The project has no frontend test framework, so verification is `tsc` type-check (via `npm run build`) plus visual check via `docker-compose up -d --build frontend` and the `agent-browser` CLI per the user's CLAUDE.md.

**Spec:** `docs/superpowers/specs/2026-05-11-ui-theme-redesign-design.md`

---

## File map

| File                                              | Action  | Responsibility                                            |
|---------------------------------------------------|---------|-----------------------------------------------------------|
| `frontend/src/util/theme.ts`                      | CREATE  | Theme state: `Theme` type, `getTheme`, `setTheme`, `useTheme` hook |
| `frontend/src/main.tsx`                           | MODIFY  | Apply initial theme before React mounts                   |
| `frontend/src/index.css`                          | REWRITE | Tokens, theme blocks, button classes, `.row`, tab styles  |
| `frontend/src/components/Layout.tsx`              | MODIFY  | Replace inline-hex header styles with token classes       |
| `frontend/src/pages/SettingsPage.tsx`             | MODIFY  | Add sub-tab strip + AppearanceTab section                 |
| `frontend/src/hooks/useReaderSettings.ts`         | MODIFY  | Remove `bgTheme` field; keep fontSize/fontFamily/confetti |
| `frontend/src/components/ReadingLayout.tsx`       | MODIFY  | Drop `data-reader-bg` body toggle; drop bgTheme prop      |
| `frontend/src/components/ReaderSettingsPanel.tsx` | MODIFY  | Drop bg-palette swatches; keep font-size/family controls  |
| `frontend/src/pages/ArticlePage.tsx`              | MODIFY  | Drop bgTheme prop passed to ReadingLayout                 |
| `frontend/src/pages/SharePage.tsx`                | MODIFY  | Drop bgTheme prop passed to ReadingLayout (if used)       |
| `frontend/src/components/ArticleCard.tsx`         | MODIFY  | Migrate inline-style buttons → `.btn-ghost .btn-sm`       |
| `frontend/src/components/RecommendationsCard.tsx` | MODIFY  | Migrate inline-style buttons → token classes              |
| `frontend/src/components/ArticlePlayerCard.tsx`   | MODIFY  | Migrate inline-style buttons → token classes              |
| `frontend/src/components/FeedHealthTable.tsx`     | MODIFY  | Migrate inline-style buttons → token classes              |
| `frontend/src/components/SavedTagSidebar.tsx`     | MODIFY  | Migrate inline-style buttons (sweep)                      |
| `frontend/src/components/TagBar.tsx`              | MODIFY  | Migrate inline-style buttons (sweep)                      |

---

## Task 1: Theme module + boot-time application

**Files:**
- Create: `frontend/src/util/theme.ts`
- Modify: `frontend/src/main.tsx`

- [ ] **Step 1: Create `frontend/src/util/theme.ts`**

```ts
import { useEffect, useState } from 'react'

export type Theme = 'paper' | 'quiet' | 'pearl' | 'night'

const STORAGE_KEY = 'rsspal:theme'
const DEFAULT: Theme = 'paper'
const EVENT = 'rsspal-theme-change'

const VALID: Record<Theme, true> = { paper: true, quiet: true, pearl: true, night: true }

function isTheme(v: unknown): v is Theme {
  return typeof v === 'string' && Object.prototype.hasOwnProperty.call(VALID, v)
}

export function getTheme(): Theme {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (isTheme(raw)) return raw
    // Best-effort migration from the legacy reader-mode bgTheme key so
    // users who picked sepia/dark there don't lose continuity.
    const legacyRaw = localStorage.getItem('rsspal:reader-settings')
    if (legacyRaw) {
      try {
        const parsed = JSON.parse(legacyRaw) as { bgTheme?: string }
        if (parsed.bgTheme === 'sepia') return 'paper'
        if (parsed.bgTheme === 'dark') return 'night'
        if (parsed.bgTheme === 'gray') return 'pearl'
      } catch { /* ignore */ }
    }
  } catch { /* ignore */ }
  return DEFAULT
}

export function setTheme(t: Theme): void {
  document.body.setAttribute('data-theme', t)
  try { localStorage.setItem(STORAGE_KEY, t) } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent(EVENT, { detail: t }))
}

export function applyInitialTheme(): void {
  document.body.setAttribute('data-theme', getTheme())
}

export function useTheme(): [Theme, (t: Theme) => void] {
  const [theme, setThemeState] = useState<Theme>(() => getTheme())
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<Theme>).detail
      if (isTheme(detail)) setThemeState(detail)
    }
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY && isTheme(e.newValue)) setThemeState(e.newValue)
    }
    window.addEventListener(EVENT, handler)
    window.addEventListener('storage', onStorage)
    return () => {
      window.removeEventListener(EVENT, handler)
      window.removeEventListener('storage', onStorage)
    }
  }, [])
  return [theme, setTheme]
}
```

- [ ] **Step 2: Update `frontend/src/main.tsx`**

Replace the entire file with:

```tsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { applyInitialTheme } from './util/theme'
import './index.css'

applyInitialTheme()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
```

- [ ] **Step 3: Type-check**

Run: `cd frontend && npm run build`
Expected: PASS (tsc + vite build emit no errors). Build is allowed to take 30-60s.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/util/theme.ts frontend/src/main.tsx
git commit -m "feat(ui): theme state module with localStorage + boot-time apply"
```

---

## Task 2: Rewrite `index.css` with token system

**Files:**
- Modify (rewrite): `frontend/src/index.css`

- [ ] **Step 1: Replace `frontend/src/index.css` with the new token-based stylesheet**

Use this exact content (overwrite the whole file):

```css
/* === Reset === */
*, *::before, *::after { box-sizing: border-box; }
* { margin: 0; padding: 0; }

/* === Tokens === */
:root {
  --font-sans: Inter, "Source Han Sans SC", system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif;
  --font-serif: "Source Han Serif SC", "Noto Serif SC", "Crimson Pro", Georgia, "Songti SC", serif;
  --font-mono: Menlo, Monaco, "Courier New", monospace;
  --radius: 6px;
  --content-max: 760px;

  font-family: var(--font-sans);
  font-weight: 400;
  line-height: 1.65;
}

body[data-theme='paper'] {
  --bg: #f5edd6;
  --surface: #ede4c6;
  --surface-hover: #e4d9b3;
  --fg: #3a2f1a;
  --fg-muted: #7a6f55;
  --accent: #5b3b00;
  --accent-soft: rgba(91, 59, 0, 0.18);
  --accent-fg: #f5edd6;
  --border: #d9cdaa;
  --code-bg: #ebe2c8;
  --link: #5b3b00;
}
body[data-theme='quiet'] {
  --bg: #fafaf7;
  --surface: #ffffff;
  --surface-hover: #f3f3ee;
  --fg: #1f2933;
  --fg-muted: #5a6473;
  --accent: #3a5a40;
  --accent-soft: rgba(58, 90, 64, 0.18);
  --accent-fg: #ffffff;
  --border: #e3e3de;
  --code-bg: #f3f3ee;
  --link: #3a5a40;
}
body[data-theme='pearl'] {
  --bg: #ebebeb;
  --surface: #ffffff;
  --surface-hover: #f3f3f3;
  --fg: #262626;
  --fg-muted: #666666;
  --accent: #0066cc;
  --accent-soft: rgba(0, 102, 204, 0.18);
  --accent-fg: #ffffff;
  --border: #d4d4d4;
  --code-bg: #f3f3f3;
  --link: #0066cc;
}
body[data-theme='night'] {
  --bg: #1a1a1a;
  --surface: #242424;
  --surface-hover: #2e2e2e;
  --fg: #d4d4d4;
  --fg-muted: #888888;
  --accent: #7eb6ff;
  --accent-soft: rgba(126, 182, 255, 0.20);
  --accent-fg: #1a1a1a;
  --border: #333333;
  --code-bg: #262626;
  --link: #7eb6ff;
}

/* Fallback when no theme attr yet (pre-boot) — Paper. */
body:not([data-theme]) {
  --bg: #f5edd6; --surface: #ede4c6; --surface-hover: #e4d9b3;
  --fg: #3a2f1a; --fg-muted: #7a6f55;
  --accent: #5b3b00; --accent-soft: rgba(91, 59, 0, 0.18); --accent-fg: #f5edd6;
  --border: #d9cdaa; --code-bg: #ebe2c8; --link: #5b3b00;
}

/* === Element defaults === */
body {
  min-height: 100vh;
  background: var(--bg);
  color: var(--fg);
  transition: background-color 0.15s ease, color 0.15s ease;
}

#root {
  max-width: 1200px;
  margin: 0 auto;
  padding: 24px;
}

.content {
  max-width: var(--content-max);
  margin: 0 auto;
}

a { color: var(--link); text-decoration: none; }
a:hover { text-decoration: underline; }

h1, h2, h3 {
  font-family: var(--font-serif);
  font-weight: 600;
  line-height: 1.3;
  color: var(--fg);
}

input, textarea {
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  font-size: 14px;
  width: 100%;
  background: var(--surface);
  color: var(--fg);
  font-family: inherit;
}
input:focus, textarea:focus {
  outline: 2px solid var(--accent-soft);
  outline-offset: 1px;
  border-color: var(--accent);
}

/* === Buttons === */
button {
  cursor: pointer;
  padding: 0 14px;
  height: 32px;
  border: 1px solid transparent;
  border-radius: var(--radius);
  background: var(--accent);
  color: var(--accent-fg);
  font-size: 14px;
  font-weight: 600;
  font-family: inherit;
  line-height: 1;
  transition: background 0.12s ease, border-color 0.12s ease, opacity 0.12s ease;
}
button:hover { opacity: 0.88; }
button:focus-visible {
  outline: 2px solid var(--accent-soft);
  outline-offset: 2px;
}
button:disabled { opacity: 0.45; cursor: not-allowed; }
button:disabled:hover { opacity: 0.45; }

button.secondary,
.btn-ghost {
  background: transparent;
  color: var(--fg);
  border-color: var(--border);
}
button.secondary:hover,
.btn-ghost:hover { background: var(--surface-hover); opacity: 1; }
button.secondary:disabled,
.btn-ghost:disabled {
  background: transparent;
  color: var(--fg-muted);
}

.btn-text {
  background: transparent;
  color: var(--accent);
  border-color: transparent;
  padding: 0 6px;
}
.btn-text:hover { background: var(--surface-hover); opacity: 1; }

.btn-sm {
  height: 26px;
  padding: 0 10px;
  font-size: 12px;
}

/* === Rows & cards === */
.row {
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
}
.row:last-child { border-bottom: none; }

.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px;
  margin-bottom: 12px;
  transition: border-color 0.15s ease, transform 0.1s ease;
}
a.card:hover, div.card[style*="cursor: pointer"]:hover {
  border-color: var(--accent-soft);
  text-decoration: none;
}

/* === Layout utilities === */
.flex { display: flex; }
.flex-between { display: flex; justify-content: space-between; align-items: center; }
.gap-1 { gap: 4px; }
.gap-2 { gap: 8px; }
.gap-3 { gap: 12px; }
.text-muted { color: var(--fg-muted); }
.text-sm { font-size: 12px; }
.text-bold { font-weight: 600; }
.mt-1 { margin-top: 4px; }
.mt-2 { margin-top: 8px; }
.mb-1 { margin-bottom: 4px; }
.mb-2 { margin-bottom: 8px; }

/* === Nav === */
.nav-brand {
  font-family: var(--font-serif);
  font-size: 22px;
  font-weight: 700;
  color: var(--accent);
}
.nav-link {
  color: var(--fg);
  font-size: 14px;
  padding: 6px 10px;
  border-radius: var(--radius);
  text-decoration: none;
}
.nav-link:hover {
  background: var(--surface-hover);
  text-decoration: none;
}
.nav-link.active {
  color: var(--accent);
  background: var(--accent-soft);
  font-weight: 600;
}
.unread-badge {
  background: var(--accent);
  color: var(--accent-fg);
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
  padding: 1px 5px;
  min-width: 18px;
  text-align: center;
  line-height: 16px;
}

/* === Settings sub-tabs === */
.settings-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 20px;
  overflow-x: auto;
}
.settings-tab {
  background: transparent;
  border: none;
  border-bottom: 2px solid transparent;
  border-radius: 0;
  color: var(--fg-muted);
  padding: 10px 14px;
  height: auto;
  font-weight: 500;
  margin-bottom: -1px;
  white-space: nowrap;
  flex-shrink: 0;
}
.settings-tab:hover { color: var(--fg); background: transparent; opacity: 1; }
.settings-tab.active {
  color: var(--accent);
  border-bottom-color: var(--accent);
  font-weight: 600;
}

/* === Theme swatch grid === */
.theme-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}
.theme-swatch {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 8px;
  padding: 10px;
  background: var(--surface);
  color: var(--fg);
  border: 2px solid var(--border);
  border-radius: 10px;
  height: auto;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  text-align: center;
}
.theme-swatch:hover { background: var(--surface-hover); opacity: 1; }
.theme-swatch.active { border-color: var(--accent); }
.theme-swatch-preview {
  width: 100%;
  height: 64px;
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 8px;
  border: 1px solid rgba(0,0,0,0.06);
}
.theme-swatch-preview .swatch-title {
  font-size: 13px;
  font-family: var(--font-serif);
  font-weight: 600;
}
.theme-swatch-preview .swatch-dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  align-self: flex-end;
}
.theme-preview {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px;
  background: var(--surface);
  margin-bottom: 12px;
}

/* === Rec panel === */
.rec-panel {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  margin-bottom: 12px;
  overflow: hidden;
}
.rec-panel-header {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 10px 14px;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  color: inherit;
  font: inherit;
  height: auto;
  font-weight: 500;
}
.rec-panel-header:hover { background: var(--surface-hover); opacity: 1; }
.rec-panel-arrow { font-size: 11px; color: var(--fg-muted); width: 12px; display: inline-block; text-align: center; }
.rec-panel-title { margin: 0; font-size: 1rem; font-family: var(--font-sans); font-weight: 600; }

.rec-row {
  display: block;
  padding: 12px 14px;
  border-top: 1px solid var(--border);
  color: inherit;
  text-decoration: none;
  transition: background 0.15s ease;
}
.rec-row:hover { background: var(--surface-hover); text-decoration: none; }

.rec-feedback {
  display: flex;
  gap: 4px;
  align-items: center;
  margin-left: 12px;
  flex-shrink: 0;
  opacity: 0.5;
  transition: opacity 0.15s ease;
}
.rec-row:hover .rec-feedback { opacity: 1; }

.rec-feedback-btn {
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 4px 6px;
  height: auto;
  border-radius: 4px;
  font-size: 14px;
  line-height: 1;
  color: inherit;
}
.rec-feedback-btn:hover { background: var(--accent-soft); opacity: 1; }

.rec-feedback-badge {
  font-size: 12px;
  color: var(--accent);
  padding: 2px 8px;
  background: var(--accent-soft);
  border-radius: 4px;
  white-space: nowrap;
}

/* === Progress bar === */
.progress-bar {
  height: 4px;
  background-color: var(--border);
  border-radius: 2px;
  overflow: hidden;
}
.progress-bar-fill {
  height: 100%;
  background-color: var(--accent);
  transition: width 0.3s ease;
}

/* === Markdown === */
.markdown-body { line-height: 1.75; font-size: 15px; color: var(--fg); }
.markdown-body h1, .markdown-body h2, .markdown-body h3,
.markdown-body h4, .markdown-body h5 {
  font-family: var(--font-serif);
  font-weight: 600;
  margin: 12px 0 6px;
  line-height: 1.4;
  color: var(--fg);
}
.markdown-body h1 { font-size: 1.3em; }
.markdown-body h2 { font-size: 1.15em; }
.markdown-body h3 { font-size: 1.05em; }
.markdown-body p { margin: 6px 0; }
.markdown-body ul, .markdown-body ol { padding-left: 20px; margin: 6px 0; }
.markdown-body li { margin: 3px 0; }
.markdown-body strong { font-weight: 600; }
.markdown-body em { font-style: italic; }
.markdown-body code {
  background: var(--code-bg);
  border-radius: 3px;
  padding: 1px 5px;
  font-size: 0.9em;
  font-family: var(--font-mono);
}
.markdown-body pre {
  background: var(--code-bg);
  border-radius: 6px;
  padding: 12px;
  overflow-x: auto;
  margin: 8px 0;
}
.markdown-body pre code { background: none; padding: 0; }
.markdown-body blockquote {
  border-left: 3px solid var(--accent);
  padding-left: 12px;
  color: var(--fg-muted);
  margin: 8px 0;
}
.markdown-body a { color: var(--link); }
.markdown-body hr { border: none; border-top: 1px solid var(--border); margin: 12px 0; }

/* === Toast === */
@keyframes slideIn {
  from { opacity: 0; transform: translateX(20px); }
  to   { opacity: 1; transform: translateX(0); }
}

/* === Reading layout (inherits global theme) === */
.reading-layout { background: var(--bg); color: var(--fg); min-height: 100vh; }
.reading-toolbar { padding: 16px 24px; }
.reading-exit {
  background: transparent;
  border: 1px solid var(--border);
  color: var(--fg);
  border-radius: var(--radius);
  padding: 0 12px;
  height: 32px;
  font-weight: 500;
  cursor: pointer;
}
.reading-exit:hover { background: var(--surface-hover); opacity: 1; }

.reading-article { max-width: 720px; margin: 0 auto; padding: 8px 24px 96px; line-height: 1.8; }
.reading-article .reading-title { color: var(--fg); margin-bottom: 8px; }
.reading-article .reading-meta { color: var(--fg-muted); font-size: 13px; margin-bottom: 24px; }
.reading-article .reading-meta a { color: var(--link); }

.reading-summary { border: 1px dashed var(--border); border-radius: 6px; padding: 8px 12px; margin-bottom: 24px; }
.reading-summary-toggle {
  background: transparent;
  border: none;
  color: var(--fg);
  cursor: pointer;
  font-size: 13px;
  padding: 0;
  height: auto;
  font-weight: 500;
}
.reading-summary-toggle:hover { background: transparent; opacity: 0.8; }
.reading-summary-body { margin-top: 8px; color: var(--fg); }
.reading-summary-body hr { border: none; border-top: 1px solid var(--border); margin: 12px 0; }

.reading-article .markdown-body img { max-width: 100%; height: auto; border-radius: 4px; }

/* Hide app chrome (header) when reading mode is active. */
body.reading-mode-active #root > div > header { display: none; }

/* === Typing caret === */
.typing-caret {
  display: inline-block;
  margin-left: 2px;
  animation: typing-caret-blink 1s step-end infinite;
  opacity: 0.7;
  font-weight: normal;
}
@keyframes typing-caret-blink { 50% { opacity: 0; } }

/* === AI marker + confetti === */
.ai-marker {
  position: absolute;
  top: 4px;
  font-size: 14px;
  line-height: 1;
  transform: translateX(-50%);
  filter: drop-shadow(0 1px 2px rgba(0, 0, 0, 0.25));
  pointer-events: auto;
  cursor: help;
  user-select: none;
}
.ai-marker.pulse { animation: ai-marker-pulse 0.6s ease-out; }
@keyframes ai-marker-pulse {
  0%   { transform: translateX(-50%) scale(1)   rotate(0deg); }
  40%  { transform: translateX(-50%) scale(1.6) rotate(-12deg); }
  70%  { transform: translateX(-50%) scale(1.3) rotate(8deg); }
  100% { transform: translateX(-50%) scale(1)   rotate(0deg); }
}

.confetti-burst {
  position: absolute;
  left: 50%;
  top: 50%;
  width: 0;
  height: 0;
  pointer-events: none;
}
.confetti-burst span {
  position: absolute;
  left: 0;
  top: 0;
  width: 6px;
  height: 6px;
  border-radius: 1px;
  transform-origin: 0 0;
  opacity: 0;
  animation: confetti-fly 900ms cubic-bezier(0.2, 0.7, 0.3, 1) forwards;
}
@keyframes confetti-fly {
  0%   { opacity: 1; transform: translate(0, 0) rotate(0deg) scale(0.6); }
  100% { opacity: 0; transform: translate(var(--cx), var(--cy)) rotate(var(--cr)) scale(1); }
}

/* === Tag chips === */
.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 12px;
  line-height: 1.3;
  white-space: nowrap;
}
.tag-chip-source { background: var(--surface-hover); color: var(--fg-muted); }
.tag-chip-suggestion {
  background: transparent;
  color: var(--fg-muted);
  border: 1px dashed var(--border);
  cursor: pointer;
}
.tag-chip-suggestion:hover { background: var(--surface-hover); border-style: solid; }
.tag-chip-remove {
  background: none;
  border: none;
  padding: 0 2px;
  height: auto;
  font-size: 11px;
  color: var(--fg-muted);
  cursor: pointer;
  line-height: 1;
  font-weight: normal;
}
.tag-chip-remove:hover { color: var(--fg); opacity: 1; }

.tag-add-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 8px;
  height: auto;
  border-radius: 999px;
  font-size: 12px;
  line-height: 1.3;
  background: transparent;
  border: 1px solid var(--border);
  color: var(--fg-muted);
  cursor: pointer;
  font-weight: 400;
}
.tag-add-btn:hover { background: var(--surface-hover); opacity: 1; }

.tag-input {
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 12px;
  line-height: 1.3;
  border: 1px solid var(--accent);
  outline: none;
  background: var(--surface);
  color: var(--fg);
  width: auto;
}

.tag-suggest-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  margin-top: 4px;
  z-index: 10;
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 4px;
  box-shadow: 0 2px 8px rgba(0,0,0,0.08);
  font-size: 12px;
  min-width: 120px;
}
.tag-suggest-dropdown button {
  display: block;
  width: 100%;
  text-align: left;
  padding: 4px 12px;
  height: auto;
  background: none;
  border: none;
  cursor: pointer;
  color: var(--fg);
  font-weight: 400;
  border-radius: 0;
}
.tag-suggest-dropdown button:hover { background: var(--surface-hover); opacity: 1; }

/* === Saved page === */
.saved-row {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 4px 8px;
  height: auto;
  font-size: 14px;
  color: var(--fg);
  background: none;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  text-align: left;
  font-weight: 400;
}
.saved-row:hover { background: var(--surface-hover); opacity: 1; }
.saved-row.active {
  background: var(--accent-soft);
  color: var(--accent);
  font-weight: 500;
}
.saved-row-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.saved-row-count {
  flex-shrink: 0;
  font-size: 11px;
  color: var(--fg-muted);
}
.saved-row.active .saved-row-count { color: var(--accent); }
.saved-section-title {
  font-size: 11px;
  text-transform: uppercase;
  color: var(--fg-muted);
  margin: 8px 0 4px;
  letter-spacing: 0.05em;
}
.saved-mode-toggle {
  display: inline-flex;
  border: 1px solid var(--border);
  border-radius: 4px;
  overflow: hidden;
}
.saved-mode-toggle button {
  padding: 0 10px;
  height: 26px;
  font-size: 12px;
  background: var(--surface);
  color: var(--fg-muted);
  border: none;
  border-radius: 0;
  font-weight: 500;
}
.saved-mode-toggle button:hover { background: var(--surface-hover); opacity: 1; }
.saved-mode-toggle button.active {
  background: var(--accent);
  color: var(--accent-fg);
}
.saved-mode-toggle button.active:hover { background: var(--accent); }

/* === Responsive === */
.mobile-menu-btn { display: none; }
.mobile-nav { display: none; }

@media (max-width: 640px) {
  .desktop-nav { display: none !important; }
  .mobile-menu-btn { display: block; }
  .mobile-nav { display: block; }
  #root { padding: 12px; }
  .card { padding: 12px; }
  button:not(.btn-sm):not(.settings-tab):not(.theme-swatch):not(.tag-chip-remove):not(.tag-add-btn):not(.reading-summary-toggle):not(.rec-feedback-btn):not(.rec-panel-header):not(.saved-row):not(.tag-suggest-dropdown button):not(.saved-mode-toggle button) { height: 36px; }
}
```

- [ ] **Step 2: Type-check + visual smoke test**

Run: `cd frontend && npm run build`
Expected: PASS.

Then rebuild docker frontend so the new CSS is served:
Run: `docker-compose up -d --build frontend` (from repo root)
Expected: container restarts cleanly. Logs show nginx serving the new bundle.

Open the site in browser; confirm the body now has a sepia-paper background, brown headings, and that the existing pages haven't visually broken (some inline-styled blue elements will still look out-of-place — that's expected; later tasks fix those).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/index.css
git commit -m "feat(ui): rewrite index.css around CSS-variable theme tokens"
```

---

## Task 3: Restyle Layout.tsx top nav

**Files:**
- Modify: `frontend/src/components/Layout.tsx`

- [ ] **Step 1: Replace `frontend/src/components/Layout.tsx`**

The structure stays the same; inline-hex backgrounds/colors are removed in favor of `nav-brand` / `unread-badge` / token-aware inline styles where unavoidable.

```tsx
import { useState, useEffect, useCallback } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { logout, getUnreadCount } from '../api/client'
import Toaster from './Toaster'
import { PlayerProvider } from '../player/PlayerContext'
import MiniPlayer from './MiniPlayer'

interface LayoutProps {
  user: { id: number; username: string; is_admin: boolean } | null
  onLogout: () => void
}

export default function Layout({ user, onLogout }: LayoutProps) {
  const [menuOpen, setMenuOpen] = useState(false)
  const [unreadCount, setUnreadCount] = useState(0)

  const refreshUnread = useCallback(() => {
    getUnreadCount().then(setUnreadCount).catch(() => {})
  }, [])

  useEffect(() => {
    refreshUnread()
    window.addEventListener('refresh-unread', refreshUnread)
    const interval = setInterval(refreshUnread, 2 * 60 * 1000)
    return () => {
      window.removeEventListener('refresh-unread', refreshUnread)
      clearInterval(interval)
    }
  }, [refreshUnread])

  const handleLogout = () => {
    logout()
    onLogout()
  }

  const navLinkClass = ({ isActive }: { isActive: boolean }) =>
    isActive ? 'nav-link active' : 'nav-link'

  const articlesLabel = (
    <span style={{ position: 'relative', display: 'inline-flex', alignItems: 'center', gap: 4 }}>
      文章
      {unreadCount > 0 && (
        <span className="unread-badge">
          {unreadCount > 99 ? '99+' : unreadCount}
        </span>
      )}
    </span>
  )

  return (
    <PlayerProvider>
      <div>
      <header style={{ marginBottom: 20 }}>
        <div className="flex-between">
          <h1 className="nav-brand">RSS Pal</h1>

          <nav className="flex gap-2 desktop-nav" style={{ alignItems: 'center' }}>
            <NavLink to="/articles" className={navLinkClass}>{articlesLabel}</NavLink>
            <NavLink to="/weekly" className={navLinkClass}>周刊</NavLink>
            <NavLink to="/feeds" className={navLinkClass}>订阅</NavLink>
            <NavLink to="/recommended" className={navLinkClass}>推荐</NavLink>
            <NavLink to="/insights" className={navLinkClass}>洞察</NavLink>
            <NavLink to="/stats" className={navLinkClass}>统计</NavLink>
            <NavLink to="/settings" className={navLinkClass}>设置</NavLink>
            <span className="text-muted text-sm" style={{ borderLeft: '1px solid var(--border)', paddingLeft: 8 }}>
              {user?.username}
            </span>
            <button className="btn-ghost btn-sm" onClick={handleLogout}>
              登出
            </button>
          </nav>

          <button
            className="btn-ghost btn-sm mobile-menu-btn"
            onClick={() => setMenuOpen(o => !o)}
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
            {[
              { to: '/articles', label: articlesLabel },
              { to: '/weekly', label: '周刊' },
              { to: '/feeds', label: '订阅' },
              { to: '/recommended', label: '推荐' },
              { to: '/insights', label: '洞察' },
              { to: '/stats', label: '统计' },
              { to: '/settings', label: '设置' },
            ].map(({ to, label }) => (
              <NavLink
                key={to}
                to={to}
                className={navLinkClass}
                onClick={() => setMenuOpen(false)}
                style={{ display: 'block', padding: '10px 16px', borderBottom: '1px solid var(--border)', borderRadius: 0 }}
              >
                {label}
              </NavLink>
            ))}
            <div style={{ padding: '10px 16px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <span className="text-muted text-sm">{user?.username}</span>
              <button className="btn-ghost btn-sm" onClick={handleLogout}>
                登出
              </button>
            </div>
          </nav>
        )}
      </header>
      <main style={{ paddingBottom: 80 }}>
        <Outlet />
      </main>
      <Toaster />
      <MiniPlayer />
    </div>
    </PlayerProvider>
  )
}
```

- [ ] **Step 2: Type-check**

Run: `cd frontend && npm run build`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/Layout.tsx
git commit -m "feat(ui): restyle top nav with theme tokens + serif brand"
```

---

## Task 4: Fold reader-mode palette into global theme

This removes the duplicate `bgTheme` palette from `useReaderSettings`, drops the body `data-reader-bg` toggle in `ReadingLayout`, and trims the swatches out of `ReaderSettingsPanel`.

**Files:**
- Modify: `frontend/src/hooks/useReaderSettings.ts`
- Modify: `frontend/src/components/ReadingLayout.tsx`
- Modify: `frontend/src/components/ReaderSettingsPanel.tsx`
- Modify: `frontend/src/pages/ArticlePage.tsx`
- Possibly modify: `frontend/src/pages/SharePage.tsx` (only if it uses `ReadingLayout`)

- [ ] **Step 1: Update `frontend/src/hooks/useReaderSettings.ts`**

Replace entire file:

```ts
import { useCallback, useEffect, useState } from 'react'

export type ReaderMode = 'normal' | 'reading'
export type ReaderFontFamily = 'sans' | 'serif'

// Persisted settings. `mode` is intentionally NOT in here — reading mode is
// a per-session toggle. Background palette also lives outside this hook
// now; the global theme (util/theme.ts) provides the bg/fg colors and the
// reading view simply inherits them.
export type ReaderSettings = {
  fontSize: number      // 12..24, step 1
  fontFamily: ReaderFontFamily
  confettiEnabled: boolean
}

const STORAGE_KEY = 'rsspal:reader-settings'

const DEFAULTS: ReaderSettings = {
  fontSize: 16,
  fontFamily: 'sans',
  confettiEnabled: true,
}

function load(): ReaderSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULTS
    const parsed = JSON.parse(raw) as Partial<ReaderSettings>
    return {
      fontSize: clampFont(parsed.fontSize ?? DEFAULTS.fontSize),
      fontFamily: parsed.fontFamily === 'serif' ? 'serif' : 'sans',
      confettiEnabled: parsed.confettiEnabled !== false,
    }
  } catch {
    return DEFAULTS
  }
}

function clampFont(n: number): number {
  if (!Number.isFinite(n)) return DEFAULTS.fontSize
  return Math.max(12, Math.min(24, Math.round(n)))
}

export function useReaderSettings() {
  const [settings, setSettings] = useState<ReaderSettings>(() => load())
  const [mode, setModeState] = useState<ReaderMode>('normal')

  useEffect(() => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(settings)) } catch {}
  }, [settings])

  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY) setSettings(load())
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  const setMode = useCallback((m: ReaderMode) => setModeState(m), [])
  const toggleMode = useCallback(() =>
    setModeState(m => m === 'reading' ? 'normal' : 'reading'), [])
  const setFontSize = useCallback((fontSize: number) =>
    setSettings(s => ({ ...s, fontSize: clampFont(fontSize) })), [])
  const setFontFamily = useCallback((fontFamily: ReaderFontFamily) =>
    setSettings(s => ({ ...s, fontFamily })), [])
  const setConfettiEnabled = useCallback((confettiEnabled: boolean) =>
    setSettings(s => ({ ...s, confettiEnabled })), [])

  return {
    ...settings,
    mode,
    setMode,
    toggleMode,
    setFontSize,
    setFontFamily,
    setConfettiEnabled,
  }
}
```

- [ ] **Step 2: Update `frontend/src/components/ReaderSettingsPanel.tsx`**

Replace entire file:

```tsx
import { useEffect, useRef, useState } from 'react'
import type { ReaderFontFamily } from '../hooks/useReaderSettings'

type Props = {
  fontSize: number
  fontFamily: ReaderFontFamily
  onFontSize: (n: number) => void
  onFontFamily: (f: ReaderFontFamily) => void
}

export default function ReaderSettingsPanel({
  fontSize,
  fontFamily,
  onFontSize,
  onFontFamily,
}: Props) {
  const [open, setOpen] = useState(false)
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
        </div>
      )}

      <button
        onClick={() => setOpen(o => !o)}
        aria-label="阅读设置"
        title="阅读设置"
        style={{
          width: 48, height: 48, borderRadius: '50%',
          padding: 0,
          fontWeight: 700,
        }}
      >Aa</button>
    </div>
  )
}
```

- [ ] **Step 3: Update `frontend/src/components/ReadingLayout.tsx`**

Replace entire file:

```tsx
import { useState } from 'react'
import ReactMarkdown from 'react-markdown'
import MarkdownArticle from './MarkdownArticle'
import ReaderSettingsPanel from './ReaderSettingsPanel'
import type { ReaderFontFamily } from '../hooks/useReaderSettings'

type ArticleLite = {
  title: string
  url: string
  published_at: string | null
  word_count: number
  reading_minutes: number
  content: string
  summary_brief: string
  summary_detailed: string
}

type Props = {
  article: ArticleLite
  fontSize: number
  fontFamily: ReaderFontFamily
  onExit: () => void
  onFontSize: (n: number) => void
  onFontFamily: (f: ReaderFontFamily) => void
}

export default function ReadingLayout(props: Props) {
  const { article, fontSize, fontFamily, onExit } = props

  const [summaryOpen, setSummaryOpen] = useState(false)

  const fmtDate = (s: string | null) => s ? new Date(s).toLocaleString('zh-CN') : ''
  const ff = fontFamily === 'serif'
    ? 'var(--font-serif)'
    : 'var(--font-sans)'

  return (
    <div className="reading-layout" style={{ fontFamily: ff }}>
      <div className="reading-toolbar">
        <button className="reading-exit" onClick={onExit} title="退出阅读模式 (Esc / r)">← 退出阅读模式</button>
      </div>

      <article className="reading-article" style={{ fontSize }}>
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

- [ ] **Step 4: Audit ArticlePage and SharePage for `bgTheme` / `setBgTheme` / `onBgTheme` references and remove them**

Run: `cd frontend && grep -n "bgTheme\|setBgTheme\|onBgTheme\|data-reader-bg" src/`
Expected after edit: zero matches (the only files that should ever have referenced them are `ReadingLayout`, `ReaderSettingsPanel`, `useReaderSettings`, `ArticlePage`, possibly `SharePage`).

For each match found in `src/pages/ArticlePage.tsx` or `src/pages/SharePage.tsx`:
- Remove the destructure of `bgTheme` / `setBgTheme` from `useReaderSettings()`.
- Remove the `bgTheme={...}` and `onBgTheme={...}` props passed to `<ReadingLayout>`.

After edits, re-run the grep — must be zero matches.

- [ ] **Step 5: Type-check**

Run: `cd frontend && npm run build`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/hooks/useReaderSettings.ts \
        frontend/src/components/ReadingLayout.tsx \
        frontend/src/components/ReaderSettingsPanel.tsx \
        frontend/src/pages/ArticlePage.tsx
# Add SharePage too if it was modified:
[ -n "$(git diff --name-only frontend/src/pages/SharePage.tsx 2>/dev/null)" ] && git add frontend/src/pages/SharePage.tsx
git commit -m "refactor(reader): fold reading-mode palette into global theme"
```

---

## Task 5: Settings page sub-tabs + Appearance tab with theme picker

**Files:**
- Modify: `frontend/src/pages/SettingsPage.tsx`

The Settings page is 840 lines and contains several sub-components (`PromptField`, `BackupSection`, `BookmarkletSection`) and a long return block of cards. Rather than rewriting the whole file, this task wraps the existing content in a tab dispatcher and adds an Appearance tab.

- [ ] **Step 1: Read the current SettingsPage to identify section boundaries**

Run: `cd frontend && grep -n "<div className=\"card\"\|<BackupSection\|<BookmarkletSection" src/pages/SettingsPage.tsx`
Expected output (line numbers may vary slightly):
- L541 `工具` card
- L548 `阅读体验` card
- L567 `邀请码管理` card (admin only)
- L603 `修改密码` card
- L635 `<BackupSection ...>`
- L638 `<BookmarkletSection />`
- L641 `我的 AI 配置` card
- L697 `摘要模板` card

Capture these in the next step so each section is wrapped under the right tab.

- [ ] **Step 2: Edit `frontend/src/pages/SettingsPage.tsx`**

Make the following changes in order:

**(a) Add imports at top of file (after existing imports):**

```tsx
import { useTheme, type Theme } from '../util/theme'
```

**(b) Insert this new sub-component before the default-exported `SettingsPage` function:**

```tsx
type TabId = 'appearance' | 'account' | 'ai' | 'tools'

const TABS: { id: TabId; label: string }[] = [
  { id: 'appearance', label: '外观' },
  { id: 'account',    label: '账号' },
  { id: 'ai',         label: 'AI' },
  { id: 'tools',      label: '工具' },
]

function readTabFromHash(): TabId {
  const h = window.location.hash.replace(/^#/, '')
  if (h === 'appearance' || h === 'account' || h === 'ai' || h === 'tools') return h
  return 'appearance'
}

function useSettingsTab(): [TabId, (t: TabId) => void] {
  const [tab, setTabState] = useState<TabId>(() => readTabFromHash())
  useEffect(() => {
    const onHash = () => setTabState(readTabFromHash())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])
  const setTab = (t: TabId) => {
    window.location.hash = t
    setTabState(t)
  }
  return [tab, setTab]
}

function SettingsTabs({ tab, onTab }: { tab: TabId; onTab: (t: TabId) => void }) {
  return (
    <div className="settings-tabs" role="tablist">
      {TABS.map(t => (
        <button
          key={t.id}
          role="tab"
          aria-selected={tab === t.id}
          className={tab === t.id ? 'settings-tab active' : 'settings-tab'}
          onClick={() => onTab(t.id)}
        >
          {t.label}
        </button>
      ))}
    </div>
  )
}

const THEME_META: { id: Theme; label: string; bg: string; fg: string; accent: string; border: string }[] = [
  { id: 'paper', label: 'Paper · 暖纸',  bg: '#f5edd6', fg: '#3a2f1a', accent: '#5b3b00', border: '#d9cdaa' },
  { id: 'quiet', label: 'Quiet · 静白',  bg: '#fafaf7', fg: '#1f2933', accent: '#3a5a40', border: '#e3e3de' },
  { id: 'pearl', label: 'Pearl · 珍珠',  bg: '#ebebeb', fg: '#262626', accent: '#0066cc', border: '#d4d4d4' },
  { id: 'night', label: 'Night · 夜读',  bg: '#1a1a1a', fg: '#d4d4d4', accent: '#7eb6ff', border: '#333333' },
]

function ThemePicker() {
  const [theme, setTheme] = useTheme()
  return (
    <div>
      <div className="theme-grid">
        {THEME_META.map(t => (
          <button
            key={t.id}
            type="button"
            className={theme === t.id ? 'theme-swatch active' : 'theme-swatch'}
            onClick={() => setTheme(t.id)}
            aria-pressed={theme === t.id}
          >
            <span
              className="theme-swatch-preview"
              style={{ background: t.bg, color: t.fg, borderColor: t.border }}
            >
              <span className="swatch-title">Aa</span>
              <span className="swatch-dot" style={{ background: t.accent }} />
            </span>
            <span>{t.label}</span>
          </button>
        ))}
      </div>
      <div className="theme-preview">
        <h3 style={{ marginBottom: 6 }}>预览</h3>
        <p className="mb-1">这是一段示例正文，用来预览当前主题下的对比度和阅读感。</p>
        <div className="flex gap-2 mt-2" style={{ alignItems: 'center' }}>
          <button type="button">主按钮</button>
          <button type="button" className="btn-ghost">次按钮</button>
          <button type="button" className="btn-text">文本按钮</button>
          <code style={{ background: 'var(--code-bg)', padding: '2px 6px', borderRadius: 3, fontSize: 13 }}>
            const x = 1
          </code>
        </div>
      </div>
    </div>
  )
}

function AppearanceTab({ readerSettings }: { readerSettings: ReturnType<typeof useReaderSettings> }) {
  return (
    <>
      <div className="card mb-2">
        <h3 className="mb-2">主题</h3>
        <p className="text-muted text-sm mb-2">主题作用于整个站点，包括文章阅读视图。</p>
        <ThemePicker />
      </div>

      <div className="card mb-2">
        <h3 className="mb-1">阅读体验</h3>
        <div className="flex gap-2 mt-2" style={{ alignItems: 'center', flexWrap: 'wrap' }}>
          <span className="text-sm">字号：</span>
          <button className="btn-ghost btn-sm" onClick={() => readerSettings.setFontSize(readerSettings.fontSize - 1)} disabled={readerSettings.fontSize <= 12}>A−</button>
          <span className="text-sm" style={{ minWidth: 48, textAlign: 'center' }}>{readerSettings.fontSize} px</span>
          <button className="btn-ghost btn-sm" onClick={() => readerSettings.setFontSize(readerSettings.fontSize + 1)} disabled={readerSettings.fontSize >= 24}>A+</button>
        </div>
        <div className="flex gap-2 mt-2" style={{ alignItems: 'center' }}>
          <span className="text-sm">字体：</span>
          <button
            className={readerSettings.fontFamily === 'sans' ? 'btn-sm' : 'btn-ghost btn-sm'}
            onClick={() => readerSettings.setFontFamily('sans')}
          >Sans</button>
          <button
            className={readerSettings.fontFamily === 'serif' ? 'btn-sm' : 'btn-ghost btn-sm'}
            onClick={() => readerSettings.setFontFamily('serif')}
            style={{ fontFamily: 'var(--font-serif)' }}
          >Serif</button>
        </div>
        <div className="flex gap-2 mt-2" style={{ alignItems: 'center' }}>
          <label className="text-sm flex gap-1" style={{ alignItems: 'center' }}>
            <input
              type="checkbox"
              style={{ width: 'auto' }}
              checked={readerSettings.confettiEnabled}
              onChange={e => readerSettings.setConfettiEnabled(e.target.checked)}
            />
            阅读完成时显示彩带
          </label>
        </div>
      </div>
    </>
  )
}
```

**(c) In the `SettingsPage` default-exported function**, find the return block that starts roughly with:

```tsx
return (
  <div>
    <h2 className="mb-2">设置</h2>
    ...
```

Replace the entire return block with this tab dispatcher. **Preserve every existing card/section** — they get moved unchanged into the right tab body:

```tsx
const [tab, setTab] = useSettingsTab()

return (
  <div>
    <h2 className="mb-2">设置</h2>
    <SettingsTabs tab={tab} onTab={setTab} />

    {tab === 'appearance' && (
      <AppearanceTab readerSettings={readerSettings} />
    )}

    {tab === 'account' && (
      <>
        {/* 修改密码 card — moved from original position */}
        <div className="card mb-2">
          <h3 className="mb-2">修改密码</h3>
          {/* ...existing 修改密码 JSX unchanged... */}
        </div>

        {/* 邀请码管理 card (admin only) — moved */}
        {user?.is_admin && (
          <div className="card mb-2">
            {/* ...existing 邀请码管理 JSX unchanged... */}
          </div>
        )}
      </>
    )}

    {tab === 'ai' && (
      <>
        {/* 我的 AI 配置 card — moved */}
        <div className="card mb-2">
          {/* ...existing 我的 AI 配置 JSX unchanged... */}
        </div>

        {/* 摘要模板 card — moved */}
        <div className="card">
          {/* ...existing 摘要模板 JSX unchanged... */}
        </div>
      </>
    )}

    {tab === 'tools' && (
      <>
        {/* 工具 card — moved */}
        <div className="card mb-2">
          <h3 className="mb-1">工具</h3>
          {/* ...existing 工具 JSX unchanged... */}
        </div>

        <BookmarkletSection />
        <BackupSection isAdmin={!!user?.is_admin} />
      </>
    )}
  </div>
)
```

Note: where the comments say `...existing ...JSX unchanged...`, paste in the literal JSX from the original return block — do NOT abbreviate or paraphrase. The goal is a pure reorganization, not a rewrite.

**(d) Also**: the SettingsPage already has a `useReaderSettings()` call somewhere near the top of the function (the existing `阅读体验` card uses it). If it's not already named `readerSettings`, rename the destructured object reference so the new `<AppearanceTab readerSettings={readerSettings} />` line works:

```tsx
const readerSettings = useReaderSettings()
```

If the existing code instead destructures `const { fontSize, setFontSize, ... } = useReaderSettings()`, replace it with the line above, and update the existing 阅读体验 JSX usages of `fontSize` → `readerSettings.fontSize`, etc — though that JSX is being moved into `AppearanceTab` which already uses the dotted form, so likely the original `阅读体验` JSX can be **deleted from the page** entirely (it lives only in `AppearanceTab` now).

- [ ] **Step 3: Type-check**

Run: `cd frontend && npm run build`
Expected: PASS.

If a "X is declared but never used" error appears for an old destructured variable, remove the now-orphaned destructure.

- [ ] **Step 4: Visual verification**

Run: `docker-compose up -d --build frontend` (from repo root).

Then with `agent-browser`:
1. `agent-browser navigate http://localhost:8080/settings --session-name rss-pal-local`
2. Confirm a 4-tab strip is visible: 外观 · 账号 · AI · 工具.
3. Click each tab; URL hash should change to `#account`, `#ai`, `#tools` respectively.
4. On 外观 tab, click each of 4 swatches; the entire app should re-paint immediately (background, headings, buttons).
5. Reload page (`F5`); the chosen theme should still be active.
6. Visit `/settings#ai` directly; the AI tab should be selected on load.

Note about login: if not logged in, agent-browser opens login first; default password from CLAUDE.md is `admin` (env `AUTH_PASSWORD`).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/SettingsPage.tsx
git commit -m "feat(settings): sub-tabs (外观/账号/AI/工具) + theme picker"
```

---

## Task 6: Migrate inline-style buttons in components (sweep)

The goal is to eliminate `style={{ background: '#...', color: '...', padding: ... }}` from buttons across the frontend and replace with `.btn-ghost`, `.btn-text`, or default-primary classes. Visual continuity is paramount — match the prior visual weight.

**Files (one commit per file group for easy rollback):**
- `frontend/src/components/ArticleCard.tsx`
- `frontend/src/components/RecommendationsCard.tsx`
- `frontend/src/components/ArticlePlayerCard.tsx`
- `frontend/src/components/FeedHealthTable.tsx`
- `frontend/src/components/SavedTagSidebar.tsx`
- `frontend/src/components/TagBar.tsx`

- [ ] **Step 1: ArticleCard — migrate the video/audio media pills**

In `ArticleCard.tsx` (around lines 13-60), the `MediaIndicator` sub-component renders two pills with inline hex backgrounds (`#fff5f5`, `#cc3a3a`, `#0066cc`). Replace those inline-color rules with token references.

Open `frontend/src/components/ArticleCard.tsx` and replace the `MediaIndicator` function (everything from `function MediaIndicator(` through its closing `}`) with:

```tsx
function MediaIndicator({ article, onPlay }: { article: Article; onPlay: (a: Article) => void }) {
  if (!article.media_url) return null
  const t = article.media_type ?? ''
  const isVideo = t.startsWith('video/')
  const isAudio = t.startsWith('audio/')
  const audioFallback = !isVideo && !isAudio

  return (
    <span style={{ display: 'inline-flex', gap: 4, marginRight: 8, flexShrink: 0 }}>
      {isVideo && (
        <span
          title="视频"
          aria-label="视频"
          className="tag-chip"
          style={{ border: '1px solid var(--border)' }}
        >
          🎬 视频
        </span>
      )}
      {(isAudio || audioFallback) && (
        <button
          type="button"
          aria-label="播放"
          title="音频 · 点击播放"
          className="btn-ghost btn-sm"
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            onPlay(article)
          }}
          style={{ borderRadius: 999 }}
        >
          ▶ 音频
        </button>
      )}
    </span>
  )
}
```

Run: `cd frontend && npm run build`
Expected: PASS.

Commit:

```bash
git add frontend/src/components/ArticleCard.tsx
git commit -m "refactor(ui): ArticleCard media pills use token classes"
```

- [ ] **Step 2: RecommendationsCard, ArticlePlayerCard, FeedHealthTable**

For each of the following three files, open it and:
1. Locate any `<button>` that has an inline `style={{ ... background ... }}` or hex color.
2. Replace those props with one of these classes:
   - **Default primary** (no class needed): when the button is the screen's main action.
   - **`.btn-ghost`** when it's a secondary action (cancel, dismiss, "next", etc.).
   - **`.btn-text`** when it's a small inline icon/text button (feedback emoji, dismiss-x).
   - Add **`.btn-sm`** if the original was visually smaller than 32px.
3. Remove the inline color/background style props. Keep layout-only props (`position`, `flex`, `margin`).

After editing each file, run `cd frontend && npm run build`. If it passes, commit:

```bash
git add frontend/src/components/RecommendationsCard.tsx
git commit -m "refactor(ui): RecommendationsCard buttons use token classes"

git add frontend/src/components/ArticlePlayerCard.tsx
git commit -m "refactor(ui): ArticlePlayerCard buttons use token classes"

git add frontend/src/components/FeedHealthTable.tsx
git commit -m "refactor(ui): FeedHealthTable buttons use token classes"
```

If a file turns out to have no inline-color buttons (already class-based), skip it — no empty commits.

- [ ] **Step 3: SavedTagSidebar + TagBar**

Same sweep procedure. Commit each separately:

```bash
git add frontend/src/components/SavedTagSidebar.tsx
git commit -m "refactor(ui): SavedTagSidebar buttons use token classes"

git add frontend/src/components/TagBar.tsx
git commit -m "refactor(ui): TagBar buttons use token classes"
```

- [ ] **Step 4: Stragglers sweep**

Run:

```bash
cd /Users/bytedance/mygit/rss-pal/frontend && \
  grep -rn "background: *['\"]#\|background-color: *['\"]#\|background: *#[0-9a-fA-F]" src/ --include='*.tsx' --include='*.ts' \
  | grep -v 'index.css' \
  | grep -v '// '
```

Expected: only hits left should be:
- `theme.ts` (no — it sets no colors)
- inline `style={{ background: 'var(--surface)' }}` etc — these are FINE (they use tokens)
- color literals inside `Toaster.tsx`, `BackFab.tsx`, `BackToTopButton.tsx`, `Spinner.tsx`, `VideoEmbed.tsx`, `ConfettiBurst.tsx`, `PruningDrawer.tsx`, `GroupedArticleView.tsx`, `MiniPlayer.tsx`, `MarkdownArticle.tsx`, `categoryLabels.ts`, `parseVideoPlaceholder.ts` — review each and migrate to tokens if it's a button/surface/text background; leave alone if it's a one-off visual (e.g. confetti color palette, video placeholder thumbnail).

For each remaining hit that *is* a UI surface (not e.g. confetti random colors), replace the literal with the appropriate `var(--token)` reference. Examples:
- `background: '#fff'` → `background: 'var(--surface)'`
- `color: '#333'` → `color: 'var(--fg)'`
- `color: '#666'` / `'#888'` → `color: 'var(--fg-muted)'`
- `border: '1px solid #eee'` / `'#ddd'` → `border: '1px solid var(--border)'`

Type-check and commit per file edited:

```bash
cd frontend && npm run build

# Then for each modified file:
git add frontend/src/components/<filename>
git commit -m "refactor(ui): <filename> use theme tokens"
```

---

## Task 7: Final verification + PR

- [ ] **Step 1: Final build**

Run: `cd frontend && npm run build`
Expected: PASS, no warnings about unused imports or undefined types.

- [ ] **Step 2: Rebuild docker frontend**

Run from repo root: `docker-compose up -d --build frontend`
Expected: container restarts, `docker-compose logs --tail=30 frontend` shows nginx 200s.

- [ ] **Step 3: Visual QA across themes**

Using `agent-browser --session-name rss-pal-local`, for each theme (paper → quiet → pearl → night):

1. Set the theme via Settings → 外观 (or evaluate `setTheme()` directly).
2. Take screenshots of these pages:
   - `/articles`
   - `/articles/:id` (pick any article)
   - `/feeds`
   - `/recommended`
   - `/weekly`
   - `/insights`
   - `/stats`
   - `/settings#appearance`
   - `/settings#account`
3. Verify on each: no white-block "hole" in the layout that didn't pick up the theme background; buttons have one consistent height per breakpoint; focus rings visible when tabbing.

- [ ] **Step 4: Verify reader-mode bg-palette is fully gone**

```bash
cd /Users/bytedance/mygit/rss-pal && grep -rn "data-reader-bg\|bgTheme\|reader-bg" frontend/src/ frontend/src/index.css
```

Expected: zero matches.

- [ ] **Step 5: Push branch and open PR**

```bash
cd /Users/bytedance/mygit/rss-pal && git push -u origin feature/ui-theme-redesign
```

Then:

```bash
gh pr create --title "feat(ui): reading-focused themes + button/layout cleanup" --body "$(cat <<'EOF'
## Summary
- Four switchable themes (**Paper**, **Quiet**, **Pearl**, **Night**) driven by CSS-variable tokens under `body[data-theme=…]`.
- Settings page reorganised into sub-tabs (`#appearance` / `#account` / `#ai` / `#tools`) with URL-hash deep links; new theme picker on the 外观 tab.
- Buttons consolidated to three variants (primary / `.btn-ghost` / `.btn-text`); inline-hex button styles across components migrated to token classes.
- Reader-mode background palette removed — the reading view now inherits whichever global theme is active, so there's no double toggle.
- Top nav restyled with token-aware serif brand mark; rest of layout structure unchanged.

Spec: `docs/superpowers/specs/2026-05-11-ui-theme-redesign-design.md`
Plan: `docs/superpowers/plans/2026-05-11-ui-theme-redesign.md`

## Test plan
- [ ] `cd frontend && npm run build` — passes clean
- [ ] `docker-compose up -d --build frontend` — frontend container healthy
- [ ] Visit `/settings#appearance`, switch through Paper/Quiet/Pearl/Night — site re-paints immediately, no flash of wrong theme on reload
- [ ] Open any article — reader view bg matches the active theme; no separate bg picker in the Aa panel
- [ ] Mobile breakpoint (≤640px) — hamburger nav still works; Settings tab strip scrolls horizontally if needed
- [ ] `grep -rn "data-reader-bg\\|bgTheme" frontend/src/` returns no matches

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Report the PR URL.

---

## Self-Review Notes (already applied)

- **Spec coverage**: every spec section maps to a task — themes (Task 2 CSS), boot apply (Task 1), Settings sub-tabs + theme picker (Task 5), reader-mode integration (Task 4), button unification + .row (Task 2 CSS + Task 6 sweep), CSS architecture (Task 2), files-changed list matches Tasks 1–6.
- **Placeholder scan**: no TBDs; the only `…existing JSX unchanged…` markers in Task 5 are explicit instructions to paste literal existing JSX from the source file (which is too long to include verbatim in the plan) — the engineer needs to actually open `SettingsPage.tsx` and move blocks. This is acceptable because the blocks are stable and the task is a pure reorganization.
- **Type consistency**: `Theme` type used identically in `theme.ts`, `ThemePicker`, `SettingsPage`. `ReaderFontFamily` continues to come from `useReaderSettings`. `useReaderSettings()` return shape changes (loses `bgTheme`/`setBgTheme`) — Task 4 explicitly removes consumers via Step 4 grep.
- **No test framework**: there is none in the frontend; verification is `tsc` + visual checks rather than unit tests. This is consistent with the project's existing development style.
