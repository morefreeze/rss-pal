# Known reading and summary defects

## Outcome

Repair confirmed QA issues 003–010 without changing unrelated reader behavior, personal-key privacy, automatic generation budgets, or public data ownership. Preserve the already deployed dark-code fix. The old checkout contains unrelated work; all changes use the managed `codex/qa-known-fixes-1028` worktree based on `13ae23d`.

## Evidence and scope

- Source snapshots for articles 3347, 3414 and 3415 end at about 50,000 bytes. Direct and Reader normalization impose that byte limit. Store complete extracted articles; bound model input separately. Transport limits must fail explicitly on overflow instead of persisting a successful prefix.
- `$6N$` is classified as currency. A rejected raw-math pair is scanned again from its closing delimiter, corrupting the following `\\approx` formula. Correct backend conversion and the corresponding frontend dollar heuristic while preserving prices and code.
- Wide KaTeX blocks enlarge the mobile document. Constrain math locally, preserve both ends and native keyboard access, MathML, short inline baseline and dark syntax colors.
- Summaries omit meaning-changing conditions in advice, conditional steps, shared-prompt evaluation and benchmark tiers. Strengthen the shared grounding contract, explicitly prioritize conditions over compression/de-duplication, and close the independent 8,000-character browser input truncation. Retain length budgets, routing, completion checks and browser-only personal keys. Do not add extra automatic model calls or broad historical regeneration.
- Explore admits far-future publication timestamps, then substitutes fetched time during ranking. Reject implausibly future-dated items at ingestion/ranking and filter cached rows before per-source limits. Preserve modest clock skew, absent dates and legitimate text mentioning future events. Do not keyword-ban “demo”.
- Inspect image proxy completeness separately: avoid caching partial bytes as a successful immutable image if the upstream stream fails. Do not add speculative retry loops.

## Acceptance

Write regression tests that fail on the baseline, then verify focused and full suites. Use real browser layout at 390×844 and desktop on stored public-source math excerpts and affected live pages. After integration and established Tencent deployment, verify the exact revision and services, repair affected test-account bodies through normal content refresh where appropriate, and regenerate only a bounded affected set through the normal UI. Compare the actual resulting summaries with original conditions; prompt tests alone do not establish semantic quality. Keep before/after evidence locally with mode 600 and report any remaining uncertainty honestly.

## Boundaries

No blanket historical rewrite, public share creation, account re-registration, production schema changes, global Explore deletes, exposed credentials or unrelated service restarts. If normal refresh does not update derived fields safely, use a reviewed, scoped CAS repair with a private backup rather than unguarded SQL.
