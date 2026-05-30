#!/usr/bin/env bash
# ============================================================
# Oracle Cloud 创建 Always Free 实例 (Cloud Shell 执行)
# ============================================================
set -euo pipefail

COMPARTMENT_ID="ocid1.compartment.oc1..aaaaaaaacx7uwmbba6taoff722xcoinlg6ajjvzykphx6hbayzdgssyl5mhq"
REGION=$(oci iam region-subscription list --query "data[0].\"region-key\"" --raw-output)
echo "Region: $REGION"

# 1. 创建 SSH 密钥（如果不存在）
mkdir -p ~/.ssh
if [[ ! -f ~/.ssh/rss-pal-key ]]; then
  ssh-keygen -t rsa -b 4096 -f ~/.ssh/rss-pal-key -N ""
  echo "✅ SSH 密钥已创建"
else
  echo "✅ SSH 密钥已存在"
fi
SSH_PUB_KEY=$(cat ~/.ssh/rss-pal-key.pub)
echo "公钥: $SSH_PUB_KEY"

# 2. 获取 Availability Domain
AD=$(oci iam availability-domain list --compartment-id "$COMPARTMENT_ID" --query "data[0].id" --raw-output)
echo "Availability Domain: $AD"

# 3. 查找 Ubuntu 24.04 ARM 镜像（Always Free 兼容）
echo "查找 Ubuntu 24.04 ARM 镜像..."
IMAGE_ID=$(oci compute image list \
  --compartment-id "$COMPARTMENT_ID" \
  --operating-system "Canonical Ubuntu" \
  --operating-system-version "24.04" \
  --query "data[0].id" \
  --raw-output)
echo "Image ID: $IMAGE_ID"

# 4. 检查是否有现成的 VCN
VCN_ID=$(oci network vcn list --compartment-id "$COMPARTMENT_ID" --query "data[0].id" --raw-output 2>/dev/null || echo "")

if [[ -z "$VCN_ID" || "$VCN_ID" == "null" ]]; then
  echo "创建 VCN..."
  VCN_ID=$(oci network vcn create \
    --compartment-id "$COMPARTMENT_ID" \
    --cidr-block "10.0.0.0/16" \
    --display-name "rss-pal-vcn" \
    --dns-label "rsspal" \
    --query "data.id" \
    --raw-output)
  echo "✅ VCN: $VCN_ID"
  
  # 创建子网
  SUBNET_ID=$(oci network subnet create \
    --compartment-id "$COMPARTMENT_ID" \
    --vcn-id "$VCN_ID" \
    --cidr-block "10.0.0.0/24" \
    --display-name "rss-pal-subnet" \
    --dns-label "rsspalsub" \
    --query "data.id" \
    --raw-output)
  echo "✅ Subnet: $SUBNET_ID"
  
  # 创建安全列表（开放 22, 80, 443）
  SECURITY_LIST_ID=$(oci network security-list create \
    --compartment-id "$COMPARTMENT_ID" \
    --vcn-id "$VCN_ID" \
    --display-name "rss-pal-security" \
    --egress-security-rules '[{"destination":"0.0.0.0/0","protocol":"all","isStateless":false}]' \
    --ingress-security-rules '[{"source":"0.0.0.0/0","protocol":"6","isStateless":false,"tcpOptions":{"destinationPortRange":{"min":22,"max":22}}},{"source":"0.0.0.0/0","protocol":"6","isStateless":false,"tcpOptions":{"destinationPortRange":{"min":80,"max":80}}},{"source":"0.0.0.0/0","protocol":"6","isStateless":false,"tcpOptions":{"destinationPortRange":{"min":443,"max":443}}}]' \
    --query "data.id" \
    --raw-output)
  echo "✅ Security List: $SECURITY_LIST_ID"

  # 创建 Internet Gateway
  IGW_ID=$(oci network internet-gateway create \
    --compartment-id "$COMPARTMENT_ID" \
    --vcn-id "$VCN_ID" \
    --display-name "rss-pal-igw" \
    --is-enabled true \
    --query "data.id" \
    --raw-output)
  echo "✅ Internet Gateway: $IGW_ID"
  
  # 更新路由表（默认路由指向 Internet Gateway）
  ROUTE_TABLE_ID=$(oci network route-table list --compartment-id "$COMPARTMENT_ID" --vcn-id "$VCN_ID" --query "data[0].id" --raw-output)
  oci network route-table update --rt-id "$ROUTE_TABLE_ID" \
    --route-rules "[{\"cidrBlock\":\"0.0.0.0/0\",\"networkEntityId\":\"$IGW_ID\"}]" \
    --force
  echo "✅ Route Table 已更新"
else
  echo "✅ 使用已有 VCN: $VCN_ID"
  SUBNET_ID=$(oci network subnet list --compartment-id "$COMPARTMENT_ID" --vcn-id "$VCN_ID" --query "data[0].id" --raw-output)
  echo "✅ 使用已有 Subnet: $SUBNET_ID"
fi

# 5. 创建实例
echo ""
echo "🚀 创建实例（Always Free: 4 OCPU + 24GB RAM + 50GB Boot Volume）..."
INSTANCE_ID=$(oci compute instance launch \
  --compartment-id "$COMPARTMENT_ID" \
  --availability-domain "$AD" \
  --display-name "rss-pal" \
  --shape "VM.Standard.A1.Flex" \
  --shape-config "{\"ocpus\":4,\"memoryInGBs\":24}" \
  --source-details "{\"sourceType\":\"image\",\"imageId\":\"$IMAGE_ID\",\"bootVolumeSizeInGBs\":47}" \
  --subnet-id "$SUBNET_ID" \
  --ssh-authorized-keys-file ~/.ssh/rss-pal-key.pub \
  --query "data.id" \
  --raw-output)

echo "✅ 实例创建中: $INSTANCE_ID"

# 6. 等待实例就绪
echo "等待实例启动..."
oci compute instance get --instance-id "$INSTANCE_ID" --wait-for-state RUNNING --query "data.\"lifecycle-state\"" --raw-output

# 7. 获取公网 IP
PUBLIC_IP=$(oci compute instance list-vnics --instance-id "$INSTANCE_ID" --query "data[0].\"public-ip\"" --raw-output)

echo ""
echo "══════════════════════════════════════════════"
echo "  🎉 实例创建成功！"
echo "  📋 实例 ID: $INSTANCE_ID"
echo "  🌐 公网 IP: $PUBLIC_IP"
echo ""
echo "  SSH 登录："
echo "    ssh opc@$PUBLIC_IP"
echo ""
echo "  登录后部署："
echo "    sudo bash /tmp/deploy-oracle.sh"
echo "══════════════════════════════════════════════"
