package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

	for i := range tf.Tasks {
		tf.Tasks[i].SourceFile = path
	}

	return &tf, nil
}

// loadRaw reads and structurally validates a task file without checking
// dependency references. Used by LoadMulti where cross-file deps are
// validated after merging.
func loadRaw(path string) (*task.TaskFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tasks file: %w", err)
	}

	var tf task.TaskFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parse tasks file: %w", err)
	}

	if err := validateStructure(&tf); err != nil {
		return nil, err
	}

	for i := range tf.Tasks {
		tf.Tasks[i].SourceFile = path
	}

	return &tf, nil
}

// ResolveGlob expands a --tasks argument into concrete file paths.
// If the argument contains glob metacharacters it expands the pattern;
// otherwise it returns the literal path.
func ResolveGlob(pattern string) ([]string, error) {
	if !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}, nil
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no files match pattern %q", pattern)
	}
	sort.Strings(matches)
	return matches, nil
}

// LoadMulti loads multiple task files and returns them individually.
// Each file is structurally validated but dependency references are not
// checked (use MergeTaskFiles for cross-file validation).
func LoadMulti(paths []string) ([]*task.TaskFile, error) {
	files := make([]*task.TaskFile, 0, len(paths))
	for _, p := range paths {
		tf, err := loadRaw(p)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", p, err)
		}
		files = append(files, tf)
	}
	return files, nil
}

// MergeTaskFiles combines multiple task files into a single TaskFile.
// It validates that task IDs are unique across files, runner profiles
// don't conflict, and all dependency references resolve.
func MergeTaskFiles(files []*task.TaskFile) (*task.TaskFile, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no task files to merge")
	}
	if len(files) == 1 {
		return files[0], nil
	}

	merged := &task.TaskFile{
		DefaultRunner:    files[0].DefaultRunner,
		DefaultFallbacks: files[0].DefaultFallbacks,
		Review:           files[0].Review,
		Runners:          make(map[string]*task.RunnerProfileConfig),
	}

	allIDs := make(map[string]string)        // task ID → source file
	runnerSources := make(map[string]string) // runner name → source file
	allowedSet := make(map[string]struct{})

	for _, tf := range files {
		src := sourceLabel(tf)

		// merge tasks — check for cross-file ID collisions
		for _, t := range tf.Tasks {
			if prevFile, exists := allIDs[t.ID]; exists {
				return nil, fmt.Errorf("duplicate task id %q (in %s and %s)", t.ID, prevFile, t.SourceFile)
			}
			allIDs[t.ID] = t.SourceFile
			merged.Tasks = append(merged.Tasks, t)
		}

		// merge runner profiles — conflict = error
		for name, profile := range tf.Runners {
			if prevFile, exists := runnerSources[name]; exists {
				if !runnerProfilesEqual(merged.Runners[name], profile) {
					return nil, fmt.Errorf("runner profile %q conflicts between %s and %s", name, prevFile, src)
				}
				continue
			}
			runnerSources[name] = src
			merged.Runners[name] = profile
		}

		// merge allowed_repos (union)
		for _, r := range tf.AllowedRepos {
			if _, ok := allowedSet[r]; !ok {
				allowedSet[r] = struct{}{}
				merged.AllowedRepos = append(merged.AllowedRepos, r)
			}
		}

		// fill gaps for defaults
		if merged.DefaultRunner == "" && tf.DefaultRunner != "" {
			merged.DefaultRunner = tf.DefaultRunner
		}
		if len(merged.DefaultFallbacks) == 0 && len(tf.DefaultFallbacks) > 0 {
			merged.DefaultFallbacks = tf.DefaultFallbacks
		}
		if merged.Review == nil && tf.Review != nil {
			merged.Review = tf.Review
		}
	}

	// validate cross-file dependency references
	for _, t := range merged.Tasks {
		for _, dep := range t.DependsOn {
			if _, ok := allIDs[dep]; !ok {
				return nil, fmt.Errorf("task %q (from %s) depends on unknown task %q", t.ID, t.SourceFile, dep)
			}
		}
	}

	return merged, nil
}

func sourceLabel(tf *task.TaskFile) string {
	if len(tf.Tasks) > 0 && tf.Tasks[0].SourceFile != "" {
		return tf.Tasks[0].SourceFile
	}
	return "<unknown>"
}

func runnerProfilesEqual(a, b *task.RunnerProfileConfig) bool {
	if a.Type != b.Type || a.Model != b.Model || a.Profile != b.Profile {
		return false
	}
	if len(a.Env) != len(b.Env) {
		return false
	}
	for k, v := range a.Env {
		if b.Env[k] != v {
			return false
		}
	}
	return true
}

// validate checks structure, duplicate IDs, dependency references, and runner profiles.
func validate(tf *task.TaskFile) error {
	if err := validateStructure(tf); err != nil {
		return err
	}
	return validateDeps(tf)
}

// validateStructure checks task fields, duplicate IDs, runner profiles,
// and allowed_repos — everything except dependency references.
func validateStructure(tf *task.TaskFile) error {
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

	// validate runner profiles
	knownTypes := map[string]struct{}{"codex": {}, "claude": {}, "gemini": {}, "opencode": {}, "script": {}}
	knownRunners := map[string]struct{}{"codex": {}, "claude": {}, "gemini": {}, "opencode": {}, "script": {}}
	for name, profile := range tf.Runners {
		if profile.Type == "" {
			return fmt.Errorf("runner profile %q has empty type", name)
		}
		if _, ok := knownTypes[profile.Type]; !ok {
			return fmt.Errorf("runner profile %q has unknown type %q", name, profile.Type)
		}
		knownRunners[name] = struct{}{}
	}

	if tf.DefaultRunner != "" {
		if _, ok := knownRunners[tf.DefaultRunner]; !ok {
			return fmt.Errorf("default_runner %q is not a known runner", tf.DefaultRunner)
		}
	}

	for _, fb := range tf.DefaultFallbacks {
		if _, ok := knownRunners[fb]; !ok {
			return fmt.Errorf("default_fallbacks references unknown runner %q", fb)
		}
	}

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

	if tf.Review != nil && tf.Review.Runner != "" {
		if _, ok := knownRunners[tf.Review.Runner]; !ok {
			return fmt.Errorf("review runner %q is not a known runner", tf.Review.Runner)
		}
	}

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

// validateDeps checks that all depends_on references resolve within the file.
func validateDeps(tf *task.TaskFile) error {
	ids := make(map[string]struct{}, len(tf.Tasks))
	for _, t := range tf.Tasks {
		ids[t.ID] = struct{}{}
	}
	for _, t := range tf.Tasks {
		for _, dep := range t.DependsOn {
			if _, ok := ids[dep]; !ok {
				return fmt.Errorf("task %q depends on unknown task %q", t.ID, dep)
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
