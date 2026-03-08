package runner

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// ResolveAPIKey returns an API key for the given provider.
// Priority: env var → CLI tool config file → empty string.
func ResolveAPIKey(provider string, getenv func(string) string) string {
	// 1. check env var (highest priority)
	envVar := ProviderEnvVar(provider)
	if envVar != "" {
		if key := getenv(envVar); key != "" {
			return key
		}
	}

	// 2. check CLI tool config files
	switch provider {
	case "openai":
		return resolveOpenAIKey()
	case "anthropic":
		return resolveAnthropicKey()
	case "deepseek":
		return resolveDeepSeekKey()
	default:
		return ""
	}
}

// resolveOpenAIKey reads the API key from ~/.codex/auth.json (if using api_key auth mode).
func resolveOpenAIKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// try ~/.codex/auth.json
	data, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return ""
	}

	var auth struct {
		AuthMode  string `json:"auth_mode"`
		OpenAIKey string `json:"OPENAI_API_KEY"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		slog.Debug("failed to parse codex auth.json", "error", err)
		return ""
	}

	// only use the key if it's actually set (OAuth mode leaves it empty)
	if auth.OpenAIKey != "" {
		return auth.OpenAIKey
	}

	if auth.AuthMode == "chatgpt" {
		slog.Debug("codex uses OAuth auth — set OPENAI_API_KEY env var for quota checking")
	}

	return ""
}

// resolveAnthropicKey checks common locations for an Anthropic API key.
func resolveAnthropicKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// check ~/.anthropic/api_key (some setups store it here)
	data, err := os.ReadFile(filepath.Join(home, ".anthropic", "api_key"))
	if err == nil {
		key := trimKey(data)
		if key != "" {
			return key
		}
	}

	return ""
}

// resolveDeepSeekKey checks common locations for a DeepSeek API key.
func resolveDeepSeekKey() string {
	// no known config file location — env var only
	return ""
}

// trimKey removes whitespace and newlines from a key file.
func trimKey(data []byte) string {
	return strings.TrimSpace(string(data))
}
