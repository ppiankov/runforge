package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ppiankov/runforge/internal/config"
)

// fakeEnv returns a doctorEnv where all tools are found and all creds are set.
func fakeEnv() *doctorEnv {
	return &doctorEnv{
		version: "0.11.0",
		lookPath: func(name string) (string, error) {
			return "/usr/local/bin/" + name, nil
		},
		getenv: func(key string) string {
			return "test-value"
		},
		loadConfig: func(path string) (*config.Settings, error) {
			return &config.Settings{
				DefaultRunner:    "codex",
				DefaultFallbacks: []string{"claude"},
				Runners: map[string]*config.RunnerProfile{
					"codex":  {Type: "codex"},
					"claude": {Type: "claude"},
				},
			}, nil
		},
	}
}

func TestDoctor_AllOK(t *testing.T) {
	env := fakeEnv()
	result := runDoctor(env, ".runforge.yml")

	if result.Status != "ok" {
		t.Fatalf("expected ok, got %s", result.Status)
	}
	// 1 version + 6 runners + 3 creds + 1 config + 1 graylist + 2 companions + 1 git = 15
	if len(result.Checks) != 15 {
		t.Fatalf("expected 15 checks, got %d", len(result.Checks))
	}
	for _, c := range result.Checks {
		if c.Status != "ok" {
			t.Errorf("check %s: expected ok, got %s (%s)", c.Name, c.Status, c.Message)
		}
	}
}

func TestDoctor_MissingRunner(t *testing.T) {
	env := fakeEnv()
	env.lookPath = func(name string) (string, error) {
		if name == "codex" {
			return "", errors.New("not found")
		}
		return "/usr/local/bin/" + name, nil
	}

	result := runDoctor(env, ".runforge.yml")

	if result.Status != "warn" {
		t.Fatalf("expected warn, got %s", result.Status)
	}
	for _, c := range result.Checks {
		if c.Name == "runner-codex" {
			if c.Status != "warn" {
				t.Errorf("expected warn for runner-codex, got %s", c.Status)
			}
			return
		}
	}
	t.Fatal("runner-codex check not found")
}

func TestDoctor_MissingGit(t *testing.T) {
	env := fakeEnv()
	env.lookPath = func(name string) (string, error) {
		if name == "git" {
			return "", errors.New("not found")
		}
		return "/usr/local/bin/" + name, nil
	}

	result := runDoctor(env, ".runforge.yml")

	if result.Status != "error" {
		t.Fatalf("expected error, got %s", result.Status)
	}
	for _, c := range result.Checks {
		if c.Name == "git" {
			if c.Status != "error" {
				t.Errorf("expected error for git, got %s", c.Status)
			}
			return
		}
	}
	t.Fatal("git check not found")
}

func TestDoctor_MissingCredential(t *testing.T) {
	env := fakeEnv()
	env.getenv = func(key string) string {
		if key == "ANTHROPIC_API_KEY" {
			return ""
		}
		return "test-value"
	}

	result := runDoctor(env, ".runforge.yml")

	if result.Status != "warn" {
		t.Fatalf("expected warn, got %s", result.Status)
	}
	for _, c := range result.Checks {
		if c.Name == "cred-anthropic-api-key" {
			if c.Status != "warn" {
				t.Errorf("expected warn for cred-anthropic-api-key, got %s", c.Status)
			}
			if c.Message != "not set" {
				t.Errorf("expected 'not set', got %q", c.Message)
			}
			return
		}
	}
	t.Fatal("cred-anthropic-api-key check not found")
}

func TestDoctor_BadConfig(t *testing.T) {
	env := fakeEnv()
	env.loadConfig = func(path string) (*config.Settings, error) {
		return nil, errors.New("parse config .runforge.yml: yaml: unmarshal errors")
	}

	result := runDoctor(env, ".runforge.yml")

	for _, c := range result.Checks {
		if c.Name == "config" {
			if c.Status != "warn" {
				t.Errorf("expected warn for config, got %s", c.Status)
			}
			if !strings.Contains(c.Message, "unmarshal") {
				t.Errorf("expected error message in config check, got %q", c.Message)
			}
			return
		}
	}
	t.Fatal("config check not found")
}

func TestDoctor_NoConfig(t *testing.T) {
	env := fakeEnv()
	env.loadConfig = func(path string) (*config.Settings, error) {
		return &config.Settings{}, nil
	}

	result := runDoctor(env, ".runforge.yml")

	for _, c := range result.Checks {
		if c.Name == "config" {
			if c.Status != "ok" {
				t.Errorf("expected ok for missing config, got %s", c.Status)
			}
			if !strings.Contains(c.Message, "no config") {
				t.Errorf("expected 'no config' message, got %q", c.Message)
			}
			return
		}
	}
	t.Fatal("config check not found")
}

func TestDoctor_JSONOutput(t *testing.T) {
	env := fakeEnv()
	result := runDoctor(env, ".runforge.yml")

	var buf bytes.Buffer
	if err := formatDoctorJSON(&buf, result); err != nil {
		t.Fatalf("formatDoctorJSON: %v", err)
	}

	var parsed DoctorResult
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if parsed.Status != "ok" {
		t.Errorf("expected ok, got %s", parsed.Status)
	}
	if len(parsed.Checks) != 15 {
		t.Errorf("expected 15 checks, got %d", len(parsed.Checks))
	}
	// Verify first check is version
	if parsed.Checks[0].Name != "runforge-version" {
		t.Errorf("expected first check runforge-version, got %s", parsed.Checks[0].Name)
	}
}

func TestDoctor_TextOutput(t *testing.T) {
	env := fakeEnv()
	result := runDoctor(env, ".runforge.yml")

	var buf bytes.Buffer
	formatDoctorText(&buf, result)

	out := buf.String()
	if !strings.Contains(out, "runforge version") {
		t.Error("text output missing 'runforge version'")
	}
	if !strings.Contains(out, "Runner: codex") {
		t.Error("text output missing 'Runner: codex'")
	}
	if !strings.Contains(out, "Result: OK") {
		t.Error("text output missing 'Result: OK'")
	}
	if !strings.Contains(out, "OPENAI_API_KEY") {
		t.Error("text output missing 'OPENAI_API_KEY'")
	}
}

func TestDoctor_StatusAggregation(t *testing.T) {
	tests := []struct {
		name     string
		gitMiss  bool
		credMiss bool
		want     string
	}{
		{"all ok", false, false, "ok"},
		{"warn only", false, true, "warn"},
		{"error overrides warn", true, true, "error"},
		{"error alone", true, false, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := fakeEnv()
			env.lookPath = func(name string) (string, error) {
				if tt.gitMiss && name == "git" {
					return "", errors.New("not found")
				}
				return "/usr/local/bin/" + name, nil
			}
			if tt.credMiss {
				env.getenv = func(key string) string { return "" }
			}

			result := runDoctor(env, ".runforge.yml")
			if result.Status != tt.want {
				t.Errorf("expected %s, got %s", tt.want, result.Status)
			}
		})
	}
}
