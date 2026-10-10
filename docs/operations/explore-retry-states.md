# Explore source retry states

The approved retry delays after the initial failure are 1h, 4h, 16h, 2d, 8d and 32d, with ±10% jitter. A later HTTP Retry-After takes precedence. These are earliest eligibility times; continuous worker dispatch still determines actual execution.

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

## Continuous queue consumption (2026-10-10)

Queue dispatch is independent from the six daily discovery/snapshot slots. Each minute the worker can start one recovered or fresh batch (up to 500 tasks, at most 5 concurrent handlers). A finished attempt is followed by at least 60 seconds of cooldown; minute polling means the next start is generally 60–120 seconds later. Database lease guards prevent fresh batches while older leases remain across worker instances. Only eligible work runs: stopped sources and future retry deadlines remain excluded.

Monitoring alerts on shared exploration processing inactivity (default 30 minutes while work is executable or leased), and on expired leases needing recovery. Historical record age and current eligibility age are only details, not service-failure signals. Duplicate source discovery must not advance the queue processing timestamp; successful outcomes and failed attempts entering backoff do count as progress. Daily quota exhaustion appears once with its reset time, based on the current UTC-day ledger and current policy; repeated daily-denial events no longer drive burst alerts.

The completion estimate now covers executable and in-flight tasks using recent batch throughput and up to 120 seconds between batches. It excludes future retry eligibility and incoming work. Provider synchronization and recommendation snapshots retain the six daily slots. Existing source budget, terminal-state rules, and lease fencing remain in effect.
