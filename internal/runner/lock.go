package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const lockPollInterval = 5 * time.Second

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
		// corrupt or empty lock file — remove and retry
		slog.Warn("removing corrupt lock file", "repo", repoDir, "error", readErr)
		if rmErr := os.Remove(lockPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return fmt.Errorf("remove corrupt lock: %w", rmErr)
		}
		if err := writeLock(lockPath, &info); err != nil {
			return fmt.Errorf("acquire after corrupt removal: %w", err)
		}
		return nil
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

// WaitAndAcquire retries Acquire until the lock is obtained or ctx is cancelled.
func WaitAndAcquire(ctx context.Context, repoDir, taskID string) error {
	for {
		err := Acquire(repoDir, taskID)
		if err == nil {
			return nil
		}
		slog.Debug("waiting for repo lock", "repo", repoDir, "task", taskID, "holder", err)
		select {
		case <-ctx.Done():
			return fmt.Errorf("lock wait cancelled: %w", ctx.Err())
		case <-time.After(lockPollInterval):
		}
	}
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

// writeLock atomically creates the lock file.
// Writes JSON to a temp file first, then hard-links it into place so
// readers never see partial content. Link fails with ErrExist when the
// lock is already held, preserving the exclusive-creation semantics.
func writeLock(path string, info *LockInfo) error {
	tmp := fmt.Sprintf("%s.tmp.%d.%d", path, os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	encErr := json.NewEncoder(f).Encode(info)
	closeErr := f.Close()
	if encErr != nil {
		_ = os.Remove(tmp)
		return encErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}

	if err := os.Link(tmp, path); err != nil {
		_ = os.Remove(tmp)
		if errors.Is(err, os.ErrExist) {
			return os.ErrExist
		}
		return err
	}
	_ = os.Remove(tmp)
	return nil
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
