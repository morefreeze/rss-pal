# Summary character limits implementation plan

**Goal:** Add prompt instructions limiting brief summaries to 300 characters and detailed summaries to 2000, with complete endings.

**Architecture:** Append one shared instruction at summary-specific prompt construction sites. Cover default and custom-template text calls, streaming calls and vision builders. Retain any stricter template limits. Count the full output including Markdown and links; do not truncate returned text. Existing token caps stay unchanged.

**Tech Stack:** Go, existing AI request-capture tests.

- [x] Add prompt-contract coverage using existing capture servers for default/template, streaming and vision paths; verify failure before implementation.
- [x] Append the shared character-limit instruction at all summary-specific prompt sites. Preserve anchor instructions and stricter template requirements.
- [x] Run AI tests, service/API/worker checks and build; review changes for unrelated behavior.
- [ ] Integrate on latest origin/master, push master, verify Tencent Actions deployment and runtime revision/health. Prompt constraints improve compliance but do not guarantee model output length or prevent token exhaustion.
