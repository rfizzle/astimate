//go:build !unix

package runner

import "os/exec"

// killGroup leaves cmd as it is: without process groups, canceling its
// context kills only the process itself.
func killGroup(*exec.Cmd) {}
