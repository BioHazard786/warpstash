package config

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config represents all application configuration parameters.
type Config struct {
	Port               string        `env:"WARPSTASH_PORT" envDefault:"8080"`
	BaseURL            string        `env:"WARPSTASH_BASE_URL" envDefault:"http://localhost:8080"`
	StoragePath        string        `env:"WARPSTASH_STORAGE_PATH" envDefault:"./data/storage"`
	DBPath             string        `env:"WARPSTASH_DB_PATH" envDefault:"./data/warpstash.db"`
	MaxFileSizeMB      int64         `env:"WARPSTASH_MAX_FILE_SIZE_MB" envDefault:"1024"`
	MaxFiles           int           `env:"WARPSTASH_MAX_FILES" envDefault:"10"`
	MaxTotalStorageGB  int64         `env:"WARPSTASH_MAX_TOTAL_STORAGE_GB" envDefault:"0"`
	AuthToken          string        `env:"WARPSTASH_AUTH_TOKEN"`
	DefaultExpiry      string        `env:"WARPSTASH_DEFAULT_EXPIRY" envDefault:"24h"`
	AllowedExpiries    []string      `env:"WARPSTASH_ALLOWED_EXPIRIES" envDefault:"1h,12h,24h,72h,burn" envSeparator:","`
	GCInterval         time.Duration `env:"WARPSTASH_GC_INTERVAL" envDefault:"60s"`
	RateLimitReqPerSec float64       `env:"WARPSTASH_RATE_LIMIT_RPS" envDefault:"10.0"`
	RateLimitBurst     int           `env:"WARPSTASH_RATE_LIMIT_BURST" envDefault:"30"`
	TrustProxy         bool          `env:"WARPSTASH_TRUST_PROXY" envDefault:"false"`
}

// LoadConfig parses flags and environment variables with sensible defaults.
// Environment variables override defaults, and CLI flags override environment variables.
func LoadConfig() *Config {
	// Attempt to load .env from current directory or parents (ignored if file does not exist)
	_ = godotenv.Load(".env", "../.env", "../../.env")

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: error parsing environment variables: %v\n", err)
	}

	// CLI flags override environment variables
	if !flag.Parsed() && flag.CommandLine.Lookup("port") == nil {
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
	}

	if !flag.Parsed() {
		flag.Parse()
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
