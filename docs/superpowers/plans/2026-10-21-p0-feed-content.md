# P0 subscription and content reliability

Goal: deliver P0-1/P0-2 against current master without incorporating stale or unrelated edits.

Confirmed scope: investigate actual defects, fix, review, test, integrate master, deploy Tencent and verify runtime behavior. No historical data rewrite or public exposure changes.

Architecture: preserve the independent subscription loop and budgets already shipped. Persist per-feed failure/retry state separately from successful check time. Do not acknowledge RSS validators until bounded ingestion drains. Use atomic insert-if-absent for subscription articles. Protect full text replacement and invalidate summaries only when body changes.

- [x] Compare original dirty checkout, origin/master and Tencent revision. Both previously uncommitted subscription isolation and BestBlogs newsletter extraction are already shipped.
- [x] Capture production baseline: 36 active rows, including clip/Twitter non-polling sources and malformed legacy imports. Last 24h: 9 articles, none <300 bytes, all with summary. No duplicate feed+URL rows. API healthy.
- [x] Reproduce failed-source retry storm, 10-entry conditional-cache loss, manual 304 timestamp, shorter body replacement and stale summary with real PostgreSQL/HTTP tests.
- [x] Persist retry state, expose failure/next retry in subscriptions, atomically deduplicate concurrent ingestion, drain uncached bounded batches.
- [x] Preserve existing content on suspicious shrink/empty/error; conditional replacement to avoid concurrent overwrite; update frontend summaries consistently.
- [x] Run fixed representative extraction samples (long text, newsletter, code/math, table/images), API/worker/repository regressions and frontend tests/build.
- [x] Independent code review; incorporate concurrent master; push master and verify Actions exact revision, migration, containers, public health, >=2 subscription ticks and failure backoff.

Baseline test environment: isolated PostgreSQL 15 on 127.0.0.1:55441, tmpfs PGDATA, serial database tests. Initial disk-backed run failed because Docker VM disk was full; removed only this task's disposable test container/volume and switched test DB storage to tmpfs. This is infrastructure failure, not product test evidence.
