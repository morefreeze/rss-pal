# 用户订阅独立 Implementation Plan

**Goal:** 新用户0订阅，推荐与个人订阅完全分离，状态互不影响。
**Architecture:** 使用现有 feeds.owner_id 及 owner/url 唯一索引；推荐目录和探索缓存保持独立；迁移历史共享订阅归admin。
**Tech Stack:** Go, PostgreSQL RLS, React/TypeScript。
**Execution:** subagent-driven-development：后端归属/迁移为一个独立任务，主任务完成前端语义及集成，之后规格和质量独立审查。

- [x] 后端先添加失败回归：两用户订阅同URL，确认不同feedID，A暂停/B仍active；新用户不得列出共享源；探索不得将全局源视作本人已订阅。
- [x] 更新 backend/internal/explore/subscribe.go、repository用户可见查询及必要RLS；新增044迁移保留admin历史数据与ID，处理冲突fail closed。只改影响该契约的旧测试。
- [x] 主任务更新 frontend订阅页面文案/空状态；真实行为回归验证推荐点击新建本人订阅。
- [x] Compose status-migrate 接入044，部署选择脚本同步；规格审查→质量审查。
- [x] 使用本机 PostgreSQL 完整后端回归（旧共享画像预期更新后 worker 全包补跑通过，其余包通过）、前端check/build及迁移升级回归；合并推送并部署腾讯。
- [x] 生产核对admin订阅/状态保持、普通用户0订阅且推荐可见、源码版本/资产/健康；更新文档。验收证据见 `docs/operations/public-registration-todo.md`。
