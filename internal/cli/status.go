package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/task"
)

func newStatusCmd() *cobra.Command {
	var runDir string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Inspect results of a completed run",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runDir == "" {
				latest, err := findLatestRunDir(".")
				if err != nil {
					return fmt.Errorf("no --run-dir specified and %w", err)
				}
				runDir = latest
			}
			return showStatus(runDir)
		},
	}

	cmd.Flags().StringVar(&runDir, "run-dir", "", "path to .runforge/<timestamp> directory (auto-detects latest if omitted)")

	return cmd
}

// findLatestRunDir scans baseDir/.runforge/ for the most recent run
// directory that contains a report.json.
func findLatestRunDir(baseDir string) (string, error) {
	rfDir := fmt.Sprintf("%s/.runforge", baseDir)
	entries, err := os.ReadDir(rfDir)
	if err != nil {
		return "", fmt.Errorf("cannot read .runforge directory: %w", err)
	}

	// entries are sorted alphabetically; timestamps sort chronologically
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if !e.IsDir() {
			continue
		}
		candidate := fmt.Sprintf("%s/%s", rfDir, e.Name())
		if _, err := os.Stat(fmt.Sprintf("%s/report.json", candidate)); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no completed runs found in %s", rfDir)
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
	if report.RunID != "" {
		fmt.Printf("Run ID: %s\n", report.RunID)
	}
	if report.ParentRunID != "" {
		fmt.Printf("Parent: %s\n", report.ParentRunID)
	}
	fmt.Printf("Tasks files: %s\n", strings.Join(report.TasksFiles, ", "))
	fmt.Printf("Workers: %d\n", report.Workers)
	if report.Filter != "" {
		fmt.Printf("Filter: %s\n", report.Filter)
	}
	fmt.Printf("Duration: %s\n\n", report.TotalDuration)

	fmt.Printf("Total: %d  Completed: %d  Failed: %d  Skipped: %d  Rate limited: %d\n\n",
		report.TotalTasks, report.Completed, report.Failed, report.Skipped, report.RateLimited)

	for id, r := range report.Results {
		status := r.State.String()
		line := fmt.Sprintf("  %-30s  %s", id, status)
		errDisplay := r.Error
		if r.ConnectivityError != "" {
			errDisplay = r.ConnectivityError
		}
		if errDisplay != "" {
			line += fmt.Sprintf("  (%s)", errDisplay)
		}
		if r.Duration > 0 {
			line += fmt.Sprintf("  %s", r.Duration)
		}
		fmt.Println(line)
	}

	return nil
}
