# Explore article actions

Approved behavior: cards and article detail (top and bottom) expose Skip this article and Read later. Skip persists per account, removes only this article from recommendations, returns detail to list, supports undo. Read later toggles in place and is available from Explore's Read later view. Neither action writes source/topic feedback or subscribes.

API contract: PUT /api/explore/articles/:id/state with a partial JSON object {skipped?: boolean, saved?: boolean}; returns {skipped:boolean,saved:boolean}. At least one field required. GET /api/explore?view=later returns the existing ExplorePage shape. Both list items and detail include saved/skipped booleans. Normal recommendations omit skipped articles; later shows saved even if skipped. Later sort/topic/offset/limit supported. Saved article visibility and retention independent of current snapshot age.

Storage: per-user normalized article URL preferences, skipped and saved flags, saved_at timestamp; stable URL keeps skip after cache pruning. Saved articles exempt from pruning and directly accessible to their owner. RLS and authorization use existing request transaction. No influence on feedback ranking.

Tasks:
1. Backend failing regression tests, migration and repository queries, state handler and route, retention preservation. Verify real PostgreSQL tests, isolation and no subscription/feedback writes.
2. Frontend failing action tests, API contract, shared actions with request guards, card/detail placement, later tab, errors/undo and pagination refresh. Verify tests and build.
3. Independent specification/quality review, fix findings, integrated tests, push master using isolated checkout, deploy existing Tencent workflow, verify revision/health and live browser actions.
