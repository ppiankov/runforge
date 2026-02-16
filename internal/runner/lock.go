package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const lockFileName = ".runforge.lock"

// LockInfo describes the owner of a repo lock.
type LockInfo struct {
	PID       int       `json:"pid"`
	TaskID    string    `json:"task_id"`
	StartedAt time.Time `json:"started_at"`
}

// Acquire creates a lock file in repoDir. Returns nil on success.
// If the lock exists and the owning PID is dead, the stale lock is reclaimed.
func Acquire(repoDir, taskID string) error {
	lockPath := filepath.Join(repoDir, lockFileName)

	info := LockInfo{
		PID:       os.Getpid(),
		TaskID:    taskID,
		StartedAt: time.Now(),
	}

	err := writeLock(lockPath, &info)
	if err == nil {
		return nil
	}

	if !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create lock %s: %w", lockPath, err)
	}

	// lock exists — check if stale
	existing, readErr := ReadLock(repoDir)
	if readErr != nil {
		return fmt.Errorf("repo %s is locked (could not read lock: %v)", repoDir, readErr)
	}

	if isProcessAlive(existing.PID) {
		return fmt.Errorf("repo locked by PID %d since %s (task %s)",
			existing.PID, existing.StartedAt.Format(time.RFC3339), existing.TaskID)
	}

	// stale lock — reclaim
	slog.Warn("reclaiming stale lock", "repo", repoDir, "stale_pid", existing.PID, "task", existing.TaskID)
	if err := os.Remove(lockPath); err != nil {
		return fmt.Errorf("remove stale lock: %w", err)
	}

	if err := writeLock(lockPath, &info); err != nil {
		return fmt.Errorf("acquire after stale removal: %w", err)
	}

	return nil
}

// Release removes the lock file from repoDir. It is idempotent.
func Release(repoDir string) {
	lockPath := filepath.Join(repoDir, lockFileName)
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("failed to release lock", "path", lockPath, "error", err)
	}
}

// ReadLock reads the lock file from repoDir.
func ReadLock(repoDir string) (*LockInfo, error) {
	lockPath := filepath.Join(repoDir, lockFileName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, err
	}

	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("parse lock: %w", err)
	}

	return &info, nil
}

// writeLock atomically creates the lock file using O_CREATE|O_EXCL.
func writeLock(path string, info *LockInfo) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	encErr := json.NewEncoder(f).Encode(info)
	closeErr := f.Close()
	if encErr != nil {
		return encErr
	}
	return closeErr
}

// isProcessAlive checks if a process with the given PID exists and is running.
func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// signal 0 checks existence without actually sending a signal
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
