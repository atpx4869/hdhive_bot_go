package telegram

import (
	"fmt"
	"strings"

	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

func HelpText(admin bool) string {
	commands := []string{
		"欢迎使用 HDHive Bot。",
		"直接发送影视关键词即可搜索。",
		"/myid - 查看 Telegram User ID",
		"/set115 - 分步配置 115 Cookie 和目标目录",
		"/cancel - 取消当前配置流程",
		"/unset115 - 确认后停用 115 配置",
		"/my115 - 查看配置状态和目标目录",
	}
	if admin {
		commands = append(commands,
			"/authorize <id> - 授权用户", "/revoke <id> - 撤销授权",
			"/users - 用户列表", "/note <id> <备注> - 设置备注", "/logs - 活动日志")
	}
	return strings.Join(commands, "\n")
}

// StatusPanel 生成用户状态面板
func StatusPanel(userID int64, admin, authorized bool, p115Enabled bool, p115Target string) string {
	var b strings.Builder
	b.WriteString("🎬 <b>HDHive 影视搜索</b>\n\n")

	// 状态信息
	b.WriteString("📊 <b>状态</b>\n")
	if admin {
		b.WriteString("• 身份：管理员\n")
	} else if authorized {
		b.WriteString("• 身份：已授权用户\n")
	} else {
		b.WriteString("• 身份：未授权\n")
	}

	// 115 配置状态
	if p115Enabled {
		target := "根目录"
		if p115Target != "" && p115Target != "0" {
			target = "已配置目录"
		}
		fmt.Fprintf(&b, "• 115：已启用 · %s\n", target)
	} else {
		b.WriteString("• 115：未配置\n")
	}
	b.WriteString("\n")

	// 使用说明
	b.WriteString("📖 <b>使用方法</b>\n")
	b.WriteString("直接发送电影或剧集名称即可搜索。\n\n")

	// 命令列表
	b.WriteString("⚙️ <b>可用命令</b>\n")
	b.WriteString("/myid - 查看 Telegram User ID\n")
	b.WriteString("/set115 - 配置 115 账号\n")
	b.WriteString("/my115 - 查看 115 配置状态\n")
	b.WriteString("/unset115 - 停用 115 配置\n")

	if admin {
		b.WriteString("\n👑 <b>管理员命令</b>\n")
		b.WriteString("/authorize <id> - 授权用户\n")
		b.WriteString("/revoke <id> - 撤销授权\n")
		b.WriteString("/users - 用户列表\n")
		b.WriteString("/note <id> <备注> - 设置备注\n")
		b.WriteString("/logs - 活动日志\n")
	}

	return strings.TrimSpace(b.String())
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

func FormatUsersPage(users []store.User, page int, hasMore bool) string {
	if len(users) == 0 {
		return "暂无用户。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "👥 <b>用户列表</b>（第 %d 页）\n\n", page)
	for _, u := range users {
		status := "未授权"
		if u.Authorized {
			status = "已授权"
		}
		fmt.Fprintf(&b, "• <code>%d</code>｜%s", u.ID, status)
		if u.Note != "" {
			fmt.Fprintf(&b, "｜%s", u.Note)
		}
		b.WriteByte('\n')
	}
	if hasMore {
		b.WriteString("\n还有更多用户...")
	}
	return strings.TrimSpace(b.String())
}

func FormatLogsPage(logs []store.ActivityLog, page int, hasMore bool) string {
	if len(logs) == 0 {
		return "暂无日志。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📋 <b>最近活动</b>（第 %d 页）\n\n", page)
	for _, l := range logs {
		fmt.Fprintf(&b, "• %s｜<code>%d</code>｜%s\n", l.CreatedAt.Local().Format("01-02 15:04"), l.UserID, l.Action)
		if l.Detail != "" {
			fmt.Fprintf(&b, "  %s\n", l.Detail)
		}
	}
	if hasMore {
		b.WriteString("\n还有更多日志...")
	}
	return strings.TrimSpace(b.String())
}

func FormatTMDB(items []TMDBItem, page, totalPages int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "TMDB 搜索结果（%d/%d）：\n", page, max(totalPages, 1))
	for i, item := range items {
		title := displayTitle(item)
		year := item.ReleaseDate
		if len(year) >= 4 {
			year = year[:4]
		}
		fmt.Fprintf(&b, "%d. %s", i+1, title)
		if year != "" {
			fmt.Fprintf(&b, " (%s)", year)
		}
		if item.VoteAverage > 0 {
			fmt.Fprintf(&b, " ⭐ %.1f", item.VoteAverage)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func FormatResources(page ResourcePage) string {
	if len(page.Items) == 0 {
		return "HDHive 暂无匹配资源。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "HDHive 资源（%d/%d）：\n", page.Page, max(page.TotalPages, 1))
	for _, r := range page.Items {
		fmt.Fprintf(&b, "• %s", r.Title)
		if r.Quality != "" {
			fmt.Fprintf(&b, "｜%s", r.Quality)
		}
		if r.Size != "" {
			fmt.Fprintf(&b, "｜%s", r.Size)
		}
		if r.Subtitle != "" {
			fmt.Fprintf(&b, "｜🌐 %s", r.Subtitle)
		}
		if r.Source != "" {
			fmt.Fprintf(&b, "｜💿 %s", r.Source)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func FormatResource(r Resource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "资源：%s\n", r.Title)
	if r.Quality != "" {
		fmt.Fprintf(&b, "画质：%s\n", r.Quality)
	}
	if r.Size != "" {
		fmt.Fprintf(&b, "大小：%s\n", r.Size)
	}
	if r.Subtitle != "" {
		fmt.Fprintf(&b, "字幕：%s\n", r.Subtitle)
	}
	if r.Source != "" {
		fmt.Fprintf(&b, "来源：%s\n", r.Source)
	}
	if r.FeeKnown {
		fmt.Fprintf(&b, "费用：%d\n", r.Fee)
	} else {
		b.WriteString("费用：未知\n")
	}
	if r.Description != "" {
		b.WriteString(r.Description)
	}
	return strings.TrimSpace(b.String())
}
