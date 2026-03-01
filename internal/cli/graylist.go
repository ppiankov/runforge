package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/runner"
)

func newGraylistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graylist",
		Short: "Manage runner quality graylist",
		Long: `Manage the runner quality graylist.

Runners that report success but produce no real work (0 events, no commits)
are automatically graylisted during runs. Graylisted runners are removed
from fallback cascades but can still run if explicitly assigned as task runner.

Entries are model-aware: graylisting "deepseek" with model "deepseek-chat"
does NOT block "deepseek" with model "deepseek-reasoner". Use --model to
target a specific model, or omit it to block all models for that runner.

Detection: after each task completes, runforge checks events.jsonl. If the
file is empty or missing, the task is flagged as a false positive and the
runner+model pair is auto-graylisted.

To reinstate a runner: runforge graylist remove <runner> [--model <model>]
To view all graylisted runners: runforge graylist list
To clear all entries: runforge graylist clear

Stored in: ~/.runforge/graylist.json`,
	}

	cmd.AddCommand(newGraylistListCmd())
	cmd.AddCommand(newGraylistAddCmd())
	cmd.AddCommand(newGraylistRemoveCmd())
	cmd.AddCommand(newGraylistClearCmd())

	return cmd
}

func newGraylistListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show all graylisted runners",
		RunE: func(cmd *cobra.Command, args []string) error {
			gl := runner.LoadGraylist(runner.DefaultGraylistPath())
			entries := gl.Entries()

			if len(entries) == 0 {
				fmt.Println("No graylisted runners.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "RUNNER\tMODEL\tREASON\tADDED\n")
			for key, info := range entries {
				model := info.Model
				if model == "" {
					model = "(all)"
				}
				_ = key // key is runner:model, but we show them separately
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", key, model, info.Reason, info.AddedAt.Format(time.RFC3339))
			}
			return w.Flush()
		},
	}
}

func newGraylistAddCmd() *cobra.Command {
	var (
		reason string
		model  string
	)

	cmd := &cobra.Command{
		Use:   "add <runner>",
		Short: "Add a runner to the graylist (remove from fallback cascades)",
		Long:  "Add a runner+model pair to the graylist. Use --model to target a specific model.\nOmit --model to block ALL models for that runner (use with caution — prefer targeting a specific model).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gl := runner.LoadGraylist(runner.DefaultGraylistPath())
			gl.Add(args[0], model, reason)
			if model != "" {
				fmt.Printf("Graylisted %q (model: %s)\n", args[0], model)
			} else {
				fmt.Printf("Graylisted %q (all models)\n", args[0])
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "reason for graylisting")
	cmd.Flags().StringVar(&model, "model", "", "specific model to graylist (omit for all models)")

	return cmd
}

func newGraylistRemoveCmd() *cobra.Command {
	var model string

	cmd := &cobra.Command{
		Use:   "remove <runner>",
		Short: "Remove a runner from the graylist (reinstate for fallback use)",
		Long:  "Remove a runner+model pair from the graylist so it can be used in fallback cascades again.\nUse --model to target a specific model entry. Omit to remove the wildcard entry.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gl := runner.LoadGraylist(runner.DefaultGraylistPath())
			gl.Remove(args[0], model)
			if model != "" {
				fmt.Printf("Removed %q (model: %s) from graylist\n", args[0], model)
			} else {
				fmt.Printf("Removed %q from graylist\n", args[0])
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&model, "model", "", "specific model to remove (omit for wildcard entry)")

	return cmd
}

func newGraylistClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Remove all runners from the graylist",
		RunE: func(cmd *cobra.Command, args []string) error {
			gl := runner.LoadGraylist(runner.DefaultGraylistPath())
			gl.Clear()
			fmt.Println("Graylist cleared.")
			return nil
		},
	}
}
