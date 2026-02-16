package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// Version and Commit are set via LDFLAGS at build time.
var (
	Version = "dev"
	Commit  = "none"
)

var (
	verbose    bool
	configFile string
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "runforge",
		Short: "Dependency-aware parallel task runner",
		Long:  "runforge reads a task file and orchestrates parallel task runner processes with dependency-aware scheduling.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			level := slog.LevelWarn
			if verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: level,
			})))
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
	root.PersistentFlags().StringVar(&configFile, "config", ".runforge.yml", "path to config file")

	root.AddCommand(newRunCmd())
	root.AddCommand(newRerunCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newGenerateCmd())
	root.AddCommand(newValidateTasksCmd())
	root.AddCommand(newWatchCmd())

	return root
}
