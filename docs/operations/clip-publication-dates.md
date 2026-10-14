# Web clip publication dates

Web captures retain publication meta and inert JSON-LD. The backend extracts explicit publication dates before cleaning HTML. New captures save the date; recapture and manual content refresh only fill a missing date. Existing dates are preserved. Updated dates, arbitrary body dates, URLs, comment dates and relative dates are not publication evidence.

The extension download contains the new capture logic. Existing installations must update/reload the extension and refresh the source tab; existing saved bookmarklets must be replaced from Settings. Already captured HTML containing standard publication meta works with the backend update alone.

## Historical repair

Use the API image's `./backfill_clip_dates`. Both `--owner` and an inclusive RFC3339 `--since` are required. `--until` is exclusive and defaults to command start. The window is based on capture time, not the publication date being recovered. Preview is the default:

```sh
docker compose exec -T api ./backfill_clip_dates --owner 1 --since 2026-09-13T16:00:00Z --until 2026-10-14T03:00:00Z
```

Review JSONL results. `preview` means a source date was verified; `no_verified_date`, `date_after_capture`, and `fetch_failed` require no write. Before applying, back up the selected articles' IDs, URLs, fetched/published timestamps and associated share snapshots. Use the same window plus `--apply` to perform the approved repair.

Each article update rechecks owner, clip feed/type, URL, capture window and NULL date. The transaction fills the article and any active, owned share snapshot with a missing date; it changes no other snapshot fields and never revives expired/revoked links. Rerunning skips repaired articles. Public shares normally remain immutable; this explicit operator repair is the narrow exception.

Verify resulting database dates, unchanged snapshot fields and the rendered share page. Keep source failures separate from pages with no identifiable date. PDF creation metadata is not treated as an article publication date.
