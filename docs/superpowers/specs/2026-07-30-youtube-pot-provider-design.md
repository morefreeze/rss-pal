# YouTube PO Token Provider Design

## Context

The DASH Range relay is deployed, but a real authenticated production request
for article `2401` fails while resolving YouTube metadata. The Beijing API
reaches YouTube through the configured Starlink Mihomo route, and `yt-dlp`
returns:

```text
Sign in to confirm you’re not a bot
```

Forcing the `web_embedded` and `android_vr` clients does not make this video
resolvable from the selected exit. The failure occurs before MP4 probing,
manifest generation, or media Range relay, so Nginx and the ticketed media
routes are not the cause.

## Goal

Let the Beijing API resolve normal embeddable YouTube videos from the Starlink
exit without storing or using a personal YouTube account cookie. Preserve the
existing security, bitrate, Range, session, and Bilibili-direct contracts.

## Chosen Approach

Run the pinned BgUtils PO Token provider as an internal Docker Compose sidecar
and install its matching yt-dlp plugin in the API image:

```text
Authenticated browser
  -> Beijing API yt-dlp
  -> internal bgutil HTTP plugin provider
  -> mweb player request with generated PO token
  -> YouTube and GoogleVideo through Mihomo / Starlink
```

Versions are pinned to `1.3.1` for both the sidecar and plugin. The plugin
artifact is downloaded during the API image build and verified against its
published SHA-256 digest. The provider has no host port, volume, cookie, or
credential.

The resolver enables the provider only when
`YOUTUBE_POT_PROVIDER_URL` is configured. Docker Compose sets it to
`http://youtube-pot:4416`. Tests and non-Compose development can omit the
variable and retain the existing deterministic command.

## Resolver Contract

When the provider URL is set, the resolver adds these yt-dlp arguments:

```text
--extractor-args youtube:player_client=mweb
--extractor-args youtubepot-bgutilhttp:base_url=http://youtube-pot:4416
```

The command still runs without a shell, validates the eleven-character video
ID, requests metadata only, uses Deno for YouTube JavaScript challenges, has a
45-second hard timeout, and caps JSON output at 16 MiB.

Signed GoogleVideo URLs remain server-only. The plugin provider URL is
server-owned configuration and is never accepted from an HTTP request.

## Deployment

`docker-compose.yml` adds:

- an internal `youtube-pot` service using
  `brainicism/bgutil-ytdlp-pot-provider:1.3.1`;
- `restart: unless-stopped`;
- the provider URL in the API environment;
- an API dependency on the started sidecar.

The API Dockerfile installs the release plugin zip under yt-dlp's root plugin
directory after checking its SHA-256 digest. No personal account state is
copied into the image or mounted at runtime.

## Failure Handling

If the provider is unavailable or YouTube rejects the generated token, the
existing resolver error path returns HTTP 502. The frontend keeps its retry
button and original YouTube link. The service does not fall back to account
cookies, arbitrary upstream URLs, a direct YouTube iframe, or a full-file
download.

## Verification

Automated verification covers:

- exact provider-related yt-dlp arguments when configured;
- unchanged arguments when the provider is absent;
- pinned sidecar image, internal-only deployment, provider environment, and
  plugin checksum;
- all existing relay, API, and frontend tests.

Production verification uses an authenticated article and requires:

1. playback session creation returns HTTP 200;
2. the manifest is same-origin and contains only ticketed relay URLs;
3. video or audio media responds with HTTP 206 and `Content-Range`;
4. seeking changes the HTML video current time without a complete-file
   download;
5. Mihomo logs YouTube/GoogleVideo through Starlink;
6. Bilibili remains client-direct and server-side Bilibili fetches remain
   `DIRECT`.

## Rollback

Before switching containers, retain tags for the current API and frontend
images. A rollback removes the new API container and sidecar by restoring the
prior image tag and recreating only the affected services. Existing article,
database, Nginx, DNS, and OCI configuration are unchanged.
