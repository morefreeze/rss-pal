# Reddit scored exploration seeds

**Goal:** Discover original RSS sources from posts scoring at least 100 in programming, MachineLearning, LocalLLaMA and artificial.

**Architecture:** Replace the obsolete Reddit RSSHub seed with eight bounded Reddit JSON top listings (week/month, 100 posts each). A distinct reddit_top adapter enforces a configurable positive minimum score before creating candidates. Only qualifying original external links are used, with post identity and score retained as provenance. Existing safe RSS autodiscovery, validation and source merging remain in charge of admission. A qualifying post supplies sufficient public evidence without inventing repeated observations.

**Tech Stack:** Go, PostgreSQL migrations, existing worker registry and source fetcher.

- [ ] Add tests for 99/100/101 boundaries, missing scores, self posts, media/platform links, duplicate post IDs, unsafe URLs, malformed responses and custom positive threshold.
- [ ] Implement JSON listing parsing and seed migration; keep old RSS parser for backward compatibility but disable its obsolete seed. Configure score via provider endpoint fragment `#min_score=100`, stripped before HTTP transport, avoiding global settings and DB schema churn beyond provider kind.
- [ ] Preserve original link, subreddit, score and Reddit post URL as observation metadata; no automatic user subscription.
- [ ] Add read-only dry-run command that uses the exact parser and SourceFetcher, reports fetched/scored/external/unique/feed counts, errors and truncation explicitly, and can consume saved JSON fixtures for deterministic verification.
- [ ] Review and verify parser, confidence gate, migration and dry-run; test exact threshold boundaries before implementation.
- [ ] Trial the real eight listings, report independently failed listings rather than counting failures as zero results.
- [ ] Integrate current origin/master, push and use established Deploy Tencent workflow; verify revision, health and registered providers. If Reddit auth blocks trial, retain exact error evidence and request credentials location.

Decisions confirmed by user: mandatory >=100 score; weekly/monthly scanning; four subreddits; threshold configurable; actual trial required. Sampling bounded to top 100 per listing and identified in results.
