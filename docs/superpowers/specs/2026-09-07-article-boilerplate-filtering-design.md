# Article boilerplate filtering and selective Tencent builds

## Problem

Article 2691 was fetched from Simon Willison's weblog. Its real article is
inside `.entry.entryPage`, while recent posts, newsletter promotion, tags,
previous/next navigation, and the 2002–2026 archive links are sibling page
chrome. The extractor recognizes none of that site's content selectors and
falls back to `body`, so the complete page is stored as article Markdown.

Separately, Tencent auto-deploy currently runs `docker compose up -d --build`
for every runtime change. A backend-only change therefore rebuilds the
unmodified `status-monitor` image and pulls its `python:3.12-alpine` base even
though status monitoring is still an active production feature.

## Approved design

### Content boundary

Use DOM structure, not year-text matching:

1. Add conventional blog entry roots (`.entryPage` and `.entry`) before the
   broad `body` fallback in both direct RSS extraction and bookmarklet capture.
2. Expand page-chrome removal to recognizable class/id tokens for footer,
   social/share, related/recent posts, archive navigation, pagination, and
   newsletter promotion. Never remove the selected top-level document or
   article root itself.
3. Keep existing article text, dates, inline links, reference lists, and any
   four-digit year that occurs inside the selected content root. There is no
   regex that deletes years from Markdown.

The content-root boundary is the primary defense. Noise selectors are a
secondary defense for sites that place boilerplate inside otherwise valid
article wrappers.

### Deployment selection

Keep `status-monitor` and its Python image. Map changed paths to Compose
services:

- `backend/**` -> `api worker`
- `frontend/**`, nginx config, or certificates -> `frontend`
- `status-monitor/**` -> `status-monitor`
- Compose-file changes -> all services

Pass the selected service list to Compose for both deployment and rollback.
Continue running the migration dependency and checking all runtime services
after backend deployments. This keeps the existing rollback and health gates
while avoiding unrelated image builds.

## Existing data

Before changing production article 2691, export its current row to a dated
backup. Re-fetch the original URL with the deployed extractor and update only
that article's content and metrics. Do not bulk rewrite historical articles.

## Verification

- A Simon-style fixture keeps the entry text and removes recent articles,
  newsletter/footer, and archive years.
- A fixture with prose containing years and a legitimate reference list keeps
  both.
- Bookmarklet extraction observes the same root-selection behavior.
- Shell tests prove backend-only changes select `api worker`, status-monitor
  changes select only `status-monitor`, and Compose changes select all.
- Run the full backend suite, deployment-script tests, and shell syntax checks.
- After pushing master, verify the GitHub deployment, exact Tencent revision,
  all containers, direct/public health, and article 2691's stored/public tail.

