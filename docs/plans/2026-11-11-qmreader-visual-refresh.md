# QMReader visual refinement

Scope: refine the approved v2 workspace against the inspected rss.qiaomu.ai reference. Preserve routing, list caches, reading actions, five mobile destinations and account/theme preferences.

Design: system sans typography; neutral gray sidebar, white list and restrained reader; line icons; compact source/date-first rows, two-line summaries and neutral selection. Paper keeps a slightly warm reader; dark themes retain dark surfaces. Avoid new dependencies or copied reference code. UI/UX Pro Max accessibility and spacing guidance applies; its generic landing-page recommendation does not apply to this reader.

Implementation: shared SVG icon, scoped workspace tokens/chrome, article row hierarchy and reader typography; then build and regression tests, desktop/mobile/theme browser inspection, diff review. Back up the current production frontend image/static files and retain the existing database backup. Integrate into master, use the established Tencent deployment entry point, verify revision/assets/health/live UI. Restore the saved image immediately on failed rollout.

Self-review: visual scope only; no backend or database changes; scoped rules leave public/auth layouts alone; retain visible keyboard focus, mobile 44px controls and native links. Existing worktree isolates unrelated main-checkout edits.
