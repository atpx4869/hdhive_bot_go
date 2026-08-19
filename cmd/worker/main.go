package main

import (
	"log/slog"
	"os"

	"github.com/atpx4869/hdhive_bot_go/internal/app"
	"github.com/atpx4869/hdhive_bot_go/internal/config"
	"github.com/atpx4869/hdhive_bot_go/internal/logging"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Create logger with secret redaction
	base := slog.NewJSONHandler(os.Stdout, nil)
	logger := slog.New(logging.NewRedactingHandler(base,
		cfg.TelegramToken,
		cfg.TMDBToken,
		cfg.HDHiveSecret,
		cfg.HDHiveUserKey,
	))

	if err := app.RunWithSignals(cfg, logger); err != nil {
		logger.Error("worker exited", "error", err)
		os.Exit(1)
	}
}
