# YouTube Logged-in Browser Bridge Design

## Goal

Play YouTube videos inside the normal Chrome version of RSS Pal without an
iframe and without sending video traffic through the Beijing or OCI servers.
The RSS Pal Chrome extension uses the user's existing signed-in YouTube session
to resolve short-lived media URLs, then the RSS Pal page plays those URLs
directly from the user's Chrome network path and local Clash rules.

Playback must:

- prefer 1080p and accept 720p as the normal fallback;
- support seeking, fullscreen, volume, and normal browser controls;
- require an explicit user click before resolving or loading media;
- close the temporary YouTube tab after resolution;
- keep YouTube cookies and account state inside Chrome;
- leave Bilibili playback client-direct and unchanged;
- show a useful fallback when the extension, YouTube login, or local proxy is
  unavailable.

This feature is browser-only. `rsspal.app`, public share pages, and browsers
without the RSS Pal extension are out of scope.

## Why This Architecture

OpenCLI demonstrates the useful primitive: execute a narrowly defined action in
a real, logged-in browser tab instead of copying account cookies to a server.
Its generic CLI daemon and WebSocket bridge are unnecessary for RSS Pal because
the web page and extension already share Chrome's native messaging path.

Three approaches were considered:

1. A YouTube iframe is the simplest, but the user explicitly does not want an
   iframe and it does not give RSS Pal control over the playback experience.
2. The existing Beijing DASH relay gives the web page a same-origin stream, but
   anonymous server-side YouTube resolution is bot-blocked, consumes server
   egress, and makes the Starlink exit a production dependency.
3. A dedicated RSS Pal extension bridge can use the user's already signed-in
   Chrome session and local Clash path. It adds no local daemon and no cloud
   media relay. This is the chosen approach.

The bridge is not a generic remote browser API. It supports one hard-coded
operation: resolve playable media for one validated YouTube video ID.

## Traffic and Trust Boundaries

```text
RSS Pal page on rss.morefreeze.top
  -> window.postMessage (fixed request schema)
  -> RSS Pal content script on the same page
  -> chrome.runtime.sendMessage
  -> RSS Pal MV3 service worker
  -> inactive youtube.com/watch tab using the user's signed-in session
  -> MAIN-world extraction of actual player requests
  -> short-lived media metadata returned to the RSS Pal page

RSS Pal dash.js player
  -> googlevideo.com directly from local Chrome
  -> local Clash routing, if required
```

The Beijing API, OCI proxy, RSS Pal database, and server logs are not involved
in media resolution or transfer. Existing relay code can remain available for
rollback during the first release, but the browser UI must not call
`POST /api/articles/:id/youtube-playback`.

The signed GoogleVideo URL is a bearer capability. It may cross the extension
boundary only into the trusted RSS Pal page that requested it. It stays in
memory and must never be sent to the RSS Pal API, stored in `localStorage`,
written to extension storage, or logged.

## Extension Components

### Manifest

The extension adds:

- a content script matched only on `https://rss.morefreeze.top/*`;
- the `declarativeNetRequestWithHostAccess` permission;
- an enabled static declarative-net-request ruleset for RSS Pal-initiated
  GoogleVideo media requests.

The existing `tabs`, `scripting`, and broad host permissions are sufficient for
opening YouTube and injecting the extractor. The first release does not request
`cookies`, `debugger`, `webRequest`, `tabCapture`, or native-messaging
permissions.

The declarative rule applies only when:

- the initiator domain is `rss.morefreeze.top`;
- the request domain is `googlevideo.com`;
- the URL is a `/videoplayback` media URL;
- the resource is an XHR/fetch request made by the in-page DASH player.

It supplies the narrow CORS response headers required by dash.js, including an
RSS Pal-specific `Access-Control-Allow-Origin` and exposure of range headers.
This rule is needed because 1080p YouTube formats are normally separate audio
and video resources that dash.js must fetch through Media Source Extensions.
It does not expose GoogleVideo responses to arbitrary websites.

### Page content script

The bridge content script has no YouTube logic. It:

1. announces `RSS_PAL_YOUTUBE_BRIDGE_READY` after loading;
2. listens only to messages from its own `window`;
3. validates the exact request type, request ID, and eleven-character YouTube
   video ID;
4. forwards the request to the extension service worker;
5. posts a response carrying the same request ID back to the page;
6. forwards cancellation when the player unmounts or the user retries.

It rejects messages on any origin other than
`https://rss.morefreeze.top`. Responses use that exact target origin rather
than `"*"`.

The public contract is intentionally small:

```ts
type YouTubeResolveRequest = {
  type: 'RSS_PAL_YOUTUBE_RESOLVE_REQUEST'
  requestId: string
  videoId: string
}

type YouTubeResolveResponse =
  | {
      type: 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE'
      requestId: string
      ok: true
      playback: BrowserPlayback
    }
  | {
      type: 'RSS_PAL_YOUTUBE_RESOLVE_RESPONSE'
      requestId: string
      ok: false
      code: YouTubeBridgeErrorCode
    }
```

No request accepts a URL, script, selector, headers, cookies, or arbitrary
JavaScript.

### Service worker

The service worker verifies both the payload and the Chrome sender:

- the sender must be a tab at `https://rss.morefreeze.top/*`;
- the video ID must match `^[A-Za-z0-9_-]{11}$`;
- the request ID must be bounded and contain only safe characters;
- only a small number of resolutions may be active at once.

For an accepted request it:

1. creates an inactive tab at the canonical
   `https://www.youtube.com/watch?v=<id>` URL;
2. waits for the tab to complete, with a hard timeout;
3. injects the fixed extractor with `chrome.scripting.executeScript` in
   `world: "MAIN"`;
4. receives structured playback metadata;
5. closes the YouTube tab in a `finally` block;
6. returns the result to the requesting RSS Pal tab.

The service worker deduplicates concurrent requests for the same video. A
cancelled request closes its temporary tab if no other requester is waiting.
Tabs also close on timeout, extraction failure, or loss of the original RSS Pal
tab. Temporary tab IDs are recorded in `chrome.storage.session` so the next
service-worker startup can close an orphan left by an unexpected worker or
browser shutdown.

### MAIN-world YouTube extractor

The extractor is a bundled fixed function, not user-supplied code. MAIN-world
execution is required because Chrome content scripts run in an isolated world
and cannot reliably access YouTube's player objects.

It waits for the watch page's player response and checks playability. It reads
the structured format metadata from `movie_player.getPlayerResponse()` or
`ytInitialPlayerResponse`, including:

- format ID (`itag`);
- MIME type and codecs;
- width, height, frame rate, and bitrate;
- duration;
- MP4 initialization and index ranges;
- progressive versus adaptive stream type.

Player responses may contain a usable `url`, a ciphered URL, or no finalized
URL. The extractor therefore treats the browser's real network requests as the
source of truth:

1. collect already-finalized format URLs when present;
2. mute the real YouTube player;
3. request `hd1080` when available, otherwise `hd720`;
4. start playback long enough to create real audio and video requests;
5. poll `performance.getEntriesByType("resource")` for
   `googlevideo.com/videoplayback` entries;
6. parse each actual request's `itag` from its query or path and join it to the
   player-response metadata;
7. stop the player as soon as a usable pair or progressive fallback is found.

This preserves any signature, `n` transformation, PO token, and account/session
decision made by YouTube's own player code without reimplementing them.

The extractor prefers:

1. a playable adaptive video track at 1080p or lower plus a compatible audio
   track;
2. a 720p adaptive pair;
3. a progressive MP4 at 720p or lower;
4. otherwise a typed failure.

Within the chosen height it prefers a Chrome-supported codec and 30 fps, while
allowing the player's working codec/fps choice when no lower-cost equivalent
was observed. It reports the actual height and never labels a lower format as
1080p.

The extractor returns only the selected tracks and minimal playback metadata.
It must not return cookies, authorization headers, page HTML, player scripts,
Google account identifiers, recommendations, history, or unrelated resource
entries.

## Browser Playback Contract

The successful payload is kept in memory:

```ts
type ByteRange = { start: number; end: number }

type AdaptiveTrack = {
  url: string
  mimeType: string
  codecs: string
  bitrate: number
  initRange: ByteRange
  indexRange: ByteRange
  durationMs: number
  width?: number
  height?: number
  frameRate?: number
  audioSampleRate?: number
}

type ProgressiveTrack = {
  url: string
  mimeType: string
  height: number
}

type BrowserPlayback = {
  mode: 'dash' | 'progressive'
  quality: number
  expiresAt: string
  video?: AdaptiveTrack
  audio?: AdaptiveTrack
  progressive?: ProgressiveTrack
}
```

For adaptive playback the frontend generates an in-memory static VOD MPD using
the returned metadata and absolute media URLs. XML values are escaped. The MPD
uses each track's `SegmentBase`, initialization range, and index range, then
the existing dash.js dependency attaches it to the normal `<video>` element.

For progressive playback the direct media URL is assigned to the same
`<video>` element. Native progressive playback is also the fallback when DASH
initialization or cross-origin range loading fails and a progressive track was
returned.

Signed URL expiry is parsed from the observed URL. The frontend does not begin
playback when the returned expiry is too close. On a DASH or native media error
it performs at most one transparent re-resolution, then shows a visible retry
button.

## Frontend Experience

Add a `YouTubeBrowserPlayer` used by both:

- a stored primary `video/youtube` item in `ArticlePlayerCard`;
- an inline YouTube placeholder in `VideoEmbed`.

The component receives the parsed video ID, optional start time, and original
YouTube URL. It does not require an article ID because authorization is the
local extension boundary, not the RSS Pal media relay.

Initial rendering shows a player-shaped card with a play button and
`使用已登录的 Chrome 播放`. Resolution begins only after the click. The UI then
has four explicit states:

- extension not detected: ask the user to install or reload the RSS Pal
  extension and retain the YouTube link;
- resolving: show that a temporary YouTube tab is being prepared;
- ready: show the normal video controls and actual selected quality;
- failed: show a typed explanation, retry, and `在 YouTube 打开`.

Autoplay remains disabled on the RSS Pal page. The first click authorizes
resolution; after media is ready the user presses the native play control.

The existing Bilibili iframe is unchanged. Without a Chrome extension,
`rsspal.app` falls into the explicit extension-unavailable state and offers the
original YouTube link; it must not call the bridge or the server relay.

## Error Model

The service worker maps internal details to stable page-facing codes:

- `EXTENSION_UNAVAILABLE`: frontend handshake timed out;
- `LOGIN_REQUIRED`: YouTube reports login/account verification is required;
- `VIDEO_UNAVAILABLE`: deleted, private, age-restricted, regional, or
  owner-blocked media that the current session cannot play;
- `NO_SUPPORTED_FORMAT`: no observed 1080p/720p adaptive pair or acceptable
  progressive fallback;
- `RESOLVE_TIMEOUT`: YouTube did not become ready or emit media requests;
- `LOCAL_NETWORK_ERROR`: the YouTube page or GoogleVideo host was unreachable
  through the user's current network/Clash configuration;
- `PLAYBACK_EXPIRED`: signed URLs expired before playback;
- `INTERNAL_ERROR`: all other bounded extension failures.

Detailed exception text and upstream URLs are not posted to the page or logged.
The UI may tell the user to open YouTube in a normal tab to complete login or
verification, then retry.

## Security and Privacy

- Only `https://rss.morefreeze.top` can invoke the bridge.
- The background independently validates the sender; content-script validation
  is not the only boundary.
- The bridge accepts a video ID, never an arbitrary target URL.
- The extractor is fixed at build time and has no generic eval interface.
- YouTube tabs are inactive, muted, short-lived, and always closed.
- No cookies API is requested and no browser cookies leave YouTube.
- Signed media URLs remain in the trusted page's memory and are redacted from
  logs and error telemetry.
- The CORS header rule is limited to RSS Pal-initiated GoogleVideo playback
  requests.
- No media bytes traverse the extension message channel, RSS Pal backend,
  Beijing server, OCI server, or database.
- Resolution concurrency and timeouts prevent tab floods.

An XSS running on `rss.morefreeze.top` could invoke the same bridge as the RSS
Pal app and read the returned signed URL. That is why the operation is limited
to video playback, returns no account data, and keeps URL lifetimes short. The
existing site's XSS controls remain part of the trust boundary.

## Testing and Acceptance

### Automated tests

- Content-script request/response schema, origin checks, timeout, cancellation,
  and request correlation.
- Service-worker sender validation, video-ID validation, deduplication,
  concurrency limits, timeouts, and guaranteed tab cleanup.
- Player-response parsing and captured-resource `itag` matching with fixtures
  for direct URLs, ciphered entries, adaptive pairs, and progressive fallback.
- Deterministic 1080p preference, 720p fallback, codec compatibility, and
  truthful quality labels.
- MPD escaping and required `SegmentBase`, initialization, and index ranges.
- Frontend extension-missing, resolving, ready, retry, progressive fallback,
  and unmount cancellation states.
- Regression coverage proving Bilibili still uses its existing direct iframe
  and the YouTube browser player never calls the backend relay endpoint.
- Extension manifest/ruleset smoke tests proving the CORS rule is restricted to
  RSS Pal-initiated GoogleVideo playback.

### Live Chrome acceptance

Using the installed unpacked extension and the user's existing Chrome profile:

1. load `https://rss.morefreeze.top/articles/2391` and a real YouTube article;
2. confirm no YouTube request is made before pressing play;
3. press play and observe one inactive YouTube tab open and close;
4. confirm the RSS Pal player reports 1080p when available, otherwise 720p;
5. play audio/video in sync and seek to an arbitrary later point;
6. confirm the media requests go from local Chrome to `googlevideo.com`;
7. confirm no `/youtube-playback` or `/api/media/youtube/` request is made;
8. confirm Beijing and OCI media byte counts remain unchanged;
9. temporarily disable the extension and verify the explicit fallback state;
10. confirm a Bilibili article still loads directly as before.

If direct adaptive GoogleVideo range requests still fail after the restricted
CORS rule, the first release must fall back to an observed progressive track
and report its real quality. It must not silently add `debugger`, proxy media
through extension messages, or restore the Beijing relay without a new design
decision.

## Deployment and Rollback

Deployment has two independently versioned pieces:

1. build and deploy the frontend to the existing Beijing runtime;
2. increment the RSS Pal extension version and have the user reload the
   unpacked extension in Chrome.

The frontend should detect the bridge version so an old extension produces an
upgrade prompt rather than a generic playback failure. The feature is not
declared live until the deployed frontend and reloaded extension pass the live
Chrome acceptance checks.

Rollback restores `YouTubeRelayPlayer` in the frontend and reloads the previous
extension build. No database, backend, DNS, Nginx, OCI, or Beijing migration is
required. The experimental server relay and PO-token code are not removed by
this feature; cleanup can be a separate change after the browser path is proven.
