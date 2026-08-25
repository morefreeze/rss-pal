# Interest Rename Design

## Goal

Replace the user-facing and code-level `insight` concept with `interest`, remove
the recommendation page from routing and navigation while retaining its source
code, and preserve compatibility for existing interest URLs and API clients.

## Scope

### Frontend

- Rename `InsightsPage` and its file to `InterestsPage`.
- Rename frontend insight-specific types, functions, variables, comments, and
  tests to interest terminology.
- Serve the page at `/interests`.
- Redirect the legacy `/insights` route to `/interests`.
- Change the desktop and mobile navigation label from `洞察` to `兴趣` and point
  it at `/interests`.
- Remove the `推荐` navigation items and stop registering `/recommended`.
- Retain `RecommendedPage.tsx`, its client functions, and all recommendation
  backend code for possible reuse.
- Update document titles and article return-navigation state to use
  `/interests`.
- Use natural Chinese copy: the page is `兴趣`, generated content is `兴趣分析`,
  and insufficient data is described as an incomplete `兴趣画像`.

### Backend

- Rename insight-specific Go files, handlers, repositories, model types, AI
  prompt/parser functions, worker functions, logs, comments, and tests to
  interest terminology.
- Register the canonical endpoints as:
  - `GET /api/interests/latest`
  - `POST /api/interests/generate`
- Keep `GET /api/insights/latest` and `POST /api/insights/generate` as temporary
  compatibility aliases backed by the same internal interest implementation.
  The canonical latest response uses the `interest` field; the legacy latest
  response keeps the `insight` field so an already-open old frontend bundle
  remains functional during deployment.
- Rename the worker development hook from `INSIGHTS_RUN_NOW` to
  `INTERESTS_RUN_NOW`, while accepting the old variable as a temporary alias so
  existing operational commands do not silently stop working.

### Persistence Boundary

The database schema does not change. The repository continues to read and
write the existing `user_insights` table and its existing columns. Historical
migrations are immutable and keep their current filenames and SQL identifiers.
No production data migration is required.

## Compatibility

- Browser bookmarks for `/insights` continue to work through a replace-style
  redirect to `/interests`.
- Old frontend bundles and API clients continue to work through the legacy
  `/api/insights/*` aliases.
- `/recommended` is intentionally not given a compatibility redirect. Once its
  explicit route is removed, the existing application wildcard sends it to the
  standard article list.
- Stored article-entry paths written by the new frontend use `/interests`.

## Architecture

This is a terminology and routing change, not a merge of recommendation and
interest-generation behavior. The deterministic recommendation repositories
and `RecommendedPage` remain independent. Interest generation keeps its
existing async lifecycle, quota handling, AI validation, and persistence
semantics; only code-level names, routes, and user copy change.

The canonical interest handlers own the implementation. Canonical and legacy
routes must call the same internal query/response builder rather than duplicate
business logic; only the compatibility response field name may differ.

## Testing

Implementation follows red-green-refactor:

1. Add or update frontend tests that fail until `/interests` has the correct
   page title and the old `/insights` route redirects.
2. Add navigation assertions that `兴趣` points to `/interests` and no `推荐`
   item is exposed on desktop or mobile.
3. Add backend route tests that require both canonical interest endpoints and
   legacy insight aliases to reach the same handlers.
4. Rename and update existing AI parser, prompt, repository, handler, and worker
   tests without weakening their behavioral assertions.
5. Run the complete frontend test suites and production build.
6. Run the complete backend Go test suite.
7. Search active source code for remaining insight terminology. Allow only the
   documented compatibility route strings and persistence identifiers such as
   `user_insights` and historical migrations.

## Non-goals

- Renaming the `user_insights` table or editing historical migrations.
- Changing the interest-generation algorithm, quotas, scheduling, prompts, or
  recommendation ranking behavior beyond terminology.
- Deleting recommendation implementation code.
- Deploying the change to production in this task unless separately requested.
