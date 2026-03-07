package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ppiankov/tokencontrol/internal/task"
)

func TestRunCodexQuotaPreflight_UsesHistoryAndHeuristic(t *testing.T) {
	historyDir := t.TempDir()
	writeRunReport(t, historyDir, "20260307-120000", task.RunReport{
		Results: map[string]*task.TaskResult{
			"vectorpad-WO05": {
				RunnerUsed: "codex",
				TokensUsed: &task.TokenUsage{TotalTokens: 3_000_000},
			},
		},
	})

	tasks := []task.Task{
		{ID: "vectorpad-WO05", Runner: "codex", Difficulty: "complex", Score: 16},
		{ID: "vectorpad-WO02", Runner: "codex", Difficulty: "simple"},
	}
	cfg := quotaPreflightConfig{
		RemainingTokens: 10_000_000,
		SafetyFactor:    1.0,
		Enforce:         true,
		LookbackRuns:    10,
	}

	result, err := runCodexQuotaPreflight(tasks, nil, cfg, historyDir)
	if err != nil {
		t.Fatalf("unexpected preflight error: %v", err)
	}
	if result == nil {
		t.Fatal("expected preflight result")
	}
	if result.CodexTasks != 2 {
		t.Fatalf("codex tasks: got %d, want 2", result.CodexTasks)
	}
	if result.HistoricalTasks != 1 {
		t.Fatalf("history hits: got %d, want 1", result.HistoricalTasks)
	}
	if result.HeuristicTasks != 1 {
		t.Fatalf("heuristic tasks: got %d, want 1", result.HeuristicTasks)
	}
	if result.EstimatedTokens != 4_800_000 {
		t.Fatalf("estimated tokens: got %d, want 4800000", result.EstimatedTokens)
	}
	if result.RequiredTokens != 4_800_000 {
		t.Fatalf("required tokens: got %d, want 4800000", result.RequiredTokens)
	}
	if result.ShortfallTokens != 0 {
		t.Fatalf("shortfall: got %d, want 0", result.ShortfallTokens)
	}
}

func TestRunCodexQuotaPreflight_EnforceBlocksOnShortfall(t *testing.T) {
	tasks := []task.Task{{ID: "vectorpad-WO05", Runner: "codex", Difficulty: "complex", Score: 16}}
	cfg := quotaPreflightConfig{
		RemainingTokens: 2_000_000,
		SafetyFactor:    1.0,
		Enforce:         true,
	}

	result, err := runCodexQuotaPreflight(tasks, nil, cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected preflight error on shortfall")
	}
	if result == nil {
		t.Fatal("expected preflight result on error")
	}
	if result.ShortfallTokens <= 0 {
		t.Fatalf("expected positive shortfall, got %d", result.ShortfallTokens)
	}
}

func TestRunCodexQuotaPreflight_WarnModeAllowsShortfall(t *testing.T) {
	tasks := []task.Task{{ID: "vectorpad-WO05", Runner: "codex", Difficulty: "complex", Score: 16}}
	cfg := quotaPreflightConfig{
		RemainingTokens: 2_000_000,
		SafetyFactor:    1.0,
		Enforce:         false,
	}

	result, err := runCodexQuotaPreflight(tasks, nil, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("warn mode should not block, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected preflight result")
	}
	if result.ShortfallTokens <= 0 {
		t.Fatalf("expected positive shortfall, got %d", result.ShortfallTokens)
	}
}

func TestRunCodexQuotaPreflight_RecognizesCodexProfiles(t *testing.T) {
	profiles := map[string]*task.RunnerProfileConfig{
		"codex-pro": {Type: "codex"},
	}
	tasks := []task.Task{{ID: "vectorpad-WO03", Runner: "codex-pro", Difficulty: "medium"}}
	cfg := quotaPreflightConfig{
		RemainingTokens: 10_000_000,
		SafetyFactor:    1.0,
		Enforce:         true,
	}

	result, err := runCodexQuotaPreflight(tasks, profiles, cfg, t.TempDir())
	if err != nil {
		t.Fatalf("unexpected preflight error: %v", err)
	}
	if result == nil {
		t.Fatal("expected preflight result")
	}
	if result.CodexTasks != 1 {
		t.Fatalf("codex tasks: got %d, want 1", result.CodexTasks)
	}
}

func writeRunReport(t *testing.T, root, runID string, report task.RunReport) {
	t.Helper()

	runDir := filepath.Join(root, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("create run dir: %v", err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "report.json"), data, 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
}
