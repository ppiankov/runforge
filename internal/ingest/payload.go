// Package ingest handles work order ingestion from nullbot.
// It loads IngestPayload files, maps WO constraints to chainwatch profiles,
// and builds cloud agent prompts.
package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// IngestPayload is the handoff from nullbot approve to runforge ingest.
// Same JSON schema as chainwatch/internal/ingest — independent Go types.
type IngestPayload struct {
	Version       string              `json:"version"`
	WOID          string              `json:"wo_id"`
	IncidentID    string              `json:"incident_id"`
	CreatedAt     time.Time           `json:"created_at"`
	ApprovedAt    time.Time           `json:"approved_at"`
	Target        IngestTarget        `json:"target"`
	Observations  []IngestObservation `json:"observations"`
	Constraints   IngestConstraints   `json:"constraints"`
	ProposedGoals []string            `json:"proposed_goals"`
}

// IngestTarget identifies the system under remediation.
type IngestTarget struct {
	Host  string `json:"host"`
	Scope string `json:"scope"`
}

// IngestObservation is a finding stripped of raw evidence data.
type IngestObservation struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Detail   string `json:"detail"`
}

// IngestConstraints define what the remediation agent is allowed to do.
type IngestConstraints struct {
	AllowPaths []string `json:"allow_paths"`
	DenyPaths  []string `json:"deny_paths"`
	Network    bool     `json:"network"`
	Sudo       bool     `json:"sudo"`
	MaxSteps   int      `json:"max_steps"`
}

// Load reads and validates an IngestPayload from a JSON file.
func Load(path string) (*IngestPayload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}

	var p IngestPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse payload: %w", err)
	}

	if err := Validate(&p); err != nil {
		return nil, fmt.Errorf("validate payload: %w", err)
	}

	return &p, nil
}

// Validate checks that a payload has all required fields.
func Validate(p *IngestPayload) error {
	if p.WOID == "" {
		return fmt.Errorf("wo_id is required")
	}
	if p.IncidentID == "" {
		return fmt.Errorf("incident_id is required")
	}
	if p.Target.Host == "" {
		return fmt.Errorf("target host is required")
	}
	if p.Target.Scope == "" {
		return fmt.Errorf("target scope is required")
	}
	if len(p.Observations) == 0 {
		return fmt.Errorf("at least one observation is required")
	}
	if len(p.ProposedGoals) == 0 {
		return fmt.Errorf("at least one proposed goal is required")
	}
	return nil
}
