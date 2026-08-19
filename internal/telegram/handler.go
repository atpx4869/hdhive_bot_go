package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/atpx4869/hdhive_bot_go/internal/session"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

var ErrNotFound = errors.New("not found")

// ──────────────────────── Service Interfaces ────────────────────────

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
	AddActivityLog(context.Context, int64, string, string, ...store.ActivityLogOption) (store.ActivityLog, error)
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

// ──────────────────────── Handler ────────────────────────

type Button struct{ Text, CallbackData string }
type Outgoing struct {
	Text    string
	Buttons [][]Button
}
type Messenger interface {
	Send(context.Context, int64, Outgoing) error
	AnswerCallback(context.Context, string, string) error
	DeleteMessage(context.Context, int64, int) error
	SendDocument(ctx context.Context, chatID int64, filename string, data []byte, caption string) error
}

// #11: Authorization cache entry
type authCacheEntry struct {
	authorized bool
	expiresAt  time.Time
}

type Handler struct {
	services   Services
	sessions   *session.Manager
	messenger  Messenger
	admins     map[int64]struct{}
	httpClient *http.Client
	// #11: Authorization cache — avoids DB query on every message
	authCache    map[int64]authCacheEntry
	authCacheMu  sync.RWMutex
	authCacheTTL time.Duration
}

func NewHandler(services Services, sessions *session.Manager, messenger Messenger, adminIDs []int64, httpClient *http.Client) (*Handler, error) {
	if sessions == nil || messenger == nil {
		return nil, errors.New("sessions and messenger are required")
	}
	admins := make(map[int64]struct{}, len(adminIDs))
	for _, id := range adminIDs {
		admins[id] = struct{}{}
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Handler{
		services:     services,
		sessions:     sessions,
		messenger:    messenger,
		admins:       admins,
		httpClient:   httpClient,
		authCache:    make(map[int64]authCacheEntry),
		authCacheTTL: 5 * time.Minute,
	}, nil
}

func (h *Handler) isAdmin(id int64) bool { _, ok := h.admins[id]; return ok }

// #11: Cached authorization check
func (h *Handler) authorized(ctx context.Context, id int64) bool {
	if h.isAdmin(id) {
		return true
	}
	// Check cache first
	h.authCacheMu.RLock()
	if entry, ok := h.authCache[id]; ok && time.Now().Before(entry.expiresAt) {
		h.authCacheMu.RUnlock()
		return entry.authorized
	}
	h.authCacheMu.RUnlock()

	// Cache miss — query DB
	if h.services.Users == nil {
		return false
	}
	u, err := h.services.Users.GetUser(ctx, id)
	auth := err == nil && u.Authorized

	// Update cache
	h.authCacheMu.Lock()
	h.authCache[id] = authCacheEntry{authorized: auth, expiresAt: time.Now().Add(h.authCacheTTL)}
	h.authCacheMu.Unlock()
	return auth
}

func (h *Handler) invalidateAuthCache(userID int64) {
	h.authCacheMu.Lock()
	delete(h.authCache, userID)
	h.authCacheMu.Unlock()
}

// ──────────────────────── Message Routing ────────────────────────

func (h *Handler) HandleText(ctx context.Context, userID, chatID int64, text string) error {
	return h.HandleMessage(ctx, userID, chatID, 0, text)
}

// #10: End-to-end timeout for all message handling
const handleTimeout = 60 * time.Second

func (h *Handler) HandleMessage(ctx context.Context, userID, chatID int64, messageID int, text string) error {
	ctx, cancel := context.WithTimeout(ctx, handleTimeout)
	defer cancel()

	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// #16: Input validation — limit search query length
	if len(text) > 500 {
		return h.send(ctx, chatID, "消息过长，请简化后重试。")
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
	case "/authorize", "/revoke", "/users", "/note", "/logs", "/unlockreset", "/enable115", "/disable115", "/unknown", "/export", "/import":
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
		target := "根目录 (0)"
		if strings.TrimSpace(cfg.TargetCID) != "" && cfg.TargetCID != "0" {
			target = fmt.Sprintf("目录 %s", cfg.TargetCID)
		}
		text := fmt.Sprintf("115 配置状态：\n• 状态：%s\n• 转存目标：%s", status, target)
		var buttons []Button
		changeBtn, _ := h.sessions.BindCallback(userID, "change115cid", "")
		buttons = append(buttons, Button{Text: "修改目标目录", CallbackData: changeBtn})
		return h.messenger.Send(ctx, chatID, Outgoing{Text: text, Buttons: [][]Button{buttons}})
	}

	// Check for active interaction sessions
	if s, ok := h.sessions.Get(userID); ok {
		if chatID != userID {
			return h.send(ctx, chatID, "请回到 Bot 私聊完成 115 配置。")
		}
		switch s.Kind {
		case "set115_cookie":
			return h.receive115Cookie(ctx, userID, chatID, messageID, text)
		case "set115_cid":
			return h.receive115CID(ctx, userID, chatID, text, s.Data["cookie"])
		case "change115cid":
			return h.change115CID(ctx, userID, chatID, text)
		}
	}

	// #16: Validate search query before sending to TMDB
	query := text
	if len(query) > 200 {
		query = query[:200]
	}
	return h.search(ctx, userID, chatID, query, 1)
}

// ──────────────────────── Callback Routing ────────────────────────

func (h *Handler) HandleCallback(ctx context.Context, userID, chatID int64, callbackID, token string) error {
	ctx, cancel := context.WithTimeout(ctx, handleTimeout)
	defer cancel()

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
	case "search_page":
		page := 1
		fmt.Sscanf(cb.Value, "%d", &page)
		ctx2, ok := h.sessions.GetSearchContext(userID)
		if !ok {
			return h.send(ctx, chatID, "搜索已过期，请重新搜索。")
		}
		return h.search(ctx, userID, chatID, ctx2.Query, page)
	case "search_retry":
		return h.search(ctx, userID, chatID, cb.Value, 1)
	case "change115cid":
		if chatID != userID {
			return h.send(ctx, chatID, "为保护 Cookie，此操作只能在 Bot 私聊中使用。")
		}
		_ = h.sessions.Set(userID, "change115cid", nil)
		return h.send(ctx, chatID, "请发送新的目标目录 cid；发送 0 表示根目录。")
	case "set115":
		if chatID != userID {
			return h.send(ctx, chatID, "为保护 Cookie，此操作只能在 Bot 私聊中使用。")
		}
		_ = h.sessions.Set(userID, "set115_cookie", nil)
		return h.send(ctx, chatID, "请发送完整的 115 Cookie（应包含 UID、CID、SEID）。Bot 会尝试立即删除该消息。")
	}
	return nil
}

// ──────────────────────── Helpers ────────────────────────

func (h *Handler) log(ctx context.Context, id int64, action, detail string, opts ...store.ActivityLogOption) {
	if h.services.Logs != nil {
		_, _ = h.services.Logs.AddActivityLog(ctx, id, action, detail, opts...)
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
