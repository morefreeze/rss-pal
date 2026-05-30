#!/usr/bin/env bash
# ============================================================
# rss-pal 一键部署脚本 — Oracle Cloud (Ubuntu 22.04/24.04 ARM64)
# 
# 使用方法：
#   1. 在 Oracle Cloud 创建 ARM 实例（Ubuntu 22.04+）
#   2. 开放安全列表端口：80, 443, 22
#   3. scp 本脚本到服务器：
#      scp scripts/deploy-oracle.sh ubuntu@<IP>:/tmp/
#   4. SSH 登录后执行：
#      bash /tmp/deploy-oracle.sh
# ============================================================
set -euo pipefail

# ---------- 颜色 ----------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }
step()  { echo -e "\n${CYAN}==== $* ====${NC}"; }

# ---------- 配置 ----------
APP_DIR="/opt/rss-pal"
BACKUP_DIR="/opt/rss-pal/backups"
ENV_FILE="$APP_DIR/.env"
REPO_URL="https://github.com/morefreeze/rss-pal.git"
BRANCH="master"
DOMAIN=""
EMAIL=""

# ---------- 参数解析 ----------
while [[ $# -gt 0 ]]; do
  case $1 in
    --domain)  DOMAIN="$2";  shift 2 ;;
    --email)   EMAIL="$2";   shift 2 ;;
    --dir)     APP_DIR="$2"; BACKUP_DIR="$2/backups"; ENV_FILE="$2/.env"; shift 2 ;;
    *)         warn "未知参数: $1"; shift ;;
  esac
done

# ==========================================
step "0. 系统检查"
# ==========================================
if [[ "$(uname -m)" != "aarch64" && "$(uname -m)" != "x86_64" ]]; then
  error "不支持的架构: $(uname -m)，仅支持 aarch64 (ARM) 和 x86_64"
fi
info "架构: $(uname -m)"

# Oracle Cloud Ubuntu 默认用户是 ubuntu，需要 sudo
if [[ "$EUID" -ne 0 ]]; then
  SUDO="sudo"
  info "以普通用户运行，将使用 sudo"
else
  SUDO=""
  info "以 root 用户运行"
fi

# ==========================================
step "1. 安装系统依赖"
# ==========================================
$SUDO apt-get update -qq
$SUDO apt-get install -y -qq \
  curl git wget unzip ca-certificates \
  gnupg lsb-release software-properties-common \
  nginx certbot python3-certbot-nginx

info "系统依赖安装完成"

# ==========================================
step "2. 安装 Docker"
# ==========================================
if command -v docker &>/dev/null; then
  info "Docker 已安装: $(docker --version)"
else
  info "安装 Docker..."
  curl -fsSL https://get.docker.com | sh
  $SUDO usermod -aG docker $USER
  info "Docker 安装完成: $(docker --version)"
fi

# 确保 docker 服务启动
$SUDO systemctl enable docker
$SUDO systemctl start docker

# docker compose v2 检查
if docker compose version &>/dev/null; then
  info "Docker Compose: $(docker compose version)"
else
  error "Docker Compose v2 未找到，请检查 Docker 安装"
fi

# ==========================================
step "3. 拉取代码"
# ==========================================
if [[ -d "$APP_DIR" ]]; then
  info "目录已存在: $APP_DIR，拉取最新代码..."
  cd "$APP_DIR"
  git fetch origin "$BRANCH"
  git reset --hard "origin/$BRANCH"
else
  info "克隆仓库到 $APP_DIR..."
  $SUDO git clone -b "$BRANCH" "$REPO_URL" "$APP_DIR"
  cd "$APP_DIR"
  $SUDO chown -R $USER:$USER "$APP_DIR"
fi

info "当前版本: $(git log --oneline -1)"

# ==========================================
step "4. 配置环境变量"
# ==========================================
if [[ -f "$ENV_FILE" ]]; then
  info ".env 已存在，跳过配置"
else
  info "创建 .env 文件..."
  cat > "$ENV_FILE" << 'ENVEOF'
# ==================== AI 服务 ====================
# OpenAI 兼容 API 端点
CLAUDE_BASE_URL=https://api.z.ai/api/coding/paas/v4
# API Key（必填）
CLAUDE_API_KEY=

# ==================== 认证 ====================
# 管理员密码（首次登录使用）
AUTH_PASSWORD=admin
# JWT 密钥（建议改为随机长字符串）
JWT_SECRET=rss-pal-secret-change-in-production

# ==================== 可选 ====================
# Jina Reader API Key
JINA_API_KEY=
# Bilibili Cookie（B站 RSS 需要）
BILIBILI_COOKIE=
ENVEOF

  echo ""
  warn "请编辑 $ENV_FILE 填入必要的配置："
  warn "  必填: CLAUDE_API_KEY"
  warn "  建议: AUTH_PASSWORD, JWT_SECRET"
  echo ""
  read -p "现在编辑 .env？[Y/n] " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Nn]$ ]]; then
    ${EDITOR:-vim} "$ENV_FILE"
  fi
fi

# 验证必填项
if grep -q "^CLAUDE_API_KEY=$" "$ENV_FILE" || ! grep -q "^CLAUDE_API_KEY=" "$ENV_FILE"; then
  warn "CLAUDE_API_KEY 未设置，AI 摘要功能将不可用"
fi

# ==========================================
step "5. 修改 nginx 配置（去掉本地 SSL）"
# ==========================================
# 生产环境用 certbot 或云平台 TLS，nginx 只监听 80
NGINX_CONF="$APP_DIR/frontend/nginx.conf"
if grep -q "listen 443 ssl" "$NGINX_CONF"; then
  info "修改 nginx.conf — 移除本地 SSL 配置..."
  # 用 sed 而不是 patch，兼容性更好
  $SUDO sed -i.bak \
    -e '/listen 443 ssl;/d' \
    -e '/ssl_certificate/d' \
    -e '/ssl_session_/d' \
    -e '/ssl_session_tickets/d' \
    "$NGINX_CONF"
  # 也去掉 SSL 相关行注释
  info "nginx.conf 已更新（备份: nginx.conf.bak）"
fi

# ==========================================
step "6. 创建必要目录"
# ==========================================
mkdir -p "$BACKUP_DIR"
info "备份目录: $BACKUP_DIR"

# ==========================================
step "7. 构建并启动服务"
# ==========================================
info "开始构建（Go 编译约需 5-6 分钟）..."
docker compose up -d --build 2>&1

info "等待服务启动..."
sleep 15

# 检查容器状态
FAILED=$(docker compose ps --filter "status=exited" -q 2>/dev/null | wc -l | tr -d ' ')
if [[ "$FAILED" -gt 0 ]]; then
  error "有 $FAILED 个容器退出，请检查: docker compose logs"
fi

info "所有容器启动成功"
docker compose ps --format "table {{.Name}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null || true

# ==========================================
step "8. 等待 PostgreSQL 就绪 & 应用迁移"
# ==========================================
info "等待 PostgreSQL 就绪..."
for i in $(seq 1 30); do
  if docker compose exec -T postgres pg_isready -U postgres &>/dev/null; then
    info "PostgreSQL 就绪"
    break
  fi
  echo "  等待... ($i/30)"
  sleep 2
done

# 自动发现并应用所有迁移
MIGRATIONS_DIR="$APP_DIR/backend/migrations"
APPLIED=0
SKIPPED=0

info "检查数据库迁移..."
for f in "$MIGRATIONS_DIR"/*.sql; do
  fname=$(basename "$f")
  
  # 027 使用 CONCURRENTLY，不能在事务里跑，逐条执行
  if [[ "$fname" == *"concurrently"* ]] || [[ "$fname" == *"027_"* ]]; then
    # 跳过注释行和空行，逐条执行 CREATE INDEX CONCURRENTLY
    while IFS= read -r line; do
      [[ "$line" =~ ^-- ]] && continue
      [[ -z "${line// }" ]] && continue
      docker compose exec -T postgres psql -U postgres -d rsspal -c "$line" 2>&1 | grep -i "error" && warn "迁移 $fname 执行出错" || true
    done < "$f"
    APPLIED=$((APPLIED + 1))
    continue
  fi

  # 普通迁移文件
  if docker compose exec -T postgres psql -U postgres -d rsspal < "$f" &>/dev/null; then
    APPLIED=$((APPLIED + 1))
    info "  ✅ $fname"
  else
    SKIPPED=$((SKIPPED + 1))
    warn "  ⏭️  $fname (可能已应用)"
  fi
done

info "迁移完成: $APPLIED 个应用, $SKIPPED 个跳过"

# ==========================================
step "9. 健康检查"
# ==========================================
for i in $(seq 1 10); do
  HTTP=$(curl -sf -o /dev/null -w "%{http_code}" "http://localhost/api/health" 2>/dev/null || echo "000")
  if [[ "$HTTP" == "200" || "$HTTP" == "401" ]]; then
    info "✅ API 健康检查通过 (HTTP $HTTP)"
    break
  fi
  echo "  等待 API... ($i/10)"
  sleep 5
done

# ==========================================
step "10. 配置域名 & HTTPS（可选）"
# ==========================================
if [[ -n "$DOMAIN" ]]; then
  info "配置域名: $DOMAIN"
  
  # 更新 nginx 配置中的 server_name
  $SUDO sed -i "s/server_name localhost;/server_name $DOMAIN;/" "$NGINX_CONF"
  
  # 配置系统 nginx 反向代理
  cat > /tmp/rss-pal.conf << EOF
server {
    listen 80;
    server_name $DOMAIN;
    
    location / {
        proxy_pass http://127.0.0.1:80;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF
  $SUDO cp /tmp/rss-pal.conf /etc/nginx/sites-available/rss-pal
  $SUDO ln -sf /etc/nginx/sites-available/rss-pal /etc/nginx/sites-enabled/
  $SUDO nginx -t && $SUDO systemctl reload nginx
  
  # 申请 Let's Encrypt 证书
  if [[ -n "$EMAIL" ]]; then
    info "申请 SSL 证书..."
    $SUDO certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos --email "$EMAIL" --redirect
    info "✅ HTTPS 配置完成"
  else
    warn "未提供 --email，跳过 SSL 证书申请"
    warn "  后续可手动执行: sudo certbot --nginx -d $DOMAIN"
  fi
  
  info "✅ 域名配置完成: https://$DOMAIN"
else
  warn "未指定域名 (--domain)，使用 IP 直接访问"
  # 获取公网 IP
  PUBLIC_IP=$(curl -sf ifconfig.me 2>/dev/null || curl -sf icanhazip.com 2>/dev/null || echo "<YOUR_IP>")
  info "访问地址: http://$PUBLIC_IP"
fi

# ==========================================
step "11. 配置自动更新（可选）"
# ==========================================
read -p "配置每日自动更新？[Y/n] " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Nn]$ ]]; then
  # 写入 systemd timer 或 crontab
  CRON_SCRIPT="$APP_DIR/scripts/auto_deploy.sh"
  if [[ -f "$CRON_SCRIPT" ]]; then
    (crontab -l 2>/dev/null; echo "0 3 * * * /bin/bash $CRON_SCRIPT >> $APP_DIR/scripts/deploy.log 2>&1") | crontab -
    info "✅ 已配置每日凌晨 3:00 自动更新"
  else
    warn "auto_deploy.sh 不存在，跳过"
  fi
fi

# ==========================================
# 完成！
# ==========================================
echo ""
echo -e "${GREEN}╔══════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║          🎉 rss-pal 部署完成！               ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════╝${NC}"
echo ""

PUBLIC_IP=$(curl -sf ifconfig.me 2>/dev/null || curl -sf icanhazip.com 2>/dev/null || echo "<YOUR_IP>")

echo "  📂 项目目录: $APP_DIR"
echo "  💾 备份目录: $BACKUP_DIR"
echo "  🌐 访问地址: ${DOMAIN:+https://$DOMAIN}${DOMAIN:-http://$PUBLIC_IP}"
echo ""
echo "  常用命令:"
echo "    cd $APP_DIR"
echo "    docker compose logs -f api       # 查看 API 日志"
echo "    docker compose logs -f worker    # 查看 Worker 日志"
echo "    docker compose ps                # 查看容器状态"
echo "    docker compose up -d --build     # 重新构建部署"
echo ""
echo "  数据库备份:"
echo "    docker compose exec postgres pg_dump -U postgres rsspal > backup.sql"
echo ""
echo "  数据库恢复:"
echo "    docker compose exec -T postgres psql -U postgres rsspal < backup.sql"
echo ""
