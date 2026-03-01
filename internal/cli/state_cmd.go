package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/state"
)

func newStateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Manage persistent task state",
		Long: `Manage the persistent task state that prevents duplicate runs.

Tasks that completed successfully are automatically skipped on subsequent runs.
Use 'runforge state list' to see tracked tasks, 'runforge state reset <id>'
to allow a task to re-execute, or 'runforge state clear' to reset all state.`,
	}

	cmd.AddCommand(newStateListCmd())
	cmd.AddCommand(newStateResetCmd())
	cmd.AddCommand(newStateClearCmd())

	return cmd
}

func newStateListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show all tracked task states",
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker := state.Load(state.DefaultPath())
			entries := tracker.Entries()
			if len(entries) == 0 {
				fmt.Println("No tracked tasks.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "TASK\tSTATUS\tRUNNER\tCOMMIT\tFINISHED\n")
			for id, e := range entries {
				commit := e.Commit
				if len(commit) > 7 {
					commit = commit[:7]
				}
				finished := ""
				if !e.FinishedAt.IsZero() {
					finished = e.FinishedAt.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, e.Status, e.Runner, commit, finished)
			}
			return w.Flush()
		},
	}
}

func newStateResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset <task-id>",
		Short: "Reset a task to allow re-execution",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker := state.Load(state.DefaultPath())
			entry := tracker.Get(args[0])
			if entry == nil {
				return fmt.Errorf("task %q not found in state", args[0])
			}
			tracker.Reset(args[0])
			fmt.Printf("Reset %q (was %s)\n", args[0], entry.Status)
			return nil
		},
	}
}

func newStateClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Remove all task state (allows full re-execution)",
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker := state.Load(state.DefaultPath())
			tracker.Clear()
			fmt.Println("State cleared.")
			return nil
		},
	}
}
