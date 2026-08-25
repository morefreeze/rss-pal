# Article List Briefing Panel Design

## Goal

Replace the `/articles` page's `为你推荐` panel with a `简报` panel that
shows the current daily briefing and is collapsed by default.

## Behavior

- Fetch the current daily briefing through the existing `getDailyDigest` API.
- Show the panel only in the same list context as the current recommendation
  panel: normal article mode, without search or grouped view.
- Reuse the current `rec-panel`, header, arrow, row, count, hover, keyboard, and
  article-navigation presentation.
- Label the panel `简报` and show the number of briefing articles.
- Reflect each article's real per-user read state. The daily-digest API already
  returns `is_read` from `reading_progress`; combine that value with the
  article list's existing session read-ID set through the existing `isRead`
  helper, then use the same unread dot and reduced opacity as normal article
  rows. Returning from an article therefore updates the briefing row without
  inventing a second read-state source.
- Use a new `showBriefing` preference whose initial value is false, so an old
  expanded `showRecommended` preference cannot expand the replacement panel.
- Preserve the current article-entry path when a briefing row opens an article,
  so Back returns to the filtered article list.
- If the briefing request fails or contains no articles, omit the panel, matching
  the current recommendation panel's unobtrusive failure behavior.

## Removed Behavior

- Stop fetching `/articles/recommended` from the article list page.
- Remove recommendation boost/dampen controls and their page-local state.
- Do not change the separate interest-page recommendation card.

## Testing

- Add a focused ArticleListPage test proving the panel is titled `简报`, uses
  daily briefing data, starts collapsed, and expands on click.
- Cover both unread and read briefing rows, including a read ID recorded in the
  current browser session, so their visual state follows the regular list.
- Assert that the old `为你推荐` label and recommendation request are absent.
- Run the focused test, the full frontend test suite, and the production build.

## Out of Scope

- Weekly or last-selected briefing selection inside the article list.
- Rendering the briefing intro or calendar controls.
- Backend or API contract changes.
