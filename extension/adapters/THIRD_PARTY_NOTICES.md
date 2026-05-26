# Third-Party Notices for extension/adapters

The rss-pal Chrome extension's `extension/adapters/twitter/*` files are
**independent DOM scrapers** developed for this project. They are NOT derived
from any third-party source code.

## OpenCLI reference

We took the following non-code inspiration from
**OpenCLI** (https://github.com/jackwener/opencli, Apache License 2.0):

- The `extension/adapters/<site>/<command>.js` directory layout
- The cli/adapter registry pattern (one file = one adapter, register on load)
- The `TweetItem` output schema (field names like `id`, `author`, `display_name`,
  `text`, `created_at`, `url`, `media_urls`, `quoted_url`, `likes`, `retweets`,
  `replies`, `views`) — chosen so a future user could pipe rss-pal extension
  output through OpenCLI tooling or vice versa

We did NOT copy OpenCLI's extraction logic. OpenCLI's twitter adapters
authenticate to Twitter's internal GraphQL API (`/i/api/graphql/<queryId>/...`)
using the `ct0` CSRF cookie and a hardcoded Bearer token (`Strategy.COOKIE`).
That approach is incompatible with a Manifest V3 content script: we can read
the rendered DOM but not the Bearer token (which would require main-world
script injection that we explicitly want to avoid). So our adapters scrape
the rendered DOM independently.

If you adapt OpenCLI code into this codebase in the future, you must:

1. Include the upstream commit hash and file path in the borrowing file's
   header
2. Preserve the Apache-2.0 NOTICE for that snippet
3. Update this file with the specific borrowing

For now, no third-party source code is included.
