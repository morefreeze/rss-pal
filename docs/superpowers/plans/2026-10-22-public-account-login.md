# Public account login implementation plan

Goal: public verified email/Google/Apple login and signup using existing account IDs and data, retaining existing password login.

- [x] Backend implementer: additive migration064 identity/OTP/OAuth-state/completion-ticket records, secure auth service/handlers, provider/sender configuration and route wiring. Tests first using real PostgreSQL and fake external providers/mail transports; no actual paid mail/provider calls. Preserve RLS and old auth tests under legacy default. Supply agreed frontend contract.
- [x] Frontend/root: config-driven verified sign-in/sign-up and callback ticket exchange; preserve auth intents/remember-device and existing password login; authenticated identity binding; missing configuration and error/retry states, mobile layout tests. Do not store provider tokens or redirect bearer credentials.
- [ ] Separate spec and quality reviews. Full relevant backend/real-role tests and race, frontend checks/build and deployment scripts. Configure available production credentials securely, verify actual provider/email flow when available, otherwise report exact readiness blockers and keep missing channels disabled. Public self-registration is user-authorized; do not add unrelated account migrations or paid services.
- [ ] Incorporate concurrent master, push and deploy established Tencent runtime, verify source revision/health/provider readiness and actual supported user flow. Distinguish fixture proof from real account/email proof.
