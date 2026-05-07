# Podcast Audio Playback — Design

**Date:** 2026-05-07
**Status:** Draft for review
**Scope:** Add audio playback for podcast feeds (RSS `<enclosure>` audio). Video / YouTube / inline `<audio>` tags in scraped content are out of scope and tracked as future iterations.

---

## 1. Goal

RSS Pal currently has `feed_type='podcast'` as a recommendation category but no actual playback path — podcast feeds skip deep content fetch (`api/feed.go:293`) and `<enclosure>` data is never read. Users who subscribe to a podcast feed see entries in their list but cannot listen.

This spec adds a global mini-player so users can play any podcast episode, with cross-page continuity, cloud-synced playback position, and a recommendation signal for completed listens.

## 2. Non-goals

- Video playback (YouTube, native `<video>`)
- Inline `<audio>` / `<video>` tags inside scraped article HTML
- Playback queue (next-up, autoplay next episode)
- Sleep timer
- Chapter markers / shownote parsing
- Offline download
- Streaming through a backend proxy (direct streaming from podcast host is sufficient)

## 3. User flows

1. **From article list** — user opens a feed of `feed_type='podcast'`. Each item with `media_url` shows a ▶ button next to its title. Clicking ▶ starts playback in the global mini-player without navigating away.
2. **From article page** — user opens an episode. A player card above the article body shows a large play button and total duration. Click → mini-player starts; the card mirrors play/pause state.
3. **Cross-page continuity** — user starts an episode, navigates to `/insights` or `/feeds`. Audio keeps playing. Mini-player stays fixed at the bottom.
4. **Resume** — user closes the tab mid-episode. Next time they open the same article (or click ▶ in the list), playback resumes from the saved position. Works across devices.
5. **Completion** — when an episode finishes (or position ≥ 95%), `is_completed` flips to `true` and a recommendation signal is recorded.
6. **Mobile lock screen** — on mobile, `MediaSession` API surfaces play/pause/skip controls on the lock screen.

## 4. Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         Frontend (React)                    │
│                                                             │
│  Layout.tsx ── PlayerProvider ── <audio ref=ref/>  ◄─ 全局  │
│       │              │                 │                    │
│       │              ▼                 │                    │
│       │      MiniPlayer.tsx ───────────┘                    │
│       │   (倍速 / -5s / +10s / 拖条 / ✕)                   │
│       │                                                     │
│       ├─ ArticleListPage  ── ▶ (when media_url set) ──┐    │
│       └─ ArticlePage      ── ArticlePlayerCard ───────┤    │
│                                                       │    │
│                          playArticle(article)─────────┘    │
│                                                             │
│         每 10s 上报 progress  ──► PUT /api/articles/:id/playback
│         播放结束/手动标记完成 ──► 同上 (is_completed=true)  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      Backend (Go / Gin)                     │
│                                                             │
│  GET  /api/articles/:id          ──► includes media_*       │
│  GET  /api/articles/:id/playback ──► position_sec, completed│
│  PUT  /api/articles/:id/playback ──► upsert + signal write  │
│                                                             │
│  cmd/worker (每分钟轮询)                                    │
│   └─ rss.Fetcher                                            │
│       └─ rss.ExtractMedia(item)                             │
│       └─ INSERT ... ON CONFLICT (feed_id, url) DO UPDATE    │
│           SET media_* WHERE media_url IS NULL               │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      PostgreSQL                             │
│                                                             │
│  articles (+3 列)                                           │
│  playback_progress (新表, UNIQUE(user_id, article_id))      │
│  user_preferences (复用, signal_type='completed_listen')    │
└─────────────────────────────────────────────────────────────┘
```

## 5. Database

New migration `backend/migrations/011_audio_video.sql`:

```sql
ALTER TABLE articles
    ADD COLUMN IF NOT EXISTS media_url VARCHAR(2048),
    ADD COLUMN IF NOT EXISTS media_type VARCHAR(64),
    ADD COLUMN IF NOT EXISTS media_duration_seconds INT;

CREATE TABLE IF NOT EXISTS playback_progress (
    id SERIAL PRIMARY KEY,
    user_id INT REFERENCES users(id) ON DELETE CASCADE,
    article_id INT REFERENCES articles(id) ON DELETE CASCADE,
    position_seconds INT DEFAULT 0,
    last_played_at TIMESTAMP DEFAULT NOW(),
    is_completed BOOLEAN DEFAULT false,
    UNIQUE(user_id, article_id)
);

CREATE INDEX idx_playback_progress_user ON playback_progress(user_id);
```

**Rationale**

- `INT` seconds (not `FLOAT`) — sub-second precision is meaningless for podcasts; `Math.floor(audio.currentTime)` is the natural unit.
- `(user_id, article_id)` UNIQUE — supports `ON CONFLICT … DO UPDATE` upserts.
- Separate from `reading_progress` — scroll position is normalized 0–1, playback is absolute seconds; "completed" semantics also differ (read to bottom vs. listened ≥95%).
- `user_preferences` table is **not** modified; we only introduce a new `signal_type='completed_listen'` value (existing schema already accepts arbitrary strings).

## 6. Backend

### 6.1 RSS media extraction

New file `backend/internal/rss/media.go`:

```go
type MediaInfo struct {
    URL      string
    Type     string  // e.g. "audio/mpeg"
    Duration int     // seconds, 0 if unknown
}

// ExtractMedia returns the first audio/video enclosure for an item, or nil.
// Filters by Type prefix "audio/" or "video/" to skip image enclosures.
// Duration parses item.ITunesExt.Duration if present (formats: "hh:mm:ss",
// "mm:ss", or raw seconds). enclosure.Length is bytes, not seconds — ignored.
func ExtractMedia(item *gofeed.Item) *MediaInfo
```

Pure function; unit-testable in isolation.

### 6.2 Worker write path

`internal/repository/article.go` `Create`/`Upsert` extends the SQL to write `media_url, media_type, media_duration_seconds`. The `ON CONFLICT (feed_id, url)` clause becomes `DO UPDATE SET media_url = EXCLUDED.media_url, media_type = EXCLUDED.media_type, media_duration_seconds = EXCLUDED.media_duration_seconds WHERE articles.media_url IS NULL`.

The `WHERE articles.media_url IS NULL` predicate makes backfill idempotent: existing rows without media_url get filled on the next poll cycle (RSS payload still contains them); already-filled rows are not touched on subsequent polls. No standalone backfill script needed because podcast RSS feeds typically list 50–500 historical episodes in every poll.

### 6.3 New API

| Method | Path | Body / Response |
|---|---|---|
| `GET` | `/api/articles/:id/playback` | `{ position_seconds, is_completed }`. No row → `{ 0, false }` |
| `PUT` | `/api/articles/:id/playback` | Body `{ position_seconds, is_completed }`. Upserts `playback_progress`. When `is_completed` transitions `false → true`, also inserts `user_preferences(user_id, article_id, signal_type='completed_listen', signal_value=8)` — guarded by `WHERE NOT EXISTS (SELECT 1 FROM user_preferences WHERE article_id=$1 AND signal_type='completed_listen')` for idempotency. |

`GET /api/articles/:id` already returns the full `Article` struct, so adding `MediaURL`, `MediaType`, `MediaDurationSeconds` fields automatically exposes them — no new endpoint.

### 6.4 Recommendation scoring

The spec assumes recommendation scoring code accepts arbitrary `signal_type` values via `GROUP BY signal_type`. **Implementation step:** before merging, audit `internal/service/recommendation.go` (or whichever file owns the scoring SQL). If signal types are hard-coded, add `'completed_listen'` with weight `8`. If they aggregate generically, no change needed.

`signal_value=8` rationale: ranks above `like=5` and `save=3`, below `dislike=-10`. Listening through a 40-minute podcast is a stronger commitment than a single click, and we want this to noticeably bias future recommendations toward that show.

### 6.5 What's intentionally not done

- No backend audio proxy. `<audio>` element streaming does not require CORS for direct playback; podcast hosts overwhelmingly allow hotlinking (it's how every podcast app works).
- No `<podcast:chapters>` / shownote parsing.
- No download-to-local feature.

## 7. Frontend

### 7.1 PlayerContext

New module `frontend/src/player/PlayerContext.tsx`:

```ts
type PlayerState = {
  articleId: number | null
  title: string
  feedTitle: string
  src: string
  duration: number
  position: number
  playing: boolean
  speed: 1 | 1.25 | 1.5 | 1.75 | 2
  loading: boolean
  error: string | null
}

type PlayerActions = {
  playArticle(article: Article): Promise<void>
  toggle(): void
  seek(sec: number): void
  skip(deltaSec: number): void   // +10 forward, -5 back
  setSpeed(s: number): void
  close(): void
}
```

`<PlayerProvider>` wraps `<Outlet/>` inside `Layout.tsx`. The provider owns:

- A real `<audio ref={ref} preload="metadata" />` element rendered inline (NOT `new Audio()`) so React controls its lifecycle.
- `useEffect` listeners for `loadedmetadata`, `timeupdate`, `play`, `pause`, `ended`, `error` synchronizing into state.
- A `setInterval(10s)` that calls `PUT /playback` while `playing===true`. Plus immediate flushes on `pause`, `ended`, switching `playArticle()` to a different episode, and provider unmount. Route navigation alone does NOT trigger a flush — audio keeps playing across routes; the 10s tick continues.
- On `ended` (or `position/duration ≥ 0.95` reached): writes `is_completed=true` once, sets `playing=false`, keeps `src` so the UI shows "played".

### 7.2 MiniPlayer component

New `frontend/src/components/MiniPlayer.tsx`:

```
┌────────────────────────────────────────────────────────────┐
│ ▶/⏸  ⏪5  ⏩10   ━━━━●─────────  12:34/42:13   1.5x ▼  ✕  │
│                                                            │
│ 节目标题 · 来自 Feed 名                                    │
└────────────────────────────────────────────────────────────┘
```

- `position: fixed; bottom: 0; left: 0; right: 0`, ~64px tall, plain CSS to match project conventions (no UI library).
- Renders only when `articleId !== null`.
- Scrub bar uses native `<input type="range">`; `seek()` fires on `onChange` end (mouseup), not on every drag pixel, to avoid hammering the backend.
- Speed selector: dropdown with 5 fixed values.
- ✕ → `close()`: pause + flush final position + clear state. Does NOT mark completed.

### 7.3 List & detail entry points

**`ArticleListPage.tsx`** — for items where `article.media_url` is non-empty, render a small ▶ button next to the title. Click handler does `e.stopPropagation()` + `playArticle(article)` so it does not navigate to the article page.

The condition is `media_url`, not `feed_type==='podcast'`, because (a) it future-proofs against blogs that occasionally attach enclosures, and (b) old podcast articles need worker backfill before becoming playable — `media_url` is the accurate signal.

**`ArticlePage.tsx`** — new component `<ArticlePlayerCard article={article} />` rendered between title and body. If `media_url` is non-empty, it shows a large play button + duration. Click → `playArticle()`. If the mini-player is currently playing this article, the button mirrors its play/pause state.

### 7.4 Types & API client

- `frontend/src/api/types.ts` (or equivalent): `Article` gains `media_url?: string; media_type?: string; media_duration_seconds?: number`.
- `frontend/src/api/client.ts`:
  ```ts
  getPlayback(articleId: number): Promise<{position_seconds: number, is_completed: boolean}>
  putPlayback(articleId: number, body: {position_seconds: number, is_completed: boolean}): Promise<void>
  ```

### 7.5 Lock-screen / MediaSession

When playback starts, set:

```ts
navigator.mediaSession.metadata = new MediaMetadata({
  title, artist: feedTitle,
})
navigator.mediaSession.setActionHandler('play', toggle)
navigator.mediaSession.setActionHandler('pause', toggle)
navigator.mediaSession.setActionHandler('seekforward', () => skip(10))
navigator.mediaSession.setActionHandler('seekbackward', () => skip(-5))
```

~20 lines, zero new dependencies. iOS/Android lock-screen and media keys (Bluetooth headsets) work for free.

## 8. Error handling & edge cases

- **Media load error** (404 / CORS / network): `<audio onerror>` → state `error="无法加载音频"`. Mini-player shows the error and ▶ becomes a retry button.
- **`PUT /playback` 5xx**: local playback continues; the next 10s tick retries. No user-facing failure.
- **`PUT /playback` 401**: user logged out — stop further uploads.
- **Two tabs open with the same episode**: last write wins. Acceptable: rare, no locking warranted, write frequency is low.
- **Switching articles while one is playing**: flush current position immediately, then swap `<audio>.src` to the new article.
- **`media_duration_seconds` is NULL** (e.g., feed didn't ship `itunes:duration`): UI shows `--:--` until `loadedmetadata` fires, then uses `audio.duration`.
- **Completion detection**: `audio.ended` event OR (`position / duration ≥ 0.95` AND playback paused/exited). Backend guards against double-write of `completed_listen` signal.

## 9. Testing

- **Go: `internal/rss/media_test.go`** — `ExtractMedia` against fixtures: audio enclosure, video enclosure, image enclosure (must be ignored), no enclosure, multiple enclosures, `itunes:duration` in three formats (`hh:mm:ss`, `mm:ss`, raw seconds), missing duration.
- **Go: `internal/repository/article_test.go`** — UPSERT does not overwrite `media_url` when row already has one; backfill DOES write when row had NULL.
- **Go: handler integration tests** — `GET /playback` empty → defaults; `PUT /playback` upserts; `is_completed` transition writes exactly one `completed_listen` user_preferences row even on repeated PUTs.
- **Frontend** — no automated tests in this iteration. The project currently has no frontend test infrastructure; introducing it is out of scope. Manual validation via the dockerized app, plus a Playwright smoke pass for the play-from-list and resume flows.

## 10. Rollout

1. Migration `011_audio_video.sql` runs automatically via Docker entrypoint.
2. First worker poll cycle after deploy backfills `media_url` on all existing podcast articles whose RSS feed still lists them.
3. Frontend ships behind no flag — the player UI only appears for articles where `media_url` is set, so non-podcast users see no change.

## 11. Open implementation notes

- Audit the recommendation scoring SQL (§6.4) before adding `completed_listen`. Spec assumes generic `GROUP BY signal_type`; if it's a hard-coded list, add the type explicitly.
- Confirm `internal/repository/article.go` `Create`/`Upsert` is the single insertion path used by both worker and bookmarklet flows; if there are two separate paths, both need the new columns.
