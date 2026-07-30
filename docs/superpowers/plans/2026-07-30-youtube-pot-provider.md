# YouTube PO Token Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the deployed YouTube DASH relay resolve videos from the Starlink exit without personal YouTube cookies by adding a pinned internal PO Token provider.

**Architecture:** A Docker Compose sidecar generates PO tokens and exposes them only to the Compose network. The API image contains the matching yt-dlp provider plugin, while the resolver opts into the mweb client and provider URL only when `YOUTUBE_POT_PROVIDER_URL` is configured; all ticket, Range, bitrate, and frontend behavior remains unchanged.

**Tech Stack:** Go 1.25, yt-dlp, BgUtils PO Token Provider 1.3.1, Docker Compose, Alpine, Mihomo, Nginx.

---

## File Map

- Modify `backend/internal/youtuberelay/resolver.go`: configure deterministic
  provider arguments from a server-owned environment variable.
- Modify `backend/internal/youtuberelay/resolver_test.go`: prove provider
  arguments are present only when configured.
- Create `backend/internal/youtuberelay/deployment_test.go`: guard the pinned,
  internal-only Compose sidecar and checksummed plugin installation.
- Modify `backend/Dockerfile`: install the pinned provider plugin zip after
  SHA-256 verification.
- Modify `docker-compose.yml`: add the internal provider service and connect
  the API to it.

## Task 1: Resolver Provider Arguments

**Files:**
- Modify: `backend/internal/youtuberelay/resolver_test.go`
- Modify: `backend/internal/youtuberelay/resolver.go`

- [ ] **Step 1: Write the failing resolver tests**

Add a test that constructs the resolver with a provider URL and expects the
two extractor arguments before `-J`:

```go
func TestYTDLPResolverUsesConfiguredPOTProvider(t *testing.T) {
    runner := &fakeCommandRunner{output: resolvedInfoJSON(t)}
    resolver := YTDLPResolver{
        Runner:         runner,
        Binary:         "yt-dlp",
        Timeout:        time.Second,
        POTProviderURL: "http://youtube-pot:4416",
    }

    if _, err := resolver.Resolve(context.Background(), "dQw4w9WgXcQ"); err != nil {
        t.Fatal(err)
    }
    wantArgs := []string{
        "--no-warnings",
        "--no-playlist",
        "--skip-download",
        "--socket-timeout", "20",
        "--js-runtimes", "deno",
        "--extractor-args", "youtube:player_client=mweb",
        "--extractor-args", "youtubepot-bgutilhttp:base_url=http://youtube-pot:4416",
        "-J",
        "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
    }
    if !reflect.DeepEqual(runner.args, wantArgs) {
        t.Fatalf("args = %q, want %q", runner.args, wantArgs)
    }
}
```

Add a second test using `t.Setenv("YOUTUBE_POT_PROVIDER_URL",
"http://youtube-pot:4416")` and assert `NewYTDLPResolver()` copies that value
into the resolver. Keep the existing safe-argument test unchanged to prove
the unconfigured path does not add provider flags.

- [ ] **Step 2: Run the resolver tests and verify RED**

Run:

```bash
cd backend
go test ./internal/youtuberelay -run 'TestYTDLPResolverUsesConfiguredPOTProvider|TestNewYTDLPResolverReadsPOTProviderURL' -count=1 -v
```

Expected: compilation fails because `POTProviderURL` does not exist.

- [ ] **Step 3: Implement the minimal resolver configuration**

Add `POTProviderURL string` to `YTDLPResolver`. In `NewYTDLPResolver`, read
`os.Getenv("YOUTUBE_POT_PROVIDER_URL")`. In `Resolve`, append:

```go
if r.POTProviderURL != "" {
    args = append(args,
        "--extractor-args", "youtube:player_client=mweb",
        "--extractor-args", "youtubepot-bgutilhttp:base_url="+r.POTProviderURL,
    )
}
args = append(args,
    "-J",
    "https://www.youtube.com/watch?v="+videoID,
)
```

Keep command execution shell-free and do not log the provider or upstream
media URLs.

- [ ] **Step 4: Run resolver tests and verify GREEN**

Run:

```bash
cd backend
go test ./internal/youtuberelay -run 'TestYTDLPResolver' -count=1 -v
```

Expected: all resolver tests pass.

- [ ] **Step 5: Commit Task 1**

```bash
git add backend/internal/youtuberelay/resolver.go backend/internal/youtuberelay/resolver_test.go
git commit -m "fix: configure YouTube PO token resolver"
```

## Task 2: Pinned Internal Provider Deployment

**Files:**
- Create: `backend/internal/youtuberelay/deployment_test.go`
- Modify: `backend/Dockerfile`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Write the failing deployment contract test**

Create a test that reads the repository files and checks exact deployment
contracts:

```go
func TestPOTProviderDeploymentIsPinnedAndInternal(t *testing.T) {
    compose := mustReadDeploymentFile(t, "../../../docker-compose.yml")
    dockerfile := mustReadDeploymentFile(t, "../../Dockerfile")

    requiredCompose := []string{
        "youtube-pot:",
        "image: brainicism/bgutil-ytdlp-pot-provider:1.3.1",
        "YOUTUBE_POT_PROVIDER_URL: http://youtube-pot:4416",
        "youtube-pot:\n        condition: service_started",
    }
    for _, value := range requiredCompose {
        if !strings.Contains(compose, value) {
            t.Fatalf("docker-compose.yml missing %q", value)
        }
    }
    if strings.Contains(compose, "4416:4416") {
        t.Fatal("youtube-pot must not publish its port to the host")
    }

    requiredDockerfile := []string{
        "BGUTIL_PROVIDER_VERSION=1.3.1",
        "BGUTIL_PROVIDER_SHA256=b8ceec7f76143da172aaf5ebeec0c2d218e5680c063b931586bca48567069b38",
        "/root/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip",
        "sha256sum -c -",
    }
    for _, value := range requiredDockerfile {
        if !strings.Contains(dockerfile, value) {
            t.Fatalf("backend/Dockerfile missing %q", value)
        }
    }
}
```

`mustReadDeploymentFile` calls `os.ReadFile`, fails the test on error, and
returns the file as a string.

- [ ] **Step 2: Run the deployment test and verify RED**

Run:

```bash
cd backend
go test ./internal/youtuberelay -run TestPOTProviderDeploymentIsPinnedAndInternal -count=1 -v
```

Expected: failure reporting the missing `youtube-pot:` service.

- [ ] **Step 3: Install the checksummed plugin in the API image**

After installing runtime packages in `backend/Dockerfile`, add:

```dockerfile
ARG BGUTIL_PROVIDER_VERSION=1.3.1
ARG BGUTIL_PROVIDER_SHA256=b8ceec7f76143da172aaf5ebeec0c2d218e5680c063b931586bca48567069b38
RUN mkdir -p /root/.config/yt-dlp/plugins \
    && wget -qO /root/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip \
        "https://github.com/Brainicism/bgutil-ytdlp-pot-provider/releases/download/${BGUTIL_PROVIDER_VERSION}/bgutil-ytdlp-pot-provider.zip" \
    && echo "${BGUTIL_PROVIDER_SHA256}  /root/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip" \
        | sha256sum -c -
```

- [ ] **Step 4: Add the internal Compose sidecar**

Add this service without a `ports` key:

```yaml
  youtube-pot:
    image: brainicism/bgutil-ytdlp-pot-provider:1.3.1
    restart: unless-stopped
```

Add this API environment value:

```yaml
      YOUTUBE_POT_PROVIDER_URL: http://youtube-pot:4416
```

Add this API dependency alongside PostgreSQL and RSSHub:

```yaml
      youtube-pot:
        condition: service_started
```

- [ ] **Step 5: Run deployment and package tests**

Run:

```bash
cd backend
go test ./internal/youtuberelay -count=1 -v
go test ./... -count=1
```

Expected: all tests pass.

- [ ] **Step 6: Commit Task 2**

```bash
git add backend/Dockerfile backend/internal/youtuberelay/deployment_test.go docker-compose.yml
git commit -m "fix: add internal YouTube PO token provider"
```

## Task 3: Build, Deploy, and Production Validation

**Files:**
- No source changes expected.
- Update the live checkout at `/opt/rss-pal` only after commits are pushed.

- [ ] **Step 1: Run pre-deploy verification**

Run:

```bash
cd backend
gofmt -w internal/youtuberelay/resolver.go \
    internal/youtuberelay/resolver_test.go \
    internal/youtuberelay/deployment_test.go
go vet ./...
go test ./internal/youtuberelay ./internal/api -race -count=1
go test ./... -count=1
```

Expected: every command exits zero.

- [ ] **Step 2: Merge and push**

Fast-forward `master` to the verified branch while preserving unrelated
untracked backup files, then push `origin/master`. Verify local and remote
commit IDs match.

- [ ] **Step 3: Update the Beijing checkout**

All access must use the OCI jump host:

```bash
ssh -o ControlMaster=no -o ControlPath=none \
  -o ProxyJump=oci-rss-pal tencent-rss-pal \
  'cd /opt/rss-pal && git pull --ff-only'
```

Tag the currently running API image with a timestamped rollback tag before
building.

- [ ] **Step 4: Build and start provider/API**

Build the API with the Beijing host's Mihomo HTTP proxy build arguments, then:

```bash
docker compose up -d youtube-pot
docker compose up -d --no-deps api
```

Verify:

```bash
docker compose ps youtube-pot api
docker compose exec -T api yt-dlp -v --simulate \
  --extractor-args youtubepot-bgutilhttp:base_url=http://youtube-pot:4416 \
  --extractor-args youtube:player_client=mweb \
  https://www.youtube.com/watch?v=2RJiaf0SY8s
```

The verbose output must list the external `bgutil:http-1.3.1` provider and
resolve video metadata without the bot-check error. Do not print signed media
URLs.

- [ ] **Step 5: Validate the public playback path**

In the existing authenticated Chrome session, open article `2401` and verify:

- the player reports the actual adaptive or progressive quality;
- `POST /api/articles/2401/youtube-playback` returns HTTP 200;
- a same-origin manifest loads;
- media requests return HTTP 206 with `Content-Range`;
- changing `video.currentTime` triggers a seek and resumes media requests;
- no direct browser request goes to `youtube.com` or `googlevideo.com`.

Check API logs for ticket-free `youtube_relay` entries and Mihomo logs for
YouTube/GoogleVideo using Starlink. Re-open Bilibili article `2391` and verify
its iframe remains direct and the server route rule remains `DIRECT`.

- [ ] **Step 6: Apply the rollback threshold**

Rollback the API image if any of these remain after one clean retry:

- playback session creation still returns 5xx;
- the manifest contains an upstream URL;
- media does not return 206;
- seeking begins a full-file transfer;
- public health fails.

Otherwise retain the provider and record the final commit, container status,
public health result, selected quality, Range response, seek result, and
Mihomo route in the handoff.
