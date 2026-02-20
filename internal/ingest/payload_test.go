package ingest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testPayload() *IngestPayload {
	return &IngestPayload{
		Version:    "1",
		WOID:       "wo-a1b2c3d4",
		IncidentID: "job-001",
		CreatedAt:  time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC),
		ApprovedAt: time.Date(2026, 2, 20, 12, 5, 0, 0, time.UTC),
		Target: IngestTarget{
			Host:  "web-01.example.com",
			Scope: "/var/www/site",
		},
		Observations: []IngestObservation{
			{Type: "suspicious_code", Severity: "high", Detail: "eval/base64_decode found"},
			{Type: "file_hash_mismatch", Severity: "medium", Detail: "wp-login.php modified"},
		},
		Constraints: IngestConstraints{
			AllowPaths: []string{"/var/www/site"},
			DenyPaths:  []string{"/etc", "/root"},
			Network:    false,
			Sudo:       false,
			MaxSteps:   10,
		},
		ProposedGoals: []string{
			"Investigate and remediate: eval/base64_decode found",
			"Investigate and remediate: wp-login.php modified",
		},
	}
}

func TestLoad(t *testing.T) {
	p := testPayload()
	data, _ := json.MarshalIndent(p, "", "  ")

	dir := t.TempDir()
	path := filepath.Join(dir, "wo-test.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.WOID != p.WOID {
		t.Errorf("WOID = %q, want %q", loaded.WOID, p.WOID)
	}
	if loaded.Target.Host != p.Target.Host {
		t.Errorf("Target.Host = %q, want %q", loaded.Target.Host, p.Target.Host)
	}
	if len(loaded.Observations) != len(p.Observations) {
		t.Errorf("Observations count = %d, want %d", len(loaded.Observations), len(p.Observations))
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(path, []byte("{invalid"), 0600)

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadNonexistent(t *testing.T) {
	_, err := Load("/nonexistent/path.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*IngestPayload)
		wantErr string
	}{
		{"valid", func(p *IngestPayload) {}, ""},
		{"missing wo_id", func(p *IngestPayload) { p.WOID = "" }, "wo_id is required"},
		{"missing incident_id", func(p *IngestPayload) { p.IncidentID = "" }, "incident_id is required"},
		{"missing host", func(p *IngestPayload) { p.Target.Host = "" }, "target host is required"},
		{"missing scope", func(p *IngestPayload) { p.Target.Scope = "" }, "target scope is required"},
		{"no observations", func(p *IngestPayload) { p.Observations = nil }, "at least one observation is required"},
		{"no goals", func(p *IngestPayload) { p.ProposedGoals = nil }, "at least one proposed goal is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testPayload()
			tt.modify(p)
			err := Validate(p)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q", tt.wantErr)
				} else if err.Error() != tt.wantErr {
					t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestLoadValidationFailure(t *testing.T) {
	p := testPayload()
	p.WOID = "" // missing required field
	data, _ := json.MarshalIndent(p, "", "  ")

	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.json")
	_ = os.WriteFile(path, data, 0600)

	_, err := Load(path)
	if err == nil {
		t.Error("expected validation error")
	}
}
