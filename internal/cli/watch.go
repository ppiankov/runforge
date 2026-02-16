package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/reporter"
)

func newWatchCmd() *cobra.Command {
	var runDir string

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Monitor a running runforge session in real-time",
		Long:  "Watch provides a top-like TUI that monitors .runforge run directories, showing task state, event counts, and last action.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runDir == "" {
				detected, err := detectLatestRunDir()
				if err != nil {
					return err
				}
				runDir = detected
			}
			return runWatch(runDir)
		},
	}

	cmd.Flags().StringVar(&runDir, "run-dir", "", "path to .runforge/<timestamp> directory (auto-detects latest if omitted)")

	return cmd
}

func detectLatestRunDir() (string, error) {
	entries, err := os.ReadDir(".runforge")
	if err != nil {
		return "", fmt.Errorf("no .runforge directory found: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		return "", fmt.Errorf("no run directories found in .runforge/")
	}
	sort.Strings(dirs)
	return filepath.Join(".runforge", dirs[len(dirs)-1]), nil
}

func runWatch(runDir string) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	isTTY := isTerminal()
	w := reporter.NewWatchReporter(os.Stdout, isTTY, runDir)

	stop := make(chan struct{})
	go func() {
		<-sigCh
		close(stop)
	}()

	return w.Run(stop)
}
