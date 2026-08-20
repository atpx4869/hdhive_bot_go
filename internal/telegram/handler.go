package telegram

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"sync"

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
	PosterPath                                             string // e.g. "/abc123.jpg"
}

// PosterURL returns the full poster URL or empty string if no poster.
func (t TMDBItem) PosterURL() string {
	if t.PosterPath == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w780" + t.PosterPath
}

type TMDBService interface {
	Search(context.Context, string, int) ([]TMDBItem, int, error)
}

type Resource struct {
	ID, Title, Quality, Size, Description string
	Subtitle                              string
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
	Total            int
}

// ResourceCategory 资源分组。
type ResourceCategory string

const (
	CatDefault ResourceCategory = "default" // 115 + ed2k（不含蓝光原盘/ISO）
	CatISO     ResourceCategory = "iso"     // 蓝光原盘/ISO
	CatOther   ResourceCategory = "other"   // 其他网盘类型
)

type HDHiveService interface {
	Search(context.Context, TMDBItem, int, ResourceCategory, int64) (ResourcePage, error)
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

// Button, View, MessageRef, Messenger etc. are defined in view_types.go

type Handler struct {
	services  Services
	sessions  *session.Manager
	messenger Messenger
	admins    map[int64]struct{}
	logger    *slog.Logger
	version   string
	// Active message tracking: key = chatID (private chat = userID)
	activeMsg   map[int64]MessageRef // chatID → current main card
	activeMsgMu sync.RWMutex
}

func NewHandler(services Services, sessions *session.Manager, messenger Messenger, adminIDs []int64, logger *slog.Logger, version string) (*Handler, error) {
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
	return &Handler{services: services, sessions: sessions, messenger: messenger, admins: admins, logger: logger, version: version, activeMsg: make(map[int64]MessageRef)}, nil
}

// getActiveMsg returns the current main card for a chat, or zero value if none.
func (h *Handler) getActiveMsg(chatID int64) (MessageRef, bool) {
	h.activeMsgMu.RLock()
	defer h.activeMsgMu.RUnlock()
	ref, ok := h.activeMsg[chatID]
	return ref, ok
}

// setActiveMsg stores the current main card for a chat.
func (h *Handler) setActiveMsg(chatID int64, ref MessageRef) {
	h.activeMsgMu.Lock()
	defer h.activeMsgMu.Unlock()
	h.activeMsg[chatID] = ref
}

// clearActiveMsg removes the tracked main card for a chat.
func (h *Handler) clearActiveMsg(chatID int64) {
	h.activeMsgMu.Lock()
	defer h.activeMsgMu.Unlock()
	delete(h.activeMsg, chatID)
}

// renderOrCreate edits the active message if it exists, or creates a new one.
// Updates the active message reference on success.
func (h *Handler) renderOrCreate(ctx context.Context, chatID int64, view View) (MessageRef, error) {
	if ref, ok := h.getActiveMsg(chatID); ok {
		newRef, err := h.messenger.Render(ctx, ref, view)
		if err != nil {
			// Edit failed (message deleted?) → send new card
			h.logger.Warn("render failed, creating new card", "chat_id", chatID, "error", err)
			newRef, err = h.messenger.Send(ctx, chatID, view)
			if err != nil {
				return MessageRef{}, err
			}
		}
		h.setActiveMsg(chatID, newRef)
		return newRef, nil
	}
	// No active message → create new
	ref, err := h.messenger.Send(ctx, chatID, view)
	if err != nil {
		return MessageRef{}, err
	}
	h.setActiveMsg(chatID, ref)
	return ref, nil
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

// showHome 渲染 /start 首页（状态面板 + 按钮）。
func (h *Handler) showHome(ctx context.Context, userID, chatID int64) error {
	authorized := h.authorized(ctx, userID)
	has115 := false
	if h.services.Accounts != nil {
		_, err := h.services.Accounts.GetP115Config(ctx, userID)
		has115 = err == nil
	}
	view := BuildHomeView(userID, h.isAdmin(userID), authorized, has115, h.version)
	// Set callback tokens
	for i, row := range view.Buttons {
		for j, btn := range row {
			if btn.CallbackData == "noop" {
				switch btn.Text {
				case "⚙️ 115 设置":
					t, _ := h.sessions.BindCallback(userID, "settings_115", "")
					view.Buttons[i][j].CallbackData = t
				case "❓ 使用帮助":
					t, _ := h.sessions.BindCallback(userID, "noop", "help")
					view.Buttons[i][j].CallbackData = t
				case "🛠 管理面板":
					t, _ := h.sessions.BindCallback(userID, "noop", "admin")
					view.Buttons[i][j].CallbackData = t
				}
			}
		}
	}
	_, err := h.messenger.Send(ctx, chatID, view)
	return err
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
	case "/start", "/help":
		// Remove old ReplyKeyboard if present
		if bm, ok := h.messenger.(BotMessenger); ok {
			_ = bm.RemoveReplyKeyboard(ctx, chatID)
		}
		return h.showHome(ctx, userID, chatID)
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
			return h.send(ctx, chatID, "🍪 请发送 115 Cookie。\n\n发送后将<b>加密保存</b>；可用 <code>/unset115</code> 删除。")
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
			return h.send(ctx, chatID, "🍪 尚未配置 115 Cookie。\n\n使用 <code>/set115</code> 进行配置。")
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
	return h.send(ctx, chatID, "✅ 115 Cookie 已加密保存。\n\n⚠️ <i>包含 Cookie 的消息已尝试删除，请确认。</i>\n\n使用 <code>/my115</code> 查看配置状态。")
}

func (h *Handler) search(ctx context.Context, userID, chatID int64, query string, page int) error {
	if h.services.TMDB == nil {
		return h.send(ctx, chatID, "⚠️ 搜索服务未配置。")
	}
	h.logger.Info("searching TMDB", "user_id", userID, "query", query, "page", page)

	// Show loading state
	loadingView := BuildSearchLoadingView(query)
	if _, err := h.renderOrCreate(ctx, chatID, loadingView); err != nil {
		h.logger.Warn("failed to show loading state", "error", err)
	}

	items, total, err := h.services.TMDB.Search(ctx, query, page)
	if err != nil {
		h.logger.Error("TMDB search failed", "user_id", userID, "query", query, "error", err)
		_, _ = h.renderOrCreate(ctx, chatID, View{Body: "❌ 搜索失败，请稍后重试。", Buttons: [][]Button{{CallbackButton("🔄 重试", "noop", "")}}})
		return err
	}

	if len(items) == 0 {
		_, _ = h.renderOrCreate(ctx, chatID, BuildSearchEmptyView(query))
		return nil
	}

	// Build results view with callback tokens
	out := BuildSearchResultsView(items, page, total, query)
	for i, item := range items {
		value := encodeTMDB(item)
		token, err := h.sessions.BindCallback(userID, "tmdb", value)
		if err != nil {
			return err
		}
		btnText := formatSearchButton(item)
		out.Buttons[i] = []Button{CallbackButton(btnText, token, "")}
	}
	// Set navigation tokens
	btnIdx := len(items)
	if page > 1 {
		prevToken, _ := h.sessions.BindCallback(userID, "search_page", fmt.Sprintf("%d:%s", page-1, query))
		out.Buttons[btnIdx][0].CallbackData = prevToken
	}
	if len(out.Buttons[btnIdx]) > 1 {
		// noop page indicator - no token needed
		if page < total && len(out.Buttons[btnIdx]) > 2 {
			nextToken, _ := h.sessions.BindCallback(userID, "search_page", fmt.Sprintf("%d:%s", page+1, query))
			out.Buttons[btnIdx][2].CallbackData = nextToken
		}
	}

	// Try to use poster from first result
	posterURL := SearchResultsPoster(items)
	if posterURL != "" {
		out.Media = &Media{Type: "photo", URL: posterURL}
	}

	_, err = h.renderOrCreate(ctx, chatID, out)
	return err
}

func (h *Handler) HandleCallback(ctx context.Context, cctx CallbackContext) error {
	cb, err := h.sessions.ResolveCallback(cctx.CallbackData, cctx.UserID)
	if err != nil {
		// 字面 action（close/new_search/noop/back_search 等）不经 token 解析，直接按 action 处理
		switch cctx.CallbackData {
		case "close", "new_search", "noop", "back_search":
			cb = session.Callback{UserID: cctx.UserID, Action: cctx.CallbackData, Value: ""}
		default:
			h.logger.Warn("callback resolution failed",
				"user_id", cctx.UserID,
				"callback_id", cctx.CallbackID,
				"error", err,
			)
			_ = h.messenger.AnswerCallback(ctx, cctx.CallbackID, CallbackAnswer{Text: "⏰ 此页面已过期，请重新搜索", ShowAlert: true})
			return nil
		}
	}
	h.logger.Info("received callback",
		"user_id", cctx.UserID,
		"chat_id", cctx.ChatID,
		"message_id", cctx.MessageID,
		"action", cb.Action,
		"value", cb.Value,
	)
	_ = h.messenger.AnswerCallback(ctx, cctx.CallbackID, CallbackAnswer{Text: "⏳ 处理中…"})
	if !h.authorized(ctx, cctx.UserID) {
		_, err := h.messenger.Send(ctx, cctx.ChatID, ViewFromText("🔒 你尚未获得授权。"))
		return err
	}
	switch cb.Action {
	case "noop":
		// Show contextual hint based on value
		switch cb.Value {
		case "search_hint":
			_ = h.messenger.AnswerCallback(ctx, cctx.CallbackID, CallbackAnswer{Text: "请直接发送影视名称", ShowAlert: true})
		case "help":
			_ = h.messenger.AnswerCallback(ctx, cctx.CallbackID, CallbackAnswer{Text: "直接发送影视关键词即可搜索", ShowAlert: true})
		case "admin":
			_ = h.messenger.AnswerCallback(ctx, cctx.CallbackID, CallbackAnswer{Text: "请使用管理命令", ShowAlert: true})
		}
		return nil
	case "tmdb":
		return h.showMovieCard(ctx, cctx.UserID, cctx.ChatID, decodeTMDB(cb.Value))
	case "movie_resources":
		return h.showResources(ctx, cctx.UserID, cctx.ChatID, decodeTMDB(cb.Value), 1, CatDefault)
	case "back_search":
		// Return to search results - need to re-search
		return h.send(ctx, cctx.ChatID, "🔍 请发送新的搜索关键词")
	case "search_page":
		// Parse "page:query" format
		parts := strings.SplitN(cb.Value, ":", 2)
		page := 1
		query := cb.Value
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &page)
			query = parts[1]
		}
		return h.search(ctx, cctx.UserID, cctx.ChatID, query, page)
	case "resources":
		item, page, cat := decodeResourceNav(cb.Value)
		return h.showResources(ctx, cctx.UserID, cctx.ChatID, item, page, cat)
	case "unlock":
		return h.unlockResource(ctx, cctx.UserID, cctx.ChatID, cb.Value)
	case "settings_115":
		return h.show115Settings(ctx, cctx.UserID, cctx.ChatID)
	case "home":
		return h.showHome(ctx, cctx.UserID, cctx.ChatID)
	case "transfer":
		return h.transfer(ctx, cctx.UserID, cctx.ChatID, cb.Value)
	case "new_search":
		_, _ = h.messenger.Send(ctx, cctx.ChatID, ViewFromText("🔍 请发送新的搜索关键词"))
		return nil
	case "close":
		return h.closeCard(ctx, cctx)
	}
	return nil
}

// closeCard clears the keyboard and marks the message as closed.
func (h *Handler) show115Settings(ctx context.Context, userID, chatID int64) error {
	homeToken, _ := h.sessions.BindCallback(userID, "home", "")
	if h.services.Accounts == nil {
		_, err := h.renderOrCreate(ctx, chatID, View{
			Body: "⚠️ 115 服务未配置。",
			Buttons: [][]Button{
				{CallbackButton("‹ 返回首页", homeToken, "")},
			},
		})
		return err
	}
	cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
	if err != nil {
		// Not configured
		view := View{
			Body: "🍪 <b>115 设置</b>\n\n尚未配置 115 Cookie。\n\n使用 <code>/set115</code> 命令配置。",
			Buttons: [][]Button{
				{CallbackButton("‹ 返回首页", homeToken, "")},
				{CallbackButton("✕ 关闭", "close", "")},
			},
		}
		_, err := h.renderOrCreate(ctx, chatID, view)
		return err
	}
	// Show status
	status := "✅ 已配置"
	if !cfg.Enabled {
		status = "⏸ 已停用"
	}
	target := "默认目录"
	if cfg.TargetCID != "" && cfg.TargetCID != "0" {
		target = fmt.Sprintf("目录 %s", cfg.TargetCID)
	}
	var b strings.Builder
	b.WriteString("🍪 <b>115 设置</b>\n\n")
	fmt.Fprintf(&b, "状态：%s\n", status)
	fmt.Fprintf(&b, "目标：%s\n", target)
	buttons := [][]Button{
		{CallbackButton("‹ 返回首页", homeToken, ""), CallbackButton("✕ 关闭", "close", "")},
	}
	view := View{Body: b.String(), Buttons: buttons}
	_, err = h.renderOrCreate(ctx, chatID, view)
	return err
}

func (h *Handler) closeCard(ctx context.Context, cctx CallbackContext) error {
	// 关闭即删除当前消息，并清除该聊天的活动消息跟踪
	h.clearActiveMsg(cctx.ChatID)
	if err := h.messenger.DeleteMessage(ctx, cctx.ChatID, cctx.MessageID); err != nil {
		h.logger.Warn("failed to delete message", "chat_id", cctx.ChatID, "message_id", cctx.MessageID, "error", err)
		return err
	}
	return nil
}

// showMovieCard displays the movie/TV card with poster and "view resources" button.
func (h *Handler) showMovieCard(ctx context.Context, userID, chatID int64, item TMDBItem) error {
	h.logger.Info("showing movie card", "user_id", userID, "tmdb_id", item.ID, "title", item.Title)

	view := BuildMovieCardView(item)

	// Set callback tokens
	resourcesToken, _ := h.sessions.BindCallback(userID, "movie_resources", encodeTMDB(item))
	view.Buttons[0][0].CallbackData = resourcesToken // "查看资源"
	// back_search and close already have tokens set

	_, err := h.renderOrCreate(ctx, chatID, view)
	return err
}

func (h *Handler) showResources(ctx context.Context, userID, chatID int64, item TMDBItem, page int, category ResourceCategory) error {
	if h.services.HDHive == nil {
		return h.send(ctx, chatID, "⚠️ HDHive 服务未配置。")
	}
	h.logger.Info("searching HDHive resources",
		"user_id", userID,
		"tmdb_id", item.ID,
		"media_type", item.MediaType,
		"title", item.Title,
		"page", page,
		"category", category,
	)
	result, err := h.services.HDHive.Search(ctx, item, page, category, userID)
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
		"category", category,
	)
	body := FormatResources(result)
	if item.Title != "" {
		body = "🎬 <b>" + html.EscapeString(item.Title) + "</b>\n\n" + body
	}
	out := View{Body: body}

	// 资源选择按钮：序号 + 状态，一行 4 个
	var row []Button
	for i, r := range result.Items {
		token, _ := h.sessions.BindCallback(userID, "unlock", encodeDetail(item, page, category, r.ID))
		row = append(row, CallbackButton(resourceStateLabel(r, i), token, ""))
		if len(row) == 4 {
			out.Buttons = append(out.Buttons, row)
			row = nil
		}
	}
	if len(row) > 0 {
		out.Buttons = append(out.Buttons, row)
	}

	// 翻页
	var nav []Button
	if page > 1 {
		t, _ := h.sessions.BindCallback(userID, "resources", encodeResourceNav(item, page-1, category))
		nav = append(nav, CallbackButton("‹ 上一页", t, ""))
	}
	nav = append(nav, NoopButton(fmt.Sprintf("%d/%d", page, max(result.TotalPages, 1))))
	if page < result.TotalPages {
		t, _ := h.sessions.BindCallback(userID, "resources", encodeResourceNav(item, page+1, category))
		nav = append(nav, CallbackButton("下一页 ›", t, ""))
	}
	if len(nav) > 0 {
		out.Buttons = append(out.Buttons, nav)
	}

	// 分类切换按钮（显示除当前分类外的其他分组）
	catRow := []Button{}
	if category != CatISO {
		t, _ := h.sessions.BindCallback(userID, "resources", encodeResourceNav(item, 1, CatISO))
		catRow = append(catRow, CallbackButton("蓝光原盘/ISO", t, ""))
	}
	if category != CatOther {
		t, _ := h.sessions.BindCallback(userID, "resources", encodeResourceNav(item, 1, CatOther))
		catRow = append(catRow, CallbackButton("其他类型资源", t, ""))
	}
	if category != CatDefault {
		t, _ := h.sessions.BindCallback(userID, "resources", encodeResourceNav(item, 1, CatDefault))
		catRow = append(catRow, CallbackButton("常规资源", t, ""))
	}
	if len(catRow) > 0 {
		out.Buttons = append(out.Buttons, catRow)
	}

	// 返回影片 + 关闭
	backToken, _ := h.sessions.BindCallback(userID, "movie_resources", encodeTMDB(item))
	out.Buttons = append(out.Buttons, []Button{
		CallbackButton("‹ 返回影片", backToken, ""),
		CallbackButton("✕ 关闭", "close", ""),
	})
	_, err = h.renderOrCreate(ctx, chatID, out)
	return err
}

func (h *Handler) showDetail(ctx context.Context, userID, chatID int64, value string) error {
	item, page, category, id := decodeDetail(value)
	r, err := h.services.HDHive.Detail(ctx, userID, id)
	if err != nil {
		return err
	}
	h.logger.Info("showing resource detail", "user_id", userID, "resource_id", id, "unlocked", r.Unlocked)

	out := BuildResourceDetailView(r, "")

	// 返回按钮真正回到之前资源列表的对应页/分类
	backToken, _ := h.sessions.BindCallback(userID, "resources", encodeResourceNav(item, page, category))

	if r.Unlocked {
		transferToken, _ := h.sessions.BindCallback(userID, "transfer", id)
		out.Buttons = [][]Button{
			{CallbackButton("📥 转存到 115", transferToken, "success")},
		}
		// 复制完整分享链接
		if link := shareLinkText(r); link != "" {
			out.Buttons = append(out.Buttons, []Button{CopyButton("📋 复制分享链接", link)})
		}
		out.Buttons = append(out.Buttons, []Button{
			CallbackButton("‹ 返回资源列表", backToken, ""),
			CallbackButton("✕ 关闭", "close", ""),
		})
	} else {
		unlockToken, _ := h.sessions.BindCallback(userID, "unlock", value)
		out.Buttons = [][]Button{
			{CallbackButton("🔓 解锁资源", unlockToken, "success")},
			{CallbackButton("‹ 返回资源列表", backToken, ""), CallbackButton("✕ 关闭", "close", "")},
		}
		if r.FeeKnown {
			feeText := "免费"
			if r.Fee > 0 {
				feeText = fmt.Sprintf("%d积分", r.Fee)
			}
			out.Body += fmt.Sprintf("\n\n该操作将消耗 %s。", feeText)
		} else {
			out.Body += "\n\n该操作可能消耗积分。"
		}
	}

	_, err = h.renderOrCreate(ctx, chatID, out)
	return err
}

// unlockResource 是资源解锁的统一入口：
// 已解锁的资源显示详情（转存/复制链接），未解锁的直接执行解锁（不再二次确认）。
func (h *Handler) unlockResource(ctx context.Context, userID, chatID int64, value string) error {
	item, page, category, id := decodeDetail(value)
	r, err := h.services.HDHive.Detail(ctx, userID, id)
	if err != nil {
		return err
	}
	h.logger.Info("unlock requested",
		"user_id", userID,
		"resource_id", id,
		"resource_title", r.Title,
		"fee_known", r.FeeKnown,
		"fee", r.Fee,
		"already_unlocked", r.Unlocked,
	)
	if r.Unlocked {
		return h.showDetail(ctx, userID, chatID, value)
	}
	if err := h.sessions.BeginUnlock(userID, id); err != nil {
		h.logger.Warn("unlock duplicate attempt",
			"user_id", userID,
			"resource_id", id,
			"error", err,
		)
		return h.send(ctx, chatID, "⏳ 该资源已在处理，请勿重复提交。")
	}
	return h.unlock(ctx, userID, chatID, id, item, page, category)
}

func (h *Handler) unlock(ctx context.Context, userID, chatID int64, id string, item TMDBItem, page int, category ResourceCategory) error {
	if current, err := h.services.HDHive.Detail(ctx, userID, id); err == nil && current.Unlocked {
		return h.send(ctx, chatID, "✅ 该资源已经解锁，无需重复提交。")
	}
	h.logger.Info("starting unlock", "user_id", userID, "resource_id", id)
	if err := h.sessions.TransitionUnlock(userID, id, session.UnlockPending, session.UnlockInFlight); err != nil {
		return h.send(ctx, chatID, "⏳ 该资源已在处理或已成功解锁。")
	}

	// Show busy UI
	r, _ := h.services.HDHive.Detail(ctx, userID, id)
	_, _ = h.renderOrCreate(ctx, chatID, BuildUnlockBusyView(r))

	// Execute unlock
	result, err := h.services.HDHive.Unlock(ctx, userID, id)
	if err != nil {
		h.logger.Error("unlock failed", "user_id", userID, "resource_id", id, "error", err)
		_ = h.sessions.SetUnlockStatus(userID, id, session.UnlockUnknown)

		// Build result view with tokens
		view := BuildUnlockUnknownView(r)
		backToken, _ := h.sessions.BindCallback(userID, "resources", encodeResourceNav(item, page, category))
		view.Buttons[0][0].CallbackData = backToken
		_, _ = h.renderOrCreate(ctx, chatID, view)
		return nil
	}

	h.logger.Info("unlock succeeded", "user_id", userID, "resource_id", id, "title", result.Title)
	_ = h.sessions.SetUnlockStatus(userID, id, session.UnlockSuccess)
	h.log(ctx, userID, "unlock", id)

	// Build success view with tokens
	out := BuildUnlockSuccessView(result)
	// Set callback tokens
	btnIdx := 0
	if h.services.Transfer != nil {
		transferToken, _ := h.sessions.BindCallback(userID, "transfer", id)
		out.Buttons[btnIdx][0].CallbackData = transferToken
		btnIdx++
	}
	// 返回资源列表按钮：最后一行第一个
	if n := len(out.Buttons); n > 0 {
		last := n - 1
		if len(out.Buttons[last]) > 0 {
			backToken, _ := h.sessions.BindCallback(userID, "resources", encodeResourceNav(item, page, category))
			out.Buttons[last][0].CallbackData = backToken
		}
	}

	_, err = h.renderOrCreate(ctx, chatID, out)
	return err
}

func (h *Handler) transfer(ctx context.Context, userID, chatID int64, id string) error {
	if h.services.Transfer == nil || h.services.Accounts == nil {
		return h.send(ctx, chatID, "⚠️ 115 转存服务未配置。")
	}
	cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
	if err != nil {
		return h.send(ctx, chatID, "🍪 请先使用 <code>/set115</code> 配置 115 Cookie。")
	}
	r, err := h.services.HDHive.Detail(ctx, userID, id)
	if err != nil {
		return err
	}
	h.logger.Info("starting 115 transfer", "user_id", userID, "resource_id", id, "title", r.Title, "share_code", maskCode(r.ShareCode), "receive_code", maskCode(r.ReceiveCode))

	// Show busy UI
	_, _ = h.renderOrCreate(ctx, chatID, BuildTransferBusyView())

	// Execute transfer
	result, err := h.services.Transfer.Transfer115(ctx, userID, cfg, r)
	if err != nil {
		h.logger.Error("115 transfer failed", "user_id", userID, "resource_id", id, "error", err)
		view := BuildTransferFailedView(err)
		// Set callback tokens for buttons
		for i, row := range view.Buttons {
			for j, btn := range row {
				if btn.CallbackData == "" {
					switch btn.Text {
					case "🔄 重试转存":
						t, _ := h.sessions.BindCallback(userID, "transfer", id)
						view.Buttons[i][j].CallbackData = t
					case "‹ 返回资源":
						t, _ := h.sessions.BindCallback(userID, "noop", "")
						view.Buttons[i][j].CallbackData = t
					}
				}
			}
		}
		_, _ = h.renderOrCreate(ctx, chatID, view)
		return nil
	}

	h.logger.Info("115 transfer succeeded", "user_id", userID, "resource_id", id, "result", result)
	h.log(ctx, userID, "transfer115", id)

	view := BuildTransferSuccessView(result)
	// Set callback tokens
	for i, row := range view.Buttons {
		for j, btn := range row {
			if btn.CallbackData == "" {
				t, _ := h.sessions.BindCallback(userID, "noop", "")
				view.Buttons[i][j].CallbackData = t
			}
		}
	}
	_, _ = h.renderOrCreate(ctx, chatID, view)
	return nil
}

func (h *Handler) log(ctx context.Context, id int64, action, detail string) {
	if h.services.Logs != nil {
		_, _ = h.services.Logs.AddActivityLog(ctx, id, action, detail)
	}
}
func (h *Handler) send(ctx context.Context, chatID int64, text string) error {
	_, err := h.messenger.Send(ctx, chatID, ViewFromText(text))
	return err
}

// maskCode 脱敏分享码/提取码，仅保留首尾各 2 字符用于排查日志。
func maskCode(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 6 {
		return "***"
	}
	return s[:2] + "***" + s[len(s)-2:]
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
	return strings.Join([]string{
		strconv.FormatInt(i.ID, 10),
		cleanField(i.MediaType),
		cleanField(i.Title),
		cleanField(i.OriginalTitle),
		cleanField(i.ReleaseDate),
		cleanField(i.PosterPath),
		strconv.FormatFloat(i.VoteAverage, 'f', 1, 64),
		cleanField(i.Overview),
	}, "|")
}
func decodeTMDB(v string) TMDBItem {
	var i TMDBItem
	p := strings.Split(v, "|")
	if len(p) >= 1 {
		fmt.Sscan(p[0], &i.ID)
	}
	if len(p) >= 2 {
		i.MediaType = p[1]
	}
	if len(p) >= 3 {
		i.Title = p[2]
	}
	if len(p) >= 4 {
		i.OriginalTitle = p[3]
	}
	if len(p) >= 5 {
		i.ReleaseDate = p[4]
	}
	if len(p) >= 6 {
		i.PosterPath = p[5]
	}
	if len(p) >= 7 {
		fmt.Sscan(p[6], &i.VoteAverage)
	}
	if len(p) >= 8 {
		i.Overview = p[7]
	}
	return i
}
func cleanField(s string) string {
	s = strings.ReplaceAll(s, "|", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
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

// encodeResourceNav 编码 TMDB + 页码 + 分类，用于资源列表翻页/分类切换按钮。
func encodeResourceNav(i TMDBItem, p int, cat ResourceCategory) string {
	return encodeResourcePage(i, p) + "\n" + string(cat)
}
func decodeResourceNav(v string) (TMDBItem, int, ResourceCategory) {
	idx := strings.LastIndex(v, "\n")
	if idx < 0 {
		return TMDBItem{}, 1, CatDefault
	}
	item, page := decodeResourcePage(v[:idx])
	return item, page, ResourceCategory(v[idx+1:])
}

// encodeDetail 编码 TMDB + 页码 + 分类 + 资源ID，用于资源详情返回列表。
func encodeDetail(i TMDBItem, p int, cat ResourceCategory, id string) string {
	return encodeResourceNav(i, p, cat) + "\n" + id
}
func decodeDetail(v string) (TMDBItem, int, ResourceCategory, string) {
	idx := strings.LastIndex(v, "\n")
	if idx < 0 {
		return TMDBItem{}, 1, CatDefault, v
	}
	item, page, cat := decodeResourceNav(v[:idx])
	return item, page, cat, v[idx+1:]
}

// resourceStateLabel 生成资源选择按钮文案：序号 + 状态 emoji/积分。
func resourceStateLabel(r Resource, index int) string {
	if r.Unlocked {
		return fmt.Sprintf("%d、✅", index+1)
	}
	if r.FeeKnown {
		if r.Fee == 0 {
			return fmt.Sprintf("%d、🆓", index+1)
		}
		return fmt.Sprintf("%d、%d积分", index+1, r.Fee)
	}
	return fmt.Sprintf("%d、❓", index+1)
}
func displayTitle(i TMDBItem) string {
	if i.Title != "" {
		return i.Title
	}
	return i.OriginalTitle
}

// homeButtons returns the Inline Keyboard for the /start home page.
func homeButtons(userID int64, isAdmin bool) [][]Button {
	rows := [][]Button{
		{CallbackButton("⚙️ 115 设置", "noop", ""), CallbackButton("❓ 使用帮助", "noop", "")},
	}
	if isAdmin {
		rows = append(rows, []Button{CallbackButton("🛠 管理面板", "noop", "")})
	}
	return rows
}
