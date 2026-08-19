package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/atpx4869/hdhive_bot_go/internal/hdhive"
	"github.com/atpx4869/hdhive_bot_go/internal/session"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

var ErrNotFound = errors.New("not found")

type UserService interface {
	GetUser(context.Context, int64) (store.User, error)
	SetUserAuthorization(context.Context, int64, bool) error
	SetUserNote(context.Context, int64, string) error
	ListUsers(context.Context, int, int) ([]store.User, error)
}

type AccountService interface {
	SetP115Config(context.Context, int64, store.P115Config) error
	GetP115Config(context.Context, int64) (store.P115Config, error)
	DisableP115Config(context.Context, int64) error
}

type UnlockAdminService interface {
	ResetUnlockRecord(context.Context, int64, string) error
}

type LogService interface {
	AddActivityLog(context.Context, int64, string, string) (store.ActivityLog, error)
	QueryActivityLogs(context.Context, store.ActivityQuery) ([]store.ActivityLog, error)
}

type TMDBItem struct {
	ID                                                     int64
	MediaType, Title, OriginalTitle, ReleaseDate, Overview string
	VoteAverage                                            float64
}

type TMDBService interface {
	Search(context.Context, string, int) ([]TMDBItem, int, error)
}

type Resource struct {
	ID, Title, Quality, Size, Description string
	Subtitle, Source                      string
	ShareURL, ShareCode, ReceiveCode      string
	UnlockSlug                            string
	FeeKnown                              bool
	Fee                                   int
	Unlocked                              bool
}

type ResourcePage struct {
	Items            []Resource
	Page, TotalPages int
}

type HDHiveService interface {
	Search(context.Context, TMDBItem, int) (ResourcePage, error)
	Detail(context.Context, int64, string) (Resource, error)
	Unlock(context.Context, int64, string) (Resource, error)
}

type TransferService interface {
	Transfer115(context.Context, int64, store.P115Config, Resource) (string, error)
}

type Services struct {
	Users    UserService
	Accounts AccountService
	Logs     LogService
	TMDB     TMDBService
	HDHive   HDHiveService
	Transfer TransferService
}

type Button struct{ Text, CallbackData string }
type Outgoing struct {
	Text    string
	Buttons [][]Button
}
type Messenger interface {
	Send(context.Context, int64, Outgoing) error
	AnswerCallback(context.Context, string, string) error
	DeleteMessage(context.Context, int64, int) error
}

type Handler struct {
	services  Services
	sessions  *session.Manager
	messenger Messenger
	admins    map[int64]struct{}
}

func NewHandler(services Services, sessions *session.Manager, messenger Messenger, adminIDs []int64) (*Handler, error) {
	if sessions == nil || messenger == nil {
		return nil, errors.New("sessions and messenger are required")
	}
	admins := make(map[int64]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		admins[id] = struct{}{}
	}
	return &Handler{services: services, sessions: sessions, messenger: messenger, admins: admins}, nil
}

func (h *Handler) isAdmin(id int64) bool { _, ok := h.admins[id]; return ok }
func (h *Handler) authorized(ctx context.Context, id int64) bool {
	if h.isAdmin(id) {
		return true
	}
	if h.services.Users == nil {
		return false
	}
	u, err := h.services.Users.GetUser(ctx, id)
	return err == nil && u.Authorized
}

func (h *Handler) HandleText(ctx context.Context, userID, chatID int64, text string) error {
	return h.HandleMessage(ctx, userID, chatID, 0, text)
}

func (h *Handler) HandleMessage(ctx context.Context, userID, chatID int64, messageID int, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	cmd, arg := splitCommand(text)
	if s, ok := h.sessions.Get(userID); ok && s.Kind == "set115_cookie" && chatID != userID {
		deleted := messageID > 0 && h.messenger.DeleteMessage(ctx, chatID, messageID) == nil
		warning := ""
		if !deleted {
			warning = " 原消息无法自动删除，请立即手动删除。"
		}
		return h.send(ctx, chatID, "检测到正在配置 115，但 Cookie 只能在 Bot 私聊发送。"+warning)
	}
	if cmd == "/set115" && arg != "" {
		if messageID > 0 {
			_ = h.messenger.DeleteMessage(ctx, chatID, messageID)
		}
		return h.send(ctx, chatID, "请不要把 Cookie 放在命令后。请在 Bot 私聊重新发送 /set115，再按提示单独发送 Cookie；若原消息仍可见请立即手动删除。")
	}
	switch cmd {
	case "/start", "/help":
		// 获取 115 配置状态
		p115Enabled := false
		p115Target := ""
		if h.services.Accounts != nil {
			if cfg, err := h.services.Accounts.GetP115Config(ctx, userID); err == nil {
				p115Enabled = cfg.Enabled
				p115Target = cfg.TargetCID
			}
		}
		return h.send(ctx, chatID, StatusPanel(userID, h.isAdmin(userID), h.authorized(ctx, userID), p115Enabled, p115Target))
	case "/myid":
		return h.send(ctx, chatID, fmt.Sprintf("你的 Telegram User ID：%d", userID))
	case "/authorize", "/revoke", "/users", "/note", "/logs", "/unlockreset":
		if !h.isAdmin(userID) {
			return h.send(ctx, chatID, "无权限：仅管理员可使用此命令。")
		}
		return h.handleAdmin(ctx, userID, chatID, cmd, arg)
	}
	if !h.authorized(ctx, userID) {
		return h.send(ctx, chatID, "你尚未获得授权，请将 /myid 的结果发送给管理员。")
	}
	switch cmd {
	case "/set115":
		if chatID != userID {
			return h.send(ctx, chatID, "为保护 Cookie，/set115 只能在 Bot 私聊中使用。")
		}
		_ = h.sessions.Set(userID, "set115_cookie", nil)
		return h.send(ctx, chatID, "请发送完整的 115 Cookie（应包含 UID、CID、SEID）。Bot 会尝试立即删除该消息。")
	case "/cancel":
		h.sessions.ClearInteraction(userID)
		return h.send(ctx, chatID, "已取消当前操作。")
	case "/unset115":
		if h.isAdmin(userID) {
			return h.send(ctx, chatID, "管理员不能通过 Bot 删除自己的 115 配置。")
		}
		if h.services.Accounts == nil {
			return h.send(ctx, chatID, "115 服务未配置。")
		}
		cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
		if err != nil || !cfg.Enabled {
			return h.send(ctx, chatID, "你当前没有启用的 115 配置。")
		}
		yes, err := h.sessions.BindCallback(userID, "unset115_confirm", cfg.Cookie)
		if err != nil {
			return err
		}
		no, err := h.sessions.BindCallback(userID, "unset115_cancel", "")
		if err != nil {
			return err
		}
		return h.messenger.Send(ctx, chatID, Outgoing{Text: "确认停用你的 115 配置吗？服务端会保留加密记录。", Buttons: [][]Button{{{Text: "确认停用", CallbackData: yes}, {Text: "取消", CallbackData: no}}}})
	case "/my115":
		if h.services.Accounts == nil {
			return h.send(ctx, chatID, "115 服务未配置。")
		}
		cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
		if err != nil {
			return h.send(ctx, chatID, "尚未配置 115。发送 /set115 开始配置。")
		}
		status := "已启用"
		if !cfg.Enabled {
			status = "已停用"
		}
		target := "根目录"
		if strings.TrimSpace(cfg.TargetCID) != "" && cfg.TargetCID != "0" {
			target = "已配置目录"
		}
		return h.send(ctx, chatID, "115："+status+"\n目标："+target)
	}
	if s, ok := h.sessions.Get(userID); ok {
		if chatID != userID {
			return h.send(ctx, chatID, "请回到 Bot 私聊完成 115 配置。")
		}
		switch s.Kind {
		case "set115_cookie":
			return h.receive115Cookie(ctx, userID, chatID, messageID, text)
		case "set115_cid":
			return h.receive115CID(ctx, userID, chatID, text, s.Data["cookie"])
		}
	}
	return h.search(ctx, userID, chatID, text, 1)
}

func (h *Handler) handleAdmin(ctx context.Context, actor, chatID int64, cmd, arg string) error {
	if h.services.Users == nil {
		return h.send(ctx, chatID, "用户服务未配置。")
	}
	switch cmd {
	case "/authorize", "/revoke":
		id, rest, err := parseIDArg(arg)
		if err != nil {
			return h.send(ctx, chatID, "用法："+cmd+" <user_id>")
		}
		authorized := cmd == "/authorize"
		if err := h.services.Users.SetUserAuthorization(ctx, id, authorized); err != nil {
			return err
		}
		h.log(ctx, actor, strings.TrimPrefix(cmd, "/"), fmt.Sprintf("user=%d %s", id, rest))
		return h.send(ctx, chatID, fmt.Sprintf("用户 %d 已%s授权。", id, map[bool]string{true: "获得", false: "撤销"}[authorized]))
	case "/note":
		id, note, err := parseIDArg(arg)
		if err != nil || note == "" {
			return h.send(ctx, chatID, "用法：/note <user_id> <备注>")
		}
		if err := h.services.Users.SetUserNote(ctx, id, note); err != nil {
			return err
		}
		return h.send(ctx, chatID, "备注已更新。")
	case "/users":
		page := 1
		if arg != "" {
			fmt.Sscanf(arg, "%d", &page)
		}
		if page < 1 {
			page = 1
		}
		const pageSize = 20
		offset := (page - 1) * pageSize
		users, err := h.services.Users.ListUsers(ctx, pageSize+1, offset)
		if err != nil {
			return err
		}
		hasMore := len(users) > pageSize
		if hasMore {
			users = users[:pageSize]
		}
		out := Outgoing{Text: FormatUsersPage(users, page, hasMore)}
		var buttons []Button
		if page > 1 {
			prevToken, _ := h.sessions.BindCallback(actor, "admin_users", fmt.Sprintf("%d", page-1))
			buttons = append(buttons, Button{Text: "⬅️ 上一页", CallbackData: prevToken})
		}
		if hasMore {
			nextToken, _ := h.sessions.BindCallback(actor, "admin_users", fmt.Sprintf("%d", page+1))
			buttons = append(buttons, Button{Text: "下一页 ➡️", CallbackData: nextToken})
		}
		if len(buttons) > 0 {
			out.Buttons = [][]Button{buttons}
		}
		return h.messenger.Send(ctx, chatID, out)
	case "/logs":
		if h.services.Logs == nil {
			return h.send(ctx, chatID, "日志服务未配置。")
		}
		page := 1
		if arg != "" {
			fmt.Sscanf(arg, "%d", &page)
		}
		if page < 1 {
			page = 1
		}
		const logPageSize = 20
		logs, err := h.services.Logs.QueryActivityLogs(ctx, store.ActivityQuery{Limit: logPageSize + 1, Offset: (page - 1) * logPageSize})
		if err != nil {
			return err
		}
		hasMore := len(logs) > logPageSize
		if hasMore {
			logs = logs[:logPageSize]
		}
		out := Outgoing{Text: FormatLogsPage(logs, page, hasMore)}
		var buttons []Button
		if page > 1 {
			prevToken, _ := h.sessions.BindCallback(actor, "admin_logs", fmt.Sprintf("%d", page-1))
			buttons = append(buttons, Button{Text: "⬅️ 上一页", CallbackData: prevToken})
		}
		if hasMore {
			nextToken, _ := h.sessions.BindCallback(actor, "admin_logs", fmt.Sprintf("%d", page+1))
			buttons = append(buttons, Button{Text: "下一页 ➡️", CallbackData: nextToken})
		}
		if len(buttons) > 0 {
			out.Buttons = [][]Button{buttons}
		}
		return h.messenger.Send(ctx, chatID, out)
	case "/unlockreset":
		var userID int64
		var resourceID string
		if _, err := fmt.Sscanf(arg, "%d %s", &userID, &resourceID); err != nil || userID <= 0 || strings.TrimSpace(resourceID) == "" {
			return h.send(ctx, chatID, "用法：/unlockreset <user_id> <resource_id>")
		}
		admin, ok := h.services.HDHive.(UnlockAdminService)
		if !ok {
			return h.send(ctx, chatID, "解锁恢复服务未配置。")
		}
		if err := admin.ResetUnlockRecord(ctx, userID, resourceID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return h.send(ctx, chatID, "只有 unknown 状态可以解除；记录可能仍在 in_flight、已经成功或不存在。")
			}
			return err
		}
		h.sessions.ResetUnlock(userID, resourceID)
		h.log(ctx, actor, "unlockreset", fmt.Sprintf("user=%d resource=%s", userID, resourceID))
		return h.send(ctx, chatID, "解锁状态已解除。请先人工核验是否已扣费，再让用户重新操作。")
	}
	return nil
}

func normalize115Cookie(cookie string) string {
	parts := strings.Split(strings.TrimSpace(cookie), ";")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			clean = append(clean, value)
		}
	}
	return strings.Join(clean, ";")
}

func valid115Cookie(cookie string) bool {
	keys := map[string]bool{}
	for _, part := range strings.Split(cookie, ";") {
		pair := strings.SplitN(part, "=", 2)
		if len(pair) == 2 {
			keys[strings.ToUpper(strings.TrimSpace(pair[0]))] = true
		}
	}
	return keys["UID"] && keys["CID"] && keys["SEID"]
}

func (h *Handler) receive115Cookie(ctx context.Context, userID, chatID int64, messageID int, raw string) error {
	cookie := normalize115Cookie(raw)
	deleted := messageID > 0 && h.messenger.DeleteMessage(ctx, chatID, messageID) == nil
	if !valid115Cookie(cookie) {
		warning := ""
		if !deleted {
			warning = "\nBot 无法自动删除原消息，请立即手动删除。"
		}
		return h.send(ctx, chatID, "Cookie 格式不正确，应包含 UID、CID、SEID，请重新发送。"+warning)
	}
	if err := h.sessions.Set(userID, "set115_cid", map[string]string{"cookie": cookie}); err != nil {
		return err
	}
	warning := ""
	if !deleted {
		warning = "\nBot 无法自动删除 Cookie 消息，请立即手动删除。"
	}
	return h.send(ctx, chatID, "Cookie 格式已确认。请发送目标目录 cid；发送 0 表示根目录。"+warning)
}

func (h *Handler) receive115CID(ctx context.Context, userID, chatID int64, targetCID, cookie string) error {
	targetCID = strings.TrimSpace(targetCID)
	if targetCID == "" || strings.Trim(targetCID, "0123456789") != "" {
		return h.send(ctx, chatID, "目标目录 cid 必须是数字；根目录请输入 0。")
	}
	if h.services.Accounts == nil {
		return h.send(ctx, chatID, "115 服务未配置。")
	}
	if err := h.services.Accounts.SetP115Config(ctx, userID, store.P115Config{Cookie: cookie, TargetCID: targetCID, Enabled: true}); err != nil {
		return err
	}
	h.sessions.ClearInteraction(userID)
	h.log(ctx, userID, "set115", map[bool]string{true: "root", false: "configured"}[targetCID == "0"])
	return h.send(ctx, chatID, "115 配置已加密保存。")
}

func (h *Handler) search(ctx context.Context, userID, chatID int64, query string, page int) error {
	if h.services.TMDB == nil {
		return h.send(ctx, chatID, "搜索服务未配置。")
	}
	items, total, err := h.services.TMDB.Search(ctx, query, page)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return h.send(ctx, chatID, "未找到 TMDB 结果。")
	}
	out := Outgoing{Text: FormatTMDB(items, page, total)}
	for _, item := range items {
		value := encodeTMDB(item)
		token, err := h.sessions.BindCallback(userID, "tmdb", value)
		if err != nil {
			return err
		}
		btnText := displayTitle(item)
		if len(btnText) > 60 {
			btnText = btnText[:57] + "..."
		}
		out.Buttons = append(out.Buttons, []Button{{Text: btnText, CallbackData: token}})
	}
	return h.messenger.Send(ctx, chatID, out)
}

func (h *Handler) HandleCallback(ctx context.Context, userID, chatID int64, callbackID, token string) error {
	cb, err := h.sessions.ResolveCallback(token, userID)
	if err != nil {
		_ = h.messenger.AnswerCallback(ctx, callbackID, "按钮已过期或不属于你")
		return nil
	}
	_ = h.messenger.AnswerCallback(ctx, callbackID, "处理中…")
	if !h.authorized(ctx, userID) {
		return h.send(ctx, chatID, "你尚未获得授权。")
	}
	switch cb.Action {
	case "tmdb":
		return h.showResources(ctx, userID, chatID, decodeTMDB(cb.Value), 1)
	case "resources":
		item, page := decodeResourcePage(cb.Value)
		return h.showResources(ctx, userID, chatID, item, page)
	case "detail":
		return h.showDetail(ctx, userID, chatID, cb.Value)
	case "unlock":
		return h.confirmUnlock(ctx, userID, chatID, cb.Value)
	case "unlock_confirm":
		return h.unlock(ctx, userID, chatID, cb.Value)
	case "unlock_reject":
		h.sessions.DeleteCallback(token)
		if err := h.sessions.TransitionUnlock(userID, cb.Value, session.UnlockPending, session.UnlockRejected); err != nil {
			return h.send(ctx, chatID, "该解锁请求已处理，不能取消。")
		}
		return h.send(ctx, chatID, "已取消解锁。")
	case "transfer":
		return h.transfer(ctx, userID, chatID, cb.Value)
	case "unset115_cancel":
		h.sessions.DeleteCallback(token)
		return h.send(ctx, chatID, "已取消停用 115 配置。")
	case "unset115_confirm":
		h.sessions.DeleteCallback(token)
		if h.isAdmin(userID) {
			return h.send(ctx, chatID, "管理员不能通过 Bot 删除自己的 115 配置。")
		}
		cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
		if err != nil || !cfg.Enabled || cfg.Cookie != cb.Value {
			return h.send(ctx, chatID, "确认已过期或配置已变化，请重新发送 /unset115。")
		}
		if err := h.services.Accounts.DisableP115Config(ctx, userID); err != nil {
			return err
		}
		h.log(ctx, userID, "unset115", "disabled")
		return h.send(ctx, chatID, "115 配置已停用。")
	case "admin_users":
		if !h.isAdmin(userID) {
			return h.send(ctx, chatID, "无权限。")
		}
		page := 1
		fmt.Sscanf(cb.Value, "%d", &page)
		return h.handleAdmin(ctx, userID, chatID, "/users", fmt.Sprintf("%d", page))
	case "admin_logs":
		if !h.isAdmin(userID) {
			return h.send(ctx, chatID, "无权限。")
		}
		page := 1
		fmt.Sscanf(cb.Value, "%d", &page)
		return h.handleAdmin(ctx, userID, chatID, "/logs", fmt.Sprintf("%d", page))
	}
	return nil
}

func (h *Handler) showResources(ctx context.Context, userID, chatID int64, item TMDBItem, page int) error {
	if h.services.HDHive == nil {
		return h.send(ctx, chatID, "HDHive 服务未配置。")
	}
	result, err := h.services.HDHive.Search(ctx, item, page)
	if err != nil {
		return err
	}
	out := Outgoing{Text: FormatResources(result)}
	for _, r := range result.Items {
		token, _ := h.sessions.BindCallback(userID, "detail", r.ID)
		out.Buttons = append(out.Buttons, []Button{{Text: r.Title, CallbackData: token}})
	}
	var nav []Button
	if page > 1 {
		t, _ := h.sessions.BindCallback(userID, "resources", encodeResourcePage(item, page-1))
		nav = append(nav, Button{Text: "⬅️ 上一页", CallbackData: t})
	}
	if page < result.TotalPages {
		t, _ := h.sessions.BindCallback(userID, "resources", encodeResourcePage(item, page+1))
		nav = append(nav, Button{Text: "下一页 ➡️", CallbackData: t})
	}
	if len(nav) > 0 {
		out.Buttons = append(out.Buttons, nav)
	}
	return h.messenger.Send(ctx, chatID, out)
}

func (h *Handler) showDetail(ctx context.Context, userID, chatID int64, id string) error {
	r, err := h.services.HDHive.Detail(ctx, userID, id)
	if err != nil {
		return err
	}
	out := Outgoing{Text: FormatResource(r)}
	action := "unlock"
	label := "解锁资源"
	if r.Unlocked {
		action, label = "transfer", "转存到 115"
	}
	t, _ := h.sessions.BindCallback(userID, action, id)
	out.Buttons = [][]Button{{{Text: label, CallbackData: t}}}
	return h.messenger.Send(ctx, chatID, out)
}

func (h *Handler) confirmUnlock(ctx context.Context, userID, chatID int64, id string) error {
	r, err := h.services.HDHive.Detail(ctx, userID, id)
	if err != nil {
		return err
	}
	if r.Unlocked {
		return h.send(ctx, chatID, "该资源已经解锁，无需重复提交。")
	}
	if err := h.sessions.BeginUnlock(userID, id); err != nil {
		return h.send(ctx, chatID, "该资源已在处理，请勿重复提交。")
	}
	if r.FeeKnown && r.Fee == 0 {
		return h.unlock(ctx, userID, chatID, id)
	}
	fee := "费用未知"
	if r.FeeKnown {
		fee = fmt.Sprintf("费用 %d", r.Fee)
	}
	y, _ := h.sessions.BindCallback(userID, "unlock_confirm", id)
	n, _ := h.sessions.BindCallback(userID, "unlock_reject", id)
	return h.messenger.Send(ctx, chatID, Outgoing{Text: "此操作" + fee + "，是否确认解锁？", Buttons: [][]Button{{{Text: "确认解锁", CallbackData: y}, {Text: "取消", CallbackData: n}}}})
}

func (h *Handler) unlock(ctx context.Context, userID, chatID int64, id string) error {
	if current, err := h.services.HDHive.Detail(ctx, userID, id); err == nil && current.Unlocked {
		return h.send(ctx, chatID, "该资源已经解锁，无需重复提交。")
	}
	if err := h.sessions.TransitionUnlock(userID, id, session.UnlockPending, session.UnlockInFlight); err != nil {
		return h.send(ctx, chatID, "该资源已在处理或已成功解锁。")
	}
	r, err := h.services.HDHive.Unlock(ctx, userID, id)
	if err != nil {
		// 区分业务拒绝和网络不确定错误
		var apiErr *hdhive.APIError
		if errors.As(err, &apiErr) && apiErr.Business {
			_ = h.sessions.SetUnlockStatus(userID, id, session.UnlockRejected)
			retryBtn, _ := h.sessions.BindCallback(userID, "unlock", id)
			return h.messenger.Send(ctx, chatID, Outgoing{
				Text:    "解锁被拒绝：" + apiErr.Message + "\n可能是积分不足、资源失效或账号权限不足。",
				Buttons: [][]Button{{{Text: "重试解锁", CallbackData: retryBtn}}},
			})
		}
		_ = h.sessions.SetUnlockStatus(userID, id, session.UnlockUnknown)
		return h.send(ctx, chatID, "解锁结果不确定，请联系管理员。请勿重复付费解锁。")
	}
	_ = h.sessions.SetUnlockStatus(userID, id, session.UnlockSuccess)
	h.log(ctx, userID, "unlock", id)
	out := Outgoing{Text: "解锁成功。\n" + FormatResource(r)}
	var buttons []Button
	if h.services.Transfer != nil {
		t, _ := h.sessions.BindCallback(userID, "transfer", id)
		buttons = append(buttons, Button{Text: "转存到 115", CallbackData: t})
	}
	// 添加返回资源列表按钮
	backBtn, _ := h.sessions.BindCallback(userID, "resources", id)
	buttons = append(buttons, Button{Text: "返回资源列表", CallbackData: backBtn})
	if len(buttons) > 0 {
		out.Buttons = [][]Button{buttons}
	}
	return h.messenger.Send(ctx, chatID, out)
}

func (h *Handler) transfer(ctx context.Context, userID, chatID int64, id string) error {
	if h.services.Transfer == nil || h.services.Accounts == nil {
		return h.send(ctx, chatID, "115 转存服务未配置。")
	}
	cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
	if err != nil {
		return h.send(ctx, chatID, "请先使用 /set115 配置 115 Cookie。")
	}
	r, err := h.services.HDHive.Detail(ctx, userID, id)
	if err != nil {
		return err
	}
	result, err := h.services.Transfer.Transfer115(ctx, userID, cfg, r)
	if err != nil {
		return err
	}
	h.log(ctx, userID, "transfer115", id)
	return h.send(ctx, chatID, "115 转存成功："+result)
}

func (h *Handler) log(ctx context.Context, id int64, action, detail string) {
	if h.services.Logs != nil {
		_, _ = h.services.Logs.AddActivityLog(ctx, id, action, detail)
	}
}
func (h *Handler) send(ctx context.Context, chatID int64, text string) error {
	return h.messenger.Send(ctx, chatID, Outgoing{Text: text})
}
func splitCommand(text string) (string, string) {
	index := strings.IndexFunc(text, unicode.IsSpace)
	command := text
	arg := ""
	if index >= 0 {
		command = text[:index]
		arg = strings.TrimSpace(text[index:])
	}
	cmd := strings.ToLower(strings.SplitN(command, "@", 2)[0])
	return cmd, arg
}
func parseIDArg(s string) (int64, string, error) {
	var id int64
	var rest string
	_, err := fmt.Sscanf(s, "%d %s", &id, &rest)
	if err != nil {
		_, err = fmt.Sscanf(s, "%d", &id)
	}
	if id <= 0 {
		return 0, "", errors.New("invalid id")
	}
	fields := strings.Fields(s)
	if len(fields) > 1 {
		rest = strings.Join(fields[1:], " ")
	}
	return id, rest, err
}
func encodeTMDB(i TMDBItem) string {
	return fmt.Sprintf("%d|%s|%s|%s|%s", i.ID, i.MediaType, strings.ReplaceAll(i.Title, "|", " "), strings.ReplaceAll(i.OriginalTitle, "|", " "), i.ReleaseDate)
}
func decodeTMDB(v string) TMDBItem {
	var i TMDBItem
	p := strings.Split(v, "|")
	if len(p) >= 5 {
		fmt.Sscan(p[0], &i.ID)
		i.MediaType, i.Title, i.OriginalTitle, i.ReleaseDate = p[1], p[2], p[3], p[4]
	}
	return i
}
func encodeResourcePage(i TMDBItem, p int) string {
	return fmt.Sprintf("%d;%s;%s;%s;%d", i.ID, i.MediaType, strings.ReplaceAll(i.Title, ";", " "), i.ReleaseDate, p)
}
func decodeResourcePage(v string) (TMDBItem, int) {
	p := strings.Split(v, ";")
	var i TMDBItem
	page := 1
	if len(p) >= 5 {
		fmt.Sscan(p[0], &i.ID)
		i.MediaType, i.Title, i.ReleaseDate = p[1], p[2], p[3]
		fmt.Sscan(p[4], &page)
	}
	return i, page
}
func displayTitle(i TMDBItem) string {
	title := i.Title
	if title == "" {
		title = i.OriginalTitle
	}
	year := i.ReleaseDate
	if len(year) >= 4 {
		year = year[:4]
	}
	if year != "" {
		return fmt.Sprintf("%s (%s)", title, year)
	}
	return title
}
