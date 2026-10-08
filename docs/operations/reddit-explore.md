# Reddit exploration seeds

The worker scans `programming`, `MachineLearning`, `LocalLLaMA`, and `artificial` every six hours. Each has a weekly and monthly top listing, limited to the first 100 posts. These are bounded samples, not a complete Reddit archive.

`reddit_top` requires Reddit's numeric `score >= 100`. Missing scores fail closed. Self posts, videos, galleries, NSFW/promoted posts, Reddit links, common media/repository/paper platforms and unsafe URLs are excluded. Remaining original article URLs enter the existing RSS discovery, recent-content and feed validation pipeline; an external URL is not proof of a personal blog or usable feed. No user subscription is created.

Repeated post IDs are counted once. Multiple qualifying posts pointing to the same article retain the highest-score post's provenance and a real occurrence count. The feed registry subsequently merges canonical RSS sources. A single qualifying post supplies confidence; no fake repeat count is used. Source observations retain the representative Reddit post URL, score and subreddit in their public tags.

The threshold is per provider: the endpoint fragment `#min_score=100` is local adapter configuration and is not sent to Reddit. Only positive integers are accepted. Operators changing the threshold should reset that provider's conditional-fetch validators and schedule it for a fresh sync. The old unscored `reddit-programming` RSSHub seed is disabled; the XML adapter remains for compatibility.

## Read-only trial

From `backend/`:

```sh
go run ./cmd/reddit-explore-trial -min-score 100 -save-dir /tmp/reddit-listings > /tmp/reddit-trial.json
go run ./cmd/reddit-explore-trial -min-score 100 -input-dir /tmp/reddit-listings > /tmp/reddit-replay.json
```

The command does not connect to the database. It uses the production provider client, parser and RSS source validator. It reports per-listing errors and pagination availability, distinct qualifying post IDs, eligible external post IDs, external hosts and verified feed URLs. Distinct counts combine only successfully fetched listings; `successful_listings < 8` makes the run partial and exits nonzero. Zero counters in a wholly failed run do **not** mean no qualifying posts exist. `-validate=false` explicitly skips feed validation.

## Trial on 2026-10-08 (Asia/Shanghai)

- Local production client: 0/8 listings fetched; all returned HTTP 403.
- Tencent worker network: 0/8 listings fetched; all eight timed out after the provider client's 20-second deadline.
- Independent Tencent RSSHub probe: original route returned HTTP 404; direct Reddit JSON through its existing outbound proxy returned HTTP 403.
- OpenCLI probe could not connect to its browser extension; the agent-browser fallback displayed Reddit's network-security block and requested a login or developer token.
- Consequently, the number of posts/sites/feeds discoverable at 100 points is **unknown**, not zero. The user confirmed no Reddit API authorization is currently available.
- Synthetic replay (explicitly not live data): all eight fixture listings processed; scores 99/100/101 yielded two distinct qualifying external posts/sites after cross-list deduplication.

Reddit may require approved API access. Credentials are not embedded, copied from browser sessions or sent to unrelated hosts. Fetch failures remain visible in provider state and use the existing exponential backoff; no fallback silently removes the score requirement.
