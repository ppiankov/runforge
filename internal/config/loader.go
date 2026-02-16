package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ppiankov/runforge/internal/task"
)

// Load reads and validates a runforge task file.
func Load(path string) (*task.TaskFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tasks file: %w", err)
	}

	var tf task.TaskFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parse tasks file: %w", err)
	}

	if err := validate(&tf); err != nil {
		return nil, err
	}

	return &tf, nil
}

// validate checks for duplicate IDs and dangling depends_on references.
func validate(tf *task.TaskFile) error {
	if len(tf.Tasks) == 0 {
		return fmt.Errorf("tasks file contains no tasks")
	}

	ids := make(map[string]struct{}, len(tf.Tasks))
	for _, t := range tf.Tasks {
		if t.ID == "" {
			return fmt.Errorf("task with empty id")
		}
		if t.Repo == "" {
			return fmt.Errorf("task %q has empty repo", t.ID)
		}
		if t.Prompt == "" {
			return fmt.Errorf("task %q has empty prompt", t.ID)
		}
		if _, dup := ids[t.ID]; dup {
			return fmt.Errorf("duplicate task id: %q", t.ID)
		}
		ids[t.ID] = struct{}{}
	}

	for _, t := range tf.Tasks {
		for _, dep := range t.DependsOn {
			if _, ok := ids[dep]; !ok {
				return fmt.Errorf("task %q depends on unknown task %q", t.ID, dep)
			}
		}
	}

	// validate runner profiles
	knownTypes := map[string]struct{}{"codex": {}, "claude": {}, "script": {}}
	// build set of known runner names: built-ins + profiles
	knownRunners := map[string]struct{}{"codex": {}, "claude": {}, "script": {}}
	for name, profile := range tf.Runners {
		if profile.Type == "" {
			return fmt.Errorf("runner profile %q has empty type", name)
		}
		if _, ok := knownTypes[profile.Type]; !ok {
			return fmt.Errorf("runner profile %q has unknown type %q", name, profile.Type)
		}
		knownRunners[name] = struct{}{}
	}

	// validate default_runner
	if tf.DefaultRunner != "" {
		if _, ok := knownRunners[tf.DefaultRunner]; !ok {
			return fmt.Errorf("default_runner %q is not a known runner", tf.DefaultRunner)
		}
	}

	// validate default_fallbacks
	for _, fb := range tf.DefaultFallbacks {
		if _, ok := knownRunners[fb]; !ok {
			return fmt.Errorf("default_fallbacks references unknown runner %q", fb)
		}
	}

	// validate per-task runner and fallbacks
	for _, t := range tf.Tasks {
		if t.Runner != "" {
			if _, ok := knownRunners[t.Runner]; !ok {
				return fmt.Errorf("task %q references unknown runner %q", t.ID, t.Runner)
			}
		}
		for _, fb := range t.Fallbacks {
			if _, ok := knownRunners[fb]; !ok {
				return fmt.Errorf("task %q fallback references unknown runner %q", t.ID, fb)
			}
		}
	}

	// validate review config
	if tf.Review != nil && tf.Review.Runner != "" {
		if _, ok := knownRunners[tf.Review.Runner]; !ok {
			return fmt.Errorf("review runner %q is not a known runner", tf.Review.Runner)
		}
	}

	// validate allowed_repos constraint
	if len(tf.AllowedRepos) > 0 {
		allowed := make(map[string]struct{}, len(tf.AllowedRepos))
		for _, r := range tf.AllowedRepos {
			allowed[r] = struct{}{}
		}
		for _, t := range tf.Tasks {
			if _, ok := allowed[t.Repo]; !ok {
				return fmt.Errorf("task %q targets repo %q not in allowed_repos", t.ID, t.Repo)
			}
		}
	}

	return nil
}

// ValidateRepos checks that all referenced repos exist on disk under reposDir.
// Repos are referenced as "owner/name" — we resolve to reposDir/name.
func ValidateRepos(tf *task.TaskFile, reposDir string) error {
	seen := make(map[string]struct{})
	for _, t := range tf.Tasks {
		if _, ok := seen[t.Repo]; ok {
			continue
		}
		seen[t.Repo] = struct{}{}

		repoPath := RepoPath(t.Repo, reposDir)
		info, err := os.Stat(repoPath)
		if err != nil {
			return fmt.Errorf("repo %q not found at %s: %w", t.Repo, repoPath, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("repo %q path %s is not a directory", t.Repo, repoPath)
		}
	}
	return nil
}

// RepoPath returns the filesystem path for a repo reference.
// "ppiankov/kafkaspectre" + "/home/user/repos" → "/home/user/repos/kafkaspectre"
func RepoPath(repo, reposDir string) string {
	name := filepath.Base(repo)
	return filepath.Join(reposDir, name)
}
