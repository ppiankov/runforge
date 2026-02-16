package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSettings_Valid(t *testing.T) {
	content := `
workers: 8
repos_dir: ~/dev/repos
max_runtime: 45m
fail_fast: true
verify: true
`
	path := writeTemp(t, content)
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}

	if s.Workers != 8 {
		t.Errorf("workers: got %d, want 8", s.Workers)
	}
	if s.ReposDir != "~/dev/repos" {
		t.Errorf("repos_dir: got %q, want ~/dev/repos", s.ReposDir)
	}
	if s.MaxRuntime != 45*time.Minute {
		t.Errorf("max_runtime: got %v, want 45m", s.MaxRuntime)
	}
	if !s.FailFast {
		t.Error("fail_fast: got false, want true")
	}
	if !s.Verify {
		t.Error("verify: got false, want true")
	}
}

func TestLoadSettings_Partial(t *testing.T) {
	content := `workers: 12`
	path := writeTemp(t, content)
	s, err := LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}

	if s.Workers != 12 {
		t.Errorf("workers: got %d, want 12", s.Workers)
	}
	if s.ReposDir != "" {
		t.Errorf("repos_dir: got %q, want empty", s.ReposDir)
	}
	if s.MaxRuntime != 0 {
		t.Errorf("max_runtime: got %v, want 0", s.MaxRuntime)
	}
	if s.FailFast {
		t.Error("fail_fast: got true, want false")
	}
}

func TestLoadSettings_MissingFile(t *testing.T) {
	s, err := LoadSettings(filepath.Join(t.TempDir(), "nonexistent.yml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if s.Workers != 0 {
		t.Errorf("expected zero-value settings, got workers=%d", s.Workers)
	}
}

func TestLoadSettings_InvalidYAML(t *testing.T) {
	path := writeTemp(t, "workers: [invalid\n")
	_, err := LoadSettings(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadSettings_Duration(t *testing.T) {
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"max_runtime: 1h", time.Hour},
		{"max_runtime: 30m", 30 * time.Minute},
		{"max_runtime: 90s", 90 * time.Second},
		{"max_runtime: 1h30m", 90 * time.Minute},
	}

	for _, tc := range cases {
		path := writeTemp(t, tc.input)
		s, err := LoadSettings(path)
		if err != nil {
			t.Errorf("input %q: %v", tc.input, err)
			continue
		}
		if s.MaxRuntime != tc.want {
			t.Errorf("input %q: got %v, want %v", tc.input, s.MaxRuntime, tc.want)
		}
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".runforge.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
