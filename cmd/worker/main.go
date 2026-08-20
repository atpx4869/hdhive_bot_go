package main

import (
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/atpx4869/hdhive_bot_go/internal/app"
	"github.com/atpx4869/hdhive_bot_go/internal/config"
)

// version 由构建时通过 -ldflags "-X main.version=vX.Y.Z" 注入，本地运行默认 dev。
var version = "dev"

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
		"version", version,
		"go_version", runtime.Version(),
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
