# Extension Adapter Upstream Map

Our `extension/adapters/twitter/*` files are **independent DOM scrapers**, not
derivatives of OpenCLI's adapters (which use Twitter's internal GraphQL API).
We track OpenCLI's adapter commits anyway because:

- OpenCLI's maintainers frequently update those files when x.com changes
- A burst of OpenCLI commits is a useful **signal** that x.com's API or DOM
  shifted — even though their fix lives at a different layer, ours will
  often need attention too
- We share their `TweetItem` output schema; if they add a field, we may
  want to mirror it

## Twitter

Last manual review: 2026-05-26

| rss-pal adapter | OpenCLI source (reference only) | Last reviewed |
|---|---|---|
| extension/adapters/twitter/list-tweets.js | clis/twitter/list-tweets.js | 2026-05-26 |
| extension/adapters/twitter/tweets.js | clis/twitter/tweets.js | 2026-05-26 |
| extension/adapters/twitter/bookmarks.js | clis/twitter/bookmarks.js | 2026-05-26 |

Run `scripts/check-upstream-adapters.sh` periodically to see if OpenCLI
churned these files since your last review. A "yes" doesn't mean port the
changes — it means manually QA rss-pal's adapter against x.com (in your
logged-in browser, visit a real list / profile / bookmarks page and confirm
items still extract) and bump the "Last reviewed" date.
