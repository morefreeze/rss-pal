# 默认推荐分类目录实施计划

**Goal:** 用 Folo 式目录组织默认推荐并补充可直接预览的订阅源。
**Architecture:** 完整分类元数据 + 既有公共推荐 API + 可复用 React 浏览组件 + FeedListPage 现有预览回调。保留整个区域的折叠初始化与 aria-controls。
**Tech Stack:** React, TypeScript, CSS, Vitest, Testing Library.

- [ ] 新增目录交互测试：分类导航、全局搜索去重、空结果、推荐预览回调，以及键盘可访问名称。
- [ ] 兼容已有默认源分类，并以幂等迁移新增草稿；补充 Folo 相关具体源；检查新增 URL 的 RSS/Atom 内容，记录结果。
- [ ] 实现响应式分类卡片、分类列表、返回、搜索及已有订阅提示；FeedListPage 调用现有 doPreview。
- [ ] 更新旧静态接线检查为新组件接线，并运行前端检查、构建和迁移隔离测试。
- [ ] 自审 URL 路由、预览认证边界与交互；浏览器桌面/移动端验证。
- [ ] 合入 master 并推送；等待腾讯部署；核对 SHA、容器、健康、线上目录行为。
