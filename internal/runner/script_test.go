package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

func TestScriptRunner_Success(t *testing.T) {
	r := NewScriptRunner()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")

	tk := &task.Task{ID: "t1", Repo: "org/r", Prompt: "echo hello"}
	result := r.Run(context.Background(), tk, dir, outDir)

	if result.State != task.StateCompleted {
		t.Fatalf("expected COMPLETED, got %s (error: %s)", result.State, result.Error)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "output.log"))
	if err != nil {
		t.Fatalf("read output.log: %v", err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Errorf("expected 'hello' in output.log, got: %s", data)
	}
}

func TestScriptRunner_Failure(t *testing.T) {
	r := NewScriptRunner()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")

	tk := &task.Task{ID: "t1", Repo: "org/r", Prompt: "exit 1"}
	result := r.Run(context.Background(), tk, dir, outDir)

	if result.State != task.StateFailed {
		t.Fatalf("expected FAILED, got %s", result.State)
	}
	if !strings.Contains(result.Error, "exit") {
		t.Errorf("expected exit info in error, got: %s", result.Error)
	}
}

func TestScriptRunner_Timeout(t *testing.T) {
	r := NewScriptRunner()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	tk := &task.Task{ID: "t1", Repo: "org/r", Prompt: "sleep 10"}
	result := r.Run(ctx, tk, dir, outDir)

	if result.State != task.StateFailed {
		t.Fatalf("expected FAILED (timeout), got %s", result.State)
	}
}

func TestScriptRunner_WorkingDir(t *testing.T) {
	r := NewScriptRunner()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")

	tk := &task.Task{ID: "t1", Repo: "org/r", Prompt: "pwd"}
	result := r.Run(context.Background(), tk, dir, outDir)

	if result.State != task.StateCompleted {
		t.Fatalf("expected COMPLETED, got %s", result.State)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "output.log"))
	if err != nil {
		t.Fatalf("read output.log: %v", err)
	}
	if !strings.Contains(string(data), dir) {
		t.Errorf("expected working dir %q in output, got: %s", dir, data)
	}
}

func TestScriptRunner_StderrCapture(t *testing.T) {
	r := NewScriptRunner()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")

	tk := &task.Task{ID: "t1", Repo: "org/r", Prompt: "echo err >&2"}
	result := r.Run(context.Background(), tk, dir, outDir)

	if result.State != task.StateCompleted {
		t.Fatalf("expected COMPLETED, got %s", result.State)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "stderr.log"))
	if err != nil {
		t.Fatalf("read stderr.log: %v", err)
	}
	if !strings.Contains(string(data), "err") {
		t.Errorf("expected 'err' in stderr.log, got: %s", data)
	}
}

func TestScriptRunner_LastMsg(t *testing.T) {
	r := NewScriptRunner()
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")

	tk := &task.Task{ID: "t1", Repo: "org/r", Prompt: "echo first; echo second; echo third"}
	result := r.Run(context.Background(), tk, dir, outDir)

	if result.State != task.StateCompleted {
		t.Fatalf("expected COMPLETED, got %s", result.State)
	}
	if result.LastMsg != "third" {
		t.Errorf("expected LastMsg 'third', got %q", result.LastMsg)
	}
}
