//go:build !windows

package runner

import (
	"context"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestSetupProcessGroup_KillsChildren(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Spawn a shell that starts a background child (sleep 60) then sleeps itself.
	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 60 & sleep 60")
	setupProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	pid := cmd.Process.Pid

	// Verify the process is alive.
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("process %d not alive after start: %v", pid, err)
	}

	// Cancel context — should kill the entire process group.
	cancel()

	// Wait for the process to exit (cmd.Cancel fires SIGKILL on the group).
	_ = cmd.Wait()

	// Give the OS a moment to reap.
	time.Sleep(50 * time.Millisecond)

	// Verify the process group is dead.
	err := syscall.Kill(-pid, 0)
	if err == nil {
		t.Errorf("process group %d still alive after context cancel", pid)
	}
}

func TestSetupProcessGroup_SetsAttributes(t *testing.T) {
	cmd := exec.Command("echo", "test")
	setupProcessGroup(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr not set")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid not set to true")
	}
	if cmd.Cancel == nil {
		t.Error("Cancel function not set")
	}
}

func TestSetupProcessGroup_NormalExit(t *testing.T) {
	// Verify process group setup doesn't interfere with normal execution.
	cmd := exec.CommandContext(context.Background(), "echo", "hello")
	setupProcessGroup(cmd)

	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("expected clean exit, got: %v", err)
	}
	if len(out) == 0 {
		t.Error("expected output from echo")
	}
}

func TestSetupProcessGroup_ScriptTimeout(t *testing.T) {
	// Simulate idle timeout: cancel context while script is running.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 60")
	setupProcessGroup(cmd)

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error from timeout, got nil")
	}

	// Verify the process is dead.
	if cmd.Process != nil {
		if killErr := syscall.Kill(cmd.Process.Pid, 0); killErr == nil {
			// Attempt cleanup.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			// Wait for reap.
			time.Sleep(50 * time.Millisecond)
			if retryErr := syscall.Kill(cmd.Process.Pid, 0); retryErr == nil {
				t.Error("process still alive after timeout and cleanup")
			}
		}
	}
}

func TestSetupProcessGroup_CancelNilProcess(t *testing.T) {
	// Verify Cancel doesn't panic when Process is nil (cmd never started).
	cmd := exec.Command("nonexistent-binary-xyz")
	setupProcessGroup(cmd)

	err := cmd.Cancel()
	if err != nil {
		t.Errorf("expected nil error for nil process, got: %v", err)
	}
}
