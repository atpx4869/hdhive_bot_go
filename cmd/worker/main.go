package main

import (
	"log/slog"
	"os"

	"github.com/atpx4869/hdhive_bot_go/internal/app"
	"github.com/atpx4869/hdhive_bot_go/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err == nil {
		err = app.RunWithSignals(cfg, logger)
	}
	if err != nil {
		logger.Error("worker exited", "error", err)
		os.Exit(1)
	}
}
