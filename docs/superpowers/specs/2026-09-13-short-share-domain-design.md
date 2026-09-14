# RSS Pal 独立短域名分享设计

日期：2026-09-13

## 1. 目标

新创建的文章分享使用独立短域名：

```text
https://r.morefreeze.top/aZ7kP2mQ8xLs
```

完整 URL 约 37 个字符。打开后地址栏始终保持短地址，不跳转到现有长 token。短地址继续访问同一份不可变文章快照，并完整继承分享的到期、撤销、文章删除和资源鉴权语义。

已确认的产品边界：

- 使用独立域名 `r.morefreeze.top`。
- 短码固定为 12 位、大小写敏感的 Base62。
- 只有部署后的新分享生成短码。
- 历史普通分享和 legacy 分享不回填、不改写。
- 短域名直接承载公开阅读页，不做 `301` 或 `302` 跳转。

## 2. 不在本期范围

- 用户自定义、编辑或恢复短码。
- 为历史分享批量补短码。
- 点击统计、访客画像和分享分析。
- 二维码生成。
- 第三方短链服务。
- 专门为社交平台实现服务端 Open Graph 预览。
- 改变现有分享快照的内容、有效期或撤销规则。

## 3. 方案选择

采用数据库原生短码，不采用截断 HMAC 或独立短链服务。

原因：

- 短码与现有 `article_shares` 生命周期天然一致，无需同步两套状态。
- 撤销、过期、文章删除和本地资源鉴权可以复用现有实现。
- 不引入 Cloudflare KV 或第三方短链的可用性、隐私和运维依赖。
- 保留原有 `public_id + HMAC`，旧链接无需迁移。

## 4. 数据模型

在 `article_shares` 增加可空短码：

```sql
ALTER TABLE article_shares
ADD COLUMN short_code VARCHAR(12);

ALTER TABLE article_shares
ADD CONSTRAINT article_shares_short_code_format
CHECK (
    short_code IS NULL
    OR short_code ~ '^[0-9A-Za-z]{12}$'
);

CREATE UNIQUE INDEX article_shares_short_code_unique
ON article_shares (short_code)
WHERE short_code IS NOT NULL;
```

字段职责：

- `public_id`：内部管理 ID、撤销目标和现有 HMAC 长链接的输入。
- `short_code`：新链接的公开 bearer secret。
- `snapshot`：唯一的不可变文章快照；短链不复制快照。
- `expires_at`、`revoked_at`：新旧入口共享的生命周期状态。

历史记录迁移后 `short_code` 保持 `NULL`。新分享同时写入 `public_id`、`short_code` 和快照。管理和撤销接口继续使用 `public_id`，不把短码暴露为管理 ID。

系统不主动回收或复用已撤销、过期或删除分享的短码。文章删除后现有外键仍会级联删除分享记录；此后随机生成相同短码的概率由约 71-bit 熵控制，并由唯一约束在记录仍存在时兜底。本期不额外引入永久 tombstone 表。

## 5. 短码生成

短码字符集固定为：

```text
0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz
```

生成器使用 `crypto/rand` 和拒绝采样，避免对随机字节直接 `% 62` 造成模偏差。12 位 Base62 提供约 71-bit 熵。短码大小写敏感，不做大小写归一化、易混淆字符替换或人工纠错。

创建分享时：

1. 在受现有 RLS 事务约束的文章读取完成后构造一次快照。
2. 每次尝试生成一组新的 `public_id` 和 `short_code`。
3. 使用 `INSERT ... ON CONFLICT DO NOTHING RETURNING` 原子插入，避免唯一冲突令事务进入失败状态。
4. 冲突时整组重新生成，最多尝试 5 次。
5. 随机源失败或 5 次均冲突时返回 500，事务不产生半成品记录。

## 6. 后端组件

### 6.1 短码生成器

在 `internal/sharetoken` 中增加独立的短码生成纯函数。它只负责安全随机生成和字符集映射，不负责数据库重试、URL 拼接或分享生命周期。

### 6.2 仓储

`ShareRepository` 增加：

- 创建时保存 `short_code`。
- `GetActiveByShortCode(shortCode, now)`，同时约束未撤销且未过期。
- 扫描模型时读取可空 `short_code`。

历史 `GetActiveByPublicID` 和 `GetActiveByLegacyDigest` 保持不变。

### 6.3 分享处理器

新增公开接口：

```text
GET /api/s/:short_code
GET /api/s/:short_code/assets/:asset
```

短码必须先通过 `^[0-9A-Za-z]{12}$` 校验；无效格式不访问数据库。公开读取继续验证分享存在、未撤销、未过期且原文章仍存在。本地资源继续验证资源属于分享对应文章。

现有接口保持不变：

```text
GET /api/share/:token
GET /api/share/:token/assets/:asset
POST /api/articles/:id/shares
GET /api/articles/:id/shares
DELETE /api/articles/:id/shares/:share_id
```

分享正文中的本地资源需要按入口生成不同前缀：

- 长链接：`/api/share/:token/assets/`
- 短链接：`/api/s/:short_code/assets/`

资源重写仍只处理完整匹配的本地文章图片路径，不能把短码拼入攻击者控制的外部 URL。

### 6.4 管理响应

`shareManagementResponse.id` 继续返回 `public_id`。URL 规则为：

- `short_code` 非空：返回绝对地址 `https://r.morefreeze.top/<short_code>`。
- `short_code` 为空的普通历史记录：返回现有 `/share/v1_...`。
- legacy 记录继续沿用现有不可复制和兼容策略。

服务端负责构造短地址，前端不根据当前页面 hostname 猜测生产域名。

## 7. 配置

新增配置：

```text
SHORT_SHARE_ORIGIN=https://r.morefreeze.top
```

启动时必须满足：

- 配置必须精确等于 `https://r.morefreeze.top`。
- 不接受其他主机、显式端口、大小写、尾点、尾斜杠、path、query、fragment 或用户凭据变体。

配置无效时服务拒绝启动，避免生成不可用或指向错误站点的短链。本地端到端测试必须通过 hosts、DNS 或反向代理将精确域名 `r.morefreeze.top` 路由到本地实例；不允许用测试 origin 替代，也不为配置提供静默回退。

## 8. 独立短域名路由

`r.morefreeze.top` 指向与 `rss.morefreeze.top` 相同的生产入口，但使用独立 HTTPS virtual host。证书必须覆盖短域名。短域名不重定向到主站，而是内部转发到现有 frontend/API 容器。

短域名仅开放：

- `/<12 位短码>`。
- `/api/s/<短码>` 及其资源路径。
- 前端构建产物 `/assets/*`、favicon 等静态资源。
- 公开阅读器实际依赖的 `/api/proxy/image` 等只读公开媒体路径。

登录、注册、文章列表和其他私有页面不在短域名承载。未知路径以及未列入允许清单的 API 返回 404。具体 Nginx location 顺序必须保证 `/api/s/` 和静态资源先于 `/<short_code>` SPA fallback 匹配。

HTTP 请求统一升级到 HTTPS。页面、API 和本地资源继续返回 `Cache-Control: no-store`、`Referrer-Policy: no-referrer` 和 `X-Content-Type-Options: nosniff`。

## 9. 前端路由与页面

前端根据 hostname 选择路由表：

- `r.morefreeze.top`：只接受 `/:shortCode` 公开分享路由。
- 主站：继续接受 `/share/:token` 和现有私有路由。

因此主站上的未知一级路径不会被误判为短码。短码页面校验格式后请求 `/api/s/:shortCode`，并复用 `PublicArticleReader`。现有 `SharePage` 应抽取“加载公开快照”的小边界，使长 token 和短码分别选择 API，而不复制页面状态、错误处理或 head title 管理。

`MarkdownArticle` 的公开本地资源白名单需要同时接受严格格式的 `/api/share/.../assets/...` 和 `/api/s/.../assets/...`，不能放宽到任意 `/api/` 图片。

短域名页面上的承接链接必须使用绝对主站地址：

```text
https://rss.morefreeze.top/login?...
https://rss.morefreeze.top/register?...
```

“分享到 X”继续从当前 `origin + pathname` 构造公开链接，因此预填内容自然使用短地址，并继续排除 query、hash、原文 URL 和私有数据。预填文案沿用已确认的 140 字预算：保留短地址后，按“标题、尽可能完整的文章总结、链接”的优先级裁剪，最终只差用户点击发送。

## 10. 安全与隐私

短码本身是 bearer secret，不再追加 HMAC，否则无法实现目标长度。防护包括：

- 约 71-bit 随机熵。
- 数据库格式和唯一约束。
- 现有公开分享 IP 限流。
- 无效格式、未知、过期、撤销和文章删除统一返回相同 404。
- 不提供短码搜索、补全、目录或存在性差异。
- `Referrer-Policy: no-referrer` 防止访问外部链接时泄露短码。
- 应用业务日志不记录短码、完整短 URL 或正文。

当前服务使用 `gin.Default()`，默认访问日志会记录请求路径。实现时必须改为可脱敏的访问日志中间件，对 `/api/s/:short_code`、`/api/share/:token` 及其资源路径输出固定模板，不记录 bearer 值。短域名入口层同样不得把原始 URI 写入 Nginx access log；可以为该 virtual host 关闭 access log，或使用不含 request URI 的专用格式。

指标只按状态类别聚合，例如成功、404、限流和内部错误，不以短码作为 label。

## 11. 错误处理

- 随机源失败：500，不创建记录。
- 5 次唯一冲突：500，不创建记录。
- `SHORT_SHARE_ORIGIN` 非法：启动失败并输出不包含凭证的配置错误。
- 短码格式非法：统一 404，不查询数据库。
- 不存在、过期、撤销或文章删除：统一 404。
- 数据库查询失败：500；日志只记录操作名和内部错误，不记录短码。
- 单个图片或媒体失败：页面降级展示，不影响正文。
- 前端页面请求失败：保留现有重试入口。
- 复制 API 不可用：继续使用现有可选中文本框回退。

## 12. 兼容与回滚

- 历史记录不回填，`short_code` 为 `NULL`。
- 历史 HMAC 长链接继续访问 `/share/:token`。
- legacy 8 位链接继续按既定兼容期运行。
- 新分享仍保留 `public_id`，因此数据不会被新版本锁死。
- 数据库迁移是增量字段和索引，旧版本可以忽略该字段。
- 已发布短链依赖短域名、短码 API 和新前端路由；上线后不能只回滚其中一个组件。

## 13. 上线顺序

1. 为 `r.morefreeze.top` 配置 DNS，指向主站当前生产入口。
2. 签发并验证覆盖短域名的 HTTPS 证书。
3. 部署短域名 virtual host；此时未知短码返回 404。
4. 执行数据库增量迁移。
5. 配置并验证 `SHORT_SHARE_ORIGIN`。
6. 部署前端短域名路由；此时没有短码被公开生成。
7. 最后部署后端短码创建与读取接口；该版本上线即为新短链的启用边界。
8. 创建一条新分享并验证返回短地址。

在应用开始生成短链之前，必须先验证 DNS、TLS 和入口路由，避免发出不可访问的链接。

## 14. 测试

### 14.1 后端

- 短码固定 12 位且只包含 Base62 字符。
- 通过可注入字节流验证超出最大整除区间的随机字节会被拒绝采样，并覆盖随机源失败。
- 唯一冲突重试成功及第 5 次失败。
- 新分享返回配置 origin 下的绝对短地址。
- 历史普通分享和 legacy 分享响应不变。
- 短码读取完整快照及本地资源。
- 非法格式不查询数据库。
- 不存在、过期、撤销和文章删除统一 404。
- 撤销仍按所有者、文章 ID 和 `public_id` 鉴权。
- 资源重写不会向外部 URL 泄露短码。
- Gin 访问日志对长 token 和短码均脱敏。

### 14.2 数据库迁移

- 既有记录迁移后 `short_code IS NULL`。
- 格式约束、大小写敏感和唯一约束生效。
- 重复执行迁移不破坏已有数据。
- 新旧入口读取同一份快照。

### 14.3 前端

- 只有短域名的 12 位 Base62 根路径进入短分享页。
- 主站未知路径不被识别为短码。
- 短页请求 `/api/s/:short_code`，且不发生跳转。
- `PublicArticleReader` 的正文、图片、媒体和错误状态不回归。
- “分享到 X”只携带短地址，不携带旧 token、query 或 hash。
- 登录和订阅 CTA 始终指向主站。
- 无效短码显示统一失效页，不跳登录。
- 主站 `/share/:token` 行为保持不变。

### 14.4 Nginx 与生产验收

- HTTP 自动升级 HTTPS，TLS 主机名和证书链有效。
- 短地址返回 200，最终 URL 和地址栏保持短地址。
- 短域名私有页面、私有 API 和未知路径返回 404。
- Nginx 和应用访问日志均不包含短码。
- 撤销后页面和本地资源立即失效。
- DNS、直接生产入口和公网访问结果一致。

交付前运行完整后端测试、前端测试、legacy Nginx 测试、前端生产构建以及真实无痕浏览器验收。

## 15. 验收标准

1. 新分享返回约 37 字符的 `https://r.morefreeze.top/<12 位短码>`。
2. 无痕浏览器直接打开后可以阅读完整快照，地址栏不变化。
3. X 发帖预填内容按 140 字预算包含短地址和尽可能完整的文章总结，并继续只差用户点击发送。
4. 撤销或到期后，短页面和本地资源均不可访问。
5. 原有长分享链接继续正常工作。
6. 历史数据库记录没有被补短码或改写。
7. 页面、API 响应、应用日志和入口日志均不泄露用户私有数据；访问日志不记录 bearer token。
