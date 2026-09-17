# Public Feed Catalog Implementation Plan

**Goal:** 独立管理人工精选公共目录，保持个人订阅隔离。
**Architecture:** public_feed_catalog 独立表；管理员写接口直接校验数据库角色；公开列表仅已登录可读且只返回已上架项。检查和发布走安全 RSS 抓取，并用版本号阻止并发编辑后的旧结果发布。
**Tech Stack:** Go/Gin/PostgreSQL、React/TypeScript。

## Task 1 — Backend and migration
- [ ] 在 backend/internal/api/feed_catalog_test.go 写真实数据库测试，验证权限、草稿不可见、检查发布、URL 编辑撤销验证、并发旧验证失败、重复 URL、个人订阅不受影响。
- [ ] 运行 `TEST_DB_URL=postgres://postgres:postgres@127.0.0.1:55432/rsspal_test?sslmode=disable go test ./internal/api -run TestFeedCatalog` 并确认失败。
- [ ] 新建 046_public_feed_catalog.sql：独立表和28项前端目录草稿种子，不读取 feeds。
- [ ] 新建 repository/feed_catalog.go、api/feed_catalog.go，实现 GET /feed-catalog；管理员 GET/POST /admin/feed-catalog、PUT /admin/feed-catalog/:id、POST /admin/feed-catalog/:id/check、POST /admin/feed-catalog/:id/publication（{published:boolean}）。返回条目或数组。
- [ ] 条目类型：id,title,url,category,description,sort_order,published,check_status,last_checked_at,last_error,revision,created_at,updated_at。编辑请求带 revision；发布请求带 revision。冲突返回409；普通用户返回403；错误统一{error:string}。
- [ ] published=true 必须真实检查，false仅下架；check失败返回条目且check_status=failed，发布失败422；revision条件写入保证旧URL检查不能发布新URL。
- [ ] 安全抓取、超时、fetch配额纳入既有机制。路由用管理员检查在预算前执行，避免普通用户触发昂贵操作。
- [ ] 运行相关测试与全量Go测试，修复后提交。

## Task 2 — Frontend (independent contract)
- [ ] 先写管理页操作与权限、公开目录加载测试，再实现 src/api/feedCatalog.ts、pages/AdminFeedCatalogPage.tsx。
- [ ] App 与桌面/移动导航添加管理员入口；FeedListPage 移除 POPULAR_FEEDS，读取服务端已发布目录，保留现有预览确认订阅流程。
- [ ] 管理员支持列表搜索、发布状态筛选、创建/编辑、检查/上架/下架；显示检查时间与失败原因。请求中禁用重复动作，失败可重试。
- [ ] npm run check 与 npm run build 通过；提交限定前端文件。

## Task 3 — Review and delivery
- [ ] 先按设计审查，再审查权限、并发和UI错误分支；修复后完成全量验证。
- [ ] 推送master，等待腾讯流水线成功，核对版本、容器、内外健康。
- [ ] 通过共享服务逻辑逐一检查迁入种子并仅发布有效源，失败仍为草稿；核对0个人订阅变化。
- [ ] 实际管理员页面和普通推荐展示验收。
