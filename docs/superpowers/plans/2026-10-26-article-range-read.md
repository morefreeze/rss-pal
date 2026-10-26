# Article range read implementation plan

Goal: mark this article, this and above, or this and below read across the full filtered list. Include the anchor. Desktop menu; mobile left swipe/long press reads one, right swipe opens range choices.

Architecture: authenticated atomic PostgreSQL operation sharing list filters and timestamp ordering, with ID tie breaker. Scope includes feed, saved, tags and clip source. An already-read anchor remains a valid boundary in unread mode. Return newly changed IDs/count, refresh loaded pages and sidebar counts, and reject anchors outside the user's scope. Search/grouped views expose single-article actions only because their visual order differs from the chronological list.

- [x] Write/run failing database tests for inclusive ranges, >1 page, asc/desc, equal timestamps, captured/published, saved/tag/hidden/owner scope and read anchors.
- [x] Add range endpoint and repository query; add deterministic ID tie break to the normal list.
- [x] Write/run failing gesture component tests: left, right menu, long press, cancelled/vertical/multi-touch movement, interactive descendants, click suppression and keyboard menu.
- [x] Add touch/menu surface to normal/tweet cards and integrate normal and clip list mutations. Preserve pagination and reject stale requests after mutation/filter changes.
- [ ] Run frontend checks/build and backend tests against isolated local PostgreSQL. Review diff, resolve defects, integrate remote mainline, push and verify established Tencent workflow plus public health and shipped frontend.

Validation before integration: `go test -p 1 ./...` with temporary PostgreSQL on port 55439 passed; frontend 940 Vitest tests plus 9 legacy tests passed; final changed frontend areas 44 tests passed; production build passed (existing bundle-size warning). Separate code review found long-hold click suppression and stale grouped responses; both fixed with regressions. Browser fixture at desktop and 390px mobile widths confirmed menu visibility/no horizontal overflow; simulated swipe consumed click. Physical iPhone remains unverified.
