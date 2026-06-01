# RSS Pal

个人 RSS 阅读服务，支持 AI 总结、个性化推荐和多用户管理。

## 功能特性

- **智能订阅抓取** — 支持 RSS/Atom 订阅，也支持任意网页（自动识别文章链接），添加前预览内容
- **AI 驱动总结** — 每篇文章自动生成要点摘要 + 详细分析，支持自定义模板
- **PDF 网摘** — 三种入口（Chrome 内置 viewer / 本地 file:// / 直接粘 URL）抓取 PDF，数字版秒级入库，扫描版自动走 OCR（中简+英）；图片按页穿插，每篇上限 100 张去重图
- **个性化推荐** — 基于点赞/收藏/阅读时长学习兴趣，生成个性化文章推荐
- **阅读进度追踪** — 记录滚动位置，下次打开自动恢复
- **多用户支持** — 邀请码注册，最多 10 名测试用户
- **搜索** — 全文搜索文章标题、摘要和正文
- **未读计数** — 导航栏显示未读数量，支持一键全部已读
- **文章分享** — 生成公开分享链接，展示 AI 总结

## 快速开始

### 使用 Docker Compose（推荐）

1. 克隆并配置环境变量：

```bash
cp .env.example .env
# 编辑 .env，填入 AI API 密钥和管理员密码
```

2. 启动：

```bash
docker-compose up -d
```

3. 访问 http://localhost，首次使用会自动创建管理员账号

### 手动部署

```bash
# 后端
cd backend
go mod tidy
psql -U postgres -d rsspal -f migrations/001_init.sql
go run ./cmd/server   # API 服务，端口 8080
go run ./cmd/worker   # RSS 抓取 Worker（另开终端）

# 前端
cd frontend
npm install
npm run dev   # 开发模式，代理到 :8080
```

## 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `SERVER_PORT` | `8080` | API 服务端口 |
| `DB_HOST` | `localhost` | PostgreSQL 主机 |
| `DB_PORT` | `5432` | PostgreSQL 端口 |
| `DB_USER` | `postgres` | 数据库用户名 |
| `DB_PASSWORD` | `postgres` | 数据库密码 |
| `DB_NAME` | `rsspal` | 数据库名称 |
| `CLAUDE_API_KEY` | — | AI API 密钥（OpenAI 兼容格式） |
| `CLAUDE_BASE_URL` | `https://api.anthropic.com` | AI API 地址 |
| `AUTH_PASSWORD` | `admin` | 管理员初始密码 |
| `JWT_SECRET` | — | JWT 签名密钥（**生产环境必须设置**） |

## 云服务器部署（生产环境）

### 需求基线

rss-pal 跑全套服务（postgres + api + worker + frontend + rsshub + status-monitor），实测：

- **CPU**: 2 核起步；worker AI 摘要并发跑 OCR/视觉时建议 4 核
- **内存**: 4 GB 起步（含 rsshub-chromium，仅 chromium 就要 ~1 GB）
- **磁盘**: 50 GB 起步（PostgreSQL + AI summary cache + PDF 网摘图片 + 备份）
- **带宽**: 5 Mbps 够个人用；抓取 RSS 是上传消耗，不大
- **公网 IP**: 必须独立 IP（共享 IP 不能装 SSL）

### 国内云厂商对比（2026 年价格，3 年付）

| 厂商 | 套餐 | 配置 | 3 年价格 | 月均 | 续费 | 备注 |
|------|------|------|---------|------|------|------|
| **阿里云轻量** | 2C4G | 2核4G / 200M峰值带宽 / 50G SSD / 不限流量 | ¥597 | ¥16.6 | ¥199/年同价 | 续费不涨价，性价比最高 |
| **腾讯云轻量** | 2C4G6M | 2核4G / 6M带宽 / 80G SSD / 1200G/月流量 | ¥528 | ¥14.7 | 同价 | "买1年送3个月"活动可叠加 |
| **京东云轻量** | 2C4G5M | 2核4G / 5M带宽 / 60G SSD / 500G/月流量 | ¥528–558 | ¥14.7 | 看活动 | 价格接近腾讯但生态弱 |
| **华为云 Flexus L** | 2C4G | 2核4G / 5M带宽 / 80G SSD | ~¥800（年付 ~¥268） | ~¥22 | 略涨 | 没看到明确 3 年套餐 |
| **UCloud 快杰共享** | 2C4G5M | 2核4G / 5M带宽 | ¥1398 | ¥38.8 | 涨价 | 偏企业，个人贵 |
| **Oracle Cloud Always Free** | A1.Flex | 4 OCPU ARM / 24G RAM / 47G | **¥0** | ¥0 | 永久免费 | 当前部署用的就是这个；国内访问绕路 |

**结论**：
1. 当前 Oracle Cloud Always Free（4 OCPU ARM + 24G RAM）规格远超任何国内付费套餐，且永久免费。国内访问慢是唯一短板。
2. 如果一定要切国内：**腾讯云轻量 2核4G6M / 528 元 3 年**最划算，6M 带宽对外服务体验好于阿里云轻量的"200M 峰值"（峰值通常只跑 30 秒）。
3. 阿里云轻量"续费不涨价"是它独有的承诺，3 年到期后续费仍 199/年，长期持有更划算。
4. 警惕首年特价：很多套餐 3 年后续费按原价（2核4G 标价 ~¥1000/年），下单前查清续费规则。
5. **不要买 1C2G**——chromium-bundled rsshub 单进程就接近 1 GB 内存，OOM 风险高。

### 部署前安全检查（**手动**，部署 README 不会自动做）

`scripts/deploy-oracle.sh` 扫描出以下需要你手动确认的风险：

#### A. 必须改的弱默认值（漏改会被打）
- [ ] **`.env` 里 `JWT_SECRET`**：默认是 `rss-pal-secret-change-in-production`（硬编码在 `deploy-oracle.sh:184` 和 `docker-compose.yml:42`）。生成方式：
  ```bash
  openssl rand -hex 32   # 复制到 JWT_SECRET=
  ```
  确认命令：`grep JWT_SECRET /opt/rss-pal/.env`，确保不是默认字符串。
- [ ] **`.env` 里 `AUTH_PASSWORD`**：默认 `admin`（`deploy-oracle.sh:182`）。改成 16 位以上随机字符串。首次登录后通过 UI 再改一次。
- [ ] **PostgreSQL 密码**：`docker-compose.yml:8` 写死 `POSTGRES_PASSWORD: postgres`，且 `DB_PASSWORD: postgres` 也写死在 `api`/`worker` 环境。本地开发无所谓，**云上必须改**：把两处都改成 `${DB_PASSWORD}` 并在 `.env` 里设强密码。
- [ ] **`CLAUDE_API_KEY` 不要回传**：检查 `.env` 文件权限 `chmod 600 /opt/rss-pal/.env`，确保非 root 用户读不到。

#### B. 网络暴露面（云上 firewall 配错就漏）
- [ ] **PostgreSQL 5432 端口不要暴露**：`docker-compose.yml:14` 当前是 `"5432:5432"`，即绑定到 0.0.0.0。Oracle Cloud / 阿里云安全列表如果配错（开了 0.0.0.0），数据库直接公网可达。**改成** `"127.0.0.1:5432:5432"`。
- [ ] **API 8080 端口同理**：`docker-compose.yml:53` 的 `"8080:8080"` 建议改为 `"127.0.0.1:8080:8080"`，让外网只能通过 nginx 80/443 访问。
- [ ] **status-monitor 8090** 同上：`docker-compose.yml:127`，改 `127.0.0.1:8090:8090`。
- [ ] **云厂商安全组只开** `22 / 80 / 443`，且 22 端口源限定你自己的 IP（不要 0.0.0.0/0）。
- [ ] **服务器 firewall** 与云安全组双重防护：`ufw status` 或 `firewall-cmd --list-all` 确认。

#### C. SSH 加固
- [ ] **禁用密码登录**：`/etc/ssh/sshd_config` 设 `PasswordAuthentication no`、`PermitRootLogin no`。仅允许密钥登录。
- [ ] **改默认端口**（可选）：`Port 22222` 之类，配合云安全组同步改。
- [ ] **fail2ban**：`apt install fail2ban` / `dnf install fail2ban`，启用 sshd jail。
- [ ] **SSH key 离线备份**：`scripts/create-oracle-instance.sh:14` 生成的 `~/.ssh/rss-pal-key` 是登录唯一凭证，丢了进不去。

#### D. TLS / HTTPS
- [ ] **必须申请 Let's Encrypt 证书**：`deploy-oracle.sh:347` 的 `certbot --nginx -d $DOMAIN` 必须跑。**不要**用 IP 直接对外暴露 HTTP（admin token 走 Authorization 明文头）。
- [ ] **certbot 自动续期**：`systemctl status certbot.timer` 确认。Let's Encrypt 90 天有效期。
- [ ] **HSTS / 安全头**：当前 `frontend/nginx.conf` 没有加 `Strict-Transport-Security`、`X-Frame-Options`、`X-Content-Type-Options`、`Referrer-Policy`、`Content-Security-Policy`。生产环境建议在 nginx server 块里加：
  ```nginx
  add_header Strict-Transport-Security "max-age=63072000; includeSubDomains" always;
  add_header X-Content-Type-Options "nosniff" always;
  add_header X-Frame-Options "SAMEORIGIN" always;
  add_header Referrer-Policy "strict-origin-when-cross-origin" always;
  ```
- [ ] **TLS 协议**：`frontend/nginx.conf` 未显式设置 `ssl_protocols`，依赖 nginx 默认；建议显式 `ssl_protocols TLSv1.2 TLSv1.3;`。

#### E. 供应链与脚本执行
- [ ] **`deploy-oracle.sh:106` 直接 pipe `https://get.docker.com | sh`**：官方脚本但仍是供应链风险。生产可改成：
  ```bash
  curl -fsSL https://get.docker.com -o /tmp/get-docker.sh
  # 人工审查 /tmp/get-docker.sh
  sh /tmp/get-docker.sh
  ```
- [ ] **`auto_deploy.sh` 自动 `git pull + docker compose up --build`**：如果 GitHub 仓库或你的账号被劫持，恶意 commit 凌晨 3 点自动跑到生产。生产环境建议：
  - 关掉 `auto_deploy.sh` cron（`crontab -e` 删除那行）
  - 或者把它改成"只拉取，不重启"，需要人工 `docker compose up -d --build` 才生效
  - 或者用 commit signature 校验：`git verify-commit HEAD` 失败就退出
- [ ] **`package-lock.json` 被 `sed -i` 改镜像源**（`deploy-oracle.sh:160`）：会破坏 npm 的 integrity 校验（lock 文件的 hash 仍是原镜像的）。功能上 OK 但 supply chain 校验降级。

#### F. 数据库与备份
- [ ] **备份目录权限**：`/opt/rss-pal/backups/` 含全量数据库 dump。`chmod 700 /opt/rss-pal/backups` 且 owner 不要是其他用户。
- [ ] **异地备份**：当前备份只在本机。Oracle Cloud 实例如果被删/坏盘，数据全没。每周拷一份到对象存储（OSS / S3 / 本地 NAS）。
- [ ] **`DB_SSLMODE=disable`** 是默认值；同主机连 postgres 无所谓，但如果 DB 拆到外部托管，必须改 `require`。
- [ ] **不要把 `.env` / `backups/` 提交进 git**：`.gitignore` 已经包含，部署前 `git status` 再确认一次。

#### G. 容器加固
- [ ] **Docker 镜像非 root 运行**：当前 `backend/Dockerfile` / `Dockerfile.worker` / `frontend/Dockerfile` 都没 `USER` 指令，容器进程跑 root。建议加 `USER 1000:1000`。这一项需要测试，不要直接 patch。
- [ ] **资源限制**：`docker-compose.yml` 无 `mem_limit` / `cpus`，单个容器 OOM 会拖死整机。`api` 和 `worker` 各加 `mem_limit: 1g`，`rsshub` 加 `mem_limit: 1500m`，`postgres` 加 `mem_limit: 2g`。
- [ ] **日志大小限制**：默认 docker json-file driver 无限增长，磁盘吃满。compose 顶部加：
  ```yaml
  x-logging: &default-logging
    driver: json-file
    options:
      max-size: "10m"
      max-file: "3"
  ```
  每个 service 加 `logging: *default-logging`。

#### H. 系统层
- [ ] **自动安全更新**：Ubuntu 装 `unattended-upgrades`，CentOS 装 `dnf-automatic`。
- [ ] **时钟同步**：`timedatectl status` 确认 NTP 启用，JWT 过期校验依赖系统时间。
- [ ] **swap 文件**：4G 内存机器建议开 2G swap，防 build 时 OOM。
- [ ] **首次登录后**：通过 UI 改 admin 密码（即使 `.env` 已设了强 `AUTH_PASSWORD`，UI 再改一次走 hash 流程）。

#### I. 部署后 30 秒自检
```bash
# 1. 没有默认密钥
grep -E "rss-pal-secret-change|=admin$|=postgres$" /opt/rss-pal/.env && echo "⚠️ 仍有默认值" || echo "✅"

# 2. 数据库不监听公网
ss -tlnp | grep -E "5432|8080|8090" | grep -v "127.0.0.1" && echo "⚠️ 端口暴露公网" || echo "✅"

# 3. HTTPS 工作
curl -sI https://your-domain.com | grep -i "strict-transport-security" || echo "⚠️ 缺 HSTS"

# 4. SSH 密码登录禁用
sudo sshd -T 2>/dev/null | grep -E "passwordauthentication|permitrootlogin"

# 5. .env 权限
stat -c "%a %U" /opt/rss-pal/.env  # 应该是 600 + root
```

> 备注：本节由 `scripts/deploy-oracle.sh` / `docker-compose.yml` / `frontend/nginx.conf` / `auto_deploy.sh` 静态审计得出，未在线上验证。改完 docker-compose 端口/密码后，**先在测试机跑一遍**，确认 api 仍能连上 db、nginx 仍能反代 api，再推到生产。

## 项目结构

```
rss-pal/
├── backend/
│   ├── cmd/
│   │   ├── server/     # Gin HTTP API
│   │   └── worker/     # 后台抓取调度器（每分钟运行）
│   └── internal/
│       ├── api/        # HTTP handlers
│       ├── repository/ # SQL 数据访问层
│       ├── model/      # 数据模型
│       ├── rss/        # RSS/HTML 抓取 + 内容提取
│       ├── ai/         # OpenAI-compatible API 集成
│       └── config/     # 环境变量配置
├── frontend/           # React 18 + TypeScript + Vite
└── docker-compose.yml
```

## 主要 API 接口

### 认证（无需登录）
- `POST /api/auth/init` — 首次运行初始化管理员
- `POST /api/auth/login` — 登录，返回 JWT
- `POST /api/auth/register` — 使用邀请码注册
- `GET /api/share/:token` — 查看分享文章（公开）

### 订阅管理（需登录）
- `GET /api/feeds` — 订阅列表
- `POST /api/feeds/preview` — 预览订阅源（添加前）
- `POST /api/feeds` — 添加订阅
- `DELETE /api/feeds/:id` — 删除订阅
- `POST /api/feeds/:id/fetch` — 立即抓取

### 文章
- `GET /api/articles` — 列表（支持 feed_id/unread 过滤、分页）
- `GET /api/articles/search?q=关键词` — 全文搜索
- `GET /api/articles/recommended` — 个性化推荐
- `GET /api/articles/unread-count` — 未读数量
- `POST /api/articles/mark-all-read` — 全部标记已读
- `GET /api/articles/:id` — 文章详情
- `POST /api/articles/:id/summary` — 生成 AI 总结
- `POST /api/articles/:id/content` — 重新抓取原文
- `GET /api/articles/:id/export/md` — 导出 Markdown
- `POST /api/articles/:id/share` — 生成分享链接
- `GET /api/articles/:id/images/:idx` — PDF 网摘抽出的图片（强缓存 + ETag）

### PDF 网摘

三种入口：
- **Chrome 扩展**（`extension/` 目录）打开 PDF 后点击图标——浏览器内置 viewer + 本地 `file://` 都支持；前者直接可用，后者需要在 `chrome://extensions` 该扩展详情页开启「允许访问文件 URL」
- **前端添加 feed** 时输入末尾是 `.pdf` 的 URL，会自动识别为 PDF 单篇网摘
- **直接调 API** `POST /api/bookmarklet/capture-pdf-url`（body `{url}`、bookmarklet token 鉴权）

处理 pipeline：数字版 PDF 用 `pdftotext` 秒级提取，立即入库；提取不到足够文字（< 200 字符）的 PDF 视为扫描版，标 `processing_state='processing'` 后由 worker 用 `tesseract`（中简 + 英）异步 OCR。图片用 `pdfimages -png` 抽取并按 SHA-256 去重，每篇上限 100 张（按页号取前 100），文末加截断提示。Markdown 按 PDF 页号分节。

依赖：`poppler-utils`（`pdftotext`、`pdfimages`、`pdftoppm`、`pdfinfo`）+ `tesseract-ocr` 含 `chi_sim` / `eng` 语言数据，api 和 worker 镜像都已预装。

#### 升级到 PDF 网摘版的部署清单（首次启用此功能时执行一次）

1. **手动应用 migration 028**——`docker-entrypoint-initdb.d` 只在空 volume 上跑一次，已有数据库要手动：
   ```bash
   docker-compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/028_articles_processing_error.sql
   ```
2. **重建 api + worker + frontend 镜像**——新增依赖、新增路由、前端组件：
   ```bash
   docker-compose up -d --build api worker frontend
   ```
3. **重新加载 Chrome 扩展**——`chrome://extensions` → RSS Pal → 刷新；本地 PDF 用户需在「详情」中开启「允许访问文件 URL」。
4. **存储增长提示**——PDF 抽出的图片落到 `${BACKUP_DIR}/article_images/<article_id>/`，每篇网摘最多 100 张 PNG/JPG（典型 0.5–5 MB）。如果开了大量扫描版 PDF 网摘，注意备份目录磁盘用量。

### 用户偏好
- `POST /api/preferences/like` — 标记喜欢
- `POST /api/preferences/dislike` — 标记不喜欢
- `POST /api/preferences/save` — 保存文章
- `POST /api/preferences/read-duration` — 记录阅读时长
- `GET /api/preferences/topics` — 兴趣主题列表

### 其他
- `GET /api/progress/:article_id` — 阅读进度
- `POST /api/progress/:article_id` — 更新进度
- `GET /api/stats` — 统计数据
- `POST /api/insights/generate` — 生成个性化洞察

## Browser Extension Adapters (Twitter, MVP)

The extension supports per-site adapters that auto-archive streaming content
from logged-in tabs. With Chrome logged into x.com, browsing any of these
pages will auto-archive recent tweets into rss-pal:

- `https://x.com/i/lists/<id>` → twitter:list source
- `https://x.com/<handle>` → twitter:user source
- `https://x.com/i/bookmarks` → twitter:bookmarks source

Sources auto-appear in the popup's "同步 Source" dropdown after they're first
visited. Click "立即同步" to refresh a source manually (opens a background
tab, extracts, flushes, closes).

Auto-extract toggles (Options page):
- Twitter list / user / bookmarks — ON by default
- Twitter Home Timeline — OFF (high noise; future work)

Tweet articles render as compact tweet cards (`kind=tweet` discriminator);
all other articles keep their existing rendering.

### Apply database migrations

This feature adds two new columns:
- `articles.kind` (TEXT, default `'article'`)
- `feeds.provider_source_id` (TEXT, nullable)

Apply locally:

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/029_articles_kind.sql
docker-compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/030_feeds_provider_source_id.sql
```

The migrations are idempotent (`IF NOT EXISTS` everywhere) and backward-compatible.

### Rebuild + reload

```bash
docker-compose up -d --build api worker frontend
# then in Chrome: chrome://extensions → reload "RSS Pal"
```

See `docs/superpowers/specs/2026-05-26-extension-adapter-platform-twitter-mvp-design.md`
for the full design, and `docs/extension-adapters/upstream-map.md` for the OpenCLI
upstream tracking SOP.
