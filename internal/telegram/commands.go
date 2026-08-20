package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/atpx4869/hdhive_bot_go/internal/store"
)

func (h *Handler) receive115Cookie(ctx context.Context, userID, chatID int64, messageID int, raw string) error {
	cookie := normalize115Cookie(raw)
	deleted := messageID > 0 && h.messenger.DeleteMessage(ctx, chatID, messageID) == nil
	if !valid115Cookie(cookie) {
		warning := ""
		if !deleted {
			warning = "\nBot 无法自动删除原消息，请立即手动删除。"
		}
		return h.send(ctx, chatID, "❌ Cookie 格式不正确\n\n应包含 <code>UID</code>、<code>CID</code>、<code>SEID</code>，请重新发送。"+warning)
	}
	if err := h.sessions.Set(userID, "set115_cid", map[string]string{"cookie": cookie}); err != nil {
		return err
	}
	warning := ""
	if !deleted {
		warning = "\n⚠️ Bot 无法自动删除 Cookie 消息，请立即手动删除。"
	}
	return h.send(ctx, chatID, "✅ Cookie 格式已确认\n\n请发送目标目录 <code>cid</code>\n发送 <code>0</code> 表示根目录"+warning)
}

func (h *Handler) receive115CID(ctx context.Context, userID, chatID int64, targetCID, cookie string) error {
	targetCID = strings.TrimSpace(targetCID)
	if targetCID == "" || strings.Trim(targetCID, "0123456789") != "" {
		return h.send(ctx, chatID, "⚠️ 目标目录 cid 必须是数字\n\n根目录请输入 <code>0</code>")
	}
	if h.services.Accounts == nil {
		return h.send(ctx, chatID, "115 服务未配置。")
	}
	if err := h.services.Accounts.SetP115Config(ctx, userID, store.P115Config{Cookie: cookie, TargetCID: targetCID, Enabled: true}); err != nil {
		return err
	}
	h.sessions.ClearInteraction(userID)
	h.log(ctx, userID, "set115", map[bool]string{true: "root", false: "configured"}[targetCID == "0"])
	return h.send(ctx, chatID, "✅ 115 配置已加密保存。")
}

func (h *Handler) change115CID(ctx context.Context, userID, chatID int64, targetCID string) error {
	targetCID = strings.TrimSpace(targetCID)
	if targetCID == "" || strings.Trim(targetCID, "0123456789") != "" {
		return h.send(ctx, chatID, "⚠️ 目标目录 cid 必须是数字\n\n根目录请输入 <code>0</code>")
	}
	if h.services.Accounts == nil {
		return h.send(ctx, chatID, "115 服务未配置。")
	}
	cfg, err := h.services.Accounts.GetP115Config(ctx, userID)
	if err != nil {
		return h.send(ctx, chatID, "⚠️ 你还没有配置 115\n\n发送 <code>/set115</code> 开始配置。")
	}
	cfg.TargetCID = targetCID
	if err := h.services.Accounts.SetP115Config(ctx, userID, cfg); err != nil {
		return err
	}
	h.sessions.ClearInteraction(userID)
	target := "根目录"
	if targetCID != "0" {
		target = fmt.Sprintf("目录 %s", targetCID)
	}
	h.log(ctx, userID, "change115cid", targetCID)
	return h.send(ctx, chatID, fmt.Sprintf("✅ 115 转存目标已更新为：%s", target))
}

func normalize115Cookie(cookie string) string {
	parts := strings.Split(strings.TrimSpace(cookie), ";")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			clean = append(clean, value)
		}
	}
	return strings.Join(clean, ";")
}

func valid115Cookie(cookie string) bool {
	keys := map[string]bool{}
	for _, part := range strings.Split(cookie, ";") {
		pair := strings.SplitN(part, "=", 2)
		if len(pair) == 2 {
			keys[strings.ToUpper(strings.TrimSpace(pair[0]))] = true
		}
	}
	return keys["UID"] && keys["CID"] && keys["SEID"]
}
