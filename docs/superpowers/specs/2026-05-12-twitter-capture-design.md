# Twitter / X 推文网摘抓取设计

## 背景

RSS Pal 已有 bookmarklet 网摘抓取流程:用户在任意页面点书签按钮,前端把
`{url, title, html}` 发到 `POST /api/bookmarklet/capture`,服务端用
`FetchContentFromReader` 提正文,落到用户的「📑 收藏」saved feed。

这套通用提取在 Twitter / X 上表现很差 —— 页面是 React 渲染的,DOM 里夹着
大量回复 tweet 和侧边栏 noise,通用提取要么抓不到、要么把整个 timeline
当正文塞进去。Jina Reader 兜底对未登录 tweet 也时灵时不灵。

但用户在自己浏览器里**已经登录**twitter.com,看到的 DOM 是完整的。我们
只需要在服务端识别 Twitter URL,对 DOM 做 Twitter 专用解析,就能拿到干净
的推文内容。

例:`https://x.com/karpathy/status/2053872850101285137` —— 用户希望抓
focal tweet 的正文 + 图片,引用的 thread 用一个 markdown 链接占位即可。

## 目标与非目标

**目标**

- 用户在 `x.com` / `twitter.com` / `mobile.twitter.com` 的单条推文页面点
  现有 bookmarklet,得到一篇干净的 RSS Pal 文章(text + images + 引用
  链接)
- bookmarklet 代码**不**改动 —— 现有用户无需重新拖按钮
- 与 saved feed / dedup / AI summary 等下游链路无缝衔接
- 解析逻辑可用 HTML fixture 单测,Twitter 改 DOM 时由代码部署修复

**非目标**

- 抓取整条 thread(只抓 focal tweet)
- 抓取被引用 tweet 的正文(只留链接)
- 视频 / GIF / poll / link card 渲染
- 服务端通过 syndication API / Nitter 直连 Twitter
- profile 页面、搜索页面、列表页面的抓取
- 任何前端 UI 改动

## 架构

```
用户浏览器 (已登录 twitter.com)
    │ 在 https://x.com/karpathy/status/2053... 点 📑 bookmarklet
    │ (bookmarklet 行为不变,送 {url, title, html})
    ▼
POST /api/bookmarklet/capture
    │
    ▼
BookmarkletHandler.Capture
    1. NormalizeURL(url) —— 现有规则 + 新增:twitter.com/mobile.twitter.com → x.com
                                          x.com/<u>/status/<id> 整段 query 剥光
    2. IsTwitterStatusURL(normalizedURL) → statusID, ok
       │
       ├── ok = true → twitter.ExtractTweet(html, statusID)
       │     返回 TweetCapture{
       │         author, displayName, publishedAt,
       │         textMarkdown, imageURLs, quoteURL
       │     }
       │   handler 拼装 article.content,把 article.title / author /
       │   published_at 按 TweetCapture 填,后续走现有 get-or-create
       │   saved feed + dedup/overwrite 路径
       │
       │   ExtractTweet 返回 ErrTweetNotFound 时,fall through 到通用
       │   FetchContentFromReader(html) —— 至少存下一条文章,不返 422
       │
       └── ok = false → 现有 FetchContentFromReader 路径(完全不变)
    3. 后续:存 article、清 summary、worker 异步重算 summary —— 全部走
       现有代码,无变更
```

新增文件:
- `backend/internal/rss/twitter.go` —— `IsTwitterStatusURL`、`ExtractTweet`、
  `TweetCapture` 结构、`ErrTweetNotFound`
- `backend/internal/rss/twitter_test.go`
- `backend/internal/rss/testdata/twitter/*.html`

修改文件:
- `backend/internal/api/bookmarklet.go` —— Capture handler 增加 Twitter
  分支
- `backend/internal/util/url.go`(或现有 `NormalizeURL` 所在文件) ——
  新增 Twitter 主机/query 规则
- `backend/internal/api/bookmarklet_test.go` —— 新增一个 Twitter handler
  case

## 数据模型变更

无。所有字段塞进现有 `articles` 表;saved feed 已存在。

## 关键组件

### 1. `IsTwitterStatusURL`

纯函数:

```go
// IsTwitterStatusURL reports whether u is a single-tweet permalink on x.com
// or one of its legacy hosts. statusID is the numeric tweet id from the path.
// Profile pages, search pages, lists, and malformed paths return ok=false.
func IsTwitterStatusURL(u string) (statusID string, ok bool)
```

接受的 host(大小写无关):`x.com`、`www.x.com`、`twitter.com`、
`www.twitter.com`、`mobile.twitter.com`。

接受的 path 形态:`/<handle>/status/<numeric_id>`(允许尾部 `/`,允许
`<handle>` 含 `_` 和字母数字,`<numeric_id>` 必须是十进制数字串)。

拒绝:`/<handle>`、`/<handle>/with_replies`、`/search?q=...`、
`/i/lists/...`、空 path、非 Twitter 主机。

### 2. `NormalizeURL` 增量规则

加在现有 host-lowercase 之后、query-strip 之前:

1. host ∈ {`twitter.com`, `www.twitter.com`, `mobile.twitter.com`,
   `www.x.com`} → 改写成 `x.com`
2. host == `x.com` 且 path 匹配 `/<handle>/status/<id>` → query string
   整段丢掉(Twitter 的 `?s=20` / `?t=...` 都是分享追踪,无信息量)

`<handle>` casing 保留(Twitter handle 不区分大小写,但保留用户选择的
casing 看起来更自然;dedup 仍然靠 status id 的唯一性)。

新增 normalize test cases:

| 输入 | 期望输出 |
|------|----------|
| `https://twitter.com/x/status/1?s=20` | `https://x.com/x/status/1` |
| `https://mobile.twitter.com/x/status/1` | `https://x.com/x/status/1` |
| `https://x.com/x/status/1?t=abc&s=20` | `https://x.com/x/status/1` |
| `https://x.com/Karpathy/status/2053872850101285137` | `https://x.com/Karpathy/status/2053872850101285137` |
| `https://x.com/karpathy`(profile) | `https://x.com/karpathy` |

### 3. `ExtractTweet`

```go
type TweetCapture struct {
    Author       string    // handle, e.g. "karpathy"
    DisplayName  string    // display name, e.g. "Andrej Karpathy"
    PublishedAt  time.Time // from <time datetime="...">
    TextMarkdown string    // tweet text as markdown
    ImageURLs    []string  // pbs.twimg.com URLs, upgraded to ?name=large
    QuoteURL     string    // normalized x.com URL of quoted tweet, "" if none
}

var ErrTweetNotFound = errors.New("twitter: focal tweet not found in html")

func ExtractTweet(html string, statusID string) (*TweetCapture, error)
```

#### Focal tweet 选择

页面里通常有多个 `<article role="article" data-testid="tweet">`(focal
tweet + 回复 + 上下文)。挑选规则:

1. 遍历所有 `article[role="article"][data-testid="tweet"]`
2. 取其内部存在 `a[href]`,且 `href` 匹配正则
   `(?i)^/[^/]+/status/<statusID>($|[/?#])` 的那一个
3. 多个候选时,优先 `tabindex="-1"`(focal tweet 标记);仍有歧义则取
   第一个

匹配不到 → `ErrTweetNotFound`。

#### 字段提取

在 focal `<article>` 子树内:

- **TextMarkdown**:取 `[data-testid="tweetText"]`,自定义 walker:
  - text node → 原样追加
  - `<a href>` → `[innerText](href)`(href 是 Twitter 的 t.co 短链时也
    保留 —— 与展示一致)
  - `<img alt>`(text 区里的表情图)→ `alt` 字符(emoji)
  - `<br>` → `\n`
  - block-level child(`<div>`、`<span style="...">` 段落分隔)→ 末尾追
    加 `\n`,合并连续空行
  - 末尾 trim
  - 没有 `tweetText` 节点时 → 空字符串(可能是纯图片推文)
- **ImageURLs**:`[data-testid="tweetPhoto"] img[src]`,过滤 host =
  `pbs.twimg.com` 且 path 包含 `/media/`;每个 URL 把 query 里 `name=*`
  改成 `name=large`(没有 `name` 参数则不动)。**排除条件**:
  - host 含 `profile_images`(头像)
  - 节点祖先链上存在 `[role="link"]`(说明在引用 tweet 卡片内,这部分
    交给 QuoteURL 处理,不重复抓图)
- **QuoteURL**:在 focal article 内查 `[role="link"] a[href*="/status/"]`,
  过滤掉等于 focal 自身 permalink 的链接。剩下第一个的 `href` 拼上
  `https://x.com` 前缀后跑一遍 `NormalizeURL`。没有则 `""`
- **Author**:从传入的 `statusID` 不够,需要 handle —— 从同一个 focal
  article 内 `a[href^="/"][role="link"]`(指向 profile)的 href 取
  `/<handle>` 段。失败兜底:从外部传入(调用方有完整 URL,handler 那
  层拼好)
- **DisplayName**:`[data-testid="User-Name"]` 内第一个 `<span>` 的
  innerText
- **PublishedAt**:focal article 内 `time[datetime]` 的 `datetime`
  属性(RFC3339);解析失败时回退 `time.Time{}`,handler 那层判空再
  fallback 到 `time.Now()`

#### 退化路径

- focal article 缺失 → `ErrTweetNotFound`
- TextMarkdown 空 && ImageURLs 空 && QuoteURL 空 → `ErrTweetNotFound`
- TextMarkdown 空但有图 / 有引用 → 正常返回(image-only / quote-only
  推文)

### 4. Handler 拼装

`BookmarkletHandler.Capture` 在 normalize URL 之后插入:

```go
if statusID, ok := IsTwitterStatusURL(normalizedURL); ok {
    cap, err := twitter.ExtractTweet(req.HTML, statusID)
    if err == nil {
        title := buildTweetTitle(cap)
        content := buildTweetContent(cap)
        var publishedAt *time.Time
        if !cap.PublishedAt.IsZero() {
            t := cap.PublishedAt
            publishedAt = &t
        }
        // → existing get-or-create saved feed + dedup/overwrite path,
        //   using these fields instead of FetchContentFromReader output
        return
    }
    // err == ErrTweetNotFound → fall through, log a warning
    log.Printf("twitter extract failed for %s, falling back to general extractor: %v", normalizedURL, err)
}

// existing general path unchanged
```

`buildTweetContent` 模板:

```
> @{handle} ({displayName}) · {YYYY-MM-DD}

{textMarkdown}

![]({image1})

![]({image2})

…

引用: {quoteURL}
```

空白处理:
- byline 行字段缺失时退化:
  - 仅 handle:`> @{handle}`
  - 没 displayName:`> @{handle} · {date}`
  - 没 date(`PublishedAt` 零值):`> @{handle} ({displayName})`
- 任一区块为空就整段跳过(包括前后空行)
- 仅当至少一张图片时才出现图片区块
- 仅当 `quoteURL != ""` 时才出现引用行

`buildTweetTitle`:
- `textMarkdown != ""` → 取前 60 个 rune,如果实际长度更长追加 `…`,把
  换行替换成空格
- 否则 → `"@" + handle + " 的推文"`

## Article 字段映射

| `articles` 列 | 来源 |
|---|---|
| `url` | `NormalizeURL(req.URL)` |
| `title` | `buildTweetTitle(cap)` |
| `author` | `buildTweetAuthor(cap)` |
| `published_at` | `cap.PublishedAt`,零值 fallback `time.Now()` |
| `content` | `buildTweetContent(cap)` |
| `feed_id` | 用户的 saved feed(get-or-create,现有逻辑) |
| `summary_brief`, `summary_detailed` | NULL,worker 异步补 |

dedup / overwrite 规则完全沿用现有 capture handler:同 owner_id 下按
normalized URL 命中已有文章 → 看 `shouldPromptDuplicate` 比较长度和图片
数,决定 silent overwrite / prompt / unchanged。

## 错误处理

| 场景 | 行为 |
|---|---|
| URL 不是 Twitter status | 走通用路径(完全不变) |
| Twitter URL,但 `ExtractTweet` 返 `ErrTweetNotFound` | 日志一行 warn,fall through 到通用路径 |
| Twitter URL,`ExtractTweet` 成功但 `TextMarkdown`/`ImageURLs`/`QuoteURL` 全空 | 上面规则已转成 `ErrTweetNotFound`,fall through |
| `req.HTML` 缺失 | 现有 capture handler 已有校验,return 400(不变) |
| body 超过 4 MiB | 现有 413 路径(不变) |

不引入新的 HTTP 状态码或错误消息文案。

## 测试

### Unit tests

`backend/internal/rss/twitter_test.go`:

- `TestIsTwitterStatusURL` —— 表驱动:
  - x.com / twitter.com / mobile.twitter.com 单 tweet URL 正向
  - profile / search / lists / 无 status / 非数字 id / 空 path 负向
  - 大小写混合 host
- `TestExtractTweet_TextOnly` —— fixture `tweet_text_only.html`,断言
  `TextMarkdown` exact 字符串、`ImageURLs` 长度 0、`QuoteURL` 空
- `TestExtractTweet_WithImages` —— fixture `tweet_with_images.html`,
  断言 `ImageURLs` 每个 URL 都含 `name=large`、host 是 `pbs.twimg.com`
- `TestExtractTweet_WithQuote` —— fixture
  `karpathy_2053872850101285137.html`,断言 `QuoteURL` 非空且不等于
  focal permalink、host 是 `x.com`
- `TestExtractTweet_ImageOnly` —— fixture `tweet_image_only.html`,
  `TextMarkdown == ""` 但 `ImageURLs` 非空,无 error
- `TestExtractTweet_NotFound` —— fixture `tweet_not_found.html`(profile
  页 或被删 tweet),返回 `ErrTweetNotFound`

`NormalizeURL` 现有测试表追加上面列出的 5 个 case。

### Handler test

`backend/internal/api/bookmarklet_test.go` 增加一个 case:

- POST `/api/bookmarklet/capture` 带 Twitter URL + `karpathy_2053872850101285137.html`
- 断言 created article 的 `title` 是 tweet 前 60 字符 + `…`
- 断言 `author` 形如 `"Andrej Karpathy (@karpathy)"`
- 断言 `content` 以 tweet 文本开头、末尾包含 `引用: https://x.com/.../status/...`
- 断言 `url` 是 normalized `https://x.com/karpathy/status/2053872850101285137`

### Fixture 准备

`backend/internal/rss/testdata/twitter/`:

| 文件 | 用途 |
|---|---|
| `karpathy_2053872850101285137.html` | 文 + 引用 thread,主验 quote |
| `tweet_with_images.html` | 1–4 张内联图,主验 image 提取 |
| `tweet_text_only.html` | 无媒体 baseline |
| `tweet_image_only.html` | 空文 + 图,主验 title fallback |
| `tweet_not_found.html` | profile 页或 404,主验 fallback 路径 |

获取方式:用 `agent-browser` skill 的 `twitter` session(headed 一次完成
登录,后续 headless 复用 cookies),访问目标 URL,保存
`document.documentElement.outerHTML` 到 testdata 目录。Fixture 不进
secrets:保存前去掉 `<meta name="..." content="..." />` 里任何 csrf /
auth token,以及任何 `<script>` 里包含 token 字面量的内联脚本(grep
`auth_token` / `ct0` 删除整个 script 块)。

不做 live integration test —— fixture 是契约,Twitter 改 DOM 时表现是
某个 fixture 重新保存后 ExtractTweet test 红,修代码 / 重存 fixture 至
绿。

## 上线 / 部署

- 无 schema migration。
- 无新增环境变量。
- 无 bookmarklet 代码改动 —— 现有用户 token / 拖好的书签按钮**不**失
  效,直接生效。
- 部署后,既有 saved feed 内若已有用通用提取存的 Twitter 文章,下次用
  bookmarklet 重抓同一 URL 会:
  - normalize 命中已有 article(twitter.com → x.com 改写后等价)
  - 走 `shouldPromptDuplicate`:Twitter 通用提取通常很短/无图,新内容
    会更长、可能更多图 → silent overwrite,清 summary,worker 重算
  - 用户感知:旧 Twitter capture 自动得到一次清洗
- rollback:回滚 backend 即可,数据形态没变。

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| Twitter 改 DOM(改 `data-testid`、改 class) | fixture-driven test 立刻红;改 selector 重新保存 fixture 即可,无需 schema/迁移 |
| 用户在 x.com 未登录,bookmarklet 抓到的 HTML 缺少 focal tweet | `ErrTweetNotFound` → fall through 到通用提取,至少存条文章,不 422 |
| 长推 / 多段推 `tweetText` 被 Twitter 折叠("Show more") | 折叠 tweet 在 DOM 里实际仍是完整文本(`Show more` 仅是 CSS 截断),walker 抓全文 —— 见 fixture `tweet_text_only.html` 选用一条长推作为回归 |
| 引用链不止一层(focal 引用 A,A 引用 B) | 我们只抓 focal 的 `QuoteURL`,引用链向下展开是非目标 |
| 视频推文 | 视频暂不抓;`tweetPhoto` 选择器不匹配视频,视频 tweet 的 `TextMarkdown` 仍会被存下,体验上等同于 text-only |
| 推文里 t.co 短链 | 保留原文不展开 —— 与浏览器里看到的展示一致;reader 渲染时仍是可点击链接 |

## 不在本期(给未来)

- 整条 thread 抓取(walk 上下游 self-replies)
- 引用 tweet 递归展开成 blockquote
- 视频 / GIF 内联播放
- 服务端通过 syndication API 抓未登录的公开 tweet(对未登录场景的补强)
- 浏览器扩展替代 bookmarklet(可绕过 4 MiB 限制 + 拿到更结构化数据)
