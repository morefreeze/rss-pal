# Weekly Digest Generation Status Design

## Context

`GET /api/weekly-digest` currently returns `pending: true` whenever a weekly
digest row is absent. The frontend consequently renders every missing digest
as `周报生成中,稍后刷新…`, even when the requested week has not finished yet
or is too old for the worker's automatic catch-up window.

Weekly generation currently starts at 05:00 Asia/Shanghai on the Monday after
the covered week. On worker startup, only the two most recently completed
weeks are eligible for catch-up.

## Goals

- Show the expected generation start time, to the hour, before a week becomes
  eligible for generation.
- Keep the existing in-progress message while a missing digest remains inside
  the automatic generation window.
- Explicitly say that a missing digest will not be generated after it leaves
  the automatic generation window.
- Keep the API backward compatible for existing clients that only read
  `pending`.
- Keep the API and worker on one shared definition of generation hour and
  catch-up window.

## Non-goals

- Do not change when the worker runs.
- Do not change the two-week catch-up window.
- Do not add manual generation or retry controls.
- Do not regenerate historical weekly digests.
- Do not change daily digest behavior.

## Generation states

The API exposes `generation_status` with one of four values:

| Status | Condition | Frontend behavior |
| --- | --- | --- |
| `ready` | A cached digest row exists | Render the digest normally |
| `scheduled` | No row exists and current time is before the scheduled start | Show `预计于 YYYY-MM-DD HH:00（北京时间）开始生成` |
| `pending` | No row exists, the scheduled start has passed, and the week is one of the two most recently completed weeks | Show `周报生成中，稍后刷新…` |
| `not_planned` | No row exists and the week is outside the two-week automatic catch-up window | Show `该周报已过自动生成范围，不再生成。` |

The scheduled start is 05:00 Asia/Shanghai on the Monday immediately after the
requested week. At exactly that timestamp, the state changes from `scheduled`
to `pending`.

Future requested weeks remain `scheduled` and use their own following Monday
as the estimated start. A missing week older than the two most recently
completed weeks is `not_planned` even if it was previously eligible.

## API contract

`GET /api/weekly-digest` adds these fields:

```json
{
  "generation_status": "scheduled",
  "estimated_generation_at": "2026-08-31T05:00:00+08:00"
}
```

`estimated_generation_at` is included only for `scheduled`. The existing
`pending` field remains:

- `true` for `scheduled` and `pending`, because no digest exists and older
  clients should continue to show a placeholder;
- `false` for `ready` and `not_planned`.

The API computes the status after normalizing the requested date to its Monday
week label. Cached data always wins: if a row exists, the response is `ready`
regardless of its age.

## Shared schedule rules

Move the weekly schedule constants and pure time calculations into the shared
API package already imported by the worker:

- generation hour: `05:00` Asia/Shanghai;
- catch-up count: two completed weeks;
- scheduled start for a week;
- missing-digest state at a supplied `now`.

The worker reuses the shared catch-up count instead of defining a second local
constant. The API handler uses the same helpers to build the response.

## Frontend

Extend `WeeklyDigest` with `generation_status` and optional
`estimated_generation_at`. Keep digest content rendering unchanged. For an
empty response, select exactly one message from the state:

- `scheduled`: format the server timestamp in Asia/Shanghai as
  `YYYY-MM-DD HH:00` and append `（北京时间）`;
- `pending`: show the existing message with Chinese punctuation;
- `not_planned`: show the explicit no-generation message.

If an older server omits `generation_status`, fall back to the existing
`pending` behavior.

## Error handling

- Invalid `week` query handling remains unchanged.
- If `scheduled` is returned without `estimated_generation_at`, the frontend
  falls back to the pending message instead of rendering an invalid date.
- API repository errors remain HTTP 500 and are not converted into generation
  states.

## Testing

Backend tests cover:

- before the following Monday 05:00 is `scheduled` with the exact timestamp;
- exactly at 05:00 is `pending` for an eligible week;
- the second completed week remains `pending`;
- an older missing week is `not_planned`;
- a cached digest is `ready`;
- `pending` remains backward compatible for every state;
- the worker catch-up helper still returns exactly the shared two-week window.

Frontend tests cover the three empty-state messages and the legacy response
fallback. The full frontend test suite/build and backend `go test ./...` must
pass before deployment.

## Deployment verification

- Deploy through the existing `master` GitHub Actions workflow.
- Verify the deployed commit and Tencent frontend/API containers.
- Verify public `/api/health` and `/api/status`.
- With an authenticated production session, open
  `/weekly?week=2026-08-24` before 2026-08-31 05:00 Asia/Shanghai and confirm it
  displays the expected start time.
- Open a missing week outside the catch-up window and confirm it displays the
  no-generation state.
