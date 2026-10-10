# Formula extraction and seven-day repair

Goal: retain MathJax script expressions and raw delimited inline/block TeX in feed and page extraction, preserve numeric equations in the reader, and repair affected recently fetched subscription and Explore articles.

Implementation (inline execution; same conversion pipeline):
- Add regression tests covering NormalizeFeedContent and FetchContentFromReader, script display math, raw inline backslashes, numeric equations, currency, code and repeated extraction. Run RSS tests and observe failures.
- Preserve math/tex scripts during cleanup; protect delimited text nodes with existing sentinels before Markdown conversion; clone selections to avoid mutating inputs; keep frontend/backend dollar heuristics aligned. Run RSS and reader tests.
- Audit a fixed seven-day fetched_at window plus Explore 37518. Fetch original source HTML/feed for candidates; compare old/new conversions and only repair proven defects. Save before/after records, use ID+URL+old-content compare-and-swap; retain metadata and report unavailable sources/conflicts.
- Review diff independently, run full applicable Go/frontend checks, integrate latest origin/master, push master, wait for Tencent deployment and verify revision, health and rendered formulas.

No broad string replacement of stored TeX; missing formulas require source recovery. No unrelated updates or service restarts.
