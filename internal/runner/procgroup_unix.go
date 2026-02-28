//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// setupProcessGroup puts the child process in its own process group and
// overrides cmd.Cancel to kill the entire group on context cancellation.
// This prevents orphan/zombie grandchildren when idle_timeout or max_runtime fires.
func setupProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}
