package warpstash

import (
	"io"
	"testing"
)

func TestStaticFSEmbed(t *testing.T) {
	fsys, err := StaticFS()
	if err != nil {
		t.Fatalf("StaticFS() failed: %v", err)
	}

	f, err := fsys.Open("index.html")
	if err != nil {
		t.Fatalf("failed to open embedded index.html: %v", err)
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("failed to read embedded index.html: %v", err)
	}

	if len(content) == 0 {
		t.Errorf("embedded index.html is empty")
	}
}
