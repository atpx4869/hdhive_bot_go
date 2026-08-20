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

// ──────────────────────── Deprecated: keep old formatters for backward compat ────────────────────────

// FormatSearchButtonText returns button text for TMDB search results.
// Deprecated: use formatSearchButton instead.
func FormatSearchButtonText(item TMDBItem) string {
	return formatSearchButton(item)
}
