package runner

import (
	"context"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

const prescanTimeout = 30 * time.Second

var (
	prescanOnce   sync.Once
	prescanAvail  bool
	safeRunnerSet = map[string]struct{}{"claude": {}, "cline": {}}
)

// PreScan runs pastewatch-cli scan --dir <repoDir> --check to detect secrets.
// Returns true if secrets are detected, false if clean or pastewatch unavailable.
func PreScan(ctx context.Context, repoDir string) (bool, error) {
	prescanOnce.Do(func() {
		if _, err := lookupPastewatch(); err == nil {
			prescanAvail = true
		} else {
			slog.Warn("pastewatch-cli not found, pre-dispatch secret scan disabled")
		}
	})

	if !prescanAvail {
		return false, nil
	}

	return preScan(ctx, repoDir)
}

func lookupPastewatch() (string, error) {
	return exec.LookPath("pastewatch-cli")
}

func preScan(ctx context.Context, repoDir string) (bool, error) {
	scanCtx, cancel := context.WithTimeout(ctx, prescanTimeout)
	defer cancel()

	cmd := exec.CommandContext(scanCtx, "pastewatch-cli", "scan", "--dir", repoDir, "--format", "json", "--fail-on-severity", "low")
	err := cmd.Run()
	if err == nil {
		return false, nil // exit 0 = no secrets
	}

	// non-zero exit = secrets detected (or scan error)
	if exitErr, ok := err.(*exec.ExitError); ok {
		slog.Info("secrets detected in repo", "dir", repoDir, "exit_code", exitErr.ExitCode())
		return true, nil
	}

	// unexpected error (timeout, signal, etc.)
	slog.Warn("pre-scan failed", "dir", repoDir, "error", err)
	return false, nil
}

// IsSafeRunner returns true if the runner has structural secret protection
// (e.g., PreToolUse hooks that block secret leakage).
func IsSafeRunner(name string) bool {
	_, ok := safeRunnerSet[name]
	return ok
}
