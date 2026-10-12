# Inline summary return control

Goal: remove the mobile scroll lag caused by a body-level fixed overlay.
Approved behavior: append the existing ↩⌖ control to the target paragraph's text flow; use a sibling row after structural/media blocks. Preserve exact summary source, highlight, history, keyboard and reduced-motion behavior.

Implementation and verification:
- [x] Replace viewport geometry and scroll/resize positioning with DOM insertion and CSS normal flow.
- [x] Reuse the last paragraph for quotes/list items; avoid invalid table children and nested links.
- [x] Observe detachments for cleanup; remove sibling rows on return/replacement.
- [x] Exclude navigation controls from reader selection and touch link actions.
- [x] Confirm failing regressions before implementing both fixes.
- [x] Independent review identified reader-action integration; regression verified and fixed.
- [x] Mobile browser at 390px: target paragraph contains the static link, scroll delta 170px equals button delta -170px; return removes link and highlights original source without URL hash.
- [x] Table browser check: link in sibling row, outside table.
- [ ] Final frontend checks/build, commit and push to master, Tencent deployment and served asset/health verification.

Scope: frontend only. Existing unrelated edits in the user's original checkout are preserved. Browser fixture is temporary and is excluded from the commit.
