# Cloudflare Placeholder Truncation Fix

## Problem

Article 2608 is complete on the Cloudflare source page but truncated in RSS
Pal. The source HTML contains visible remote images alongside decorative
`data:image/bmp;base64,...` placeholders. RSS Pal currently converts both to
Markdown. Those placeholders consume the 50,000-byte content allowance, and
the existing byte slice cuts the second Base64 URL before the remaining prose.

## Scope

- Prevent explicit decorative Base64 image placeholders from entering extracted
  Markdown.
- Keep legitimate remote images and data images that are not identified as
  placeholders.
- Make the 50,000-byte fallback limit UTF-8 safe and avoid returning a partial
  Markdown image when the boundary lands inside one.
- Deploy the backend and worker to the Tencent production runtime.
- After deployment, inspect articles fetched during the preceding 72 hours and
  re-fetch only rows that contain a `data:image` payload or end with the
  extractor's `...` truncation marker.
- Do not rewrite other recent articles or older history.

## Extraction Design

Before Markdown conversion, remove an image when all of the following hold:

1. Its `src` starts with `data:image/`; and
2. it is explicitly marked as a presentation placeholder by
   `data-image-placeholder`, class `pt-image-placeholder`, or
   `aria-hidden="true"`.

This rule is intentionally narrower than removing every data URI. It handles
Cloudflare's current markup without deleting legitimate embedded diagrams.
Existing lazy-image promotion remains unchanged: when a recognized `data-src`
attribute exists, it can still replace a placeholder before cleanup.

Content limiting will operate through a helper that returns at most 50,000
bytes plus `...`, backs up to a valid UTF-8 boundary, and removes an unmatched
Markdown image opener at the end. Both direct extraction and Jina fallback use
the same helper.

## Testing

Add focused tests covering:

- Cloudflare-style visible image plus Base64 placeholder: the remote image and
  prose after it remain, while the Base64 payload is absent.
- A legitimate unmarked data image remains unchanged.
- Over-limit UTF-8 content remains valid.
- A limit reached inside an image destination does not return a partial image.

The first Cloudflare regression test must fail against the current code before
the implementation is added. Run the focused RSS package tests, then the full
backend test suite.

## Deployment and Backfill

1. Push the verified commit to `master` through the repository's normal GitHub
   workflow.
2. Verify the Tencent checkout revision and healthy `api`, `worker`, and
   `frontend` containers.
3. Query, without modifying data, the IDs fetched in the last 72 hours whose
   stored content contains `data:image` or ends in `...`.
4. Re-fetch that fixed ID set through the existing content-fetch path. Process
   rows serially so failures are attributable and retryable.
5. Verify every selected row no longer contains explicit placeholder data,
   retains a valid ending, and is in `ready` state. Specifically verify article
   2608 ends with the source article's RFC 9234 deployment conclusion.
6. Verify the private health endpoint on the host and the public health endpoint
   through `rss.morefreeze.top`.

Before the production mutation, record the selected IDs and their existing
content lengths so the exact backfill scope is auditable. A failed individual
re-fetch must not widen the selection or trigger an all-article rewrite.

## Compatibility and Rollback

No API or database schema changes are required. Rollback is the previous
application commit. Re-fetching restores source-derived content and is not
automatically reversible, so the pre-backfill ID/length inventory and existing
database backups are the operational audit boundary.
