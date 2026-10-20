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
- [x] Initial integration: `dc4a2de` fast-forwarded actual remote master and deployed to Tencent; exact remote revision, local/public health, bundle checksum and receiver/guide verified. Original local master remains behind with unrelated dirty files preserved.

## Acceptance boundaries

The native share sheet normally passes the link rather than source article identity or logged-in page HTML. URL capture uses the existing server fetch path, so sites requiring browser login remain subject to its limitations. Browser automation and macOS signing do not establish real-iPhone installation, native-menu behavior or execution; the final report must name this remaining device verification.

## Signing limitation found during execution

macOS `shortcuts sign --mode anyone` returns `In order to do this, you must be signed into iCloud.` Do not publish an unsigned file as installable or show a dead download link. Ship the explicit manual setup guide as the installation path, retain the credential-free reproducible builder for a future signed export on an authorized signed-in Mac, and report the exact limitation. No iCloud credentials or third-party signing services are requested or used.

## One-button installation correction

The user rejected manual setup as the product flow. After the user completed iCloud sign-in, macOS signing succeeded and Shortcuts.app published the native workflow at:

`https://www.icloud.com/shortcuts/bee8c10b751e47a18d87fb75f889b3d3`

Apple's public record reports `name=加入 RSS Pal`, `signingStatus=APPROVED` and supplies a downloadable signed artifact. The raw published workflow is archived at `shortcuts/rss-pal-capture.plist`; the builder re-exports this actual native source instead of hand-written action identifiers. Its five native actions and variable references were inspected, and native macOS execution opened the receiver with the shared CJK URL, query and fragment intact.

Settings now exposes an “添加到快捷指令” button opening that Apple installer directly. The old `/shortcuts/iphone-setup.html` URL also becomes a direct install button, with no action-construction guide. System confirmation to add remains required. No account identifiers, passwords, tokens, clipboard reads or third-party signing services are involved.

Verification: 755 frontend tests, 9 legacy checks, production build and independent review passed. Physical iPhone addition and native share execution remain device acceptance checks.
