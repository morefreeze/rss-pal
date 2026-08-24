# Summary Anchor Highlight and Link Icon Design

## Goal

Improve the visibility and feedback of detailed-summary links that jump to an article section:

- keep the target article section highlighted for seven seconds;
- keep the existing `查看原文` text;
- add a small outlined crosshair icon at the upper-right of that text link.

## Scope

This change applies only to valid detailed-summary article-anchor links matching the existing `#article-section-NNN` contract. It does not change anchor generation, article Markdown, ordinary fragment links, external links, scrolling behavior, or keyboard-focus behavior.

## Link presentation

The existing `查看原文` text remains the single interactive link. A small outlined crosshair icon (`⌖`) is positioned at the upper-right of the link label.

The icon is decorative:

- it is hidden from assistive technology;
- it does not create a second tab stop or click target;
- clicking either the text or its icon uses the same existing anchor navigation;
- the combined link keeps a practical touch target on mobile.

Only links accepted by `parseArticleAnchor` receive this treatment. All other Markdown links retain their baseline rendering and behavior.

## Highlight timing

After a valid anchor link is activated, the target section scrolls to the center and receives the existing highlight class.

The CSS animation lasts 7 seconds:

- the first 35% of the animation (2.45 seconds) retains a clearly visible background and outline;
- the remaining time gradually fades both to transparent.

The JavaScript fallback cleanup runs at 7.1 seconds so it cannot remove the class before the animation completes. An `animationend` event may still clean up earlier when the browser reports normal completion.

Activating the same link again restarts the full seven-second highlight. An older timer must never clear a newer highlight.

With reduced motion enabled, scrolling remains instant and the highlight remains static until the 7.1-second cleanup; no animation is introduced.

## Implementation boundary

The change stays within the existing summary renderer and shared frontend styling:

- `frontend/src/components/SummaryMarkdown.tsx` owns valid-link rendering and cleanup timing;
- `frontend/src/index.css` owns icon placement and the seven-second animation;
- `frontend/src/components/SummaryMarkdown.test.tsx` verifies rendering and timer behavior.

No backend, database, API, prompt, or migration change is required.

## Verification

Automated verification must cover:

- a valid article-anchor link renders the text and decorative crosshair;
- the icon is absent from ordinary links and invalid article-anchor fragments;
- the highlight remains before seven seconds and is cleaned up by 7.1 seconds;
- repeated activation resets the cleanup timer;
- reduced-motion behavior remains accessible;
- the focused component tests and frontend build pass.

Production verification, if deployed, should open an article with anchored detailed summary, follow a `查看原文` link, and confirm both the icon presentation and the visible seven-second fade.
