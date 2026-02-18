package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppiankov/runforge/internal/task"
)

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{
		"tasks": [
			{"id": "t1", "repo": "org/repo1", "priority": 1, "title": "Task 1", "prompt": "Do thing 1"},
			{"id": "t2", "repo": "org/repo1", "priority": 2, "depends_on": "t1", "title": "Task 2", "prompt": "Do thing 2"}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	tf, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tf.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tf.Tasks))
	}
	if tf.Tasks[0].ID != "t1" {
		t.Errorf("expected id t1, got %q", tf.Tasks[0].ID)
	}
	if len(tf.Tasks[1].DependsOn) != 1 || tf.Tasks[1].DependsOn[0] != "t1" {
		t.Errorf("expected depends_on [t1], got %v", tf.Tasks[1].DependsOn)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/tasks.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(`{invalid`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoad_EmptyTasks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(`{"tasks": []}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty tasks")
	}
}

func TestLoad_DuplicateID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{"tasks": [
		{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": "a"},
		{"id": "t1", "repo": "org/r", "priority": 1, "title": "B", "prompt": "b"}
	]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestLoad_DanglingDependency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{"tasks": [
		{"id": "t1", "repo": "org/r", "priority": 1, "depends_on": "nonexistent", "title": "A", "prompt": "a"}
	]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for dangling dependency")
	}
}

func TestLoad_EmptyID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{"tasks": [{"id": "", "repo": "org/r", "priority": 1, "title": "A", "prompt": "a"}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestLoad_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{"tasks": [{"id": "t1", "repo": "", "priority": 1, "title": "A", "prompt": "a"}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty repo")
	}
}

func TestLoad_EmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{"tasks": [{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": ""}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
}

func TestValidateRepos(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "repo1"), 0o755); err != nil {
		t.Fatal(err)
	}

	tf := loadTestTasks(t, `{"tasks": [
		{"id": "t1", "repo": "org/repo1", "priority": 1, "title": "A", "prompt": "a"}
	]}`)

	if err := ValidateRepos(tf, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRepos_Missing(t *testing.T) {
	dir := t.TempDir()
	tf := loadTestTasks(t, `{"tasks": [
		{"id": "t1", "repo": "org/missing", "priority": 1, "title": "A", "prompt": "a"}
	]}`)

	err := ValidateRepos(tf, dir)
	if err == nil {
		t.Fatal("expected error for missing repo")
	}
}

func TestRepoPath(t *testing.T) {
	tests := []struct {
		repo     string
		reposDir string
		want     string
	}{
		{"ppiankov/kafkaspectre", "/home/user/repos", "/home/user/repos/kafkaspectre"},
		{"org/tool", "/opt/src", "/opt/src/tool"},
	}

	for _, tt := range tests {
		got := RepoPath(tt.repo, tt.reposDir)
		if got != tt.want {
			t.Errorf("RepoPath(%q, %q) = %q, want %q", tt.repo, tt.reposDir, got, tt.want)
		}
	}
}

func TestLoad_AllowedRepos(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{
		"allowed_repos": ["org/repo1", "org/repo2"],
		"tasks": [
			{"id": "t1", "repo": "org/repo1", "priority": 1, "title": "A", "prompt": "a"},
			{"id": "t2", "repo": "org/repo2", "priority": 1, "title": "B", "prompt": "b"}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	tf, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tf.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tf.Tasks))
	}
}

func TestLoad_AllowedReposViolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{
		"allowed_repos": ["org/repo1"],
		"tasks": [
			{"id": "t1", "repo": "org/repo1", "priority": 1, "title": "A", "prompt": "a"},
			{"id": "t2", "repo": "org/forbidden", "priority": 1, "title": "B", "prompt": "b"}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for repo not in allowed_repos")
	}
}

func TestLoad_ArrayDeps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{
		"tasks": [
			{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": "a"},
			{"id": "t2", "repo": "org/r", "priority": 1, "title": "B", "prompt": "b"},
			{"id": "t3", "repo": "org/r", "priority": 1, "depends_on": ["t1", "t2"], "title": "C", "prompt": "c"}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	tf, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tf.Tasks[2].DependsOn) != 2 {
		t.Fatalf("expected 2 deps, got %d", len(tf.Tasks[2].DependsOn))
	}
	if tf.Tasks[2].DependsOn[0] != "t1" || tf.Tasks[2].DependsOn[1] != "t2" {
		t.Errorf("expected deps [t1 t2], got %v", tf.Tasks[2].DependsOn)
	}
}

func TestLoad_StringDepBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{
		"tasks": [
			{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": "a"},
			{"id": "t2", "repo": "org/r", "priority": 1, "depends_on": "t1", "title": "B", "prompt": "b"}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	tf, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tf.Tasks[1].DependsOn) != 1 || tf.Tasks[1].DependsOn[0] != "t1" {
		t.Errorf("expected deps [t1], got %v", tf.Tasks[1].DependsOn)
	}
}

func TestLoad_DanglingArrayDep(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{
		"tasks": [
			{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": "a"},
			{"id": "t2", "repo": "org/r", "priority": 1, "depends_on": ["t1", "missing"], "title": "B", "prompt": "b"}
		]
	}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for dangling dep in array")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected error to mention 'missing', got: %v", err)
	}
}

func TestLoad_SourceFileStamped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	data := `{"tasks": [{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": "a"}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	tf, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tf.Tasks[0].SourceFile != path {
		t.Errorf("expected SourceFile %q, got %q", path, tf.Tasks[0].SourceFile)
	}
}

func TestResolveGlob_LiteralPath(t *testing.T) {
	paths, err := ResolveGlob("/some/file.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 1 || paths[0] != "/some/file.json" {
		t.Errorf("expected [/some/file.json], got %v", paths)
	}
}

func TestResolveGlob_Pattern(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.json", "b.json", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	paths, err := ResolveGlob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(paths), paths)
	}
	if filepath.Base(paths[0]) != "a.json" || filepath.Base(paths[1]) != "b.json" {
		t.Errorf("unexpected matches: %v", paths)
	}
}

func TestResolveGlob_NoMatches(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolveGlob(filepath.Join(dir, "*.json"))
	if err == nil {
		t.Fatal("expected error for no matches")
	}
	if !strings.Contains(err.Error(), "no files match") {
		t.Errorf("expected 'no files match' error, got: %v", err)
	}
}

func TestLoadMulti(t *testing.T) {
	dir := t.TempDir()
	file1 := filepath.Join(dir, "a.json")
	file2 := filepath.Join(dir, "b.json")
	if err := os.WriteFile(file1, []byte(`{"tasks": [{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": "a"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file2, []byte(`{"tasks": [{"id": "t2", "repo": "org/r", "priority": 1, "title": "B", "prompt": "b"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := LoadMulti([]string{file1, file2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0].Tasks[0].SourceFile != file1 {
		t.Errorf("expected SourceFile %q, got %q", file1, files[0].Tasks[0].SourceFile)
	}
}

func TestMergeTaskFiles_Basic(t *testing.T) {
	tf1 := &task.TaskFile{
		DefaultRunner: "codex",
		Tasks: []task.Task{
			{ID: "t1", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a", SourceFile: "a.json"},
		},
		AllowedRepos: []string{"org/r"},
	}
	tf2 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t2", Repo: "org/r2", Priority: 1, Title: "B", Prompt: "b", SourceFile: "b.json"},
		},
		AllowedRepos: []string{"org/r2"},
	}

	merged, err := MergeTaskFiles([]*task.TaskFile{tf1, tf2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(merged.Tasks))
	}
	if merged.DefaultRunner != "codex" {
		t.Errorf("expected default_runner 'codex', got %q", merged.DefaultRunner)
	}
	if len(merged.AllowedRepos) != 2 {
		t.Errorf("expected 2 allowed_repos, got %d", len(merged.AllowedRepos))
	}
}

func TestMergeTaskFiles_DuplicateTaskID(t *testing.T) {
	tf1 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t1", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a", SourceFile: "a.json"},
		},
	}
	tf2 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t1", Repo: "org/r", Priority: 1, Title: "B", Prompt: "b", SourceFile: "b.json"},
		},
	}

	_, err := MergeTaskFiles([]*task.TaskFile{tf1, tf2})
	if err == nil {
		t.Fatal("expected error for duplicate task ID across files")
	}
	if !strings.Contains(err.Error(), "duplicate task id") {
		t.Errorf("expected 'duplicate task id' error, got: %v", err)
	}
}

func TestMergeTaskFiles_ConflictingRunnerProfile(t *testing.T) {
	tf1 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t1", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a", SourceFile: "a.json"},
		},
		Runners: map[string]*task.RunnerProfileConfig{
			"deepseek": {Type: "codex", Model: "deepseek-chat"},
		},
	}
	tf2 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t2", Repo: "org/r", Priority: 1, Title: "B", Prompt: "b", SourceFile: "b.json"},
		},
		Runners: map[string]*task.RunnerProfileConfig{
			"deepseek": {Type: "codex", Model: "different-model"},
		},
	}

	_, err := MergeTaskFiles([]*task.TaskFile{tf1, tf2})
	if err == nil {
		t.Fatal("expected error for conflicting runner profile")
	}
	if !strings.Contains(err.Error(), "conflicts") {
		t.Errorf("expected 'conflicts' error, got: %v", err)
	}
}

func TestMergeTaskFiles_IdenticalRunnerProfileOK(t *testing.T) {
	profile := &task.RunnerProfileConfig{Type: "codex", Profile: "ds"}
	tf1 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t1", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a", SourceFile: "a.json"},
		},
		Runners: map[string]*task.RunnerProfileConfig{"deepseek": profile},
	}
	tf2 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t2", Repo: "org/r", Priority: 1, Title: "B", Prompt: "b", SourceFile: "b.json"},
		},
		Runners: map[string]*task.RunnerProfileConfig{
			"deepseek": {Type: "codex", Profile: "ds"},
		},
	}

	_, err := MergeTaskFiles([]*task.TaskFile{tf1, tf2})
	if err != nil {
		t.Fatalf("expected no error for identical runner profile, got: %v", err)
	}
}

func TestMergeTaskFiles_CrossFileDependency(t *testing.T) {
	tf1 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t1", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a", SourceFile: "a.json"},
		},
	}
	tf2 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t2", Repo: "org/r", Priority: 1, Title: "B", Prompt: "b", SourceFile: "b.json", DependsOn: []string{"t1"}},
		},
	}

	merged, err := MergeTaskFiles([]*task.TaskFile{tf1, tf2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(merged.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(merged.Tasks))
	}
}

func TestMergeTaskFiles_DanglingCrossFileDep(t *testing.T) {
	tf1 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t1", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a", SourceFile: "a.json"},
		},
	}
	tf2 := &task.TaskFile{
		Tasks: []task.Task{
			{ID: "t2", Repo: "org/r", Priority: 1, Title: "B", Prompt: "b", SourceFile: "b.json", DependsOn: []string{"nonexistent"}},
		},
	}

	_, err := MergeTaskFiles([]*task.TaskFile{tf1, tf2})
	if err == nil {
		t.Fatal("expected error for dangling cross-file dependency")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("expected error to mention 'nonexistent', got: %v", err)
	}
}

func TestMergeTaskFiles_SingleFile(t *testing.T) {
	tf := &task.TaskFile{
		DefaultRunner: "codex",
		Tasks: []task.Task{
			{ID: "t1", Repo: "org/r", Priority: 1, Title: "A", Prompt: "a"},
		},
	}
	merged, err := MergeTaskFiles([]*task.TaskFile{tf})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged != tf {
		t.Error("single file merge should return the original pointer")
	}
}

func loadTestTasks(t *testing.T, data string) *task.TaskFile {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	tf, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return tf
}
