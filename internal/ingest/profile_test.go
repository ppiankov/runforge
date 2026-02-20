package ingest

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildProfileDenyPaths(t *testing.T) {
	c := IngestConstraints{
		DenyPaths: []string{"/etc", "/root"},
		Network:   true,
		Sudo:      true,
	}
	cfg := BuildProfile(c, "wo-test")

	if len(cfg.ExecutionBoundaries.Files) != 2 {
		t.Fatalf("Files = %d, want 2", len(cfg.ExecutionBoundaries.Files))
	}
	if cfg.ExecutionBoundaries.Files[0] != "/etc" {
		t.Errorf("Files[0] = %q, want /etc", cfg.ExecutionBoundaries.Files[0])
	}
	if cfg.ExecutionBoundaries.Files[1] != "/root" {
		t.Errorf("Files[1] = %q, want /root", cfg.ExecutionBoundaries.Files[1])
	}
}

func TestBuildProfileNoNetwork(t *testing.T) {
	c := IngestConstraints{
		Network: false,
		Sudo:    true,
	}
	cfg := BuildProfile(c, "wo-test")

	for _, cmd := range networkDenyCommands {
		found := false
		for _, denied := range cfg.ExecutionBoundaries.Commands {
			if denied == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in denied commands", cmd)
		}
	}
}

func TestBuildProfileNoSudo(t *testing.T) {
	c := IngestConstraints{
		Network: true,
		Sudo:    false,
	}
	cfg := BuildProfile(c, "wo-test")

	for _, cmd := range sudoDenyCommands {
		found := false
		for _, denied := range cfg.ExecutionBoundaries.Commands {
			if denied == cmd {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in denied commands", cmd)
		}
	}
}

func TestBuildProfileNetworkAndSudoAllowed(t *testing.T) {
	c := IngestConstraints{
		Network: true,
		Sudo:    true,
	}
	cfg := BuildProfile(c, "wo-test")

	if len(cfg.ExecutionBoundaries.Commands) != 0 {
		t.Errorf("Commands should be empty when network and sudo allowed, got %v", cfg.ExecutionBoundaries.Commands)
	}
}

func TestBuildProfileAllowPaths(t *testing.T) {
	c := IngestConstraints{
		AllowPaths: []string{"/var/www/site", "/tmp/scratch"},
		Network:    true,
		Sudo:       true,
	}
	cfg := BuildProfile(c, "wo-test")

	if cfg.Policy == nil {
		t.Fatal("Policy should not be nil with AllowPaths")
	}
	if len(cfg.Policy.Rules) != 2 {
		t.Fatalf("Rules = %d, want 2", len(cfg.Policy.Rules))
	}
	if cfg.Policy.Rules[0].ResourcePattern != "/var/www/site*" {
		t.Errorf("ResourcePattern = %q", cfg.Policy.Rules[0].ResourcePattern)
	}
	if cfg.Policy.Rules[0].Decision != "allow" {
		t.Errorf("Decision = %q, want allow", cfg.Policy.Rules[0].Decision)
	}
}

func TestBuildProfileNoAllowPaths(t *testing.T) {
	c := IngestConstraints{
		Network: true,
		Sudo:    true,
	}
	cfg := BuildProfile(c, "wo-test")

	if cfg.Policy != nil {
		t.Error("Policy should be nil without AllowPaths")
	}
}

func TestBuildProfileName(t *testing.T) {
	cfg := BuildProfile(IngestConstraints{Network: true, Sudo: true}, "a1b2c3d4")
	if cfg.Name != "wo-a1b2c3d4" {
		t.Errorf("Name = %q, want wo-a1b2c3d4", cfg.Name)
	}
	if !strings.Contains(cfg.Description, "a1b2c3d4") {
		t.Errorf("Description should mention WO ID")
	}
}

func TestWriteProfile(t *testing.T) {
	c := IngestConstraints{
		DenyPaths:  []string{"/etc"},
		AllowPaths: []string{"/var/www"},
		Network:    false,
		Sudo:       false,
	}
	cfg := BuildProfile(c, "wo-writetest")

	dir := t.TempDir()
	path, err := WriteProfile(cfg, dir)
	if err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}

	// Read back and verify it's valid YAML.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}

	var loaded ProfileConfig
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal profile: %v", err)
	}

	if loaded.Name != cfg.Name {
		t.Errorf("Name = %q, want %q", loaded.Name, cfg.Name)
	}
	if len(loaded.ExecutionBoundaries.Files) != 1 || loaded.ExecutionBoundaries.Files[0] != "/etc" {
		t.Errorf("Files = %v", loaded.ExecutionBoundaries.Files)
	}
	if loaded.Policy == nil || len(loaded.Policy.Rules) != 1 {
		t.Error("expected 1 policy rule")
	}
}

func TestWriteProfileNoTmpLeftover(t *testing.T) {
	cfg := BuildProfile(IngestConstraints{Network: true, Sudo: true}, "wo-notmp")

	dir := t.TempDir()
	if _, err := WriteProfile(cfg, dir); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}
