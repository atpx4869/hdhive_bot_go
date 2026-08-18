# 开发交接文档

本文用于将项目上传到 GitHub 后，在另一台机器或新会话中快速恢复开发上下文。

## 1. 项目目标

本项目是原 Python 项目 `hdhive_proxy` 的独立 Go 重构版本，只保留 Telegram Bot 相关能力：

- 管理员与授权用户管理
- TMDB 影视搜索
- HDHive 资源查询与解锁
- 每用户独立的 115 Cookie 配置
- 115 分享转存
- 用户活动日志
- 旧 JSON 数据迁移

明确不包含：

- FastAPI / Uvicorn
- ForwardWidget
- 播放直链、302 跳转和 CDN 反代
- HTTP 服务端口和 Web 管理后台

运行形态为单实例 Telegram long-polling worker。

## 2. 当前状态

当前代码已完成首版功能实现，但尚未使用生产凭据进行完整端到端验收，也尚未创建 Git 初始提交。

已通过：

```sh
go test ./...
go vet ./...
go build ./...
git diff --check
```

最终安全审查结果：无 CRITICAL/HIGH 阻塞项。

最终独立正确性审查结果：无 blocking 问题。

## 3. 目录说明

```text
cmd/worker/        Telegram worker 入口
cmd/migrate/       旧 JSON 数据迁移入口
internal/app/      依赖装配、生命周期和各服务 adapter
internal/config/   环境变量加载与严格校验
internal/crypto/   AES-256-GCM 加密
internal/hdhive/   HDHive 会话、签名、查询和解锁客户端
internal/migrate/  telegram_users.json / telegram_p115.json 导入
internal/p115/     115 分享读取和转存客户端
internal/session/  Callback、交互会话和内存解锁状态
internal/store/    SQLite schema、用户、115、解锁和日志存储
internal/telegram/ 命令、Callback、格式化和 Bot adapter
internal/tmdb/     TMDB v3/v4 搜索客户端
```

## 4. 核心设计决策

### 4.1 单实例 Worker

不监听 HTTP 端口。Telegram polling 返回或发生异常时进程退出，由 Docker：

```yaml
restart: unless-stopped
```

负责恢复。同一个 Bot Token 不得同时运行多个实例。

### 4.2 SQLite

使用：

```text
database/sql + modernc.org/sqlite
```

主要数据表：

```text
users
p115_accounts
unlock_records
activity_logs
```

SQLite 默认只使用一个连接，以降低单 worker 下的并发和锁复杂度。

### 4.3 敏感信息加密

115 Cookie 和 HDHive 解锁结果使用 AES-256-GCM 加密后写入 SQLite：

- 密钥来自 `ENCRYPTION_KEY`
- 密钥必须是 32 字节的 Base64 编码
- 每次加密使用随机 nonce
- Telegram user ID 作为 AAD

`ENCRYPTION_KEY` 丢失后，数据库中的 Cookie 和解锁结果无法恢复。当前尚未实现密钥轮换。

### 4.4 解锁防重复

解锁可能产生积分消耗，因此使用两层保护：

1. 内存 session 状态机，防止重复点击。
2. SQLite `unlock_records` 持久化状态，防止进程重启或 session 过期后重复扣费。

SQLite 使用复合主键：

```text
(user_id, resource_id)
```

通过：

```sql
INSERT ... ON CONFLICT DO NOTHING
```

实现原子 claim。状态包括：

```text
in_flight
success
rejected
unknown
```

`success` 保存加密后的分享信息；`in_flight` 和 `unknown` 默认禁止再次提交，需要人工核验。

HDHive 解锁成功后使用独立的 5 秒 context 保存结果，避免 Telegram 请求 context 被取消后丢失已付费结果。

### 4.5 用户隔离

- Callback token 绑定 Telegram user ID。
- 115 Cookie 按 user ID 独立加密保存。
- HDHive 分享信息只写入当前用户的解锁记录。
- 115 转存必须存在当前用户成功解锁的证明。
- 全局资源缓存只保存非敏感元数据。

### 4.6 `/set115` 安全限制

`/set115` 只能在 Bot 私聊中使用，后续 Cookie 输入也会校验当前 chat 是否为私聊。

目前 Go Telegram adapter 尚未实现自动删除包含 Cookie 的入站消息，这是待办项。

## 5. 外部服务实现情况

### TMDB

支持：

- 32 位 v3 API key
- v4 Read Access Token
- `/3/search/multi`
- 中文搜索和分页

### HDHive

已实现：

- nonce 会话协商
- client/server proof
- session key 派生
- HMAC 请求签名
- sequence
- session 更新后 sequence 归零
- 403 鉴权失败后重建 session
- 资源查询
- 资源解锁
- 宽松响应解析

生产配置强制使用 HTTPS。

### 115

已实现：

- 分享链接解析
- `GET /share/snap` 分页
- `GET /files` 根目录读取
- `POST /share/receive`
- 单目录分享转存
- 多根项目 ID 列表转存
- `target_cid`
- 重复接收识别
- 错误分类和日志脱敏

旧项目开发过程中已经真实验证：

- 公开分享根目录可以直接读取
- Cookie 可以读取用户根目录一级文件夹

当前 Go 项目尚未执行真实 `share/receive`，以免未经确认修改网盘。

## 6. 配置

以 `config.example.env` 为准。关键环境变量：

```env
TELEGRAM_BOT_TOKEN=
TELEGRAM_ADMIN_USER_IDS=
TMDB_TOKEN=
HDHIVE_BASE_URL=
HDHIVE_SECRET=
HDHIVE_USER_ID=
HDHIVE_USER_KEY=
SQLITE_DSN=file:/data/bot.db
ENCRYPTION_KEY=
HTTP_PROXY_URL=
HTTP_TIMEOUT=30s
SESSION_TTL=30m
SESSION_CAPACITY=1000
```

生成加密密钥：

```sh
openssl rand -base64 32
```

不要将 `config.env`、数据库或旧明文 Cookie JSON 提交到 Git。

## 7. 本地开发

```sh
cp config.example.env config.env
# 将 config.env 加载到环境变量
go run ./cmd/worker
```

标准检查：

```sh
gofmt -w cmd internal
go mod tidy
go test ./...
go vet ./...
go build ./...
```

## 8. Docker

```sh
cp config.example.env config.env
docker compose up -d --build
```

项目不开放端口，数据持久化到 `/data`。

## 9. 旧数据迁移

```sh
go run ./cmd/migrate \
  --users /path/telegram_users.json \
  --p115 /path/telegram_p115.json
```

迁移支持重复执行。迁移前必须：

1. 停止旧 Python Bot。
2. 备份旧 JSON 和数据库。
3. 确保只有一个实例消费 Telegram Bot Token。

## 10. 新环境恢复步骤

克隆仓库后建议按以下顺序恢复：

```sh
git clone https://github.com/atpx4869/hdhive_bot_go.git
cd hdhive_bot_go
go version
go mod download
go test ./...
go vet ./...
go build ./...
cp config.example.env config.env
```

随后阅读：

```text
README.md
DEVELOPMENT.md
TODO.md
```

开始开发前执行：

```sh
git status
git log --oneline -5
```

## 11. 发布前注意事项

- 当前尚未创建初始 commit。
- 上传 GitHub 前应先检查仓库中没有真实凭据。
- 曾用于开发验证的 115 Cookie 已出现在旧会话中，应轮换后再投入使用。
- 首次生产启动应使用测试 Bot 或低风险账号进行完整验收。
- 不应让旧 Python Bot 与新 Go Bot 同时使用同一个 Token。
