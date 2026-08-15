# English Article Justification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give English article prose a consistent right edge while preserving automatic hyphenation and natural paragraph-final lines.

**Architecture:** Keep `MarkdownArticle` language detection unchanged and use its existing `lang="en"` attribute as the CSS boundary. Add justification only to paragraph and list-item descendants, then verify the rule through the existing static CSS contract test and browser rendering.

**Tech Stack:** React, CSS, Node.js built-in test runner, Vitest, Vite, agent-browser

---

### Task 1: Justify English Article Prose

**Files:**
- Modify: `frontend/test/articleEnglishHyphenation.test.cjs`
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Write the failing CSS contract test**

Add a separate test after the existing hyphenation test:

```js
test('English article prose uses justified lines with a natural final line', () => {
  const rule = css.match(/\.markdown-body\[lang="en"\]\s+:is\(p,\s*li\)\s*\{[^}]+\}/)?.[0] ?? ''
  assert.match(rule, /text-align:\s*justify\b/)
  assert.match(rule, /text-align-last:\s*start\b/)
})
```

- [ ] **Step 2: Run the focused test and verify RED**

Run from `frontend/` under Node 22:

```bash
source ~/.nvm/nvm.sh
nvm use 22
node --test test/articleEnglishHyphenation.test.cjs
```

Expected: the existing hyphenation test passes and the new justification test
fails because the scoped CSS rule is absent.

- [ ] **Step 3: Add the minimal scoped CSS rule**

Add this rule immediately after the base `.markdown-body` rule:

```css
.markdown-body[lang="en"] :is(p, li) {
  text-align: justify;
  text-align-last: start;
}
```

- [ ] **Step 4: Run the focused test and verify GREEN**

Run the same Node command. Expected: both CSS contract tests pass.

- [ ] **Step 5: Run frontend regression checks**

Run from `frontend/` under Node 22:

```bash
npm run check
npm run build
```

Expected: all frontend tests and static checks pass, and the production build
finishes successfully. Existing non-blocking Vite warnings may remain.

- [ ] **Step 6: Verify browser rendering at desktop and mobile widths**

Start the Vite development server, open it with agent-browser, and inject a
representative English `.markdown-body[lang="en"]` fixture into the loaded
document so it uses the real application stylesheet. At 1440x900 and 390x844,
verify through computed styles and screenshots that:

- prose paragraphs report `text-align: justify`;
- paragraph-final lines remain start-aligned;
- headings and code blocks do not receive justified alignment;
- no text overlaps or horizontal overflow appears.

- [ ] **Step 7: Verify and commit only the scoped implementation**

```bash
git diff --check
git status --short
git add frontend/test/articleEnglishHyphenation.test.cjs frontend/src/index.css
git diff --cached --check
git commit -m "Justify English article prose"
```

Expected: the implementation commit contains only the CSS contract test and
the scoped Markdown style change; unrelated untracked backup files remain
untouched.
