//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

// killGroup starts cmd in its own process group and makes canceling its
// context kill the whole group, so an agent's children (sh starts the
// agent, the agent starts go test) die with it.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
