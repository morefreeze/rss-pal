# YouTube DASH Range Relay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Play primary YouTube article videos on any authenticated client through the Beijing Mihomo/Starlink exit, preferring 1080p under a 4 Mbps cap, supporting arbitrary seeking without complete-file downloads, while leaving Bilibili client-direct.

**Architecture:** The protected API creates an in-memory playback session from a server-owned article and yt-dlp metadata. It generates a synthetic indexed DASH MPD whose video/audio `SegmentBase` URLs point to ticketed same-origin Range relays; dash.js requests only the byte ranges needed for playback and seeking. Signed upstream URLs never leave the API, and the public ticket routes cannot accept arbitrary URLs.

**Tech Stack:** Go 1.25, Gin, Alpine yt-dlp with its existing Deno/EJS challenge runtime, ISO BMFF top-level box parsing, MPEG-DASH `SegmentBase`, React 18, TypeScript, dash.js 5.2, Vitest, Docker Compose, Nginx.

---

## File Map

### Backend

- Create `backend/internal/youtuberelay/types.go`: resolver/session DTOs, errors, stream kinds.
- Create `backend/internal/youtuberelay/selector.go`: deterministic H.264/AAC selection under 4 Mbps.
- Create `backend/internal/youtuberelay/selector_test.go`: selection and fallback contract.
- Create `backend/internal/youtuberelay/mp4.go`: bounded ISO BMFF initialization/`sidx` range parser.
- Create `backend/internal/youtuberelay/mp4_test.go`: valid/truncated/oversized/missing-box fixtures.
- Create `backend/internal/youtuberelay/mpd.go`: typed static VOD MPD generator.
- Create `backend/internal/youtuberelay/mpd_test.go`: golden assertions for ticket-only URLs/ranges.
- Create `backend/internal/youtuberelay/resolver.go`: timed yt-dlp JSON resolver.
- Create `backend/internal/youtuberelay/resolver_test.go`: fake-command success, timeout, invalid JSON, unsafe host.
- Create `backend/internal/youtuberelay/service.go`: session lifecycle, MP4 probes, concurrency, refresh, relay opening.
- Create `backend/internal/youtuberelay/service_test.go`: fake resolver/upstream end-to-end service behavior.
- Create `backend/internal/api/youtube_playback.go`: protected session creation and ticketed public handlers.
- Create `backend/internal/api/youtube_playback_test.go`: auth/media/error/status/header tests.
- Modify `backend/cmd/server/main.go`: construct service, register protected/public routes, exclude relay from gzip.

### Frontend

- Modify `frontend/package.json` and `frontend/package-lock.json`: add `dashjs@5.2.0`.
- Modify `frontend/src/api/client.ts`: playback session response and start call.
- Create `frontend/src/components/YouTubeRelayPlayer.tsx`: MSE/dash.js player and progressive fallback.
- Create `frontend/test/YouTubeRelayPlayer.test.tsx`: startup, quality, cleanup, fallback, retry.
- Modify `frontend/src/components/ArticlePlayerCard.tsx`: primary YouTube uses relay; Bilibili stays iframe.
- Modify `frontend/src/components/VideoEmbed.tsx`: body YouTube placeholders never create a direct iframe.
- Modify `frontend/test/VideoEmbed.test.tsx`: direct-Bilibili and no-direct-YouTube regression coverage.

### Proxy/deployment

- Modify `frontend/nginx.conf`: unbuffered media proxy location with long timeouts and Range passthrough.
- Modify live `/opt/rss-pal/nginx.prod.conf` on Beijing after backing it up.
- Modify live `/etc/nginx/sites-enabled/rss-pal` on OCI after backing it up.

## Task 1: Format Types and Deterministic Selection

**Files:**
- Create: `backend/internal/youtuberelay/types.go`
- Create: `backend/internal/youtuberelay/selector.go`
- Test: `backend/internal/youtuberelay/selector_test.go`

- [ ] **Step 1: Write the failing selector tests**

Define table tests with concrete format fixtures:

```go
func TestSelectFormatsPrefers1080UnderCap(t *testing.T) {
    info := VideoInfo{ID: "dQw4w9WgXcQ", Duration: 212, Formats: []Format{
        {ID: "137", URL: googleURL("v137"), Ext: "mp4", VCodec: "avc1.640028", ACodec: "none", Height: 1080, FPS: 30, TBR: 3650},
        {ID: "399", URL: googleURL("v399"), Ext: "mp4", VCodec: "av01.0.08M.08", ACodec: "none", Height: 1080, FPS: 30, TBR: 2500},
        {ID: "136", URL: googleURL("v136"), Ext: "mp4", VCodec: "avc1.4d401f", ACodec: "none", Height: 720, FPS: 30, TBR: 2200},
        {ID: "140", URL: googleURL("a140"), Ext: "m4a", VCodec: "none", ACodec: "mp4a.40.2", ABR: 128, TBR: 128},
        {ID: "22", URL: googleURL("p22"), Ext: "mp4", VCodec: "avc1.64001F", ACodec: "mp4a.40.2", Height: 720, FPS: 30, TBR: 2400},
    }}

    got, err := SelectFormats(info, 4000)
    if err != nil {
        t.Fatal(err)
    }
    if got.Video.ID != "137" || got.Audio.ID != "140" ||
        got.Quality != 1080 || got.Progressive.ID != "22" {
        t.Fatalf("unexpected selection: %+v", got)
    }
}

func TestSelectFormatsFallsBackTo720When1080ExceedsCap(t *testing.T) {
    // 1080 video 4,500 Kbps + audio exceeds the cap; 720 pair fits.
}

func TestSelectFormatsRejectsVP9AV1AndNonGoogleVideoHosts(t *testing.T) {
    // Only VP9/AV1 or https://example.com media must return ErrNoCompatibleFormat.
}
```

- [ ] **Step 2: Run the selector tests and verify RED**

Run:

```bash
cd backend
go test ./internal/youtuberelay -run TestSelectFormats -count=1 -v
```

Expected: compile failure because `VideoInfo`, `Format`, and `SelectFormats` do not exist.

- [ ] **Step 3: Implement types and selection**

Add these contracts:

```go
const MaxCombinedKbps = 4000.0

type Format struct {
    ID             string            `json:"format_id"`
    URL            string            `json:"url"`
    Ext            string            `json:"ext"`
    Protocol       string            `json:"protocol"`
    VCodec         string            `json:"vcodec"`
    ACodec         string            `json:"acodec"`
    Width          int               `json:"width"`
    Height         int               `json:"height"`
    FPS            float64           `json:"fps"`
    TBR            float64           `json:"tbr"`
    VBR            float64           `json:"vbr"`
    ABR            float64           `json:"abr"`
    ASR            int               `json:"asr"`
    Filesize       int64             `json:"filesize"`
    FilesizeApprox int64             `json:"filesize_approx"`
    HTTPHeaders    map[string]string `json:"http_headers"`
}

type VideoInfo struct {
    ID       string   `json:"id"`
    Duration float64  `json:"duration"`
    Formats  []Format `json:"formats"`
}

type Selection struct {
    Video       *Format
    Audio       *Format
    Progressive *Format
    Quality     int
}
```

`SelectFormats` must:

1. accept only HTTPS `googlevideo.com` or subdomains;
2. pair MP4 `avc1` video-only formats with M4A/MP4 `mp4a` audio-only formats;
3. reject video above 1080p or above 30 fps;
4. calculate bitrate from `TBR`, then `VBR`/`ABR`, then file size and duration;
5. keep pair total at or below the supplied Kbps cap;
6. sort by height descending then total Kbps descending;
7. select the best progressive H.264/AAC MP4 at 720p or lower separately.

- [ ] **Step 4: Run selector tests and full package tests**

Run:

```bash
cd backend
go test ./internal/youtuberelay -count=1 -v
```

Expected: all selector tests pass.

- [ ] **Step 5: Commit Task 1**

```bash
git add internal/youtuberelay
git commit -m "feat: select bounded YouTube relay formats"
```

## Task 2: MP4 Index Probe Parser and Static MPD

**Files:**
- Create: `backend/internal/youtuberelay/mp4.go`
- Create: `backend/internal/youtuberelay/mp4_test.go`
- Create: `backend/internal/youtuberelay/mpd.go`
- Create: `backend/internal/youtuberelay/mpd_test.go`

- [ ] **Step 1: Write failing MP4 parser tests**

Build boxes in the test instead of checking in media:

```go
func box(kind string, payload []byte) []byte {
    out := make([]byte, 8+len(payload))
    binary.BigEndian.PutUint32(out[:4], uint32(len(out)))
    copy(out[4:8], kind)
    copy(out[8:], payload)
    return out
}

func TestParseMP4IndexRanges(t *testing.T) {
    prefix := bytes.Join([][]byte{
        box("ftyp", make([]byte, 16)),
        box("moov", make([]byte, 72)),
        box("sidx", make([]byte, 40)),
        box("moof", make([]byte, 24)),
    }, nil)
    got, err := ParseMP4IndexRanges(prefix)
    if err != nil {
        t.Fatal(err)
    }
    if got.Initialization != (ByteRange{Start: 0, End: 103}) ||
        got.Index != (ByteRange{Start: 104, End: 151}) {
        t.Fatalf("unexpected ranges: %+v", got)
    }
}
```

Also test extended-size boxes, declared size beyond input, size smaller than header,
missing `ftyp`, missing `moov`, missing `sidx`, and a box count/size limit.

- [ ] **Step 2: Run parser tests and verify RED**

```bash
cd backend
go test ./internal/youtuberelay -run 'TestParseMP4|TestGenerateMPD' -count=1 -v
```

Expected: compile failure for missing parser/MPD symbols.

- [ ] **Step 3: Implement bounded top-level box parsing**

Use:

```go
type ByteRange struct {
    Start int64
    End   int64
}

type MP4IndexRanges struct {
    Initialization ByteRange
    Index          ByteRange
}
```

Parse 32-bit size, extended 64-bit size, and reject size zero. Walk no more than
64 boxes and never read beyond `len(prefix)`. The initialization range begins at
zero and ends at the end of `moov`; the index range is the complete `sidx` box.

- [ ] **Step 4: Write failing MPD tests**

Create a `ManifestInput` with video/audio metadata and assert:

```go
for _, want := range []string{
    `mediaPresentationDuration="PT212S"`,
    `mimeType="video/mp4"`,
    `codecs="avc1.640028"`,
    `indexRange="104-151"`,
    `<BaseURL>/api/media/youtube/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA/video</BaseURL>`,
} {
    if !strings.Contains(xml, want) {
        t.Fatalf("MPD missing %q: %s", want, xml)
    }
}
if strings.Contains(xml, "googlevideo.com") {
    t.Fatalf("MPD leaked upstream URL: %s", xml)
}
```

Use a ticket containing XML-sensitive characters in a negative validation test;
the generator must reject tickets outside `[A-Za-z0-9_-]{43}` rather than
interpolating them.

- [ ] **Step 5: Implement typed MPD generation**

Use `encoding/xml` structs for MPD, Period, AdaptationSet, Representation,
SegmentBase, Initialization, and BaseURL. Produce a static on-demand VOD MPD
with separate video/audio adaptation sets and `SegmentBase@indexRange`.

- [ ] **Step 6: Run package tests and commit**

```bash
cd backend
go test ./internal/youtuberelay -count=1 -v
git add internal/youtuberelay/mp4.go internal/youtuberelay/mp4_test.go \
  internal/youtuberelay/mpd.go internal/youtuberelay/mpd_test.go
git commit -m "feat: generate indexed DASH manifests"
```

## Task 3: Timed yt-dlp Metadata Resolver

**Files:**
- Create: `backend/internal/youtuberelay/resolver.go`
- Create: `backend/internal/youtuberelay/resolver_test.go`

- [ ] **Step 1: Write failing resolver tests**

Define an injected runner:

```go
type CommandRunner interface {
    Output(ctx context.Context, name string, args ...string) ([]byte, error)
}
```

Tests assert exact safe arguments:

```go
want := []string{
    "--no-warnings", "--no-playlist", "--skip-download",
    "--socket-timeout", "20", "--js-runtimes", "deno",
    "-J", "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
}
```

Cover valid JSON, mismatched returned ID, malformed JSON, command error, timeout,
invalid 11-character ID, and a format whose URL is not GoogleVideo.

- [ ] **Step 2: Run resolver tests and verify RED**

```bash
cd backend
go test ./internal/youtuberelay -run TestYTDLPResolver -count=1 -v
```

Expected: compile failure because the resolver is absent.

- [ ] **Step 3: Implement the resolver**

`YTDLPResolver.Resolve` validates the ID with `^[A-Za-z0-9_-]{11}$`, builds the
canonical watch URL internally, runs `exec.CommandContext`, caps stdout at
16 MiB, parses `VideoInfo`, verifies the returned ID, and calls `SelectFormats`.
Use a 45-second outer timeout even if the request context is longer.

Return typed sentinel errors:

```go
var (
    ErrInvalidVideoID    = errors.New("invalid youtube video id")
    ErrResolveFailed     = errors.New("youtube metadata resolution failed")
    ErrNoCompatibleMedia = errors.New("no compatible youtube media")
)
```

- [ ] **Step 4: Verify the existing challenge runtime contract**

Do not change either Dockerfile. The production Alpine `yt-dlp` package already
provides `/usr/bin/deno` and the importable `yt_dlp_ejs` package. Record these
deployment smoke checks for Task 9:

```bash
docker compose exec -T api sh -lc \
  'command -v deno && python3 -c "import importlib.util; assert importlib.util.find_spec(\"yt_dlp_ejs\")"'
```

The resolver explicitly enables Deno. Do not add ffmpeg or Node.js.

- [ ] **Step 5: Run resolver/package tests and commit**

```bash
cd backend
go test ./internal/youtuberelay -count=1 -v
git add internal/youtuberelay/resolver.go internal/youtuberelay/resolver_test.go
git commit -m "feat: resolve YouTube relay metadata"
```

## Task 4: Session Lifecycle, MP4 Probe, and Range Relay

**Files:**
- Create: `backend/internal/youtuberelay/service.go`
- Create: `backend/internal/youtuberelay/service_test.go`

- [ ] **Step 1: Write failing service tests**

Use an `httptest.Server` whose hostname is injected through a test-only
upstream validator. Tests cover:

- first 1 MiB probe succeeds and records initialization/index ranges;
- probe expands once up to 4 MiB when the first response is incomplete;
- no complete media body is requested;
- session ticket is 32 random bytes encoded with base64url (43 characters);
- two sessions succeed and the third returns `ErrCapacity`;
- same user/article reuses its live session;
- idle expiry removes a session;
- `Open` forwards single `Range` and `If-Range`;
- multiple ranges return `ErrInvalidRange`;
- only the response-header allowlist is exposed;
- upstream 403 re-resolves the same format IDs once;
- client context cancellation stops response copying/opening.

- [ ] **Step 2: Run service tests and verify RED**

```bash
cd backend
go test ./internal/youtuberelay -run 'TestService|TestRange' -count=1 -v
```

Expected: compile failure for missing `Service`.

- [ ] **Step 3: Implement session/service contracts**

Expose:

```go
type StartRequest struct {
    UserID    int
    ArticleID int
    VideoID   string
}

type Playback struct {
    Ticket         string
    Mode           string
    Quality        int
    ExpiresAt      time.Time
    HasProgressive bool
}

type StreamKind string

const (
    StreamVideo       StreamKind = "video"
    StreamAudio       StreamKind = "audio"
    StreamProgressive StreamKind = "progressive"
)

func (s *Service) Start(ctx context.Context, req StartRequest) (Playback, error)
func (s *Service) Manifest(ticket string) ([]byte, error)
func (s *Service) Open(ctx context.Context, ticket string, kind StreamKind, rangeHeader, ifRange string) (*http.Response, error)
func (s *Service) Close()
```

The service owns a mutex-protected ticket map and user/article reuse index.
Cleanup runs on a ticker, with injectable clock/ticker settings in tests.

- [ ] **Step 4: Implement bounded MP4 probing**

Send:

```http
Range: bytes=0-1048575
Accept-Encoding: identity
```

Retry once with `bytes=0-4194303` only for an incomplete-box error. Read through
`io.LimitReader(max+1)`, require 200 or 206, and reject larger bodies. Apply
resolver-provided `User-Agent` and `Referer`, but discard hop-by-hop headers.

- [ ] **Step 5: Implement Range opening and one refresh**

Accept empty range or exactly one `bytes=N-M`, `bytes=N-`, or `bytes=-N`.
Reject commas and malformed values before touching upstream. On 403/410, call
the resolver once, find the stored format IDs, replace URLs/headers under the
session lock, and retry.

- [ ] **Step 6: Run package tests and commit**

```bash
cd backend
go test ./internal/youtuberelay -count=1 -v
git add internal/youtuberelay/service.go internal/youtuberelay/service_test.go
git commit -m "feat: relay ticketed YouTube byte ranges"
```

## Task 5: Gin API Handlers and Server Wiring

**Files:**
- Create: `backend/internal/api/youtube_playback.go`
- Create: `backend/internal/api/youtube_playback_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing handler tests**

Use fakes for article lookup and relay service. Cover:

```go
func TestCreateYouTubePlaybackUsesServerOwnedMediaURL(t *testing.T) {
    // Fake article has media_type video/youtube and stored embed ID.
    // Request body containing {"url":"https://evil.example"} is ignored.
    // Service receives only the ID extracted from the stored article.
}
```

Also cover invalid article ID, missing article, Bilibili article, malformed
stored YouTube URL, capacity 429, resolver 502, no media 422, invalid ticket
404, invalid range 416, timeout 504, manifest MIME type, HEAD without body,
206 header forwarding, and copy cancellation.

- [ ] **Step 2: Run handler tests and verify RED**

```bash
cd backend
go test ./internal/api -run TestYouTubePlayback -count=1 -v
```

Expected: compile failure because the handler is absent.

- [ ] **Step 3: Implement handler interfaces and responses**

The start response is:

```go
type youtubePlaybackResponse struct {
    ManifestURL    string    `json:"manifest_url,omitempty"`
    ProgressiveURL string    `json:"progressive_url,omitempty"`
    Mode           string    `json:"mode"`
    Quality        int       `json:"quality"`
    ExpiresAt      time.Time `json:"expires_at"`
}
```

Use `rss.ExtractVideo(article.MediaURL)` and require platform `youtube`. Public
media handlers set `Cache-Control: private, no-store`, `X-Content-Type-Options:
nosniff`, and copy only:

```text
Content-Type Content-Length Content-Range Accept-Ranges ETag Last-Modified
```

Do not log the ticket.

- [ ] **Step 4: Wire service and routes**

In `main.go`:

1. construct resolver/service/handler;
2. `defer service.Close()`;
3. register public ticket routes before the protected group;
4. register `POST /api/articles/:id/youtube-playback` inside the JWT/RLS group;
5. exclude `/api/media/youtube/.*` from gzip path regexes.

- [ ] **Step 5: Run backend tests and commit**

```bash
cd backend
gofmt -w internal/youtuberelay internal/api/youtube_playback.go \
  internal/api/youtube_playback_test.go cmd/server/main.go
go test ./... -count=1
git add internal/api/youtube_playback.go internal/api/youtube_playback_test.go cmd/server/main.go
git commit -m "feat: expose authenticated YouTube relay API"
```

## Task 6: React DASH Player and Platform Branching

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/package-lock.json`
- Modify: `frontend/src/api/client.ts`
- Create: `frontend/src/components/YouTubeRelayPlayer.tsx`
- Create: `frontend/test/YouTubeRelayPlayer.test.tsx`
- Modify: `frontend/src/components/ArticlePlayerCard.tsx`
- Modify: `frontend/src/components/VideoEmbed.tsx`
- Modify: `frontend/test/VideoEmbed.test.tsx`

- [ ] **Step 1: Install dash.js**

```bash
cd frontend
npm install dashjs@5.2.0
```

Expected: only `package.json` and `package-lock.json` dependency entries change.

- [ ] **Step 2: Write failing API/player tests**

Mock dash.js:

```ts
const initialize = vi.fn()
const destroy = vi.fn()
vi.mock('dashjs', () => ({
  MediaPlayer: () => ({ create: () => ({ initialize, destroy, on: vi.fn(), off: vi.fn() }) }),
}))
```

Tests must assert:

- `POST /articles/2391/youtube-playback` is called once;
- `initialize(video, manifest_url, false)` receives the returned MPD;
- the UI shows `1080p` or actual `720p`;
- unmount calls `destroy`;
- missing `MediaSource` assigns the progressive relay to `<video src>`;
- startup failure shows `重试` and does not create a YouTube iframe;
- clicking retry starts a new session;
- Bilibili still renders `player.bilibili.com` eagerly;
- a body YouTube `VideoEmbed` renders an external link/message, not an iframe.

- [ ] **Step 3: Run targeted tests and verify RED**

```bash
cd frontend
npm test -- YouTubeRelayPlayer.test.tsx VideoEmbed.test.tsx
```

Expected: missing component/API symbols and the old YouTube iframe assertion fail.

- [ ] **Step 4: Add API types and component**

Add:

```ts
export interface YouTubePlaybackSession {
  manifest_url?: string
  progressive_url?: string
  mode: 'dash' | 'progressive'
  quality: number
  expires_at: string
}

export const createYouTubePlayback = (articleId: number) =>
  api.post<YouTubePlaybackSession>(`/articles/${articleId}/youtube-playback`)
    .then(res => res.data)
```

`YouTubeRelayPlayer` owns loading/error/session state, a `<video controls
playsInline preload="metadata">`, and the dash.js lifecycle. Autoplay stays
false. On dash.js fatal error, destroy and show retry rather than silently
switching to a direct iframe.

- [ ] **Step 5: Branch the primary player**

In `ArticlePlayerCard`:

```tsx
if (v.platform === 'youtube') {
  return <YouTubeRelayPlayer articleId={article.id} originalURL={article.url} />
}
return <VideoEmbed {...v} />
```

In `VideoEmbed`, Bilibili keeps its eager iframe. YouTube returns a card/link
that says playback is available from the article's primary player and never
sets an iframe `src`.

- [ ] **Step 6: Run frontend tests/build and commit**

```bash
cd frontend
npm run check
npm run build
git add package.json package-lock.json src/api/client.ts \
  src/components/YouTubeRelayPlayer.tsx src/components/ArticlePlayerCard.tsx \
  src/components/VideoEmbed.tsx test/YouTubeRelayPlayer.test.tsx test/VideoEmbed.test.tsx
git commit -m "feat: play YouTube through the authenticated relay"
```

## Task 7: Repository Nginx Media Location

**Files:**
- Modify: `frontend/nginx.conf`
- Test: `frontend/test/nginxMediaRelay.test.cjs`

- [ ] **Step 1: Write a failing static Nginx contract test**

The Node test reads `frontend/nginx.conf`, extracts the media location, and
asserts it appears before generic `/api` and contains:

```text
location ^~ /api/media/youtube/
proxy_buffering off;
proxy_request_buffering off;
proxy_cache off;
gzip off;
proxy_set_header Range $http_range;
proxy_set_header If-Range $http_if_range;
proxy_read_timeout 6h;
proxy_send_timeout 6h;
```

- [ ] **Step 2: Run the legacy test and verify RED**

```bash
cd frontend
node --test test/nginxMediaRelay.test.cjs
```

Expected: FAIL because the media location is absent.

- [ ] **Step 3: Add the media location**

Use the same Docker DNS pattern as the generic API location:

```nginx
location ^~ /api/media/youtube/ {
    set $upstream_api http://api:8080;
    proxy_pass $upstream_api;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Range $http_range;
    proxy_set_header If-Range $http_if_range;
    proxy_buffering off;
    proxy_request_buffering off;
    proxy_cache off;
    gzip off;
    proxy_connect_timeout 10s;
    proxy_read_timeout 6h;
    proxy_send_timeout 6h;
}
```

- [ ] **Step 4: Run frontend verification and commit**

```bash
cd frontend
npm run check
npm run build
git add nginx.conf test/nginxMediaRelay.test.cjs
git commit -m "infra: stream YouTube ranges without buffering"
```

## Task 8: Full Local Verification and Review

**Files:**
- Review all files changed since `f23fb05`.

- [ ] **Step 1: Run formatting and static diff checks**

```bash
cd backend
gofmt -w internal/youtuberelay internal/api/youtube_playback.go \
  internal/api/youtube_playback_test.go cmd/server/main.go
cd ..
git diff --check
```

Expected: no output from `git diff --check`.

- [ ] **Step 2: Run backend race-sensitive package tests**

```bash
cd backend
go test ./internal/youtuberelay ./internal/api -race -count=1
go test ./... -count=1
```

Expected: all packages pass, no race report.

- [ ] **Step 3: Run frontend tests and production build**

```bash
cd frontend
npm run check
npm run build
```

Expected: all Vitest and legacy tests pass; production build succeeds.

- [ ] **Step 4: Build the production API and frontend images**

```bash
docker compose build api frontend
```

Expected: both images build; API image contains working `yt-dlp` and `node`.

- [ ] **Step 5: Inspect the complete branch**

```bash
git status --short
git log --oneline f23fb05..HEAD
git diff --stat f23fb05..HEAD
```

Expected: only planned tracked changes; no generated or backup files.

## Task 9: Integrate, Push, Deploy, and Verify Production

**Files/hosts:**
- Local `master`
- GitHub `origin/master`
- Beijing `/opt/rss-pal`
- Beijing `/opt/rss-pal/nginx.prod.conf`
- OCI `/etc/nginx/sites-enabled/rss-pal`

- [ ] **Step 1: Merge the feature branch into local master**

From the main worktree:

```bash
git checkout master
git merge --ff-only feat/youtube-dash-relay
```

Expected: fast-forward only. Existing untracked backups remain untouched.

- [ ] **Step 2: Push master**

```bash
git push origin master
```

Expected: `origin/master` advances to the verified commit.

- [ ] **Step 3: Back up and update Beijing Nginx**

Connect only through:

```bash
ssh -o ControlMaster=no -o ControlPath=none \
  -o ProxyJump=oci-rss-pal tencent-rss-pal
```

Copy `/opt/rss-pal/nginx.prod.conf` to a timestamped backup, add the media
location before generic `/api`, then validate the exact mounted config using
the frontend image/container before reload. Do not replace certificate or
server-name directives.

- [ ] **Step 4: Back up and update OCI Nginx**

Copy `/etc/nginx/sites-enabled/rss-pal` to a timestamped backup outside the
enabled directory. Add a `/api/media/youtube/` location that uses the same
Beijing HTTPS upstream/TLS verification settings as the existing catch-all,
with buffering/cache disabled, Range/If-Range preserved, and six-hour media
timeouts. Run:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

Expected: validation passes before reload.

- [ ] **Step 5: Update and deploy Beijing application**

Use the approved jump chain and the existing HTTPS fetch fallback:

```bash
cd /opt/rss-pal
git fetch --no-tags https://github.com/morefreeze/rss-pal.git master
git merge --ff-only FETCH_HEAD
docker compose build api frontend
docker compose up -d --no-deps api frontend
docker compose ps
```

Do not use `docker compose up -d --build frontend`, because that implicitly
rebuilds unrelated dependencies.

- [ ] **Step 6: Verify service and public health**

```bash
curl --noproxy '*' -fsS https://rss.morefreeze.top/api/health
curl --noproxy '*' -fsS -o /dev/null -w '%{http_code}\n' https://rss.morefreeze.top/
```

Expected: JSON status `ok` and public HTTP 200.

- [ ] **Step 7: Verify authenticated playback without client Clash**

Use the browser test account/session:

1. open a primary YouTube article;
2. confirm session creation succeeds and MPD loads;
3. confirm separate video/audio requests return 206;
4. seek near middle and end;
5. confirm actual quality label is 1080p when under cap or 720p fallback;
6. repeat in Safari/Pake;
7. confirm no request goes to `youtube.com`, `youtube-nocookie.com`, or
   `googlevideo.com` from the client.

- [ ] **Step 8: Verify split routing on Beijing**

Check fresh Mihomo journal entries while playback runs:

```bash
journalctl -u mihomo --since '5 minutes ago' --no-pager -o cat \
  | grep -Ei 'youtube|googlevideo|bilibili'
```

Expected: YouTube/GoogleVideo uses the Starlink node; a Bilibili article still
uses `DIRECT`.

- [ ] **Step 9: Roll back on acceptance failure**

If health, audio/video sync, seeking, or non-media API behavior fails:

1. deploy the prior application commit;
2. restore both timestamped Nginx backups;
3. validate both Nginx configurations;
4. reload Nginx;
5. recheck public health.

Do not alter DNS or restart the stopped OCI application services.
