# Search highlights verification

Implementation preserves list payload size with a bounded `search_snippet`. Match navigation indexes only the current article; linked child summaries are explicitly excluded. Matching spans inline Markdown formatting without counting fragments as separate occurrences. Media placeholders remain untouched.

Code review found and fixed video-placeholder rendering, source-filter q retention, pinyin snippet offsets, read-card highlight opacity and linked-child count leakage. Targeted regressions cover these affected paths.

Browser checks used the production build with local API fixtures, not production article data. Desktop 1440x900 and mobile 390x844 screenshots inspected for paper/quiet/pearl/night/neon; no horizontal overflow. Ordinary/selected match contrast ranges: 5.85–7.83 and 6.91–9.58. Bottom toolbar is above mobile tab navigation; floating controls are above toolbar. Literal matches, inline fragments, 1/7 to 7/7 wrap, reading mode expanded summaries, and selected-hit scrolling verified.

Go full suite passes. Database-backed repository tests skip because local PostgreSQL is unavailable; this is not database integration proof. Frontend unit/integration suite: 120 files / 1065 tests pass; final targeted reader/search regressions: 96 tests pass; production build passes. Local legacy checks: 9 pass, Docker-dependent nginx parser test cannot run because local Docker is stopped; existing Tencent frontend container's actual `nginx -t` passes. Deployment verification is performed after integration through Deploy Tencent and direct host/public checks.
