# TODO

本文按优先级记录后续开发任务。完成任务时请同时补充测试，并更新 `DEVELOPMENT.md` 中受影响的设计说明。

## P0：首次发布前必须完成

- [ ] 创建 Git 初始提交并推送到 [`atpx4869/hdhive_bot_go`](https://github.com/atpx4869/hdhive_bot_go)。
- [ ] 对仓库执行凭据扫描，确认不存在真实 Bot Token、TMDB Token、HDHive 凭据、115 Cookie 或 `ENCRYPTION_KEY`。
- [ ] 轮换开发期间曾暴露的 115 Cookie，不再使用旧 Cookie。
- [ ] 使用测试 Bot Token 启动 worker，验证 Telegram long polling、命令注册和 graceful shutdown。
- [ ] 使用真实 TMDB Token 验证中文电影、剧集搜索和分页。
- [ ] 使用真实 HDHive 测试账号验证 session 协商、资源查询、解锁和 403 session 重建。
- [ ] 使用低风险或已解锁资源验证 115 单目录转存。
- [ ] 使用测试分享验证 115 多文件/多目录 ID 列表转存。
- [ ] 重复转存同一分享，验证 `already received` 的幂等结果。
- [ ] 验证 `target_cid` 实际落入指定 115 目录。
- [ ] 验证进程重启后，成功解锁记录能恢复并继续转存。
- [ ] 验证 `in_flight` / `unknown` 记录不会再次调用付费解锁。
- [ ] 运行旧 JSON 迁移，核对用户、备注、115 Cookie、`target_cid` 和 `enabled`。
- [ ] 在 Linux/Docker 环境完成 `docker compose up -d --build` 验收。

## P1：核心可靠性

- [x] 为 `internal/app` 增加 adapter 和生命周期单元测试。
- [x] 为 `internal/migrate` 增加完整迁移测试，包括损坏 JSON、重复导入和明文 Cookie 不落库。
- [x] 为 `unlock_records` 增加并发 claim 测试，证明同一 `(user_id, resource_id)` 只有一个请求成功。
- [ ] 增加“解锁成功后请求 context 取消，结果仍能保存”的回归测试。
- [ ] 增加“数据库查询/解密失败时禁止继续解锁”的回归测试。
- [ ] 增加“进程重启后从 SQLite 恢复分享信息并转存”的集成测试。
- [x] 将 HDHive `unknown` 状态增加管理员人工核验/解除机制，避免永久锁死（`/unlockreset <user_id> <resource_id>`）。
- [x] 为 `in_flight` 增加安全的超时或租约恢复策略；活跃请求不得被管理员直接解除，避免并行重复扣费。
- [ ] 定义稳定的业务错误码，并将 TMDB、HDHive、115 错误映射成用户可理解的提示。
- [x] 区分 HDHive 业务拒绝和网络不确定：明确拒绝可标记 `rejected`，只有结果不确定才标记 `unknown`。
- [x] 对 Telegram handler 增加统一错误日志和用户重试按钮。
- [x] 增加 115 转存 keyed lock / 完成状态缓存，防止用户并发重复转存。
- [x] 为 TMDB、HDHive 和 115 设置更细的 connect/read/write 超时，而不只使用总超时。
- [ ] 为 115 Web API 增加可配置 User-Agent 和 endpoint，便于接口变化时快速调整。

## P1：安全

- [x] `/set115` 成功读取 Cookie 后，尝试删除用户发送的 Cookie 消息；失败时提示用户手动删除。
- [x] `/my115` 不再显示 Cookie 掩码，只显示“已配置/已停用”和目标目录。
- [x] 增加日志脱敏 middleware，统一屏蔽 Bot Token、TMDB Token、HDHive Secret/User Key、115 Cookie 和访问码。
- [x] 为错误链增加脱敏测试，确保 `http.Client` 和第三方 Telegram library 的错误不会包含完整 URL/Token。
- [x] 设计并实现 `ENCRYPTION_KEY` 轮换命令，对 SQLite 中的 Cookie 和解锁结果重新加密。
- [x] 启动时检查 SQLite 文件权限；Linux 下建议限制为 `0600`。
- [x] Docker Compose 增加 `read_only`、`tmpfs`、`security_opt: no-new-privileges:true` 和 capability drop 的可行性验证。
- [ ] 为迁移源 JSON 增加权限警告，迁移完成后提示用户安全删除旧文件。
- [x] 管理员列表以环境变量为最高权威，增加测试确保数据库用户无法提升为管理员。

## P2：Telegram UI/UX

- [x] `/start` 改为状态面板，显示授权状态、115 配置状态和管理员快捷入口。
- [x] 搜索结果按钮显示年份，区分同名新版/旧版影视。
- [ ] 增加 TMDB “更多结果”和“重新搜索”按钮。
- [ ] 资源页标题显示影视名称，而不只显示资源数据。
- [x] 资源列表显示网盘、画质、大小、字幕、来源、积分和已解锁状态。
- [ ] 增加资源筛选：115、免费/已解锁、4K、积分排序、体积排序。
- [ ] 所有收费或费用未知资源必须经过确认；免费资源也建议显示明确提示。
- [ ] 解锁成功后提供“转存到 115”“返回资源列表”“新搜索”快捷按钮。
- [ ] 转存失败时根据错误类型提供“重新配置 115”“重试”“换线路”等操作。
- [ ] `/users` 和 `/logs` 增加分页、过滤和 Callback owner 校验测试。
- [x] `/set115` 改为分步交互：先 Cookie，再填写目标目录 cid；支持 `/cancel` 退出。
- [x] 增加 `/cancel`，用于退出 `/set115` 等交互状态。
- [x] `/unset115` 增加管理员保护、普通用户二次确认、配置版本校验和 `enabled=false` 软删除。

## P2：数据和运维

- [ ] 为活动日志增加 `status`、`media_title`、`resource_title`、`error_code` 等结构化字段，减少当前 `detail` 自由文本。
- [ ] 增加活动日志保留天数和最大数据库体积清理任务。
- [ ] 增加 SQLite WAL 模式和 busy timeout 的评估与测试。
- [ ] 增加数据库备份、恢复和完整性检查文档。
- [ ] 增加应用版本、启动时间和依赖版本日志。
- [ ] 增加 polling 重启次数、搜索/解锁/转存成功率和延迟指标；若仍坚持无 HTTP 端口，可输出结构化日志供采集。
- [ ] 增加 Docker image CI：测试、vet、build、多架构镜像和依赖缓存。
- [ ] 增加 Dependabot/Renovate 更新 Go modules 和 GitHub Actions。
- [ ] 增加 release workflow 和语义化版本标签。

## P2：架构与代码质量

- [ ] 将 `internal/app/adapters.go` 中的大型映射逻辑拆分为独立 mapper/service。
- [ ] 给 TMDB、HDHive、115 clients 增加统一的 request ID、重试策略和错误接口。
- [ ] 为 HDHive 签名创建 Python/Go golden vectors，确保协议完全一致。
- [ ] 检查当前 HKDF/session key 派生是否与生产 Python 实现逐字节一致，并记录协议说明。
- [ ] 对 HDHive 资源排序、季集匹配和 115 资源过滤进行显式建模，不继续依赖松散 `map[string]any`。
- [ ] 将 `telegram.Resource`、TMDB 和 HDHive 数据模型中的多字符串字段拆成可读字段。
- [ ] 为 Callback value 增加版本化编码，避免字段分隔符导致兼容问题。
- [ ] 检查 Telegram callback data 长度始终不超过 64 bytes。
- [ ] 建立 `internal/domain`，减少 Telegram 层直接依赖 Store 类型。
- [ ] 使用接口隔离 Store，避免 Handler 直接绑定具体 SQLite 模型。

## P3：可选功能

- [ ] 支持管理员在 Bot 中启用/停用用户的 115 配置。
- [ ] 支持用户查看和选择 115 目标目录。
- [ ] 支持管理员查看当前 `unknown` 解锁记录并进行人工处理。
- [ ] 支持配置多个 HDHive 账号或按用户隔离 HDHive 身份。
- [ ] 支持 SQLite 数据导出为脱敏 JSON。
- [ ] 评估是否需要 webhook 模式；当前 long polling 更符合单 worker 需求，不应无理由增加 HTTP 服务。

## 每次提交前检查清单

```sh
gofmt -w cmd internal
go mod tidy
go test ./...
go vet ./...
go build ./...
git diff --check
git status --short
```

另外必须确认：

- [ ] 没有真实凭据。
- [ ] 没有数据库、Cookie 文件和本地 `config.env`。
- [ ] 新增外部请求有 timeout 和 context。
- [ ] 新增解锁路径经过 SQLite 原子防重复。
- [ ] 新增敏感数据按 user ID 加密和隔离。
- [ ] 任何可能扣积分的操作都有确认和不确定状态处理。
