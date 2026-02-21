package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/ingest"
	"github.com/ppiankov/runforge/internal/sentinel"
	"github.com/ppiankov/runforge/internal/task"
)

func newSentinelCmd() *cobra.Command {
	var (
		ingestedDir string
		stateDir    string
		profileDir  string
		repoDir     string
		runnerName  string
		fallbacks   []string
		pollMode    bool
		maxRuntime  time.Duration
		idleTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "sentinel",
		Short: "Watch for approved WOs and auto-execute them",
		Long: `Sentinel watches the ingested/ directory for approved work orders
from nullbot and automatically executes them through the runner cascade.

It moves each payload through processing/ → completed/ or failed/,
ensuring no WO is lost or executed twice. On restart, orphaned WOs
in processing/ are recovered to failed/.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Build the execution function that the sentinel calls for each WO.
			// This closure captures runner config and wires it to ExecuteIngest,
			// breaking the import cycle (sentinel → cli is not allowed).
			execFn := func(ctx context.Context, payload *ingest.IngestPayload, profileName string) *task.TaskResult {
				icfg := IngestConfig{
					Runner:      runnerName,
					Fallbacks:   fallbacks,
					RepoDir:     repoDir,
					MaxRuntime:  maxRuntime,
					IdleTimeout: idleTimeout,
				}
				return ExecuteIngest(ctx, payload, profileName, icfg)
			}

			cfg := sentinel.Config{
				IngestedDir: ingestedDir,
				StateDir:    stateDir,
				ProfileDir:  profileDir,
				PollMode:    pollMode,
				MaxRuntime:  maxRuntime,
				IdleTimeout: idleTimeout,
				Runner:      runnerName,
				Fallbacks:   fallbacks,
				RepoDir:     repoDir,
				ExecFn:      execFn,
			}

			s, err := sentinel.New(cfg)
			if err != nil {
				return fmt.Errorf("init sentinel: %w", err)
			}

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			return s.Run(ctx)
		},
	}

	cmd.Flags().StringVar(&ingestedDir, "ingested", "/home/nullbot/state/ingested", "path to ingested/ directory")
	cmd.Flags().StringVar(&stateDir, "state", "/home/nullbot/state/sentinel", "sentinel state directory")
	cmd.Flags().StringVar(&profileDir, "profile-dir", "", "chainwatch profile directory (default: ~/.chainwatch/profiles)")
	cmd.Flags().StringVar(&repoDir, "repo-dir", ".", "target directory for remediation")
	cmd.Flags().StringVar(&runnerName, "runner", "claude", "primary runner")
	cmd.Flags().StringSliceVar(&fallbacks, "fallbacks", nil, "fallback runners (comma-separated)")
	cmd.Flags().BoolVar(&pollMode, "poll", false, "use polling instead of fsnotify")
	cmd.Flags().DurationVar(&maxRuntime, "max-runtime", 30*time.Minute, "per-WO execution timeout")
	cmd.Flags().DurationVar(&idleTimeout, "idle-timeout", 5*time.Minute, "idle timeout for runner")

	return cmd
}
