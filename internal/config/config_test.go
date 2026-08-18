package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envBotToken, "123456:token")
	t.Setenv(envAdminUserIDs, "123,456")
	t.Setenv(envEncryptionKey, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	t.Setenv(envDatabaseDSN, "file:bot.db")
	t.Setenv(envTMDBToken, "tmdb-test-token")
	t.Setenv(envHDHiveBaseURL, "https://proxy.example.test")
	t.Setenv(envHDHiveSecret, "secret")
	t.Setenv(envHDHiveUserID, "user")
	t.Setenv(envHDHiveUserKey, "key")
}
func TestLoad(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TelegramToken != "123456:token" || cfg.DatabaseDSN != "file:bot.db" || cfg.TMDBToken == "" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if len(cfg.AdminUserIDs) != 2 || cfg.AdminUserIDs[0] != 123 || cfg.AdminUserIDs[1] != 456 {
		t.Fatalf("unexpected admin IDs: %v", cfg.AdminUserIDs)
	}
	if string(cfg.EncryptionKey) != "0123456789abcdef0123456789abcdef" {
		t.Fatal("unexpected encryption key")
	}
}
func TestLoadRejectsInvalidEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*testing.T)
		wantErr string
	}{
		{"missing token", func(t *testing.T) { t.Setenv(envBotToken, "") }, envBotToken},
		{"admin whitespace", func(t *testing.T) { t.Setenv(envAdminUserIDs, "123, 456") }, envAdminUserIDs},
		{"admin duplicate", func(t *testing.T) { t.Setenv(envAdminUserIDs, "123,123") }, "duplicate"},
		{"bad base64", func(t *testing.T) { t.Setenv(envEncryptionKey, "not-base64") }, "base64"},
		{"wrong key length", func(t *testing.T) { t.Setenv(envEncryptionKey, base64.StdEncoding.EncodeToString([]byte("short"))) }, "32 bytes"},
		{"bad URL", func(t *testing.T) { t.Setenv(envHDHiveBaseURL, "relative") }, envHDHiveBaseURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setValidEnv(t)
			tt.mutate(t)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
