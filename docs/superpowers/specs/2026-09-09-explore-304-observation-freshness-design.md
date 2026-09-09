# Explore 304 Observation Freshness Fix

## Problem

Explore registry providers use conditional HTTP requests. A successful `304 Not Modified` updates the provider's `last_success_at`, but it does not update the provider's existing `explore_source_observations.last_seen_at` values. Candidate ranking treats observations older than `max(2 * sync_interval, 6 hours)` as stale, so an unchanged registry eventually contributes zero candidates. Empty snapshots are intentionally rejected, causing every scheduled Explore refresh to fail and fall back to the last successful batch.

## Decision

Add a dedicated `RecordNotModified` operation to the registry store. It will run one transaction that:

1. records provider success, validators, and the synchronization timestamp; and
2. advances `last_seen_at` for every existing observation owned by that provider to the synchronization timestamp.

The `304` path will call this operation and return without parsing the response body, upserting candidates, or enqueueing validation work. Normal `200` synchronization retains its current per-candidate upsert behavior, so entries removed from a changed registry are not incorrectly refreshed.

## Alternatives Rejected

- Use `provider.last_success_at` in candidate freshness queries: this would keep entries eligible even after a changed `200` response removed them from a provider.
- Disable conditional requests: this would repeatedly download and parse unchanged registries and enqueue redundant validation work.
- Allow empty snapshots: this would hide registry data loss instead of fixing the freshness source.

## Error Handling

Provider success and observation refresh must commit atomically. A missing provider or any database failure returns an error; the existing registry flow records that synchronization as failed. Observation timestamps use `GREATEST(last_seen_at, synced_at)` so an out-of-order call cannot move freshness backward.

## Verification

- Unit test: a `304` uses the not-modified persistence path and performs no candidate upsert or queue enqueue.
- Repository test: `RecordNotModified` updates provider success fields and only that provider's observation timestamps, without changing occurrence counts.
- Full backend tests, race tests for the affected packages, and `go vet` pass.
- Tencent production verification confirms the deployed SHA, running worker image/container, provider sync, non-zero candidate inputs, a new `done` Explore batch, and healthy internal/public endpoints.
