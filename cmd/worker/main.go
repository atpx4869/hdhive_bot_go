package main

import (
	"log/slog"
	"os"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/app"
	"github.com/atpx4869/hdhive_bot_go/internal/config"
)

func main() {
	// 配置日志级别和格式
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level:     level,
		AddSource: false,
	}))

	logger.Info("HDHive Bot Worker starting",
		"version", "1.0.0",
		"go_version", "1.25",
		"pid", os.Getpid(),
		"timestamp", time.Now().Format(time.RFC3339),
	)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger.Info("config loaded successfully",
		"admin_count", len(cfg.AdminUserIDs),
		"session_ttl", cfg.SessionTTL,
		"session_capacity", cfg.SessionCapacity,
	)

	err = app.RunWithSignals(cfg, logger)
	if err != nil {
		logger.Error("worker exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("worker exited normally")
}
