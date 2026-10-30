# Account setup prompts

Confirmed requirements: prompt accounts without a password to set one; show the name-setup prompt only once; if both name and password are missing, show only the password prompt.

User clarified that "username" means the existing profile nickname. Persist nickname-prompt acknowledgment on the account, independently of the nickname value. Keep a local seen marker across logout as an immediate fallback, retry the idempotent acknowledgment when needed, and leave the currently visible prompt open while its acknowledgment is saved. A password prompt retains priority for the entire login.

Common implementation: server returns needs_password based on the !external sentinel; authenticated first-password setup does not require an old password, while later changes do. Password writes compare the previous hash atomically. Settings show the first-password form and actual login username. Password onboarding takes priority over nickname onboarding for the login, including direct visits to settings; dismissing it lasts for that login. New login resets that password prompt suppression. Delayed boot profile responses cannot overwrite a newer profile save.

- [x] Reproduce missing password state and rejected first-password setup in backend tests; implement and pass targeted tests against local PostgreSQL on port 55449.
- [x] Reproduce and implement first-password settings form and priority, skip, account isolation and direct-settings behavior.
- [x] Reproduce and protect newer profile state against delayed boot hydration.
- [x] Implement account-persistent one-time nickname prompt tracking, including migration replay safety.
- [x] Complete review, regression and browser QA of the full behavior.
- [ ] Integrate with latest origin/master and deploy via established Tencent procedure; verify revision, health and user-facing behavior.

Verification: 1052 frontend tests, 10 legacy checks and production build passed on the integrated mainline base. All backend packages passed (the repository package was rerun after updating its staged QQ migration fixture); targeted account API/repository race tests passed. Deployment service-selection and bundle checks passed. Independent review found no remaining actionable issues.

Browser QA used an isolated local PostgreSQL database, the actual account handlers and empty reader fixtures. Verified missing-password priority, first password setup without an old-password field, actual password login after setup, no nickname follow-up during the same login, nickname acknowledgment surviving later login and a fresh browser with stale cached profile, and mobile password guidance. No real user credentials were changed. Production revision, schema, health and authenticated metadata are verified separately during release.
