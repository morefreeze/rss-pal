# Login method replacement

Goal: switching to password login replaces the primary email login form, with a clear return action and third-party providers retained.

Design: expose an optional primary-form slot on VerifiedAccountAccess. When password login is selected, render its form in that slot instead of the email form and its remember control. Keep VerifiedAccountAccess mounted so switching back preserves the email challenge, cooldown and address. Share the remember-session choice across password, email and third-party login. Registration and binding continue using the default form. Split LoginPage's form from the switch action and place that action beneath the active form.

Execution: current agent; changes are limited to two components and one regression test file.

- [x] Run LoginMethods tests before implementation; confirm email remains visible and remember controls are duplicated.
- [x] Add the primaryForm slot and wire it to the password form; change the return label to 使用邮箱验证码登录 (or 使用第三方登录 when email is unavailable).
- [x] Run frontend tests, legacy checks and production build; review the diff. Passed: 1024 Vitest tests, 10 legacy checks and production build; independent review passed.
- [ ] Integrate with latest origin/master, push master, deploy through Deploy Tencent, verify server revision, API health and both form switches in the production browser.
