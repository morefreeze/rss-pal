# Article source and provenance implementation plan

**Goal:** Extracted links show their original website as source and link back to the article that introduced them.

**Design:** Keep feed_id for ownership, subscriptions and fetch scheduling. Resolve a separate source from each child article's own URL, using parent_article_id as durable extraction provenance. Apply the same resolver to old and newly fetched children, list/detail APIs, tags, recommendations and new public snapshots. Expose the parent title on authorized article details; do not expose private parent metadata in public shares.

**Execution:** Inline execution because source resolution and the corresponding UI form one coupled change. Use test-driven-development, review and verification-before-completion.

- [x] Add failing source/insert-read/provenance tests and frontend interaction tests. Run against an isolated PostgreSQL database.
- [x] Add shared source resolution, project it in repositories and lean list responses, correct source tags and public snapshots, and return parent title on authorized details.
- [x] Add a reusable source link and a provenance link. Update article cards, details, search/grouped views and recommendations to consume the source separately from feed attribution.
- [ ] Run Go tests, frontend tests/build and review the complete diff. Incorporate current origin/master, push mainline and follow Deploy Tencent.
- [ ] Verify deployed revision, service health and live article 3249 source, tag and parent metadata. Verify rendered source/provenance and navigation where an authorized browser session is available.

Local validation: full model/repository/API/server/worker test packages passed against isolated PostgreSQL; 837 frontend tests and nine legacy checks passed; production build passed. Independent review found no outstanding issues. The initial disk-backed test database exhausted local Docker disk; the complete rerun passed using an isolated tmpfs database.
