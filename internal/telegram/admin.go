package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

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
		// Invalidate auth cache for this user
		h.invalidateAuthCache(id)
		h.log(ctx, actor, strings.TrimPrefix(cmd, "/"), fmt.Sprintf("user=%d %s", id, rest))
		icon := "✅"
		if !authorized {
			icon = "🚫"
		}
		return h.send(ctx, chatID, fmt.Sprintf("%s 用户 <code>%d</code> 已%s授权。", icon, id, map[bool]string{true: "获得", false: "撤销"}[authorized]))
	case "/note":
		id, note, err := parseIDArg(arg)
		if err != nil || note == "" {
			return h.send(ctx, chatID, "用法：/note <user_id> <备注>")
		}
		if err := h.services.Users.SetUserNote(ctx, id, note); err != nil {
			return err
		}
		return h.send(ctx, chatID, "✅ 备注已更新。")
	case "/users":
		page := 1
		if arg != "" {
			fmt.Sscanf(arg, "%d", &page)
		}
		if page < 1 {
			page = 1
		}
		const pageSize = 20
		offset := (page - 1) * pageSize
		users, err := h.services.Users.ListUsers(ctx, pageSize+1, offset)
		if err != nil {
			return err
		}
		hasMore := len(users) > pageSize
		if hasMore {
			users = users[:pageSize]
		}
		out := Outgoing{Text: FormatUsersPage(users, page, hasMore)}
		var buttons []Button
		if page > 1 {
			prevToken, _ := h.sessions.BindCallback(actor, "admin_users", fmt.Sprintf("%d", page-1))
			buttons = append(buttons, Button{Text: "⬅️ 上一页", CallbackData: prevToken})
		}
		if hasMore {
			nextToken, _ := h.sessions.BindCallback(actor, "admin_users", fmt.Sprintf("%d", page+1))
			buttons = append(buttons, Button{Text: "下一页 ➡️", CallbackData: nextToken})
		}
		if len(buttons) > 0 {
			out.Buttons = [][]Button{buttons}
		}
		return h.messenger.Send(ctx, chatID, out)
	case "/logs":
		if h.services.Logs == nil {
			return h.send(ctx, chatID, "日志服务未配置。")
		}
		page := 1
		if arg != "" {
			fmt.Sscanf(arg, "%d", &page)
		}
		if page < 1 {
			page = 1
		}
		const logPageSize = 20
		logs, err := h.services.Logs.QueryActivityLogs(ctx, store.ActivityQuery{Limit: logPageSize + 1, Offset: (page - 1) * logPageSize})
		if err != nil {
			return err
		}
		hasMore := len(logs) > logPageSize
		if hasMore {
			logs = logs[:logPageSize]
		}
		// 构建用户昵称映射
		userNames := h.buildUserNames(ctx)
		out := Outgoing{Text: FormatLogsPage(logs, page, hasMore, userNames)}
		var buttons []Button
		if page > 1 {
			prevToken, _ := h.sessions.BindCallback(actor, "admin_logs", fmt.Sprintf("%d", page-1))
			buttons = append(buttons, Button{Text: "⬅️ 上一页", CallbackData: prevToken})
		}
		if hasMore {
			nextToken, _ := h.sessions.BindCallback(actor, "admin_logs", fmt.Sprintf("%d", page+1))
			buttons = append(buttons, Button{Text: "下一页 ➡️", CallbackData: nextToken})
		}
		if len(buttons) > 0 {
			out.Buttons = [][]Button{buttons}
		}
		return h.messenger.Send(ctx, chatID, out)
	case "/unlockreset":
		var userID int64
		var resourceID string
		if _, err := fmt.Sscanf(arg, "%d %s", &userID, &resourceID); err != nil || userID <= 0 || strings.TrimSpace(resourceID) == "" {
			return h.send(ctx, chatID, "用法：/unlockreset <user_id> <resource_id>")
		}
		admin, ok := h.services.HDHive.(UnlockAdminService)
		if !ok {
			return h.send(ctx, chatID, "解锁恢复服务未配置。")
		}
		if err := admin.ResetUnlockRecord(ctx, userID, resourceID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return h.send(ctx, chatID, "只有 unknown 状态可以解除；记录可能仍在 in_flight、已经成功或不存在。")
			}
			return err
		}
		h.sessions.ResetUnlock(userID, resourceID)
		h.log(ctx, actor, "unlockreset", fmt.Sprintf("user=%d resource=%s", userID, resourceID))
		return h.send(ctx, chatID, "✅ 解锁状态已解除\n\n请先人工核验是否已扣费，再让用户重新操作。")
	case "/enable115", "/disable115":
		if h.services.Accounts == nil {
			return h.send(ctx, chatID, "115 服务未配置。")
		}
		var targetID int64
		if _, err := fmt.Sscanf(arg, "%d", &targetID); err != nil || targetID <= 0 {
			return h.send(ctx, chatID, "用法："+cmd+" <user_id>")
		}
		enable := cmd == "/enable115"
		cfg, err := h.services.Accounts.GetP115Config(ctx, targetID)
		if err != nil {
			return h.send(ctx, chatID, fmt.Sprintf("用户 %d 没有 115 配置。", targetID))
		}
		if cfg.Enabled == enable {
			status := "已启用"
			if !enable {
				status = "已停用"
			}
			return h.send(ctx, chatID, fmt.Sprintf("用户 %d 的 115 配置%s。", targetID, status))
		}
		cfg.Enabled = enable
		if err := h.services.Accounts.SetP115Config(ctx, targetID, cfg); err != nil {
			return err
		}
		action := "启用"
		if !enable {
			action = "停用"
		}
		h.log(ctx, actor, cmd[1:], fmt.Sprintf("user=%d", targetID))
		return h.send(ctx, chatID, fmt.Sprintf("✅ 已%s用户 <code>%d</code> 的 115 配置。", action, targetID))
	case "/unknown":
		if h.services.HDHive == nil {
			return h.send(ctx, chatID, "HDHive 服务未配置。")
		}
		getter, ok := h.services.HDHive.(interface {
			GetUnknownUnlockRecords(context.Context, int) ([]store.UnlockRecordWithUser, error)
		})
		if !ok {
			return h.send(ctx, chatID, "查询服务不可用。")
		}
		records, err := getter.GetUnknownUnlockRecords(ctx, 50)
		if err != nil {
			return h.send(ctx, chatID, fmt.Sprintf("查询失败：%s", err.Error()))
		}
		if len(records) == 0 {
			return h.send(ctx, chatID, "✅ 当前没有 unknown 状态的解锁记录。")
		}
		userNames := h.buildUserNames(ctx)
		var b strings.Builder
		b.WriteString("⚠️ <b>Unknown 解锁记录</b>\n\n")
		for _, r := range records {
			name := userDisplayName(r.UserID, userNames[r.UserID])
			fmt.Fprintf(&b, "• %s / 资源 <code>%s</code>\n", name, r.ResourceID)
			fmt.Fprintf(&b, "  更新时间：%s\n", r.UpdatedAt.Local().Format("01-02 15:04"))
		}
		b.WriteString("\n使用 /unlockreset &lt;user_id&gt; &lt;resource_id&gt; 解除")
		return h.send(ctx, chatID, b.String())
	case "/export":
		return h.exportData(ctx, actor, chatID)
	case "/import":
		h.sessions.SetImportMode(actor)
		return h.send(ctx, chatID, "📥 请发送 JSON 文件进行导入。\n\n支持格式：\n• Go 版本导出的数据\n• Python 版本导出的数据\n\n可导入内容：\n• 用户列表（授权状态、备注）\n• 115 配置（Cookie、目标目录）")
	}
	return nil
}

// HandleDocument handles document messages for data import.
func (h *Handler) HandleDocument(ctx context.Context, userID, chatID int64, fileName string, fileData []byte) error {
	if !h.sessions.IsImportMode(userID) {
		return nil
	}
	h.sessions.ClearImportMode(userID)

	if !h.isAdmin(userID) {
		return h.send(ctx, chatID, "无权限：仅管理员可使用导入功能。")
	}

	if !strings.HasSuffix(strings.ToLower(fileName), ".json") {
		return h.send(ctx, chatID, "只支持 JSON 文件。")
	}

	var data map[string]any
	if err := json.Unmarshal(fileData, &data); err != nil {
		return h.send(ctx, chatID, fmt.Sprintf("JSON 解析失败：%s", err.Error()))
	}

	source, _ := data["source"].(string)
	imported := 0
	skipped := 0
	importErrors := []string{}

	// Import users
	if users, ok := data["users"]; ok {
		switch u := users.(type) {
		case map[string]any: // Python format
			for uidStr, userData := range u {
				var uid int64
				fmt.Sscanf(uidStr, "%d", &uid)
				if uid <= 0 {
					continue
				}
				userMap, ok := userData.(map[string]any)
				if !ok {
					continue
				}
				authorized, _ := userMap["authorized"].(bool)
				note, _ := userMap["note"].(string)
				if err := h.services.Users.SetUserAuthorization(ctx, uid, authorized); err != nil {
					importErrors = append(importErrors, fmt.Sprintf("用户 %d: %v", uid, err))
					continue
				}
				if note != "" {
					h.services.Users.SetUserNote(ctx, uid, note)
				}
				imported++
			}
		case []any: // Go format
			for _, userData := range u {
				userMap, ok := userData.(map[string]any)
				if !ok {
					continue
				}
				uid, _ := userMap["id"].(float64)
				if uid <= 0 {
					continue
				}
				authorized, _ := userMap["authorized"].(bool)
				note, _ := userMap["note"].(string)
				if err := h.services.Users.SetUserAuthorization(ctx, int64(uid), authorized); err != nil {
					importErrors = append(importErrors, fmt.Sprintf("用户 %d: %v", int64(uid), err))
					continue
				}
				if note != "" {
					h.services.Users.SetUserNote(ctx, int64(uid), note)
				}
				imported++
			}
		}
	}

	// Import 115 accounts
	if accounts, ok := data["p115_accounts"]; ok {
		if accMap, ok := accounts.(map[string]any); ok {
			for uidStr, accData := range accMap {
				var uid int64
				fmt.Sscanf(uidStr, "%d", &uid)
				if uid <= 0 {
					continue
				}
				accMap, ok := accData.(map[string]any)
				if !ok {
					continue
				}
				cookie, _ := accMap["cookie"].(string)
				targetCID, _ := accMap["target_cid"].(string)
				enabled, _ := accMap["enabled"].(bool)
				if cookie == "" {
					continue
				}
				if strings.Contains(cookie, "=***") {
					skipped++
					continue
				}
				if h.services.Accounts != nil {
					if err := h.services.Accounts.SetP115Config(ctx, uid, store.P115Config{
						Cookie:    cookie,
						TargetCID: targetCID,
						Enabled:   enabled,
					}); err != nil {
						importErrors = append(importErrors, fmt.Sprintf("115 配置 %d: %v", uid, err))
						continue
					}
				}
			}
		}
	}

	// Note activity_logs are not imported
	if _, hasLogs := data["activity_logs"]; hasLogs {
		skipped++ // count as skipped for reporting
	}

	var result strings.Builder
	result.WriteString("📥 <b>导入完成</b>\n\n")
	result.WriteString(fmt.Sprintf("• 数据源：%s\n", source))
	result.WriteString(fmt.Sprintf("• 成功导入：%d 个用户\n", imported))
	if skipped > 0 {
		result.WriteString(fmt.Sprintf("• 跳过：%d 个\n", skipped))
	}
	if len(importErrors) > 0 {
		result.WriteString(fmt.Sprintf("\n⚠️ 错误：%d 个\n", len(importErrors)))
		for i, e := range importErrors {
			if i >= 5 {
				result.WriteString("...\n")
				break
			}
			result.WriteString(fmt.Sprintf("  • %s\n", e))
		}
	}
	result.WriteString("\n💡 活动日志未导入（仅限导出查看）。")

	h.log(ctx, userID, "import", fileName)
	return h.send(ctx, chatID, result.String())
}

func (h *Handler) exportData(ctx context.Context, userID, chatID int64) error {
	if h.services.Users == nil || h.services.Logs == nil {
		return h.send(ctx, chatID, "导出服务未配置。")
	}

	users, err := h.services.Users.ListUsers(ctx, 10000, 0)
	if err != nil {
		return h.send(ctx, chatID, fmt.Sprintf("获取用户失败：%s", err.Error()))
	}

	export := map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"version":     "1.0",
		"source":      "hdhive_bot_go",
	}

	userData := make([]map[string]any, 0, len(users))
	for _, u := range users {
		userData = append(userData, map[string]any{
			"id":         u.ID,
			"authorized": u.Authorized,
			"note":       u.Note,
			"is_admin":   h.isAdmin(u.ID),
		})
	}
	export["users"] = userData

	logs, err := h.services.Logs.QueryActivityLogs(ctx, store.ActivityQuery{Limit: 1000})
	if err == nil {
		logData := make([]map[string]any, 0, len(logs))
		for _, l := range logs {
			logData = append(logData, map[string]any{
				"timestamp":      l.CreatedAt.Format(time.RFC3339),
				"user_id":        l.UserID,
				"action":         l.Action,
				"status":         l.Status,
				"media_title":    l.MediaTitle,
				"resource_title": l.ResourceTitle,
				"detail":         l.Detail,
			})
		}
		export["activity_logs"] = logData
	}

	stats := map[string]any{
		"total_users": len(users),
		"admin_count": len(h.admins),
	}
	export["stats"] = stats

	jsonData, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return h.send(ctx, chatID, fmt.Sprintf("序列化失败：%s", err.Error()))
	}

	filename := fmt.Sprintf("hdhive_export_%s.json", time.Now().Format("20060102_150405"))
	caption := fmt.Sprintf("📊 数据导出完成\n\n• 用户：%d 人\n• 活动日志：%d 条", len(userData), len(logs))
	return h.messenger.SendDocument(ctx, chatID, filename, jsonData, caption)
}

// buildUserNames 构建 userID → 昵称映射表，用于日志和 unknown 列表显示
func (h *Handler) buildUserNames(ctx context.Context) map[int64]string {
	names := make(map[int64]string)
	if h.services.Users == nil {
		return names
	}
	users, err := h.services.Users.ListUsers(ctx, 5000, 0)
	if err != nil {
		return names
	}
	for _, u := range users {
		if u.Note != "" {
			names[u.ID] = u.Note
		}
	}
	return names
}
