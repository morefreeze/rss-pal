# Video Embed Support (YouTube + Bilibili)

**Status:** Draft
**Date:** 2026-05-07
**Scope:** Add embedded video playback to RSS Pal, prioritising YouTube and Bilibili. Covers both video-first articles (e.g. YouTube channel feeds) and inline videos embedded inside blog posts.

## Goals

- A YouTube/Bilibili video that arrives via RSS is playable inside the reader without leaving the page.
- The two distinct delivery shapes are both handled:
  - **Case A — video-first articles**: the article's main link is a video URL (typical of YouTube channel RSS or RSSHub-emitted Bilibili feeds). Render a top-of-article video card.
  - **Case B — inline videos in blog posts**: a blog article embeds a YouTube/Bilibili iframe or links to the video in passing. Render the embed in place where it appeared.
- Reuse the existing `media_url` / `media_type` / `media_duration_seconds` columns introduced for podcast audio. No new database schema.
- Detection runs at fetch time (in the worker / content fetcher), so existing articles backfill via the existing "re-fetch short content" pass.

## Non-goals

- Direct `<video>` tags pointing at `.mp4` / `.webm` files. The existing media-detection path already covers these for audio; extending it to inline `<video>` tags is out of scope here.
- YouTube playlists (`?list=...`). The embed renders the single video only.
- Bilibili AV-form IDs (`av12345...`) and `b23.tv` short links. AV IDs are essentially dead in active feeds; `b23.tv` requires a redirect resolution per article, which we don't want to pay for v1.
- Vimeo, Tencent Video, Youku, and other platforms.
- Click-to-play / consent overlays. We accept the privacy tradeoff of loading third-party iframes; `youtube-nocookie.com` and `referrerpolicy="no-referrer"` are the only mitigations.
- One-shot historical backfill migration. Existing articles backfill organically when the worker re-fetches them.

## Supported URL formats

**YouTube** (all map to a single video ID):

- `youtube.com/watch?v=ID`
- `youtu.be/ID`
- `youtube.com/embed/ID`
- `youtube.com/shorts/ID`

A `?t=` / `#t=` start-time parameter is preserved if present.

**Bilibili**:

- `bilibili.com/video/BV...` (modern BV ID — what RSSHub emits)

A `?p=` page parameter is preserved if present; otherwise defaults to page 1. A `?t=` start-time parameter is preserved if present.

## Architecture

The pipeline mirrors the audio (podcast) pipeline that already exists in `internal/rss/`. There are two integration points on the backend (one per case) and two on the frontend.

```
RSS item ──► Fetcher ──► ExtractVideo(item.Link) ────────► media_url / media_type   (Case A)
                │
                └─► Content fetch ──► HTML→Markdown step
                                      ├─► <iframe> handler emits [[video:...]] placeholder   (Case B)
                                      └─► Post-conversion URL scan rewrites bare/linked URLs (Case B)

DB row ──► API ──► Frontend
                  ├─► ArticlePlayerCard branches on media_type → <VideoEmbed>   (Case A)
                  └─► MarkdownArticle paragraph override → <VideoEmbed>          (Case B)
```

## Data model

No schema changes. Reuse the columns that podcast audio already uses on `articles`:

| Column                    | Audio (existing)        | Video (new)                                           |
|---------------------------|-------------------------|-------------------------------------------------------|
| `media_url`               | direct audio file URL   | canonical embed URL (`youtube-nocookie.com/embed/ID`, `player.bilibili.com/player.html?bvid=...`) |
| `media_type`              | `audio/mpeg`, etc.      | `video/youtube` or `video/bilibili`                  |
| `media_duration_seconds`  | from `itunes:duration`  | from feed if available, else null                     |

Inline embeds (Case B) live inside the markdown `content` column as `[[video:platform:id]]` placeholders. Nothing about them is denormalised into separate columns.

## Backend: detection

### New package: `internal/rss/video.go`

```go
type VideoEmbed struct {
    Platform string // "youtube" | "bilibili"
    ID       string // dQw4w9WgXcQ, BV1xx...
    Start    int    // seconds, optional
    Page     int    // bilibili only, optional (default 1)
    EmbedURL string // canonical iframe src
}

// ExtractVideo parses a single URL and returns a VideoEmbed if it
// matches a supported platform. Regex-only, no network calls.
func ExtractVideo(rawURL string) (*VideoEmbed, bool)

// FindVideosInMarkdown scans markdown body text and returns each
// matched VideoEmbed in order of appearance, including the byte
// offsets of the matched text so callers can rewrite in place.
func FindVideosInMarkdown(md string) []VideoMatch

type VideoMatch struct {
    VideoEmbed
    Start int // byte offset in source markdown
    End   int
    Raw   string // original text (URL or [text](url) form)
}
```

Implementation notes:

- Regexes are compiled once at package init.
- YouTube ID character set: `[\w-]{11}`. Bilibili BV ID: `BV[\w]{10}`.
- `EmbedURL` is constructed deterministically from `Platform`, `ID`, `Start`, `Page`. Frontend re-derives it the same way; backend just stores it for convenience and observability.

### Case A integration: top-card detection

In the worker post-fetch step (next to where `rss.ExtractMedia` is called for podcast audio in `internal/rss/media.go`), call `ExtractVideo(item.Link)` *before* `ExtractMedia`. If `ExtractVideo` matches:

- Set `article.MediaURL = embed.EmbedURL`
- Set `article.MediaType = "video/" + embed.Platform`
- Set `article.MediaDurationSeconds` from `itunes:duration` if present (rare but harmless)

If `ExtractVideo` does not match, fall through to the existing audio extraction path. This means a single article never has both a video card and an audio card — first match wins, video preferred.

This step runs on every fetch, including the existing "re-fetch articles with short content" loop, so existing articles backfill organically.

### Case B integration: inline embed preservation

Two changes in `internal/rss/content.go`:

1. **Iframe preservation hook** during HTML→Markdown conversion. Register a custom rule with `html-to-markdown/v2` that matches `<iframe>` whose `src` matches a YouTube or Bilibili URL pattern and emits `[[video:platform:id]]` (with `:start` suffix when nonzero) on its own line, instead of letting the iframe be silently dropped. Iframes that don't match a supported platform continue to be dropped as today.

2. **Post-conversion URL fallback scan**. After the markdown is produced, run `FindVideosInMarkdown` and rewrite each match into the same `[[video:platform:id]]` placeholder. This catches links that were never iframes — bare URLs and `[text](url)` markdown links.

3. **De-duplication with the top card** (the "A3 strip" agreed during brainstorming). After steps 1 and 2, if `article.MediaURL` is set and points to a video, scan the body for any placeholder with the same platform + ID and remove it (and any surrounding empty paragraph). This prevents the same video appearing twice — once as the top card, once inline.

### Placeholder grammar

```
[[video:<platform>:<id>(?<query>)?]]
```

- `<platform>` ∈ `{youtube, bilibili}`
- `<id>` matches `[\w-]+`
- `<query>`, when present, is a `&`-separated list of `key=value` pairs from the allow-list `{start, page}`. `start` is seconds (non-negative integer); `page` is a positive integer (Bilibili only). Keys and values not in the allow-list are dropped during placeholder construction.
- Both keys are omitted entirely when 0/unset, so the simplest form remains `[[video:youtube:dQw4w9WgXcQ]]`.
- Examples: `[[video:youtube:dQw4w9WgXcQ?start=42]]`, `[[video:bilibili:BV1xx411c7mD?page=2&start=15]]`.
- The placeholder must occupy its own paragraph (preceded and followed by a blank line) for the frontend renderer to pick it up.

The format was chosen because it (a) survives further markdown processing untouched, (b) is grep-friendly for debugging stored articles, and (c) can be matched on the frontend with a single regex applied to a paragraph node's text.

## Frontend: rendering

### New component: `frontend/src/components/VideoEmbed.tsx`

```tsx
type Props = {
  platform: 'youtube' | 'bilibili'
  id: string
  start?: number
  page?: number  // bilibili only
}
```

Renders a 16:9 responsive container (`aspect-ratio: 16 / 9; width: 100%; max-width: 800px`) with a single iframe.

Embed URLs:

- YouTube: `https://www.youtube-nocookie.com/embed/{id}?rel=0` plus `&start={start}` if set.
- Bilibili: `https://player.bilibili.com/player.html?bvid={id}&high_quality=1&autoplay=0&page={page||1}` plus `&t={start}` if set.

Iframe attributes: `allowfullscreen`, `loading="lazy"`, `referrerpolicy="no-referrer"`, `allow="encrypted-media; picture-in-picture"`. No autoplay.

### Case A integration: top-card video

Extend `frontend/src/components/ArticlePlayerCard.tsx` (the component that today renders the podcast audio player). Add a branch: when `article.media_type` starts with `video/`, render `<VideoEmbed>`. The audio rendering path is unchanged.

A small helper `parseStoredEmbed(mediaURL, mediaType)` (colocated with `VideoEmbed.tsx`) takes the stored embed URL and the `video/<platform>` type and returns `{ platform, id, start?, page? }` — the same shape `VideoEmbed` accepts as props. The parser handles both canonical embed URL forms (`youtube-nocookie.com/embed/ID`, `player.bilibili.com/player.html?bvid=...`).

If parsing fails (unexpected, but treat defensively), render nothing — same null-return pattern the audio path uses today when `media_url` is missing.

### Case B integration: inline placeholders

In `frontend/src/components/MarkdownArticle.tsx`, add a custom `p` (paragraph) component to the `react-markdown` overrides. The override:

1. Inspects the paragraph's text content.
2. Matches against `/^\[\[video:(youtube|bilibili):([\w-]+)(?:\?([\w=&]+))?]]$/`. When the optional query group is present, it is parsed as URL-encoded `key=value&key=value` pairs and only `start` and `page` keys (matching the placeholder grammar) are used; everything else is ignored.
3. On match, returns `<VideoEmbed>` with the parsed values; on no match, returns the default `<p>` rendering.

This stays entirely inside the existing safe rendering pipeline. No `dangerouslySetInnerHTML` is introduced.

A non-matching `[[video:...]]` (malformed ID, unknown platform, extra text in the paragraph) renders as literal paragraph text — safe and obvious failure mode.

## Edge cases

- **YouTube channel feeds** (`feed_type="youtube"`, deep content fetch already skipped by the worker): top-card detection runs on `item.Link` before the deep-fetch skip, so these articles get a video card despite their short bodies. The skip remains correct — the YouTube watch page is JS-heavy and contains no useful text.
- **Bilibili via RSSHub**: video URL is in `item.Link`, top-card detection just works. RSSHub item descriptions sometimes duplicate the `bilibili.com/video/BV...` link in the body; the A3 dedup step removes it.
- **Articles that only mention a YouTube link in passing**: they get an inline embed by design. No "max embeds per article" guard in v1 (YAGNI). If real-world feeds turn out noisy, add it later.
- **Live streams, unavailable videos, age-gated content**: the iframe loads YouTube's / Bilibili's own error UI. No server-side detection.
- **Playlists, timecodes, page numbers**: explicitly: timecodes and Bilibili page numbers preserved; playlists ignored (single video rendered).
- **Unknown placeholder formats**: e.g. `[[video:vimeo:abc]]` slipping through. Frontend regex doesn't match → renders as literal text. No crash, no silent drop, easy to spot during dev.
- **Existing articles**: backfill via the existing "re-fetch short content" loop — no one-shot migration written.
- **Both video and audio detected on the same article**: video wins (extraction order). A YouTube channel feed that also has an audio enclosure would be unusual; if it happens, we lose the audio. Acceptable.

## Testing

### Backend

- **`ExtractVideo` table-driven unit test**: all four YouTube URL shapes; YouTube with `?t=`, `#t=`, `?list=` (list ignored, video extracted); BV-form Bilibili with and without `?p=` and `?t=`; negative cases (Vimeo URL, malformed YouTube ID, AV-form Bilibili, `b23.tv` short link, plain URL with no video).
- **`FindVideosInMarkdown` unit test**: bare URLs, markdown `[text](url)` links, multiple matches in one body, URL inside a code block (must not match), URL inside a sentence, two matches for the same ID (both rewritten).
- **A3 dedup unit test**: article with `media_type=video/youtube` and a body containing the same video as a placeholder → placeholder removed; same video as a different ID → placeholder kept.
- **Integration test for the fetcher**: feed a known-shape `*gofeed.Item` (YouTube channel) through the fetcher and assert `MediaURL`, `MediaType` populated correctly.

### Frontend

- **`MarkdownArticle` render test**: paragraph containing exactly `[[video:youtube:dQw4w9WgXcQ]]` renders `<VideoEmbed>`; paragraph containing `Check out [[video:vimeo:abc]] today` renders plain text; paragraph containing `[[video:youtube:dQw4w9WgXcQ?start=42]]` renders `<VideoEmbed>` with `start=42`; paragraph containing `[[video:bilibili:BV1xx411c7mD?page=2&start=15]]` renders `<VideoEmbed>` with `page=2, start=15`.
- **`VideoEmbed` snapshot test**: pin the iframe `src` for both platforms so accidental drifts surface in review.

### Manual smoke test before merge

Run the Docker stack and verify, in a real browser, all three real-world shapes:

1. A YouTube channel RSS feed → article shows top-card video, no duplicate inline link.
2. A Bilibili RSSHub feed → article shows top-card video, no duplicate inline link.
3. A tech blog post that embeds a YouTube iframe → inline video appears in the body where the iframe was.

## Open questions

None at brainstorm-close. AV-form Bilibili IDs and `b23.tv` short links are deliberately deferred; revisit only if a real feed forces it.
