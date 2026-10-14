# Capture Notes Implementation Plan

**Goal:** 给手动网页保存添加便捷的自由随手记，持久化、私人查看和编辑。
**Architecture:** 插件将 capture_note 与 HTML/PDF 一起提交；独立用户笔记表由现有请求事务管理。前端通过 article.capture_note 渲染，PATCH 更新。
**Tech Stack:** Chrome MV3、Go/Gin/PostgreSQL RLS、React/TypeScript、node:test/vitest。

## Tasks
- [ ] Backend: 先写 HTTP/DB 失败测试；新增 055 migration、RLS 矩阵与 compose migration；repository WithCtx，capture append（2000字，累计10000字，重试不重复，空值不擦除），详情/网摘与 PATCH 访问隔离，响应 note_saved；公开分享不得暴露，备份覆盖。运行对应 go tests。
- [x] Extension: 先写 popup DOM 测试；新增 focused capture-note helper 与可选输入/快捷提示/引用/计数/快捷键，草稿按账号网页隔离，输入期间关闭也保留，保存锁定输入，只在 note_saved 后清理；对接 HTML/PDF 和 duplicate，版本 1.9.0。运行 npm run check。
- [x] Frontend: 先写 ArticleNote 组件编辑/失败保留测试；新增组件与样式、client PATCH/类型、ArticlePage 嵌入并更新 detail cache、ArticleCard 单行预览。运行 npm run check 与 build。
- [ ] Review: 按 requesting-code-review 派独立 reviewer，修正发现，完整回归。用真实浏览器验证布局/输入流程。
- [ ] Delivery: 保留主目录未提交工作，合并 master、推送，等待 deploy-tencent workflow；远端 exact revision、容器、API、bundle/extension ZIP 与用户行为验证。

## Execution
后端任务可独立检查，由实现子代理负责；主代理负责插件和前端，文件所有权不重叠。合并后统一审查。

## Verification progress

- Extension: 192 tests plus Twitter adapter smoke passed.
- Frontend: 695 Vitest cases and 9 legacy tests passed; production build passed.
- Real Chrome extension: prompt insertion, close/reopen draft restore, capture acknowledgement and draft clear verified against isolated API/DB. Article detail edit, page reload persistence, and clip-list preview verified through actual UI.
- Independent review: stale article GET overriding a new note reproduced then fixed; adapter-only scope hint added. Backend commit acknowledgement, PDF filesystem rollback and child-article backup identity are being hardened before integration.
