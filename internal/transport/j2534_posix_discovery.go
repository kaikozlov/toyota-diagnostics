//go:build !windows

package transport

import (
	"os"
	"path/filepath"
	"runtime"
)

func jSystemProviders() []J2534Provider {
	name := "libopenmvci.so"
	dirs := []string{"/usr/local/lib", "/usr/lib", "/usr/lib/x86_64-linux-gnu", "/usr/lib/aarch64-linux-gnu"}
	if runtime.GOOS == "darwin" {
		name = "libopenmvci.dylib"
		dirs = []string{"/opt/homebrew/lib", "/usr/local/lib"}
	}
	dirs = append(filepath.SplitList(os.Getenv("LD_LIBRARY_PATH")), dirs...)
	if runtime.GOOS == "darwin" {
		dirs = append(filepath.SplitList(os.Getenv("DYLD_LIBRARY_PATH")), dirs...)
	}
	var out []J2534Provider
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		path := filepath.Join(dir, name)
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			out = append(out, J2534Provider{"OpenMVCI", path, "system-search"})
		}
	}
	// Resolve loader-configured locations too, without opening hardware.
	if lib, err := loadJ2534(name); err == nil {
		lib.Close()
		out = append(out, J2534Provider{"OpenMVCI", name, "system-search"})
	}
	return out
}
