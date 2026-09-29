# englife 视频整理

管理员后台地址：`/admin/integrations/englife`。一个 englife 账号供全站 YouTube 抓取使用，共用该账号额度。仅在现有 YouTube 字幕和网页正文策略都没有结果时使用 englife，结果注明“视频整理（englife）”，保留章节和原视频时间点。

## 部署准备

1. 通过项目原有迁移程序应用 `049_englife_integration.sql`。由 status-migrate 按顺序自动执行，支持重复运行。
2. 生成一次加密密钥：`openssl rand -base64 32`。将结果放入部署的 `ENGLIFE_SESSION_KEY`，API 与 worker 必须一致。没有配置时集成关闭；更换密钥后需重新授权。保存备份数据库时也应保护密钥；单独拿到数据库不应得到明文 Cookie。
3. 部署包含这次改动的 API、worker、前端及更新后的 RSS Pal 扩展。扩展请求的是可选 cookies 权限，只有在其授权页点击确认时才请求/使用该能力。

## 管理员操作

在 Chrome 登录 englife.space；打开 RSS Pal 后台的 englife 连接页，点击“登录后授权连接”。扩展自己的确认页显示接收方 `https://rss.morefreeze.top`，确认供全站使用后才读取精确 englife.space 域的 Cookie，直接发送给本站 API。不会读取 Google Cookie 或密码，页面消息中也没有会话数据。

后台校验 englife 登录状态和额度后，才显示已连接。只打开网页或者扩展窗口不代表授权完成。状态读取不会启动视频任务。检查状态会主动查询 englife，断开连接会删除保存的会话并吊销尚未使用的授权令牌；已经提交到 englife 的生成任务不会因此被取消。

本地开发仅允许 `http://localhost:5173` 和 `http://127.0.0.1:5173`，通过 Vite 同源 `/api` 代理连接本地后端。扩展不会接受页面指定的任意上传地址。

## 任务和恢复

任务以 englife 账号、YouTube 视频 ID、中文语言去重，并存储在 PostgreSQL。API 和 worker 使用同一数据库锁，提交前持久化状态。完成结果缓存复用。等待中的任务在之后的 worker 周期继续，当前文章延迟两分钟再尝试，给其他文章留出处理机会。已连接时，原字幕策略最多使用 45 秒，并为 englife 保留 worker 窗口中的 45 秒；未配置或未连接时保持原有策略时限。

- 登录过期：暂停调用，管理员重新登录授权后恢复。额度不足：暂停新生成，已有任务仍可只读查询、取回结果；检查额度恢复后允许新任务。
- 临时网络故障：至少退避五分钟后再检查。
- 提交结果不明：只查询 englife 任务列表寻找原任务，不自动重新 POST，避免重复扣额度；无法确认的任务显示需要处理。
- 远端明确失败：不自动重新生成。应先在 englife 查看原因。
- 断开/连接不会删除已抓取正文；重新连接时让此前没有字幕的 YouTube 文章重新进入待抓取队列。

这是对网站当前私有接口的兼容，不是 englife 官方 OAuth/API 集成。接口来自公开客户端：`/api/auth/me`、`/api/quota`、`/api/jobs`、`/api/documents/:id/preview` 和 `/api/documents/:id`。文档字段从公开成品验证；登录后的实际响应、跨 IP 会话复用、会话续期和生成成功率需要连接真实账号后验证。本次开发只用本地模拟服务和独立数据库验证，没有读取真实 Cookie、登录或消耗生成额度。
