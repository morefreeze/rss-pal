# Explore source retry states

The approved retry delays after the initial failure are 1h, 4h, 16h, 2d, 8d and 32d, with ±10% jitter. A later HTTP Retry-After takes precedence. These are earliest eligibility times; existing worker windows still determine actual dispatch.

`recommended_feeds.fetch_state` distinguishes `active`, `retry_wait`, `retry_exhausted`, `unavailable`, and `ineligible`. Counters live on the source so rediscovery cannot reset the budget. Inactivity, insufficient provenance, and missing RSS stop automatically. Retrying is bounded; exhaustion is not evidence of permanent unavailability. Existing cached articles and task records are retained.

## Initial rollout prerequisite

The established Tencent deploy workflow rebuilds services; its status-migrate service currently lists PostgreSQL migrations only through 052 and does **not** include migration 053. Migration `backend/migrations/053_explore_retry_states.sql` must be explicitly applied once, with a backup and a transaction, before deploying code that queries the new columns. Do not use API health alone as migration evidence: `/api/health` can return OK while monitoring and exploration queries fail.

For this rollout, production migration approval is required by automatic approval review. Do not execute it until approved. After approval, back up `recommended_feeds` and `explore_fetch_queue` on the host with pg_dump, verify a nonempty backup, then apply the exact reviewed migration with psql `ON_ERROR_STOP=1` and `--single-transaction`. This migration classifies existing records and closes pending work for stopped sources; it deletes no records. Do not rerun once the columns exist.

## Verification

- Deployed Git revision and restarted API/worker/frontend match the release.
- API health succeeds and authenticated monitoring visibly displays source-state counts.
- No pending task joins a source in `retry_exhausted`, `unavailable`, or `ineligible`.
- Every pending retry task has `not_before >= next_retry_at`.
- Deferred deadlines and source failure counts obey the six-retry budget.

Source states persist until an explicit reset; routine observations, source refresh scheduling, and canonical alias discovery cannot reactivate stopped sources. A code rollback can keep additive columns, but reverting the historical queue/source changes requires the saved backup and a separately reviewed recovery plan.

## Monitoring completion estimate

The monitoring page estimates the first pass over the current executable source-fetch and related-discovery backlog together, since both share one batch limit. API and worker receive the same `EXPLORE_FETCH_BATCH_LIMIT`. The estimate includes the six Shanghai dispatch windows, overnight waits, and the average preparation and per-task execution cost from up to 12 completed standard runs within seven days. At least three samples are required. A consumed current window is skipped; the final partial batch receives a full batch duration allowance.

The estimate assumes normal dispatch and excludes tasks waiting for backoff, future discoveries, and subsequent retries. Running or expired leases show an explicit unavailable state rather than a completion time. No executable tasks show an empty state. This is an estimate, not a drain-time guarantee.
