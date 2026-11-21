package config

import (
	"os"
	"testing"
	"time"
)

func TestParseExpiry(t *testing.T) {
	cfg := &Config{
		DefaultExpiry:   "24h",
		AllowedExpiries: []string{"1h", "12h", "24h", "72h", "burn"},
		MaxExpiry:       "30d",
	}

	tests := []struct {
		input       string
		expectedDur time.Duration
		isBurn      bool
		expectErr   bool
	}{
		{"1h", 1 * time.Hour, false, false},
		{"12h", 12 * time.Hour, false, false},
		{"24h", 24 * time.Hour, false, false},
		{"72h", 72 * time.Hour, false, false},
		{"burn", 72 * time.Hour, true, false},
		{"1x", 72 * time.Hour, true, false},
		{"", 24 * time.Hour, false, false},      // fallback to default
		{"90m", 90 * time.Minute, false, false}, // custom values beyond the presets
		{"7d", 7 * 24 * time.Hour, false, false},
		{"2w", 14 * 24 * time.Hour, false, false},
		{"1d12h", 36 * time.Hour, false, false},
		{"30d", 30 * 24 * time.Hour, false, false},
		{"31d", 0, false, true}, // over MaxExpiry
		{"30s", 0, false, true}, // under a minute
		{"0h", 0, false, true},
		{"-5h", 0, false, true},
		{"999999999w", 0, false, true}, // overflow guard
		{"invalid", 0, false, true},
	}

	for _, tt := range tests {
		dur, isBurn, err := cfg.ParseExpiry(tt.input)
		if (err != nil) != tt.expectErr {
			t.Errorf("ParseExpiry(%q) error = %v, expectErr = %v", tt.input, err, tt.expectErr)
			continue
		}
		if !tt.expectErr {
			if dur != tt.expectedDur {
				t.Errorf("ParseExpiry(%q) dur = %v, expected %v", tt.input, dur, tt.expectedDur)
			}
			if isBurn != tt.isBurn {
				t.Errorf("ParseExpiry(%q) isBurn = %v, expected %v", tt.input, isBurn, tt.isBurn)
			}
		}
	}
}

func TestLoadConfigEnv(t *testing.T) {
	os.Setenv("WARPSTASH_PORT", "9090")
	os.Setenv("WARPSTASH_MAX_FILE_SIZE_MB", "512")
	defer func() {
		os.Unsetenv("WARPSTASH_PORT")
		os.Unsetenv("WARPSTASH_MAX_FILE_SIZE_MB")
	}()

	cfg := LoadConfig()
	if cfg.Version != Version {
		t.Errorf("expected version %s, got %s", Version, cfg.Version)
	}
	if cfg.Port != "9090" {
		t.Errorf("expected port 9090, got %s", cfg.Port)
	}
	if cfg.MaxFileSizeMB != 512 {
		t.Errorf("expected max file size 512, got %d", cfg.MaxFileSizeMB)
	}
	if cfg.MaxFileSizeBytes() != 512*1024*1024 {
		t.Errorf("unexpected max file size bytes: %d", cfg.MaxFileSizeBytes())
	}
}

func TestLoadDotEnv(t *testing.T) {
	tmpFile, err := os.CreateTemp("", ".env.*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	content := `# Comment line
WARPSTASH_PORT=7777
WARPSTASH_MAX_FILE_SIZE_MB=256
WARPSTASH_ALLOWED_EXPIRIES="1h,24h"
`
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	// Test loading via --env-file
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"warpstash", "--env-file", tmpFile.Name()}

	cfg := LoadConfig()

	if cfg.Port != "7777" {
		t.Errorf("expected port 7777, got %q", cfg.Port)
	}
	if cfg.MaxFileSizeMB != 256 {
		t.Errorf("expected max file size 256, got %d", cfg.MaxFileSizeMB)
	}
	if len(cfg.AllowedExpiries) != 2 || cfg.AllowedExpiries[0] != "1h" || cfg.AllowedExpiries[1] != "24h" {
		t.Errorf("expected [1h 24h], got %v", cfg.AllowedExpiries)
	}
	if cfg.LoadedEnvFile != tmpFile.Name() {
		t.Errorf("expected LoadedEnvFile %q, got %q", tmpFile.Name(), cfg.LoadedEnvFile)
	}
}

func TestMaxExpiry(t *testing.T) {
	unlimited := &Config{MaxExpiry: "0", DefaultExpiry: "24h"}
	if d, _, err := unlimited.ParseExpiry("52w"); err != nil || d != 52*7*24*time.Hour {
		t.Errorf("MaxExpiry=0 should not cap: %v %v", d, err)
	}
	// a typo must not lift the cap
	typo := &Config{MaxExpiry: "thirty", DefaultExpiry: "24h"}
	if _, _, err := typo.ParseExpiry("90d"); err == nil {
		t.Error("an unparseable MaxExpiry should fall back to 30d, not unlimited")
	}
	// burn files never outlive the cap
	short := &Config{MaxExpiry: "24h", DefaultExpiry: "1h"}
	if d, burn, _ := short.ParseExpiry("burn"); !burn || d != 24*time.Hour {
		t.Errorf("burn lifetime = %v, want capped to 24h", d)
	}
}
