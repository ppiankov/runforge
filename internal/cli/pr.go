package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ppiankov/tokencontrol/internal/config"
	"github.com/ppiankov/tokencontrol/internal/reporter"
	"github.com/ppiankov/tokencontrol/internal/task"
	"github.com/spf13/cobra"
)

// prEnv provides dependency injection for testability.
type prEnv struct {
	runGit func(ctx context.Context, dir string, args ...string) (string, error)
	runGH  func(ctx context.Context, dir string, args ...string) (string, error)
}

// prResult tracks per-task PR creation outcome.
type prResult struct {
	TaskID  string
	Repo    string
	Branch  string
	PRURL   string
	Skipped string // reason if skipped
	Error   string // reason if failed
}

func newPRCmd() *cobra.Command {
	var (
		runDir   string
		reposDir string
		base     string
		dryRun   bool
		draft    bool
	)

	cmd := &cobra.Command{
		Use:   "pr",
		Short: "Create GitHub PRs from completed tasks",
		Long:  "Push worktree branches and create pull requests for completed tasks in a run.",
		RunE: func(cmd *cobra.Command, args []string) error {
			env := &prEnv{
				runGit: shellCmd("git"),
				runGH:  shellCmd("gh"),
			}
			return runPR(cmd.Context(), env, cmd.OutOrStdout(), runDir, reposDir, base, dryRun, draft)
		},
	}

	cmd.Flags().StringVar(&runDir, "run-dir", "", "run directory (auto-detects latest if omitted)")
	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	cmd.Flags().StringVar(&base, "base", "main", "base branch for PRs")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be created without executing")
	cmd.Flags().BoolVar(&draft, "draft", false, "create draft PRs")
	return cmd
}

// shellCmd returns a function that runs a CLI tool and captures stdout.
func shellCmd(bin string) func(ctx context.Context, dir string, args ...string) (string, error) {
	return func(ctx context.Context, dir string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%s %s: %s: %w", bin, strings.Join(args, " "), strings.TrimSpace(string(out)), err)
		}
		return strings.TrimSpace(string(out)), nil
	}
}

func runPR(ctx context.Context, env *prEnv, w io.Writer, runDir, reposDir, base string, dryRun, draft bool) error {
	// Auto-detect latest run dir if not specified.
	if runDir == "" {
		dir, err := findLatestRunDir(reposDir)
		if err != nil {
			return err
		}
		runDir = dir
	}

	// Load report.
	reportPath := filepath.Join(runDir, "report.json")
	report, err := reporter.ReadJSONReport(reportPath)
	if err != nil {
		return fmt.Errorf("load report: %w", err)
	}

	// Use report's repos_dir if available, otherwise use flag.
	if report.ReposDir != "" {
		reposDir = report.ReposDir
	}

	// Collect completed tasks with worktree branches.
	var results []prResult
	taskIDs := sortedTaskIDs(report.Results)

	for _, taskID := range taskIDs {
		result := report.Results[taskID]
		if result.State != task.StateCompleted {
			continue
		}
		if result.WorktreeBranch == "" {
			results = append(results, prResult{
				TaskID:  taskID,
				Skipped: "no worktree branch (ran on main)",
			})
			continue
		}

		// Load task metadata for title and repo.
		meta, err := loadTaskMeta(result.OutputDir)
		if err != nil {
			results = append(results, prResult{
				TaskID: taskID,
				Branch: result.WorktreeBranch,
				Error:  fmt.Sprintf("load task metadata: %v", err),
			})
			continue
		}

		repoDir := config.RepoPath(meta.Repo, reposDir)
		pr := createPR(ctx, env, w, meta, result, repoDir, base, dryRun, draft)
		results = append(results, pr)
	}

	printPRSummary(w, results)
	return nil
}

func createPR(
	ctx context.Context,
	env *prEnv,
	w io.Writer,
	meta *task.Task,
	result *task.TaskResult,
	repoDir, base string,
	dryRun, draft bool,
) prResult {
	pr := prResult{
		TaskID: meta.ID,
		Repo:   meta.Repo,
		Branch: result.WorktreeBranch,
	}

	prefix := ""
	if dryRun {
		prefix = "[dry-run] "
	}

	// Push branch.
	fmt.Fprintf(w, "%sPushing %s → origin ...", prefix, result.WorktreeBranch)
	if !dryRun {
		timeout, cancel := context.WithTimeout(ctx, 60*time.Second)
		_, err := env.runGit(timeout, repoDir, "push", "-u", "origin", result.WorktreeBranch)
		cancel()
		if err != nil {
			fmt.Fprintln(w, " failed")
			pr.Error = fmt.Sprintf("push: %v", err)
			return pr
		}
	}
	fmt.Fprintln(w, " ok")

	// Create PR.
	title := meta.Title
	if title == "" {
		title = meta.ID
	}
	body := buildPRBody(meta, result)

	fmt.Fprintf(w, "%sCreating PR: %q → %s ...", prefix, title, meta.Repo)
	if !dryRun {
		args := []string{
			"pr", "create",
			"--head", result.WorktreeBranch,
			"--base", base,
			"--title", title,
			"--body", body,
		}
		if draft {
			args = append(args, "--draft")
		}

		timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
		out, err := env.runGH(timeout, repoDir, args...)
		cancel()
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "already exists") {
				fmt.Fprintln(w, " already exists")
				pr.Skipped = "PR already exists"
				return pr
			}
			fmt.Fprintln(w, " failed")
			pr.Error = fmt.Sprintf("gh pr create: %v", err)
			return pr
		}
		pr.PRURL = out
		fmt.Fprintf(w, " %s\n", out)
	} else {
		fmt.Fprintln(w, " skipped (dry-run)")
	}

	return pr
}

func buildPRBody(meta *task.Task, result *task.TaskResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", meta.Title)
	fmt.Fprintf(&b, "- **Runner:** %s\n", result.RunnerUsed)
	fmt.Fprintf(&b, "- **Duration:** %s\n", result.Duration.Round(time.Second))
	if result.AutoCommitted {
		b.WriteString("- **Auto-committed:** yes\n")
	}
	b.WriteString("\n---\n*Created by [tokencontrol](https://github.com/ppiankov/tokencontrol)*\n")
	return b.String()
}

func loadTaskMeta(outputDir string) (*task.Task, error) {
	data, err := os.ReadFile(filepath.Join(outputDir, "task.json"))
	if err != nil {
		return nil, err
	}
	var t task.Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func sortedTaskIDs(results map[string]*task.TaskResult) []string {
	ids := make([]string, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func printPRSummary(w io.Writer, results []prResult) {
	created, skipped, failed := 0, 0, 0
	for _, r := range results {
		switch {
		case r.Error != "":
			failed++
		case r.Skipped != "":
			skipped++
		case r.PRURL != "" || r.Branch != "":
			created++
		}
	}
	fmt.Fprintf(w, "\nSummary: %d PRs created, %d skipped, %d failed\n", created, skipped, failed)
}
