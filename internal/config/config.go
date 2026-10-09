package config

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Version is the build-time application version injected via -ldflags.
// Example: -ldflags "-X warpstash/internal/config.Version=v1.0.0"
// Defaults to "1.0.0" or reads Go module build info.
var Version = "v1.0.0"

func init() {
	if Version == "" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			Version = bi.Main.Version
		} else {
			Version = "v1.0.0"
		}
	}
}

// Config represents all application configuration parameters.
type Config struct {
	Version                string        `env:"WARPSTASH_VERSION" json:"version"`
	Port                   string        `env:"WARPSTASH_PORT" envDefault:"8080"`
	BaseURL                string        `env:"WARPSTASH_BASE_URL" envDefault:"http://localhost:8080"`
	StoragePath            string        `env:"WARPSTASH_STORAGE_PATH" envDefault:"./data/storage"`
	DBPath                 string        `env:"WARPSTASH_DB_PATH" envDefault:"./data/warpstash.db"`
	MaxFileSizeMB          int64         `env:"WARPSTASH_MAX_FILE_SIZE_MB" envDefault:"1024"`
	MaxFiles               int           `env:"WARPSTASH_MAX_FILES" envDefault:"10"`
	MaxTotalStorageGB      int64         `env:"WARPSTASH_MAX_TOTAL_STORAGE_GB" envDefault:"0"`
	AuthToken              string        `env:"WARPSTASH_AUTH_TOKEN"`
	DefaultExpiry          string        `env:"WARPSTASH_DEFAULT_EXPIRY" envDefault:"24h"`
	AllowedExpiries        []string      `env:"WARPSTASH_ALLOWED_EXPIRIES" envDefault:"1h,12h,24h,72h,burn" envSeparator:","`
	GCInterval             time.Duration `env:"WARPSTASH_GC_INTERVAL" envDefault:"60s"`
	RateLimitReqPerSec     float64       `env:"WARPSTASH_RATE_LIMIT_RPS" envDefault:"10.0"`
	RateLimitBurst         int           `env:"WARPSTASH_RATE_LIMIT_BURST" envDefault:"30"`
	TrustProxy             bool          `env:"WARPSTASH_TRUST_PROXY" envDefault:"false"`
	TelegramBotToken       string        `env:"WARPSTASH_TELEGRAM_BOT_TOKEN"`
	TelegramAppID          int           `env:"WARPSTASH_TELEGRAM_APP_ID"`
	TelegramAppHash        string        `env:"WARPSTASH_TELEGRAM_APP_HASH"`
	TelegramAllowedUsers   []int64       `env:"WARPSTASH_TELEGRAM_ALLOWED_USERS" envSeparator:","`
	TelegramRateLimitRPS   float64       `env:"WARPSTASH_TELEGRAM_RATE_LIMIT_RPS" envDefault:"1.0"`
	TelegramRateLimitBurst int           `env:"WARPSTASH_TELEGRAM_RATE_LIMIT_BURST" envDefault:"5"`

	LoadedEnvFile string `json:"-"`
}

// LoadConfig parses flags and environment variables with sensible defaults.
// Environment variables override defaults, and CLI flags override environment variables.
func LoadConfig() *Config {
	cfg := &Config{
		Version: Version,
	}

	// 1. Detect if --env-file was specified, otherwise default to .env
	envPath := ".env"
	for i, arg := range os.Args {
		if (arg == "--env-file" || arg == "-env-file") && i+1 < len(os.Args) {
			envPath = os.Args[i+1]
			break
		}
		if strings.HasPrefix(arg, "--env-file=") || strings.HasPrefix(arg, "-env-file=") {
			parts := strings.SplitN(arg, "=", 2)
			envPath = parts[1]
			break
		}
	}

	_ = godotenv.Overload(envPath)
	cfg.LoadedEnvFile = envPath

	// 2. Parse environment variables into cfg
	if err := env.Parse(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: error parsing environment variables: %v\n", err)
	}

	// 3. Register CLI flags (defaults taken from env / cfg)
	var envFileFlag string
	var showVersion bool
	if !flag.Parsed() && flag.CommandLine.Lookup("port") == nil {
		flag.BoolVar(&showVersion, "v", false, "Print Warpstash version and exit")
		flag.BoolVar(&showVersion, "version", false, "Print Warpstash version and exit")
		flag.StringVar(&envFileFlag, "env-file", cfg.LoadedEnvFile, "Path to .env configuration file")
		flag.StringVar(&cfg.Port, "port", cfg.Port, "HTTP server listening port")
		flag.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "Canonical public base URL (e.g. https://files.example.com)")
		flag.StringVar(&cfg.StoragePath, "storage-path", cfg.StoragePath, "Path to store uploaded files")
		flag.StringVar(&cfg.DBPath, "db-path", cfg.DBPath, "Path to SQLite database file")
		flag.Int64Var(&cfg.MaxFileSizeMB, "max-file-size-mb", cfg.MaxFileSizeMB, "Max single upload size in MB")
		flag.IntVar(&cfg.MaxFiles, "max-files", cfg.MaxFiles, "Max files per batch in web interface")
		flag.Int64Var(&cfg.MaxTotalStorageGB, "max-storage-gb", cfg.MaxTotalStorageGB, "Max total disk quota in GB (0 = unlimited)")
		flag.StringVar(&cfg.AuthToken, "auth-token", cfg.AuthToken, "Optional secret token required for uploads")
		flag.StringVar(&cfg.DefaultExpiry, "default-expiry", cfg.DefaultExpiry, "Default expiration duration if omitted")
		flag.BoolVar(&cfg.TrustProxy, "trust-proxy", cfg.TrustProxy, "Trust X-Real-IP and X-Forwarded-For headers from reverse proxy")
		flag.StringVar(&cfg.TelegramBotToken, "tg-bot-token", cfg.TelegramBotToken, "Telegram bot token from @BotFather")
		flag.IntVar(&cfg.TelegramAppID, "tg-app-id", cfg.TelegramAppID, "Telegram App ID from my.telegram.org")
		flag.StringVar(&cfg.TelegramAppHash, "tg-app-hash", cfg.TelegramAppHash, "Telegram App Hash from my.telegram.org")
		flag.Float64Var(&cfg.TelegramRateLimitRPS, "tg-rate-limit-rps", cfg.TelegramRateLimitRPS, "Telegram bot rate limit requests per second per user")
		flag.IntVar(&cfg.TelegramRateLimitBurst, "tg-rate-limit-burst", cfg.TelegramRateLimitBurst, "Telegram bot rate limit burst allowance per user")
	}

	if !flag.Parsed() {
		flag.Parse()
	}

	if showVersion {
		fmt.Printf("Warpstash %s\n", cfg.Version)
		os.Exit(0)
	}

	// Normalize BaseURL (strip trailing slash)
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	return cfg
}

// MaxFileSizeBytes returns the maximum upload size in bytes.
func (c *Config) MaxFileSizeBytes() int64 {
	return c.MaxFileSizeMB * 1024 * 1024
}

// MaxTotalStorageBytes returns the maximum storage quota in bytes (0 if unlimited).
func (c *Config) MaxTotalStorageBytes() int64 {
	return c.MaxTotalStorageGB * 1024 * 1024 * 1024
}

// ParseExpiry parses an expiry string (e.g., "1h", "12h", "24h", "72h", "burn")
// returning the duration and whether it is a burn-after-reading file.
func (c *Config) ParseExpiry(expiryStr string) (time.Duration, bool, error) {
	clean := strings.ToLower(strings.TrimSpace(expiryStr))
	if clean == "" {
		clean = strings.ToLower(strings.TrimSpace(c.DefaultExpiry))
	}

	if clean == "burn" || clean == "1x" || clean == "once" {
		// Burn files default to a fallback max lifetime of 72 hours if never downloaded
		return 72 * time.Hour, true, nil
	}

	// Verify against allowed list if configured
	if len(c.AllowedExpiries) > 0 {
		allowed := false
		for _, exp := range c.AllowedExpiries {
			if strings.EqualFold(exp, clean) {
				allowed = true
				break
			}
		}
		if !allowed {
			return 0, false, fmt.Errorf("expiry '%s' is not in allowed list: %v", expiryStr, c.AllowedExpiries)
		}
	}

	dur, err := time.ParseDuration(clean)
	if err != nil {
		return 0, false, fmt.Errorf("invalid expiry duration: %w", err)
	}

	if dur <= 0 {
		return 0, false, fmt.Errorf("expiry duration must be positive")
	}

	return dur, false, nil
}

// TelegramEnabled returns true if all required Telegram MTProto credentials are configured.
func (c *Config) TelegramEnabled() bool {
	return c.TelegramBotToken != "" && c.TelegramAppID > 0 && c.TelegramAppHash != ""
}

// IsTelegramUserAllowed checks whether a Telegram user is allowed to upload files.
// If AllowedUsers is empty, access is unrestricted.
func (c *Config) IsTelegramUserAllowed(userID int64) bool {
	if len(c.TelegramAllowedUsers) == 0 {
		return true
	}
	for _, id := range c.TelegramAllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}
