# Social sharing implementation plan

Goal: Copy a short title, summary or opening paragraph and public URL; support WeChat and native sharing on owner and public surfaces.
Architecture: Extend existing pure xShare composer with content fallback and literal URL budget. ShareActions owns user-triggered native share, clipboard feedback and local QR fallback. ShareDialog retains guarded create/revoke lifecycle and uses the composer for automatic copy. PublicArticleReader reuses the actions.
Tech stack: React, TypeScript, qrcode.react, Vitest.

- [x] Add regression tests for content fallback, text limits, summary preference, native share cancellation and failure, QR fallback, and creation auto-copy.
- [x] Extend xShare.ts with content fallback and URL-aware conservative 140 budget, preserving grapheme boundaries and full public links.
- [x] Add ShareActions.tsx for copy text, copy URL, X intent, WeChat and more sharing. Call navigator.share synchronously from click; cancellation must not copy. Clipboard failure reveals selectable textarea. Generate QR locally. Key actions by URL and article to avoid stale state.
- [x] Wire owner article content and summaries into ShareDialog; auto-copy composed text after create, keep existing generation guards. Reuse actions on public reader.
- [x] Adapt wrapping controls, 44px targets, dvh and bottom safe area. Test with npm test, npm run test:legacy, npm run build.
- [x] Review via independent code reviewer and inspect browser at mobile and desktop sizes.
- [ ] Integrate master without unrelated work, push and observe Deploy Tencent. Verify deployed SHA, health, public UI and share output.

Decisions: Existing AI summary preferred; no new AI request. Browser/system chooser plus copy and QR for WeChat; native branded card integration excluded. Full links always preserved, even if a legacy URL alone exceeds the budget. No publishing to social platforms.

Verification: 689 full frontend tests and 9 legacy checks passed after QR-cancellation regression; 115 focused share tests passed. Production build passed. Independent review found no blocking issue; QR reachability suggestion implemented. Browser 390px viewport has no horizontal overflow, 44px share targets, correct 97-codepoint Chinese summary copy. Native OS/WeChat delivery still requires a physical device. Deployment verification recorded in final report.
