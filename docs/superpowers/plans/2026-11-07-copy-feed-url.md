# Copy subscription address

**Goal:** Every existing subscription, including paused sources, offers one-click copying of its complete stored URL.

**Design:** Place a visible secondary “复制地址” button beside the displayed URL. Keep it outside the title link and stop click propagation so copying does not open the article list. Use the existing clipboard and toast conventions, showing success only after the write resolves and an actionable error on rejection or unavailable clipboard. Wrap long addresses and retain the mobile 44px touch target. Preserve URL bytes, query parameters, and feed state.

**Implementation plan:** Execute inline; this is one tightly coupled UI change.

- Add an asynchronous clipboard handler in `frontend/src/pages/FeedListPage.tsx`.
- Split the title link from the address row and add a per-source accessible copy button.
- Add scoped address wrapping styles in `frontend/src/index.css`.
- Run the existing frontend suite, legacy checks and production build. Verify desktop/mobile layouts, native clipboard contents, no navigation, and denied/unavailable clipboard in a browser using isolated fixture feeds.
- Review the final diff, integrate on current `origin/master`, push mainline, wait for Tencent deployment and verify revision, service health and public bundle.

**Self-review:** No API/data changes or dependencies. No unresolved product decisions. Clipboard denial must never report success. Existing subscription navigation remains available through both the title link and card.
