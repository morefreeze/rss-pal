# Explore 304 Observation Freshness Fix

## Problem

Explore registry providers use conditional HTTP requests. A successful `304 Not Modified` updates the provider's `last_success_at`, but it does not update the provider's existing `explore_source_observations.last_seen_at` values. Candidate ranking treats observations older than `max(2 * sync_interval, 6 hours)` as stale, so an unchanged registry eventually contributes zero candidates. Empty snapshots are intentionally rejected, causing every scheduled Explore refresh to fail and fall back to the last successful batch.

## Decision

Add a dedicated `RecordNotModified` operation to the registry store. It will run one transaction that:

1. records provider success, validators, and the synchronization timestamp; and
2. advances `last_seen_at` only for observations written at that provider's `last_materialized_at`, and advances their canonical sources' `last_observed_at`, to the new synchronization timestamp; and
3. advances `last_materialized_at` to the new synchronization timestamp, including when the represented generation is empty.

All candidates from one successful `200` synchronization are written with the same timestamp that is then stored in `provider.last_materialized_at`. Entries absent from that response retain an older timestamp. An empty successful `200` advances `last_materialized_at` without advancing any observation, deliberately representing an empty generation. A following `304` therefore refreshes exactly the prior materialized generation without reviving entries removed by either a partial or empty `200`. The `304` path returns without parsing the response body, upserting candidates, or enqueueing validation work. Updating `recommended_feeds.last_observed_at` keeps unchanged current members correctly ordered before the worker's bounded candidate input.

Migration `040_explore_provider_materialized_at.sql` backfills a provider only when its maximum persisted observation timestamp exactly matches `last_success_at`. A mismatch is ambiguous: it can mean either that an older worker advanced success on 304 while leaving observations stale, or that the last successful 200 was empty. The migration therefore fails closed for mismatches by clearing conditional validators and `last_sync_at`; the provider becomes immediately due for a full 200 rematerialization. It never guesses that historical observations are current.

## Alternatives Rejected

- Use `provider.last_success_at` in candidate freshness queries: this would keep entries eligible even after a changed `200` response removed them from a provider.
- Disable conditional requests: this would repeatedly download and parse unchanged registries and enqueue redundant validation work.
- Allow empty snapshots: this would hide registry data loss instead of fixing the freshness source.

## Error Handling

The operation locks the provider row, reads its previous `last_materialized_at` and `last_sync_at`, and commits provider success, matching-generation observation refresh, source timestamp refresh, and the new materialized timestamp atomically. A synchronization older than the locked `last_sync_at` is an idempotent no-op; ordinary 200 success uses the same lock and guard. Candidate upserts use `GREATEST` for observation and source timestamps, so delayed writes cannot move freshness backward. A missing provider or any database failure returns an error; the existing registry flow records that synchronization as failed. A provider with no matching observations succeeds without creating any.

## Verification

- Unit test: a `304` uses the not-modified persistence path and performs no candidate upsert or queue enqueue.
- Repository tests: after `A+B` at `t1` and `A` at `t2`, a `304` at `t3` advances only A; after `A+B` at `t1` and an empty successful `200` at `t2`, a `304` at `t3` advances neither entry. Source timestamps, other providers, and occurrence counts remain correct.
- Ordering tests: delayed 200 and 304 success calls older than the committed provider state do not regress validators, provider generation, observations, or source timestamps.
- Migration test: a legacy provider whose `last_success_at` is newer than its observations has its validators and scheduling timestamp cleared, then a forced full 200 establishes the generation that a later 304 safely renews.
- Full backend tests, race tests for the affected packages, and `go vet` pass.
- Tencent production verification confirms the deployed SHA, running worker image/container, provider sync, non-zero candidate inputs, a new `done` Explore batch, and healthy internal/public endpoints.
