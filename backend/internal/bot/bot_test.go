package bot

import (
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	"warpstash/internal/config"
)

func TestPendingManager(t *testing.T) {
	pm := NewPendingManager(100 * time.Millisecond)

	upload := &PendingUpload{
		Filename:  "test.txt",
		Size:      1234,
		MimeType:  "text/plain",
		UserID:    999,
		CreatedAt: time.Now(),
	}

	pm.Store("1:42", upload)

	got, ok := pm.Get("1:42")
	if !ok || got == nil {
		t.Fatalf("expected pending upload to be found")
	}
	if got.Filename != "test.txt" || got.Size != 1234 {
		t.Errorf("mismatched payload: %+v", got)
	}

	// Test Delete
	pm.Delete("1:42")
	_, ok = pm.Get("1:42")
	if ok {
		t.Fatalf("expected item to be deleted")
	}

	// Test Expiration
	pm.Store("1:43", upload)
	time.Sleep(150 * time.Millisecond)
	_, ok = pm.Get("1:43")
	if ok {
		t.Fatalf("expected expired item to be rejected")
	}
}

func TestBuildExpiryMarkup(t *testing.T) {
	cfg := &config.Config{
		AllowedExpiries: []string{"1h", "24h", "burn"},
	}
	svc := &BotService{cfg: cfg}

	markup := svc.buildExpiryMarkup(100)
	if markup == nil || len(markup.Rows) == 0 {
		t.Fatalf("expected non-empty markup rows")
	}

	foundBurn := false
	for _, row := range markup.Rows {
		for _, btn := range row.Buttons {
			if cbBtn, ok := btn.(*tg.KeyboardButtonCallback); ok {
				if string(cbBtn.Data) == "exp:burn:100" {
					foundBurn = true
				}
			}
		}
	}
	if !foundBurn {
		t.Errorf("expected burn button callback to be found")
	}

	// Verify count of buttons equals count of allowed expiries
	totalButtons := 0
	for _, row := range markup.Rows {
		totalButtons += len(row.Buttons)
	}
	if totalButtons != 3 {
		t.Errorf("expected 3 buttons, got %d", totalButtons)
	}
}

func TestHTMLEscape(t *testing.T) {
	input := "<script>alert('xss&fun')</script>"
	expected := "&lt;script&gt;alert('xss&amp;fun')&lt;/script&gt;"
	res := htmlEscape(input)
	if res != expected {
		t.Errorf("expected %q, got %q", expected, res)
	}
}

func TestTelegramAllowedUsers(t *testing.T) {
	cfg := &config.Config{
		TelegramAllowedUsers: []int64{12345, 67890},
	}
	if !cfg.IsTelegramUserAllowed(12345) {
		t.Errorf("expected user 12345 to be allowed")
	}
	if !cfg.IsTelegramUserAllowed(67890) {
		t.Errorf("expected user 67890 to be allowed")
	}
	if cfg.IsTelegramUserAllowed(99999) {
		t.Errorf("expected user 99999 to be blocked")
	}

	// Empty whitelist allows all
	openCfg := &config.Config{TelegramAllowedUsers: nil}
	if !openCfg.IsTelegramUserAllowed(99999) {
		t.Errorf("expected any user to be allowed when whitelist is empty")
	}
}

func TestIsValidTelegramButtonURL(t *testing.T) {
	tests := []struct {
		url   string
		valid bool
	}{
		{"http://localhost:8080/f/abc", false},
		{"http://127.0.0.1:8080/f/abc", false},
		{"http://0.0.0.0:8080/f/abc", false},
		{"http://192.168.1.10:8080/f/abc", false},
		{"http://10.0.0.1:8080/f/abc", false},
		{"http://172.16.0.5:8080/f/abc", false},
		{"http://myserver.local:8080/f/abc", false},
		{"not-a-url", false},
		{"ftp://example.com/file", false},
		{"https://warpstash.example.com/f/abc", true},
		{"http://warpstash.org/f/abc", true},
		{"https://1.1.1.1:8443/f/abc", true},
	}

	for _, tc := range tests {
		got := isValidTelegramButtonURL(tc.url)
		if got != tc.valid {
			t.Errorf("isValidTelegramButtonURL(%q) = %v; want %v", tc.url, got, tc.valid)
		}
	}
}

func TestIsNilMarkup(t *testing.T) {
	// Literal nil
	if !isNilMarkup(nil) {
		t.Errorf("expected isNilMarkup(nil) to be true")
	}

	// Typed nil pointer inside interface (Go typed nil interface trap)
	var typedNil *tg.ReplyInlineMarkup
	var iface tg.ReplyMarkupClass = typedNil
	if !isNilMarkup(iface) {
		t.Errorf("expected isNilMarkup(typedNil) to be true")
	}

	// Valid non-nil markup
	validMarkup := &tg.ReplyInlineMarkup{
		Rows: []tg.KeyboardButtonRow{},
	}
	if isNilMarkup(validMarkup) {
		t.Errorf("expected isNilMarkup(validMarkup) to be false")
	}
}

func TestFormatSuccessMessage(t *testing.T) {
	filename := "report.pdf"
	sizeBytes := int64(1048576) // 1.0 MB
	expiryDisplay := "24h"
	fileURL := "https://warpstash.example.com/f/abc12345.pdf"

	t.Run("with inline download button", func(t *testing.T) {
		msg := formatSuccessMessage(filename, sizeBytes, expiryDisplay, fileURL, true)

		if !strings.Contains(msg, "File Stashed Successfully!") {
			t.Errorf("expected success header in message")
		}
		if !strings.Contains(msg, "report.pdf") {
			t.Errorf("expected filename in message")
		}
		if !strings.Contains(msg, "24h") {
			t.Errorf("expected retention in message")
		}
		// Download link should be omitted from text because inline button is provided
		if strings.Contains(msg, "Download Link:") || strings.Contains(msg, fileURL) {
			t.Errorf("expected download link to be omitted from text when inline button is provided, got: %s", msg)
		}
		// Delete link should be omitted from text
		if strings.Contains(msg, "Delete:") || strings.Contains(msg, "web link") {
			t.Errorf("expected delete link to be omitted from text, got: %s", msg)
		}
	})

	t.Run("without inline download button (fallback)", func(t *testing.T) {
		localURL := "http://localhost:8080/f/abc12345.pdf"
		msg := formatSuccessMessage(filename, sizeBytes, expiryDisplay, localURL, false)

		if !strings.Contains(msg, "File Stashed Successfully!") {
			t.Errorf("expected success header in message")
		}
		if !strings.Contains(msg, "report.pdf") {
			t.Errorf("expected filename in message")
		}
		// Download link must be present in text
		if !strings.Contains(msg, "Download Link:") || !strings.Contains(msg, localURL) {
			t.Errorf("expected download link to be included in text when inline button is absent, got: %s", msg)
		}
		// Delete link should still be omitted from text
		if strings.Contains(msg, "Delete:") || strings.Contains(msg, "web link") {
			t.Errorf("expected delete link to be omitted from text, got: %s", msg)
		}
	})
}

