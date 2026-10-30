# Search highlights implementation plan

Goal: expose relevant search context and navigate all visible article matches.
Architecture: bounded API snippet plus shared safe text matcher and HAST plugin; context provider and DOM mark index coordinate the bottom toolbar without rewriting React text nodes.
Stack: Go, React, react-markdown, Vitest.

- [x] Add failing API snippet tests, implement search-specific DTO/helper, retain lean normal list payload. Run Go API/repository tests.
- [x] Add failing matcher and rendered article search tests covering split inline text, code, pinyin, summary, wrap, query updates and close. Implement shared matcher, HAST plugin, provider and toolbar.
- [x] Connect list/result routing, restore q, reject stale requests and pass search through adjacent articles. Connect MarkdownArticle, SummaryMarkdown, TweetCard and both detail layouts. Skip saved-scroll restoration while searching.
- [x] Add theme tokens and bottom safe-area/mobile spacing. Run frontend suite/legacy/build and relevant Go suite. Review changes, inspect desktop/mobile five-theme browser screenshots and fix defects.
- [ ] Commit scoped changes, integrate current master without unrelated edits, push and verify Tencent workflow, exact deployed revision, containers/health and public frontend assets.
