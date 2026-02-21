package sentinel

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ppiankov/runforge/internal/ingest"
	"github.com/ppiankov/runforge/internal/task"
)

// ProcessResult records the outcome of a WO execution.
type ProcessResult struct {
	WOID       string         `json:"wo_id"`
	State      task.TaskState `json:"state"`
	RunnerUsed string         `json:"runner_used,omitempty"`
	Duration   time.Duration  `json:"duration,omitempty"`
	Error      string         `json:"error,omitempty"`
	OutputDir  string         `json:"output_dir,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	EndedAt    time.Time      `json:"ended_at"`
}

// ExecFunc is the function signature for executing a WO payload.
// It decouples the sentinel from the cli package to avoid import cycles.
type ExecFunc func(ctx context.Context, payload *ingest.IngestPayload, profileName string) *task.TaskResult

// Processor handles the lifecycle of a single WO execution.
type Processor struct {
	dirs       Dirs
	profileDir string
	execFn     ExecFunc
}

// NewProcessor creates a WO processor.
func NewProcessor(dirs Dirs, profileDir string, execFn ExecFunc) *Processor {
	return &Processor{
		dirs:       dirs,
		profileDir: profileDir,
		execFn:     execFn,
	}
}

// Process validates, executes, and records the result for a single WO payload.
func (p *Processor) Process(ctx context.Context, payloadPath string) error {
	name := filepath.Base(payloadPath)
	woID := name[:len(name)-5] // strip .json

	slog.Info("processing work order", "wo", woID, "path", payloadPath)

	// 1. Load and validate payload.
	payload, err := ingest.Load(payloadPath)
	if err != nil {
		slog.Error("invalid payload", "wo", woID, "error", err)
		return p.writeFailed(woID, fmt.Sprintf("invalid payload: %v", err))
	}

	// 2. Move to processing/ (atomic where possible).
	procPath := filepath.Join(p.dirs.Processing, name)
	if err := moveFile(payloadPath, procPath); err != nil {
		return fmt.Errorf("move to processing: %w", err)
	}

	// 3. Build ephemeral chainwatch profile.
	profile := ingest.BuildProfile(payload.Constraints, payload.WOID)
	profilePath, err := ingest.WriteProfile(profile, p.profileDir)
	if err != nil {
		_ = moveFile(procPath, filepath.Join(p.dirs.Failed, name))
		return p.writeFailed(woID, fmt.Sprintf("write profile: %v", err))
	}
	defer func() { _ = os.Remove(profilePath) }()

	// 4. Execute via runner cascade.
	start := time.Now()
	result := p.execFn(ctx, payload, profile.Name)
	elapsed := time.Since(start)

	// 5. Record result.
	pr := ProcessResult{
		WOID:       woID,
		State:      result.State,
		RunnerUsed: result.RunnerUsed,
		Duration:   elapsed,
		Error:      result.Error,
		OutputDir:  result.OutputDir,
		StartedAt:  start,
		EndedAt:    time.Now(),
	}

	if result.State == task.StateCompleted {
		slog.Info("work order completed", "wo", woID, "runner", result.RunnerUsed, "duration", elapsed.Round(time.Second))
		if err := p.writeResult(p.dirs.Completed, woID, pr); err != nil {
			return err
		}
	} else {
		slog.Warn("work order failed", "wo", woID, "error", result.Error)
		if err := p.writeResult(p.dirs.Failed, woID, pr); err != nil {
			return err
		}
	}

	// 6. Remove from processing/.
	_ = os.Remove(procPath)

	return nil
}

// writeFailed writes a failure result without execution.
func (p *Processor) writeFailed(woID, errMsg string) error {
	pr := ProcessResult{
		WOID:      woID,
		State:     task.StateFailed,
		Error:     errMsg,
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
	}
	return p.writeResult(p.dirs.Failed, woID, pr)
}

// writeResult writes a ProcessResult to the target directory.
func (p *Processor) writeResult(dir, woID string, pr ProcessResult) error {
	data, err := json.MarshalIndent(pr, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	path := filepath.Join(dir, woID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write result: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename result: %w", err)
	}
	return nil
}

// moveFile moves a file from src to dst. Falls back to copy+remove
// when rename fails (cross-device, bind mounts).
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Fallback: copy + remove.
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return err
	}
	return os.Remove(src)
}
