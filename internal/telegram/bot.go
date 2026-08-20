package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	gbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// BotMessenger implements Messenger using go-telegram/bot.
type BotMessenger struct {
	Bot    *gbot.Bot
	Logger *slog.Logger
	HTTP   *http.Client // for downloading poster images
}

// ──────────────────────── Send (create new message) ────────────────────────

func (m BotMessenger) Send(ctx context.Context, chatID int64, view View) (MessageRef, error) {
	keyboard := buildInlineKeyboard(view.Buttons)

	if view.Media != nil && view.Media.URL != "" {
		// SendPhoto
		params := &gbot.SendPhotoParams{
			ChatID:      chatID,
			Caption:     view.Body,
			ParseMode:   "HTML",
			Photo:       &models.InputFileString{Data: view.Media.URL},
			ReplyMarkup: keyboard,
		}
		msg, err := m.Bot.SendPhoto(ctx, params)
		if err != nil {
			// Fallback: poster failed, send as text
			if m.Logger != nil {
				m.Logger.Warn("SendPhoto failed, falling back to text", "error", err)
			}
			return m.sendText(ctx, chatID, view.Body, keyboard)
		}
		return MessageRef{
			ChatID:    chatID,
			MessageID: msg.ID,
			HasMedia:  true,
			MediaURL:  view.Media.URL,
		}, nil
	}

	return m.sendText(ctx, chatID, view.Body, keyboard)
}

func (m BotMessenger) sendText(ctx context.Context, chatID int64, text string, keyboard any) (MessageRef, error) {
	params := &gbot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	}
	msg, err := m.Bot.SendMessage(ctx, params)
	if err != nil {
		return MessageRef{}, err
	}
	return MessageRef{ChatID: chatID, MessageID: msg.ID}, nil
}

// ──────────────────────── Render (edit existing message in-place) ────────────────────────

func (m BotMessenger) Render(ctx context.Context, current MessageRef, view View) (MessageRef, error) {
	hasNewMedia := view.Media != nil && view.Media.URL != ""
	keyboard := buildInlineKeyboard(view.Buttons)

	// Case 1: Both have media → EditMedia or EditCaption
	if current.HasMedia && hasNewMedia {
		if current.MediaURL != view.Media.URL {
			// Different poster → EditMessageMedia
			return m.editMedia(ctx, current, view, keyboard)
		}
		// Same poster → EditMessageCaption
		return m.editCaption(ctx, current, view, keyboard)
	}

	// Case 2: Current has media, new has no media → keep media, edit caption
	if current.HasMedia && !hasNewMedia {
		return m.editCaption(ctx, current, view, keyboard)
	}

	// Case 3: Current is text, new has media → upgrade to photo (EditMessageMedia)
	if !current.HasMedia && hasNewMedia {
		return m.editMediaWithFallback(ctx, current, view, keyboard)
	}

	// Case 4: Both text → EditMessageText
	return m.editText(ctx, current, view, keyboard)
}

func (m BotMessenger) editText(ctx context.Context, current MessageRef, view View, keyboard any) (MessageRef, error) {
	params := &gbot.EditMessageTextParams{
		ChatID:      current.ChatID,
		MessageID:   current.MessageID,
		Text:        view.Body,
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	}
	_, err := m.Bot.EditMessageText(ctx, params)
	if isNotModified(err) {
		return current, nil // treat as success
	}
	if err != nil {
		return current, fmt.Errorf("edit text: %w", err)
	}
	return current, nil
}

func (m BotMessenger) editCaption(ctx context.Context, current MessageRef, view View, keyboard any) (MessageRef, error) {
	params := &gbot.EditMessageCaptionParams{
		ChatID:      current.ChatID,
		MessageID:   current.MessageID,
		Caption:     view.Body,
		ParseMode:   "HTML",
		ReplyMarkup: keyboard,
	}
	_, err := m.Bot.EditMessageCaption(ctx, params)
	if isNotModified(err) {
		return current, nil
	}
	if err != nil {
		return current, fmt.Errorf("edit caption: %w", err)
	}
	return current, nil
}

func (m BotMessenger) editMedia(ctx context.Context, current MessageRef, view View, keyboard any) (MessageRef, error) {
	params := &gbot.EditMessageMediaParams{
		ChatID:    current.ChatID,
		MessageID: current.MessageID,
		Media: &models.InputMediaPhoto{
			Type:    "photo",
			Media:   view.Media.URL,
			Caption: view.Body,
		},
		ReplyMarkup: keyboard,
	}
	_, err := m.Bot.EditMessageMedia(ctx, params)
	if isNotModified(err) {
		return current, nil
	}
	if err != nil {
		return current, fmt.Errorf("edit media: %w", err)
	}
	return MessageRef{
		ChatID:    current.ChatID,
		MessageID: current.MessageID,
		HasMedia:  true,
		MediaURL:  view.Media.URL,
	}, nil
}

// editMediaWithFallback tries EditMessageMedia, falls back to editText on failure.
func (m BotMessenger) editMediaWithFallback(ctx context.Context, current MessageRef, view View, keyboard any) (MessageRef, error) {
	ref, err := m.editMedia(ctx, current, view, keyboard)
	if err != nil {
		// Poster download failed → fallback to text
		if m.Logger != nil {
			m.Logger.Warn("editMedia failed, falling back to text", "error", err)
		}
		return m.editText(ctx, current, View{Body: view.Body, Buttons: view.Buttons}, keyboard)
	}
	return ref, nil
}

// ──────────────────────── AnswerCallback ────────────────────────

func (m BotMessenger) AnswerCallback(ctx context.Context, callbackID string, answer CallbackAnswer) error {
	_, err := m.Bot.AnswerCallbackQuery(ctx, &gbot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
		Text:            answer.Text,
		ShowAlert:       answer.ShowAlert,
	})
	return err
}

// ──────────────────────── DeleteMessage ────────────────────────

func (m BotMessenger) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	_, err := m.Bot.DeleteMessage(ctx, &gbot.DeleteMessageParams{ChatID: chatID, MessageID: messageID})
	return err
}

// ──────────────────────── RemoveReplyKeyboard (migration helper) ────────────────────────

// RemoveReplyKeyboard sends a message that removes the persistent reply keyboard.
func (m BotMessenger) RemoveReplyKeyboard(ctx context.Context, chatID int64) error {
	_, err := m.Bot.SendMessage(ctx, &gbot.SendMessageParams{
		ChatID:    chatID,
		Text:      "✅ 已切换到新版界面",
		ParseMode: "HTML",
		ReplyMarkup: models.ReplyKeyboardRemove{
			RemoveKeyboard: true,
		},
	})
	return err
}

// ──────────────────────── UpdateHandler (extracts callback context) ────────────────────────

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
			messageID := 0
			hasMedia := false
			if update.CallbackQuery.Message.Message != nil {
				chatID = update.CallbackQuery.Message.Message.Chat.ID
				messageID = update.CallbackQuery.Message.Message.ID
				hasMedia = len(update.CallbackQuery.Message.Message.Photo) > 0
			}
			cctx := CallbackContext{
				UserID:       update.CallbackQuery.From.ID,
				ChatID:       chatID,
				MessageID:    messageID,
				HasMedia:     hasMedia,
				CallbackID:   update.CallbackQuery.ID,
				CallbackData: update.CallbackQuery.Data,
			}
			if err := h.HandleCallback(ctx, cctx); err != nil {
				h.reportError(ctx, chatID, err)
			}
		}
	}
}

// ──────────────────────── Helpers ────────────────────────

func buildInlineKeyboard(buttons [][]Button) *models.InlineKeyboardMarkup {
	if len(buttons) == 0 {
		return nil
	}
	rows := make([][]models.InlineKeyboardButton, 0, len(buttons))
	for _, row := range buttons {
		btns := make([]models.InlineKeyboardButton, 0, len(row))
		for _, b := range row {
			btn := models.InlineKeyboardButton{Text: b.Text}
			switch {
			case b.URL != "":
				btn.URL = b.URL
			case b.CopyText != "":
				btn.CopyText = &models.CopyTextButton{Text: b.CopyText}
			default:
				btn.CallbackData = b.CallbackData
			}
			btns = append(btns, btn)
		}
		rows = append(rows, btns)
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// isNotModified checks if the Telegram API returned "message is not modified".
func isNotModified(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "message is not modified")
}

// downloadPoster downloads a poster image and returns its bytes (for future use).
func downloadPoster(ctx context.Context, httpClient *http.Client, url string) ([]byte, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poster HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB max
}

func (h *Handler) reportError(ctx context.Context, chatID int64, err error) {
	if messenger, ok := h.messenger.(BotMessenger); ok && messenger.Logger != nil {
		messenger.Logger.ErrorContext(ctx, "telegram handler failed", "error_type", fmt.Sprintf("%T", err))
	}
	_, _ = h.messenger.Send(ctx, chatID, ViewFromText("❌ 操作暂时失败，请稍后重试。"))
}
