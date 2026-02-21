package runner

import (
	"strings"
	"testing"
)

func TestSanitizeEnvStripsKeys(t *testing.T) {
	input := []string{
		"HOME=/home/user",
		"PATH=/usr/bin",
		"GROQ_API_KEY=gsk_secret123",
		"OPENAI_API_KEY=sk-secret456",
		"ANTHROPIC_API_KEY=sk-ant-secret",
		"NULLBOT_PROFILE=vm-cloud",
		"CHAINWATCH_AUDIT=/tmp/log",
		"RUNFORGE_TOKEN=abc",
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI",
		"AWS_SESSION_TOKEN=FwoGZX",
		"GITHUB_TOKEN=ghp_abc123",
		"API_KEY=generic-key",
		"API_SECRET=generic-secret",
		"SECRET_KEY=django-secret",
	}

	result := sanitizeEnv(input)

	// Only HOME and PATH should survive.
	if len(result) != 2 {
		t.Errorf("expected 2 safe vars, got %d: %v", len(result), result)
	}

	for _, entry := range result {
		name, _, _ := strings.Cut(entry, "=")
		if name != "HOME" && name != "PATH" {
			t.Errorf("unexpected env var survived: %s", name)
		}
	}
}

func TestSanitizeEnvPreservesSafe(t *testing.T) {
	input := []string{
		"HOME=/home/user",
		"PATH=/usr/bin:/usr/local/bin",
		"LANG=en_US.UTF-8",
		"TERM=xterm-256color",
		"EDITOR=vim",
	}

	result := sanitizeEnv(input)

	if len(result) != len(input) {
		t.Errorf("expected %d vars, got %d", len(input), len(result))
	}
}

func TestSanitizeEnvCaseInsensitive(t *testing.T) {
	input := []string{
		"groq_api_key=lower",
		"Openai_Api_Key=mixed",
	}

	result := sanitizeEnv(input)

	if len(result) != 0 {
		t.Errorf("expected 0 vars (case-insensitive strip), got %d: %v", len(result), result)
	}
}

func TestSanitizeEnvEmptyInput(t *testing.T) {
	result := sanitizeEnv(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 vars for nil input, got %d", len(result))
	}
}

func TestSanitizeEnvMalformedEntry(t *testing.T) {
	input := []string{
		"HOME=/home/user",
		"NO_EQUALS_SIGN",
		"PATH=/usr/bin",
	}

	result := sanitizeEnv(input)

	// Malformed entries (no =) are preserved.
	if len(result) != 3 {
		t.Errorf("expected 3 vars, got %d: %v", len(result), result)
	}
}
