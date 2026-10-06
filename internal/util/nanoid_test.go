package util

import (
	"strings"
	"testing"
)

func TestGenerateID(t *testing.T) {
	lengths := []int{6, 8, 10, 12, 16, 21}
	for _, l := range lengths {
		id, err := GenerateID(l)
		if err != nil {
			t.Fatalf("unexpected error generating ID of length %d: %v", l, err)
		}
		if len(id) != l {
			t.Errorf("expected length %d, got %d (id: %s)", l, len(id), id)
		}
		for _, c := range id {
			if !strings.ContainsRune(DefaultAlphabet, c) {
				t.Errorf("unexpected character %c in generated ID: %s", c, id)
			}
		}
	}
}

func TestGenerateIDUniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 10000)
	for i := 0; i < 10000; i++ {
		id, err := GenerateID(10)
		if err != nil {
			t.Fatalf("error generating ID: %v", err)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("collision detected for ID %s at iteration %d", id, i)
		}
		seen[id] = struct{}{}
	}
}

func TestGenerateDeleteToken(t *testing.T) {
	token, err := GenerateDeleteToken()
	if err != nil {
		t.Fatalf("error generating delete token: %v", err)
	}
	if len(token) != 64 { // 32 bytes hex encoded = 64 hex characters
		t.Errorf("expected 64 characters, got %d", len(token))
	}
}

func BenchmarkGenerateID(b *testing.B) {
	for b.Loop() {
		_, _ = GenerateID(10)
	}
}
