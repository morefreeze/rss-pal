# Continuous exploration and actionable monitoring

Approved: drain eligible work continuously with existing batch cap 500 and concurrency 5; preserve provider/snapshot six-slot schedules and six retry backoffs. Redefine historical-age alerts.

- Worker: independent minute tick, one recovered or fresh batch, no overlap, one-minute cooldown after finish; provider slowness cannot block queue.
- Repository: serialize claims via existing advisory transaction lock; reject fresh claims while any leases remain; recover expired only after live leases drain. Preserve lease fencing and source-state eligibility.
- Monitor: historical age only in details; shared progress health with 30-minute inactivity threshold and expired lease detection; daily quota exhaustion deduped against live ledger/policy with reset time. ETA includes ready and executing work based on throughput and minute cadence.
- UI: actionable health and quota sections, progress/ready wait upfront, historical age in details; no raw table names in alerts.
- Verification: isolated PostgreSQL scheduler/repository/monitor tests, UI tests/build, independent spec and code review, integrate current master, Tencent deployment and real throughput/monitoring proof.
