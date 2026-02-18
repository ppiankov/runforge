package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Settings holds persistent CLI defaults loaded from a config file.
type Settings struct {
	Workers     int           `yaml:"workers"`
	ReposDir    string        `yaml:"repos_dir"`
	MaxRuntime  time.Duration `yaml:"max_runtime"`
	IdleTimeout time.Duration `yaml:"idle_timeout"`
	FailFast    bool          `yaml:"fail_fast"`
	Verify      bool          `yaml:"verify"`
	PostRun     string        `yaml:"post_run"` // shell command to run after report is written; $RUNFORGE_RUN_DIR is set

	// Runner config injected into generated task files
	DefaultRunner    string                    `yaml:"default_runner"`
	DefaultFallbacks []string                  `yaml:"default_fallbacks"`
	Runners          map[string]*RunnerProfile `yaml:"runners"`

	// Repos where code must not be sent to data-collecting providers
	PrivateRepos []string `yaml:"private_repos,omitempty"`

	// Responses API → Chat Completions translation proxy
	Proxy *ProxyConfig `yaml:"proxy,omitempty"`

	// Conventions appended verbatim to every generated task prompt
	PromptConventions string `yaml:"prompt_conventions,omitempty"`

	// Scan configuration
	Scan *ScanConfig `yaml:"scan,omitempty"`
}

// ScanConfig holds settings for the scan command.
type ScanConfig struct {
	ExcludeRepos []string `yaml:"exclude_repos,omitempty"`
}

// RunnerProfile mirrors task.RunnerProfileConfig for YAML config.
type RunnerProfile struct {
	Type           string            `yaml:"type"`
	Model          string            `yaml:"model,omitempty"`
	Profile        string            `yaml:"profile,omitempty"`
	Env            map[string]string `yaml:"env,omitempty"`
	MaxConcurrent  int               `yaml:"max_concurrent,omitempty"`
	DataCollection bool              `yaml:"data_collection,omitempty"` // true = provider may use data for training
}

// ProxyConfig controls the built-in Responses API → Chat Completions proxy.
type ProxyConfig struct {
	Enabled bool                    `yaml:"enabled"`
	Listen  string                  `yaml:"listen,omitempty"` // default ":4000"
	Targets map[string]*ProxyTarget `yaml:"targets"`
}

// ProxyTarget describes an upstream Chat Completions endpoint.
type ProxyTarget struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key,omitempty"` // literal or "env:VAR_NAME"
}

// LoadSettings reads a YAML config file into Settings.
// If the file does not exist, it returns zero-value Settings and nil error.
func LoadSettings(path string) (*Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Settings{}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var s Settings
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	return &s, nil
}
