# Public share attribution

Goal: Show “来自 XX 的分享” above the article title on long and short public links. XX is the share creator's current nickname, falling back to their login username. Existing links pick up the same attribution without recreating article snapshots.

Implementation: Query only the share creator's display name after resolving an active share. Add sharer_name to the public response without exposing account IDs, emails, identities or private profile fields. Render the name as React text, separate from article source metadata. No database migration.

- Add failing API and share-page tests for nickname, username fallback, existing links and both URL forms; update the public-field allowlist.
- Implement repository name lookup, shared public response and title-area attribution.
- Run share API tests with PostgreSQL, frontend checks/build and server build; review.
- Integrate into origin/master, push, deploy via Deploy Tencent and verify revision, public assets and health.

Validation: PostgreSQL share API regression passed; frontend 106 suites / 943 tests plus 9 legacy tests passed; production build and server package tests passed. Independent review found no blocking issues.
