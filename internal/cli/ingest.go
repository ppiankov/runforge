package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/ingest"
	"github.com/ppiankov/runforge/internal/runner"
	"github.com/ppiankov/runforge/internal/task"
)

func newIngestCmd() *cobra.Command {
	var (
		payloadPath string
		runnerName  string
		fallbacks   []string
		repoDir     string
		profileDir  string
		dryRun      bool
		maxRuntime  time.Duration
		idleTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Execute an approved work order from nullbot",
		Long: `Reads a nullbot IngestPayload JSON, maps WO constraints to a chainwatch
profile, builds a remediation prompt, and executes it through the runner
cascade with chainwatch enforcement.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIngest(payloadPath, runnerName, fallbacks, repoDir, profileDir, dryRun, maxRuntime, idleTimeout)
		},
	}

	cmd.Flags().StringVar(&payloadPath, "payload", "", "path to IngestPayload JSON (required)")
	cmd.Flags().StringVar(&runnerName, "runner", "claude", "primary runner")
	cmd.Flags().StringSliceVar(&fallbacks, "fallbacks", nil, "fallback runners (comma-separated)")
	cmd.Flags().StringVar(&repoDir, "repo-dir", ".", "target directory for remediation")
	cmd.Flags().StringVar(&profileDir, "profile-dir", "", "directory for ephemeral chainwatch profile (default: ~/.chainwatch/profiles)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show prompt and profile without executing")
	cmd.Flags().DurationVar(&maxRuntime, "max-runtime", 30*time.Minute, "per-task timeout")
	cmd.Flags().DurationVar(&idleTimeout, "idle-timeout", 5*time.Minute, "idle timeout for runner")
	_ = cmd.MarkFlagRequired("payload")

	return cmd
}

func runIngest(payloadPath, runnerName string, fallbacks []string, repoDir, profileDir string, dryRun bool, maxRuntime, idleTimeout time.Duration) error {
	// 1. Load payload.
	payload, err := ingest.Load(payloadPath)
	if err != nil {
		return fmt.Errorf("load payload: %w", err)
	}

	// 2. Build ephemeral chainwatch profile.
	profile := ingest.BuildProfile(payload.Constraints, payload.WOID)

	if profileDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("determine home dir: %w", err)
		}
		profileDir = filepath.Join(home, ".chainwatch", "profiles")
	}

	profilePath, err := ingest.WriteProfile(profile, profileDir)
	if err != nil {
		return fmt.Errorf("write profile: %w", err)
	}
	defer func() {
		// Clean up ephemeral profile after execution.
		_ = os.Remove(profilePath)
	}()

	// 3. Build prompt.
	prompt := ingest.BuildPrompt(payload)

	// Dry-run: show prompt and profile, no execution.
	if dryRun {
		fmt.Println("=== IngestPayload ===")
		fmt.Printf("WO:       %s\n", payload.WOID)
		fmt.Printf("Incident: %s\n", payload.IncidentID)
		fmt.Printf("Host:     %s\n", payload.Target.Host)
		fmt.Printf("Scope:    %s\n\n", payload.Target.Scope)

		fmt.Println("=== Chainwatch Profile ===")
		fmt.Printf("Path: %s\n", profilePath)
		fmt.Printf("Name: %s\n\n", profile.Name)

		fmt.Println("=== Prompt ===")
		fmt.Println(prompt)
		return nil
	}

	// 4. Execute via shared logic.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := IngestConfig{
		Runner:      runnerName,
		Fallbacks:   fallbacks,
		RepoDir:     repoDir,
		MaxRuntime:  maxRuntime,
		IdleTimeout: idleTimeout,
	}

	result := ExecuteIngest(ctx, payload, profile.Name, cfg)

	// 5. Report result.
	outputDir := filepath.Join(repoDir, ".runforge", "ingest", payload.WOID)
	fmt.Printf("\n=== Result ===\n")
	fmt.Printf("WO:     %s\n", payload.WOID)
	fmt.Printf("State:  %s\n", result.State)
	fmt.Printf("Runner: %s\n", result.RunnerUsed)
	if result.Duration > 0 {
		fmt.Printf("Duration: %s\n", result.Duration.Round(time.Second))
	}
	if result.Error != "" {
		fmt.Printf("Error: %s\n", result.Error)
	}
	fmt.Printf("Output: %s\n", outputDir)

	if result.State != task.StateCompleted {
		return fmt.Errorf("task %s: %s", result.State, result.Error)
	}

	return nil
}

// IngestConfig holds parameters for ExecuteIngest.
type IngestConfig struct {
	Runner      string
	Fallbacks   []string
	RepoDir     string
	MaxRuntime  time.Duration
	IdleTimeout time.Duration
}

// ExecuteIngest runs an approved WO payload through the runner cascade.
// This is the shared logic used by both the CLI and the sentinel daemon.
func ExecuteIngest(ctx context.Context, payload *ingest.IngestPayload, profileName string, cfg IngestConfig) *task.TaskResult {
	prompt := ingest.BuildPrompt(payload)

	t := &task.Task{
		ID:     payload.WOID,
		Title:  fmt.Sprintf("WO %s: %s", payload.WOID, payload.IncidentID),
		Prompt: prompt,
		Runner: cfg.Runner,
	}

	cascade := []string{cfg.Runner}
	for _, fb := range cfg.Fallbacks {
		if fb != cfg.Runner {
			cascade = append(cascade, fb)
		}
	}

	runners := map[string]runner.Runner{
		"codex":    runner.NewCodexRunnerWithProfile("", profileName, nil, cfg.IdleTimeout),
		"claude":   runner.NewClaudeRunner(cfg.IdleTimeout),
		"gemini":   runner.NewGeminiRunner(cfg.IdleTimeout),
		"opencode": runner.NewOpencodeRunner(cfg.IdleTimeout),
		"script":   runner.NewScriptRunner(),
	}

	blacklist := runner.NewRunnerBlacklist()
	outputDir := filepath.Join(cfg.RepoDir, ".runforge", "ingest", payload.WOID)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return &task.TaskResult{
			TaskID:  payload.WOID,
			State:   task.StateFailed,
			Error:   fmt.Sprintf("create output dir: %v", err),
			EndedAt: time.Now(),
		}
	}

	return RunWithCascade(ctx, t, cfg.RepoDir, outputDir, runners, cascade, cfg.MaxRuntime, blacklist, nil, nil)
}
