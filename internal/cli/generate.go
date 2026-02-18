package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/generate"
	"github.com/ppiankov/runforge/internal/task"
)

func newGenerateCmd() *cobra.Command {
	var (
		reposDir      string
		output        string
		owner         string
		filterRepo    string
		defaultRunner string
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate task file from work-orders.md files",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadSettings(configFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if !cmd.Flags().Changed("repos-dir") && cfg.ReposDir != "" {
				reposDir = cfg.ReposDir
			}
			if !cmd.Flags().Changed("runner") && cfg.DefaultRunner != "" {
				defaultRunner = cfg.DefaultRunner
			}
			return generateTasks(reposDir, output, owner, filterRepo, defaultRunner, cfg)
		},
	}

	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	cmd.Flags().StringVar(&output, "output", "", "output file path (default: <repos-dir>/runforge-tasks.json)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub owner/org (inferred from git remote if omitted)")
	cmd.Flags().StringVar(&filterRepo, "filter-repo", "", "only scan this repo name")
	cmd.Flags().StringVar(&defaultRunner, "runner", "codex", "default runner for generated tasks")

	return cmd
}

func generateTasks(reposDir, output, owner, filterRepo, defaultRunner string, cfg *config.Settings) error {
	reposDir, err := filepath.Abs(reposDir)
	if err != nil {
		return fmt.Errorf("resolve repos dir: %w", err)
	}

	if output == "" {
		output = filepath.Join(reposDir, "runforge-tasks.json")
	}

	entries, err := os.ReadDir(reposDir)
	if err != nil {
		return fmt.Errorf("read repos dir: %w", err)
	}

	var tasks []task.Task
	var repoCount int

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		repoName := entry.Name()

		if filterRepo != "" && repoName != filterRepo {
			continue
		}

		woPath := filepath.Join(reposDir, repoName, "docs", "work-orders.md")
		content, err := os.ReadFile(woPath)
		if err != nil {
			slog.Debug("no work-orders.md", "repo", repoName)
			continue
		}

		repoOwner := owner
		if repoOwner == "" {
			repoOwner, err = inferOwnerFromGitRemote(filepath.Join(reposDir, repoName))
			if err != nil {
				return fmt.Errorf("repo %q: cannot determine owner (use --owner flag): %w", repoName, err)
			}
		}
		repoSlug := repoOwner + "/" + repoName

		workOrders := generate.ParseWorkOrders(string(content))

		// Collect IDs of included (non-done) WOs to resolve dependencies.
		includedIDs := make(map[string]struct{})
		for _, wo := range workOrders {
			if wo.Status != generate.StatusDone {
				includedIDs[generate.TaskID(repoName, wo.RawID)] = struct{}{}
			}
		}

		added := 0
		for _, wo := range workOrders {
			if wo.Status == generate.StatusDone {
				continue
			}

			prompt := generate.BuildPrompt(wo)
			if wo.Summary == "" {
				prompt = wo.Title + ". " + prompt
			}

			t := task.Task{
				ID:       generate.TaskID(repoName, wo.RawID),
				Repo:     repoSlug,
				Priority: wo.Priority,
				Title:    wo.Title,
				Prompt:   prompt,
				Runner:   defaultRunner,
			}

			// Only set depends_on if the target task is also included (not done).
			if wo.DependsOn != "" {
				depID := generate.TaskID(repoName, wo.DependsOn)
				if _, ok := includedIDs[depID]; ok {
					t.DependsOn = []string{depID}
				} else {
					slog.Debug("dropping satisfied dependency", "task", t.ID, "dep", depID)
				}
			}

			if wo.Runner != "" {
				t.Runner = wo.Runner
			}

			tasks = append(tasks, t)
			added++
		}

		if added > 0 {
			repoCount++
			slog.Debug("scanned repo", "repo", repoName, "tasks", added)
		}
	}

	if len(tasks) == 0 {
		return fmt.Errorf("no pending work orders found in %s", reposDir)
	}

	tf := task.TaskFile{
		Description: fmt.Sprintf("Generated from work-orders.md in %s", filepath.Base(reposDir)),
		Generated:   time.Now().Format("2006-01-02"),
		Tasks:       tasks,
	}

	// Inject runner profiles from settings config.
	if cfg != nil {
		if cfg.DefaultRunner != "" {
			tf.DefaultRunner = cfg.DefaultRunner
		}
		if len(cfg.DefaultFallbacks) > 0 {
			tf.DefaultFallbacks = cfg.DefaultFallbacks
		}
		if len(cfg.Runners) > 0 {
			tf.Runners = make(map[string]*task.RunnerProfileConfig, len(cfg.Runners))
			for name, rp := range cfg.Runners {
				tf.Runners[name] = &task.RunnerProfileConfig{
					Type:           rp.Type,
					Model:          rp.Model,
					Profile:        rp.Profile,
					Env:            rp.Env,
					DataCollection: rp.DataCollection,
				}
			}
		}
	}

	data, err := json.MarshalIndent(tf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}

	if err := os.WriteFile(output, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Printf("Generated %d tasks from %d repos → %s\n", len(tasks), repoCount, output)
	fmt.Printf("\nTo run:\n  runforge run --tasks %s --tui minimal\n", output)
	return nil
}

var reGitHubOwner = regexp.MustCompile(`github\.com[:/]([^/]+)/`)

// inferOwnerFromGitRemote extracts the GitHub owner from the git remote origin URL.
func inferOwnerFromGitRemote(repoDir string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git remote get-url origin: %w", err)
	}
	url := strings.TrimSpace(string(out))

	m := reGitHubOwner.FindStringSubmatch(url)
	if m == nil {
		return "", fmt.Errorf("cannot parse owner from remote URL: %s", url)
	}
	return m[1], nil
}
