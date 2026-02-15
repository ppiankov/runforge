package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/codexrun/internal/config"
	"github.com/ppiankov/codexrun/internal/runner"
	"github.com/ppiankov/codexrun/internal/task"
)

func newVerifyCmd() *cobra.Command {
	var (
		runDir   string
		reposDir string
	)

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Run make test && make lint for repos in a completed run",
		RunE: func(cmd *cobra.Command, args []string) error {
			return verifyRun(runDir, reposDir)
		},
	}

	cmd.Flags().StringVar(&runDir, "run-dir", "", "path to .codexrun/<timestamp> directory (required)")
	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	_ = cmd.MarkFlagRequired("run-dir")

	return cmd
}

func verifyRun(runDir, reposDir string) error {
	reportPath := fmt.Sprintf("%s/report.json", runDir)
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}

	var report task.RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("parse report: %w", err)
	}

	// collect unique repos from results
	repos := make(map[string]struct{})
	// repos are collected below via task file lookup

	// re-read tasks file to get repo mapping
	tf, err := config.Load(report.TasksFile)
	if err != nil {
		return fmt.Errorf("load tasks file: %w", err)
	}

	for _, t := range tf.Tasks {
		if _, ok := report.Results[t.ID]; ok {
			repos[t.Repo] = struct{}{}
		}
	}

	ctx := context.Background()
	anyFailed := false

	for repo := range repos {
		repoPath := config.RepoPath(repo, reposDir)
		vr := runner.Verify(ctx, repo, repoPath, runDir)
		if vr.Passed {
			fmt.Printf("  ✓ %s (%s)\n", repo, vr.Duration)
		} else {
			fmt.Printf("  ✗ %s: %s\n", repo, vr.Error)
			anyFailed = true
		}
	}

	if anyFailed {
		return fmt.Errorf("verification failed for one or more repos")
	}
	return nil
}
