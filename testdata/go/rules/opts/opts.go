// Package opts has no tests; its option constructors are left out of
// untested_exports when they only set fields.
package opts

import (
	"errors"
	"time"
)

type settings struct {
	timeout time.Duration
	retries int
	name    string
}

// Option sets a field of the settings.
type Option func(*settings)

// WithTimeout only assigns a field of its parameter: a functional option.
func WithTimeout(d time.Duration) Option {
	return func(o *settings) { o.timeout = d }
}

// WithRetry validates and returns an error, so it is more than a setter.
func WithRetry(n int) func(*settings) error {
	return func(o *settings) error {
		if n < 0 {
			return errors.New("negative retries")
		}
		o.retries = n
		return nil
	}
}

// Builder makes options from a method.
type Builder struct{}

// WithName is an option constructor declared as a method.
func (Builder) WithName(n string) Option {
	return func(o *settings) { o.name = n }
}

// Apply runs the options.
func Apply(opts ...Option) {
	var s settings
	for _, opt := range opts {
		opt(&s)
	}
}
