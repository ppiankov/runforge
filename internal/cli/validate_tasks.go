package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/task"
)

func newValidateTasksCmd() *cobra.Command {
	var (
		tasksFile string
		reposDir  string
	)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a task file without running anything",
		RunE: func(cmd *cobra.Command, args []string) error {
			return validateTasks(tasksFile, reposDir)
		},
	}

	cmd.Flags().StringVar(&tasksFile, "tasks", "runforge.json", "path to tasks JSON file")
	cmd.Flags().StringVar(&reposDir, "repos-dir", "", "verify repos exist on disk (optional)")

	return cmd
}

func validateTasks(tasksFile, reposDir string) error {
	tf, err := config.Load(tasksFile)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	graph, err := task.BuildGraph(tf.Tasks)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	if reposDir != "" {
		reposDir, err = filepath.Abs(reposDir)
		if err != nil {
			return fmt.Errorf("resolve repos dir: %w", err)
		}
		if err := config.ValidateRepos(tf, reposDir); err != nil {
			return fmt.Errorf("validate: %w", err)
		}
	}

	repos := countRepos(tf.Tasks)
	depth := maxDepth(graph)

	fmt.Printf("valid: %d tasks, %d repos, max depth %d\n", len(tf.Tasks), repos, depth)
	return nil
}

func countRepos(tasks []task.Task) int {
	seen := make(map[string]struct{})
	for _, t := range tasks {
		seen[t.Repo] = struct{}{}
	}
	return len(seen)
}

func maxDepth(graph *task.Graph) int {
	depths := make(map[string]int)
	max := 0

	for _, id := range graph.Order() {
		d := 0
		for _, parentID := range graph.Deps(id) {
			if pd, ok := depths[parentID]; ok && pd+1 > d {
				d = pd + 1
			}
		}
		depths[id] = d
		if d > max {
			max = d
		}
	}

	return max
}
