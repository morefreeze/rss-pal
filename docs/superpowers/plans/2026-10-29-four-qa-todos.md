# Close the four evidenced QA TODOs

The user requested repair, deployment, live acceptance, and checkbox/count updates for ISSUE-007, IMG502, ISSUE-013, and ISSUE-014. The existing test account is the only account whose article data may be changed. The current isolated worktree starts at `924c136d`; the dirty primary checkout must remain untouched.

## Evidence and design

- ISSUE-007: stored article 3419 and 3421 bodies are each exactly 50,003 bytes and end in an ellipsis mid-sentence/formula. The current fetcher no longer applies the old 50KB persistence cap. Repair these historical rows through the ordinary article refetch UI, then regenerate the affected summaries. Verify full tails, references, and reload persistence against their original sources.
- ISSUE-013: preserve what an experiment measures. GPT3.5/human labels disagree on nearly half the samples, while classifiers trained on those labels achieve comparable accuracy. A summary must not transfer the downstream classifier result to annotator accuracy or omit the disagreement that qualifies it.
- ISSUE-014: preserve the degree and kind of a claim. Possibility must not become certainty; diminished UI importance must not become disappearance. Grouped highlights must preserve the separate conclusions and agree with detailed text.
- IMG502: earlier captures show three image requests timing out at about 30 seconds. Trace the deployed proxy request, validation, response headers, and full body before selecting a minimal fix. Keep checked-IP SSRF/redirect protection, bounded size, complete-body-before-cache behavior, proxy privacy, and cancellation. Do not silently fall back to loading source images in the browser.

## Tasks and verification

1. Add failing request-level summary regression tests for measured-object/comparison boundaries and modal/conclusion strength. Update the common generation guidance, covering text/stream/vision and local generation without article-specific rewrites or added model calls. Run the AI tests; obtain independent specification and quality review.
2. Diagnose IMG502 in the actual Tencent API environment. Add failing tests for the demonstrated transport/failure behavior, implement a bounded fix, retain existing proxy and HTTP safety tests, and review the resulting request path.
3. Run appropriate backend/frontend checks for the final changes. Integrate current remote mainline, push the actual mainline, deploy using the established Tencent bundle procedure, and verify the exact revision plus API/worker health.
4. With the existing authenticated test browser, refetch 3419/3421 and regenerate only the required summaries (3419, 3421, 3402), waiting for each in-flight operation. Compare actual output, source tails, semantic boundaries, images, desktop/mobile rendering, and persistence. Keep acceptance evidence under the local QA repair directory. Prompt assertions alone do not establish model correctness.
5. Fresh-read the existing private Page and local state, preserve user edits and old attachments, and check off only items with acceptance evidence. Update the shared Page, local TODO/state/registry, and exact pending/completed counts. Report commit, deployment, acceptance, and any real remaining limitation.

Implementation uses one implementer at a time, with specification review before quality review. Independent runtime diagnosis may proceed while summary work is isolated to `backend/internal/ai/summary_quality*`. Plans and reviews are internal working records; no new workflow approval is required.

## Image diagnosis and selected repair

The diagnostic built from the current `httpx` path ran inside the deployed API container with its existing proxy environment. A 30s attempt cut image 01 after 117,711 of 1,170,068 bytes and timed out image 03 before headers. A traced attempt with only its diagnostic budget extended completed all three files: 31.2s, 32.2s, and 40.3s. DNS, CONNECT and TLS completed within roughly one second. Image 03 alone spent 39.4s transferring its 4,195,646-byte body. This reproduces a complete, permitted image taking longer than the current total deadline; it is not evidence of a broken source file or an SSRF validation failure.

Use a bounded 90s image-only total budget and 120s image-only read timeouts at both nginx hops, including the short host. Leave general API/media budgets and full-body/cache/SSRF behavior intact. A real delayed-body integration regression crosses the old 30s limit and fails as 502 before the fix; cancellation and incomplete/oversized response tests remain required. The host nginx source template must be synchronized to its active deployment file with a successful syntax check and narrow reload as part of deployment; the normal container deploy does not install that host file.

Raw diagnostic records and red/green logs are private local evidence under `/Users/bytedance/.codex/qa/rss-pal/2026-10-29/fix-todo-four/` and contain no credentials.

## Acceptance follow-ups

The first deployed image acceptance completed three cold requests in about 9, 46 and 64 seconds, then verified complete cached reloads and a second cold mobile session. This confirmed that both the former 30s application deadline and 60s nginx deadline could reject a valid image. IMG502 was accepted and checked off with accessible evidence; the other three items remained pending.

The first real regenerated 3419 summary restored mitigation coverage but failed semantic review: it dropped one conjunct from a measured event and strengthened a negated cross-environment detector result. The shared generation guard was extended to bind numbers to their complete event, population, denominator and quantifier scope, with compression dropping secondary numbers before material qualifications. Selected methods in the default detailed summary must briefly explain their mechanisms and source-stated limits. No additional model calls or output-budget changes were introduced by the implementation.

Two completed 3421 UI refetches exhausted the existing 45s caller deadline while preserving the old article. Direct extraction and the multi-request reader stage could consume the entire budget before the final fallback. Reserve a recomputed share of any finite remaining deadline for each remaining route; count an absent reader correctly and bound its whole MCP lifecycle. No-deadline callers retain their existing client limits. Virtual-time regressions cover the exact 45s failure, a 75s multi-request boundary, fast failures, ample/no deadline, cancellation and fresh-cache bypass. The API and nginx content deadlines remain unchanged. Source and summary acceptance must be rerun after deploying these follow-ups.
