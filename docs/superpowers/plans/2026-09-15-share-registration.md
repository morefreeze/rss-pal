# Share Registration Implementation Plan

> 使用 executing-plans；这是已有注册规则的前后端衔接修复，当前代理按紧密耦合步骤执行，完成后独立审查。

**Goal:** 有效分享可供陌生读者直接注册，同时保留人机验证、持久化限流和普通邀请码。
**Architecture:** 前端携带share引用；API验证引用类型和签名；repository在事务内锁定有效分享并创建普通用户。
**Tech Stack:** Go/Gin/PostgreSQL、React/TypeScript/Vitest。

- [x] 后端先写失败HTTP回归：有效短码/签名/legacy无需code注册，缺证明/撤销/失效/伪造拒绝。
- [x] 注册事务与share引用实现，增加并发撤销保护及普通用户断言。
- [x] 前端先写失败导航和提交回归：share在登录/注册切换中保留；无需额外邀请码；只向注册请求传share_ref。
- [x] 实现分享CTA、AuthIntent和RegisterPage/API client衔接。
- [x] Go/Vitest/构建全检，独立安全审查并修正。
- [ ] 提交合并master、部署Tencent、精确提交/服务/健康/前端字节/原分享URL验收。

2026-09-16: approved Tencent captcha and selected-owner revision supersedes original all-active-share scope; see 2026-09-16-tencent-registration.md.
