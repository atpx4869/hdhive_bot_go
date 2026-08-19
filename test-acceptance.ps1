# HDHive Bot Go - P0 验收测试脚本 (PowerShell)
# 使用方法：.\test-acceptance.ps1

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "HDHive Bot Go - P0 验收测试" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan

# 检查环境变量
function Test-EnvVar {
    param([string]$Name)
    $value = [Environment]::GetEnvironmentVariable($Name)
    if ([string]::IsNullOrEmpty($value)) {
        Write-Host "ERROR: 环境变量 $Name 未设置" -ForegroundColor Red
        return $false
    }
    Write-Host "OK: $Name 已设置" -ForegroundColor Green
    return $true
}

Write-Host ""
Write-Host "=== 1. 环境变量检查 ===" -ForegroundColor Yellow
$envOk = $true
$envOk = (Test-EnvVar "TELEGRAM_BOT_TOKEN") -and $envOk
$envOk = (Test-EnvVar "TELEGRAM_ADMIN_USER_IDS") -and $envOk
$envOk = (Test-EnvVar "TMDB_TOKEN") -and $envOk
$envOk = (Test-EnvVar "HDHIVE_BASE_URL") -and $envOk
$envOk = (Test-EnvVar "HDHIVE_SECRET") -and $envOk
$envOk = (Test-EnvVar "HDHIVE_USER_ID") -and $envOk
$envOk = (Test-EnvVar "HDHIVE_USER_KEY") -and $envOk
$envOk = (Test-EnvVar "SQLITE_DSN") -and $envOk
$envOk = (Test-EnvVar "ENCRYPTION_KEY") -and $envOk

if (-not $envOk) {
    Write-Host ""
    Write-Host "请先设置环境变量，示例：" -ForegroundColor Yellow
    Write-Host '`$env:TELEGRAM_BOT_TOKEN = "123456:ABC-DEF..."' -ForegroundColor Gray
    Write-Host '`$env:TELEGRAM_ADMIN_USER_IDS = "123456789"' -ForegroundColor Gray
    Write-Host '`$env:TMDB_TOKEN = "your-tmdb-token"' -ForegroundColor Gray
    Write-Host '`$env:SQLITE_DSN = "file:test.db"' -ForegroundColor Gray
    Write-Host '`$env:ENCRYPTION_KEY = (openssl rand -base64 32)' -ForegroundColor Gray
}

Write-Host ""
Write-Host "=== 2. 编译检查 ===" -ForegroundColor Yellow
Write-Host "正在编译..."
$env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
go build ./cmd/worker
if ($LASTEXITCODE -eq 0) {
    Write-Host "OK: worker 编译成功" -ForegroundColor Green
} else {
    Write-Host "ERROR: worker 编译失败" -ForegroundColor Red
}

go build ./cmd/migrate
if ($LASTEXITCODE -eq 0) {
    Write-Host "OK: migrate 编译成功" -ForegroundColor Green
} else {
    Write-Host "ERROR: migrate 编译失败" -ForegroundColor Red
}

go build ./cmd/export
if ($LASTEXITCODE -eq 0) {
    Write-Host "OK: export 编译成功" -ForegroundColor Green
} else {
    Write-Host "ERROR: export 编译失败" -ForegroundColor Red
}

Write-Host ""
Write-Host "=== 3. 单元测试 ===" -ForegroundColor Yellow
Write-Host "正在运行测试..."
go test ./... -count=1 2>&1 | Select-Object -Last 15

Write-Host ""
Write-Host "=== 4. Docker 构建检查 ===" -ForegroundColor Yellow
$dockerPath = Get-Command docker -ErrorAction SilentlyContinue
if ($dockerPath) {
    Write-Host "正在构建 Docker 镜像..."
    docker build -t hdhive-bot-go:test . 2>&1 | Select-Object -Last 5
    if ($LASTEXITCODE -eq 0) {
        Write-Host "OK: Docker 镜像构建成功" -ForegroundColor Green
    } else {
        Write-Host "WARNING: Docker 构建失败" -ForegroundColor Yellow
    }
} else {
    Write-Host "SKIP: Docker 未安装" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "=== 5. 凭据扫描 ===" -ForegroundColor Yellow
Write-Host "检查是否有硬编码凭据..."
$found = Select-String -Path "*.go" -Pattern "bot\d+:[A-Za-z0-9_-]{35}" -ErrorAction SilentlyContinue
if ($found) {
    Write-Host "WARNING: 发现可能的 Bot Token" -ForegroundColor Red
} else {
    Write-Host "OK: 未发现硬编码凭据" -ForegroundColor Green
}

Write-Host ""
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "P0 验收检查完成" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "接下来需要手动测试：" -ForegroundColor Yellow
Write-Host "1. 启动 worker: go run ./cmd/worker"
Write-Host "2. 在 Telegram 中发送 /start"
Write-Host "3. 测试搜索: 发送电影名称"
Write-Host "4. 测试资源查询: 点击搜索结果"
Write-Host "5. 测试解锁: 点击资源按钮"
Write-Host "6. 测试 115 转存: 配置后转存"
Write-Host ""
Write-Host "如需测试迁移：" -ForegroundColor Yellow
Write-Host "go run ./cmd/migrate --users telegram_users.json --p115 telegram_p115.json"
