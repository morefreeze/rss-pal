# 主流网站 URL 自动路由设计

**日期：** 2026-07-15
**范围：** 后端 URL 解析与抓取入口
**主要文件：** `backend/internal/rss/resolver.go`、`backend/internal/rss/resolver_test.go`

## 背景

RSS Pal 的添加订阅流程允许用户粘贴普通网页 URL。后端 `Fetcher.Preview`、`Fetcher.Fetch` 和 `Fetcher.FetchHTML` 都会先调用 `ResolveFeedURL`，但当前该函数只会把 B 站 UP 主主页转换为 RSSHub 路由。用户粘贴 YouTube、微博、抖音、知乎、CSDN、GitHub 等主流平台主页时，系统通常会把平台 HTML 当作普通网页抓取，无法得到稳定的订阅内容。

RSS Pal 已在 Compose 中运行 `diygod/rsshub:chromium-bundled`，所以可以把“不直接提供 RSS、但 RSSHub 已有确定性路由”的平台 URL 转成内部 RSSHub 地址。用户数据库仍保存原始平台 URL；每次 Preview 或 Fetch 时临时解析，避免把只在容器网络可用的 `http://rsshub:1200` 持久化或暴露给客户端。

## 目标

- 用户粘贴受支持平台的普通主页、频道或集合 URL 后，Preview 和后续定时抓取都访问正确的原生 Feed 或 RSSHub 路由。
- 优先使用平台官方 Feed；只有不存在稳定官方 Feed 时才使用 RSSHub。
- 只有 URL 结构足以唯一确定订阅语义时才转换。
- 未识别、参数不完整、单篇内容或要求当前部署不具备的强制认证时保持原 URL，让现有 RSS 自动发现和 HTML fallback 继续处理。
- 不引入网络查询、浏览器探测、数据库迁移或前端改动。

## 非目标

- 不实现 RSSHub 全量路由发现或 RSSHub Radar 的动态规则同步。
- 不承诺绕过平台反爬；本功能只保证 URL 到路由的转换正确。
- 不为 X/Twitter、Instagram 配置 Token、Cookie 或登录态。
- 不把普通微信公众号文章反推出公众号订阅源；文章 URL 不包含足够的历史消息入口信息。
- 不改变 Feed 表中保存的原始 URL，也不新增 provider 字段。

## 方案比较

### 方案 A：代码内确定性规则表（采用）

在 Go 后端维护小规模、显式、可单测的 URL 解析规则。每条规则只依赖已解析的 host、path 和 query，不访问网络。

优点是行为稳定、失败边界明确、没有运行时依赖；缺点是新增平台需要提交代码。首批仅覆盖主流且可从 URL 唯一推导的路由，这个维护成本可接受。

### 方案 B：运行时读取 RSSHub `/api/namespace`

RSSHub 能返回 namespace、path、Radar 和 feature 元数据，但同一页面可能命中多个路由，例如知乎用户主页可对应动态、回答、文章等不同语义。运行时加载还会增加启动依赖、缓存与版本兼容问题。

### 方案 C：引入浏览器 Radar 探测

复用 RSSHub Radar 式页面匹配能扩大覆盖，但需要浏览器上下文或前端扩展，且强反爬页面本身可能无法加载。它适合后续“高级订阅发现”，不适合作为当前纯后端 resolver 的第一版。

## 架构

`ResolveFeedURL` 保持现有签名：

```go
func ResolveFeedURL(input, rsshubBase string) string
```

内部流程：

```text
原始 URL
  ├─ rsshubBase 为空 / URL 非法 / 非 HTTP(S) ───────────> 原样返回
  ├─ 已是 rsshubBase 地址 ─────────────────────────────> 原样返回
  ├─ 官方 Feed 可确定（YouTube channel / playlist）────> 官方 Feed URL
  ├─ 命中平台确定性规则 ───────────────────────────────> rsshubBase + route path
  └─ 未命中或语义不唯一 ───────────────────────────────> 原样返回
```

实现上把平台逻辑拆为小型 resolver 函数，并按稳定顺序调用：

```go
type platformResolver func(*url.URL) (route string, ok bool)
```

平台 resolver 只返回相对于 RSSHub base 的 route。YouTube 官方 Feed 属于例外，由顶层函数直接返回完整 URL。公共辅助函数负责：

- host 小写化并移除可接受的 `www.` 前缀；
- 清理重复/尾部斜杠；
- 对 route path segment 执行安全转义；
- 使用统一函数拼接去掉尾斜杠的 `rsshubBase`。

## 首批 URL 映射

### Bilibili

| 输入 | 输出 |
|---|---|
| `space.bilibili.com/<numeric_uid>` | `/bilibili/user/video/<uid>` |
| `space.bilibili.com/<numeric_uid>/video` | `/bilibili/user/video/<uid>` |

现有行为保持不变。动态页、单个视频页和非数字 UID 不转换。

### YouTube

| 输入 | 输出 |
|---|---|
| `youtube.com/channel/<channel_id>` | `https://www.youtube.com/feeds/videos.xml?channel_id=<channel_id>` |
| `youtube.com/playlist?list=<playlist_id>` | `https://www.youtube.com/feeds/videos.xml?playlist_id=<playlist_id>` |
| `youtube.com/@handle[/videos]` | `/youtube/user/@handle` |
| `youtube.com/user/<username>` | `/youtube/user/<username>` |
| `youtube.com/c/<custom_name>` | `/youtube/c/<custom_name>` |

`watch`、`shorts/<id>` 和单个直播 URL 不转换。频道 ID 与播放列表有官方 Feed，因此不经过 RSSHub。

### 微博

| 输入 | 输出 |
|---|---|
| `weibo.com/u/<numeric_uid>` | `/weibo/user/<uid>` |
| `m.weibo.cn/u/<numeric_uid>` | `/weibo/user/<uid>` |
| `m.weibo.cn/profile/<numeric_uid>` | `/weibo/user/<uid>` |

部分账号仍可能要求 `WEIBO_COOKIES`；该配置在 RSSHub 中是可选项，因此允许路由转换并保留真实抓取错误。

### 抖音

| 输入 | 输出 |
|---|---|
| `douyin.com/user/<uid>` | `/douyin/user/<uid>` |
| `douyin.com/hashtag/<cid>` | `/douyin/hashtag/<cid>` |
| `live.douyin.com/<rid>` | `/douyin/live/<rid>` |

单个视频 URL 不转换。上述路由依赖已部署的 Chromium，并可能受 WAF 影响。

### 知乎

| 输入 | 输出 |
|---|---|
| `zhihu.com/people/<id>` 或 `/activities` | `/zhihu/people/activities/<id>` |
| `zhihu.com/people/<id>/answers` | `/zhihu/people/answers/<id>` |
| `zhihu.com/question/<question_id>` | `/zhihu/question/<question_id>` |
| `zhihu.com/topic/<topic_id>` | `/zhihu/topic/<topic_id>` |

普通回答详情、文章详情和 `zhuanlan.zhihu.com` 不转换。当前 RSSHub 专栏路由把 `ZHIHU_COOKIES` 标为强制配置，而 rss-pal Compose 未提供该配置。

### CSDN

| 输入 | 输出 |
|---|---|
| `blog.csdn.net/<user>` 及该用户博客下的文章 URL | `/csdn/blog/<user>` |

从文章 URL 可唯一推导博客作者，因此允许把文章页转换为作者 Feed。

### GitHub

| 输入 | 输出 |
|---|---|
| `github.com/<user>` | `/github/activity/<user>` |
| `github.com/<owner>/<repo>` 及仓库子页面 | `/github/repo_event/<owner>/<repo>` |

`settings`、`marketplace`、`topics`、`search`、`notifications`、`orgs`、`features` 等 GitHub 保留一级路径不转换。仓库名尾部的 `.git` 会移除。

### 微信公众号主页

| 输入 | 输出 |
|---|---|
| `mp.weixin.qq.com/mp/homepage?__biz=<biz>&hid=<hid>[&cid=<cid>]` | `/wechat/mp/homepage/<biz>/<hid>[/<cid>]` |

`__biz` 和 `hid` 都必须存在。`mp.weixin.qq.com/s/<article_token>` 保持原样，因为无法仅凭单篇文章 URL 得到历史消息主页参数。

### 小红书

| 输入 | 输出 |
|---|---|
| `xiaohongshu.com/user/profile/<user_id>` | `/xiaohongshu/user/<user_id>/notes` |

探索页和单篇笔记不转换。RSSHub 可以在无 Cookie 时尝试浏览器路径；失败时 Preview 返回实际反爬错误。

### TikTok

| 输入 | 输出 |
|---|---|
| `tiktok.com/@handle` | `/tiktok/user/@handle` |

`/video/<id>` 和 `/live` 不转换为用户 Feed。路由使用 Chromium。

## 数据流与持久化

添加订阅时：

1. 前端把用户输入的 URL 发送到 `POST /api/feeds/preview`。
2. `Fetcher.Preview` 调用 `ResolveFeedURL`，用解析后的地址抓取。
3. Preview 返回内容，但 `actual_url` 保留用户原始 URL。
4. 用户确认后，Feed 表保存原始平台 URL。
5. Worker 定时调用 `Fetcher.Fetch`，每次重新执行相同的确定性转换。

这样 RSSHub base 改变时无需迁移已有 Feed，也避免前端或 OPML 导出包含容器内部地址。

## 错误处理

- 非法 URL、相对 URL、非 HTTP(S) scheme：原样返回，维持现有调用契约。
- 缺少必须的 path/query 参数：原样返回，不拼接半成品路由。
- URL 中 query、fragment 不参与平台 ID 推导，微信公众号主页和 YouTube playlist 的必要 query 参数除外。
- 平台 URL 正确转换但 RSSHub 返回 4xx/5xx：沿用 Preview 的真实错误映射，不回退 HTML 抓取，避免把登录页或错误页误识别为 Feed。
- 未匹配平台：继续走现有 RSS/Atom 自动发现与 HTML synthetic feed fallback。

## 测试设计

`resolver_test.go` 采用表驱动测试，按平台分组覆盖：

- 每种受支持 URL 的规范形式；
- `www.`、移动域名、尾斜杠、query 和 fragment；
- ID 缺失、ID 类型错误和平台单篇内容 URL；
- GitHub 保留路径；
- 微信普通文章与缺少 `__biz`/`hid` 的主页；
- 空 `rsshubBase`、已解析 RSSHub URL、非法 URL 和非 HTTP(S) scheme；
- YouTube 官方 Feed 优先于 RSSHub。

实施遵循 RED-GREEN-REFACTOR：先加入上述期望并确认旧实现失败，再增加最小 resolver 逻辑，最后运行：

```bash
cd backend
go test -count=1 ./internal/rss
go test -count=1 ./...
```

## 验收标准

- 所有首批正向样例返回本设计指定的 Feed/RSSHub URL。
- 所有负向和语义不明确样例保持输入不变。
- 原有 B 站映射行为无回归。
- Preview、Fetch、FetchHTML 无需修改即可复用新增规则。
- 后端全量 Go 测试通过。
- rss-pal 主工作区中的既有未跟踪备份文件不被修改或提交。

## 后续扩展

- 在 Preview 响应中增加 provider、resolved route 和 route requirements，向用户展示 Cookie/浏览器依赖。
- 基于 RSSHub `/api/namespace` 提供高级路由搜索，而不是扩大硬编码自动匹配范围。
- 为必须登录的平台优先使用浏览器扩展的真实登录态抓取，避免在服务端长期保存 Cookie。
