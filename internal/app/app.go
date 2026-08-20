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

	// 启动恢复：清理过期的 in_flight 记录，防止进程崩溃后永久阻塞用户
	if recovered, err := recoverStaleInFlight(ctx, db, logger); err != nil {
		logger.Warn("failed to recover stale in-flight records", "error", err)
	} else if recovered > 0 {
		logger.Info("recovered stale in-flight records", "count", recovered)
	}

	// 清理过期活动日志（保留 90 天）
	if deleted, err := db.CleanupActivityLogs(ctx, 90); err != nil {
		logger.Warn("failed to cleanup activity logs", "error", err)
	} else if deleted > 0 {
		logger.Info("cleaned up activity logs", "deleted", deleted)
	}

	// 输出数据库统计
	if size, err := db.GetDatabaseSize(ctx); err == nil {
		logger.Info("database stats", "size_bytes", size)
	}

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

// recoverStaleInFlight marks in-flight unlock records older than 1 hour as "unknown",
// so users are not permanently blocked after a crash. Returns the number of recovered records.
func recoverStaleInFlight(ctx context.Context, db *store.Store, logger *slog.Logger) (int, error) {
	records, err := db.GetInFlightUnlockRecords(ctx, 500)
	if err != nil {
		return 0, err
	}
	threshold := time.Now().Add(-1 * time.Hour)
	recovered := 0
	for _, r := range records {
		if r.UpdatedAt.Before(threshold) {
			if err := db.SetUnlockRecord(ctx, store.UnlockRecord{
				UserID:     r.UserID,
				ResourceID: r.ResourceID,
				Status:     "unknown",
			}); err != nil {
				logger.Warn("failed to recover in-flight record",
					"user_id", r.UserID, "resource_id", r.ResourceID, "error", err)
				continue
			}
			recovered++
			logger.Info("recovered stale in-flight record",
				"user_id", r.UserID, "resource_id", r.ResourceID)
		}
	}
	return recovered, nil
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
	base := &http.Transport{
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
		base.Proxy = http.ProxyURL(proxy)
	}
	return &http.Client{
		Transport: &RetryTransport{Base: base, MaxRetry: 2},
		Timeout:   timeout,
	}, nil
}
