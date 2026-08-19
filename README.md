# HDHive Telegram Worker

纯 Telegram long-polling worker：TMDB 搜索、HDHive 资源查询/解锁、115 转存、管理员授权及 SQLite 持久化。进程不监听 HTTP 端口；polling 返回或发生请求异常时退出，以便由容器重启策略恢复。

开发交接与后续计划：

- [`DEVELOPMENT.md`](DEVELOPMENT.md) — 架构、关键设计、当前状态和新环境恢复步骤
- [`TODO.md`](TODO.md) — 按优先级排列的后续任务和提交检查清单

## 配置

复制 `config.example.env` 为 `config.env`。必须配置 Telegram token、管理员 ID、TMDB token、HDHive 代理地址及认证字段、SQLite DSN 和 32 字节 base64 加密密钥。`HTTP_PROXY_URL` 可选，应用所有外部 HTTP 请求（Telegram、TMDB、HDHive、115）共用该代理。

不得提交真实凭据。生成密钥：

```sh
openssl rand -base64 32
```

## 本地运行

```sh
go run ./cmd/worker
```

worker 响应 `SIGINT`/`SIGTERM`，取消 polling 并关闭 SQLite。当前 session/callback 存于内存，只应运行一个副本。

## 旧 JSON 迁移

先停止旧 Bot，备份 JSON 与数据库，避免两个进程同时使用同一个 Bot Token。迁移命令读取旧 `telegram_users.json` 的 `authorized_user_ids`/`notes`，以及 `telegram_p115.json` 的 `cookie`、`target_cid`、`enabled`；Cookie 会使用 `ENCRYPTION_KEY` 以 AES-256-GCM 加密后写入 SQLite。命令可重复执行（upsert）：

```sh
go run ./cmd/migrate --users /path/telegram_users.json --p115 /path/telegram_p115.json
```

也可只指定其中一个文件。迁移后妥善销毁或限制旧明文 Cookie 文件权限。

## Docker Compose 部署

### 快速开始

```sh
# 1. 克隆仓库
git clone https://github.com/atpx4869/hdhive_bot_go.git
cd hdhive_bot_go

# 2. 复制并编辑配置
cp config.example.env config.env
# 编辑 config.env 填入真实配置

# 3. 创建数据目录
mkdir -p data

# 4. 启动服务
docker compose up -d

# 5. 查看日志
docker compose logs -f
```

### NAS 部署（推荐）

#### 1. 准备配置文件

在 NAS 上创建项目目录：

```bash
mkdir -p /volume1/docker/hdhive-bot-go
cd /volume1/docker/hdhive-bot-go
```

创建 `config.env`：

```env
TELEGRAM_BOT_TOKEN=你的Bot Token
TELEGRAM_ADMIN_USER_IDS=你的Telegram用户ID
TMDB_TOKEN=你的TMDB Token
HDHIVE_BASE_URL=https://你的HDHive代理地址
HDHIVE_SECRET=你的HDHive密钥
HDHIVE_USER_ID=你的HDHive用户ID
HDHIVE_USER_KEY=你的HDHive访问密钥
SQLITE_DSN=file:/data/bot.db
ENCRYPTION_KEY=生成的32字节base64密钥
HTTP_PROXY_URL=http://192.168.5.3:7893
HTTP_TIMEOUT=30s
TMDB_TIMEOUT=15s
HDHIVE_TIMEOUT=30s
P115_TIMEOUT=30s
SESSION_TTL=30m
SESSION_CAPACITY=1000
```

#### 2. 创建 docker-compose.yaml

```yaml
services:
  worker:
    image: jzrm/hdhive-bot-go:latest
    container_name: hdhive-bot-go
    restart: unless-stopped
    env_file:
      - config.env
    volumes:
      - ./data:/data
    # 安全加固
    read_only: true
    tmpfs:
      - /tmp
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
```

> **注意**：项目根目录中的 Compose 文件名为 `compose.yaml`（Docker Compose V2 推荐格式）。上文为 NAS 部署的完整示例。

#### 3. 启动服务

```bash
docker compose up -d
```

#### 4. 查看日志

```bash
# 实时日志
docker compose logs -f

# 最近100行
docker compose logs --tail 100
```

#### 5. 停止服务

```bash
docker compose down
```

### 从源码构建

如果需要从源码构建镜像：

```bash
# 克隆仓库
git clone https://github.com/atpx4869/hdhive_bot_go.git
cd hdhive_bot_go

# 构建镜像
docker build -t hdhive-bot-go .

# 启动
docker compose up -d
```

### 数据备份

数据存储在 `./data` 目录，包含：
- `bot.db` - SQLite 数据库（用户、配置、解锁记录、活动日志）

备份命令：

```bash
# 备份数据目录
tar -czf hdhive-bot-backup-$(date +%Y%m%d).tar.gz data/

# 或直接复制
cp -r data/ /backup/hdhive-bot-data/
```

### 迁移旧数据

如果从旧 Python 项目迁移：

```bash
# 1. 停止旧 Bot
docker stop hdhive-proxy  # 或其他旧容器名

# 2. 复制旧数据文件到 data 目录
cp /path/to/telegram_users.json data/
cp /path/to/telegram_p115.json data/

# 3. 运行迁移
docker compose run --rm --entrypoint /usr/local/bin/migrate worker \
  --users /data/telegram_users.json \
  --p115 /data/telegram_p115.json

# 4. 启动新 Bot
docker compose up -d
```

### 配置检查

运行配置检查工具验证配置是否正确：

```bash
docker compose run --rm --entrypoint /usr/local/bin/check worker
```

## 验证

```sh
gofmt -w cmd internal/app internal/migrate internal/config internal/store internal/telegram
go mod tidy
go test ./...
go vet ./...
go build ./...
```

## 115 配置与异常恢复

- `/set115` 使用两步交互：先发送包含 `UID`、`CID`、`SEID` 的完整 Cookie（Bot 会尝试删除该消息），再发送目标目录 cid；`0` 表示根目录。
- `/my115` 显示配置状态和目标目录，并支持修改目标目录。
- 普通用户 `/unset115` 需按钮确认并采用 `enabled=false` 软删除；管理员配置不能通过 Bot 停用。
- `/cancel` 可退出正在进行的 115 配置。
- 无法确认的 HDHive 解锁错误统一提示 `unknown，请联系管理员` 并禁止重复付费。管理员人工核验后可执行 `/unlockreset <user_id> <resource_id>`，该命令只解除 `unknown`，不会解除仍可能活跃的 `in_flight`、自动重新解锁或清除成功记录。
- 115 转存按 `user_id + resource_id` 合并并发请求，并缓存完成结果，防止重复调用。

## 管理员命令

| 命令 | 说明 |
|------|------|
| `/authorize <id>` | 授权用户 |
| `/revoke <id>` | 撤销授权 |
| `/users` | 查看用户列表 |
| `/note <id> <备注>` | 设置用户备注 |
| `/logs` | 查看活动日志 |
| `/unlockreset <user_id> <resource_id>` | 解除 unknown 状态 |
| `/enable115 <user_id>` | 启用用户 115 配置 |
| `/disable115 <user_id>` | 停用用户 115 配置 |
| `/unknown` | 查看 unknown 解锁记录 |

## 用户命令

| 命令 | 说明 |
|------|------|
| `/start` | 查看状态面板 |
| `/myid` | 查看 Telegram ID |
| `/set115` | 配置 115 账号 |
| `/my115` | 查看/修改 115 配置 |
| `/unset115` | 停用 115 配置 |
| `/cancel` | 取消当前操作 |

## 数据与安全

- SQLite 和迁移源文件放在 `/data` 持久卷。
- 115 Cookie 仅加密落库，日志不应输出 Cookie、Bot Token 或其他凭据。
- 加密密钥丢失后旧 Cookie 无法解密；轮换密钥前需设计重加密流程。
- 切换部署前停止旧 polling worker，Telegram 同一 token 不应并行 long polling。

## 故障排查

### Bot 无响应

1. 检查日志：`docker compose logs --tail 100`
2. 检查是否有其他 Bot 实例在运行
3. 检查网络代理配置

### 115 转存失败

1. 检查 115 Cookie 是否过期
2. 使用 `/my115` 查看配置状态
3. 重新配置：发送 `/set115`

### 解锁 unknown 状态

1. 使用 `/unknown` 查看记录
2. 确认是否已扣费
3. 使用 `/unlockreset <user_id> <resource_id>` 解除

## 许可证

MIT License
