package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/atpx4869/hdhive_bot_go/internal/hdhive"
	"github.com/atpx4869/hdhive_bot_go/internal/p115"
	"github.com/atpx4869/hdhive_bot_go/internal/session"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

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
	h.sessions.SetSearchContext(userID, query, page)
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
	var navButtons []Button
	if page > 1 {
		prevToken, _ := h.sessions.BindCallback(userID, "search_page", fmt.Sprintf("%d", page-1))
		navButtons = append(navButtons, Button{Text: "⬅️ 上一页", CallbackData: prevToken})
	}
	if page < total {
		nextToken, _ := h.sessions.BindCallback(userID, "search_page", fmt.Sprintf("%d", page+1))
		navButtons = append(navButtons, Button{Text: "下一页 ➡️", CallbackData: nextToken})
	}
	if len(navButtons) > 0 {
		out.Buttons = append(out.Buttons, navButtons)
	}
	retryToken, _ := h.sessions.BindCallback(userID, "search_retry", query)
	out.Buttons = append(out.Buttons, []Button{{Text: "🔄 重新搜索", CallbackData: retryToken}})
	return h.messenger.Send(ctx, chatID, out)
}

func (h *Handler) showResources(ctx context.Context, userID, chatID int64, item TMDBItem, page int) error {
	if h.services.HDHive == nil {
		return h.send(ctx, chatID, "HDHive 服务未配置。")
	}
	result, err := h.services.HDHive.Search(ctx, item, page)
	if err != nil {
		return err
	}
	mediaTitle := item.Title
	if mediaTitle == "" {
		mediaTitle = item.OriginalTitle
	}
	if mediaTitle == "" {
		mediaTitle = fmt.Sprintf("TMDB %d", item.ID)
	}
	out := Outgoing{Text: FormatResources(result, mediaTitle)}
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
		var buttons []Button

		// #7: Use typed error kind instead of string matching
		var p115Err *p115.Error
		if errors.As(err, &p115Err) {
			switch p115Err.Kind {
			case p115.KindAuth:
				reconfigBtn, _ := h.sessions.BindCallback(userID, "set115", "")
				buttons = append(buttons, Button{Text: "重新配置 115", CallbackData: reconfigBtn})
			case p115.KindRateLimit:
				// No button needed — user can use the retry button below
			}
		} else {
			// Fallback for non-p115 errors: check common auth patterns
			errMsg := err.Error()
			if strings.Contains(errMsg, "cookie") || strings.Contains(errMsg, "登录") || strings.Contains(errMsg, "auth") {
				reconfigBtn, _ := h.sessions.BindCallback(userID, "set115", "")
				buttons = append(buttons, Button{Text: "重新配置 115", CallbackData: reconfigBtn})
			}
		}

		retryBtn, _ := h.sessions.BindCallback(userID, "transfer", id)
		buttons = append(buttons, Button{Text: "重试转存", CallbackData: retryBtn})
		backBtn, _ := h.sessions.BindCallback(userID, "resources", id)
		buttons = append(buttons, Button{Text: "返回资源列表", CallbackData: backBtn})

		h.log(ctx, userID, "transfer115", id,
			store.WithStatus("failed"),
			store.WithErrorCode(err.Error()),
		)

		out := Outgoing{
			Text:    fmt.Sprintf("❌ 转存失败：%s", err.Error()),
			Buttons: [][]Button{buttons},
		}
		return h.messenger.Send(ctx, chatID, out)
	}
	h.log(ctx, userID, "transfer115", id, store.WithStatus("success"))
	return h.send(ctx, chatID, "115 转存成功："+result)
}

// #6: Use JSON encoding for callback values to avoid delimiter conflicts

func encodeTMDB(i TMDBItem) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func decodeTMDB(v string) TMDBItem {
	var i TMDBItem
	_ = json.Unmarshal([]byte(v), &i)
	return i
}

func encodeResourcePage(i TMDBItem, p int) string {
	b, _ := json.Marshal(struct {
		TMDBItem
		Page int `json:"p"`
	}{TMDBItem: i, Page: p})
	return string(b)
}

func decodeResourcePage(v string) (TMDBItem, int) {
	var s struct {
		TMDBItem
		Page int `json:"p"`
	}
	_ = json.Unmarshal([]byte(v), &s)
	if s.Page < 1 {
		s.Page = 1
	}
	return s.TMDBItem, s.Page
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
