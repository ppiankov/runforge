package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func fakeInitEnv(runners []string, creds map[string]string, cpus int) *initEnv {
	runnerSet := make(map[string]bool, len(runners))
	for _, r := range runners {
		runnerSet[r] = true
	}
	return &initEnv{
		lookPath: func(name string) (string, error) {
			if runnerSet[name] {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		getenv: func(key string) string {
			return creds[key]
		},
		numCPU: func() int { return cpus },
		stat: func(name string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		writeFile: func(name string, data []byte, perm os.FileMode) error {
			return nil
		},
	}
}

func TestInitDryRun(t *testing.T) {
	env := fakeInitEnv(
		[]string{"codex", "claude"},
		map[string]string{"OPENAI_API_KEY": "sk-test"},
		8,
	)

	var stdout, stderr bytes.Buffer
	err := runInit(env, &stderr, &stdout, ".tokencontrol.yml", ".", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yaml := stdout.String()
	if !strings.Contains(yaml, "default_runner: codex") {
		t.Errorf("expected default_runner: codex in output, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "default_fallbacks:") {
		t.Errorf("expected default_fallbacks in output, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "- claude") {
		t.Errorf("expected claude in fallbacks, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "workers: 4") {
		t.Errorf("expected workers: 4 for 8 CPUs, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "# tokencontrol configuration") {
		t.Errorf("expected header comment, got:\n%s", yaml)
	}

	summary := stderr.String()
	if !strings.Contains(summary, "Detected runners: codex, claude") {
		t.Errorf("expected runner summary, got:\n%s", summary)
	}
	if !strings.Contains(summary, "OPENAI_API_KEY set") {
		t.Errorf("expected credential status, got:\n%s", summary)
	}
	if !strings.Contains(summary, "Dry run") {
		t.Errorf("expected dry run notice, got:\n%s", summary)
	}
}

func TestInitNoRunners(t *testing.T) {
	env := fakeInitEnv(nil, nil, 4)

	var stdout, stderr bytes.Buffer
	err := runInit(env, &stderr, &stdout, ".tokencontrol.yml", ".", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yaml := stdout.String()
	if !strings.Contains(yaml, "default_runner: script") {
		t.Errorf("expected script fallback, got:\n%s", yaml)
	}

	summary := stderr.String()
	if !strings.Contains(summary, "No runners detected") {
		t.Errorf("expected no-runners warning, got:\n%s", summary)
	}
}

func TestInitConfigExists(t *testing.T) {
	env := fakeInitEnv([]string{"codex"}, nil, 4)
	env.stat = func(name string) (os.FileInfo, error) {
		return nil, nil // file exists
	}

	var stdout, stderr bytes.Buffer
	err := runInit(env, &stderr, &stdout, ".tokencontrol.yml", ".", false, false)
	if err == nil {
		t.Fatal("expected error when config exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestInitForceOverwrite(t *testing.T) {
	var written []byte
	env := fakeInitEnv([]string{"codex"}, nil, 4)
	env.stat = func(name string) (os.FileInfo, error) {
		return nil, nil // file exists
	}
	env.writeFile = func(name string, data []byte, perm os.FileMode) error {
		written = data
		return nil
	}

	var stdout, stderr bytes.Buffer
	err := runInit(env, &stderr, &stdout, ".tokencontrol.yml", ".", true, false)
	if err != nil {
		t.Fatalf("unexpected error with --force: %v", err)
	}
	if len(written) == 0 {
		t.Error("expected config to be written with --force")
	}

	summary := stderr.String()
	if !strings.Contains(summary, "Config written to") {
		t.Errorf("expected written confirmation, got:\n%s", summary)
	}
}

func TestInitWriteError(t *testing.T) {
	env := fakeInitEnv([]string{"codex"}, nil, 4)
	env.writeFile = func(name string, data []byte, perm os.FileMode) error {
		return errors.New("permission denied")
	}

	var stdout, stderr bytes.Buffer
	err := runInit(env, &stderr, &stdout, ".tokencontrol.yml", ".", false, false)
	if err == nil {
		t.Fatal("expected error on write failure")
	}
	if !strings.Contains(err.Error(), "write config") {
		t.Errorf("expected write config error, got: %v", err)
	}
}

func TestClampWorkers(t *testing.T) {
	tests := []struct {
		cpus int
		want int
	}{
		{1, 2},  // min clamp
		{2, 2},  // min clamp
		{4, 2},  // 4/2=2
		{8, 4},  // 8/2=4
		{16, 8}, // 16/2=8
		{32, 8}, // max clamp
		{64, 8}, // max clamp
	}
	for _, tc := range tests {
		got := clampWorkers(tc.cpus)
		if got != tc.want {
			t.Errorf("clampWorkers(%d) = %d, want %d", tc.cpus, got, tc.want)
		}
	}
}

func TestInitAllRunners(t *testing.T) {
	env := fakeInitEnv(
		[]string{"codex", "claude", "gemini", "opencode", "cline", "qwen"},
		map[string]string{
			"OPENAI_API_KEY":    "sk-test",
			"ANTHROPIC_API_KEY": "sk-ant",
			"GEMINI_API_KEY":    "gkey",
		},
		16,
	)

	var stdout, stderr bytes.Buffer
	err := runInit(env, &stderr, &stdout, ".tokencontrol.yml", "/tmp/repos", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yaml := stdout.String()
	if !strings.Contains(yaml, "default_runner: codex") {
		t.Errorf("expected codex as default, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "repos_dir: /tmp/repos") {
		t.Errorf("expected repos_dir: /tmp/repos, got:\n%s", yaml)
	}
	if !strings.Contains(yaml, "workers: 8") {
		t.Errorf("expected workers: 8 for 16 CPUs, got:\n%s", yaml)
	}

	// All 6 runners should have profiles.
	for _, r := range []string{"codex", "claude", "gemini", "opencode", "cline", "qwen"} {
		if !strings.Contains(yaml, r+":") {
			t.Errorf("expected runner profile for %s", r)
		}
	}

	summary := stderr.String()
	if !strings.Contains(summary, "OPENAI_API_KEY set") {
		t.Errorf("expected OPENAI_API_KEY set, got:\n%s", summary)
	}
	if !strings.Contains(summary, "ANTHROPIC_API_KEY set") {
		t.Errorf("expected ANTHROPIC_API_KEY set, got:\n%s", summary)
	}
	if !strings.Contains(summary, "GEMINI_API_KEY set") {
		t.Errorf("expected GEMINI_API_KEY set, got:\n%s", summary)
	}
}

func TestInitReposDir(t *testing.T) {
	env := fakeInitEnv([]string{"codex"}, nil, 4)

	var stdout, stderr bytes.Buffer
	err := runInit(env, &stderr, &stdout, ".tokencontrol.yml", "/home/user/repos", false, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yaml := stdout.String()
	if !strings.Contains(yaml, "repos_dir: /home/user/repos") {
		t.Errorf("expected repos_dir: /home/user/repos, got:\n%s", yaml)
	}
}
