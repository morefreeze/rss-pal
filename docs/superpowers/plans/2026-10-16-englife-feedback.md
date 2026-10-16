# Video duration and borderless article feedback

**Goal:** Show source-confirmed video length separately from reading time, and replace article like/dislike controls with borderless thumbs icons.

**Investigation:** Share 8HCRo3xaDVzD is article 3232, video cgV74qRWolo and englife document 198516fa-3f58-43e3-9a76-7a73cfa2b892. Its 97-second video has one chapter and one segment containing three translated paragraphs. All translated text is already present; preserve the body.

**Design:** Use existing article/share media_duration_seconds. ReadingMeta renders video m:ss or h:mm:ss beside reading metrics, including when word count is absent. Unknown video length remains explicit. Persist englife duration with cached jobs (migration 056), pass it through transcript results, and atomically save it with content in worker and manual-summary paths. Collect existing metadata in YouTubeCC, BilibiliCC and yt-dlp without additional generation. Existing ready caches with unknown length remain unknown; only the reported article/share is backfilled.

Feedback retains existing handlers, mutual exclusion and cancellation. Inline SVG icons have stable accessible labels, aria-pressed, dynamic title, green/red active colors, transparent background, no border and 44px hit targets.

- [x] Regression tests fail before implementation for duration rendering, collection and feedback icons.
- [x] Verify englife cache reuse, SQL persistence, migration reapplication and preservation of known duration in isolated PostgreSQL.
- [x] Cover manual-summary duration propagation and public-share presentation.
- [x] Frontend regression suite and production build; Go package checks and server/worker build.
- [x] Independent code review; repair manual-summary omission and re-review.
- [ ] Push integrated master, verify Tencent deployment, backfill only article 3232/share 8HCRo3xaDVzD, verify public API and rendered page.

Expanded repository-wide database testing encountered local Docker disk exhaustion and connection exhaustion; the clean isolated database passed the relevant duration, cache, migration and persistence tests sequentially. No unrelated Docker images or volumes were removed.
