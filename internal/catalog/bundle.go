package catalog

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Bundle is an optional sidecar for the original recovered source tree: a
// directory of gzip members (the current generator layout) or a zip of
// JSON members (the historical DEFAULT_REGISTRY artifact).
type Bundle struct {
	path string
	zip  *zip.ReadCloser
}

func OpenBundle(path string) (*Bundle, error) {
	if path == "" {
		return nil, fmt.Errorf("no source bundle configured")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return &Bundle{path: abs}, nil
	}
	zr, err := zip.OpenReader(abs)
	if err != nil {
		return nil, fmt.Errorf("source bundle %s is neither a directory nor a zip archive: %w", abs, err)
	}
	return &Bundle{path: abs, zip: zr}, nil
}

func (b *Bundle) Close() error {
	if b == nil || b.zip == nil {
		return nil
	}
	err := b.zip.Close()
	b.zip = nil
	return err
}

func (b *Bundle) Read(member string) ([]byte, error) {
	if b == nil {
		return nil, fmt.Errorf("no source bundle attached")
	}
	member = strings.TrimPrefix(filepath.ToSlash(member), "/")
	names := []string{member}
	if strings.HasSuffix(member, ".gz") {
		names = append(names, strings.TrimSuffix(member, ".gz"))
	} else {
		names = append(names, member+".gz")
	}
	base := documentName(member)
	if base != "" && base != member {
		names = append(names, base, base+".gz")
	}
	var last error
	seen := map[string]bool{}
	for _, name := range names {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		raw, err := b.readNamed(name)
		if err != nil {
			last = err
			continue
		}
		return maybeGunzip(raw)
	}
	if last == nil {
		last = fmt.Errorf("bundle member %q not found", member)
	}
	return nil, last
}

func (b *Bundle) readNamed(name string) ([]byte, error) {
	if b.zip != nil {
		for _, file := range b.zip.File {
			if file.Name == name {
				rc, err := file.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("bundle member %q not found", name)
	}
	data, err := os.ReadFile(filepath.Join(b.path, filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func maybeGunzip(data []byte) ([]byte, error) {
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return io.ReadAll(r)
	}
	return data, nil
}

func (b *Bundle) Index() (map[string]any, error) {
	return b.Document("index.json")
}

func (b *Bundle) Document(member string) (map[string]any, error) {
	raw, err := b.Read(member)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, fmt.Errorf("bundle member %s is not an object", member)
	}
	return document, nil
}

// AttachBundle opens an optional source bundle used to complete index fields
// the sqlite exporter does not copy, and as a fallback document store.
func (d *DB) AttachBundle(path string) error {
	if path == "" {
		return nil
	}
	b, err := OpenBundle(path)
	if err != nil {
		return err
	}
	if d.bundle != nil {
		_ = d.bundle.Close()
	}
	d.bundle = b
	return nil
}
