// Package appenv reads toyo-cli settings from the process environment.
// TOYO_* names win over their TOYOTA_* aliases.
package appenv

import "os"

// First returns the first non-empty environment value among keys.
func First(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func Catalog() string      { return First("TOYO_CATALOG", "TOYOTA_CATALOG") }
func Region() string       { return First("TOYO_REGION", "TOYOTA_REGION") }
func Theme() string        { return First("TOYO_THEME", "TOYOTA_THEME") }
func Pager() string        { return First("TOYO_PAGER", "TOYOTA_PAGER") }
func Root() string         { return First("TOYO_ROOT", "TOYOTA_DIAGNOSTICS_ROOT") }
func Python() string       { return First("TOYO_PYTHON", "TOYOTA_PYTHON") }
func J2534Library() string { return First("TOYO_J2534_LIBRARY", "TOYOTA_J2534_LIBRARY") }
func J2534Device() string  { return First("TOYO_J2534_DEVICE", "TOYOTA_J2534_DEVICE") }
