package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"warpstash/internal/api"
	"warpstash/internal/bot"
	"warpstash/internal/config"
	"warpstash/internal/database"
	"warpstash/internal/gc"
	"warpstash/internal/logger"
	"warpstash/internal/storage"
	"warpstash/web"
)

func main() {
	cfg := config.LoadConfig()
	if cfg.HealthCheck {
		os.Exit(healthcheck(cfg.Port))
	}
	logFormat := os.Getenv("WARPSTASH_LOG_FORMAT")
	if logFormat == "" {
		logFormat = "text"
	}
	l := logger.Init(os.Getenv("WARPSTASH_LOG_LEVEL"), logFormat)

	l.Info("starting Warpstash server",
		"version", cfg.Version,
		"port", cfg.Port,
		"base_url", cfg.BaseURL,
		"storage_path", cfg.StoragePath,
		"db_path", cfg.DBPath,
		"max_file_size_mb", cfg.MaxFileSizeMB,
		"auth_enabled", cfg.AuthToken != "",
		"telegram_bot_enabled", cfg.TelegramEnabled(),
		"loaded_env_file", cfg.LoadedEnvFile,
	)

	// 1. Initialize SQLite Database (WAL mode, pure Go)
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		l.Error("failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// 2. Initialize Sharded Storage Engine
	store, err := storage.NewDiskStorage(cfg.StoragePath)
	if err != nil {
		l.Error("failed to initialize storage engine", "error", err)
		os.Exit(1)
	}

	// 3. Start Background Garbage Collection and Unlink Pipeline
	cleaner := gc.NewCleaner(db, store, cfg.GCInterval, 4)
	defer cleaner.Stop()

	// 4. Build HTTP Router with the embedded SPA
	router := api.NewRouter(&api.RouterConfig{
		Config:   cfg,
		DB:       db,
		Storage:  store,
		Cleaner:  cleaner,
		Logger:   l,
		StaticFS: web.FS(),
	})

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// 5. Start Telegram MTProto Bot (if configured)
	if cfg.TelegramEnabled() {
		bot.Start(appCtx, cfg, db, store, l)
	}

	// Listen for OS signals for graceful shutdown
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		l.Info(fmt.Sprintf("Warpstash listening on http://localhost:%s", cfg.Port))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			l.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-stopChan
	l.Info("shutting down Warpstash server gracefully...")
	appCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		l.Error("graceful shutdown failed", "error", err)
	}

	l.Info("server shutdown complete")
}

// healthcheck asks the server on this host for /healthz. The runtime image has no curl or shell,
// so Docker's HEALTHCHECK runs the binary itself. Returns the process exit code.
func healthcheck(port string) int {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "unhealthy:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "unhealthy: HTTP", resp.StatusCode)
		return 1
	}
	return 0
}
