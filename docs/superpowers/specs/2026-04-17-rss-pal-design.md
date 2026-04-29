# RSS Pal 设计文档

## 概述

RSS Pal 是一个个人 RSS 阅读服务，帮助用户高效获取信息。核心功能包括：
- RSS 订阅管理和智能抓取
- AI 驱动的文章总结（要点列表 + 详细版本）
- 用户偏好学习和个性化推荐
- 内容洞察分析

## 技术选型

| 组件 | 技术 |
|------|------|
| 后端 | Go + Gin/Echo |
| 前端 | React + TypeScript |
| 数据库 | PostgreSQL |
| AI 模型 | Claude API |
| 部署 | VPS + Nginx |
| 认证 | 简单密码保护 |

## 架构

```
┌────────────────────────────────────────────────────────────┐
│                        VPS 部署                            │
│  ┌─────────────┐      ┌─────────────┐      ┌───────────┐  │
│  │   Nginx     │      │  Go API     │      │ Scheduler │  │
│  │   :80/443   │─────►│   :8080     │─────►│  Worker   │  │
│  │             │      └─────────────┘      └───────────┘  │
│  │ React SPA   │              │                  │        │
│  │ 静态托管    │              ▼                  ▼        │
│  └─────────────┘      ┌─────────────────────────────┐     │
│                       │        PostgreSQL           │     │
│                       └─────────────────────────────┘     │
└────────────────────────────────────────────────────────────┘
```

## 数据模型

### feeds（RSS 订阅源）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL PK | 主键 |
| url | VARCHAR(2048) | RSS 地址，唯一 |
| title | VARCHAR(500) | 订阅源标题 |
| last_fetched_at | TIMESTAMP | 最后抓取时间 |
| fetch_interval_minutes | INT | 智能计算的抓取间隔 |
| etag | VARCHAR(500) | HTTP 缓存标识 |
| last_modified | VARCHAR(500) | HTTP 缓存标识 |
| is_active | BOOLEAN | 是否启用 |
| created_at | TIMESTAMP | 创建时间 |

### articles（文章）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL PK | 主键 |
| feed_id | INT FK | 所属订阅源 |
| title | VARCHAR(500) | 文章标题 |
| url | VARCHAR(2048) | 文章链接 |
| content | TEXT | 文章内容 |
| published_at | TIMESTAMP | 发布时间 |
| summary_brief | TEXT | 要点列表总结 |
| summary_detailed | TEXT | 详细总结 |
| fetched_at | TIMESTAMP | 抓取时间 |

### user_preferences（用户偏好）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL PK | 主键 |
| article_id | INT FK | 关联文章 |
| signal_type | VARCHAR(50) | 信号类型：like/dislike/click/read_duration/save |
| signal_value | FLOAT | 信号强度值 |
| created_at | TIMESTAMP | 创建时间 |

### interest_topics（兴趣主题）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL PK | 主键 |
| topic | VARCHAR(200) | 主题关键词 |
| weight | FLOAT | 权重（随时间衰减） |
| last_reinforced_at | TIMESTAMP | 最后强化时间 |

### reading_progress（阅读进度）
| 字段 | 类型 | 说明 |
|------|------|------|
| id | SERIAL PK | 主键 |
| article_id | INT FK | 关联文章，唯一 |
| scroll_position | FLOAT | 滚动位置比例（0.0-1.0） |
| last_read_at | TIMESTAMP | 最后阅读时间 |
| is_completed | BOOLEAN | 是否读完 |

## 核心功能

### 1. RSS 智能抓取
- 根据每个订阅源的更新频率动态调整抓取间隔
- 支持 HTTP 缓存（ETag/Last-Modified）减少带宽
- 后台 Worker 定时执行，失败重试

### 2. AI 总结
- 使用 Claude API 生成两种格式：
  - **要点列表**：3-5 个关键要点，快速浏览
  - **详细总结**：完整的文章摘要
- 总结结果缓存到数据库，避免重复调用
- 用户可手动重新生成

### 3. 偏好学习
- **显式反馈**：用户主动标记喜欢/不喜欢/保存
- **隐式行为**：记录点击、阅读时长
- 定期从偏好数据中提取兴趣主题
- 权重随时间衰减，近期行为影响更大

### 4. 阅读进度
- 记录每篇文章的阅读位置（滚动比例）
- **智能更新逻辑**：用户滚动到顶部后停留超过 10 秒，才认为是重新阅读，重置进度位置
- 短暂回到顶部（检查更新）不重置进度
- 支持从上次位置继续阅读
- 前端实现：监听滚动事件，当 `scrollTop === 0` 时启动 10 秒计时器，计时结束才调用重置接口；若在 10 秒内滚动离开顶部，取消计时器

### 5. 个性化展示
- 高偏好文章放入"推荐"区域
- 其余文章按时间排序
- 可过滤已读/未读
- 显示阅读进度条

### 6. 内容洞察
- AI 分析用户兴趣变化趋势
- 发现主题关联和潜在兴趣点
- 定期生成洞察报告

## API 设计

### 认证
- `POST /api/auth/login` - 密码登录
- `POST /api/auth/logout` - 登出

### 订阅管理
- `GET /api/feeds` - 获取订阅列表
- `POST /api/feeds` - 添加订阅
- `DELETE /api/feeds/:id` - 删除订阅
- `PUT /api/feeds/:id` - 更新订阅

### 文章
- `GET /api/articles` - 获取文章列表（支持分页、过滤）
- `GET /api/articles/:id` - 获取文章详情
- `POST /api/articles/:id/summary` - 生成/重新生成总结
- `GET /api/articles/recommended` - 获取推荐文章

### 偏好
- `POST /api/preferences/like` - 标记喜欢
- `POST /api/preferences/dislike` - 标记不喜欢
- `POST /api/preferences/save` - 保存文章
- `GET /api/preferences/topics` - 获取兴趣主题

### 阅读进度
- `GET /api/progress/:article_id` - 获取文章阅读进度
- `POST /api/progress/:article_id` - 更新阅读进度
- `POST /api/progress/:article_id/reset` - 重置进度（顶部停留超时触发）

### 洞察
- `GET /api/insights` - 获取内容洞察

## 目录结构

```
rss-pal/
├── backend/
│   ├── cmd/
│   │   ├── server/        # API 服务入口
│   │   └── worker/        # 调度器入口
│   ├── internal/
│   │   ├── api/           # HTTP handlers
│   │   ├── service/       # 业务逻辑
│   │   ├── repository/    # 数据访问
│   │   ├── model/         # 数据模型
│   │   ├── rss/           # RSS 抓取
│   │   ├── ai/            # Claude API 集成
│   │   ├── scheduler/     # 智能调度
│   │   └── config/        # 配置管理
│   ├── migrations/        # 数据库迁移
│   └── go.mod
├── frontend/
│   ├── src/
│   │   ├── components/
│   │   ├── pages/
│   │   ├── hooks/
│   │   ├── api/
│   │   └── App.tsx
│   └── package.json
├── docs/
└── docker-compose.yml
```

## 部署方案

1. Go API 编译为单二进制
2. React SPA 构建为静态文件
3. Nginx 托管静态文件，反向代理 API
4. PostgreSQL 独立运行
5. Worker 作为独立进程或 systemd 服务
