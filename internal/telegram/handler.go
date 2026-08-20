package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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
	DeleteP115Config(context.Context, int64) error
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
	PanType, Source                       string
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
	DeleteMessage(ctx context.Context, chatID int64, messageID int) error
}

type Handler struct {
	services  Services
	sessions  *session.Manager
	messenger Messenger
	admins    map[int64]struct{}
	logger    *slog.Logger
}

func NewHandler(services Services, sessions *session.Manager, messenger Messenger, adminIDs []int64, logger *slog.Logger) (*Handler, error) {
	if sessions == nil || messenger == nil {
		return nil, errors.New("sessions and messenger are required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	admins := make(map[int64]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		admins[id] = struct{}{}
	}
	return &Handler{services: services, sessions: sessions, messenger: messenger, admins: admins, logger: logger}, nil
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

func (h *Handler) HandleText(ctx context.Context, userID, chatID int64, text string, messageID int) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	cmd, arg := splitCommand(text)
	h.logger.Info("received message",
		"user_id", userID,
		"chat_id", chatID,
		"cmd", cmd,
		"is_admin", h.isAdmin(userID),
	)
	switch cmd {
	case "/start":
		authorized := h.authorized(ctx, userID)
		has115 := false
		if h.services.Accounts != nil {
			_, err := h.services.Accounts.GetP115Config(ctx, userID)
			has115 = err == nil
		}
		return h.send(ctx, chatID, StatusPanel(userID, h.isAdmin(userID), authorized, has115))
	case "/cancel":
		h.sessions.ClearInteraction(userID)
		return h.send(ctx, chatID, "🚫 <b>已取消</b> 当前操作。")
	case "/myid":
		return h.send(ctx, chatID, fmt.Sprintf("🆔 你的 Telegram User ID：<b><code>%d</code></b>", userID))
	case "/authorize", "/revoke", "/users", "/note", "/logs":
		if !h.isAdmin(userID) {
			return h.send(ctx, chatID, "⛔ 无权限：仅管理员可使用此指令。")
		}
		return h.handleAdmin(ctx, userID, chatID, cmd, arg)
	}
	if !h.authorized(ctx, userID) {
		return h.send(ctx, chatID, "🔒 你尚未获得授权，请将 <code>/myid</code> 的结果发送给管理员。")
	}
	switch cmd {
	case "/set115":
		if chatID != userID {
			return h.send(ctx, chatID, "🔒 为保护 Cookie，<code>/set115</code> 只能在 Bot 私聊中使用。")
		}
		if arg == "" {
			_ = h.sessions.Set(userID, "set115", nil)
			return h.send(ctx, chatID, "🍪 请发送 115 Cookie。

发送后将<b>加密保存</b>；可用 <code>/unset115</code> 删除。")
		}
		return h.set115(ctx, userID, chatID, arg, messageID)
	case "/unset115":
		if h.services.Accounts == nil {
			return h.send(ctx, chatID, "⚠️ 115 服务未配置。")
		}
		err := h.services.Accounts.DeleteP115Config(ctx, userID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return h.send(ctx, chatID, "🗑 115 配置已删除。")
	case "/my115":
		if h.services.Accounts == nil {
			return h.send(ctx, chatID, "⚠️ 115 服务未配置。")
		}
		cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
		if err != nil {
			return h.send(ctx, chatID, "🍪 尚未配置 115 Cookie。

使用 <code>/set115</code> 进行配置。")
		}
		return h.send(ctx, chatID, "🍪 115 配置状态："+MaskSecret(cfg.Cookie))
	}
	if s, ok := h.sessions.Get(userID); ok && s.Kind == "set115" {
		if chatID != userID {
			return h.send(ctx, chatID, "🔒 请回到 Bot 私聊发送 115 Cookie。")
		}
		return h.set115(ctx, userID, chatID, text, messageID)
	}
	return h.search(ctx, userID, chatID, text, 1)
}

func (h *Handler) handleAdmin(ctx context.Context, actor, chatID int64, cmd, arg string) error {
	if h.services.Users == nil {
		return h.send(ctx, chatID, "⚠️ 用户服务未配置。")
	}
	h.logger.Info("admin command",
		"actor", actor,
		"cmd", cmd,
		"arg", arg,
	)
	switch cmd {
	case "/authorize", "/revoke":
		id, rest, err := parseIDArg(arg)
		if err != nil {
			return h.send(ctx, chatID, "📝 用法：<code>"+cmd+" &lt;user_id&gt;</code>")
		}
		authorized := cmd == "/authorize"
		if err := h.services.Users.SetUserAuthorization(ctx, id, authorized); err != nil {
			h.logger.Error("failed to set authorization",
				"actor", actor,
				"target_user", id,
				"authorized", authorized,
				"error", err,
			)
			return err
		}
		h.logger.Info("authorization changed",
			"actor", actor,
			"target_user", id,
			"authorized", authorized,
		)
		h.log(ctx, actor, strings.TrimPrefix(cmd, "/"), fmt.Sprintf("user=%d %s", id, rest))
		return h.send(ctx, chatID, fmt.Sprintf("✅ 用户 <b><code>%d</code></b> 已%s授权。", id, map[bool]string{true: "获得", false: "撤销"}[authorized]))
	case "/note":
		id, note, err := parseIDArg(arg)
		if err != nil || note == "" {
			return h.send(ctx, chatID, "📝 用法：<code>/note &lt;user_id&gt; &lt;备注&gt;</code>")
		}
		if err := h.services.Users.SetUserNote(ctx, id, note); err != nil {
			h.logger.Error("failed to set note",
				"actor", actor,
				"target_user", id,
				"error", err,
			)
			return err
		}
		h.logger.Info("note updated",
			"actor", actor,
			"target_user", id,
		)
		return h.send(ctx, chatID, "📝 备注已更新。")
	case "/users":
		users, err := h.services.Users.ListUsers(ctx, 100, 0)
		if err != nil {
			return err
		}
		return h.send(ctx, chatID, FormatUsers(users))
	case "/logs":
		if h.services.Logs == nil {
			return h.send(ctx, chatID, "⚠️ 日志服务未配置。")
		}
		logs, err := h.services.Logs.QueryActivityLogs(ctx, store.ActivityQuery{Limit: 50})
		if err != nil {
			return err
		}
		return h.send(ctx, chatID, FormatLogs(logs))
	}
	return nil
}

func (h *Handler) set115(ctx context.Context, userID, chatID int64, cookie string, messageID int) error {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return h.send(ctx, chatID, "⚠️ Cookie 不能为空。")
	}
	if h.services.Accounts == nil {
		return h.send(ctx, chatID, "⚠️ 115 服务未配置。")
	}
	h.logger.Info("setting 115 config",
		"user_id", userID,
		"cookie_length", len(cookie),
	)
	if err := h.services.Accounts.SetP115Config(ctx, userID, store.P115Config{Cookie: cookie, TargetCID: "0", Enabled: true}); err != nil {
		h.logger.Error("failed to set 115 config",
			"user_id", userID,
			"error", err,
		)
		return err
	}
	h.sessions.ClearInteraction(userID)
	h.logger.Info("115 config saved",
		"user_id", userID,
	)
	h.log(ctx, userID, "set115", "configured")
	// 尝试删除包含 Cookie 的消息
	if messageID > 0 {
		if err := h.messenger.DeleteMessage(ctx, chatID, messageID); err != nil {
			h.logger.Warn("failed to delete cookie message",
				"user_id", userID,
				"error", err,
			)
		} else {
			h.logger.Info("deleted cookie message",
				"user_id", userID,
				"message_id", messageID,
			)
		}
	}
	return h.send(ctx, chatID, "✅ 115 Cookie 已加密保存。

⚠️ <i>包含 Cookie 的消息已尝试删除，请确认。</i>

使用 <code>/my115</code> 查看配置状态。")
}

func (h *Handler) search(ctx context.Context, userID, chatID int64, query string, page int) error {
	if h.services.TMDB == nil {
		return h.send(ctx, chatID, "⚠️ 搜索服务未配置。")
	}
	h.logger.Info("searching TMDB",
		"user_id", userID,
		"query", query,
		"page", page,
	)
	items, total, err := h.services.TMDB.Search(ctx, query, page)
	if err != nil {
		h.logger.Error("TMDB search failed",
			"user_id", userID,
			"query", query,
			"error", err,
		)
		return err
	}
	h.logger.Info("TMDB search completed",
		"user_id", userID,
		"query", query,
		"results", len(items),
		"total_pages", total,
	)
	if len(items) == 0 {
		return h.send(ctx, chatID, "🔍 未找到 TMDB 结果。

请尝试其他关键词。")
	}
	out := Outgoing{Text: FormatTMDB(items, page, total)}
	for _, item := range items {
		value := encodeTMDB(item)
		token, err := h.sessions.BindCallback(userID, "tmdb", value)
		if err != nil {
			return err
		}
		out.Buttons = append(out.Buttons, []Button{{Text: FormatSearchButtonText(item), CallbackData: token}})
	}
	return h.messenger.Send(ctx, chatID, out)
}

func (h *Handler) HandleCallback(ctx context.Context, userID, chatID int64, callbackID, token string) error {
	cb, err := h.sessions.ResolveCallback(token, userID)
	if err != nil {
		h.logger.Warn("callback resolution failed",
			"user_id", userID,
			"callback_id", callbackID,
			"error", err,
		)
		_ = h.messenger.AnswerCallback(ctx, callbackID, "⏰ 按钮已过期或不属于你")
		return nil
	}
	h.logger.Info("received callback",
		"user_id", userID,
		"chat_id", chatID,
		"action", cb.Action,
		"value", cb.Value,
	)
	_ = h.messenger.AnswerCallback(ctx, callbackID, "⏳ 处理中…")
	if !h.authorized(ctx, userID) {
		return h.send(ctx, chatID, "🔒 你尚未获得授权。")
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
			return h.send(ctx, chatID, "⚠️ 该解锁请求已处理，不能取消。")
		}
		return h.send(ctx, chatID, "🚫 已取消解锁。")
	case "transfer":
		return h.transfer(ctx, userID, chatID, cb.Value)
	case "new_search":
		return h.send(ctx, chatID, "🔍 请发送新的搜索关键词")
	}
	return nil
}

func (h *Handler) showResources(ctx context.Context, userID, chatID int64, item TMDBItem, page int) error {
	if h.services.HDHive == nil {
		return h.send(ctx, chatID, "⚠️ HDHive 服务未配置。")
	}
	h.logger.Info("searching HDHive resources",
		"user_id", userID,
		"tmdb_id", item.ID,
		"media_type", item.MediaType,
		"title", item.Title,
		"page", page,
	)
	result, err := h.services.HDHive.Search(ctx, item, page)
	if err != nil {
		h.logger.Error("HDHive search failed",
			"user_id", userID,
			"tmdb_id", item.ID,
			"error", err,
		)
		return err
	}
	h.logger.Info("HDHive search completed",
		"user_id", userID,
		"tmdb_id", item.ID,
		"resources_found", len(result.Items),
		"total_pages", result.TotalPages,
	)
	out := Outgoing{Text: FormatResources(result)}
	for _, r := range result.Items {
		token, _ := h.sessions.BindCallback(userID, "detail", r.ID)
		out.Buttons = append(out.Buttons, []Button{{Text: FormatResourceButtonText(r), CallbackData: token}})
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
	label := "🔓 解锁资源"
	if r.Unlocked {
		action, label = "transfer", "📤 转存到 115"
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
	h.logger.Info("unlock confirmation requested",
		"user_id", userID,
		"resource_id", id,
		"resource_title", r.Title,
		"fee_known", r.FeeKnown,
		"fee", r.Fee,
		"already_unlocked", r.Unlocked,
	)
	if r.Unlocked {
		h.logger.Info("resource already unlocked",
			"user_id", userID,
			"resource_id", id,
		)
		return h.send(ctx, chatID, "✅ 该资源已经解锁，无需重复提交。")
	}
	if err := h.sessions.BeginUnlock(userID, id); err != nil {
		h.logger.Warn("unlock duplicate attempt",
			"user_id", userID,
			"resource_id", id,
			"error", err,
		)
		return h.send(ctx, chatID, "⏳ 该资源已在处理，请勿重复提交。")
	}
	if r.FeeKnown && r.Fee == 0 {
		return h.unlock(ctx, userID, chatID, id)
	}
	y, _ := h.sessions.BindCallback(userID, "unlock_confirm", id)
	n, _ := h.sessions.BindCallback(userID, "unlock_reject", id)
	return h.messenger.Send(ctx, chatID, Outgoing{Text: FormatUnlockConfirm(r), Buttons: [][]Button{{{Text: "✅ 确认解锁", CallbackData: y}, {Text: "❌ 取消", CallbackData: n}}}})
}

func (h *Handler) unlock(ctx context.Context, userID, chatID int64, id string) error {
	if current, err := h.services.HDHive.Detail(ctx, userID, id); err == nil && current.Unlocked {
		h.logger.Info("resource already unlocked, skipping",
			"user_id", userID,
			"resource_id", id,
		)
		return h.send(ctx, chatID, "✅ 该资源已经解锁，无需重复提交。")
	}
	h.logger.Info("starting unlock",
		"user_id", userID,
		"resource_id", id,
	)
	if err := h.sessions.TransitionUnlock(userID, id, session.UnlockPending, session.UnlockInFlight); err != nil {
		h.logger.Warn("unlock transition failed",
			"user_id", userID,
			"resource_id", id,
			"error", err,
		)
		return h.send(ctx, chatID, "⏳ 该资源已在处理或已成功解锁。")
	}
	r, err := h.services.HDHive.Unlock(ctx, userID, id)
	if err != nil {
		h.logger.Error("unlock failed",
			"user_id", userID,
			"resource_id", id,
			"error", err,
		)
		_ = h.sessions.SetUnlockStatus(userID, id, session.UnlockUnknown)
		return h.send(ctx, chatID, "⚠️ 解锁结果未知，请稍后查询详情，

<b>请勿重复付费。</b>")
	}
	h.logger.Info("unlock succeeded",
		"user_id", userID,
		"resource_id", id,
		"resource_title", r.Title,
		"share_code", r.ShareCode,
	)
	_ = h.sessions.SetUnlockStatus(userID, id, session.UnlockSuccess)
	h.log(ctx, userID, "unlock", id)
	out := Outgoing{Text: FormatUnlockSuccess(r)}
	var row1 []Button
	if h.services.Transfer != nil {
		t, _ := h.sessions.BindCallback(userID, "transfer", id)
		row1 = append(row1, Button{Text: "📤 转存到 115", CallbackData: t})
	}
	if len(row1) > 0 {
		out.Buttons = append(out.Buttons, row1)
	}
	out.Buttons = append(out.Buttons, []Button{{Text: "🔍 新搜索", CallbackData: "new_search"}})
	return h.messenger.Send(ctx, chatID, out)
}

func (h *Handler) transfer(ctx context.Context, userID, chatID int64, id string) error {
	if h.services.Transfer == nil || h.services.Accounts == nil {
		return h.send(ctx, chatID, "⚠️ 115 转存服务未配置。")
	}
	cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
	if err != nil {
		h.logger.Warn("115 config not found",
			"user_id", userID,
			"resource_id", id,
		)
		return h.send(ctx, chatID, "🍪 请先使用 <code>/set115</code> 配置 115 Cookie。")
	}
	r, err := h.services.HDHive.Detail(ctx, userID, id)
	if err != nil {
		return err
	}
	h.logger.Info("starting 115 transfer",
		"user_id", userID,
		"resource_id", id,
		"resource_title", r.Title,
		"share_code", r.ShareCode,
		"target_cid", cfg.TargetCID,
	)
	result, err := h.services.Transfer.Transfer115(ctx, userID, cfg, r)
	if err != nil {
		h.logger.Error("115 transfer failed",
			"user_id", userID,
			"resource_id", id,
			"error", err,
		)
		return h.send(ctx, chatID, FormatTransferFailed(err))
	}
	h.logger.Info("115 transfer succeeded",
		"user_id", userID,
		"resource_id", id,
		"result", result,
	)
	h.log(ctx, userID, "transfer115", id)
	return h.send(ctx, chatID, FormatTransferSuccess(result))
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
	p := strings.SplitN(text, " ", 2)
	cmd := strings.ToLower(strings.SplitN(p[0], "@", 2)[0])
	if len(p) == 1 {
		return cmd, ""
	}
	return cmd, strings.TrimSpace(p[1])
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
	if i.Title != "" {
		return i.Title
	}
	return i.OriginalTitle
}
