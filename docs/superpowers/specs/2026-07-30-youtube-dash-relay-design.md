# YouTube DASH Range Relay Design

## Goal

Make YouTube videos embedded in authenticated article pages playable on any
client without requiring a local proxy. Keep Bilibili playback direct from the
client, while all YouTube metadata and media requests leave the Beijing server
through its existing Mihomo rule and Starlink subscription.

The relay must:

- prefer 1080p playback and accept 720p as the normal fallback;
- support arbitrary seeking;
- avoid downloading or caching complete videos;
- keep combined video and audio bitrate near or below 4 Mbps so the expected
  monthly usage fits the server's 30 GB traffic allowance;
- remain unavailable to unauthenticated users and avoid becoming an open proxy.

## Constraints

- Public HTTPS remains `rss.morefreeze.top -> OCI -> Beijing`.
- The unfiled Beijing server is not exposed directly as the public origin.
- Local and company traffic must never SSH directly to Beijing. Operational
  access continues through `oci-rss-pal` as the jump host.
- Bilibili keeps the existing `player.bilibili.com` iframe and client-direct
  network path.
- YouTube upstream URLs must never be accepted from the browser or returned to
  it.
- The first release does not persist video files or monthly usage counters.
- Public share pages are out of scope. Only authenticated article pages can
  create playback sessions.

## Chosen Architecture

```text
Authenticated browser
  -> POST /api/articles/:id/youtube-playback
  -> API verifies article ownership and media_type=video/youtube
  -> yt-dlp resolves MP4 video/audio formats through Beijing Mihomo
  -> API creates an in-memory playback session and synthetic DASH MPD

dash.js in browser
  -> GET /api/media/youtube/:ticket/manifest.mpd
  -> Range GET /api/media/youtube/:ticket/video
  -> Range GET /api/media/youtube/:ticket/audio
  -> OCI Nginx -> Beijing Nginx -> API
  -> YouTube/GoogleVideo through Mihomo -> Starlink
```

The browser uses dash.js, the DASH-IF reference player for Media Source
Extensions clients. The generated MPD contains one H.264 video representation
and one AAC audio representation. Each representation points to a same-origin
relay URL rather than the signed GoogleVideo URL.

The relay forwards byte-range requests. Seeking therefore fetches only the
requested media ranges and does not require a complete local file. This uses
the indexed `SegmentBase` form of MPEG-DASH: the API makes a small leading-range
probe of each selected MP4 stream, identifies its initialization and `sidx`
index byte ranges, and puts those ranges into the generated MPD.

Reference material:

- dash.js setup:
  <https://dashif.org/dash.js/pages/quickstart/setup.html>
- DASH indexed addressing and required `SegmentBase@indexRange`:
  <https://dashif.org/Guidelines-TimingModel/Timing-Model.pdf>
- yt-dlp format selection and JSON output:
  <https://github.com/yt-dlp/yt-dlp/blob/master/README.md>

## Backend Components

### Playback session creation

Add a protected endpoint:

```text
POST /api/articles/:id/youtube-playback
```

The handler:

1. reads the article through the existing RLS-bound repository;
2. requires `media_type == video/youtube`;
3. derives the canonical YouTube ID from the stored article media URL;
4. asks the resolver for formats;
5. selects a compatible video/audio pair;
6. probes the MP4 initialization and index ranges;
7. creates an in-memory session with a 256-bit random ticket;
8. returns the same-origin MPD URL, selected quality, and expiry metadata.

The request body contains no URL or video ID. An authenticated user can only
start a relay for an article visible through that user's existing RLS scope.

### yt-dlp resolver

The resolver runs `yt-dlp` without a shell and with a hard timeout. It requests
metadata only and parses the JSON format list. The API runtime image also
provides a supported JavaScript runtime and verifies that the installed
yt-dlp package has its EJS challenge component, because current yt-dlp
documents both as required for full YouTube support.

The resolver keeps upstream URLs and required request headers only in memory.
It never logs or returns signed URLs.

### Format selection

Selection is deterministic:

1. MP4 H.264 video, height 1080 or lower, frame rate 30 or lower;
2. M4A/MP4 AAC audio;
3. combined estimated bitrate no greater than 4 Mbps;
4. highest height first, then the best bitrate within the cap;
5. if no compatible 1080p pair exists, repeat at 720p;
6. if adaptive playback is unavailable, use the best compatible progressive
   MP4 format at 720p or lower as the old-browser fallback.

The response reports the actual selected height. The UI must not label a 720p
or progressive fallback as 1080p.

### MP4 range probe and MPD generation

For each adaptive stream, the API fetches only the first bounded portion of the
file and parses top-level ISO BMFF boxes:

- `ftyp` and `moov` define the initialization range;
- `sidx` defines the DASH index range.

The probe starts with 1 MiB and may expand to at most 4 MiB. Missing, malformed,
oversized, or unsupported box layouts reject that format pair rather than
triggering a complete download.

The generated static VOD MPD includes:

- total duration;
- video dimensions, frame rate, codec, bandwidth, and `SegmentBase`;
- audio codec, sample rate, bandwidth, and `SegmentBase`;
- only same-origin ticket URLs.

XML values are escaped and the MPD is generated from typed server-side data.

### Session manager

Sessions are held in the single API process. Each session contains:

- article, user, and YouTube IDs;
- selected format IDs and metadata;
- upstream video/audio URLs and required headers;
- MP4 initialization/index ranges;
- creation time, last-access time, and refresh state.

Limits:

- two concurrent active sessions globally;
- ten minutes idle expiry;
- six hours absolute lifetime;
- one automatic upstream re-resolution per session after a 403 or 410;
- periodic cleanup plus cleanup on process shutdown.

Tickets are random bearer capabilities. Media routes sit outside JWT
middleware because native media requests cannot reliably attach the app's
localStorage JWT, but a ticket can only be minted by the protected endpoint.
Tickets are never logged.

### Range relay

Add ticket-protected public routes:

```text
GET /api/media/youtube/:ticket/manifest.mpd
GET|HEAD /api/media/youtube/:ticket/video
GET|HEAD /api/media/youtube/:ticket/audio
GET|HEAD /api/media/youtube/:ticket/progressive
```

The relay:

- accepts only one RFC 7233 byte range per request;
- forwards `Range` and `If-Range`, but no client-supplied upstream URL or host;
- applies resolver-provided User-Agent/Referer headers;
- returns upstream status, `Content-Type`, `Content-Length`, `Content-Range`,
  `Accept-Ranges`, `ETag`, and `Last-Modified`;
- does not gzip or buffer media bodies;
- cancels the upstream request when the client disconnects;
- records byte counts and selected quality in structured logs without logging
  tickets or signed URLs.

If the upstream returns 403 or 410, the session re-resolves the same format IDs
once and retries. If those IDs no longer exist, the handler tells the frontend
to recreate the playback session.

## Frontend

`VideoEmbed` continues to render the existing eager Bilibili iframe.

For YouTube it renders a new `YouTubeRelayPlayer`:

1. request a playback session for the current article;
2. feature-detect Media Source Extensions;
3. initialize dash.js with the returned MPD;
4. show the actual selected quality;
5. destroy the player and abort session-start work when unmounted;
6. show a retry action and the original YouTube link on terminal failure.

If MSE is unavailable, the component uses the returned progressive same-origin
MP4 relay when present. It does not fall back to the direct YouTube iframe,
because that would silently reintroduce the client-proxy requirement.

Autoplay remains disabled. The standard browser controls provide play/pause,
volume, fullscreen, and seeking.

## Reverse Proxy Changes

Both the Beijing frontend Nginx and OCI public Nginx add a specific media
location before the generic `/api/` proxy:

- disable response buffering and request buffering;
- disable proxy caching;
- preserve `Range`, `If-Range`, and upstream range response headers;
- use long read/send timeouts suitable for paused media;
- keep all other API behavior unchanged.

The media path still traverses both servers:

```text
YouTube -> Starlink -> Beijing -> OCI -> client
```

The 4 Mbps format cap is therefore a traffic-control default, not a guarantee:
variable bitrate, abandoned prefetch, replays, multiple clients, and seeking
can still increase monthly usage.

## Errors and User Experience

- Invalid article/media type: 404, no resolver call.
- Resolver unavailable or timed out: 502 with a retryable player error.
- No compatible 1080p/720p formats: try progressive fallback; otherwise 422.
- Session capacity reached: 429 with a short retry delay.
- Invalid or expired ticket: 404.
- Invalid/multiple Range header: 416.
- Upstream timeout: 504.
- Signed URL expired: one transparent refresh, then a visible retry prompt.
- Client disconnect: cancel copy immediately.

The player never spins indefinitely. Every startup path ends in playing,
fallback playback, or a visible error with retry.

## Testing

### Backend

- TDD unit tests for deterministic format selection and the 4 Mbps cap.
- MP4 box-parser fixtures for valid, truncated, oversized, and missing-`sidx`
  inputs.
- MPD golden tests verifying only same-origin ticket URLs and correct
  `SegmentBase` ranges.
- Handler tests for auth/RLS ownership, invalid media, ticket expiry, capacity,
  single-range forwarding, response-header filtering, client cancellation,
  and the one-refresh rule.
- Command-runner tests using a fake yt-dlp executable/output; unit tests never
  call YouTube.

### Frontend

- Bilibili keeps the eager direct iframe.
- YouTube creates a relay session and initializes dash.js.
- Selected quality is displayed accurately.
- Unmount destroys the player.
- MSE absence uses progressive fallback.
- Resolver/player failures show retry instead of a direct YouTube iframe.

### Production acceptance

From a client with local Clash disabled:

1. open an authenticated YouTube article in Chrome;
2. verify sound and 1080p when a <=4 Mbps representation exists;
3. seek near the middle and near the end without waiting for prior bytes;
4. repeat in Safari and the installed Pake app;
5. open a Bilibili article and confirm the existing direct player still works;
6. confirm Beijing Mihomo logs route YouTube/GoogleVideo through Starlink and
   Bilibili through `DIRECT`;
7. confirm media requests return 206 with correct `Content-Range`;
8. confirm API/frontend/worker health and OCI public health remain green.

## Rollback

Frontend rollback restores the direct YouTube iframe component. Backend relay
routes can remain unreachable without callers, or the API/frontend images can
be rolled back together.

OCI and Beijing Nginx files are backed up before edits. If public health,
non-media API traffic, playback startup, or seeking regress, restore both
backups, validate Nginx, reload, and deploy the prior application commit.
Bilibili behavior does not depend on the relay and must remain intact
throughout rollback.
