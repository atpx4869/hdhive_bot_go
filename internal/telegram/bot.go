package telegram

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	gbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// BotMessenger adapts github.com/go-telegram/bot while keeping handlers testable.
type BotMessenger struct {
	Bot    *gbot.Bot
	Logger *slog.Logger
}

func (m BotMessenger) Send(ctx context.Context, chatID int64, out Outgoing) error {
	params := &gbot.SendMessageParams{ChatID: chatID, Text: out.Text}
	if len(out.Buttons) > 0 {
		rows := make([][]models.InlineKeyboardButton, 0, len(out.Buttons))
		for _, row := range out.Buttons {
			buttons := make([]models.InlineKeyboardButton, 0, len(row))
			for _, button := range row {
				buttons = append(buttons, models.InlineKeyboardButton{Text: button.Text, CallbackData: button.CallbackData})
			}
			rows = append(rows, buttons)
		}
		params.ReplyMarkup = models.InlineKeyboardMarkup{InlineKeyboard: rows}
	}
	_, err := m.Bot.SendMessage(ctx, params)
	return err
}

func (m BotMessenger) AnswerCallback(ctx context.Context, id, text string) error {
	_, err := m.Bot.AnswerCallbackQuery(ctx, &gbot.AnswerCallbackQueryParams{CallbackQueryID: id, Text: text})
	return err
}

func (m BotMessenger) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	if messageID <= 0 {
		return fmt.Errorf("message ID is required")
	}
	_, err := m.Bot.DeleteMessage(ctx, &gbot.DeleteMessageParams{ChatID: chatID, MessageID: messageID})
	return err
}

func (m BotMessenger) SendDocument(ctx context.Context, chatID int64, filename string, data []byte, caption string) error {
	params := &gbot.SendDocumentParams{
		ChatID: chatID,
		Document: &models.InputFileUpload{
			Filename: filename,
			Data:     bytes.NewReader(data),
		},
		Caption: caption,
	}
	_, err := m.Bot.SendDocument(ctx, params)
	return err
}

// UpdateHandler returns a go-telegram/bot default handler.
func (h *Handler) UpdateHandler() gbot.HandlerFunc {
	return func(ctx context.Context, bot *gbot.Bot, update *models.Update) {
		if update.Message != nil && update.Message.From != nil {
			// 处理文档消息
			if update.Message.Document != nil {
				h.handleDocumentMessage(ctx, bot, update.Message, h.httpClient)
				return
			}
			if err := h.HandleMessage(ctx, update.Message.From.ID, update.Message.Chat.ID, update.Message.ID, update.Message.Text); err != nil {
				h.reportError(ctx, update.Message.Chat.ID, err)
			}
			return
		}
		if update.CallbackQuery != nil {
			chatID := update.CallbackQuery.From.ID
			if update.CallbackQuery.Message.Message != nil {
				chatID = update.CallbackQuery.Message.Message.Chat.ID
			}
			if err := h.HandleCallback(ctx, update.CallbackQuery.From.ID, chatID, update.CallbackQuery.ID, update.CallbackQuery.Data); err != nil {
				h.reportError(ctx, chatID, err)
			}
		}
	}
}

func (h *Handler) handleDocumentMessage(ctx context.Context, bot *gbot.Bot, message *models.Message, httpClient *http.Client) {
	if message.Document == nil || message.From == nil {
		return
	}

	// 只处理 JSON 文件
	if !strings.HasSuffix(strings.ToLower(message.Document.FileName), ".json") {
		_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{Text: "只支持 JSON 文件。"})
		return
	}

	// 检查文件大小（最大 10MB）
	if message.Document.FileSize > 10*1024*1024 {
		_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{Text: "文件太大，最大支持 10MB。"})
		return
	}

	// 检查是否在导入模式
	if !h.sessions.IsImportMode(message.From.ID) {
		return
	}
	h.sessions.ClearImportMode(message.From.ID)

	// 检查权限
	if !h.isAdmin(message.From.ID) {
		_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{Text: "无权限：仅管理员可使用导入功能。"})
		return
	}

	// 发送处理中提示
	_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{Text: "📥 正在处理文件..."})

	// 获取文件信息
	file, err := bot.GetFile(ctx, &gbot.GetFileParams{FileID: message.Document.FileID})
	if err != nil {
		_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{Text: "获取文件信息失败。"})
		return
	}

	// 构建下载 URL 并使用带代理的客户端下载
	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", bot.Token(), file.FilePath)
	resp, err := httpClient.Get(fileURL)
	if err != nil {
		_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{Text: fmt.Sprintf("下载文件失败：%v", err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{Text: fmt.Sprintf("下载文件失败：HTTP %d", resp.StatusCode)})
		return
	}

	fileData, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{Text: "读取文件内容失败。"})
		return
	}

	// 处理导入
	if err := h.HandleDocument(ctx, message.From.ID, message.Chat.ID, message.Document.FileName, fileData); err != nil {
		h.reportError(ctx, message.Chat.ID, err)
	}
}

func (h *Handler) reportError(ctx context.Context, chatID int64, err error) {
	if messenger, ok := h.messenger.(BotMessenger); ok && messenger.Logger != nil {
		messenger.Logger.ErrorContext(ctx, "telegram handler failed", "error_type", fmt.Sprintf("%T", err))
	}
	_ = h.messenger.Send(ctx, chatID, Outgoing{Text: "操作暂时失败，请稍后重试。"})
}
