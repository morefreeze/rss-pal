# 分享链接注册

用户已经明确：公开分享到 X 的有效分享链接等价于注册邀请，陌生读者无需另取邀请码。当前分享按钮只传 intent，未传分享凭证；注册接口仍要求独立 code，导致入口断链。

方案：分享页在登录/注册导航传递 `share=s:<short_code>` 或 `share=t:<token>`；保留原始订阅意图。注册表单展示“通过分享邀请注册”，隐藏独立邀请码；直接注册仍允许普通一次性邀请码。`POST /api/auth/register` 接受互斥的 code 或 share_ref；客户端传什么不构成授权，服务端校验短码格式/长token签名，再在创建普通用户的同一事务锁定有效分享行，检查未撤销且未过期。分享凭证可供不同读者使用，不消耗原分享、不授予分享者私有数据权限。

沿用 Turnstile signup 验证及已有持久化注册/IP/全局限流。对失效分享、伪造token、无凭证和两类凭证同时出现拒绝注册。注册成功只发新用户自身JWT。登录跳转不携带share到业务URL，禁用任意next跳转。

选择现有分享作为服务端凭证，避免仅前端填码（无后端授权）或移除全部邀请限制（超出需求）。普通邀请码单次消费保持不变。

验收：短链/签名链/legacy链；登录注册切换；有效链接多个不同用户；到期/撤销/未知/签名篡改拒绝；撤销与注册事务互斥；人机证明与限流未绕过；生产原链接导航、健康和精确构建验收。

## Approved revision 2026-09-16

Use Tencent Captcha 2.0 instead of Turnstile as the deployed registration verifier. Preserve provider abstraction and explicit rollback adapter. Form submit opens Tencent verification; only backend-verified fresh ticket/randstr permit registration. No fallback bypass.

Share invitations initially allow only fixed owner ID 1 (production share owner confirmed read-only). Configurable selected_owners/all/disabled modes leave an immediate future opening path. Ordinary users and newly added admins do not inherit eligibility. Public response has a boolean eligibility hint; locked registration transaction is authoritative. Older valid administrator shares work. No changes to tenant quotas or existing accounts.
