# Article Anchor Round-Trip Design

## Goal

Turn AI-summary references into a compact, reversible preview interaction. A reader can jump from one summary reference to its article block and then return to the exact reference without adding browser-history entries or changing the URL hash.

## Interaction

- A valid summary reference remains a real anchor whose `href` targets `#article-section-NNN`.
- Its visible content is `跳转原文 ⌖`; the accessible name and tooltip remain `跳转原文`.
- Activating the anchor prevents the browser's default fragment navigation, records that exact source anchor, and uses the existing centered scroll and target highlight behavior.
- The active target shows one compact return anchor at its upper-right edge. Its visible content is only `↩⌖`, with accessible name and tooltip `跳回 AI 总结`.
- The return anchor has an `href` targeting the recorded summary source ID. Activating it also prevents default fragment navigation, scrolls back to that exact source anchor, and highlights the source for the same duration as the forward target highlight.
- A later forward jump replaces the prior round trip. Returning removes the target-side control.

Both controls are semantic anchors, but their click handlers perform scrolling directly. Therefore neither action changes `window.location.hash` nor pushes browser history.

## Implementation Boundary

Keep the behavior centralized around `SummaryMarkdown` and the existing strict `article-section` parser. Assign a stable per-render ID to each valid summary source anchor. A single active round-trip controller records the source element and target element, renders the target-side return anchor without altering stored Markdown, and invokes a source-return callback before cleanup. `SummaryMarkdown` uses that callback to reuse the existing highlight timer and animation instead of adding a second timing implementation.

The target-side control must work with every existing anchor shape, including normal Markdown blocks, tweet headers, and promoted video-player anchors. Position it visually at the target's upper-right without inserting invalid children into table-like Markdown elements.

External links, non-article fragments, malformed article anchors, and missing targets retain their current behavior.

## Accessibility and Motion

- Both anchors remain keyboard reachable.
- Mouse activation preserves the current focus behavior. Keyboard activation transfers focus to the destination temporarily when needed.
- Scrolling is smooth by default and instant when `prefers-reduced-motion: reduce` is active.
- The icon-only return control retains visible focus treatment, `aria-label`, and `title` attributes. The forward anchor keeps its visible label and icon together.

## Verification

Add focused frontend tests before implementation for:

- `跳转原文 ⌖` forward rendering with a strict article `href`;
- forward scrolling, highlight, and unchanged URL/history;
- return anchor rendering as `↩⌖` with a source-anchor `href`;
- exact return scrolling, matching-duration source highlight, and cleanup;
- replacement after consecutive forward jumps;
- missing or detached source/target safety;
- keyboard focus and reduced-motion behavior;
- unchanged behavior for external, malformed, and ordinary fragment links;
- compatibility with promoted video and tweet targets where applicable.

Run the focused tests, the complete frontend test suite, the production build, and `git diff --check` before completion.
