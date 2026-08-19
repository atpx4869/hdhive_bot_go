package logging

import (
	"context"
	"log/slog"
	"strings"
)

// RedactingHandler wraps a slog.Handler and redacts sensitive values from log records.
type RedactingHandler struct {
	inner    slog.Handler
	secrets  []string
	redacted string
}

// NewRedactingHandler creates a handler that replaces occurrences of secrets with "***".
func NewRedactingHandler(inner slog.Handler, secrets ...string) *RedactingHandler {
	cleaned := make([]string, 0, len(secrets))
	for _, s := range secrets {
		s = strings.TrimSpace(s)
		if s != "" {
			cleaned = append(cleaned, s)
		}
	}
	return &RedactingHandler{inner: inner, secrets: cleaned, redacted: "***"}
}

func (h *RedactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *RedactingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Redact message
	r.Message = h.redact(r.Message)

	// Redact attributes
	redacted := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		redacted = append(redacted, h.redactAttr(a))
		return true
	})

	// Create a new record with redacted values
	clean := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	for _, a := range redacted {
		clean.AddAttrs(a)
	}
	return h.inner.Handle(ctx, clean)
}

func (h *RedactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.redactAttr(a)
	}
	return &RedactingHandler{inner: h.inner.WithAttrs(redacted), secrets: h.secrets, redacted: h.redacted}
}

func (h *RedactingHandler) WithGroup(name string) slog.Handler {
	return &RedactingHandler{inner: h.inner.WithGroup(name), secrets: h.secrets, redacted: h.redacted}
}

func (h *RedactingHandler) redact(s string) string {
	for _, secret := range h.secrets {
		s = strings.ReplaceAll(s, secret, h.redacted)
	}
	return s
}

func (h *RedactingHandler) redactAttr(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.Attr{Key: a.Key, Value: slog.StringValue(h.redact(a.Value.String()))}
	case slog.KindGroup:
		attrs := a.Value.Group()
		redacted := make([]slog.Attr, len(attrs))
		for i, g := range attrs {
			redacted[i] = h.redactAttr(g)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(redacted...)}
	default:
		return a
	}
}
