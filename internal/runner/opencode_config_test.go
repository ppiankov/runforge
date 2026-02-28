package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func writeOpencodeConfig(t *testing.T, dir, content string) {
	t.Helper()
	configDir := filepath.Join(dir, "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "opencode.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

const testOpencodeConfig = `{
  "model": "qwen coder/external-coder",
  "provider": {
    "qwen coder": {
      "name": "Qwen Coder",
      "models": {
        "external-coder": { "name": "External Coder" },
        "alpha-model": { "name": "Alpha Model" }
      }
    },
    "qwen thinking": {
      "name": "Qwen Thinking",
      "models": {
        "glm-4.6": { "name": "GLM 4.6" }
      }
    }
  }
}`

func TestLoadOpencodeConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	writeOpencodeConfig(t, dir, testOpencodeConfig)
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := loadOpencodeConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config, got nil")
	}
	if cfg.Model != "qwen coder/external-coder" {
		t.Errorf("default model = %q, want %q", cfg.Model, "qwen coder/external-coder")
	}
	if len(cfg.Provider) != 2 {
		t.Errorf("provider count = %d, want 2", len(cfg.Provider))
	}
}

func TestLoadOpencodeConfig_FileNotFound(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := loadOpencodeConfig()
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config, got: %+v", cfg)
	}
}

func TestLoadOpencodeConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeOpencodeConfig(t, dir, "not json{{{")
	t.Setenv("XDG_CONFIG_HOME", dir)

	_, err := loadOpencodeConfig()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestOpencodeConfig_HasModel(t *testing.T) {
	dir := t.TempDir()
	writeOpencodeConfig(t, dir, testOpencodeConfig)
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := loadOpencodeConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	tests := []struct {
		model string
		want  bool
	}{
		{"qwen coder/external-coder", true},
		{"qwen coder/alpha-model", true},
		{"qwen thinking/glm-4.6", true},
		{"qwen coder/nonexistent", false},
		{"unknown/model", false},
		{"no-slash", false},
	}

	for _, tt := range tests {
		if got := cfg.hasModel(tt.model); got != tt.want {
			t.Errorf("hasModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestOpencodeConfig_AvailableModels(t *testing.T) {
	dir := t.TempDir()
	writeOpencodeConfig(t, dir, testOpencodeConfig)
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := loadOpencodeConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	models := cfg.availableModels()
	if len(models) != 3 {
		t.Fatalf("available models = %v, want 3 models", models)
	}
	// should be sorted
	if models[0] >= models[1] {
		t.Errorf("models not sorted: %v", models)
	}
}

func TestResolveOpencodeModel_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	writeOpencodeConfig(t, dir, testOpencodeConfig)
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, _ := loadOpencodeConfig()

	resolved, err := resolveOpencodeModel(cfg, "qwen coder/external-coder")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "qwen coder/external-coder" {
		t.Errorf("resolved = %q, want unchanged", resolved)
	}
}

func TestResolveOpencodeModel_SameProviderDifferentModel(t *testing.T) {
	dir := t.TempDir()
	writeOpencodeConfig(t, dir, testOpencodeConfig)
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, _ := loadOpencodeConfig()

	resolved, err := resolveOpencodeModel(cfg, "qwen coder/nonexistent-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// should pick first alphabetical model from "qwen coder" provider
	if resolved != "qwen coder/alpha-model" {
		t.Errorf("resolved = %q, want %q", resolved, "qwen coder/alpha-model")
	}
}

func TestResolveOpencodeModel_UnknownProviderFallsToDefault(t *testing.T) {
	dir := t.TempDir()
	writeOpencodeConfig(t, dir, testOpencodeConfig)
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, _ := loadOpencodeConfig()

	resolved, err := resolveOpencodeModel(cfg, "unknown-provider/some-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "qwen coder/external-coder" {
		t.Errorf("resolved = %q, want default %q", resolved, "qwen coder/external-coder")
	}
}

func TestResolveOpencodeModel_NoDefaultError(t *testing.T) {
	cfg := &opencodeConfig{
		Model: "",
		Provider: map[string]*opencodeProviderConfig{
			"known": {
				Models: map[string]*opencodeModelEntry{
					"model-a": {Name: "A"},
				},
			},
		},
	}

	_, err := resolveOpencodeModel(cfg, "unknown/model")
	if err == nil {
		t.Fatal("expected error when no default and unknown provider")
	}
}

func TestResolveOpencodeModel_EmptyModel(t *testing.T) {
	cfg := &opencodeConfig{Model: "default/model"}

	resolved, err := resolveOpencodeModel(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "" {
		t.Errorf("resolved = %q, want empty", resolved)
	}
}

func TestResolveOpencodeModel_InvalidFormat(t *testing.T) {
	cfg := &opencodeConfig{Model: "default/model"}

	_, err := resolveOpencodeModel(cfg, "no-slash")
	if err == nil {
		t.Fatal("expected error for invalid model format")
	}
}

func TestResolveOpencodeModel_NilConfig(t *testing.T) {
	resolved, err := resolveOpencodeModel(nil, "some/model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "some/model" {
		t.Errorf("resolved = %q, want passthrough", resolved)
	}
}

func TestSplitModel(t *testing.T) {
	tests := []struct {
		input    string
		provider string
		model    string
		ok       bool
	}{
		{"provider/model", "provider", "model", true},
		{"multi word/model-name", "multi word", "model-name", true},
		{"no-slash", "", "", false},
		{"/leading-slash", "", "", false},
		{"trailing-slash/", "", "", false},
	}

	for _, tt := range tests {
		p, m, ok := splitModel(tt.input)
		if ok != tt.ok {
			t.Errorf("splitModel(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			continue
		}
		if ok {
			if p != tt.provider || m != tt.model {
				t.Errorf("splitModel(%q) = (%q, %q), want (%q, %q)", tt.input, p, m, tt.provider, tt.model)
			}
		}
	}
}
