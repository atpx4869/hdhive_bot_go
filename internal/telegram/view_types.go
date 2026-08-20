package telegram

import (
	"context"
	"fmt"
	"strings"
)

// ──────────────────────── Message Reference ────────────────────────

// MessageRef identifies an existing Telegram message for in-place editing.
type MessageRef struct {
	ChatID    int64
	MessageID int
	HasMedia  bool   // true if message currently contains photo/video
	MediaID   string // Telegram file_id of current media (for EditMedia)
	MediaURL  string // Original URL of current media
}

// ──────────────────────── View (renders as one Telegram message) ────────────────────────

// View describes the content and buttons of a single Telegram message.
type View struct {
	Body           string     // HTML body (caption for photo, text for text message)
	Media          *Media     // optional poster
	Buttons        [][]Button // inline keyboard rows
	ProtectContent bool       // prevent forwarding/saving
}

// Media describes a photo attachment.
type Media struct {
	Type string // "photo" (only type supported in Phase A)
	URL  string // HTTP(S) URL or Telegram file_id
}

// ──────────────────────── Button ────────────────────────

// Button represents a single inline keyboard button.
// Exactly one of CallbackData, URL, or CopyText must be set.
type Button struct {
	Text         string
	Style        string // "", "primary", "success", "danger"
	CallbackData string // callback action
	URL          string // opens URL (no callback)
	CopyText     string // copies text to clipboard (CopyTextButton)
}

// CallbackButton creates a button that triggers a callback.
func CallbackButton(text, token, style string) Button {
	return Button{Text: text, CallbackData: token, Style: style}
}

// URLButton creates a button that opens a URL.
func URLButton(text, url string) Button {
	return Button{Text: text, URL: url}
}

// CopyButton creates a button that copies text to clipboard.
func CopyButton(text, value string) Button {
	return Button{Text: text, CopyText: value}
}

// NoopButton creates a non-interactive display button (uses "noop" callback).
func NoopButton(text string) Button {
	return Button{Text: text, CallbackData: "noop", Style: ""}
}

// Validate checks that exactly one action type is set.
func (b Button) Validate() error {
	count := 0
	if b.CallbackData != "" {
		count++
	}
	if b.URL != "" {
		count++
	}
	if b.CopyText != "" {
		count++
	}
	if count != 1 {
		return fmt.Errorf("button %q must have exactly one action (callback/url/copy), got %d", b.Text, count)
	}
	return nil
}

// ActionKind returns "callback", "url", "copy", or "none".
func (b Button) ActionKind() string {
	if b.CallbackData != "" {
		return "callback"
	}
	if b.URL != "" {
		return "url"
	}
	if b.CopyText != "" {
		return "copy"
	}
	return "none"
}

// CallbackDataLen returns the byte length of CallbackData.
func (b Button) CallbackDataLen() int {
	return len(b.CallbackData)
}

// ──────────────────────── Callback Answer ────────────────────────

// CallbackAnswer describes the response to a callback query.
type CallbackAnswer struct {
	Text      string
	ShowAlert bool // true = popup alert, false = toast
}

// ──────────────────────── Callback Context ────────────────────────

// CallbackContext carries the full context of an incoming callback query.
type CallbackContext struct {
	UserID       int64
	ChatID       int64
	MessageID    int
	HasMedia     bool // true if the callback message has photo/video
	CallbackID   string
	CallbackData string
}

// ──────────────────────── Messenger Interface ────────────────────────

// Messenger abstracts all Telegram message operations.
type Messenger interface {
	// Send creates a new message and returns its reference.
	Send(ctx context.Context, chatID int64, view View) (MessageRef, error)

	// Render updates an existing message in-place.
	// Chooses EditMessageText, EditMessageCaption, or EditMessageMedia
	// based on current state and new view.
	Render(ctx context.Context, current MessageRef, view View) (MessageRef, error)

	// AnswerCallback responds to a callback query.
	AnswerCallback(ctx context.Context, callbackID string, answer CallbackAnswer) error

	// DeleteMessage removes a message.
	DeleteMessage(ctx context.Context, chatID int64, messageID int) error
}

// ──────────────────────── Helper: rune-aware truncation ────────────────────────

// TruncateRunes truncates s to max runes, appending "…" if truncated.
func TruncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 0 {
		return ""
	}
	return string(runes[:max-1]) + "…"
}

// TruncateCaption ensures HTML body stays within Telegram's 1024-char caption limit.
// Target safe limit is 900 runes to leave room for dynamic content.
func TruncateCaption(s string) string {
	return TruncateRunes(s, 900)
}

// ValidateCallbackDataLen checks that a callback data string fits in 64 bytes.
func ValidateCallbackDataLen(data string) bool {
	return len(data) <= 64 && len(data) >= 1
}

// ──────────────────────── Old → New Bridge ────────────────────────

// ViewFromText creates a simple text-only View (for backward compatibility during migration).
func ViewFromText(text string, buttons ...[]Button) View {
	v := View{Body: text}
	if len(buttons) > 0 {
		v.Buttons = buttons
	}
	return v
}

// containsHTML checks if a string contains HTML tags (for auto-detecting ParseMode).
func containsHTML(s string) bool {
	return strings.Contains(s, "<b>") || strings.Contains(s, "<code>") ||
		strings.Contains(s, "<i>") || strings.Contains(s, "<a ") ||
		strings.Contains(s, "<pre>") || strings.Contains(s, "<tg-spoiler>")
}
