package web

import (
	"io/fs"
	"testing"
)

func TestFSHasIndex(t *testing.T) {
	b, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Skipf("frontend not built (run `make build-frontend`): %v", err)
	}
	if len(b) == 0 {
		t.Fatal("embedded index.html is empty")
	}
}
