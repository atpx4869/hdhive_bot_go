package telegram

import (
	"fmt"
	"html"
	"strings"
)

// ──────────────────────── Home View ────────────────────────

func BuildHomeView(userID int64, isAdmin, authorized, has115 bool) View {
	return View{
		Body:    StatusPanel(userID, isAdmin, authorized, has115),
		Buttons: homeButtons(userID, isAdmin),
	}
}

// ──────────────────────── Search Loading View ────────────────────────

func BuildSearchLoadingView(query string) View {
	return View{
		Body: fmt.Sprintf("🔎 正在搜索「<b>%s</b>」…", html.EscapeString(query)),
		Buttons: [][]Button{
			{NoopButton("⏳ 正在加载…")},
		},
	}
}

// ──────────────────────── Search Results View ────────────────────────

func BuildSearchResultsView(items []TMDBItem, page, totalPages int, query string) View {
	var b strings.Builder
	fmt.Fprintf(&b, "🔎 <b>搜索结果</b>\n\n")
	fmt.Fprintf(&b, "关键词：%s\n", html.EscapeString(query))
	fmt.Fprintf(&b, "第 %d / %d 页\n", page, max(totalPages, 1))
	body := b.String()

	var buttons [][]Button
	for _, item := range items {
		token := "" // will be set by caller
		btnText := formatSearchButton(item)
		buttons = append(buttons, []Button{CallbackButton(btnText, token, "")})
	}

	// Navigation
	var nav []Button
	if page > 1 {
		nav = append(nav, CallbackButton("‹ 上一页", "", ""))
	}
	nav = append(nav, NoopButton(fmt.Sprintf("%d / %d", page, max(totalPages, 1))))
	if page < totalPages {
		nav = append(nav, CallbackButton("下一页 ›", "", ""))
	}
	if len(nav) > 0 {
		buttons = append(buttons, nav)
	}

	// Close
	buttons = append(buttons, []Button{CallbackButton("✕ 关闭", "close", "")})

	return View{Body: body, Buttons: buttons}
}

func formatSearchButton(item TMDBItem) string {
	title := item.Title
	if title == "" {
		title = item.OriginalTitle
	}
	year := ""
	if len(item.ReleaseDate) >= 4 {
		year = item.ReleaseDate[:4]
	}
	text := title
	if item.MediaType == "tv" && title != "" {
		text += " (剧集)"
	}
	if year != "" {
		text += " · " + year
	}
	if item.VoteAverage > 0 {
		text += fmt.Sprintf(" · ⭐ %.1f", item.VoteAverage)
	}
	// Truncate to ~42 visible chars
	runes := []rune(text)
	if len(runes) > 42 {
		text = string(runes[:40]) + "…"
	}
	return text
}

// SearchResultsPoster returns the best poster URL from search results.
func SearchResultsPoster(items []TMDBItem) string {
	for _, item := range items {
		if url := item.PosterURL(); url != "" {
			return url
		}
	}
	return ""
}

// ──────────────────────── Movie Card View ────────────────────────

func BuildMovieCardView(item TMDBItem) View {
	var b strings.Builder
	// Title
	title := item.Title
	if title == "" {
		title = item.OriginalTitle
	}
	fmt.Fprintf(&b, "🎬 <b>%s</b>", html.EscapeString(title))
	if item.OriginalTitle != "" && item.OriginalTitle != item.Title {
		fmt.Fprintf(&b, "\n%s", html.EscapeString(item.OriginalTitle))
	}
	b.WriteByte('\n')

	// Meta line
	meta := []string{}
	if item.MediaType == "tv" {
		meta = append(meta, "剧集")
	} else {
		meta = append(meta, "电影")
	}
	if len(item.ReleaseDate) >= 4 {
		meta = append(meta, item.ReleaseDate[:4])
	}
	if item.VoteAverage > 0 {
		meta = append(meta, fmt.Sprintf("⭐ %.1f / 10", item.VoteAverage))
	}
	if len(meta) > 0 {
		fmt.Fprintf(&b, "\n%s\n", strings.Join(meta, " · "))
	}

	// Overview
	if item.Overview != "" {
		overview := TruncateRunes(item.Overview, 180)
		fmt.Fprintf(&b, "\n%s", html.EscapeString(overview))
	}

	body := TruncateCaption(b.String())

	buttons := [][]Button{
		{CallbackButton("🔎 查看资源", "", "primary")},
		{CallbackButton("‹ 搜索结果", "back_search", ""), CallbackButton("✕ 关闭", "close", "")},
	}

	var media *Media
	if url := item.PosterURL(); url != "" {
		media = &Media{Type: "photo", URL: url}
	}

	return View{Body: body, Media: media, Buttons: buttons}
}

// ──────────────────────── Search Empty View ────────────────────────

func BuildSearchEmptyView(query string) View {
	return View{
		Body: fmt.Sprintf("🔍 没有找到「%s」的结果\n\n请尝试其他关键词。", html.EscapeString(query)),
		Buttons: [][]Button{
			{CallbackButton("🔄 重新搜索", "noop", "")},
			{CallbackButton("✕ 关闭", "close", "")},
		},
	}
}

// ──────────────────────── Close View ────────────────────────

func BuildCloseView() View {
	return View{
		Body: "已关闭 · 发送新关键词可重新搜索",
	}
}

// ──────────────────────── Resource Detail View ────────────────────────

func BuildResourceDetailView(r Resource, mediaTitle string) View {
	var b strings.Builder
	b.WriteString("📄 <b>资源详情</b>\n\n")
	if mediaTitle != "" {
		fmt.Fprintf(&b, "%s\n\n", html.EscapeString(mediaTitle))
	}

	// Info table
	if r.PanType != "" {
		fmt.Fprintf(&b, "网盘    %s\n", html.EscapeString(r.PanType))
	}
	if r.Quality != "" {
		fmt.Fprintf(&b, "画质    %s\n", html.EscapeString(r.Quality))
	}
	if r.Size != "" {
		fmt.Fprintf(&b, "大小    %s\n", html.EscapeString(r.Size))
	}
	if r.Source != "" {
		fmt.Fprintf(&b, "来源    %s\n", html.EscapeString(r.Source))
	}
	if r.Subtitle != "" {
		fmt.Fprintf(&b, "字幕    %s\n", html.EscapeString(r.Subtitle))
	}
	if r.FeeKnown {
		if r.Fee == 0 {
			b.WriteString("费用    免费\n")
		} else {
			fmt.Fprintf(&b, "费用    %d 积分\n", r.Fee)
		}
	} else {
		b.WriteString("费用    未知\n")
	}
	if r.Unlocked {
		b.WriteString("状态    已解锁\n")
	}

	return View{Body: TruncateCaption(b.String())}
}

// ──────────────────────── Unlock Confirm View ────────────────────────

func BuildUnlockConfirmView(r Resource) View {
	var b strings.Builder
	b.WriteString("⚠️ <b>确认解锁</b>\n\n")
	fmt.Fprintf(&b, "%s\n", html.EscapeString(r.Title))
	if r.Quality != "" || r.Size != "" {
		parts := []string{}
		if r.PanType != "" {
			parts = append(parts, r.PanType)
		}
		if r.Quality != "" {
			parts = append(parts, r.Quality)
		}
		if r.Size != "" {
			parts = append(parts, r.Size)
		}
		fmt.Fprintf(&b, "%s\n\n", html.EscapeString(strings.Join(parts, " · ")))
	} else {
		b.WriteByte('\n')
	}
	if r.FeeKnown {
		if r.Fee == 0 {
			b.WriteString("将<b>免费</b>解锁此资源。\n")
		} else {
			fmt.Fprintf(&b, "将消耗 <b>%d 积分</b>。\n", r.Fee)
		}
	} else {
		b.WriteString("⚠️ 服务端没有返回准确费用，实际操作仍可能扣除积分。\n只有确认愿意承担未知费用时才继续。\n")
	}
	b.WriteString("\n解锁提交后无法撤销，请勿重复点击。")

	return View{Body: TruncateCaption(b.String())}
}

// ──────────────────────── Unlock Busy View ────────────────────────

func BuildUnlockBusyView(r Resource) View {
	var b strings.Builder
	b.WriteString("⏳ <b>正在解锁…</b>\n\n")
	fmt.Fprintf(&b, "%s\n", html.EscapeString(r.Title))
	b.WriteString("\n请勿重复操作。")
	return View{
		Body: b.String(),
		Buttons: [][]Button{
			{NoopButton("⏳ 正在解锁…")},
		},
	}
}

// ──────────────────────── Unlock Success View ────────────────────────

func BuildUnlockSuccessView(r Resource) View {
	var b strings.Builder
	b.WriteString("✅ <b>解锁成功</b>\n\n")
	fmt.Fprintf(&b, "%s\n", html.EscapeString(r.Title))
	if r.Quality != "" || r.Size != "" {
		parts := []string{}
		if r.PanType != "" {
			parts = append(parts, r.PanType)
		}
		if r.Quality != "" {
			parts = append(parts, r.Quality)
		}
		if r.Size != "" {
			parts = append(parts, r.Size)
		}
		fmt.Fprintf(&b, "%s\n", html.EscapeString(strings.Join(parts, " · ")))
	}
	b.WriteByte('\n')
	if r.ReceiveCode != "" {
		fmt.Fprintf(&b, "提取码：%s\n", html.EscapeString(r.ReceiveCode))
	}
	b.WriteString("建议立即转存，避免分享失效。")

	buttons := [][]Button{}
	// Transfer button
	buttons = append(buttons, []Button{CallbackButton("📥 一键转存到 115", "", "success")})
	// URL and Copy buttons
	if r.ShareURL != "" && isValidHTTPS(r.ShareURL) {
		buttons = append(buttons, []Button{URLButton("🔗 打开资源", r.ShareURL)})
	}
	if r.ReceiveCode != "" {
		buttons = append(buttons, []Button{CopyButton("📋 复制提取码", r.ReceiveCode)})
	}
	buttons = append(buttons, []Button{
		CallbackButton("‹ 返回资源", "", ""),
		CallbackButton("🔎 新搜索", "new_search", ""),
	})

	return View{Body: TruncateCaption(b.String()), Buttons: buttons}
}

// ──────────────────────── Unlock Failed View ────────────────────────

func BuildUnlockFailedView(r Resource, reason string) View {
	var b strings.Builder
	b.WriteString("❌ <b>解锁失败</b>\n\n")
	fmt.Fprintf(&b, "%s\n\n", html.EscapeString(r.Title))
	fmt.Fprintf(&b, "%s", html.EscapeString(reason))

	return View{
		Body: TruncateCaption(b.String()),
		Buttons: [][]Button{
			{CallbackButton("‹ 返回资源", "", "")},
			{CallbackButton("✕ 关闭", "close", "")},
		},
	}
}

// ──────────────────────── Unlock Unknown View ────────────────────────

func BuildUnlockUnknownView(r Resource) View {
	var b strings.Builder
	b.WriteString("⚠️ <b>解锁结果暂时无法确认</b>\n\n")
	fmt.Fprintf(&b, "%s\n\n", html.EscapeString(r.Title))
	b.WriteString("请求可能已经提交。为避免重复扣费，Bot 不会自动重试。\n请稍后重新查看资源状态，或联系管理员核验。")

	return View{
		Body: TruncateCaption(b.String()),
		Buttons: [][]Button{
			{CallbackButton("‹ 返回资源", "", "")},
			{CallbackButton("✕ 关闭", "close", "")},
		},
	}
}

// ──────────────────────── Transfer Busy View ────────────────────────

func BuildTransferBusyView() View {
	return View{
		Body: "⏳ <b>正在转存到 115…</b>\n\n请勿重复操作。",
		Buttons: [][]Button{
			{NoopButton("⏳ 正在转存…")},
		},
	}
}

// ──────────────────────── Transfer Success View ────────────────────────

func BuildTransferSuccessView(result string) View {
	var b strings.Builder
	b.WriteString("✅ <b>已转存到 115</b>\n\n")
	if result != "" {
		fmt.Fprintf(&b, "结果：%s\n", html.EscapeString(result))
	}

	return View{
		Body: b.String(),
		Buttons: [][]Button{
			{CallbackButton("‹ 返回资源", "", "")},
			{CallbackButton("🔎 新搜索", "new_search", "")},
		},
	}
}

// ──────────────────────── Transfer Failed View ────────────────────────

func BuildTransferFailedView(err error) View {
	errMsg := err.Error()
	var body string
	var buttons [][]Button

	switch {
	case strings.Contains(errMsg, "登录") || strings.Contains(errMsg, "cookie") || strings.Contains(errMsg, "Cookie"):
		body = "❌ <b>115 转存失败</b>\n\n🍪 Cookie 已过期或无效。"
		buttons = [][]Button{
			{CallbackButton("⚙️ 重新配置 115", "noop", "")},
			{CallbackButton("‹ 返回资源", "", "")},
		}
	case strings.Contains(errMsg, "已接收") || strings.Contains(errMsg, "already"):
		body = "ℹ️ <b>该资源之前已接收过</b>\n\n未重复转存。"
		buttons = [][]Button{
			{CallbackButton("‹ 返回资源", "", "")},
		}
	case strings.Contains(errMsg, "提取码") || strings.Contains(errMsg, "password"):
		body = "❌ <b>115 转存失败</b>\n\n🔑 提取码错误或分享已失效。"
		buttons = [][]Button{
			{CallbackButton("‹ 返回资源", "", "")},
			{CallbackButton("✕ 关闭", "close", "")},
		}
	default:
		body = fmt.Sprintf("❌ <b>115 转存失败</b>\n\n%s", html.EscapeString(errMsg))
		buttons = [][]Button{
			{CallbackButton("🔄 重试转存", "", "")},
			{CallbackButton("‹ 返回资源", "", "")},
		}
	}

	return View{Body: TruncateCaption(body), Buttons: buttons}
}

// ──────────────────────── Helpers ────────────────────────

func isValidHTTPS(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}
