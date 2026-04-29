# RSS Pal

个人 RSS 阅读服务，支持 AI 总结、个性化推荐和多用户管理。

## 功能特性

- **智能订阅抓取** — 支持 RSS/Atom 订阅，也支持任意网页（自动识别文章链接），添加前预览内容
- **AI 驱动总结** — 每篇文章自动生成要点摘要 + 详细分析，支持自定义模板
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
