//go:build !windows && (!cgo || (!darwin && !linux))

package transport

import "errors"

func loadJ2534(string) (jLibrary, error) {
	return nil, errors.New("native J2534 requires a cgo-enabled build on Linux/macOS, or Windows")
}
