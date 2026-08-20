package telegram

import (
	"fmt"
	"html"
	"strings"

	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

// HelpText returns the /start message with HTML formatting.
func HelpText(admin bool) string {
	var b strings.Builder
	b.WriteString("🎬 <b>HDHive Bot</b>\n\n")
	b.WriteString("欢迎使用 HDHive 影视资源搜索机器人。\n")
	b.WriteString("直接发送<b>影视关键词</b>即可搜索，例如：<code>流浪地球</code>\n\n")
	b.WriteString("━━━━━━━━━━━━━━━━━\n\n")
	b.WriteString("📌 <b>基础指令</b>\n")
	b.WriteString("<code>/myid</code>    — 查看你的 Telegram ID\n")
	b.WriteString("<code>/set115</code>  — 配置 115 网盘 Cookie\n")
	b.WriteString("<code>/unset115</code> — 删除 115 配置\n")
	b.WriteString("<code>/my115</code>   — 查看配置状态\n")
	if admin {
		b.WriteString("\n━━━━━━━━━━━━━━━━━\n\n")
		b.WriteString("🔧 <b>管理员指令</b>\n")
		b.WriteString("<code>/authorize &lt;id&gt;</code> — 授权用户\n")
		b.WriteString("<code>/revoke &lt;id&gt;</code>    — 撤销授权\n")
		b.WriteString("<code>/users</code>             — 用户列表\n")
		b.WriteString("<code>/note &lt;id&gt; &lt;备注&gt;</code> — 设置备注\n")
		b.WriteString("<code>/logs</code>              — 活动日志\n")
	}
	b.WriteString("\n━━━━━━━━━━━━━━━━━\n\n")
	b.WriteString("💡 <i>配置 115 Cookie 后，解锁的资源可一键转存到网盘。</i>")
	return b.String()
}

// MaskSecret returns a masked representation of a secret.
func MaskSecret(secret string) string {
	if strings.TrimSpace(secret) == "" {
		return "❌ 未配置"
	}
	return "✅ 已配置"
}

// FormatUsers formats a user list with HTML.
func FormatUsers(users []store.User) string {
	if len(users) == 0 {
		return "👥 暂无用户记录。"
	}
	var b strings.Builder
	b.WriteString("👥 <b>用户列表</b>\n\n")
	for _, u := range users {
		status := "⬜ 未授权"
		if u.Authorized {
			status = "🟩 已授权"
		}
		fmt.Fprintf(&b, "<code>%d</code>  %s", u.ID, status)
		if u.Note != "" {
			fmt.Fprintf(&b, "  📝 %s", html.EscapeString(u.Note))
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// FormatLogs formats activity logs with HTML.
func FormatLogs(logs []store.ActivityLog) string {
	if len(logs) == 0 {
		return "📋 暂无活动日志。"
	}
	var b strings.Builder
	b.WriteString("📋 <b>最近活动</b>\n\n")
	for _, l := range logs {
		fmt.Fprintf(&b, "🕐 <code>%s</code>\n", l.CreatedAt.Local().Format("01-02 15:04"))
		fmt.Fprintf(&b, "   👤 <code>%d</code>  🔹 %s", l.UserID, html.EscapeString(l.Action))
		if l.Detail != "" {
			fmt.Fprintf(&b, "  →  %s", html.EscapeString(l.Detail))
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// FormatTMDB formats TMDB search results with HTML.
func FormatTMDB(items []TMDBItem, page, totalPages int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔍 <b>搜索结果</b>（第 %d / %d 页）\n\n", page, max(totalPages, 1))
	numbers := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣", "8️⃣", "9️⃣", "🔟"}
	for i, item := range items {
		title := displayTitle(item)
		year := ""
		if len(item.ReleaseDate) >= 4 {
			year = item.ReleaseDate[:4]
		}
		num := fmt.Sprintf("%d.", i+1)
		if i < len(numbers) {
			num = numbers[i]
		}
		fmt.Fprintf(&b, "%s <b>%s</b>", num, html.EscapeString(title))
		if year != "" {
			fmt.Fprintf(&b, " <i>(%s)</i>", year)
		}
		if item.VoteAverage > 0 {
			fmt.Fprintf(&b, "  ⭐ %.1f", item.VoteAverage)
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n💡 <i>点击片名查看可用资源</i>")
	return b.String()
}

// FormatResources formats HDHive resource list with HTML.
func FormatResources(page ResourcePage) string {
	if len(page.Items) == 0 {
		return "📂 暂无匹配资源。"
	}
	total := page.Total
	if total <= 0 {
		total = len(page.Items)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 <b>%d</b> 条资源 · 第 <b>%d/%d</b> 页\n\n", total, page.Page, max(page.TotalPages, 1))
	for i, r := range page.Items {
		fmt.Fprintf(&b, "%s <b>%s</b>\n", resourceNumber(i), html.EscapeString(r.Title))
		primary := []string{}
		if r.PanType != "" {
			primary = append(primary, panEmoji(r.PanType)+r.PanType)
		}
		if r.Quality != "" {
			primary = append(primary, "🎞"+r.Quality)
		}
		if r.Size != "" {
			primary = append(primary, "📦"+r.Size)
		}
		if r.FeeKnown {
			if r.Fee == 0 {
				primary = append(primary, "🆓免费")
			} else {
				primary = append(primary, fmt.Sprintf("🏷%d积分", r.Fee))
			}
		}
		if len(primary) > 0 {
			fmt.Fprintf(&b, "　%s\n", html.EscapeString(strings.Join(primary, "　")))
		}
		secondary := []string{}
		if r.Subtitle != "" {
			secondary = append(secondary, "🌐"+r.Subtitle)
		}
		if r.Source != "" {
			secondary = append(secondary, "💿"+r.Source)
		}
		if len(secondary) > 0 {
			fmt.Fprintf(&b, "　%s\n", html.EscapeString(strings.Join(secondary, "　")))
		}
		if r.Unlocked {
			b.WriteString("　✅ 已解锁\n")
		}
		b.WriteByte('\n')
	}
	b.WriteString("👇 点击下方按钮查看资源详情")
	return strings.TrimSpace(b.String())
}

func resourceNumber(i int) string {
	numbers := []string{"1️⃣", "2️⃣", "3️⃣", "4️⃣", "5️⃣", "6️⃣", "7️⃣", "8️⃣", "9️⃣", "🔟"}
	if i < len(numbers) {
		return numbers[i]
	}
	return fmt.Sprintf("%d.", i+1)
}

func panEmoji(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "115":
		return "🟦"
	case "ed2k":
		return "🟧"
	default:
		return "⬜"
	}
}

// FormatResource formats a single resource detail with HTML.
func FormatResource(r Resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🎬 <b>%s</b>\n\n", html.EscapeString(r.Title))
	hasInfo := false
	if r.PanType != "" {
		fmt.Fprintf(&b, "☁️ 网盘：<b>%s</b>\n", html.EscapeString(r.PanType))
		hasInfo = true
	}
	if r.Quality != "" {
		fmt.Fprintf(&b, "🎞 画质：<b>%s</b>\n", html.EscapeString(r.Quality))
		hasInfo = true
	}
	if r.Size != "" {
		fmt.Fprintf(&b, "💾 大小：<b>%s</b>\n", html.EscapeString(r.Size))
		hasInfo = true
	}
	if r.Source != "" {
		fmt.Fprintf(&b, "📡 来源：<b>%s</b>\n", html.EscapeString(r.Source))
		hasInfo = true
	}
	if r.FeeKnown {
		if r.Fee == 0 {
			b.WriteString("🏷 积分：<b>免费</b>\n")
		} else {
			fmt.Fprintf(&b, "🏷 积分：<b>%d</b>\n", r.Fee)
		}
		hasInfo = true
	}
	if hasInfo {
		b.WriteByte('\n')
	}
	if r.Unlocked {
		b.WriteString("✅ <b>已解锁</b>\n")
		if r.ShareCode != "" {
			fmt.Fprintf(&b, "🔗 分享码：<code>%s</code>\n", html.EscapeString(r.ShareCode))
		}
		if r.ReceiveCode != "" {
			fmt.Fprintf(&b, "🔑 提取码：<code>%s</code>\n", html.EscapeString(r.ReceiveCode))
		}
	}
	if r.Description != "" {
		fmt.Fprintf(&b, "\n📝 %s", html.EscapeString(r.Description))
	}
	return strings.TrimSpace(b.String())
}

// FormatUnlockConfirm formats the unlock confirmation prompt.
func FormatUnlockConfirm(r Resource) string {
	var b strings.Builder
	b.WriteString("⚠️ <b>解锁确认</b>\n\n")
	fmt.Fprintf(&b, "🎬 %s\n", html.EscapeString(r.Title))
	if r.Quality != "" {
		fmt.Fprintf(&b, "🎞 %s", html.EscapeString(r.Quality))
		if r.Size != "" {
			fmt.Fprintf(&b, " · %s", html.EscapeString(r.Size))
		}
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	if r.FeeKnown {
		if r.Fee == 0 {
			b.WriteString("💰 费用：<b>免费</b>\n")
		} else {
			fmt.Fprintf(&b, "💰 费用：<b>%d 积分</b>\n", r.Fee)
		}
	} else {
		b.WriteString("💰 费用：<b>未知</b>\n")
	}
	b.WriteString("\n❓ 确认解锁此资源？")
	return b.String()
}

// FormatUnlockSuccess formats the unlock success message.
func FormatUnlockSuccess(r Resource) string {
	var b strings.Builder
	b.WriteString("✅ <b>解锁成功！</b>\n\n")
	fmt.Fprintf(&b, "🎬 <b>%s</b>\n", html.EscapeString(r.Title))
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
		fmt.Fprintf(&b, "📡 %s\n", html.EscapeString(strings.Join(parts, " · ")))
	}
	b.WriteByte('\n')
	if r.ShareCode != "" {
		fmt.Fprintf(&b, "🔗 分享码：<code>%s</code>\n", html.EscapeString(r.ShareCode))
	}
	if r.ReceiveCode != "" {
		fmt.Fprintf(&b, "🔑 提取码：<code>%s</code>\n", html.EscapeString(r.ReceiveCode))
	}
	if r.ShareURL != "" {
		fmt.Fprintf(&b, "🔗 链接：%s\n", html.EscapeString(r.ShareURL))
	}
	b.WriteString("\n👇 点击下方按钮可一键转存到 115 网盘")
	return b.String()
}

// FormatTransferSuccess formats the transfer success message.
func FormatTransferSuccess(result string) string {
	return fmt.Sprintf("✅ <b>115 转存成功！</b>\n\n📤 结果：%s", html.EscapeString(result))
}

// StatusPanel returns a rich /start message with user status.
func StatusPanel(userID int64, isAdmin bool, authorized bool, has115 bool, version string) string {
	var b strings.Builder
	if version != "" {
		fmt.Fprintf(&b, "🎬 <b>HDHive Bot</b> <i>%s</i>\n\n", html.EscapeString(version))
	} else {
		b.WriteString("🎬 <b>HDHive Bot</b>\n\n")
	}

	// 用户状态
	b.WriteString("👤 <b>用户状态</b>\n")
	fmt.Fprintf(&b, "   ID: <code>%d</code>\n", userID)
	if isAdmin {
		b.WriteString("   角色: 🛡 管理员\n")
	} else if authorized {
		b.WriteString("   授权: ✅ 已授权\n")
	} else {
		b.WriteString("   授权: ❌ 未授权\n")
	}

	// 115 配置
	b.WriteString("\n🍪 <b>115 配置</b>\n")
	if has115 {
		b.WriteString("   状态: ✅ 已配置\n")
	} else {
		b.WriteString("   状态: ❌ 未配置\n")
	}

	b.WriteString("\n━━━━━━━━━━━━━━━━━\n\n")

	// 指令列表
	b.WriteString("📌 <b>基础指令</b>\n")
	b.WriteString("<code>/myid</code>    — 查看你的 Telegram ID\n")
	b.WriteString("<code>/set115</code>  — 配置 115 网盘 Cookie\n")
	b.WriteString("<code>/unset115</code> — 删除 115 配置\n")
	b.WriteString("<code>/my115</code>   — 查看配置状态\n")

	if isAdmin {
		b.WriteString("\n🔧 <b>管理员指令</b>\n")
		b.WriteString("<code>/authorize &lt;id&gt;</code> — 授权用户\n")
		b.WriteString("<code>/revoke &lt;id&gt;</code>    — 撤销授权\n")
		b.WriteString("<code>/users</code>             — 用户列表\n")
		b.WriteString("<code>/note &lt;id&gt; &lt;备注&gt;</code> — 设置备注\n")
		b.WriteString("<code>/logs</code>              — 活动日志\n")
	}

	b.WriteString("\n━━━━━━━━━━━━━━━━━\n\n")
	b.WriteString("💡 <i>直接发送影视关键词即可搜索</i>")
	return b.String()
}

// FormatSearchButtonText returns button text for TMDB search results with year.
func FormatSearchButtonText(item TMDBItem) string {
	title := displayTitle(item)
	year := ""
	if len(item.ReleaseDate) >= 4 {
		year = item.ReleaseDate[:4]
	}
	if year != "" {
		return fmt.Sprintf("🎬 %s (%s)", title, year)
	}
	return fmt.Sprintf("🎬 %s", title)
}

// FormatResourceButtonText returns button text for resource list with details.
func FormatResourceButtonText(r Resource) string {
	parts := []string{r.Title}
	if r.Quality != "" {
		parts = append(parts, r.Quality)
	}
	if r.Unlocked {
		parts = append(parts, "✅")
	}
	return "📂 " + strings.Join(parts, " · ")
}

// FormatTransferFailed returns a specific error message for transfer failures.
func FormatTransferFailed(err error) string {
	errMsg := err.Error()
	switch {
	case strings.Contains(errMsg, "登录") || strings.Contains(errMsg, "cookie") || strings.Contains(errMsg, "Cookie"):
		return "❌ <b>115 转存失败</b>\n\n🍪 Cookie 已过期或无效。\n\n请使用 <code>/set115</code> 重新配置。"
	case strings.Contains(errMsg, "已接收") || strings.Contains(errMsg, "already"):
		return "ℹ️ <b>该资源之前已接收过</b>\n\n未重复转存。"
	case strings.Contains(errMsg, "提取码") || strings.Contains(errMsg, "password"):
		return "❌ <b>115 转存失败</b>\n\n🔑 提取码错误或分享已失效。"
	case strings.Contains(errMsg, "网络") || strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "超时"):
		return "❌ <b>115 转存失败</b>\n\n🌐 网络超时，请稍后重试。"
	default:
		return fmt.Sprintf("❌ <b>115 转存失败</b>\n\n%s", html.EscapeString(errMsg))
	}
}
