package web

import (
	"io/fs"
	"strings"
	"testing"
)

// The Go server injects burn/error data into this tag (internal/api/spa.go); if the frontend drops or
// renames it, burn links and error pages silently lose their data.
func TestBuiltIndexHasPageDataTag(t *testing.T) {
	b, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Skipf("frontend not built (run `make build-frontend`): %v", err)
	}
	if !strings.Contains(string(b), `<script id="page-data" type="application/json">`) {
		t.Fatal(`built index.html lacks <script id="page-data" type="application/json">`)
	}
}
