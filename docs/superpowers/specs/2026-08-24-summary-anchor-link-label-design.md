# Summary Anchor Link Label Design

## Goal

Rename article-section links in detailed AI summaries from `查看原文` to `跳转原文` for both existing and newly generated summaries.

## Existing summaries

Stored summaries are not rewritten. When `SummaryMarkdown` recognizes a valid `#article-section-NNN` link, it renders the visible label as `跳转原文` regardless of the link text stored in Markdown.

This means existing summaries containing `[查看原文](#article-section-NNN)` change immediately after the frontend is deployed. The existing decorative `⌖` icon, scrolling, focus handling, seven-second highlight, and link destination remain unchanged.

The display override applies only to fragments accepted by `parseArticleAnchor`. External links, ordinary page fragments, and malformed article-section fragments keep their original labels and behavior.

## Newly generated summaries

The default detailed-summary anchor instruction and its example use:

```markdown
[跳转原文](#article-section-NNN)
```

All existing anchor-count, grouping, order, and exception rules remain unchanged. Brief-summary output and summary-template selection remain unchanged.

## Data and compatibility

There is no database migration or production backfill. Existing Markdown remains valid and backward-compatible because the destination fragment is unchanged. If an older frontend renders a newly generated `[跳转原文]` link, it displays that stored text normally.

## Implementation boundary

- `frontend/src/components/SummaryMarkdown.tsx` owns the visible-label override for valid article anchors.
- `frontend/src/components/SummaryMarkdown.test.tsx` verifies old-label compatibility, new-label rendering, and isolation from other links.
- `backend/internal/ai/article_anchors.go` owns the default prompt label and example.
- `backend/internal/ai/summarizer_anchor_prompt_test.go` verifies the new prompt contract and rejects stale `查看原文` wording in the active instruction.

No CSS, API, repository, database, or migration change is required.

## Verification

Automated checks must prove:

- stored `[查看原文](#article-section-001)` renders visible text `跳转原文` with the existing `⌖` icon;
- stored `[跳转原文](#article-section-001)` renders only one `跳转原文` label and one icon;
- a valid article anchor with arbitrary Markdown label is normalized to `跳转原文`;
- external links, ordinary fragments, and malformed article anchors preserve their supplied text;
- the backend instruction and example contain `[跳转原文](#article-section-NNN)` and no active `查看原文` label;
- focused frontend and backend tests pass, followed by the full frontend tests, frontend build, and backend test suite.

Production verification should open an existing anchored summary without regenerating it and confirm that every valid anchor displays `跳转原文 ⌖` while still landing on its original body target.
