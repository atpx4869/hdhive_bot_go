package telegram

import (
	"fmt"
	"strings"

	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

// ──────────────────────── Status Panel ────────────────────────

func StatusPanel(userID int64, admin, authorized bool, p115Enabled bool, p115Target string) string {
	var b strings.Builder
	b.WriteString("<b>🎬 HDHive 影视搜索</b>\n\n")

	// Identity
	if admin {
		b.WriteString("👤 身份：<b>管理员</b>\n")
	} else if authorized {
		b.WriteString("👤 身份：已授权用户\n")
	} else {
		b.WriteString("👤 身份：<i>未授权</i>\n")
	}

	// 115 status
	if p115Enabled {
		target := "根目录"
		if p115Target != "" && p115Target != "0" {
			target = "目录 <code>" + p115Target + "</code>"
		}
		fmt.Fprintf(&b, "☁️ 115：✅ 已启用 · %s\n", target)
	} else {
		b.WriteString("☁️ 115：⚪ 未配置\n")
	}

	b.WriteString("\n📖 直接发送影视名称即可搜索\n")
	b.WriteString("───────────────\n")
	b.WriteString("<b>📋 用户命令</b>\n")
	b.WriteString("<code>/myid</code>     查看 ID\n")
	b.WriteString("<code>/set115</code>   配置 115\n")
	b.WriteString("<code>/my115</code>    查看配置\n")
	b.WriteString("<code>/unset115</code> 停用 115\n")

	if admin {
		b.WriteString("\n<b>👑 管理命令</b>\n")
		b.WriteString("<code>/authorize</code>  授权\n")
		b.WriteString("<code>/revoke</code>     撤销\n")
		b.WriteString("<code>/users</code>      用户列表\n")
		b.WriteString("<code>/note</code>       备注\n")
		b.WriteString("<code>/logs</code>       日志\n")
		b.WriteString("<code>/export</code>     导出\n")
		b.WriteString("<code>/import</code>     导入\n")
		b.WriteString("<code>/unknown</code>    unknown 记录\n")
	}

	return strings.TrimSpace(b.String())
}

// ──────────────────────── TMDB Search ────────────────────────

func FormatTMDB(items []TMDBItem, page, totalPages int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>🔍 搜索结果</b>  <i>%d / %d</i>\n", page, max(totalPages, 1))
	b.WriteString("───────────────\n")
	for i, item := range items {
		title := displayTitle(item)
		year := ""
		if len(item.ReleaseDate) >= 4 {
			year = item.ReleaseDate[:4]
		}
		fmt.Fprintf(&b, "<b>%d.</b> %s", i+1, title)
		if item.VoteAverage > 0 {
			fmt.Fprintf(&b, "  ⭐ %.1f", item.VoteAverage)
		}
		if item.MediaType == "tv" {
			b.WriteString("  📺")
		} else {
			b.WriteString("  🎥")
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// ──────────────────────── HDHive Resources ────────────────────────

func FormatResources(page ResourcePage, mediaTitle string) string {
	if len(page.Items) == 0 {
		return "🔍 HDHive 暂无匹配资源\n\n换个关键词试试？"
	}
	var b strings.Builder
	if mediaTitle != "" {
		fmt.Fprintf(&b, "<b>🎬 %s</b>\n", mediaTitle)
	}
	fmt.Fprintf(&b, "📦 <b>HDHive 资源</b>  <i>%d / %d</i>\n", page.Page, max(page.TotalPages, 1))
	b.WriteString("───────────────\n")
	for _, r := range page.Items {
		b.WriteString("• ")
		b.WriteString(r.Title)
		details := []string{}
		if r.Quality != "" {
			details = append(details, r.Quality)
		}
		if r.Size != "" {
			details = append(details, r.Size)
		}
		if len(details) > 0 {
			fmt.Fprintf(&b, "  <i>%s</i>", strings.Join(details, " · "))
		}
		if r.Subtitle != "" {
			fmt.Fprintf(&b, "\n  🌐 %s", r.Subtitle)
		}
		if r.Source != "" {
			fmt.Fprintf(&b, "  💿 %s", r.Source)
		}
		if r.FeeKnown && r.Fee == 0 {
			b.WriteString("  🆓")
		} else if r.FeeKnown && r.Fee > 0 {
			fmt.Fprintf(&b, "  💰 %d", r.Fee)
		}
		if r.Unlocked {
			b.WriteString("  ✅")
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// ──────────────────────── Resource Detail ────────────────────────

func FormatResource(r Resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>📋 %s</b>\n", r.Title)
	b.WriteString("───────────────\n")
	if r.Quality != "" {
		fmt.Fprintf(&b, "📐 画质：%s\n", r.Quality)
	}
	if r.Size != "" {
		fmt.Fprintf(&b, "💾 大小：%s\n", r.Size)
	}
	if r.Subtitle != "" {
		fmt.Fprintf(&b, "🌐 字幕：%s\n", r.Subtitle)
	}
	if r.Source != "" {
		fmt.Fprintf(&b, "💿 来源：%s\n", r.Source)
	}
	if r.FeeKnown {
		if r.Fee == 0 {
			b.WriteString("💰 费用：<b>免费</b> 🆓\n")
		} else {
			fmt.Fprintf(&b, "💰 费用：<b>%d 积分</b>\n", r.Fee)
		}
	} else {
		b.WriteString("💰 费用：<i>未知</i>\n")
	}
	if r.Unlocked {
		b.WriteString("\n✅ <b>已解锁</b>\n")
	}
	if r.Description != "" {
		fmt.Fprintf(&b, "\n%s", r.Description)
	}
	return strings.TrimSpace(b.String())
}

// ──────────────────────── Users Page ────────────────────────

func FormatUsersPage(users []store.User, page int, hasMore bool) string {
	if len(users) == 0 {
		return "👥 暂无用户"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<b>👥 用户列表</b>  <i>第 %d 页</i>\n", page)
	b.WriteString("───────────────\n")
	for _, u := range users {
		icon := "⚪"
		status := "未授权"
		if u.Authorized {
			icon = "🟢"
			status = "已授权"
		}
		// 昵称优先，无昵称显示 ID
		name := userDisplayName(u.ID, u.Note)
		fmt.Fprintf(&b, "%s %s  %s", icon, name, status)
		if u.Note != "" {
			fmt.Fprintf(&b, "  <i>(%d)</i>", u.ID)
		}
		b.WriteByte('\n')
	}
	if hasMore {
		b.WriteString("\n<i>还有更多…</i>")
	}
	return strings.TrimSpace(b.String())
}

// userDisplayName 返回用户显示名：有昵称用昵称，无昵称用 ID
func userDisplayName(id int64, note string) string {
	if note != "" {
		return note
	}
	return fmt.Sprintf("<code>%d</code>", id)
}

// ──────────────────────── Logs Page ────────────────────────

func FormatLogsPage(logs []store.ActivityLog, page int, hasMore bool, userNames map[int64]string) string {
	if len(logs) == 0 {
		return "📋 暂无活动日志"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<b>📋 活动日志</b>  <i>第 %d 页</i>\n", page)
	b.WriteString("───────────────\n")
	for _, l := range logs {
		icon := actionIcon(l.Action)
		// 昵称优先，无昵称显示 ID
		name := userDisplayName(l.UserID, userNames[l.UserID])
		fmt.Fprintf(&b, "%s %s  %s", icon, name, l.Action)
		if l.Status != "" {
			fmt.Fprintf(&b, "  [%s]", l.Status)
		}
		fmt.Fprintf(&b, "  <i>%s</i>\n", l.CreatedAt.Local().Format("01-02 15:04"))
		if l.MediaTitle != "" {
			fmt.Fprintf(&b, "   🎬 %s", l.MediaTitle)
			if l.ResourceTitle != "" {
				fmt.Fprintf(&b, "  ·  %s", l.ResourceTitle)
			}
			b.WriteByte('\n')
		}
		if l.ErrorCode != "" {
			fmt.Fprintf(&b, "   ⚠️ <code>%s</code>\n", l.ErrorCode)
		}
	}
	if hasMore {
		b.WriteString("\n<i>还有更多…</i>")
	}
	return strings.TrimSpace(b.String())
}

func actionIcon(action string) string {
	switch action {
	case "search":
		return "🔍"
	case "unlock":
		return "🔓"
	case "transfer":
		return "📤"
	case "set115", "change115cid":
		return "⚙️"
	case "authorize":
		return "✅"
	case "revoke":
		return "🚫"
	case "import":
		return "📥"
	case "export":
		return "📤"
	case "unlockreset":
		return "🔧"
	case "resource_search":
		return "📦"
	default:
		return "📝"
	}
}

// ──────────────────────── Legacy (unused but kept for compatibility) ────────────────────────

func HelpText(admin bool) string {
	return StatusPanel(0, admin, true, false, "")
}

func MaskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "未配置"
	}
	if len(secret) <= 8 {
		return strings.Repeat("*", len(secret))
	}
	return secret[:4] + strings.Repeat("*", len(secret)-8) + secret[len(secret)-4:]
}

func FormatUsers(users []store.User) string {
	if len(users) == 0 {
		return "暂无用户。"
	}
	var b strings.Builder
	b.WriteString("用户列表：\n")
	for _, u := range users {
		status := "未授权"
		if u.Authorized {
			status = "已授权"
		}
		fmt.Fprintf(&b, "• %d｜%s", u.ID, status)
		if u.Note != "" {
			fmt.Fprintf(&b, "｜%s", u.Note)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func FormatLogs(logs []store.ActivityLog) string {
	if len(logs) == 0 {
		return "暂无日志。"
	}
	var b strings.Builder
	b.WriteString("最近活动：\n")
	for _, l := range logs {
		fmt.Fprintf(&b, "• %s｜%d｜%s｜%s\n", l.CreatedAt.Local().Format("01-02 15:04"), l.UserID, l.Action, l.Detail)
	}
	return strings.TrimSpace(b.String())
}
