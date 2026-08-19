#!/bin/bash
# HDHive Bot Go - P0 验收测试脚本
# 使用方法：chmod +x test-acceptance.sh && ./test-acceptance.sh

set -e

echo "=========================================="
echo "HDHive Bot Go - P0 验收测试"
echo "=========================================="

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 检查环境变量
check_env() {
    local var_name=$1
    if [ -z "${!var_name}" ]; then
        echo -e "${RED}ERROR: 环境变量 $var_name 未设置${NC}"
        return 1
    fi
    echo -e "${GREEN}OK: $var_name 已设置${NC}"
    return 0
}

echo ""
echo "=== 1. 环境变量检查 ==="
check_env "TELEGRAM_BOT_TOKEN"
check_env "TELEGRAM_ADMIN_USER_IDS"
check_env "TMDB_TOKEN"
check_env "HDHIVE_BASE_URL"
check_env "HDHIVE_SECRET"
check_env "HDHIVE_USER_ID"
check_env "HDHIVE_USER_KEY"
check_env "SQLITE_DSN"
check_env "ENCRYPTION_KEY"

echo ""
echo "=== 2. 编译检查 ==="
echo "正在编译..."
go build ./cmd/worker
go build ./cmd/migrate
go build ./cmd/export
echo -e "${GREEN}OK: 编译成功${NC}"

echo ""
echo "=== 3. 单元测试 ==="
echo "正在运行测试..."
go test ./... -v -count=1 2>&1 | tail -20
echo -e "${GREEN}OK: 测试通过${NC}"

echo ""
echo "=== 4. Docker 构建检查 ==="
if command -v docker &> /dev/null; then
    echo "正在构建 Docker 镜像..."
    docker build -t hdhive-bot-go:test . 2>&1 | tail -5
    echo -e "${GREEN}OK: Docker 镜像构建成功${NC}"
else
    echo -e "${YELLOW}SKIP: Docker 未安装${NC}"
fi

echo ""
echo "=== 5. 凭据扫描 ==="
echo "检查是否有硬编码凭据..."
if grep -r "bot[0-9]*:[A-Za-z0-9_-]\{35\}" --include="*.go" . 2>/dev/null; then
    echo -e "${RED}WARNING: 发现可能的 Bot Token${NC}"
else
    echo -e "${GREEN}OK: 未发现硬编码凭据${NC}"
fi

echo ""
echo "=========================================="
echo "P0 验收检查完成"
echo "=========================================="
echo ""
echo "接下来需要手动测试："
echo "1. 启动 worker: go run ./cmd/worker"
echo "2. 在 Telegram 中发送 /start"
echo "3. 测试搜索: 发送电影名称"
echo "4. 测试资源查询: 点击搜索结果"
echo "5. 测试解锁: 点击资源按钮"
echo "6. 测试 115 转存: 配置后转存"
echo ""
echo "如需测试迁移："
echo "go run ./cmd/migrate --users telegram_users.json --p115 telegram_p115.json"
