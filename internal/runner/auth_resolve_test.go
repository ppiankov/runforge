package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAPIKey_EnvVarPriority(t *testing.T) {
	// env var should always win over file
	getenv := func(key string) string {
		if key == "OPENAI_API_KEY" {
			return "sk-from-env"
		}
		return ""
	}

	got := ResolveAPIKey("openai", getenv)
	if got != "sk-from-env" {
		t.Errorf("expected env var value, got %q", got)
	}
}

func TestResolveAPIKey_NoEnvFallsBackToFile(t *testing.T) {
	// create a fake ~/.codex/auth.json with api_key mode
	home := t.TempDir()
	t.Setenv("HOME", home)

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	authJSON := `{"auth_mode":"api_key","OPENAI_API_KEY":"sk-from-file"}`
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	noEnv := func(string) string { return "" }
	got := ResolveAPIKey("openai", noEnv)
	if got != "sk-from-file" {
		t.Errorf("expected file value 'sk-from-file', got %q", got)
	}
}

func TestResolveAPIKey_OAuthModeReturnsEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// OAuth mode — OPENAI_API_KEY is empty
	authJSON := `{"auth_mode":"chatgpt","OPENAI_API_KEY":"","tokens":{"id_token":"xxx"}}`
	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte(authJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	noEnv := func(string) string { return "" }
	got := ResolveAPIKey("openai", noEnv)
	if got != "" {
		t.Errorf("expected empty for OAuth mode, got %q", got)
	}
}

func TestResolveAPIKey_MissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	noEnv := func(string) string { return "" }
	got := ResolveAPIKey("openai", noEnv)
	if got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}

func TestResolveAPIKey_MalformedJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(codexDir, "auth.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	noEnv := func(string) string { return "" }
	got := ResolveAPIKey("openai", noEnv)
	if got != "" {
		t.Errorf("expected empty for malformed JSON, got %q", got)
	}
}

func TestResolveAPIKey_AnthropicKeyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".anthropic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "api_key"), []byte("sk-ant-from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	noEnv := func(string) string { return "" }
	got := ResolveAPIKey("anthropic", noEnv)
	if got != "sk-ant-from-file" {
		t.Errorf("expected 'sk-ant-from-file', got %q", got)
	}
}

func TestResolveAPIKey_UnknownProvider(t *testing.T) {
	noEnv := func(string) string { return "" }
	got := ResolveAPIKey("unknown", noEnv)
	if got != "" {
		t.Errorf("expected empty for unknown provider, got %q", got)
	}
}

func TestTrimKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sk-test\n", "sk-test"},
		{" sk-test ", "sk-test"},
		{"\t sk-test \n\r", "sk-test"},
		{"", ""},
		{"sk-test", "sk-test"},
	}
	for _, tt := range tests {
		if got := trimKey([]byte(tt.input)); got != tt.want {
			t.Errorf("trimKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
