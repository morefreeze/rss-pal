# Explore failure lifecycle

Goal: Bound retries per source and retain terminal records without automatic resurrection.

Confirmed policy: Six retries after the initial request, at 1h, 4h, 16h, 2d, 8d, 32d with +/-10% jitter. Respect a later Retry-After. Stop automatically for inactivity, insufficient evidence and missing RSS. Preserve cache and historical errors. No change to snapshot schedule or concurrency.

Architecture: Add source-level fetch_state, fetch_failures and next_retry_at independent of validation_status. States: active, retry_wait, retry_exhausted, unavailable, ineligible. Enqueue, claim, processor and due-source scheduling all honor source state. Transient errors never become permanent merely because retry budget is exhausted. Source row lock and task lease fencing serialize outcomes. Terminal source transitions close pending siblings. Monitor exposes source-level state counts separately from historical task failures.

- [x] Add failing classifier, lifecycle, migration and monitor tests; run isolated PostgreSQL on port 15439.
- [x] Add migration 053: fields, constraints, historical classification, bounded retry metadata, terminal pending-task settlement. Unknown historical failures receive one finite retry budget; existing excessive failures become exhausted.
- [x] Implement error classification (403/404/network retry, 410/safety unavailable, admission failures ineligible), source retry lifecycle and all scheduling gates. Test exact six retries, jitter bounds, persistence across enqueue, lease fencing, cache retention and success reset.
- [x] Expose monitor source-state counts with frontend rendering test. Run relevant Go packages, frontend tests/build.
- [ ] Independent code review; fix findings; integrate latest origin/master without touching dirty primary checkout, push master, follow established Tencent workflow, verify revision/health/database and monitor UI.

Verification before integration: Go explore/repository/opsmonitor/worker/API packages passed against isolated PostgreSQL; all backend packages compile; 640 frontend tests, 9 legacy tests and production build passed. Independent review findings on alias revival and lock order fixed and covered by database tests. Retry eligibility uses existing worker windows.
