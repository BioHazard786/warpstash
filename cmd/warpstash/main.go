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

	"warpstash"
	"warpstash/internal/api"
	"warpstash/internal/bot"
	"warpstash/internal/config"
	"warpstash/internal/database"
	"warpstash/internal/gc"
	"warpstash/internal/logger"
	"warpstash/internal/storage"
)

func main() {
	cfg := config.LoadConfig()
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

	// 4. Build HTTP Router with Embedded Astro Frontend
	staticFS, err := warpstash.StaticFS()
	if err != nil {
		l.Warn("embedded static assets unavailable", "error", err)
	}

	router := api.NewRouter(&api.RouterConfig{
		Config:   cfg,
		DB:       db,
		Storage:  store,
		Cleaner:  cleaner,
		Logger:   l,
		StaticFS: staticFS,
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
