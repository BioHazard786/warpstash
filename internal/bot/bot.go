package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/celestix/gotgproto"
	"github.com/celestix/gotgproto/dispatcher/handlers"
	"github.com/celestix/gotgproto/dispatcher/handlers/filters"
	"github.com/celestix/gotgproto/ext"
	"github.com/celestix/gotgproto/sessionMaker"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"golang.org/x/time/rate"

	"warpstash/internal/config"
	"warpstash/internal/database"
	"warpstash/internal/storage"
)

// BotService orchestrates the Telegram MTProto bot life-cycle.
type BotService struct {
	cfg     *config.Config
	db      *database.DB
	storage storage.StorageEngine
	logger  *slog.Logger
	pending *PendingManager
	limiter *UserRateLimiter
}

// Start initiates the Telegram MTProto bot in a supervised background goroutine.
// Error and panic isolation: any failure or panic in the bot will NEVER crash or impact
// the primary HTTP server.
func Start(ctx context.Context, cfg *config.Config, db *database.DB, store storage.StorageEngine, l *slog.Logger) {
	if !cfg.TelegramEnabled() {
		l.Debug("telegram bot is not enabled (credentials omitted)")
		return
	}

	limiter := NewUserRateLimiter(rate.Limit(cfg.TelegramRateLimitRPS), cfg.TelegramRateLimitBurst)

	svc := &BotService{
		cfg:     cfg,
		db:      db,
		storage: store,
		logger:  l,
		pending: NewPendingManager(15 * time.Minute),
		limiter: limiter,
	}

	go func() {
		<-ctx.Done()
		limiter.Stop()
	}()

	go svc.supervisor(ctx)
}

// supervisor maintains a resilient loop with exponential backoff and panic recovery.
func (b *BotService) supervisor(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("telegram bot supervisor recovered from unhandled fatal panic",
				"panic", r,
				"stack", string(debug.Stack()),
			)
		}
	}()

	b.logger.Info("starting Telegram MTProto bot service",
		"app_id", b.cfg.TelegramAppID,
	)

	backoff := 2 * time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			b.logger.Info("telegram bot supervisor stopped via context cancellation")
			return
		default:
		}

		err := b.runClient(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				b.logger.Info("telegram bot stopped cleanly")
				return
			}
			b.logger.Error("telegram bot stopped with error, reconnecting...",
				"error", err,
				"retry_in", backoff.String(),
			)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// runClient initializes the gotgproto client, registers handlers, and blocks on Idle.
func (b *BotService) runClient(ctx context.Context) (runErr error) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("telegram bot client runner recovered from panic",
				"panic", r,
				"stack", string(debug.Stack()),
			)
			runErr = fmt.Errorf("bot panic: %v", r)
		}
	}()

	// Store session in data volume alongside SQLite database so auth key survives container restarts
	sessionDir := filepath.Dir(b.cfg.DBPath)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("failed to create session directory: %w", err)
	}
	sessionPath := filepath.Join(sessionDir, "warpstash.session")

	opts := &gotgproto.ClientOpts{
		Context:          ctx,
		DisableCopyright: true,
		Logger:           zap.NewNop(),
		Session: sessionMaker.SqlSession(
			sqlite.Open(sessionPath),
		),
		PanicHandler: func(c *ext.Context, u *ext.Update, panicErr string) {
			b.logger.Error("telegram bot dispatcher recovered from handler panic",
				"panic", panicErr,
			)
		},
		ErrorHandler: func(c *ext.Context, u *ext.Update, errStr string) error {
			b.logger.Error("telegram bot handler returned error",
				"error", errStr,
			)
			return nil
		},
	}

	// Use connection timeout for initial handshake
	initCtx, initCancel := context.WithTimeout(ctx, 120*time.Second)
	defer initCancel()

	type clientResult struct {
		client *gotgproto.Client
		err    error
	}
	resultChan := make(chan clientResult, 1)

	go func() {
		client, err := gotgproto.NewClient(
			b.cfg.TelegramAppID,
			b.cfg.TelegramAppHash,
			gotgproto.ClientTypeBot(b.cfg.TelegramBotToken),
			opts,
		)
		resultChan <- clientResult{client: client, err: err}
	}()

	var client *gotgproto.Client
	select {
	case <-initCtx.Done():
		return fmt.Errorf("telegram client connection timed out: %w", initCtx.Err())
	case res := <-resultChan:
		if res.err != nil {
			return fmt.Errorf("failed to initialize telegram client: %w", res.err)
		}
		client = res.client
	}

	// Register bot handlers
	dp := client.Dispatcher
	dp.AddHandler(handlers.NewCommand("start", b.handleStart))
	dp.AddHandler(handlers.NewCommand("help", b.handleHelp))
	dp.AddHandler(handlers.NewMessage(filters.Message.Media, b.handleMedia))
	dp.AddHandler(handlers.NewCallbackQuery(filters.CallbackQuery.Prefix("exp:"), b.handleCallback))
	dp.AddHandler(handlers.NewCallbackQuery(filters.CallbackQuery.Prefix("del:"), b.handleDeleteCallback))

	b.logger.Info("telegram bot connected successfully",
		"bot_username", client.Self.Username,
		"bot_id", client.Self.ID,
	)

	// Ensure clean disconnect on ctx cancellation
	stopDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			client.Stop()
		case <-stopDone:
		}
	}()
	defer close(stopDone)

	return client.Idle()
}
