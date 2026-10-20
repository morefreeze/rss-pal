# iPhone share capture implementation plan

**Goal:** Preserve native link long press, make selection capture usable on phones, and configure an iPhone share-sheet shortcut that opens a signed-in RSS Pal capture page.

**Architecture:** A token-free shortcut extracts one shared URL, URL-encodes it and opens `/capture?url=…`. The receiving page validates the URL, preserves it through login, and submits the existing authenticated `createOneoffLinkSet(url, false)` only on the user's capture button. Shared links become independent clips; reader selection actions retain their parent article and local draft behavior. Mobile selection uses an unfocused bottom toolbar, preserving native selection handles. Desktop keeps its context menu.

**Execution:** Inline, because the interaction and shortcut handoff must be verified together. No new native app, PWA, public API, credentials in shortcuts, or automatic writes on GET.

## Tasks

- [x] Add failing phone interaction tests: long press remains native; text/link selection stays selected, toolbar does not focus, handle adjustment does not close it, action uses current links.
- [x] Remove custom touch long-press interception. Add phone bottom presentation and explicit dismiss to ReaderContextMenu, retain desktop keyboard/focus behavior.
- [x] Add capture intent tests and receiving-page tests: login round trip, invalid/duplicate parameters, no mount/StrictMode writes, single-flight submit, success and failure states.
- [x] Implement `/capture` with existing capture API; add safe capture auth intent without supporting arbitrary redirects. Add settings install link and clear setup instructions.
- [x] Generate a credential-free shortcut source and verify its plist round trip. Signing was attempted and blocked by iCloud sign-in; deliver the manual setup guide instead, with no installable-file claim or download link.
- [x] Run frontend checks (749 tests + 9 legacy checks) and build; independent review fixes verified. Browser fixture at 390px: preserved Selection/focus, two-link action executes, bottom bounds 8px from viewport; desktop menu retains keyboard focus. Physical iPhone execution remains unverified.
- [ ] Integrate only this change into master while preserving dirty files; push, wait for exact-commit Tencent workflow and prove remote revision, health, public bundle, receiver and shortcut configuration guide.

## Acceptance boundaries

The native share sheet normally passes the link rather than source article identity or logged-in page HTML. URL capture uses the existing server fetch path, so sites requiring browser login remain subject to its limitations. Browser automation and macOS signing do not establish real-iPhone installation, native-menu behavior or execution; the final report must name this remaining device verification.

## Signing limitation found during execution

macOS `shortcuts sign --mode anyone` returns `In order to do this, you must be signed into iCloud.` Do not publish an unsigned file as installable or show a dead download link. Ship the explicit manual setup guide as the installation path, retain the credential-free reproducible builder for a future signed export on an authorized signed-in Mac, and report the exact limitation. No iCloud credentials or third-party signing services are requested or used.
