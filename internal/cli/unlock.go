package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/runner"
)

func newUnlockCmd() *cobra.Command {
	var (
		reposDir string
		repo     string
	)

	cmd := &cobra.Command{
		Use:   "unlock",
		Short: "Remove a stale repo lock file",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoDir := config.RepoPath(repo, reposDir)

			info, err := runner.ReadLock(repoDir)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintf(os.Stdout, "No lock found for %s\n", repo)
					return nil
				}
				return fmt.Errorf("read lock: %w", err)
			}

			lockPath := filepath.Join(repoDir, ".runforge.lock")
			if err := os.Remove(lockPath); err != nil {
				return fmt.Errorf("remove lock: %w", err)
			}

			fmt.Fprintf(os.Stdout, "Removed lock for %s (was PID %d, task %s, since %s)\n",
				repo, info.PID, info.TaskID, info.StartedAt.Format(time.RFC3339))
			return nil
		},
	}

	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	cmd.Flags().StringVar(&repo, "repo", "", "repo name to unlock (e.g. org/repo-name)")
	_ = cmd.MarkFlagRequired("repo")

	return cmd
}
