package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// opencodeConfig represents the relevant parts of ~/.config/opencode/opencode.json.
type opencodeConfig struct {
	Model    string                             `json:"model"`
	Provider map[string]*opencodeProviderConfig `json:"provider"`
}

// opencodeProviderConfig describes a single provider in the OpenCode config.
type opencodeProviderConfig struct {
	Name   string                         `json:"name"`
	Models map[string]*opencodeModelEntry `json:"models"`
}

// opencodeModelEntry describes a single model within a provider.
type opencodeModelEntry struct {
	Name string `json:"name"`
}

// loadOpencodeConfig reads and parses the OpenCode configuration file.
// Returns nil, nil if the file does not exist (no config = no validation).
func loadOpencodeConfig() (*opencodeConfig, error) {
	path := opencodeConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read opencode config: %w", err)
	}

	var cfg opencodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse opencode config %s: %w", path, err)
	}
	return &cfg, nil
}

// opencodeConfigPath returns the path to the OpenCode configuration file,
// respecting $XDG_CONFIG_HOME.
func opencodeConfigPath() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "opencode", "opencode.json")
}

// hasModel checks if the exact model ID (format "provider/model") exists.
func (c *opencodeConfig) hasModel(model string) bool {
	if c == nil {
		return false
	}
	provName, modelID, ok := splitModel(model)
	if !ok {
		return false
	}
	provider, exists := c.Provider[provName]
	if !exists {
		return false
	}
	_, exists = provider.Models[modelID]
	return exists
}

// availableModels returns all model IDs in "provider/model" format, sorted.
func (c *opencodeConfig) availableModels() []string {
	if c == nil {
		return nil
	}
	var models []string
	for provName, provider := range c.Provider {
		for modelID := range provider.Models {
			models = append(models, provName+"/"+modelID)
		}
	}
	sort.Strings(models)
	return models
}

// defaultModel returns the configured default model.
func (c *opencodeConfig) defaultModel() string {
	if c == nil {
		return ""
	}
	return c.Model
}

// resolveOpencodeModel implements the auto-resolution algorithm:
// 1. Exact match → return unchanged
// 2. Same provider, wrong model → pick first alphabetical model from that provider
// 3. Unknown provider → fall back to config default
// 4. No default → return error
func resolveOpencodeModel(cfg *opencodeConfig, model string) (string, error) {
	if model == "" {
		return "", nil
	}
	if cfg == nil {
		return model, nil
	}

	// exact match
	if cfg.hasModel(model) {
		return model, nil
	}

	provName, _, ok := splitModel(model)
	if !ok {
		return "", fmt.Errorf("invalid model format %q (expected provider/model)", model)
	}

	// same provider, different model
	if provider, exists := cfg.Provider[provName]; exists && len(provider.Models) > 0 {
		models := make([]string, 0, len(provider.Models))
		for id := range provider.Models {
			models = append(models, id)
		}
		sort.Strings(models)
		resolved := provName + "/" + models[0]
		return resolved, nil
	}

	// provider not found — fall back to default
	def := cfg.defaultModel()
	if def != "" {
		return def, nil
	}

	return "", fmt.Errorf("model %q not found and no default model configured (available: %s)",
		model, strings.Join(cfg.availableModels(), ", "))
}

// splitModel splits a "provider/model" string into its parts.
func splitModel(model string) (provider, modelID string, ok bool) {
	idx := strings.Index(model, "/")
	if idx < 1 || idx >= len(model)-1 {
		return "", "", false
	}
	return model[:idx], model[idx+1:], true
}
