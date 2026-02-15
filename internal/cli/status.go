package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/codexrun/internal/task"
)

func newStatusCmd() *cobra.Command {
	var runDir string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect results of a completed run",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showStatus(runDir)
		},
	}

	cmd.Flags().StringVar(&runDir, "run-dir", "", "path to .codexrun/<timestamp> directory (required)")
	_ = cmd.MarkFlagRequired("run-dir")

	return cmd
}

func showStatus(runDir string) error {
	reportPath := fmt.Sprintf("%s/report.json", runDir)
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}

	var report task.RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("parse report: %w", err)
	}

	fmt.Printf("Run: %s\n", report.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Printf("Tasks file: %s\n", report.TasksFile)
	fmt.Printf("Workers: %d\n", report.Workers)
	if report.Filter != "" {
		fmt.Printf("Filter: %s\n", report.Filter)
	}
	fmt.Printf("Duration: %s\n\n", report.TotalDuration)

	fmt.Printf("Total: %d  Completed: %d  Failed: %d  Skipped: %d\n\n",
		report.TotalTasks, report.Completed, report.Failed, report.Skipped)

	for id, r := range report.Results {
		status := r.State.String()
		line := fmt.Sprintf("  %-30s  %s", id, status)
		if r.Error != "" {
			line += fmt.Sprintf("  (%s)", r.Error)
		}
		if r.Duration > 0 {
			line += fmt.Sprintf("  %s", r.Duration)
		}
		fmt.Println(line)
	}

	return nil
}
