package telegram

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

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
				h.handleDocumentMessage(ctx, bot, update.Message)
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

func (h *Handler) handleDocumentMessage(ctx context.Context, bot *gbot.Bot, message *models.Message) {
	if message.Document == nil || message.From == nil {
		return
	}

	// 检查文件大小（最大 10MB）
	if message.Document.FileSize > 10*1024*1024 {
		_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{Text: "文件太大，最大支持 10MB。"})
		return
	}

	// 提示用户使用命令行导入
	_ = h.messenger.Send(ctx, message.Chat.ID, Outgoing{
		Text: fmt.Sprintf("📥 收到文件：%s\n\n请将文件保存到服务器后使用命令行导入：\ngo run ./cmd/import --file /path/to/file.json", 
			message.Document.FileName),
	})
}

func (h *Handler) reportError(ctx context.Context, chatID int64, err error) {
	if messenger, ok := h.messenger.(BotMessenger); ok && messenger.Logger != nil {
		messenger.Logger.ErrorContext(ctx, "telegram handler failed", "error_type", fmt.Sprintf("%T", err))
	}
	_ = h.messenger.Send(ctx, chatID, Outgoing{Text: "操作暂时失败，请稍后重试。"})
}
