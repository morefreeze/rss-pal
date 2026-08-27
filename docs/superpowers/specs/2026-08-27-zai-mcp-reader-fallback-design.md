# Z.ai MCP Reader Fallback Design

**Date:** 2026-08-27

## Goal

Use the Z.ai Coding Plan Web Reader MCP service as the first server-side fallback when RSS Pal cannot extract a useful article body directly, while retaining Jina Reader as the final fallback and keeping browser-captured bookmarklet content unchanged.

## Scope

The Reader fallback applies to server-side content extraction through `rss.ContentFetcher`, including:

- manual article re-fetches;
- worker short-content re-fetches;
- feed import and discovery content fetches;
- link-set and batch child-link content fetches;
- other callers of `FetchContent` or `FetchContentWithMetadata`.

The following specialized paths remain unchanged:

- bookmarklet captures that already submit content extracted in the current browser page;
- `FetchHTMLDocument`, whose callers require a DOM rather than cleaned Markdown;
- transcript-provider-specific fetching;
- PDF byte downloading and OCR;
- RSS/Atom document fetching.

## Fetch Routing

For server-side Markdown content fetching, use this ordered route:

1. Fetch and extract the target page directly.
2. If the direct request fails, returns a non-200 response, or extracts fewer than 300 characters, call Z.ai Web Reader MCP.
3. If MCP fails, returns a tool-level error, returns an embedded JSON error, or produces empty content, call the existing Jina Reader fallback.
4. If all fallbacks fail, preserve the existing `ContentFetcher` return semantics: propagate the original transport error, or return the best direct result for HTTP/short-content cases.

Automatic and batch calls use the MCP cache (`no_cache=false`). The manual `POST /api/articles/:id/content` path uses a new fresh-fetch entry point with `no_cache=true`; its response contract remains `{ "content": "..." }`.

Successful direct extraction does not consume MCP quota. Each fallback attempt makes one MCP tool call and is not automatically retried. MCP calls are bounded by a process-wide concurrency limit of two.

## Authentication and Configuration

Add a dedicated `ZAI_READER_API_KEY` environment variable. It must not fall back to or reuse `CLAUDE_API_KEY`. When the Reader key is empty, RSS Pal skips MCP and preserves the current direct-to-Jina behavior.

Use `ZAI_READER_MCP_URL` as an optional endpoint override for tests and diagnostics. Its production default is:

```text
https://api.z.ai/api/mcp/web_reader/mcp
```

Document and pass both variables to the API and worker containers. The production key will be created in the Z.ai console with the name `rss-pal-reader`, stored only in Tencent `/opt/rss-pal/.env`, and never printed in logs or chat.

## MCP Protocol

Implement the Reader client as a focused unit in `backend/internal/rss/zai_reader.go`. For each fetch:

1. Send JSON-RPC `initialize` with protocol version `2024-11-05`.
2. Read the `Mcp-Session-Id` response header.
3. Send `notifications/initialized` with that session ID.
4. Send `tools/call` for tool `webReader` with:

```json
{
  "url": "<target URL>",
  "timeout": 20,
  "no_cache": false,
  "return_format": "markdown",
  "retain_images": true,
  "no_gfm": false,
  "keep_img_data_url": false,
  "with_images_summary": false,
  "with_links_summary": false
}
```

Manual fresh fetching changes only `no_cache` to `true`.

Requests send the dedicated bearer token plus `Accept: application/json, text/event-stream`. Responses may be SSE; the client extracts JSON from `data:` lines. A successful tool response contains a text content block whose value is itself a JSON-encoded Reader result. The client decodes both layers and returns `content` and `title`.

Treat all of the following as MCP failure and allow Jina fallback:

- non-2xx HTTP status;
- missing session ID;
- JSON-RPC error;
- `result.isError=true`;
- missing text block;
- malformed nested JSON;
- nested `{ "error": "..." }`;
- empty Reader `content`;
- timeout or context cancellation.

Errors may identify the stage and HTTP status but must not include the API key, authorization header, session ID, or full response body.

## Code Boundaries

- `backend/internal/rss/zai_reader.go`: MCP session, JSON-RPC/SSE parsing, nested Reader response parsing, and concurrency limit.
- `backend/internal/rss/content.go`: fallback ordering and fresh-versus-cached Reader selection.
- `backend/internal/api/content.go`: route manual re-fetches through the fresh entry point.
- `docker-compose.yml`: pass Reader configuration to API and worker.
- `.env.example`: document the dedicated Reader variables.

No frontend change is required.

## Testing

Use `httptest.Server` protocol fixtures. Tests must prove:

- direct content of at least 300 characters bypasses MCP;
- a short direct result performs initialize, initialized notification, and tool call in order;
- the session header is forwarded after initialization;
- cached server-side calls send `no_cache=false`;
- manual fresh calls send `no_cache=true`;
- successful nested Reader JSON returns Markdown and title;
- each MCP failure class falls back to Jina;
- an empty `ZAI_READER_API_KEY` never contacts MCP;
- automatic callers still receive Jina content if Reader is unavailable;
- the existing content cleanup and limit behavior remains intact.

Run the focused RSS/API tests and the complete backend Go suite. The known transcript timeout test is considered a baseline flake only if an isolated repeated run passes; it must not conceal new failures.

## Deployment and Verification

Implement in the isolated `codex/zai-mcp-reader` worktree, commit, push, and deploy to Tencent production. Before container recreation:

1. Create the independent Z.ai key and prove it can complete a full MCP Reader session.
2. Back up Tencent `/opt/rss-pal/.env` and add `ZAI_READER_API_KEY` without printing it.
3. Pull the deployed revision and build API/worker images.
4. Recreate API and worker with all three production Compose files, including `docker-compose.override.oci-egress-20260729.yml`.

Verify:

- running revision and images match the pushed commit;
- API and worker have `ZAI_READER_API_KEY` set without exposing its value;
- worker proxy variables and three-file Compose provenance remain present;
- direct API health, worker health, Tencent-direct health, and public health pass;
- a real server-side fallback fetch returns Reader content through the production proxy;
- recent API/worker logs contain no MCP authentication, panic, fatal, or connection errors.

If deployment verification fails, restore the `.env` backup and previous images/revision, recreate API and worker with the same three Compose files, and re-run health checks.
