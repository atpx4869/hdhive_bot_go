package logging

import (
	"bytes"
	"log/slog"
	"testing"
)

func TestRedactingHandler_RedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := NewRedactingHandler(base, "secret-token-123", "api-key-456")
	logger := slog.New(handler)

	logger.Info("connecting with secret-token-123 and api-key-456")

	output := buf.String()
	if bytes.Contains([]byte(output), []byte("secret-token-123")) {
		t.Error("expected secret-token-123 to be redacted")
	}
	if bytes.Contains([]byte(output), []byte("api-key-456")) {
		t.Error("expected api-key-456 to be redacted")
	}
	if !bytes.Contains([]byte(output), []byte("***")) {
		t.Error("expected *** redaction marker")
	}
}

func TestRedactingHandler_RedactsAttributes(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := NewRedactingHandler(base, "my-secret")
	logger := slog.New(handler)

	logger.Info("test message", "token", "my-secret-value", "safe", "visible")

	output := buf.String()
	if bytes.Contains([]byte(output), []byte("my-secret-value")) {
		t.Error("expected attribute value to be redacted")
	}
	if !bytes.Contains([]byte(output), []byte("visible")) {
		t.Error("expected safe value to remain")
	}
}

func TestRedactingHandler_EmptySecretsIgnored(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := NewRedactingHandler(base, "", "  ", "valid-secret")
	logger := slog.New(handler)

	logger.Info("test with valid-secret inside")

	output := buf.String()
	if bytes.Contains([]byte(output), []byte("valid-secret")) {
		t.Error("expected valid-secret to be redacted")
	}
}

func TestRedactingHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := NewRedactingHandler(base, "hidden")
	logger := slog.New(handler).With("password", "hidden-value")

	logger.Info("test")

	output := buf.String()
	if bytes.Contains([]byte(output), []byte("hidden-value")) {
		t.Error("expected WithAttrs value to be redacted")
	}
}

func TestRedactingHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	handler := NewRedactingHandler(base, "group-secret")
	logger := slog.New(handler).WithGroup("auth").With("token", "group-secret-token")

	logger.Info("test")

	output := buf.String()
	if bytes.Contains([]byte(output), []byte("group-secret-token")) {
		t.Error("expected grouped value to be redacted")
	}
}
