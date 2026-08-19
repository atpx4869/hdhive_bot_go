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
	"github.com/atpx4869/hdhive_bot_go/internal/version"
)

func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	// 输出版本信息
	v := version.Get()
	logger.Info("starting hdhive-bot-go",
		"version", v.Version,
		"commit", v.GitCommit,
		"go", v.GoVersion,
		"os", v.OS,
		"arch", v.Arch,
	)

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	var pollingErr error
	var pollingErrMu sync.Mutex
	crypt, err := appcrypto.New(cfg.EncryptionKey)
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.DatabaseDSN, crypt)
	if err != nil {
		return err
	}
	defer db.Close()
	// 为不同服务创建独立的 HTTP 客户端，支持细粒度超时
	tmdbHTTPClient, err := newHTTPClientWithTimeout(cfg, cfg.TMDBTimeout)
	if err != nil {
		return err
	}
	hdhiveHTTPClient, err := newHTTPClientWithTimeout(cfg, cfg.HDHiveTimeout)
	if err != nil {
		return err
	}
	p115HTTPClient, err := newHTTPClientWithTimeout(cfg, cfg.P115Timeout)
	if err != nil {
		return err
	}
	tmdbClient, err := tmdb.New(cfg.TMDBToken, tmdbHTTPClient)
	if err != nil {
		return err
	}
	hdhiveClient, err := hdhive.New(cfg.HDHiveBaseURL, cfg.HDHiveSecret, cfg.HDHiveUserID, cfg.HDHiveUserKey, hdhiveHTTPClient)
	if err != nil {
		return err
	}
	var handler *telegram.Handler
	botHTTPClient, err := newTelegramHTTPClient(cfg)
	if err != nil {
		return err
	}
	bot, err := gbot.New(cfg.TelegramToken,
		gbot.WithHTTPClient(3*time.Minute, botHTTPClient),  // 增加到 3 分钟
		gbot.WithAllowedUpdates(gbot.AllowedUpdates{"message", "callback_query"}),
		gbot.WithErrorsHandler(func(err error) {
			logger.Warn("telegram polling error (will retry)", "error", err)
			// 不立即退出，让容器重启策略处理
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
	transferAdapter := &TransferAdapter{HTTP: p115HTTPClient, Logger: logger, HDHive: hdhiveAdapter, UserAgent: cfg.P115UserAgent}
	handler, err = telegram.NewHandler(telegram.Services{Users: db, Accounts: db, Logs: db, TMDB: TMDBAdapter{Client: tmdbClient}, HDHive: hdhiveAdapter, Transfer: transferAdapter}, session.New(cfg.SessionTTL, cfg.SessionCapacity), telegram.BotMessenger{Bot: bot, Logger: logger}, cfg.AdminUserIDs, botHTTPClient)
	if err != nil {
		return err
	}
	setupCtx, cancel := context.WithTimeout(ctx, 60*time.Second)  // 增加到 60 秒
	defer cancel()
	if _, err := bot.DeleteWebhook(setupCtx, &gbot.DeleteWebhookParams{DropPendingUpdates: true}); err != nil {
		return fmt.Errorf("delete telegram webhook: %w", err)
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
	return newHTTPClientWithTimeout(cfg, cfg.HTTPTimeout)
}

func newHTTPClientWithTimeout(cfg config.Config, timeout time.Duration) (*http.Client, error) {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   false,
	}
	if cfg.HTTPProxyURL != "" {
		proxy, err := url.Parse(cfg.HTTPProxyURL)
		if err != nil {
			return nil, err
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}
