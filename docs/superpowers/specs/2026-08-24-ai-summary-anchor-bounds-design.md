# AI Summary Anchor Bounds Design

**Date:** 2026-08-24

**Status:** Approved approach, awaiting written-spec review

## Goal

Make newly generated detailed summaries reliably include article navigation links when the source contains multiple meaningful sections, then regenerate and verify the two newest ready articles from Hacker News and 科技爱好者.

## Prompt Contract

The system-owned detailed-summary anchor instruction is strengthened as follows:

- Summarize in source order and group adjacent source blocks by meaning.
- If the article contains multiple meaningful sections or topic groups, the detailed summary must contain between **3 and 30** `[查看原文](#article-section-NNN)` links.
- Each summary group or paragraph may contain at most one article link.
- Every referenced ID must be copied from an anchor present in the supplied article content.
- Links should be distributed across the article in source order rather than clustered at the beginning.
- Do not attach a link to every source paragraph merely to reach the minimum; form useful semantic groups.
- A short article or an article that genuinely develops only one continuous idea may contain zero links.
- Never emit one or two links: valid outputs contain either zero links under the single-theme exception or 3–30 links for a multi-section article.

This remains a prompt-only constraint. No automatic server-side retry or post-processing is added in this change.

## Scope

The strengthened system-owned instruction applies wherever the existing anchor instruction is appended:

- default detailed summaries;
- streaming detailed summaries;
- image-assisted and image-assisted streaming detailed summaries;
- custom detailed templates that include `{content}`, as a system-owned suffix.

Brief summaries remain unchanged. Existing summaries are unchanged unless explicitly regenerated.

## Production Regeneration

After merge and Tencent deployment:

1. Resolve the exact feeds named Hacker News and 科技爱好者 from the production database.
2. Select the two newest articles per feed where `processing_state = 'ready'` and the stored body is non-empty.
3. Record article IDs, titles, publication times, previous detailed-summary hashes, and body structure before mutation.
4. Regenerate each article through the normal production summary endpoint. Use the same default regeneration behavior as the article page, including vision when usable images exist.
5. Process the four articles sequentially to avoid unnecessary model and token bursts.
6. Read back each stored detailed summary and count strict `#article-section-NNN` references.
7. Open or inspect the real article page and confirm every emitted reference has a rendered target.

No articles outside these four are regenerated.

## Acceptance

- Prompt tests prove the instruction explicitly requires either zero links for the narrow single-theme exception or 3–30 links for multi-section content.
- All detailed-summary paths retain the instruction; brief paths do not gain it.
- Backend and frontend test suites and the production frontend build pass.
- Tencent runtime reaches the deployed commit and local/public health checks pass.
- For each regenerated production article, report:
  - source/feed;
  - article ID and title;
  - whether it is multi-section or single-theme based on stored body evidence;
  - generated anchor count;
  - whether every referenced target exists in the rendered article;
  - any model non-compliance.
- A multi-section regenerated article with 0–2 or more than 30 links is a failed acceptance result, even if the generation request itself returned HTTP 200.

## Evidence and Safety

- Preserve the four pre-generation summary hashes so the exact mutation set is auditable.
- Do not print credentials, cookies, API keys, or full private configuration.
- Deployment restart requires the existing explicit confirmation boundary.
- If generation fails or violates the bound, report the failure; do not silently expand scope or regenerate additional articles.
