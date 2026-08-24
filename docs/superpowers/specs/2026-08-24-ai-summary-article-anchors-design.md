# AI Summary Article Anchors Design

**Date:** 2026-08-24

**Status:** Approved design, awaiting written-spec review

## Goal

Let newly generated detailed AI summaries link selected summary groups back to the corresponding position in the article body. The links should help navigation without adding a jump to every summary paragraph.

Existing stored summaries are unchanged. Brief summaries never contain article-anchor links.

## User Experience

- Every rendered, addressable article block receives a stable local anchor.
- Detailed summaries follow the source article's order where practical.
- A summary paragraph that combines a meaningful group of adjacent source ideas may end with one `查看原文` link.
- The model must not add a link to every summary paragraph.
- A short article or an article devoted to one continuous idea may contain no summary anchor links.
- Clicking a valid summary anchor scrolls to the matching article block on the current page and briefly highlights it.
- Ordinary external links keep their current behavior.
- A malformed or missing article anchor must not break the page; it remains inert rather than navigating away.

## Scope

This change applies to:

- default detailed-summary generation;
- streaming and non-streaming summary generation;
- image-assisted detailed summaries;
- custom detailed-summary templates, by appending a system-owned anchor instruction after the user's template;
- article-body rendering and detailed-summary rendering on the article page.

This change does not:

- regenerate historical summaries;
- add anchors to brief summaries, article cards, daily digests, or weekly digests;
- rewrite or persist anchor markers in stored article Markdown;
- change external-link behavior;
- make anchor links useful on summary-only surfaces that do not render the article body.

## Architecture

### 1. Canonical anchor model

The backend defines the canonical ordered list of addressable source blocks from the article Markdown. Addressable blocks include headings, paragraphs, and list items with meaningful textual content. A Markdown link is represented by the addressable block that contains it, so linked text and its surrounding statement lead to the same readable location rather than creating tiny inline scroll targets.

Blocks receive sequential identifiers using the format `article-section-NNN`, starting at `article-section-001`. Empty blocks, image-only blocks, fenced-code internals, and structural separators do not consume an identifier. A link-only paragraph remains addressable because it contains meaningful link text.

The frontend applies the same block-selection and ordering contract while rendering the unchanged Markdown. Backend and frontend implementations are protected by shared fixture cases that assert the exact ID sequence for headings, paragraphs, lists, inline links, link-only paragraphs, images, code fences, and blank content.

Sequential IDs are stable for a given stored article body. If the article body is later replaced and the user regenerates its summary, both the body anchors and the new summary are generated from the new content.

### 2. Prompt preparation

Before a detailed-summary request, the backend prepares an AI-only copy of the truncated article content. Each addressable block is preceded by an unambiguous anchor marker that names its canonical ID. These markers are never saved to the article and never shown in the reader.

The detailed-summary instruction tells the model to:

1. summarize in source order where practical;
2. merge adjacent source blocks by shared meaning;
3. add at most one `[查看原文](#article-section-NNN)` link to a summary paragraph when it helps the reader locate supporting detail;
4. use only anchor IDs present in the supplied article;
5. avoid links on every paragraph;
6. omit anchor links for a short or single-theme article;
7. emit no anchor marker syntax except valid Markdown links.

This system-owned instruction is appended to custom detailed templates after `{title}` and `{content}` replacement. It is not added to brief templates.

Anchor annotation happens after the existing content-length limit is applied, ensuring the model can only cite blocks it actually receives. Marker text is excluded from the content-length budget.

### 3. Rendering and navigation

The article renderer assigns the canonical `id` to each addressable block. IDs are applied directly to headings, paragraphs, and list items without inserting visible marker elements or changing stored Markdown.

The detailed-summary renderer recognizes only same-document hrefs matching `#article-section-NNN` as article navigation links. On the article page it:

- prevents a new-tab navigation;
- confirms the target exists;
- scrolls the target into view with the current page's preferred smooth-scroll behavior;
- moves focus only when needed for keyboard accessibility;
- applies a temporary highlight class that respects reduced-motion preferences.

If the ID is syntactically valid but absent, the click is ignored. All other links preserve the existing external-link behavior. Summary components on pages without an article body may render the anchor text but must not redirect to a nonexistent target.

## Components and Responsibilities

- `backend/internal/ai`: select addressable Markdown blocks, annotate detailed-summary input, and append the anchor-specific prompt contract to every detailed-summary path.
- Backend tests: verify annotation order, truncation boundaries, prompt behavior, and that brief prompts remain unchanged.
- `frontend/src/components/MarkdownArticle.tsx` or a focused helper: assign matching IDs to rendered article blocks.
- `frontend/src/components/SummaryMarkdown.tsx`: distinguish article anchors from external links and invoke local navigation safely.
- Frontend tests: verify block IDs, valid navigation, missing-target behavior, external links, and no special handling for unrelated fragments.
- Article-page styles: provide target highlight and reduced-motion behavior.

## Failure Handling

- AI output is treated as untrusted Markdown. Only the strict `article-section-NNN` fragment pattern receives special behavior.
- The frontend checks the DOM target before scrolling.
- No summary-generation request fails solely because an article has no addressable blocks; it falls back to the existing detailed-summary prompt without link expectations.
- If backend and frontend anchor selection ever drift, links degrade to inert summary links rather than opening an external location or crashing rendering.

## Testing and Acceptance

### Backend

- A multi-section Markdown fixture is annotated with the expected sequential anchors.
- Inline and link-only Markdown links resolve to their containing block anchors.
- Images, blank blocks, separators, and fenced-code internals do not shift the sequence unexpectedly.
- Default, streaming, image-assisted, and custom detailed prompts include the anchor contract.
- Brief prompts contain neither anchor markers nor anchor instructions.
- Truncated-away content cannot produce an advertised anchor.
- Content with no addressable blocks still summarizes normally.

### Frontend

- The same fixtures render the same anchor IDs in the same order.
- A valid detailed-summary anchor scrolls to and highlights its target without opening a new tab.
- A missing target is ignored safely.
- External links continue to open according to existing behavior.
- Reduced-motion mode avoids animated scrolling/highlighting.

### End-to-end acceptance

For a newly generated multi-topic article, the detailed summary contains a small number of grouped `查看原文` links in source order, and each link lands on the intended body section. For a short single-topic article, a link-free detailed summary is accepted. Existing summaries and stored article Markdown remain unchanged.

## Release Notes

No database migration or historical backfill is required. Deploying the backend and frontend together is preferred because summaries generated by the new backend rely on anchors rendered by the new frontend. A frontend-first rollout is safe; a backend-first rollout temporarily leaves new fragment links without targets.
