# RSS Pal

个人 RSS 阅读服务，支持 AI 总结和个性化推荐。

## 功能

- RSS 订阅管理和智能抓取
- AI 驱动的文章总结（要点列表 + 详细版本）
- 用户偏好学习和个性化推荐
- 阅读进度追踪
- 内容洞察分析

## 快速开始

### 环境要求

- Go 1.20+
- Node.js 18+ (用于前端构建)
- PostgreSQL 15+
- Docker & Docker Compose (推荐)

### 使用 Docker Compose

1. 创建环境变量文件：

```bash
cat > .env << EOF
CLAUDE_API_KEY=your-claude-api-key
AUTH_PASSWORD=your-password
EOF
```

2. 启动服务：

```bash
docker-compose up -d
```

3. 访问 http://localhost

### 手动部署

#### 后端

```bash
cd backend

# 安装依赖
go mod tidy

# 运行数据库迁移
psql -U postgres -d rsspal -f migrations/001_init.sql

# 启动 API 服务
go run ./cmd/server

# 启动 RSS Worker (另一个终端)
go run ./cmd/worker
```

#### 前端

```bash
cd frontend

# 安装依赖
npm install

# 开发模式
npm run dev

# 构建
npm run build
```

## 配置

### 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| SERVER_PORT | API 服务端口 | 8080 |
| DB_HOST | 数据库主机 | localhost |
| DB_PORT | 数据库端口 | 5432 |
| DB_USER | 数据库用户名 | postgres |
| DB_PASSWORD | 数据库密码 | postgres |
| DB_NAME | 数据库名称 | rsspal |
| CLAUDE_API_KEY | Claude API 密钥 | - |
| AUTH_PASSWORD | 登录密码 | admin |

## 项目结构

```
rss-pal/
├── backend/
│   ├── cmd/
│   │   ├── server/     # API 服务
│   │   └── worker/     # RSS 抓取调度器
│   ├── internal/
│   │   ├── api/        # HTTP handlers
│   │   ├── service/    # 业务逻辑
│   │   ├── repository/ # 数据访问层
│   │   ├── model/      # 数据模型
│   │   ├── rss/        # RSS 抓取
│   │   ├── ai/         # Claude API 集成
│   │   └── config/     # 配置管理
│   └── migrations/     # 数据库迁移
├── frontend/
│   └── src/
│       ├── components/ # React 组件
│       ├── pages/      # 页面
│       ├── hooks/      # 自定义 hooks
│       └── api/        # API 客户端
└── docker-compose.yml
```

## API 接口

### 认证
- `POST /api/auth/login` - 登录
- `POST /api/auth/logout` - 登出

### 订阅
- `GET /api/feeds` - 获取订阅列表
- `POST /api/feeds` - 添加订阅
- `DELETE /api/feeds/:id` - 删除订阅

### 文章
- `GET /api/articles` - 获取文章列表
- `GET /api/articles/:id` - 获取文章详情
- `POST /api/articles/:id/summary` - 生成总结
- `GET /api/articles/recommended` - 获取推荐文章

### 偏好
- `POST /api/preferences/like` - 标记喜欢
- `POST /api/preferences/dislike` - 标记不喜欢
- `POST /api/preferences/save` - 保存文章

### 阅读进度
- `GET /api/progress/:article_id` - 获取进度
- `POST /api/progress/:article_id` - 更新进度
- `POST /api/progress/:article_id/reset` - 重置进度
