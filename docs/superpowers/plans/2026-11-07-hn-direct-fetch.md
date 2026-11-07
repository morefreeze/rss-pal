# HN direct-first fetch implementation plan

**Goal:** Restore HN captures when the shared OCI proxy stalls.

**Confirmed design:** Fetch only the fixed HN Firebase API directly first, then use the existing proxy-aware client after a transport/HTTP/body failure. Reuse direct connections; recover failed connections using the existing checked-address race. Each item's direct stage uses at most four seconds and half the remaining capture budget; the pooled primary reserves half that stage for address recovery. Retain SSRF validation, canonical item URLs, ranked comment limits, all-or-nothing capture, and the 25-second total deadline. Invalid/deleted items remain terminal. Bound comment concurrency at ten so the ranked tree fits the existing time limit. Do not change global proxy configuration.

**Evidence:** Production API container exhausts 25 seconds requesting item 50326821 through its configured proxy. OCI proxy works locally; the SSH link shows retransmissions and queued data. Direct Tencent requests return 200, but individual Firebase IPs can time out/reset, so use the existing bounded checked-address recovery for the direct client.

**Execution:** Inline execution; tightly coupled HTTP routing and HN fetch changes.

- [x] Add failing HN routing tests for direct success, proxy fallback, invalid items and cancellation; retain existing ranked-discussion tests.
- [x] Add explicitly direct pooled and bounded GET factories using the existing guarded transport; verify unsafe targets and redirects stay blocked.
- [x] Route HN item requests through the direct client with a bounded share of the remaining deadline, then the existing proxy client. Preserve the 25-second capture cap.
- [x] Run HTTP, RSS and API tests and the exact production-container reproducer; review the diff independently.
- [ ] Integrate with current origin/master, push master, follow established Tencent deployment and verify revision, health and capture persistence using item 50326821.

Validation commands: `go test ./internal/httpx ./internal/rss ./internal/api`, `go test -race ./internal/rss -run TestHN`, `go vet ./internal/httpx ./internal/rss ./internal/api`. Live checks run the same guarded content fetcher inside the production API container, followed by the authenticated capture handler with credentials kept in memory.

Live verification before integration: pooled direct + address recovery with ten workers completed two consecutive exact-link runs in 14.15s and 15.35s, both with 100 comments and 38,688 bytes. The second recovered seven direct-stage timeouts through the original proxy route. Six-worker runs remained vulnerable to the overall deadline. Independent review found no code/security blocker before this final concurrency adjustment.
