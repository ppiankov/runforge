//go:build windows

package runner

import "os/exec"

// setupProcessGroup is a no-op on Windows where Setpgid is unavailable.
// Process cleanup relies on cmd.Process.Kill() via the default Cancel behavior.
func setupProcessGroup(cmd *exec.Cmd) {
	// Windows does not support Unix process groups.
	// The default exec.CommandContext cancel (SIGKILL on process) is used instead.
}
