# Shortcut reader pending-list correction plan

**Goal:** Shortcut shares appear in the original article's existing pending list and wait for user confirmation.

**Architecture:** Private server drafts supplement the existing local scratchpad. The shortcut resolves a unique owned source; the reader merges remote drafts and confirms through the established batch-fetch endpoint. Remove automatic shortcut-job consumption.

**Tech stack:** Go/Gin, PostgreSQL/RLS, React/Vitest, native Apple Shortcuts.

1. Backend: failing integration test reproduces source article -> shortcut share -> pending draft API -> explicit batch fetch; cover ownership, exact URL boundaries, ambiguity, no-fetch and dedup. Add migration 059 and repository/API methods, wire reader routes and migration chain, disable automatic shortcut worker. Verify app-role access and after-commit responses.
2. Reader: preserve local manual drafts, merge remote links on focus/visibility/pageshow/visible polling, protect route switches and mutations against stale responses, clear remote URLs on remove and confirmed submit. Test the actual pending badge/dialog and that no automatic batch-fetch occurs.
3. Native: change success/error wording to reference the source article's pending list and explicit confirmation; publish a credential-free official Apple installer and update installation links. Keep paired credential file and request format compatible.
4. Review both spec compliance and code quality, run relevant backend/full frontend/build/deployment migration checks. Merge latest actual master, push, and use the established Tencent deployment.
5. Verify real server persistence and browser original-article queue with a synthetic account, then repair only the source-confirmed user incident with before export/guards/reversible hiding. Verify no fetch before Start Fetch and only the selected child after confirmation. Remove synthetic credentials/data.

The user's correction supplies the decision: source article pending confirmation, not background automatic saving. No extra product approval is needed.
