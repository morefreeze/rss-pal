# Article boilerplate filtering implementation plan

**Goal:** Keep article正文 while excluding blog page chrome, and avoid rebuilding
the unchanged status monitor during ordinary Tencent deployments.

## Task 1: Lock the extraction behavior with failing tests

- Add a direct-fetch fixture shaped like article 2691.
- Add preservation cases for prose years and legitimate reference links.
- Add a bookmarklet fixture that proves `.entryPage` wins over `body`.
- Run the focused tests and confirm the current extractor fails.

## Task 2: Implement shared DOM cleanup

- Centralize the reusable unwanted-node selector and content-root candidates in
  `internal/rss`.
- Apply them in direct RSS and bookmarklet extraction.
- Re-run focused and complete backend tests.

## Task 3: Select deployment services

- Add a testable changed-path-to-service mapper in `scripts/auto_deploy.sh`.
- Use selected services for deployment and rollback; retain full Compose deploy
  for Compose-file changes.
- Add shell regression tests and run syntax checks.

## Task 4: Deliver and verify Tencent production

- Commit and push master.
- Wait for the Tencent workflow and verify its exact commit.
- Back up article 2691, re-extract it, and update only its content/metrics.
- Verify containers, direct/public health, and absence of the archive-year tail.

