package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/scan"
)

func newScanCmd() *cobra.Command {
	var (
		reposDir   string
		format     string
		filterRepo string
		severity   string
		checks     []string
		owner      string
		runner     string
		output     string
	)

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Audit repos for structural, security, and quality issues",
		Long:  "Walk all repos in repos-dir and run filesystem-based checks for missing files, security issues, CI gaps, and code quality problems.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadSettings(configFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if !cmd.Flags().Changed("repos-dir") && cfg.ReposDir != "" {
				reposDir = cfg.ReposDir
			}
			if !cmd.Flags().Changed("runner") && cfg.DefaultRunner != "" {
				runner = cfg.DefaultRunner
			}

			var excludeRepos []string
			if cfg.Scan != nil {
				excludeRepos = cfg.Scan.ExcludeRepos
			}

			var minSev scan.Severity
			if severity != "" {
				minSev = scan.ParseSeverity(severity)
				if minSev == 0 {
					return fmt.Errorf("invalid severity %q (use critical, warning, or info)", severity)
				}
			}

			result, err := scan.Scan(scan.ScanOptions{
				ReposDir:     reposDir,
				FilterRepo:   filterRepo,
				MinSeverity:  minSev,
				Categories:   checks,
				ExcludeRepos: excludeRepos,
			})
			if err != nil {
				return err
			}

			w := os.Stdout
			if output != "" {
				f, err := os.Create(output)
				if err != nil {
					return fmt.Errorf("create output file: %w", err)
				}
				defer func() { _ = f.Close() }()
				w = f
			}

			isTTY := isTerminal()

			switch format {
			case "json":
				return scan.NewJSONFormatter().Format(w, result)
			case "tasks":
				if err := scan.NewTaskFormatter(owner, runner).Format(w, result); err != nil {
					return err
				}
				if output != "" {
					fmt.Fprintf(os.Stderr, "\nTo run:\n  runforge run --tasks %s --tui minimal\n", output)
				}
				return nil
			default:
				return scan.NewTextFormatter(isTTY).Format(w, result)
			}
		},
	}

	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text, json, tasks")
	cmd.Flags().StringVar(&filterRepo, "filter-repo", "", "scan only this repo")
	cmd.Flags().StringVar(&severity, "severity", "", "minimum severity: critical, warning, info")
	cmd.Flags().StringSliceVar(&checks, "check", nil, "check categories to run (structure,go,python,security,ci,quality)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub owner for task format output")
	cmd.Flags().StringVar(&runner, "runner", "codex", "default runner for task format output")
	cmd.Flags().StringVar(&output, "output", "", "write output to file instead of stdout")

	return cmd
}
