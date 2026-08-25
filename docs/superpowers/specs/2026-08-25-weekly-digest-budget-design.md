# Weekly Digest Token Budget and Two-Week Catch-Up Design

## Context

Production weekly digest generation currently calls GLM 5.3 with a 600-token
output budget while leaving thinking enabled. A production-like request used
588 of 600 completion tokens for reasoning and ended with
`finish_reason=length`, leaving only a fragment of visible content. When the
visible content is empty, the worker writes no `weekly_digests` row and the UI
continues to show the week as pending.

The production database is also missing the two most recently completed weekly
digests. The existing startup catch-up checks only the latest completed week.

## Goals

- Give weekly digest generation enough output budget to keep thinking enabled
  and still produce the requested 150-200 Chinese-character introduction.
- On worker startup, generate missing digests for the two most recently
  completed weeks.
- Keep catch-up idempotent: existing weekly digest rows are not regenerated or
  overwritten.

## Non-Goals

- Do not change daily digest generation.
- Do not change thinking configuration for weekly or other AI tasks.
- Do not add retries, fallback rows, or new lifecycle states.
- Do not regenerate weekly digests older than the two most recently completed
  weeks.

## Design

### Weekly AI Budget

Define a named weekly digest output budget of 4096 tokens in the AI weekly
digest implementation. `GenerateWeeklyIntro` passes this value to the existing
non-streaming summarizer call instead of the current literal 600.

No `thinking` field is added to the request. The configured upstream behavior
therefore remains unchanged and GLM 5.3 may continue to reason before producing
the visible introduction.

### Two-Week Startup Catch-Up

Add a small pure helper that derives the two most recently completed Monday
week labels from the current Shanghai time. `runBriefingCatchUp` iterates those
labels and calls the existing `fireWeeklyForAllUsers` for each one.

`WeeklyDigestRepository.UserIDsMissing` remains the idempotency boundary. For
each week, only users without a matching `weekly_digests` row are selected.
Existing rows are left untouched.

The regular Monday 05:00 schedule remains unchanged: it generates only the
single week that has just completed. The two-week window is startup recovery,
not a change to the steady-state cron schedule.

## Data Flow

1. Worker starts and runs briefing catch-up.
2. Catch-up derives the previous two completed Shanghai weeks.
3. For each week, the repository returns users missing a digest row.
4. The worker selects each user's top ten articles for that week.
5. GLM 5.3 generates the introduction with `max_tokens=4096` and existing
   thinking behavior.
6. The existing upsert writes the generated introduction and article IDs.

## Error Handling

Existing weekly error behavior is retained. Repository and AI errors are
logged, and no row is written for a failed generation. This change addresses
the confirmed token-budget exhaustion without expanding scope into retries or
fallback behavior.

## Testing

- Add an AI package test with an `httptest` server that captures the chat
  request and verifies weekly generation sends `max_tokens=4096`.
- Add worker tests for the pure completed-week helper, including a normal
  weekday and a Monday boundary, and verify it returns exactly the previous two
  completed Monday labels in newest-to-oldest order.
- Run `go test ./internal/ai ./cmd/worker -count=1`, followed by `go test ./...`.

## Deployment and Verification

Deployment requires rebuilding and recreating the Tencent `worker` container,
which will be performed only after explicit confirmation at the deployment
boundary.

After deployment:

- Verify the production source revision and worker image.
- Verify worker logs show catch-up attempts for both completed weeks.
- Verify `weekly_digests` contains rows for both target weeks for the affected
  users.
- Verify the authenticated `/api/weekly-digest?week=YYYY-MM-DD` response is no
  longer pending for both weeks.
