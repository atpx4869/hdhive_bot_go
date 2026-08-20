package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	gbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/atpx4869/hdhive_bot_go/internal/config"
	appcrypto "github.com/atpx4869/hdhive_bot_go/internal/crypto"
	"github.com/atpx4869/hdhive_bot_go/internal/hdhive"
	"github.com/atpx4869/hdhive_bot_go/internal/session"
	"github.com/atpx4869/hdhive_bot_go/internal/store"
	"github.com/atpx4869/hdhive_bot_go/internal/telegram"
	"github.com/atpx4869/hdhive_bot_go/internal/tmdb"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("initializing worker",
		"db_dsn", cfg.DatabaseDSN,
		"hdhive_base_url", cfg.HDHiveBaseURL,
		"proxy", cfg.HTTPProxyURL,
		"http_timeout", cfg.HTTPTimeout,
		"session_ttl", cfg.SessionTTL,
		"session_capacity", cfg.SessionCapacity,
		"admin_count", len(cfg.AdminUserIDs),
	)
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	var pollingErr error
	var pollingErrMu sync.Mutex
	logger.Info("initializing encryption")
	crypt, err := appcrypto.New(cfg.EncryptionKey)
	if err != nil {
		return fmt.Errorf("init crypto: %w", err)
	}
	logger.Info("opening database")
	db, err := store.Open(ctx, cfg.DatabaseDSN, crypt)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	logger.Info("database opened successfully")
	logger.Info("creating HTTP client")
	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return fmt.Errorf("create HTTP client: %w", err)
	}
	logger.Info("initializing TMDB client")
	tmdbClient, err := tmdb.New(cfg.TMDBToken, httpClient)
	if err != nil {
		return fmt.Errorf("init TMDB: %w", err)
	}
	logger.Info("initializing HDHive client")
	hdhiveClient, err := hdhive.New(cfg.HDHiveBaseURL, cfg.HDHiveSecret, cfg.HDHiveUserID, cfg.HDHiveUserKey, httpClient)
	if err != nil {
		return fmt.Errorf("init HDHive: %w", err)
	}
	var handler *telegram.Handler
	botHTTPClient, err := newTelegramHTTPClient(cfg)
	if err != nil {
		return err
	}
	bot, err := gbot.New(cfg.TelegramToken,
		gbot.WithHTTPClient(time.Minute, botHTTPClient),
		gbot.WithAllowedUpdates(gbot.AllowedUpdates{"message", "callback_query"}),
		gbot.WithErrorsHandler(func(err error) {
			pollingErrMu.Lock()
			if pollingErr == nil {
				pollingErr = err
			}
			pollingErrMu.Unlock()
			logger.Error("telegram polling error", "error", err)
			cancelRun()
		}),
		gbot.WithDefaultHandler(func(ctx context.Context, bot *gbot.Bot, update *models.Update) {
			if handler != nil {
				handler.UpdateHandler()(ctx, bot, update)
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("create telegram bot: %w", err)
	}
	hdhiveAdapter := NewHDHiveAdapter(hdhiveClient, db)
	handler, err = telegram.NewHandler(telegram.Services{Users: db, Accounts: db, Logs: db, TMDB: TMDBAdapter{Client: tmdbClient}, HDHive: hdhiveAdapter, Transfer: TransferAdapter{HTTP: httpClient, Logger: logger, HDHive: hdhiveAdapter}}, session.New(cfg.SessionTTL, cfg.SessionCapacity), telegram.BotMessenger{Bot: bot, Logger: logger, HTTP: botHTTPClient}, cfg.AdminUserIDs, logger)
	if err != nil {
		return err
	}
	setupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := bot.DeleteWebhook(setupCtx, &gbot.DeleteWebhookParams{DropPendingUpdates: true}); err != nil {
		return fmt.Errorf("delete telegram webhook: %w", err)
	}
	// 注册Bot命令
	commands := []models.BotCommand{
		{Command: "start", Description: "开始使用"},
		{Command: "myid", Description: "查看你的ID"},
		{Command: "set115", Description: "配置115 Cookie"},
		{Command: "unset115", Description: "删除115配置"},
		{Command: "my115", Description: "查看115状态"},
		{Command: "cancel", Description: "取消当前操作"},
	}
	if _, err := bot.SetMyCommands(setupCtx, &gbot.SetMyCommandsParams{Commands: commands}); err != nil {
		logger.Warn("failed to set bot commands", "error", err)
	} else {
		logger.Info("bot commands registered", "count", len(commands))
	}
	logger.Info("telegram worker started")
	bot.Start(runCtx)
	if ctx.Err() != nil {
		logger.Info("telegram worker stopped", "reason", ctx.Err())
		return nil
	}
	pollingErrMu.Lock()
	err = pollingErr
	pollingErrMu.Unlock()
	if err != nil {
		return fmt.Errorf("telegram polling failed: %w", err)
	}
	return fmt.Errorf("telegram polling returned unexpectedly")
}

func RunWithSignals(cfg config.Config, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, cfg, logger)
}
func newTelegramHTTPClient(cfg config.Config) (*http.Client, error) {
	client, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}
	client.Timeout = 75 * time.Second
	return client, nil
}
func newHTTPClient(cfg config.Config) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.HTTPProxyURL != "" {
		proxy, err := url.Parse(cfg.HTTPProxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{Transport: transport, Timeout: cfg.HTTPTimeout}, nil
}
