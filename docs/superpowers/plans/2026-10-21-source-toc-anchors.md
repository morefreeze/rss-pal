# Source TOC anchors

Goal: keep source article table-of-contents links inside the RSS Pal reader when their heading target can be resolved; keep the source URL as fallback.

Design: a remark pass after canonical article IDs matches source fragments to heading slugs. Match absolute URLs only against the original article origin, path and query. Pass the original URL through normal, focus, Explore and public readers. Keep stored Markdown unchanged. Scope scroll targets to the clicked reader so simultaneous renders cannot cross-navigate. Preserve canonical AI-summary IDs.

Plan:
1. Reproduce with article 3283 stored content and a failing renderer test.
2. Add source heading matching and local scroll behavior; cover external/unresolved links, encoded fragments, duplicate headings and read-only rendering.
3. Run frontend suite and production build, review diff independently.
4. Integrate into master without unrelated workspace changes, push and verify established Tencent workflow, runtime revision/health and browser click behavior.
