package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppiankov/codexrun/internal/task"
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
	if tf.Tasks[1].DependsOn != "t1" {
		t.Errorf("expected depends_on t1, got %q", tf.Tasks[1].DependsOn)
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
