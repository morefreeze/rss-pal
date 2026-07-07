# Article Reading Progress Dual Bar

Date: 2026-07-06
Status: Approved for implementation approach
Scope: `frontend/src/pages/ArticlePage.tsx`, `frontend/src/index.css`

## Current Scope

The current implementation uses two top progress layers:

- Light blue: the farthest reading progress saved for the article on the
  server.
- Dark blue: the user's current viewport progress in this article view.

The AI marker and confetti path are intentionally not mounted.

## Problem

When a user re-enters an article with saved reading progress, the fixed top
progress bar can move backward and forward while scrolling. The underlying
saved progress is intended to be monotonic, but the UI currently uses the same
`progress.scroll_position` value for two meanings:

- the historical farthest saved reading position
- the current viewport reading position

`ResizeObserver` also rescales `progress.scroll_position` when content height
changes, so the value displayed in the top bar can change even when the user's
current viewport position is different from the saved farthest position.

## Goal

Show two visual meanings in the top progress bar:

- Light blue: historical farthest reading progress saved for this article on
  the server.
- Dark blue: current viewport reading progress for this article view.

When the user scrolls beyond the historical farthest point, the light-blue bar
advances and the existing progress API persists the new high-water mark. When
the user scrolls above the saved point, the light-blue bar stays at the farthest
point while the dark-blue bar follows the actual scroll position.

## Chosen Approach

Keep the backend API unchanged and split the state only in the article page:

- `progress.scroll_position` and `maxScrollRef.current` remain the saved
  high-water mark used for persistence and read completion.
- A local current-scroll state derived from `window.scrollY` drives the dark
  blue current-position layer and metadata text.
- Render the top bar through a dedicated progress-bar component so the UI no
  longer depends on legacy inline progress DOM.
- Render the fixed progress bar with two fills:
  - historical width from the server-backed high-water mark
  - current width from the current viewport progress

This preserves the existing monotonic persistence contract and limits the
change to frontend display behavior.

## Behavior Details

- On article load, initialize both layers from saved server progress until the
  restored scroll position is measured.
- On every scroll, compute the current viewport fraction from current pixels.
- If the current fraction is greater than the high-water mark, update
  `maxScrollRef.current`, local `progress`, and schedule the existing progress
  flush as today.
- The historical display uses the maximum of the latest server-backed progress
  and the local high-water ref. This prevents an older in-flight save response
  from briefly pulling the light-blue bar backward.
- If the current fraction is less than or equal to the high-water mark, update
  only the current-position layer; do not persist a lower progress.
- The metadata text "reading progress" should use the current viewport percent
  so it matches where the user is now.
- Mark-as-read sets both display layers to 100%.
- Mark-as-unread resets both display layers to 0%.
- Content height changes never rescale the historical high-water mark. They
  only re-measure local current viewport display from current pixels.

## Styling

Use the existing fixed top 4px progress track with a light-blue historical layer
and a dark-blue current-position layer above it. The AI summary marker must not
be rendered by `ArticleProgressBar`.

## Testing And Verification

The frontend currently has no configured unit test script. Verification will
therefore be:

- type/build check with `npm run build` in `frontend`
- static regression check with
  `node frontend/test/articleProgressBar.test.cjs`
- code-path review against these cases:
  - reload article with saved progress and scroll upward
  - scroll beyond saved progress
  - mark read
  - mark unread
  - content height changes after image/layout settling without historical
    progress jumping

No backend migration or API test is required because the saved data contract is
unchanged.
