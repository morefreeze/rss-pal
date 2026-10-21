# OPML Transfer Implementation Plan

**Goal:** Complete the agreed OPML migration flow in subscription management.
**Architecture:** A strict browser XML parser and a focused transfer component reuse the existing feed API. The API accepts optional titles and emits portable XML through encoding/xml.
**Tech Stack:** React, TypeScript, Vitest, Go, PostgreSQL.

- [x] Write parser and UI regression tests for malformed XML, nested outlines, titles, invalid URLs, duplicate accounting, progress and selective retry. Run `npm test -- test/opml.test.ts test/OPMLTransfer.test.tsx` and confirm missing implementation failure.
- [x] Implement `frontend/src/utils/opml.ts` and `frontend/src/components/OPMLTransfer.tsx`; replace inline handlers in FeedListPage. Extend addFeed with optional title. Re-run targeted tests.
- [x] Add API regression tests for creation title persistence, account-scoped export and XML round trip. Run targeted Go tests before backend changes. Add optional title to AddFeedRequest and Create, then replace export XML construction with encoding/xml and exclude non-subscription sources.
- [x] Run frontend check/build and backend affected-package tests. Review diff and address findings. Fetch origin/master, integrate on clean worktree, push master without altering dirty original checkout. Observe Deploy Tencent success and verify host revision, health and delivered asset behavior.

Validation: frontend 84 files / 776 tests plus 9 legacy checks passed; production build passed. API and repository packages passed against isolated PostgreSQL 16 (tmpfs). Independent review found and verified a canonical URL submission fix. Deployment verification follows mainline push.
