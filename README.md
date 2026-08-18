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

## Docker Compose

```sh
cp config.example.env config.env
docker compose up -d --build
```

镜像为多阶段构建，运行阶段使用 UID 10001 非 root 用户、不声明端口；命名卷挂载 `/data`，服务使用 `restart: unless-stopped`。

容器内迁移示例：

```sh
docker compose run --rm --entrypoint /usr/local/bin/migrate worker --users /data/telegram_users.json --p115 /data/telegram_p115.json
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
- `/my115` 只显示启用状态和“根目录/已配置目录”，不会显示 Cookie 或掩码。
- 普通用户 `/unset115` 需按钮确认并采用 `enabled=false` 软删除；管理员配置不能通过 Bot 停用。
- `/cancel` 可退出正在进行的 115 配置。
- 无法确认的 HDHive 解锁错误统一提示 `unknown，请联系管理员` 并禁止重复付费。管理员人工核验后可执行 `/unlockreset <user_id> <resource_id>`，该命令只解除 `unknown`，不会解除仍可能活跃的 `in_flight`、自动重新解锁或清除成功记录。
- 115 转存按 `user_id + resource_id` 合并并发请求，并缓存完成结果，防止重复调用。

## 数据与安全

- SQLite 和迁移源文件放在 `/data` 持久卷。
- 115 Cookie 仅加密落库，日志不应输出 Cookie、Bot Token 或其他凭据。
- 加密密钥丢失后旧 Cookie 无法解密；轮换密钥前需设计重加密流程。
- 切换部署前停止旧 polling worker，Telegram 同一 token 不应并行 long polling。
