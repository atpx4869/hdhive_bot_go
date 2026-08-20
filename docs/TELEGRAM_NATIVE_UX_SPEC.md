# HDHive Bot：Telegram 原生 UI/UX 实施规格

> 状态：可实施草案
>
> 日期：2026-08-20
>
> 目标读者：负责修改本仓库的 AI / Go 开发者
>
> 首期范围：Telegram 原生消息，不引入 Mini App

## 1. 目标

把现有“每次点击都发送一条新消息”的流程改成 Telegram 原生卡片式交互：

- 一次搜索只创建一条主消息（下文称“主卡片”）。
- 选片、资源分页、资源详情、解锁确认、解锁结果和转存结果都原地更新主卡片。
- 使用海报、HTML caption、Inline Keyboard、按钮样式、URL Button 和 CopyTextButton。
- 敏感操作只在 Bot 私聊完成。
- 任何收费或费用未知的解锁都必须明确确认，且不能因 UI 重试而重复扣费。
- 手机和桌面端、浅色和深色主题都保持可读，不依赖固定宽度或颜色表达含义。

首期不做：

- Mini App。
- 收藏、观看历史、海报瀑布流。
- TMDB 详情二次请求、演职员、预告片、完整流派和片长。
- 群聊内直接解锁或转存。
- Inline Mode。接口设计应为后续增加 Inline Mode 留出空间，但不要在本期实现。

## 2. 核心设计原则

### 2.1 一次搜索，一张主卡片

用户发送新关键词时创建一张新主卡片。此后所有 callback 操作编辑这条消息，不新增业务消息。

用户再次发送关键词时：

1. 将上一张主卡片的键盘替换为单个中性按钮或纯状态文本“已被新搜索替代”。
2. 创建新的主卡片。
3. 更新当前 UI session 的 `ActiveMessageID`。

不要删除历史主卡片，避免 Telegram 删除失败造成状态混乱，也保留用户的搜索记录。

### 2.2 媒体只升级，不强制降级

Telegram 的文本消息可以升级为海报媒体消息，但媒体消息不应依赖“降级回纯文本”。渲染规则：

| 当前消息 | 新视图有海报 | 新视图无海报 | 操作 |
|---|---:|---:|---|
| 不存在 | 是 | — | `SendPhoto` |
| 不存在 | 否 | — | `SendMessage` |
| 纯文本 | 是 | — | `EditMessageMedia` |
| 纯文本 | 否 | — | `EditMessageText` |
| 媒体 | 是 | — | 海报变化时 `EditMessageMedia`，否则 `EditMessageCaption` |
| 媒体 | 否 | — | 保留当前海报，使用 `EditMessageCaption` |

如果新海报拉取失败：

- 当前为纯文本：回退为 `EditMessageText`。
- 当前已有媒体：保留旧海报，只更新 caption 和键盘。
- 不得因为海报失败让搜索、解锁或转存失败。

### 2.3 内容与动作分层

- 消息正文负责展示信息和当前状态。
- Inline Keyboard 负责动作，不在正文中堆叠命令提示。
- 每个页面只能有一个视觉上的主操作。
- 返回、分页、关闭使用中性样式。
- `primary` 仅用于当前主操作。
- `success` 仅用于确认解锁、转存等正向提交。
- `danger` 仅用于确实有风险的操作，例如删除 115 配置；普通“返回”或“关闭”不用红色。
- emoji 必须配文字，不能只靠 emoji 或按钮颜色表达含义。

## 3. 页面与交互规格

### 3.1 首页 `/start`

首页是独立状态面板，不属于搜索主卡片。

正文：

```text
🎬 HDHive

发送电影或剧集名称，即可搜索可用资源。

账号状态
授权        ✅ 已授权
115 网盘    ✅ 已配置

你的 ID：123456789
```

普通用户按钮：

```text
[ 🔎 开始搜索 ]
[ ⚙️ 115 设置 ] [ ❓ 使用帮助 ]
```

管理员额外增加：

```text
[ 🛠 管理面板 ]
```

“开始搜索”点击后不发送复杂页面，只回答 callback：`请直接发送影视名称`。可选择使用 ForceReply，引导用户输入。

首次发布新 UI 时，应移除旧的常驻 Reply Keyboard。可在 `/start` 时先发送带 `ReplyKeyboardRemove` 的短消息，再立即将同一消息更新为首页 Inline Keyboard；如果库或客户端表现不稳定，则只发送一次“已切换到新版界面”移除键盘消息，随后发送首页。

### 3.2 搜索中

用户发送关键词后应立即给出反馈：

```text
🔎 正在搜索「流浪地球」…
```

键盘临时替换为：

```text
[ ⏳ 正在加载… ]
```

该按钮使用 `noop` callback，避免用户在请求期间重复提交。TMDB 返回后更新同一条消息。

### 3.3 搜索结果

每页显示最多 6 个结果；每个结果一行按钮。搜索结果正文保持简短，完整选择依靠按钮。

正文示例：

```text
🔎 搜索结果

关键词：流浪地球
找到多个匹配，请选择正确版本。

第 1 / 3 页
```

按钮示例：

```text
[ 流浪地球 2 · 2023 · ⭐ 8.3 ]
[ 流浪地球 · 2019 · ⭐ 7.9 ]
[ 流浪地球：飞跃2020特别版 · 2020 ]
[ ‹ 上一页 ] [ 1 / 3 ] [ 下一页 › ]
[ ✕ 关闭 ]
```

规则：

- 电影和剧集同名时，在按钮里加入 `电影` / `剧集`。
- 标题按 rune 截断，不按 byte 截断；建议按钮总长度不超过 42 个可见字符。
- 评分为 0 时不展示评分。
- 没有年份时不输出空括号。
- 中间的 `1 / 3` 是 `noop` callback。
- 第一页不显示“上一页”，最后一页不显示“下一页”；不要放不可点击的假按钮占位。
- 结果为空时，编辑为“没有找到结果”，提供“重新输入”和“帮助”按钮。

海报策略：

- 优先使用本页第一个有海报的结果作为搜索卡片媒体。
- 本页没有海报时保持当前媒体；如果消息仍是纯文本则继续使用文本消息。

### 3.4 影片卡片

选择搜索结果后，将主卡片更新为影片卡片。

正文示例：

```text
🎬 流浪地球 2
The Wandering Earth II

电影 · 2023 · ⭐ 8.3 / 10

太阳危机即将来袭，人类决定建造巨大的行星发动机……
```

按钮：

```text
[ 🔎 查看资源 ]
[ ‹ 搜索结果 ] [ ✕ 关闭 ]
```

首期字段只使用已有数据：标题、原名、电影/剧集、年份、评分、简介、海报。不要为了片长和流派增加 TMDB 详情请求。

格式规则：

- 中文标题和原名相同则只显示一次。
- 简介建议截断到 180 个中文字符，末尾使用 `…`。
- HTML caption 最终不得超过 1024 字符；formatter 应保留安全余量，目标上限 900 rune。
- 所有外部文本必须 `html.EscapeString`。
- 海报 URL 使用 TMDB `poster_path` 生成，例如 `https://image.tmdb.org/t/p/w780{poster_path}`。

### 3.5 资源列表

每页最多 6 条资源。正文中的序号必须与按钮序号一一对应。

正文示例：

```text
📦 流浪地球 2
找到 12 个可用资源

① 4K HDR · 18.6 GB
   115 · 中文字幕 · 免费

② 4K DV · 24.1 GB
   115 · 国英双语 · 5 积分

③ 1080P · 8.2 GB
   阿里云盘 · 已解锁

第 1 / 2 页
```

资源按钮：

```text
[ ① 4K HDR · 18.6G · 免费 ]
[ ② 4K DV · 24.1G · 5积分 ]
[ ③ 1080P · 8.2G · 已解锁 ]
[ ‹ 上一页 ] [ 1 / 2 ] [ 下一页 › ]
[ ‹ 返回影片 ] [ ✕ 关闭 ]
```

首期筛选可作为第二阶段功能。若实施筛选，顺序固定为：

```text
[ 115 ✓ ] [ 免费 ] [ 4K ]
```

筛选按钮再次点击即取消；状态必须体现在文字中，例如 `115 ✓`，不能只靠颜色。

排序建议：

1. 已解锁。
2. 免费。
3. 115。
4. 画质优先（4K > 1080P > 720P > 未知）。
5. 原接口顺序。

除非已经对 `Quality`、`Size`、字幕字段显式建模，否则不要做复杂积分或体积排序。

### 3.6 资源详情

正文示例：

```text
📄 资源详情

流浪地球 2

网盘    115
画质    4K HDR
大小    18.6 GB
来源    HDHive
费用    5 积分
状态    未解锁

该操作可能消耗积分。
```

未解锁按钮：

```text
[ 🔓 解锁资源 ]
[ ‹ 返回资源列表 ] [ ✕ 关闭 ]
```

已解锁按钮：

```text
[ 📥 转存到 115 ]
[ 🔗 打开资源 ] [ 📋 复制提取码 ]
[ ‹ 返回资源列表 ] [ ✕ 关闭 ]
```

规则：

- `打开资源` 使用 URL Button，不把完整 URL 暴露在正文。
- `复制提取码` 使用 CopyTextButton。
- 只有提取码非空时显示复制按钮。
- 只有 URL 通过 `https` 或明确允许的 `http` 校验时显示 URL Button。
- 不要把 Cookie、内部 unlock slug 或服务端错误原文展示给用户。

### 3.7 解锁确认

免费资源可以直接解锁，但仍要先用 callback toast 提示“免费资源，正在解锁”。收费或费用未知必须进入确认页。

收费确认正文：

```text
⚠️ 确认解锁

流浪地球 2
115 · 4K HDR · 18.6 GB

将消耗 5 积分。
解锁提交后无法撤销，请勿重复点击。
```

费用未知正文必须明确：

```text
⚠️ 无法确认解锁费用

服务端没有返回准确费用，实际操作仍可能扣除积分。
只有确认愿意承担未知费用时才继续。
```

按钮：

```text
[ 确认解锁 ]  // success
[ 取消 ]       // neutral
```

进入确认页不调用解锁接口。只有 `unlock_confirm` 才允许执行解锁。

点击确认后立即：

1. 回答 callback：`正在提交解锁，请勿重复操作`。
2. 把键盘替换成 `[ ⏳ 正在解锁… ]`。
3. 执行已有的持久化 claim / unlock 状态机。
4. 根据明确失败、成功或结果未知更新主卡片。

任何 UI 修改都不能削弱当前 SQLite 防重复解锁逻辑。

### 3.8 解锁结果

成功正文：

```text
✅ 解锁成功

流浪地球 2
115 · 4K HDR · 18.6 GB

提取码：a7k9
建议立即转存，避免分享失效。
```

按钮：

```text
[ 📥 一键转存到 115 ]  // success
[ 🔗 打开资源 ] [ 📋 复制提取码 ]
[ ‹ 返回资源 ] [ 🔎 新搜索 ]
```

明确失败：显示可理解的业务原因，并提供安全的下一步按钮。

结果未知：

```text
⚠️ 解锁结果暂时无法确认

请求可能已经提交。为避免重复扣费，Bot 不会自动重试。
请稍后重新查看资源状态，或联系管理员核验。
```

结果未知时绝对不能展示“重试解锁”按钮。

### 3.9 转存状态与结果

点击转存后立即回答 callback，并将按钮替换成 `[ ⏳ 正在转存… ]`。

成功：

```text
✅ 已转存到 115

流浪地球 2
目标目录：默认目录
结果：已接收
```

按钮：

```text
[ 📂 115 设置 ]
[ ‹ 返回资源 ] [ 🔎 新搜索 ]
```

失败按钮按错误类型生成：

| 错误类型 | 正文动作 | 按钮 |
|---|---|---|
| Cookie 失效 | 提示重新配置 | `重新配置 115` |
| 已存在 | 视为幂等成功 | `返回资源` |
| 分享失效/提取码错 | 不建议盲重试 | `换一个资源` |
| 网络超时且接口保证幂等 | 可重试 | `重试转存` |
| 状态不确定 | 不自动重试 | `稍后查看` |

### 3.10 关闭与过期

“关闭”不删除消息，只清空键盘并把正文尾部更新为：

```text
已关闭 · 发送新关键词可重新搜索
```

callback token 过期或 worker 重启后：

- 立即回答 callback alert：`此页面已过期，请重新搜索`。
- 不执行任何业务操作。
- 私聊场景可以补充发送一条短提示；不要在群聊里修改可能由别人触发的卡片。

## 4. 状态机

建议页面路由：

```text
home
  └─ search_loading
       ├─ search_results
       │    ├─ movie
       │    │    └─ resource_list
       │    │         └─ resource_detail
       │    │              ├─ unlock_confirm
       │    │              │    ├─ unlocking
       │    │              │    │    ├─ unlock_success
       │    │              │    │    ├─ unlock_failed
       │    │              │    │    └─ unlock_unknown
       │    │              │    └─ resource_detail
       │    │              └─ transferring
       │    │                   ├─ transfer_success
       │    │                   └─ transfer_failed
       │    └─ closed
       └─ search_empty / search_failed
```

建议 UI session：

```go
type UIState struct {
    UserID          int64
    ChatID          int64
    ActiveMessageID int
    HasMedia        bool
    MediaURL        string
    Route           Route
    Query           string
    SearchPage      int
    SelectedTMDB    *TMDBItem
    ResourcePage    int
    SelectedID      string
    Filters         ResourceFilters
    Busy            bool
    UpdatedAt       time.Time
}
```

UI session 建议按 `(chatID, userID)` 存储，而不是只按 `userID`。解锁防重状态仍沿用现有持久化逻辑，不要把业务幂等依赖 UI session。

返回导航不需要通用任意栈；使用显式 action 更容易测试：

- `back_search`
- `back_movie`
- `back_resources`
- `back_detail`

## 5. Callback 协议

继续使用当前随机 token 映射，不把完整标题、URL 或 JSON 放进 `callback_data`。

推荐 action：

```text
noop
search_pick
search_prev
search_next
movie_resources
resource_prev
resource_next
resource_pick
filter_toggle
back_search
back_movie
back_resources
unlock_request
unlock_confirm
unlock_cancel
transfer_start
transfer_retry
settings_115
new_search
close
```

每个 callback 必须校验：

1. token 存在且未过期。
2. token owner 等于点击用户。
3. callback 的 `chatID`、`messageID` 等于 UI session 当前主卡片。
4. 当前 route 允许该 action。
5. `Busy` 为 false；提交型 action 需要原子设置 Busy。
6. 授权状态仍有效。

随机 token 当前为 16 个 base64url 字符，满足 Telegram callback data 1–64 bytes 限制。测试必须继续断言所有 callback data 不超过 64 bytes。

一次性提交 action（`unlock_confirm`、`transfer_start`）解析成功后应删除 token；普通分页 token 可以在 TTL 内复用，但渲染新页面时应尽量淘汰旧页面 token，避免 callback map 无界增长。

## 6. Telegram 输出抽象

不要继续给 `Outgoing` 增加多个互斥 bool。建议引入明确的 View 和消息引用：

```go
type MessageRef struct {
    ChatID    int64
    MessageID int
    HasMedia  bool
    MediaID   string // Telegram file_id，可选
    MediaURL  string
}

type View struct {
    Body           string
    Media          *Media
    Buttons        [][]Button
    ProtectContent bool
}

type Media struct {
    Type string // 首期只允许 "photo"
    URL  string
}

type Button struct {
    Text         string
    Style        string // "", "primary", "success", "danger"
    CallbackData string
    URL          string
    CopyText     string
}
```

`Button` 的 `CallbackData`、`URL`、`CopyText` 必须恰好设置一个。增加构造函数减少非法组合：

```go
CallbackButton(text, token, style string) Button
URLButton(text, url string) Button
CopyButton(text, value string) Button
```

Messenger 接口建议：

```go
type Messenger interface {
    Send(ctx context.Context, chatID int64, view View) (MessageRef, error)
    Render(ctx context.Context, current MessageRef, view View) (MessageRef, error)
    AnswerCallback(ctx context.Context, id string, answer CallbackAnswer) error
    DeleteMessage(ctx context.Context, chatID int64, messageID int) error
}

type CallbackAnswer struct {
    Text      string
    ShowAlert bool
}
```

由 `BotMessenger.Render` 统一决定调用 EditText、EditCaption 还是 EditMedia，Handler 不应感知 Telegram 方法差异。

必须将 Telegram 返回的 `Message.ID` 传回 Handler。当前 `Send` 只返回 error，无法可靠维护主卡片，需要修改。

Renderer 的错误策略：

- `message is not modified`：视为成功。
- 目标消息不存在或不可编辑：发送一张新主卡片并返回新的 `MessageRef`。
- 海报 URL 拉取失败：按“媒体只升级”规则降级，不影响业务结果。
- 认证、限流、上下文取消等错误：向上返回，不要伪装成编辑失败并无限新增消息。

## 7. Handler 输入上下文

将散落的 callback 参数收拢：

```go
type CallbackContext struct {
    UserID       int64
    ChatID       int64
    MessageID    int
    HasMedia     bool
    CallbackID   string
    CallbackData string
}
```

`UpdateHandler` 从 `CallbackQuery.Message.Message` 读取：

- chat ID
- message ID
- 是否存在 Photo / Video / Animation

并完整传给 `HandleCallback`。

网络调用前必须先 `AnswerCallbackQuery`。回答 callback 的失败只记录日志，不应阻断主业务。

## 8. 数据模型改造

`TMDBItem` 首期增加：

```go
PosterPath string
PosterURL  string
```

`TMDBAdapter.Search` 把现有 `tmdb.SearchResult.PosterPath` 映射进 Telegram/domain 模型。建议只保存 `PosterPath`，由统一 helper 生成 URL，便于以后切换尺寸或 CDN。

`Resource` 当前没有字幕字段；UI formatter 只能在字段存在时显示。不要从 `Description` 猜字幕。如果 HDHive 原始数据提供字幕，应显式增加：

```go
Subtitle string
```

资源排序、大小排序不得依赖字符串直接比较。首期保持接口顺序，或只做明确的布尔优先级排序。

## 9. 文件级实施建议

### `internal/telegram/handler.go`

- 引入 `CallbackContext`。
- 把各页面逻辑拆成显式 render 方法。
- 搜索创建主卡片；callback 只 Render 当前卡片。
- 加入 active message、route、busy 和过期校验。
- 保留授权、解锁幂等和 Cookie 私聊限制。

### `internal/telegram/bot.go`

- `Send` 返回 `MessageRef`。
- 实现 `SendPhoto`、`EditMessageText`、`EditMessageCaption`、`EditMessageMedia`。
- 把 Button 映射到 callback、URL、CopyText 和 Style。
- 规范化 Telegram “message is not modified”错误。
- 从 update 中提取 callback message context。

### `internal/telegram/formatter.go`

建议逐步拆分为：

```text
view_home.go
view_search.go
view_movie.go
view_resource.go
view_unlock.go
view_transfer.go
format_helpers.go
```

Formatter 返回 `View` 或页面 DTO，不再让 Handler 手动拼正文和键盘。

### `internal/session/session.go`

- 新增 UI session，key 使用 `(chatID, userID)`。
- callback 记录可增加 `ChatID`、`MessageID`、允许 route 和 `SingleUse`。
- 增加清理旧页面 callback 的能力。
- 业务解锁状态不可因 UI session 清理而丢失；持久化 claim 仍是最终防线。

### `internal/app/adapters.go`

- 映射 `PosterPath`。
- 如 HDHive 数据确有字幕字段，显式映射 `Subtitle`。
- 不在 adapter 内拼 Telegram HTML。

### `internal/app/app.go`

- 注册 `/start`、`/help`、`/settings`、`/myid`、`/cancel`。
- 115 配置入口收敛到 `/settings`，旧命令继续兼容。
- 首期继续只订阅 `message`、`callback_query`；实现 Inline Mode 时再加入 `inline_query`、`chosen_inline_result`。

### 文档

- 完成后同步更新 `DEVELOPMENT.md` 和 `TODO.md`。
- 记录 BotFather 命令、简介、描述和菜单按钮配置。

## 10. 安全与隐私

- `/set115` 只能在 Bot 私聊使用。
- 用户发送 Cookie 后继续尝试删除原消息；删除失败时明确提醒用户手动删除。
- Cookie、Bot Token、TMDB Token、HDHive 密钥、分享访问凭证不得进入日志。
- `/my115` 只展示“已配置 / 未配置 / 已停用”和目标目录，不展示掩码或 Cookie 片段。
- 解锁成功页面可选 `protect_content=true`，但必须做成配置项；该能力不能替代真正的凭证保护。
- 分享 URL 在成为 URL Button 前必须校验 scheme 和长度。
- 所有外部文案必须 HTML escape。
- 错误映射使用稳定业务错误码，不直接显示第三方原始 error。

## 11. 文案和排版规范

- 使用简体中文，命令保持英文小写。
- 一个页面最多一个主标题 emoji。
- 不使用长横线、ASCII 表格或依赖等宽字体的列对齐。
- 字段采用短标签；缺失字段整行省略，不显示“未知”堆叠。
- 标题最多 2 行；简介最多约 180 个中文字符。
- 按钮每行：主操作 1 个；短导航最多 3 个；长标题永远 1 个。
- 操作成功用 `✅`，警告/未知用 `⚠️`，明确失败用 `❌`，处理中用 `⏳`。
- 不使用仅颜色区分的状态，例如必须写“已解锁”，不能只有绿色按钮。
- 所有 formatter 都应使用 rune-aware truncate helper，并在测试中覆盖中文和 emoji。

## 12. 测试要求

### Formatter 单元测试

- HTML 特殊字符全部转义。
- 中文、emoji 截断不产生破损 UTF-8。
- caption 不超过 1024 字符，目标 formatter 上限不超过 900 rune。
- 空年份、空评分、空原名、空海报不产生多余标点。
- 资源编号与按钮编号一致。
- Button 恰好有一个 action 类型。
- callback data 不超过 64 bytes。

### Renderer 单元测试

覆盖全部 6 种渲染组合：

- send text
- send photo
- edit text
- text → photo
- edit caption
- replace photo

另外覆盖：

- 海报失败后的 fallback。
- `message is not modified` 视为成功。
- 不可编辑时发送新卡并返回新 MessageID。
- callback、URL、CopyText、Style 映射正确。

### Handler 单元测试

- 新关键词只创建一张主卡片。
- 选择影片、分页、返回、资源详情不会新增消息。
- 新搜索使旧主卡 callback 失效。
- 非 owner 点击不改变消息。
- 过期 callback 不执行服务调用。
- Busy 状态阻止双击提交。
- 收费和费用未知必须确认。
- 免费资源只解锁一次。
- `unknown` 不出现重试解锁。
- 转存失败按钮与错误类型匹配。
- 群聊不能配置 Cookie、解锁或转存。

### 集成与人工验收

至少在 Telegram iOS/Android 任一移动端和 Desktop 验证：

- 浅色、深色主题。
- 海报加载成功和失败。
- 中文长标题、emoji、英文原名。
- 1 页和多页搜索结果。
- 1 页和多页资源。
- 付费确认、取消、成功、明确失败、未知状态。
- CopyTextButton、URL Button。
- 旧 Reply Keyboard 被移除。
- worker 重启后旧按钮只提示过期，不触发业务操作。

## 13. 分阶段实施

### Phase A：消息渲染基础设施

- 新 View / Button / MessageRef。
- Messenger 返回消息引用。
- Render 支持文本、海报和原地编辑。
- callback 上下文带 message ID。
- 完成 Renderer 单元测试。

验收：可用测试 Handler 完成 text → photo → caption 的单消息更新。

### Phase B：搜索与影片卡片

- 映射 TMDB `poster_path`。
- 搜索加载、结果分页、影片卡片、返回和关闭。
- 新搜索废弃旧卡片。

验收：搜索到进入影片卡全程只创建一条 Bot 主消息。

### Phase C：资源、解锁、转存

- 资源分页和详情原地更新。
- 收费确认、Busy UI、解锁结果。
- URL、复制提取码、115 转存结果。
- 保持现有持久化幂等约束。

验收：完整主链路没有刷屏，重复点击不会重复调用付费接口。

### Phase D：设置与收尾

- 移除常驻 Reply Keyboard。
- `/settings` 和 115 状态页。
- 统一错误码和重试按钮。
- 更新文档并完成真机测试。

验收：命令菜单、设置、错误恢复和新旧用户迁移均正常。

### Phase E（后续可选）

- Inline Mode：跨聊天搜索和分享公开影片卡。
- Deep Link：从公开卡片跳到私聊资源页。
- Mini App：收藏、复杂筛选、115 目录浏览和管理员面板。

## 14. 完成定义

只有同时满足以下条件，才算完成首期：

- 除新搜索、必要的隐私提示和不可恢复编辑 fallback 外，callback 不产生新消息。
- 搜索、影片、资源、确认、解锁、转存可在同一主卡片完成。
- 海报失败不影响主流程。
- 收费/未知费用有确认，重复点击不重复扣费。
- 解锁 `unknown` 不提供自动重试。
- CopyTextButton 和 URL Button 正常工作。
- 旧 Reply Keyboard 已移除。
- formatter、renderer、handler 测试覆盖关键路径。
- `go test ./...`、`go vet ./...`、`go build ./...`、`git diff --check` 全部通过。
- `DEVELOPMENT.md`、`TODO.md` 与实际实现一致。

## 15. 给实施 AI 的约束

1. 先做 Phase A，不要直接在现有 `Send` 上堆条件分支完成全部页面。
2. 不改变 HDHive、115 和 SQLite 的业务语义，尤其不能移除付费解锁的持久化 claim。
3. 不为了 UI 新增不必要的网络请求；首期只使用 TMDB 搜索已经返回的数据。
4. 不把完整业务对象塞进 callback data；继续使用短 token 和 owner 校验。
5. 每完成一个 phase 就运行相关测试，并更新 `DEVELOPMENT.md` / `TODO.md`。
6. 遇到 Telegram 编辑失败时只能做一次受控 fallback，禁止形成“编辑失败 → 无限发送新消息”。
7. 保留旧命令兼容性，UI 入口可以收敛但不要突然移除 `/set115`、`/my115` 等现有命令。

## 16. 官方能力参考

- [Telegram Bot API](https://core.telegram.org/bots/api)：消息、媒体、编辑方法、按钮、CopyTextButton 和长度限制。
- [Telegram Bot Features](https://core.telegram.org/bots/features)：命令菜单、Inline Keyboard、Inline Mode、Deep Link 和原生交互建议。
- [Telegram Bot Buttons](https://core.telegram.org/api/bots/buttons)：`primary`、`success`、`danger` 按钮样式和主题适配。
- [Telegram Mini Apps](https://core.telegram.org/bots/webapps)：后续 Mini App 的启动方式与适用边界。
- [Bot API Changelog](https://core.telegram.org/bots/api-changelog)：CopyTextButton、文本转媒体编辑等能力的版本记录。
