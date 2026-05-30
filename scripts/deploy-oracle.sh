#!/usr/bin/env bash
# ============================================================
# rss-pal 一键部署脚本 — Oracle Cloud (CentOS / Ubuntu)
# 
# 使用方法：
#   bash deploy-oracle.sh
#   bash deploy-oracle.sh --domain rss.yourdomain.com --email you@example.com
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

# ---------- 检测系统 ----------
detect_os() {
  if [[ -f /etc/centos-release ]] || [[ -f /etc/oracle-release ]] || [[ -f /etc/redhat-release ]]; then
    echo "centos"
  elif [[ -f /etc/lsb-release ]] || [[ -f /etc/debian_version ]]; then
    echo "ubuntu"
  else
    echo "unknown"
  fi
}

OS=$(detect_os)
info "操作系统: $OS"

if [[ "$OS" == "unknown" ]]; then
  error "不支持的操作系统"
fi

# CentOS 默认 opc 用户，Ubuntu 默认 ubuntu 用户
# Oracle Cloud Ubuntu 默认用户是 opc（不是 ubuntu）
if [[ "$EUID" -ne 0 ]]; then
  error "请用 root 用户运行此脚本（sudo bash deploy-oracle.sh）"
fi

# ==========================================
step "0. 系统检查"
# ==========================================
info "架构: $(uname -m)"

# ==========================================
step "1. 安装系统依赖"
# ==========================================
if [[ "$OS" == "centos" ]]; then
  info "CentOS — 使用 dnf 安装..."
  dnf install -y \
    curl git wget unzip ca-certificates \
    nginx certbot python3-certbot-nginx \
    policycoreutils-python-utils
  systemctl enable nginx
  systemctl start nginx
elif [[ "$OS" == "ubuntu" ]]; then
  info "Ubuntu — 使用 apt 安装..."
  apt-get update -qq
  apt-get install -y -qq \
    curl git wget unzip ca-certificates \
    gnupg lsb-release software-properties-common \
    nginx certbot python3-certbot-nginx
  systemctl enable nginx
fi
info "系统依赖安装完成"

# ==========================================
step "2. 安装 Docker"
# ==========================================
if command -v docker &>/dev/null; then
  info "Docker 已安装: $(docker --version)"
else
  info "安装 Docker..."
  if [[ "$OS" == "centos" ]]; then
    # CentOS: 用官方 yum 源
    dnf install -y yum-utils
    dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
    dnf install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
  else
    curl -fsSL https://get.docker.com | sh
  fi
  systemctl enable docker
  systemctl start docker
  info "Docker 安装完成: $(docker --version)"
fi

# docker compose v2 检查
if docker compose version &>/dev/null; then
  info "Docker Compose: $(docker compose version)"
else
  error "Docker Compose v2 未找到，请检查 Docker 安装"
fi

# ==========================================
step "3. 配置防火墙"
# ==========================================
info "配置防火墙..."
if command -v firewall-cmd &>/dev/null; then
  # CentOS firewalld
  firewall-cmd --permanent --add-port=80/tcp 2>/dev/null || true
  firewall-cmd --permanent --add-port=443/tcp 2>/dev/null || true
  firewall-cmd --reload 2>/dev/null || true
  info "firewalld 已开放 80/443"
elif command -v ufw &>/dev/null; then
  # Ubuntu ufw
  ufw allow 80/tcp 2>/dev/null || true
  ufw allow 443/tcp 2>/dev/null || true
  info "ufw 已开放 80/443"
fi
warn "记得在 Oracle Cloud 安全列表中也开放 80/443 端口"

# ==========================================
step "4. 拉取代码"
# ==========================================
if [[ -d "$APP_DIR" ]]; then
  info "目录已存在: $APP_DIR，拉取最新代码..."
  cd "$APP_DIR"
  git fetch origin "$BRANCH"
  git reset --hard "origin/$BRANCH"
else
  info "克隆仓库到 $APP_DIR..."
  git clone -b "$BRANCH" "$REPO_URL" "$APP_DIR"
  cd "$APP_DIR"
fi
info "当前版本: $(git log --oneline -1)"

# ==========================================
step "5. 配置环境变量"
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
    ${EDITOR:-vi} "$ENV_FILE"
  fi
fi

# 验证必填项
if grep -q "^CLAUDE_API_KEY=$" "$ENV_FILE" || ! grep -q "^CLAUDE_API_KEY=" "$ENV_FILE"; then
  warn "CLAUDE_API_KEY 未设置，AI 摘要功能将不可用"
fi

# ==========================================
step "6. 修改 nginx 配置（去掉本地 SSL）"
# ==========================================
NGINX_CONF="$APP_DIR/frontend/nginx.conf"
if grep -q "listen 443 ssl" "$NGINX_CONF"; then
  info "修改 nginx.conf — 移除本地 SSL 配置..."
  cp "$NGINX_CONF" "$NGINX_CONF.bak"
  sed -i \
    -e '/listen 443 ssl;/d' \
    -e '/ssl_certificate/d' \
    -e '/ssl_session_/d' \
    -e '/ssl_session_tickets/d' \
    "$NGINX_CONF"
  info "nginx.conf 已更新（备份: nginx.conf.bak）"
fi

# ==========================================
step "7. 创建必要目录"
# ==========================================
mkdir -p "$BACKUP_DIR"
info "备份目录: $BACKUP_DIR"

# ==========================================
step "8. 构建并启动服务"
# ==========================================
info "开始构建（Go 编译约需 5-6 分钟）..."
cd "$APP_DIR"
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
step "9. 等待 PostgreSQL 就绪 & 应用迁移"
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
  
  # 027 使用 CONCURRENTLY，不能在事务里跑
  if [[ "$fname" == *"027_"* ]]; then
    while IFS= read -r line; do
      [[ "$line" =~ ^-- ]] && continue
      [[ -z "${line// }" ]] && continue
      docker compose exec -T postgres psql -U postgres -d rsspal -c "$line" 2>&1 | grep -i "error" && warn "迁移 $fname 执行出错" || true
    done < "$f"
    APPLIED=$((APPLIED + 1))
    info "  ✅ $fname"
    continue
  fi

  # 普通迁移文件
  if output=$(docker compose exec -T postgres psql -U postgres -d rsspal < "$f" 2>&1); then
    APPLIED=$((APPLIED + 1))
    info "  ✅ $fname"
  else
    SKIPPED=$((SKIPPED + 1))
    warn "  ⏭️  $fname (可能已应用)"
  fi
done

info "迁移完成: $APPLIED 个应用, $SKIPPED 个跳过"

# ==========================================
step "10. 健康检查"
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
step "11. 配置域名 & HTTPS（可选）"
# ==========================================
if [[ -n "$DOMAIN" ]]; then
  info "配置域名: $DOMAIN"
  
  # 更新 docker nginx 配置中的 server_name
  sed -i "s/server_name localhost;/server_name $DOMAIN;/" "$NGINX_CONF"
  
  # 配置系统 nginx 反向代理
  if [[ "$OS" == "centos" ]]; then
    NGINX_CONF_PATH="/etc/nginx/conf.d/rss-pal.conf"
  else
    NGINX_CONF_PATH="/etc/nginx/sites-available/rss-pal"
    ln -sf "$NGINX_CONF_PATH" /etc/nginx/sites-enabled/
  fi
  
  cat > "$NGINX_CONF_PATH" << EOF
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
  nginx -t && systemctl reload nginx
  
  # 申请 Let's Encrypt 证书
  if [[ -n "$EMAIL" ]]; then
    info "申请 SSL 证书..."
    certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos --email "$EMAIL" --redirect
    info "✅ HTTPS 配置完成"
  else
    warn "未提供 --email，跳过 SSL 证书申请"
    warn "  后续可手动执行: certbot --nginx -d $DOMAIN"
  fi
  
  info "✅ 域名配置完成: https://$DOMAIN"
else
  warn "未指定域名 (--domain)，使用 IP 直接访问"
  PUBLIC_IP=$(curl -sf ifconfig.me 2>/dev/null || curl -sf icanhazip.com 2>/dev/null || echo "<YOUR_IP>")
  info "访问地址: http://$PUBLIC_IP"
fi

# ==========================================
step "12. 配置自动更新（可选）"
# ==========================================
read -p "配置每日自动更新？[Y/n] " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Nn]$ ]]; then
  CRON_SCRIPT="$APP_DIR/scripts/auto_deploy.sh"
  if [[ -f "$CRON_SCRIPT" ]]; then
    (crontab -l 2>/dev/null; echo "0 3 * * * /bin/bash $CRON_SCRIPT >> $APP_DIR/scripts/deploy.log 2>&1") | crontab -
    info "✅ 已配置每日凌晨 3:00 自动更新"
  else
    warn "auto_deploy.sh 不存在，跳过"
  fi
fi

# ==========================================
# 配置 SELinux（CentOS）
# ==========================================
if [[ "$OS" == "centos" ]] && command -v getenforce &>/dev/null && [[ "$(getenforce)" != "Disabled" ]]; then
  info "配置 SELinux..."
  setsebool -P httpd_can_network_connect 1 2>/dev/null || true
  info "SELinux: 已允许 nginx 反向代理"
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
