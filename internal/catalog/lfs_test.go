package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenLFSPointerExplainsDownload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	if err := os.WriteFile(path, []byte("version https://git-lfs.github.com/spec/v1\noid sha256:abc\nsize 100\n"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := Open(path)
	if db != nil || !errors.Is(err, ErrLFSPointer) || !strings.Contains(err.Error(), "git lfs pull") {
		t.Fatalf("%v %v", db, err)
	}
}
