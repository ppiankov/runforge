package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// Version, Commit, and BuildDate are set via LDFLAGS at build time.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var (
	verbose    bool
	configFile string
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "tokencontrol",
		Short: "Dependency-aware parallel task runner",
		Long:  "tokencontrol reads a task file and orchestrates parallel task runner processes with dependency-aware scheduling.",
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
	root.PersistentFlags().StringVar(&configFile, "config", ".tokencontrol.yml", "path to config file")

	root.AddCommand(newRunCmd())
	root.AddCommand(newRerunCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newGenerateCmd())
	root.AddCommand(newValidateTasksCmd())
	root.AddCommand(newWatchCmd())
	root.AddCommand(newUnlockCmd())
	root.AddCommand(newScanCmd())
	root.AddCommand(newIngestCmd())
	root.AddCommand(newSentinelCmd())
	root.AddCommand(newBlacklistCmd())
	root.AddCommand(newQuotaCmd())
	root.AddCommand(newGraylistCmd())
	root.AddCommand(newStateCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newPRCmd())

	return root
}
