# Article Reading Progress Dual Bar

Date: 2026-07-06
Status: Approved for implementation approach
Scope: `frontend/src/pages/ArticlePage.tsx`, `frontend/src/index.css`

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

Show two separate visual meanings in the top progress bar:

- Light blue: historical farthest reading progress saved for this article.
- Dark blue: current viewport reading progress.

When the user scrolls beyond the historical farthest point, both bars advance.
When the user scrolls above the saved point, the dark blue current bar can move
left while the light blue historical bar stays at the farthest point.

## Chosen Approach

Keep the backend API unchanged and split the state only in the article page:

- `progress.scroll_position` and `maxScrollRef.current` remain the saved
  high-water mark used for persistence and read completion.
- Add a local current-scroll state derived from `window.scrollY` and the
  article's scrollable height.
- Render the fixed progress bar with two overlapping fills:
  - historical fill width from the high-water mark
  - current fill width from the current-scroll state

This preserves the existing monotonic persistence contract and limits the
change to frontend display behavior.

## Behavior Details

- On article load, initialize both bars from saved progress until the restored
  scroll position is measured.
- On every scroll, compute the current viewport fraction from current pixels.
- If the current fraction is greater than the high-water mark, update
  `maxScrollRef.current`, local `progress`, and schedule the existing progress
  flush as today.
- If the current fraction is less than or equal to the high-water mark, update
  only the current-scroll state; do not persist a lower progress.
- The metadata text "reading progress" should use the current viewport percent
  so it matches where the user is now.
- Mark-as-read sets both current and historical display to 100%.
- Mark-as-unread resets both display values to 0%.
- Content height changes may rescale the historical high-water mark as the
  current code already does, but the current viewport bar is re-measured from
  current pixels instead of copied from the historical value.

## Styling

Use the existing fixed top 4px progress track. Add:

- a light-blue historical layer
- a dark-blue current layer above it

Both layers retain the existing width transition. The AI summary marker remains
above the fills.

## Testing And Verification

The frontend currently has no configured unit test script. Verification will
therefore be:

- type/build check with `npm run build` in `frontend`
- code-path review against these cases:
  - reload article with saved progress and scroll upward
  - scroll beyond saved progress
  - mark read
  - mark unread
  - content height changes after image/layout settling

No backend migration or API test is required because the saved data contract is
unchanged.
