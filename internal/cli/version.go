package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("runforge %s (commit: %s, built: %s, go: %s)\n", Version, Commit, BuildDate, runtime.Version())
		},
	}
}
