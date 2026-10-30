# Nickname write policy

`backend/internal/nickname/policy.go` is the authoritative policy for newly chosen nicknames and future QQ profile imports. Rules are enforced in the repository before a custom write; rejected API requests return 400 and do not consume the once-only choice. The browser displays the server's generic error and permits correction.

- Custom names retain the 1–32 rune limit and reject control/format characters.
- Reserved authority names include admin, administrator, root, system, official, support, moderator, webmaster, RSS Pal and Chinese administrator/official/customer-service labels.
- An explicit local dictionary covers common insults, sexual solicitation and unambiguous hate expressions. It is a bounded dictionary, not semantic moderation. Extend the centralized lists with allow/deny regression cases when adding a rule.
- A comparison copy is compatibility-decomposed and case-folded, stripping diacritics and handling common Latin lookalikes. English rules match word boundaries and separated letters (including full-width/styled forms, a.d.m.i.n, admin123, adm1n and r00t). Chinese terms are checked after removing separators. Accepted display spelling is not normalized in storage.
- Regression near-matches include badminton, supporter, administration笔记, Scunthorpe, ordinary Chinese names and emoji.

QQ identity checks, token handling, profile response validation and avatar validation are unchanged. After those checks, content-restricted future QQ nicknames become `QQ读者` before temporary completion storage. `ResolveQQIdentity` repeats this check at its write boundary. The fallback remains display metadata: it never writes `users.nickname`, changes ownership/privileges, or consumes the one custom choice. Structurally invalid QQ API responses still fail validation.

Per the user's explicit decision on 2026-10-30, historical stored nicknames are grandfathered. There is no migration, scan, read-time filtering, or automatic reset of chosen nicknames. A future QQ refresh may update QQ display metadata but never overwrites an existing custom nickname. Login usernames and provider subjects are authentication identifiers and are outside this display-name policy.
