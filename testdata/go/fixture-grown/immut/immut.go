// Package immut declares package-level variables that hold no mutable
// state, sentinel errors, an embedded file and build information, beside
// variables that do.
package immut

import (
	"embed"
	"errors"
	"fmt"
)

// ErrNotFound is returned for a key with no value.
var ErrNotFound = errors.New("not found")

// ErrBad is returned for an empty key.
var ErrBad = fmt.Errorf("bad: %d", 3)

// ErrWrapped wraps ErrNotFound, which is not a constant.
var ErrWrapped = fmt.Errorf("wrapped: %w", ErrNotFound)

//go:embed default.txt
var defaults embed.FS

var version = "dev"

var cfg = map[string]string{}

var count int

var mode = "fast"

// Lookup returns the configured value of key.
func Lookup(key string) (string, error) {
	count++
	if v, ok := cfg[key]; ok {
		return v, nil
	}
	if key == "" {
		return "", ErrBad
	}
	return "", ErrWrapped
}

// Describe reports the build, the mode and the embedded default.
func Describe() string {
	b, _ := defaults.ReadFile("default.txt")
	return version + " " + mode + " " + string(b)
}

// SetMode switches the mode.
func SetMode(m string) {
	mode = m
}
