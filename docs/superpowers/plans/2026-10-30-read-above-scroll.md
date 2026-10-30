# Read-above continuation implementation plan

**Goal:** Before marking an inclusive range above an article read, remember the next unread article in the active list order. After a successful refresh, scroll that article into view without opening it.

**Architecture:** Share continuation capture/resolution between article and clip lists. Use stable article IDs, preserve existing page coverage, fetch extra pages when the next unread is beyond loaded rows, and scroll in a layout effect only after the matching request generation commits. Failed mutations, stale filter requests, below/single actions and empty continuations must not scroll. Keep five-second undo behavior.

**Implementation:** Execute inline using writing-plans, executing-plans, test-driven-development and verification-before-completion. Existing user instructions authorize integration and Tencent deployment.

- [x] Extend list interaction regression tests: remember a known next unread across refresh, skip server/session read rows, find a next unread beyond the page boundary, unread-only list compaction, no-next/failure/filter changes, and clip pages.
- [x] Run the focused tests and confirm failures are specifically missing continuation scrolling.
- [x] Add shared continuation helpers and stable card ID attributes. Extend the two existing loaders to retain page coverage and find the requested continuation. Set pending scroll only on successful current-generation refresh; consume after React renders.
- [x] Run focused tests, full frontend tests, legacy checks, and production build. Review the diff for pagination, stale response and undo interactions.
- [x] Verify real browser card geometry using an isolated fixture against the production UI bundle, then push the integrated master commit using the established Tencent deploy workflow.
- [ ] Verify exact deployed revision, container health, public API, frontend bundle and browser behavior. Preserve unrelated original checkout work.

Browser geometry review: mobile clip cards require a 64px scroll margin to clear the 49px sticky tag bar. Verified target top 64.23px with the title fully visible.
