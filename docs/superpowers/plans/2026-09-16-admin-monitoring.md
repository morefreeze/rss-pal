# 管理员运行监控 Implementation Plan

> Execution: subagent-driven-development。后端统计/采集与前端页面可独立审查；主任务负责前端、迁移部署链与最终交付。

**Goal:** admin 后台可查看注册、验证码、限流、任务积压及成本风险，普通用户无法读取。
**Architecture:** PostgreSQL 受限监控记录 + 现有任务账本/队列，独立管理员只读接口；React 页面每分钟可见刷新。所有异常来自真实统计，不制造历史零值或真实账单。
**Tech Stack:** Go/Gin、PostgreSQL、React/TypeScript。

## Task 1: 后端监控与权限（后端实现任务）

Files: 新增 backend/internal/opsmonitor/*、backend/internal/api/admin_monitoring.go、backend/migrations/045_operations_monitoring.sql；修改 auth.go、auth_abuse.go、taskbudget/budget.go、server/main.go、worker/main.go。

- [ ] 先写真实数据库统计/窗口/日界线/队列/价格与阈值用例以及管理员权限用例，运行失败回归。
- [ ] 定义 DTO 并与前端共享：GET /api/admin/monitoring?hours=1|24|168；未知窗口400，数据库中的普通用户403，数据源异常503。
- [ ] 注册/验证码与限流采集只保存受控分类和必要用户标识；不保存任何敏感请求内容。采集失败记录受控日志并维持主流程授权行为。
- [ ] 复用任务账本、查询真实可执行摘要和探索队列、记录采集起点、30天保留与有界查询；成本展示配置单价乘以任务尝试次数并明确估算语义。
- [ ] 运行聚焦回归，检查不重复计数、不混淆业务拒绝/服务故障、不把系统/global owner=0重复计入用户排行。

## Task 2: 管理员页面（主任务）

Files: frontend/src/api/monitoring.ts、frontend/src/pages/AdminMonitoringPage.tsx、frontend/test/AdminMonitoringPage.test.tsx；修改 App.tsx、Layout.tsx、RoutePageTitle.tsx。

- [ ] 根据固定 DTO 写页面行为用例：普通用户不请求、管理员窗口切换、异常/空数据/未配置单价/加载失败和刷新。
- [ ] 独立页面包含告警、统计卡、趋势表、限流分类、队列快照、额度与用户排行、最近事件；导航仅admin可见，移动端也有入口。
- [ ] 只在可见页面60秒刷新，避免旧请求覆盖窗口切换结果；接口失败明确不可用。
- [ ] npm run check 与 npm run build 通过。

## Task 3: 审查、部署与验收（主任务）

Files: docker-compose.yml、scripts/tests/auto_deploy_service_selection_test.sh、CLAUDE.md、docs/operations/public-registration-todo.md。

- [ ] 将045接入有序迁移链，保留 ON_ERROR_STOP 和 &&；补齐配置说明及TODO状态。
- [ ] 分别进行规格和质量独立审查，修复实际问题。
- [ ] TEST_DB_URL=postgres://postgres:postgres@127.0.0.1:55432/rsspal_test?sslmode=disable go test -p 1 ./...；go vet ./...；部署选择脚本回归。
- [ ] 合并推送并部署腾讯，精确提交工作流成功，实际版本/容器/内外健康/公网构建资源一致；仅用正常登录管理员会话检查页面，不提取生产签名密钥制造令牌。
