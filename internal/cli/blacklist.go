package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/tokencontrol/internal/runner"
)

func newBlacklistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blacklist",
		Short: "Manage runner rate-limit and connectivity blacklist",
		Long: `Manage the runner blacklist.

Runners are automatically blacklisted when they hit rate limits or
connectivity errors. Blacklisted runners are skipped entirely (not just
demoted from fallback cascades like the graylist).

Rate-limited runners use the provider's resets_at time (or 1 hour default).
Connectivity errors block for 1 hour.

To view blocked runners: tokencontrol blacklist list
To clear all entries:    tokencontrol blacklist clear

Stored in: ~/.tokencontrol/blacklist.json`,
	}

	cmd.AddCommand(newBlacklistListCmd())
	cmd.AddCommand(newBlacklistClearCmd())

	return cmd
}

func newBlacklistListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show all blacklisted runners",
		RunE: func(cmd *cobra.Command, args []string) error {
			bl := runner.LoadBlacklist(runner.DefaultBlacklistPath())
			entries := bl.Entries()

			if len(entries) == 0 {
				fmt.Println("No blacklisted runners.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintf(w, "RUNNER\tBLOCKED UNTIL\tREMAINING\n")
			now := time.Now()
			for name, until := range entries {
				remaining := until.Sub(now).Truncate(time.Second)
				fmt.Fprintf(w, "%s\t%s\t%s\n", name, until.Format(time.RFC3339), remaining.String())
			}
			return w.Flush()
		},
	}
}

func newBlacklistClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Remove all runners from the blacklist",
		RunE: func(cmd *cobra.Command, args []string) error {
			bl := runner.LoadBlacklist(runner.DefaultBlacklistPath())
			bl.Clear()
			fmt.Println("Blacklist cleared.")
			return nil
		},
	}
}
