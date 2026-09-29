// Package errs has methods that the runtime and the errors package call,
// which no test calls.
package errs

// NotFound is an error that wraps another.
type NotFound struct{ Err error }

// Error implements error.
func (e *NotFound) Error() string { return "not found" }

// Unwrap follows the errors package's convention.
func (e *NotFound) Unwrap() error { return e.Err }

// Code is not an error: its Error takes an argument.
type Code int

// Error has the name of error's method but not its signature.
func (c Code) Error(verbose bool) string { return "code" }
