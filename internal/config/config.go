package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	envBotToken      = "TELEGRAM_BOT_TOKEN"
	envAdminUserIDs  = "TELEGRAM_ADMIN_USER_IDS"
	envEncryptionKey = "ENCRYPTION_KEY"
	envDatabaseDSN   = "SQLITE_DSN"
	envTMDBToken     = "TMDB_TOKEN"
	envHDHiveBaseURL = "HDHIVE_BASE_URL"
	envHDHiveSecret  = "HDHIVE_SECRET"
	envHDHiveUserID  = "HDHIVE_USER_ID"
	envHDHiveUserKey = "HDHIVE_USER_KEY"
	envHTTPProxy     = "HTTP_PROXY_URL"
)

type Config struct {
	TelegramToken   string
	AdminUserIDs    []int64
	TMDBToken       string
	HDHiveBaseURL   string
	HDHiveSecret    string
	HDHiveUserID    string
	HDHiveUserKey   string
	DatabaseDSN     string
	EncryptionKey   []byte
	HTTPProxyURL    string
	HTTPTimeout     time.Duration
	TMDBTimeout     time.Duration
	HDHiveTimeout   time.Duration
	P115Timeout     time.Duration
	P115UserAgent   string
	P115Endpoint    string
	SessionTTL      time.Duration
	SessionCapacity int
}

func Load() (Config, error) {
	var cfg Config
	var err error
	if cfg.TelegramToken, err = requiredEnv(envBotToken); err != nil {
		return cfg, err
	}
	admins, err := requiredEnv(envAdminUserIDs)
	if err != nil {
		return cfg, err
	}
	if cfg.AdminUserIDs, err = parseAdminUserIDs(admins); err != nil {
		return cfg, fmt.Errorf("%s: %w", envAdminUserIDs, err)
	}
	if cfg.TMDBToken, err = requiredEnv(envTMDBToken); err != nil {
		return cfg, err
	}
	if cfg.HDHiveBaseURL, err = requiredURL(envHDHiveBaseURL); err != nil {
		return cfg, err
	}
	if cfg.HDHiveSecret, err = requiredEnv(envHDHiveSecret); err != nil {
		return cfg, err
	}
	if cfg.HDHiveUserID, err = requiredEnv(envHDHiveUserID); err != nil {
		return cfg, err
	}
	if cfg.HDHiveUserKey, err = requiredEnv(envHDHiveUserKey); err != nil {
		return cfg, err
	}
	if cfg.DatabaseDSN, err = requiredEnv(envDatabaseDSN); err != nil {
		return cfg, err
	}
	key, err := requiredEnv(envEncryptionKey)
	if err != nil {
		return cfg, err
	}
	cfg.EncryptionKey, err = base64.StdEncoding.Strict().DecodeString(key)
	if err != nil {
		return cfg, fmt.Errorf("%s must be canonical base64: %w", envEncryptionKey, err)
	}
	if len(cfg.EncryptionKey) != 32 {
		return cfg, fmt.Errorf("%s must decode to exactly 32 bytes", envEncryptionKey)
	}
	cfg.HTTPProxyURL = strings.TrimSpace(os.Getenv(envHTTPProxy))
	if cfg.HTTPProxyURL != "" {
		if _, err := url.ParseRequestURI(cfg.HTTPProxyURL); err != nil {
			return cfg, fmt.Errorf("%s: %w", envHTTPProxy, err)
		}
	}
	if cfg.HTTPTimeout, err = durationEnv("HTTP_TIMEOUT", 30*time.Second); err != nil {
		return cfg, err
	}
	if cfg.TMDBTimeout, err = durationEnv("TMDB_TIMEOUT", cfg.HTTPTimeout); err != nil {
		return cfg, err
	}
	if cfg.HDHiveTimeout, err = durationEnv("HDHIVE_TIMEOUT", cfg.HTTPTimeout); err != nil {
		return cfg, err
	}
	if cfg.P115Timeout, err = durationEnv("P115_TIMEOUT", cfg.HTTPTimeout); err != nil {
		return cfg, err
	}
	cfg.P115UserAgent = strings.TrimSpace(os.Getenv("P115_USER_AGENT"))
	if cfg.P115UserAgent == "" {
		cfg.P115UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}
	cfg.P115Endpoint = strings.TrimSpace(os.Getenv("P115_ENDPOINT"))
	if cfg.P115Endpoint == "" {
		cfg.P115Endpoint = "https://proapi.115.com"
	}
	if cfg.SessionTTL, err = durationEnv("SESSION_TTL", 30*time.Minute); err != nil {
		return cfg, err
	}
	if cfg.SessionCapacity, err = positiveIntEnv("SESSION_CAPACITY", 1000); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func requiredEnv(name string) (string, error) {
	value, ok := os.LookupEnv(name)
	if !ok || value == "" || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("required environment variable %s must be set, non-empty, and have no surrounding whitespace", name)
	}
	return value, nil
}
func requiredURL(name string) (string, error) {
	value, err := requiredEnv(name)
	if err != nil {
		return "", err
	}
	u, err := url.ParseRequestURI(value)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("%s must be an absolute HTTPS URL", name)
	}
	return strings.TrimRight(value, "/"), nil
}
func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return d, nil
}
func positiveIntEnv(name string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}
func parseAdminUserIDs(value string) ([]int64, error) {
	parts := strings.Split(value, ",")
	ids := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part {
			return nil, errors.New("must be a comma-separated list without empty values or whitespace")
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid positive user ID %q", part)
		}
		if _, exists := seen[id]; exists {
			return nil, fmt.Errorf("duplicate user ID %d", id)
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}
