# Bookmarklet 浏览器抓取设计

## 背景

RSS Pal 现有抓取链路为「直连 HTTP scrape → Jina Reader 兜底」。但仍有一类 URL 抓不到正文:

- 登录墙 / paywall 页面(Jina 也读不到登录后的内容)
- Cloudflare 强校验、JS 挑战
- 某些 Next.js / 重 SPA 页面,Jina 渲染结果与真实浏览器不一致

例:`https://productonboarding.com/articles/why-product-tours-get-skipped`(article 993)直连 240 字符,Jina 输出虽长但仍是 boilerplate 居多。这类页面只有用户的真实浏览器能看到完整正文。

本设计提供一种由用户浏览器抓取并回传给 RSS Pal 的方法。除了修复短内容文章,顺带支持「随手收藏任意网页」的工作流。

## 目标与非目标

**目标**

- 用户可以在第三方网页一键把当前页面正文发回 RSS Pal
- 已有同 URL 文章则更新 content;否则在用户专属"📑 收藏" feed 下新建文章
- 与现有 AI summary、阅读进度等下游链路无缝衔接(新内容自动重算 summary)
- token 可吊销

**非目标**

- 浏览器扩展(后续可选,不在本期)
- 移动端 share extension
- 批量抓取(用户每次点一下,只发当前页)

## 架构

```
                                    用户浏览器
                          ┌─────────────────────────────┐
                          │  访问任意第三方文章页面     │
                          │      ↓ 点击书签栏按钮       │
                          │  bookmarklet JS:            │
                          │   fetch(rss-pal/api/...)    │
                          │   带 Bearer <token>         │
                          │   body: {url,title,html}    │
                          └──────────────┬──────────────┘
                                         │ CORS POST
                                         ▼
┌────────────────────────────────────────────────────────────┐
│                     RSS Pal API                            │
│   POST /api/bookmarklet/capture (CORS, 非 JWT)             │
│     1. 验证 bookmarklet_token → user                       │
│     2. 规范化 URL(去 fragment / 去追踪参数)                │
│     3. FetchContentFromReader(html) 提取正文               │
│     4. JOIN articles ON feeds.owner_id = user.id           │
│        按规范化 URL 在 user 拥有的所有 feed 里查找          │
│        ├─ 命中且 new > existing:覆盖,清空 summary         │
│        ├─ 命中但更短:返回 unchanged                        │
│        └─ 未命中:get-or-create user 的 saved feed,新建    │
│     5. 异步触发 AI summary(同 worker 现有路径)            │
│     6. 返回 {status, article_id, message}                  │
└────────────────────────────────────────────────────────────┘
```

## 数据模型变更

migration `007_bookmarklet.sql`:

```sql
-- 用户级长效 token,用于 bookmarklet 鉴权
ALTER TABLE users ADD COLUMN bookmarklet_token VARCHAR(64);
CREATE UNIQUE INDEX idx_users_bookmarklet_token
    ON users(bookmarklet_token)
    WHERE bookmarklet_token IS NOT NULL;
```

`feeds.feed_type` 已存在(migration 006),新增一个允许值 `'saved'`,无需 schema 变更。每个用户首次使用 bookmarklet 时懒加载该 feed,字段填法:

| 列 | 值 | 说明 |
|----|----|------|
| `title` | `📑 收藏` | (注:列名是 `title` 不是 `name`) |
| `url` | `bookmarklet://user/<owner_id>` | 哨兵 URL,每用户唯一 —— `feeds.url` 有全局 UNIQUE 约束,不能用空字符串 |
| `feed_type` | `saved` | |
| `owner_id` | `<user.id>` | (注:列名是 `owner_id` 不是 `user_id`) |
| `is_active` | `true` | 保持激活以便在前端 feed 列表正常显示 —— worker 通过 `feed_type` 过滤来跳过抓取(见下) |

每个用户至多一条 `feed_type='saved'` 的 feed,用 `(owner_id, feed_type='saved')` 查得唯一。

**Worker 行为变更**:`feedRepo.GetActiveFeeds()`(或等价的 worker 拉 feed 列表的查询)需追加 `feed_type IN ('rss', 'html')` 条件,把 saved feed 排除在定时抓取外 —— 否则 worker 会拿 `bookmarklet://user/<id>` 这种哨兵 URL 去 `gofeed.ParseURL`,每分钟报错一次。

## API 设计

### POST /api/bookmarklet/capture

**鉴权**:`Authorization: Bearer <bookmarklet_token>` —— **不**走 JWT 中间件,使用独立的 token 校验。

**CORS**:允许任意 origin。需要正确处理 `OPTIONS` 预检:

```
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: POST, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
Access-Control-Max-Age: 86400
```

**请求体**(`application/json`,1 MB 上限,超出返回 413):

```json
{
  "url":   "https://example.com/article?utm_source=...",
  "title": "Article Title",
  "html":  "<!DOCTYPE html>..."
}
```

**响应**:

| 场景 | HTTP | body |
|------|------|------|
| 更新已有 | 200 | `{"status":"updated","article_id":123,"message":"已更新文章: Article Title"}` |
| 新建 | 201 | `{"status":"created","article_id":456,"message":"已收藏: Article Title"}` |
| 已有内容更长,跳过 | 200 | `{"status":"unchanged","article_id":123,"message":"已有内容更完整,未覆盖"}` |
| token 无效 | 401 | `{"error":"无效的 bookmarklet token"}` |
| body 超过 1MB | 413 | `{"error":"内容过大"}` |
| HTML 解析失败 | 422 | `{"error":"无法从页面提取正文"}` |

服务端字段处理:

- `url`:必填,会被规范化(见下)再用于查重
- `title`:可空,空则用规范化 URL 的 path 作为占位
- `html`:必填,经 `goquery.NewDocumentFromReader` 解析后调用现有 `FetchContentFromReader` 提取主体文字

**查重范围**:命中条件是 `articles.url == normalized_url AND feeds.owner_id == authenticated_user.id`(走 JOIN)。**不**限制只在 saved feed 里找 —— 用户在普通 RSS feed 已订阅的文章被 bookmarklet 重新抓也应该更新原文章,而不是在 saved feed 里再造一份重复。

**覆盖规则**:命中已有文章后,只有 `len(new_content) > len(existing_content)` 才覆盖;覆盖时同时清空 `summary_brief` 和 `summary_detailed`,worker 的 `backfillSummaries` 循环会自动重算(无需新加触发逻辑)。

### GET /api/settings/bookmarklet-token

JWT 鉴权,返回当前用户的 token:

```json
{ "token": "abc123..." }   // 没生成过则 token 为 null
```

### POST /api/settings/bookmarklet-token/regenerate

JWT 鉴权,旋转 token:生成新的 32 字节随机串(hex 编码 → 64 字符),写回 `users.bookmarklet_token`,旧 token 立即失效。响应同 GET。

## URL 规范化

实现为独立纯函数 `NormalizeURL(raw string) string`,带单测,被 `capture` 端点和未来可能的工具调用复用。

规则:

1. 解析失败 → 原样返回
2. 去 `#fragment`
3. 去追踪参数(查询参数键名匹配以下任一即剔除):
   - 前缀 `utm_`
   - 完全匹配:`gclid`, `fbclid`, `mc_cid`, `mc_eid`, `_hsenc`, `_hsmi`, `ref`, `ref_src`, `igshid`, `yclid`, `msclkid`
4. host 转小写
5. protocol **保留原值**(不强行 https,避免与已存的 http 记录失配)
6. 路径末尾 `/` 保留(常见 RSS feed 直接给带尾斜杠的 URL)

### 测试用例

| 输入 | 期望输出 |
|------|----------|
| `https://Example.com/a?utm_source=x&id=1#sec` | `https://example.com/a?id=1` |
| `https://example.com/a?gclid=xx` | `https://example.com/a` |
| `https://example.com/a` | `https://example.com/a` |
| `not a url` | `not a url` |
| `https://example.com/a?utm_source=x` | `https://example.com/a` |
| `https://example.com/a?id=1&utm_medium=y&id2=2` | `https://example.com/a?id=1&id2=2` |

## 前端

### Settings 页新增「📌 浏览器抓取」区块

布局(放在现有 AI 配置 / summary 模板区块之后):

```
📌 浏览器抓取
─────────────────────────────────────────
把书签栏里这个按钮拖到浏览器书签栏,
在任意网页点一下就能把正文发回 RSS Pal。

[ 📑 发送到 RSS Pal ]   ← 可拖动的 <a href="javascript:...">

Token: abc123…  [👁 显示 / 隐藏]  [🔄 重新生成]

⚠️ 重新生成后旧书签会失效,需要重新拖一次。
```

### Bookmarklet 代码

构造时机:在 Settings 页拉到 token 之后,前端把 token 和 `window.location.origin` 拼进模板。模板:

```js
javascript:(function(){
  fetch('<API_BASE>/api/bookmarklet/capture',{
    method:'POST',
    headers:{
      'Content-Type':'application/json',
      'Authorization':'Bearer <TOKEN>'
    },
    body:JSON.stringify({
      url:location.href,
      title:document.title,
      html:document.documentElement.outerHTML
    })
  })
  .then(function(r){return r.json().then(function(j){return{ok:r.ok,j:j}})})
  .then(function(x){toast(x.ok?x.j.message:'错误: '+(x.j.error||r.status))})
  .catch(function(e){toast('错误: '+e.message)});
  function toast(m){
    var d=document.createElement('div');
    d.style.cssText='position:fixed;top:20px;right:20px;z-index:2147483647;padding:12px 16px;background:#222;color:#fff;border-radius:8px;font:14px -apple-system,sans-serif;box-shadow:0 4px 12px rgba(0,0,0,.3);max-width:320px;';
    d.textContent='RSS Pal: '+m;
    document.body.appendChild(d);
    setTimeout(function(){d.remove()},3000);
  }
})();
```

整段会被 `encodeURIComponent` 包装到 `<a href="javascript:...">` 里,空白字符压缩。

注意点:

- 使用 `var` + 老式 function 表达式(避免某些站点 CSP 对 ES6 的处理差异)
- 不依赖任何外部库
- toast 容器加 `z-index:2147483647`(int32 max),避免被站点遮挡
- API_BASE 使用用户当前访问 RSS Pal 的 origin(部署在 VPS 时不会硬编码 localhost)

### 不影响其他页面

文章详情页 / 列表页 / 推荐页一律不动。新建的"📑 收藏" feed 会和别的 feed 一起出现在 feed 列表(可以过滤/重命名),`feed_type='saved'` 和 `'rss'`/`'html'` 在后端文章查询中无差别。

## 后端组织

新文件:

- `backend/internal/api/bookmarklet.go` —— `CaptureHandler`,自带轻量 token 中间件
- `backend/internal/api/settings_bookmarklet.go` —— GET/regenerate 两个 JWT 端点
- `backend/internal/util/urlnorm.go` —— `NormalizeURL` + 单测

修改:

- `backend/internal/repository/article.go` —— 新增 `FindByOwnerAndURL(ownerID int, url string)`(SELECT articles JOIN feeds ON articles.feed_id=feeds.id WHERE feeds.owner_id=$1 AND articles.url=$2)
- `backend/internal/repository/feed.go` —— 新增 `GetOrCreateSavedFeed(ownerID int)`(查 `owner_id=$1 AND feed_type='saved'`,没有就插入)
- `backend/internal/repository/user.go` —— 新增 `GetByBookmarkletToken(token string)`、`SetBookmarkletToken(userID int, token string)`
- `backend/internal/api/router.go`(或路由注册处)—— 注册新端点;capture 端点单独走 token 中间件,不挂 JWT
- `backend/internal/repository/feed.go::GetActiveFeeds`(或 worker 实际调用的取 feed 列表方法)—— 加 `feed_type IN ('rss','html')` 过滤
- `backend/migrations/007_bookmarklet.sql` —— 新增

## 错误处理

| 失败点 | 处理 |
|--------|------|
| token 无 / 错 | 401,toast 显示「错误: 无效的 token」 |
| 网络不通 / CORS 失败 | bookmarklet 的 `.catch` 兜底,toast 显示「错误: <message>」 |
| HTML 提取出空 | 422,toast 显示「错误: 无法提取正文」 |
| body 超 1MB | 413,toast 显示「错误: 内容过大」 |
| DB 写入失败 | 500,worker 的 backfillSummaries 不会被触发(因为没 commit) |

## 测试

**单测**(`backend/internal/util/urlnorm_test.go`、`backend/internal/api/bookmarklet_test.go`):

- URL 规范化全部用例(见上)
- token 中间件:有效/无效/缺失/格式错误
- capture handler:
  - 已有文章 + 新内容更长 → 200 updated,DB content 更新,summary 字段被清空
  - 已有文章 + 新内容更短 → 200 unchanged,DB 未变
  - 无对应文章 → 201 created,saved feed 自动创建,DB 插入
  - 第二次新建 → 复用已有 saved feed,不重复创建
  - body > 1MB → 413
  - HTML 提取空 → 422

**手动验证**:
1. 在 Settings 重新生成 token,把书签拖到 Chrome 书签栏
2. 打开 `https://productonboarding.com/articles/why-product-tours-get-skipped`,点书签
3. 看右上角浮层显示「已更新文章: Why Most Product Tours Get Skipped」
4. 回到 RSS Pal,article 993 的 content 应已被浏览器抓的版本覆盖
5. 打开一个不在订阅里的网页(随便一篇博客),点书签,验证「📑 收藏」feed 自动创建并出现新文章

## 安全考量

- token 只在 Settings 显示;前端默认隐藏,需点「显示」才出现明文
- `bookmarklet_token` 列在 users 表里加 unique 索引,泄露后用户可立即重新生成,旧 token 失效
- capture 端点的 CORS 设为 `*` 是必要的(用户可能在任意第三方域名触发);token 鉴权扛住了滥用,不是靠 origin 限制
- 不复用 JWT 的原因:JWT 短时效 + 跨 origin 难取得;长效 token 单独存反而更可控
- 服务端日志只记录 url 和 article_id,不记录请求体 html(避免日志膨胀和敏感内容泄露)
- 1 MB body 上限挡住了大体积滥用

## 部署

无需 docker-compose 改动。新 migration 在 `/docker-entrypoint-initdb.d` 自动执行(首次启动时);已有数据库需要手动跑一次:

```bash
docker exec -i rss-pal-postgres-1 psql -U postgres -d rsspal < backend/migrations/007_bookmarklet.sql
```
