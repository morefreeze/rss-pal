# Shortcut capture queue implementation plan

**Goal:** End the iPhone shortcut after durable enqueue of the shared article.

**Architecture:** Existing private clip rows carry a distinct queued state. A bounded worker loop fetches queued rows independently of feed discovery and PDF OCR. Preserve capture-only authentication and the existing response ID for compatibility.

**Tech stack:** Go/Gin, PostgreSQL, React/Vitest, Apple Shortcuts.

1. Add failing HTTP tests proving enqueue does no fetch, commits a queued owner-only clip, deduplicates concurrent shares, preserves ready articles, and requeues failed rows. Retain commit-failure and credential-scope tests.
2. Add repository queue methods and the enqueue handler. Normalize/validate the URL without network access. Keep budget/auth middleware and after-commit response behavior.
3. Test and implement an independent bounded worker loop: success metadata and ready state, three failures, cancellation/budget denial, and no collision with PDF OCR or link-set queues. Exclude queued rows from generic short-content refetch.
4. Add queued UI status/polling tests and update native shortcut wording and install documentation. Export, sign, import, test and publish the updated native shortcut; update installer links only after verifying the published Apple record.
5. Run relevant backend suites with isolated PostgreSQL, frontend tests and build, request code review, resolve findings, and verify diff scope. Commit and push actual master without touching the unrelated primary checkout.
6. Follow the existing Tencent workflow, prove deployed SHA/service health, and verify live enqueue latency plus background persistence using a synthetic account; remove only synthetic test data and local test resources.

No further product decision or approval is required: the user chose enqueue-only behavior.

## Implementation verification

- Enqueue performs no network I/O and acknowledges after commit. A real PostgreSQL/application-role test panicked on the old synchronous network call; the new route passes. Concurrent first-use shares return one clip feed and one article ID without lock-upgrade deadlock.
- The worker uses a per-runner rotating ID cursor, preserving progress across budget-denied rows. Budget denial consumes no fetch attempt; recovery, three failures and shutdown cancellation are covered. PDF OCR pending rows remain isolated.
- Browser content saves atomically finish queued/failed rows, preventing stale worker success/failure from overwriting them. Tests cover both queued and failed recovery. The detail page keeps polling queued jobs beyond five minutes; the old implementation failed that regression.
- Code review found the starvation, browser takeover and polling-limit defects above; all were fixed and independently re-reviewed without remaining important findings.
- Final full backend suites passed: API 58.050s, repository 93.044s, worker 6.449s, server cached. Frontend: 82 files / 761 tests plus 9 legacy tests passed; production build passed. Existing bundle size warning remains.
- Published Apple installer: https://www.icloud.com/shortcuts/98ef2f6cfbe740e092908ab72ea9240e ; record APPROVED. Canonical source equals the published native workflow, 36 actions. The only action behavior change is success notification wording; Apple normalized plain text serialization and updated native client metadata.
