package dashboard

import (
	"io/fs"
	"testing"
)

func TestFSReturnsEmbeddedDistSubtree(t *testing.T) {
	dist, err := FS()
	if err != nil {
		t.Fatalf("open embedded dist fs: %v", err)
	}
	if _, err := fs.ReadFile(dist, "index.html"); err != nil {
		t.Fatalf("expected embedded index.html, got %v", err)
	}
}
