# Persistent article sorting implementation plan

Goal: remember sorting across home-screen launches. Architecture: localStorage preferences with legacy session fallback in ArticleListPage. Stack: React, TypeScript, Vitest.

- [x] Reproduce with fresh-session tests in ArticleListPageInfiniteScroll.test.tsx; six failures demonstrated the gap.
- [x] Read persistent preferences before session fallback, validate values, persist both values in an effect.
- [x] Run component regression tests, frontend check and production build; review diff.
- [ ] Commit, push and deploy frontend to Tencent; verify source revision, container and public asset bytes/health.
