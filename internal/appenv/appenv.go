// Package appenv reads toyo-cli settings from the process environment.
package appenv

import "os"

func Catalog() string      { return os.Getenv("TOYO_CATALOG") }
func Region() string       { return os.Getenv("TOYO_REGION") }
func Theme() string        { return os.Getenv("TOYO_THEME") }
func Pager() string        { return os.Getenv("TOYO_PAGER") }
func Root() string         { return os.Getenv("TOYO_ROOT") }
func Python() string       { return os.Getenv("TOYO_PYTHON") }
func J2534Library() string { return os.Getenv("TOYO_J2534_LIBRARY") }
func J2534Device() string  { return os.Getenv("TOYO_J2534_DEVICE") }
