package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ppiankov/tokencontrol/internal/config"
)

// fakeEnv returns a doctorEnv where all tools are found and all creds are set.
// ANCC is NOT available by default (lookPath returns error for "ancc").
func fakeEnv() *doctorEnv {
	return &doctorEnv{
		version: "0.11.0",
		lookPath: func(name string) (string, error) {
			if name == "ancc" {
				return "", errors.New("not found")
			}
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
		runCmd: func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("not available")
		},
	}
}

func TestDoctor_AllOK(t *testing.T) {
	env := fakeEnv()
	result := runDoctor(env, ".tokencontrol.yml")

	if result.Status != "ok" {
		t.Fatalf("expected ok, got %s", result.Status)
	}
	// 1 version + 6 runners + 3 creds + 1 config + 1 graylist + 2 companions + 1 ancc(info) + 1 git = 16
	if len(result.Checks) != 16 {
		t.Fatalf("expected 16 checks, got %d", len(result.Checks))
	}
	for _, c := range result.Checks {
		if c.Status != "ok" && c.Status != "info" {
			t.Errorf("check %s: expected ok or info, got %s (%s)", c.Name, c.Status, c.Message)
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

	result := runDoctor(env, ".tokencontrol.yml")

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

	result := runDoctor(env, ".tokencontrol.yml")

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

	result := runDoctor(env, ".tokencontrol.yml")

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
		return nil, errors.New("parse config .tokencontrol.yml: yaml: unmarshal errors")
	}

	result := runDoctor(env, ".tokencontrol.yml")

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

	result := runDoctor(env, ".tokencontrol.yml")

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
	result := runDoctor(env, ".tokencontrol.yml")

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
	if len(parsed.Checks) != 16 {
		t.Errorf("expected 16 checks, got %d", len(parsed.Checks))
	}
	// Verify first check is version
	if parsed.Checks[0].Name != "tokencontrol-version" {
		t.Errorf("expected first check tokencontrol-version, got %s", parsed.Checks[0].Name)
	}
}

func TestDoctor_TextOutput(t *testing.T) {
	env := fakeEnv()
	result := runDoctor(env, ".tokencontrol.yml")

	var buf bytes.Buffer
	formatDoctorText(&buf, result)

	out := buf.String()
	if !strings.Contains(out, "tokencontrol version") {
		t.Error("text output missing 'tokencontrol version'")
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
				if name == "ancc" {
					return "", errors.New("not found")
				}
				return "/usr/local/bin/" + name, nil
			}
			if tt.credMiss {
				env.getenv = func(key string) string { return "" }
			}

			result := runDoctor(env, ".tokencontrol.yml")
			if result.Status != tt.want {
				t.Errorf("expected %s, got %s", tt.want, result.Status)
			}
		})
	}
}

// fakeANCCJSON returns mock ANCC JSON output with the given agents.
func fakeANCCJSON(agents []anccAgent) []byte {
	out, _ := json.Marshal(anccSkillsResult{Agents: agents})
	return out
}

// fakeEnvWithANCC returns a doctorEnv with ANCC available and returning the given agents.
func fakeEnvWithANCC(agents []anccAgent) *doctorEnv {
	env := fakeEnv()
	env.lookPath = func(name string) (string, error) {
		return "/usr/local/bin/" + name, nil
	}
	env.runCmd = func(name string, args ...string) ([]byte, error) {
		return fakeANCCJSON(agents), nil
	}
	return env
}

func TestDoctor_ANCCUnavailable(t *testing.T) {
	env := fakeEnv() // ancc not in PATH by default
	result := runDoctor(env, ".tokencontrol.yml")

	found := false
	for _, c := range result.Checks {
		if c.Name == "ancc" {
			found = true
			if c.Status != "info" {
				t.Errorf("expected info for ancc, got %s", c.Status)
			}
			if !strings.Contains(c.Message, "skipped") {
				t.Errorf("expected 'skipped' in message, got %q", c.Message)
			}
		}
		// no agent-skills-* checks should exist
		if strings.HasPrefix(c.Name, "agent-skills-") || strings.HasPrefix(c.Name, "agent-hooks-") {
			t.Errorf("unexpected ANCC check %s when ancc unavailable", c.Name)
		}
	}
	if !found {
		t.Fatal("ancc check not found")
	}
	// overall status should still be ok (info does not downgrade)
	if result.Status != "ok" {
		t.Errorf("expected ok overall, got %s", result.Status)
	}
}

func TestDoctor_ANCCAvailable(t *testing.T) {
	agents := []anccAgent{
		{Name: "claude-code", Skills: 5, Hooks: 3, MCP: 2, Tokens: 12000},
		{Name: "codex", Skills: 2, Hooks: 0, MCP: 0, Tokens: 8000},
	}
	env := fakeEnvWithANCC(agents)
	result := runDoctor(env, ".tokencontrol.yml")

	// Should have ancc=ok, agent-skills-claude=ok, agent-hooks-claude=ok,
	// agent-skills-codex=ok, agent-token-overhead=ok
	checkMap := make(map[string]DoctorCheck)
	for _, c := range result.Checks {
		checkMap[c.Name] = c
	}

	if c, ok := checkMap["ancc"]; !ok || c.Status != "ok" {
		t.Errorf("expected ancc=ok, got %+v", checkMap["ancc"])
	}
	if c, ok := checkMap["agent-skills-claude"]; !ok || c.Status != "ok" {
		t.Errorf("expected agent-skills-claude=ok, got %+v", c)
	}
	if c, ok := checkMap["agent-hooks-claude"]; !ok || c.Status != "ok" {
		t.Errorf("expected agent-hooks-claude=ok, got %+v", c)
	}
	if c, ok := checkMap["agent-skills-codex"]; !ok || c.Status != "ok" {
		t.Errorf("expected agent-skills-codex=ok, got %+v", c)
	}
	if c, ok := checkMap["agent-token-overhead"]; !ok || c.Status != "ok" {
		t.Errorf("expected agent-token-overhead=ok, got %+v", c)
	}
}

func TestDoctor_ZeroSkills(t *testing.T) {
	agents := []anccAgent{
		{Name: "codex", Skills: 0, Hooks: 0, MCP: 0, Tokens: 1000},
	}
	env := fakeEnvWithANCC(agents)
	result := runDoctor(env, ".tokencontrol.yml")

	for _, c := range result.Checks {
		if c.Name == "agent-skills-codex" {
			if c.Status != "warn" {
				t.Errorf("expected warn for zero skills, got %s", c.Status)
			}
			if !strings.Contains(c.Message, "0 skills") {
				t.Errorf("expected '0 skills' in message, got %q", c.Message)
			}
			return
		}
	}
	t.Fatal("agent-skills-codex check not found")
}

func TestDoctor_ZeroHooksOnSafeRunner(t *testing.T) {
	agents := []anccAgent{
		{Name: "claude-code", Skills: 3, Hooks: 0, MCP: 1, Tokens: 5000},
	}
	env := fakeEnvWithANCC(agents)
	result := runDoctor(env, ".tokencontrol.yml")

	for _, c := range result.Checks {
		if c.Name == "agent-hooks-claude" {
			if c.Status != "error" {
				t.Errorf("expected error for safe runner with 0 hooks, got %s", c.Status)
			}
			if !strings.Contains(c.Message, "pastewatch") {
				t.Errorf("expected pastewatch warning in message, got %q", c.Message)
			}
			return
		}
	}
	t.Fatal("agent-hooks-claude check not found")
}

func TestDoctor_TokenOverhead(t *testing.T) {
	agents := []anccAgent{
		{Name: "claude-code", Skills: 3, Hooks: 2, MCP: 1, Tokens: 30000},
		{Name: "codex", Skills: 2, Hooks: 0, MCP: 0, Tokens: 25000},
	}
	env := fakeEnvWithANCC(agents)
	result := runDoctor(env, ".tokencontrol.yml")

	for _, c := range result.Checks {
		if c.Name == "agent-token-overhead" {
			if c.Status != "warn" {
				t.Errorf("expected warn for >50K tokens, got %s", c.Status)
			}
			if !strings.Contains(c.Message, "55K") {
				t.Errorf("expected '55K' in message, got %q", c.Message)
			}
			return
		}
	}
	t.Fatal("agent-token-overhead check not found")
}

func TestMapANCCAgents(t *testing.T) {
	agents := []anccAgent{
		{Name: "claude-code", Skills: 5},
		{Name: "codex", Skills: 2},
		{Name: "unknown-agent", Skills: 1},
	}
	m := mapANCCAgents(agents)
	if _, ok := m["claude"]; !ok {
		t.Error("expected claude-code mapped to claude")
	}
	if _, ok := m["codex"]; !ok {
		t.Error("expected codex mapped to codex")
	}
	if _, ok := m["unknown-agent"]; ok {
		t.Error("unknown-agent should not be mapped")
	}
	if len(m) != 2 {
		t.Errorf("expected 2 mappings, got %d", len(m))
	}
}
