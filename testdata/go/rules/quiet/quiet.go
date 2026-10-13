// Package quiet is rec's twin whose interface no test holds.
package quiet

// Sink is the interface fileSink implements.
type Sink interface{ Emit(string) error }

type fileSink struct{}

// Emit satisfies Sink; no test holds a Sink.
func (*fileSink) Emit(string) error { return nil }

// New returns a Sink.
func New() Sink { return &fileSink{} }
