package api

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
)

// pageDataOpen is the opening tag of the placeholder in web/index.html; whatever sits between it and
// the next </script> is replaced, so reformatting the HTML can't break injection.
const pageDataOpen = `<script id="page-data" type="application/json">`

// renderSPA writes the SPA's index.html with the given status. data, when non-nil, is injected as JSON
// into the page-data script so the client router can render burn, 404, 410 and 500 views.
// Returns false when no frontend build is embedded.
func renderSPA(w http.ResponseWriter, staticFS fs.FS, status int, data any) bool {
	if staticFS == nil {
		return false
	}
	page, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		return false
	}
	if data != nil {
		// json.Marshal escapes <, > and &, so user-supplied names can't close the script tag.
		if b, err := json.Marshal(data); err == nil {
			if i := bytes.Index(page, []byte(pageDataOpen)); i >= 0 {
				start := i + len(pageDataOpen)
				if end := bytes.Index(page[start:], []byte("</script>")); end >= 0 {
					page = bytes.Join([][]byte{page[:start], b, page[start+end:]}, nil)
				}
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(page)
	return true
}
