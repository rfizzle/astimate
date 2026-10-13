// Package rec has an unexported type behind an interface that a test holds.
package rec

// Recorder is the interface streamRecording implements.
type Recorder interface{ Emit(string) error }

type streamRecording struct{}

// Emit satisfies Recorder; no test calls it.
func (*streamRecording) Emit(string) error { return nil }

// New returns a Recorder.
func New() Recorder { return &streamRecording{} }
