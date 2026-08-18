package telegram

import (
	"context"
	"errors"
	"fmt"
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
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	cmd, arg := splitCommand(text)
	switch cmd {
	case "/start":
		return h.send(ctx, chatID, HelpText(h.isAdmin(userID)))
	case "/myid":
		return h.send(ctx, chatID, fmt.Sprintf("你的 Telegram User ID：%d", userID))
	case "/authorize", "/revoke", "/users", "/note", "/logs":
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
		if arg == "" {
			_ = h.sessions.Set(userID, "set115", nil)
			return h.send(ctx, chatID, "请发送 115 Cookie。发送后将加密保存；可用 /unset115 删除。")
		}
		return h.set115(ctx, userID, chatID, arg)
	case "/unset115":
		if h.services.Accounts == nil {
			return h.send(ctx, chatID, "115 服务未配置。")
		}
		err := h.services.Accounts.DeleteP115Config(ctx, userID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return h.send(ctx, chatID, "115 配置已删除。")
	case "/my115":
		if h.services.Accounts == nil {
			return h.send(ctx, chatID, "115 服务未配置。")
		}
		cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
		if err != nil {
			return h.send(ctx, chatID, "尚未配置 115 Cookie。")
		}
		return h.send(ctx, chatID, "115 已配置："+MaskSecret(cfg.Cookie))
	}
	if s, ok := h.sessions.Get(userID); ok && s.Kind == "set115" {
		if chatID != userID {
			return h.send(ctx, chatID, "请回到 Bot 私聊发送 115 Cookie。")
		}
		return h.set115(ctx, userID, chatID, text)
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
		users, err := h.services.Users.ListUsers(ctx, 100, 0)
		if err != nil {
			return err
		}
		return h.send(ctx, chatID, FormatUsers(users))
	case "/logs":
		if h.services.Logs == nil {
			return h.send(ctx, chatID, "日志服务未配置。")
		}
		logs, err := h.services.Logs.QueryActivityLogs(ctx, store.ActivityQuery{Limit: 50})
		if err != nil {
			return err
		}
		return h.send(ctx, chatID, FormatLogs(logs))
	}
	return nil
}

func (h *Handler) set115(ctx context.Context, userID, chatID int64, cookie string) error {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return h.send(ctx, chatID, "Cookie 不能为空。")
	}
	if h.services.Accounts == nil {
		return h.send(ctx, chatID, "115 服务未配置。")
	}
	if err := h.services.Accounts.SetP115Config(ctx, userID, store.P115Config{Cookie: cookie, TargetCID: "0", Enabled: true}); err != nil {
		return err
	}
	h.sessions.ClearInteraction(userID)
	h.log(ctx, userID, "set115", "configured")
	return h.send(ctx, chatID, "115 Cookie 已加密保存。")
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
		out.Buttons = append(out.Buttons, []Button{{Text: displayTitle(item), CallbackData: token}})
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
		_ = h.sessions.SetUnlockStatus(userID, id, session.UnlockUnknown)
		return h.send(ctx, chatID, "解锁结果未知，请稍后查询详情，勿重复付费。")
	}
	_ = h.sessions.SetUnlockStatus(userID, id, session.UnlockSuccess)
	h.log(ctx, userID, "unlock", id)
	out := Outgoing{Text: "解锁成功。\n" + FormatResource(r)}
	if h.services.Transfer != nil {
		t, _ := h.sessions.BindCallback(userID, "transfer", id)
		out.Buttons = [][]Button{{{Text: "转存到 115", CallbackData: t}}}
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
