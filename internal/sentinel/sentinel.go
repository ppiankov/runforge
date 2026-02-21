package sentinel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ppiankov/runforge/internal/task"
)

// debounceDefault is the debounce interval for file events.
const debounceDefault = 200 * time.Millisecond

// pollDefault is the polling interval when fsnotify is unavailable.
const pollDefault = 5 * time.Second

// Config holds sentinel daemon configuration.
type Config struct {
	IngestedDir string        // where approved WOs land
	StateDir    string        // sentinel working state
	ProfileDir  string        // chainwatch profile directory
	PollMode    bool          // fall back to polling if fsnotify unavailable
	MaxRuntime  time.Duration // per-WO execution timeout
	IdleTimeout time.Duration // runner idle timeout
	Runner      string        // primary runner
	Fallbacks   []string      // fallback runners
	RepoDir     string        // target directory for remediation
	ExecFn      ExecFunc      // execution function (injected by cli to break import cycle)
}

// Sentinel watches for approved WOs and auto-executes them.
type Sentinel struct {
	cfg       Config
	dirs      Dirs
	processor *Processor
}

// New creates a sentinel with validated configuration.
func New(cfg Config) (*Sentinel, error) {
	if cfg.IngestedDir == "" {
		return nil, fmt.Errorf("ingested directory is required")
	}
	if cfg.StateDir == "" {
		return nil, fmt.Errorf("state directory is required")
	}
	if cfg.ExecFn == nil {
		return nil, fmt.Errorf("execution function is required")
	}
	if cfg.MaxRuntime == 0 {
		cfg.MaxRuntime = 30 * time.Minute
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.Runner == "" {
		cfg.Runner = "claude"
	}
	if cfg.RepoDir == "" {
		cfg.RepoDir = "."
	}
	if cfg.ProfileDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine home dir: %w", err)
		}
		cfg.ProfileDir = filepath.Join(home, ".chainwatch", "profiles")
	}

	dirs := NewDirs(cfg.IngestedDir, cfg.StateDir)
	processor := NewProcessor(dirs, cfg.ProfileDir, cfg.ExecFn)

	return &Sentinel{
		cfg:       cfg,
		dirs:      dirs,
		processor: processor,
	}, nil
}

// Run starts the sentinel daemon. Blocks until ctx is cancelled.
func (s *Sentinel) Run(ctx context.Context) error {
	// Create directory structure.
	if err := EnsureDirs(s.dirs); err != nil {
		return fmt.Errorf("ensure directories: %w", err)
	}

	// Acquire PID lock.
	pidPath := filepath.Join(s.cfg.StateDir, "sentinel.pid")
	if err := acquirePIDLock(pidPath); err != nil {
		return fmt.Errorf("acquire PID lock: %w", err)
	}
	defer func() { _ = os.Remove(pidPath) }()

	slog.Info("sentinel starting",
		"ingested", s.cfg.IngestedDir,
		"state", s.cfg.StateDir,
		"runner", s.cfg.Runner,
		"max_runtime", s.cfg.MaxRuntime,
	)

	// Recovery: move orphaned processing files to failed.
	if err := s.recoverOrphans(); err != nil {
		return fmt.Errorf("recover orphans: %w", err)
	}

	// Process any existing ingested files.
	if err := s.scanExisting(ctx); err != nil {
		return fmt.Errorf("scan existing: %w", err)
	}

	// Start watching for new files.
	if s.cfg.PollMode {
		return s.runPollWatcher(ctx)
	}
	return s.runFSWatcher(ctx)
}

// scanExisting processes any .json files already in the ingested directory.
func (s *Sentinel) scanExisting(ctx context.Context) error {
	entries, err := os.ReadDir(s.cfg.IngestedDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !isPayloadFile(e.Name()) {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		path := filepath.Join(s.cfg.IngestedDir, e.Name())
		if err := s.processor.Process(ctx, path); err != nil {
			slog.Error("process existing", "file", e.Name(), "error", err)
		}
	}
	return nil
}

// runFSWatcher watches the ingested directory using fsnotify.
func (s *Sentinel) runFSWatcher(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	if err := watcher.Add(s.cfg.IngestedDir); err != nil {
		return fmt.Errorf("watch dir: %w", err)
	}

	slog.Info("watching for new payloads", "mode", "fsnotify", "dir", s.cfg.IngestedDir)

	var mu sync.Mutex
	pending := make(map[string]*time.Timer)

	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			for _, t := range pending {
				t.Stop()
			}
			mu.Unlock()
			slog.Info("sentinel stopped")
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if !event.Has(fsnotify.Create) {
				continue
			}
			if !isPayloadFile(filepath.Base(event.Name)) {
				continue
			}

			path := event.Name
			mu.Lock()
			if t, exists := pending[path]; exists {
				t.Stop()
			}
			pending[path] = time.AfterFunc(debounceDefault, func() {
				if err := s.processor.Process(ctx, path); err != nil {
					slog.Error("process payload", "file", filepath.Base(path), "error", err)
				}
				mu.Lock()
				delete(pending, path)
				mu.Unlock()
			})
			mu.Unlock()

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			slog.Error("watcher error", "error", err)
		}
	}
}

// runPollWatcher watches the ingested directory using polling.
func (s *Sentinel) runPollWatcher(ctx context.Context) error {
	slog.Info("watching for new payloads", "mode", "poll", "dir", s.cfg.IngestedDir, "interval", pollDefault)

	seen := make(map[string]bool)
	ticker := time.NewTicker(pollDefault)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("sentinel stopped")
			return nil
		case <-ticker.C:
			entries, err := os.ReadDir(s.cfg.IngestedDir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if e.IsDir() || !isPayloadFile(e.Name()) {
					continue
				}
				path := filepath.Join(s.cfg.IngestedDir, e.Name())
				if seen[path] {
					continue
				}
				seen[path] = true
				if err := s.processor.Process(ctx, path); err != nil {
					slog.Error("process payload", "file", e.Name(), "error", err)
				}
			}
		}
	}
}

// recoverOrphans moves files left in processing/ to failed results.
func (s *Sentinel) recoverOrphans() error {
	entries, err := os.ReadDir(s.dirs.Processing)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if e.IsDir() || !isPayloadFile(e.Name()) {
			continue
		}
		woID := e.Name()[:len(e.Name())-5] // strip .json
		slog.Warn("recovering orphaned WO", "wo", woID)

		pr := ProcessResult{
			WOID:      woID,
			State:     task.StateFailed,
			Error:     "interrupted: WO was processing when sentinel stopped",
			StartedAt: time.Now(),
			EndedAt:   time.Now(),
		}
		data, _ := json.MarshalIndent(pr, "", "  ")
		path := filepath.Join(s.dirs.Failed, woID+".json")
		_ = os.WriteFile(path, data, 0o600)

		_ = os.Remove(filepath.Join(s.dirs.Processing, e.Name()))
	}
	return nil
}

// isPayloadFile returns true if the filename is a .json file (not a .tmp).
func isPayloadFile(name string) bool {
	return strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".tmp")
}

// acquirePIDLock writes the current PID and checks for stale locks.
func acquirePIDLock(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	if data, err := os.ReadFile(path); err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil {
			if process, err := os.FindProcess(pid); err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					return fmt.Errorf("another sentinel is running (PID %d)", pid)
				}
			}
		}
		_ = os.Remove(path)
	}

	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
}
