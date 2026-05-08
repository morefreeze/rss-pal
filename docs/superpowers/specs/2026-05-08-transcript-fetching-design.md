# Transcript Fetching for Video & Audio Articles

**Status:** Draft
**Date:** 2026-05-08
**Scope:** Fetch a real transcript for video and audio articles, append it into article content (so the user can read along and the summarizer has real text to work with), and let the existing summary backfill loop produce a non-hallucinated summary.

## Goals

- Maximize coverage ("尽可能" per user) for transcript availability across the user's actual feeds — YouTube channels, Bilibili UP主 spaces, BBC podcasts (article 1791 was the motivating example), TED, generic podcast hosts.
- Reuse the existing summarizer pipeline. The transcript work is upstream — once the transcript lands in `article.content`, the existing `backfillSummaries` loop produces a summary as if the article were a regular text article.
- Read-along UX: the transcript becomes part of the article body (per user choice 3B during brainstorming). No new frontend component, no separate panel.
- Apply to both video (`media_type LIKE 'video/%'`) and audio (`media_type LIKE 'audio/%'`) — per user choice 2B. The same fetching mechanism handles both.

## Non-goals

- Speech-to-text (Whisper) fallback for videos with no CC. Real feature, but adds GPU/API cost and minutes-of-latency per video. Defer to its own spec.
- PDF transcript extraction. Linked PDFs are common (BBC Learning English ships PDFs alongside HTML inline transcripts), but Go PDF text extraction is finicky. We follow the inline HTML when present and skip PDFs in v1.
- Multi-language CC selection logic. We pick the first available track, with a soft preference for Chinese, then English, then anything. Configurability deferred.
- Frontend collapsibility for long transcripts. The transcript is markdown like everything else; the existing `MarkdownArticle` renders it. If real transcripts turn out to overwhelm the page, add `<details>` later.
- ASR / yt-dlp / browser-mode fetching. Pure-HTTP only, in keeping with the alpine Go worker container.

## User-visible outcome

- Article 1791 (BBC podcast): summary becomes a real summary of the episode's transcript instead of paraphrasing the title. Article body shows the BBC Learning English transcript inline below the original RSS description.
- A YouTube channel article (e.g. Sequoia Capital `#shorts`): summary based on the actual spoken content. Body shows the transcript inline.
- A Bilibili UP主 video that does not have CC: no transcript appended, no summary generated. Article still renders as today (top-card video + thin description). The system tried once and won't retry.

## Architecture

### One new package: `internal/transcript/`

A `Fetcher` interface and a small set of strategies. The package depends on `internal/model` (for `Article`) and the project's existing HTTP client patterns (see `internal/rss/content.go`). It does NOT depend on `internal/rss` — the relationship is reversed if anything (the worker calls into `transcript`).

```go
package transcript

type Result struct {
    Text   string // the transcript as plain text or simple markdown paragraphs
    Source string // human-readable label, e.g. "YouTube auto-CC", "bbc.co.uk transcript"
}

// Fetcher returns a transcript for the given article, or (nil, nil) when
// no transcript is available. Returns an error only for transient failures
// (network, parse) where retrying might succeed.
type Fetcher interface {
    Fetch(ctx context.Context, article *model.Article) (*Result, error)
}
```

Concrete strategies (each its own file in the package, ~100-200 lines each):

| Strategy | Applies when | What it does |
|----------|-------------|--------------|
| `YouTubeCC` | `media_type == "video/youtube"` | Fetches the YouTube watch page, parses `ytInitialPlayerResponse` JSON for `captions.playerCaptionsTracklistRenderer.captionTracks[]`, picks best track (Chinese > English > first), fetches the track's `baseUrl + "&fmt=json3"`, concats the segment text. |
| `BilibiliCC` | `media_type == "video/bilibili"` | Calls `https://api.bilibili.com/x/web-interface/view?bvid=BV...` to get cid; calls `https://api.bilibili.com/x/player/v2?cid=C&bvid=BV...` for subtitle list (this endpoint does NOT need WBI signing); fetches each `subtitle_url` JSON; concats `body[].content`. |
| `HTMLPageScraper` | Any `media_type` (video or audio) | Fetches `article.URL` HTML (using existing `ContentFetcher` so Jina fallback is available), looks for: (a) a heading whose text contains `transcript` / `字幕` / `Transcript` followed by paragraph text, (b) anchors with text or href suggesting a transcript file (`.vtt`, `.srt`, `.txt`). Found anchors are followed (single-hop) into `LinkedFileFetcher`. |
| `LinkedFileFetcher` | Helper used by `HTMLPageScraper` only | Downloads VTT, SRT, or plain `.txt`. VTT and SRT cues are stripped of timestamps; speaker labels preserved if present. |

A composite `MultiFetcher` runs strategies in priority order (YouTubeCC, BilibiliCC, HTMLPageScraper) and returns the first non-empty `Result`. It's also a `Fetcher`, so callers that don't care about strategy details just inject it.

### Why a new package vs. extending `internal/rss/content.go`

`content.go` is already 600+ lines and conflates direct HTTP, Jina fallback, HTML→Markdown, avatar stripping, media URL detection, and now would also do platform CC API calls and structured-transcript parsing. The strategies here are independently testable (each takes either a captured HTML fixture or a captured API JSON fixture and produces a string) — that test pattern doesn't fit `content.go`'s current shape.

## Data model

One column added to `articles`:

```sql
ALTER TABLE articles ADD COLUMN transcript_fetched_at TIMESTAMPTZ;
```

Semantics:
- `NULL` — never attempted. Eligible for the worker's backfill step.
- non-`NULL` — attempted exactly once. Not retried, regardless of whether a transcript was found.

The transcript text itself goes into the existing `content` column. When the fetcher succeeds, the worker rewrites:

```
<existing content (RSS description / scraped page text)>

---

## 字幕

> 来源：<source label>

<transcript text>
```

The `---`, the `## 字幕` heading, and the source-label blockquote together act as a stable, searchable, human-friendly separator. The format is markdown so the existing `MarkdownArticle` renders it without changes.

## Worker integration

A new step in `runFetchCycle` between `refetchShortContent` and `backfillSummaries`:

```
fetchAllFeeds              (existing)
refetchShortContent        (existing)
backfillTranscripts        (NEW)
backfillSummaries          (existing)
runClassifyCycle           (existing)
```

`backfillTranscripts(ctx, articleRepo, fetcher)`:

1. Calls a new repository method `GetMediaArticlesWithoutTranscript(limit)` that returns articles where `transcript_fetched_at IS NULL` AND (`media_type LIKE 'video/%'` OR `media_type LIKE 'audio/%'`). Limit: `maxTranscriptBackfillPerCycle = 5` (matches summary cadence).
2. For each article: invoke `fetcher.Fetch(ctx, article)`.
3. **On success** (`Result` non-nil with non-empty text):
   - Build the new content blob (existing content + separator + transcript).
   - Call a new repository method `UpdateContentAndResetSummary(articleID, content, transcriptFetchedAt)` that atomically: updates content, recomputes word_count/reading_minutes (already a helper exists: `rss.ComputeMetrics`), clears `summary_brief` and `summary_detailed`, sets `transcript_fetched_at`. Clearing the summary is what feeds the article back to `backfillSummaries` next cycle.
4. **On no transcript found** (`Result` is nil, no error):
   - Call `MarkTranscriptFetchAttempted(articleID)` which only sets `transcript_fetched_at = NOW()`. Existing summary, if any, is left alone.
5. **On error**:
   - Log and skip. Leaves `transcript_fetched_at` NULL so the next cycle retries. Used for transient network errors only.

Concurrency: reuse the existing `maxConcurrentContent = 3` semaphore for HTTP calls. Don't introduce a new semaphore.

### Important guard: `refetchShortContent`

Today's `refetchShortContent` re-fetches articles whose `LENGTH(content) < 300`. After the transcript backfill runs and content gets the transcript appended, the article will exceed 300 chars and naturally be excluded. But until the transcript step runs, video/audio articles with thin descriptions ARE picked up by `refetchShortContent` and could overwrite content with a re-fetch of the article URL (which for YouTube watch pages is JS junk).

Fix: `GetArticlesWithShortContent` query already excludes `feed_type IN ('youtube', 'podcast')`. Extend it to also exclude any article with `media_type LIKE 'video/%' OR media_type LIKE 'audio/%'` — these are the transcript pipeline's responsibility, not refetchShortContent's. This avoids races and clobbering.

## Strategy details

### YouTubeCC

Fetches `https://www.youtube.com/watch?v={ID}` with a normal browser User-Agent. The HTML contains a `<script>` block with `var ytInitialPlayerResponse = {...};`. We extract the JSON via a tolerant regex that captures the balanced object, then `json.Unmarshal` into a minimal struct that walks down `Captions.PlayerCaptionsTracklistRenderer.CaptionTracks`. Each track has:

```go
type captionTrack struct {
    BaseURL      string `json:"baseUrl"`
    LanguageCode string `json:"languageCode"`
    Kind         string `json:"kind"` // "asr" for auto-generated, omitted for human-uploaded
    Name         struct{ SimpleText string `json:"simpleText"` } `json:"name"`
}
```

Selection: prefer `languageCode == "zh"`/`zh-Hans`/`zh-CN` (any), then `en`, then first track. Within the chosen language, prefer non-`asr` over `asr` (matches the user's stated preference for human CC when both exist; takes auto-CC otherwise per Q1 answer "B").

Fetch the chosen track's `BaseURL + "&fmt=json3"`. Returns JSON with `events[].segs[].utf8` — concat all `utf8` strings, separated by spaces, with newlines on segment boundaries that have a non-trivial gap. Output is a single-paragraph-per-event markdown string.

Source label: `YouTube CC` (for human) or `YouTube 自动字幕` (for asr).

Failure modes: anti-bot interstitial (HTML doesn't contain `ytInitialPlayerResponse`), no captionTracks (video has no CC), 4xx/5xx. Each returns `(nil, nil)` (no transcript, no error to retry on) except network-level failures which return `(nil, err)`.

### BilibiliCC

Two API calls, both JSON, neither needs WBI signing as of this writing:

1. `GET https://api.bilibili.com/x/web-interface/view?bvid={BV}` — returns `{data: {cid: int64, ...}}`.
2. `GET https://api.bilibili.com/x/player/v2?cid={CID}&bvid={BV}` — returns `{data: {subtitle: {subtitles: [{subtitle_url, lan}, ...]}}}`.

Note: `subtitle_url` is sometimes protocol-relative (`//i0.hdslb.com/...`). Prefix with `https:` if it starts with `//`.

Selection: prefer `lan` starting with `zh` (Chinese), then any. Fetch the chosen subtitle URL. Returns `{body: [{from, to, content}, ...]}` — concat `content` fields with newlines. Source label: `Bilibili CC`.

Failure: empty subtitles list (most Bilibili videos), 4xx/5xx → `(nil, nil)`. Bilibili's anti-bot for these endpoints has historically been gentle for unauthenticated requests; if it tightens, we revisit.

### HTMLPageScraper

Calls the existing `ContentFetcher.FetchContent(ctx, article.URL)` — but we need the HTML, not just markdown. So a new helper: `ContentFetcher.FetchHTMLDocument(ctx, url) (*goquery.Document, error)` extracted from the direct path of `fetchDirect` (it already builds a `goquery.Document`; we just need it exposed).

On the document, run the following sub-strategies in order. First non-empty wins.

**(a) Inline transcript** — a heading element (`h1`-`h4`) whose text matches `(?i)\b(transcript|字幕|逐字稿)\b`. Take all `<p>` siblings until the next heading, concatenate their text. Accept only if the resulting text is at least 200 chars (sanity guard against headings that aren't real transcripts). Source label: `<host> 网页字幕`.

**(b) Linked transcript file** — anchors with either:
- text matching `(?i)transcript|字幕|逐字稿`, OR
- `href` ending in `.vtt`, `.srt`, or `.txt` AND text/href containing `transcript`/`字幕`

(Both conditions filter out unrelated `.txt` links like robots.txt.) Resolve relative URLs against `article.URL`. Single-hop fetch via `LinkedFileFetcher`. Accept the first matching link that yields non-empty parsed text.

**(c) Two-hop pattern** — if the page contains text like "Find a transcript at: <URL>" (BBC pattern; matched as a paragraph containing `(?i)find\s+(a\s+)?transcript.*?(https?://\S+)`), follow that URL once and re-run (a) and (b) on the linked document. The two-hop result is only attempted if (a) and (b) yielded nothing on the original page.

**Negative path**: if (a), (b), and (c) all yield nothing, return `(nil, nil)`.

### LinkedFileFetcher

Internal helper. Given a URL ending in `.vtt`, `.srt`, or `.txt`:
- Download the body (size cap: 1 MB).
- `.vtt`: strip `WEBVTT` header, strip lines that are timestamps (`00:00:01.000 --> 00:00:04.000`) or numeric cue indices, keep text lines.
- `.srt`: same — strip cue indices and `00:00:01,000 --> 00:00:04,000` lines.
- `.txt`: pass through.

Concat with newlines. No speaker-tag rewriting in v1.

## Edge cases & failure modes

- **Article with no `media_type`** (text article that happens to link to a YouTube video): not in scope. The repository query filter ensures only `audio/*` and `video/*` articles are considered.
- **Article with `media_type` but the ID isn't extractable** (e.g. unusual Bilibili URL form): `YouTubeCC` / `BilibiliCC` skip; `HTMLPageScraper` still runs on the article URL.
- **Article URL is a YouTube watch URL** (so `article.URL` matches the platform): `YouTubeCC` runs against `article.URL` directly (not `media_url` — `media_url` is the embed form).
- **Transcript is enormous** (3-hour Bilibili archive, when CC exists): stored as-is. Summarizer truncates at 8000 runes (see `internal/ai/summarizer.go:81`); rendering the full transcript in the article body is fine — markdown handles it. Future: optional `<details>` wrap in frontend.
- **Multiple successful strategies (e.g. YouTube CC works AND HTML page has a TED-style transcript)**: priority ordering picks the first hit. We don't try to merge.
- **Already-attempted articles**: `transcript_fetched_at IS NOT NULL` excludes them from the worker query. To re-attempt manually: `UPDATE articles SET transcript_fetched_at = NULL WHERE id = ?`.
- **A summary already exists from before this feature** (article 1791 has a current thin summary): on success, the worker clears the summary so the existing `backfillSummaries` re-runs against the now-richer content.
- **HTMLPageScraper false positive** (catches a "Transcript" heading that isn't a real transcript): summary will be off. Acceptable: skip will still beat the current behavior, and for the heading branch we require ≥200 chars of paragraph content as a sanity guard.
- **Network errors**: returned as errors so the article's `transcript_fetched_at` stays NULL and is retried next cycle. Persistent failures will retry on every cycle until they eventually succeed or until human intervention — this matches existing `IncrementRefetchAttempts` semantics for content. Acceptable for v1; if a feed is permanently broken we'll see it in worker logs.

## Testing

### Backend unit tests (per strategy)

Each strategy gets a fixture-driven test:

- **`YouTubeCC`**: a captured `watch?v=...` HTML fixture with a plausible `ytInitialPlayerResponse`; a captured `fmt=json3` track response. Test: parses, picks the right track (Chinese over English, non-asr over asr), concats text correctly. Negative cases: HTML without `ytInitialPlayerResponse` → returns nil; tracks list empty → returns nil.
- **`BilibiliCC`**: captured `view?bvid=...` JSON, captured `player/v2` JSON, captured subtitle JSON. Test: chains correctly. Negative case: subtitles list empty.
- **`HTMLPageScraper`**: captured BBC programme HTML (the article-1791 case), captured TED talk HTML, a generic blog page with no transcript. Test: detects the BBC "Find a transcript at" two-hop pattern, detects TED inline transcript, returns nil for the generic page.
- **`LinkedFileFetcher`**: short captured `.vtt`, `.srt`, `.txt`. Test: parses correctly.
- **`MultiFetcher`**: priority ordering, first-hit wins, transient errors don't poison subsequent strategies.

### Worker integration

- Test `backfillTranscripts` against a stub fetcher: success appends transcript and clears summary; nil result sets timestamp without touching content; error leaves timestamp NULL.
- Test the new `GetMediaArticlesWithoutTranscript` repository query against a real test DB if one is available; otherwise inspect SQL by hand.

### Manual smoke test before merge

After Docker rebuild:
1. Wait one fetch cycle. Article 1791's summary should refresh; article body should show the BBC Learning English transcript appended below the original description.
2. A YouTube channel article (e.g. an existing one in DB) should similarly get a transcript + new summary on the next cycle.
3. A Bilibili UP article with no CC should land in DB with `transcript_fetched_at` set but no transcript appended; summary stays empty.

### Frontend

No frontend changes. The existing `MarkdownArticle` renders the appended transcript correctly. Verify in the browser only — no test changes.

## Open questions

None at brainstorm-close. WBI signing for Bilibili is the most likely future flake; if `player/v2` starts requiring it, we'll add the signing logic as a follow-up commit.
