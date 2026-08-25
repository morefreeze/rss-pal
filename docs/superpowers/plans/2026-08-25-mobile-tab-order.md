# Mobile Tab Order Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show `简报` as the fourth primary mobile bottom tab and preserve the requested complete navigation order through the More sheet.

**Architecture:** Keep the existing static mobile navigation split. Add the existing `/briefing` route to `MobileTabBar` and verify the actual rendered order of both the bottom bar and More sheet with one focused component test.

**Tech Stack:** React 18, React Router 6, TypeScript, Vitest, Testing Library

---

### Task 1: Enforce the mobile navigation order

**Files:**
- Create: `frontend/test/MobileTabBar.test.tsx`
- Modify: `frontend/src/components/MobileTabBar.tsx:7-12`

- [ ] **Step 1: Write the failing rendered-order test**

Create `frontend/test/MobileTabBar.test.tsx`:

```tsx
import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import MobileTabBar from '../src/components/MobileTabBar'

describe('MobileTabBar', () => {
  it('shows primary and overflow destinations in the requested order', () => {
    render(
      <MemoryRouter initialEntries={['/articles']}>
        <MobileTabBar unreadCount={0} onLogout={() => {}} />
      </MemoryRouter>,
    )

    const nav = screen.getByRole('navigation', { name: '主导航' })
    expect(Array.from(nav.querySelectorAll('a, button')).map(item => item.textContent?.trim())).toEqual([
      '📰文章',
      '⭐网摘',
      '📡订阅',
      '📅简报',
      '⋯更多',
    ])

    fireEvent.click(within(nav).getByRole('button', { name: '更多' }))
    const sheet = screen.getByRole('dialog', { name: '更多' })
    expect(within(sheet).getAllByRole('button').map(item => item.textContent?.trim())).toEqual([
      '💡兴趣',
      '📊统计',
      '⚙️设置',
      '🚪登出',
    ])
  })
})
```

- [ ] **Step 2: Run the focused test and verify RED**

Run:

```bash
cd frontend
npm test -- MobileTabBar.test.tsx
```

Expected: FAIL because the rendered bottom controls omit `📅简报`.

- [ ] **Step 3: Add the briefing tab**

In `frontend/src/components/MobileTabBar.tsx`, make `TABS`:

```tsx
const TABS: Tab[] = [
  { to: '/articles',            icon: '📰', label: '文章', showUnread: true, matchClip: false },
  { to: '/articles?view=clip',  icon: '⭐', label: '网摘',                   matchClip: true  },
  { to: '/feeds',               icon: '📡', label: '订阅' },
  { to: '/briefing',            icon: '📅', label: '简报' },
]
```

Do not change `MoreSheet`; it already renders `兴趣`, `统计`, `设置`, `登出` in the required order.

- [ ] **Step 4: Verify GREEN and the full frontend**

Run:

```bash
cd frontend
npm test -- MobileTabBar.test.tsx
npm run check
npm run build
```

Expected: focused test passes; all Vitest and legacy tests pass; production build succeeds with only the existing chunk-size warning.

- [ ] **Step 5: Commit**

```bash
git add frontend/test/MobileTabBar.test.tsx frontend/src/components/MobileTabBar.tsx
git commit -m "fix(frontend): order mobile navigation tabs"
```
