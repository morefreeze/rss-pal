# Nickname policy implementation plan

**Goal:** Reject newly chosen reserved/inappropriate nicknames and replace restricted future QQ imports, preserving historical custom names and the one-time choice.

**Architecture:** Centralize write-time policy in `backend/internal/nickname`, invoked by the nickname repository and QQ transport/repository. Normalize a comparison copy only: compatibility/case/diacritic normalization, common Latin lookalikes, separated-letter matching, and bounded authority/profanity rules. Accepted display text retains its original spelling. No read-time filtering, data migration, or authentication identity changes.

**Confirmed decision:** Historical chosen nicknames remain unchanged. Invalid future QQ display names become `QQ读者`, without filling `users.nickname` or consuming the custom choice. Structurally invalid QQ API responses retain existing failure behavior.

**Tech stack:** Go, existing `golang.org/x/text`, PostgreSQL, React/Vitest.

- [x] Add failing API/repository/UI cases for admin, full-width/separated variants, inappropriate content, correction after rejection, QQ fallback, one-time choice, and preserved historical names.
- [x] Add `nickname.Validate` / `nickname.QQ` with focused table tests for reserved roles, explicit insults/sexual solicitation/hate expressions and safe near-matches. Keep structural limits (custom32, QQ128) and identity/token validation unchanged.
- [x] Invoke the centralized rule before custom nickname writes and before QQ completion metadata is persisted, with defense at `ResolveQQIdentity`. Return a generic 400 without reflecting the blocked text; keep rejected requests from consuming the choice.
- [x] Add brief settings guidance and verify server rejection leaves the form usable. Run frontend full tests/build and backend nickname/QQ/auth tests against explicit PostgreSQL.
- [ ] Independent review, merge/push master, established Tencent deployment, exact runtime/bundle/health verification. Use an isolated-database real API/browser correction flow; preserve production account state during deployed smoke checks.

Verification commands (from worktree root):

```sh
TEST_DB_URL=postgres://postgres:postgres@127.0.0.1:55449/rsspal_test?sslmode=disable /Users/bytedance/sdk/go1.25.3/bin/go -C backend test ./internal/nickname ./internal/api ./internal/repository -run 'Test(Nickname|NewNickname|QQ|Restricted|Ordinary)' -count=1
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm --prefix frontend run check
PATH=/Users/bytedance/.nvm/versions/node/v22.19.0/bin:$PATH npm --prefix frontend run build
```

The local dictionary covers explicit cases; it is not semantic/full-language moderation. No third-party moderation service, fee, or personal-data transfer is introduced.
