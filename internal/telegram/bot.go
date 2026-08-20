package telegram

import (
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
	params := &gbot.SendMessageParams{ChatID: chatID, Text: out.Text, ParseMode: "HTML"}
	if out.RemoveKeyboard {
		// 隐藏键盘
		params.ReplyMarkup = models.ReplyKeyboardRemove{RemoveKeyboard: true}
	} else if out.ReplyKeyboard {
		// 底部常驻键盘
		params.ReplyMarkup = models.ReplyKeyboardMarkup{
			Keyboard: [][]models.KeyboardButton{
				{{Text: "🔍 搜索"}, {Text: "🆔 我的ID"}},
				{{Text: "🍪 115配置"}, {Text: "📋 115状态"}},
				{{Text: "❌ 取消"}},
			},
			ResizeKeyboard:        true,
			IsPersistent:          true,
			InputFieldPlaceholder: "输入影视关键词搜索...",
		}
	} else if len(out.Buttons) > 0 {
		// 内联键盘
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
	_, err := m.Bot.DeleteMessage(ctx, &gbot.DeleteMessageParams{ChatID: chatID, MessageID: messageID})
	return err
}

// UpdateHandler returns a go-telegram/bot default handler.
func (h *Handler) UpdateHandler() gbot.HandlerFunc {
	return func(ctx context.Context, _ *gbot.Bot, update *models.Update) {
		if update.Message != nil && update.Message.From != nil {
			if err := h.HandleText(ctx, update.Message.From.ID, update.Message.Chat.ID, update.Message.Text, update.Message.ID); err != nil {
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

func (h *Handler) reportError(ctx context.Context, chatID int64, err error) {
	if messenger, ok := h.messenger.(BotMessenger); ok && messenger.Logger != nil {
		messenger.Logger.ErrorContext(ctx, "telegram handler failed", "error_type", fmt.Sprintf("%T", err))
	}
	_ = h.messenger.Send(ctx, chatID, Outgoing{Text: "❌ 操作暂时失败，请稍后重试。"})
}
