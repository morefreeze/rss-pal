# Bookmarklet full-content capture repair

## Problem

Both the RSS Pal extension and bookmarklet fail to save
`https://bojieli.github.io/ai-agent-book/book/chapter1/` with the public error
`新建文章失败`.

Production API logs show that both requests reach the shared
`POST /api/bookmarklet/capture` handler and fail at the final article insert:

```text
pq: invalid byte sequence for encoding "UTF8": 0xe7 0xb4 0x2e
pq: invalid byte sequence for encoding "UTF8": 0xe4 0xbd 0x2e
```

The HTML extraction path truncates Markdown with
`content[:50000] + "..."`. The 50,000-byte boundary can split a multi-byte
UTF-8 code point, producing invalid text that PostgreSQL correctly rejects.
Even when the boundary lands safely, this limit discards the remainder of a
long chapter. The required behavior is to preserve the complete extracted
article.

## Approaches considered

1. **Remove the HTML-capture storage truncation (chosen).** Preserve the full
   extracted Markdown and rely on PostgreSQL `TEXT` for storage. Keep the
   existing 4 MiB HTTP request limit and the summarizer's independent
   8,000-rune prompt limit.
2. **Remove every 50 KB ingestion limit.** This would also change PDF, RSS
   reader, OCR, and link-set behavior. It is broader than the reported HTML
   capture incident and would require separate compatibility and resource
   analysis.
3. **Raise the limit and truncate on a UTF-8 boundary.** This prevents the
   database error but still loses part of the chapter, contradicting the
   full-content requirement.

## Approved design

### Capture flow

The extension and bookmarklet continue to send cleaned page HTML to the same
capture endpoint. The handler continues to authenticate the bookmarklet token,
normalize the URL, extract Markdown, resolve duplicate captures, compute reading
metrics, and insert the article. The only behavioral change is that
`extractContentFromHTML` returns all extracted Markdown instead of slicing it at
50,000 bytes.

The existing safeguards remain separate and unchanged:

- `captureMaxBodyBytes` rejects JSON request bodies above 4 MiB;
- the database `articles.content` column remains PostgreSQL `TEXT`;
- the summarizer continues to cap model input at 8,000 runes;
- duplicate detection and overwrite prompts continue to compare the complete
  extracted content supplied by this path.

### Scope

Modify only the HTML bookmarklet/extension extraction behavior in
`backend/internal/api/bookmarklet.go` and its regression tests. Do not change
PDF capture, PDF OCR, RSS reader, link-set extraction, frontend code, extension
code, database schema, or deployment configuration.

### Error handling

Existing HTTP responses remain unchanged. Removing the invalid byte-producing
slice allows valid UTF-8 Markdown to reach the repository. Any unrelated insert
error still logs its exact database error server-side and returns
`新建文章失败` to the client.

## Verification

Add a regression test that builds an HTML article whose extracted Markdown is
larger than 50,000 bytes and contains Chinese text across the old boundary. The
test must prove that:

- the extracted result is valid UTF-8;
- the result is longer than the old 50,000-byte ceiling;
- a unique marker at the end of the article remains present;
- the old synthetic `...` truncation suffix is not added.

Run the focused API test first to demonstrate the regression, then make the
minimal production change and rerun the focused test. Finally run the complete
backend Go test suite and `git diff --check`.

## Delivery boundary

This task authorizes the design and local implementation. It does not authorize
pushing, merging, deploying to Tencent, restarting containers, or changing live
configuration. Those remain separate actions.
