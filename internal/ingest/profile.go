package ingest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProfileConfig holds a generated chainwatch profile derived from WO constraints.
type ProfileConfig struct {
	Name                string              `yaml:"name"`
	Description         string              `yaml:"description"`
	ExecutionBoundaries ExecutionBoundaries `yaml:"execution_boundaries"`
	Policy              *PolicyOverrides    `yaml:"policy,omitempty"`
}

// ExecutionBoundaries are merged into the chainwatch denylist.
type ExecutionBoundaries struct {
	Files    []string `yaml:"files,omitempty"`
	Commands []string `yaml:"commands,omitempty"`
}

// PolicyOverrides prepend rules to the config (first-match-wins).
type PolicyOverrides struct {
	Rules []PolicyRule `yaml:"rules"`
}

// PolicyRule is a single policy rule in the profile.
type PolicyRule struct {
	Purpose         string `yaml:"purpose"`
	ResourcePattern string `yaml:"resource_pattern"`
	Decision        string `yaml:"decision"`
	Reason          string `yaml:"reason"`
}

// networkDenyCommands are blocked when Network constraint is false.
var networkDenyCommands = []string{
	"curl", "wget", "nc", "ncat", "ssh", "scp", "rsync",
}

// sudoDenyCommands are blocked when Sudo constraint is false.
var sudoDenyCommands = []string{
	"sudo", "su", "doas", "pkexec",
}

// BuildProfile maps WO constraints to a chainwatch profile.
func BuildProfile(c IngestConstraints, woID string) *ProfileConfig {
	cfg := &ProfileConfig{
		Name:        "wo-" + woID,
		Description: fmt.Sprintf("Ephemeral profile for WO %s", woID),
	}

	// DenyPaths → execution_boundaries.files
	cfg.ExecutionBoundaries.Files = append(cfg.ExecutionBoundaries.Files, c.DenyPaths...)

	// Network: false → deny outbound commands
	if !c.Network {
		cfg.ExecutionBoundaries.Commands = append(cfg.ExecutionBoundaries.Commands, networkDenyCommands...)
	}

	// Sudo: false → deny privilege escalation
	if !c.Sudo {
		cfg.ExecutionBoundaries.Commands = append(cfg.ExecutionBoundaries.Commands, sudoDenyCommands...)
	}

	// AllowPaths → policy rules
	if len(c.AllowPaths) > 0 {
		var rules []PolicyRule
		for _, path := range c.AllowPaths {
			rules = append(rules, PolicyRule{
				Purpose:         "*",
				ResourcePattern: path + "*",
				Decision:        "allow",
				Reason:          fmt.Sprintf("WO %s scope", woID),
			})
		}
		cfg.Policy = &PolicyOverrides{Rules: rules}
	}

	return cfg
}

// WriteProfile writes the profile to dir/wo-{id}.yaml and returns the path.
func WriteProfile(cfg *ProfileConfig, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("create profile dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal profile: %w", err)
	}

	path := filepath.Join(dir, cfg.Name+".yaml")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return "", fmt.Errorf("write profile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("rename profile: %w", err)
	}

	return path, nil
}
