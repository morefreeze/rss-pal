# Feed source identities implementation plan

**Goal:** Repair the seven confirmed legacy sources and show readable names, types and source URIs on feed health.
**Architecture:** Add URL/type to the health response, use the existing feed metadata for title fallback, and render both in the table. A guarded, transactional operator SQL script repairs only the confirmed owner-1 rows; it removes the empty duplicate clip and preserves feed 27. Existing URL validation prevents recurrence.
**Tech stack:** Go, PostgreSQL, React, Vitest.

- [ ] Add failing HTTP coverage for empty/whitespace/null titles, metadata and owner isolation; component coverage for names, source schemes and types; repair-script coverage for exact changes, second-run safety and refusal when duplicate has articles.
- [ ] Implement health metadata/display and guarded repair SQL. Correct X URIs/types/provider IDs, name the single newsletter page and mark it html. Delete only the empty malformed clip duplicate after confirming the canonical clip exists. Preserve all unrelated rows.
- [ ] Run backend tests against local PostgreSQL, frontend tests and build; review diff and integrate current origin/master.
- [ ] Back up affected production rows on the production host. Apply the reviewed repair in a transaction, validate canonical clip/article counts and X identities. Push master, wait for Tencent deployment and verify exact revision, containers, health, authenticated response and browser rendering.
