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
	Workers    int           `yaml:"workers"`
	ReposDir   string        `yaml:"repos_dir"`
	MaxRuntime time.Duration `yaml:"max_runtime"`
	FailFast   bool          `yaml:"fail_fast"`
	Verify     bool          `yaml:"verify"`
	PostRun    string        `yaml:"post_run"` // shell command to run after report is written; $RUNFORGE_RUN_DIR is set
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
