package ingest

import (
	"strings"
	"testing"
)

func TestBuildPromptContainsSections(t *testing.T) {
	p := testPayload()
	prompt := BuildPrompt(p)

	sections := []string{
		"## Observations",
		"## Goals",
		"## Constraints",
		"## Instructions",
	}
	for _, s := range sections {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing section %q", s)
		}
	}
}

func TestBuildPromptContainsHeader(t *testing.T) {
	p := testPayload()
	prompt := BuildPrompt(p)

	if !strings.Contains(prompt, p.WOID) {
		t.Error("prompt should contain WO ID")
	}
	if !strings.Contains(prompt, p.IncidentID) {
		t.Error("prompt should contain incident ID")
	}
	if !strings.Contains(prompt, p.Target.Host) {
		t.Error("prompt should contain host")
	}
	if !strings.Contains(prompt, p.Target.Scope) {
		t.Error("prompt should contain scope")
	}
}

func TestBuildPromptSeverityOrder(t *testing.T) {
	p := &IngestPayload{
		Version:    "1",
		WOID:       "wo-sort",
		IncidentID: "job-sort",
		Target:     IngestTarget{Host: "h", Scope: "/s"},
		Observations: []IngestObservation{
			{Type: "low_thing", Severity: "low", Detail: "low detail"},
			{Type: "crit_thing", Severity: "critical", Detail: "critical detail"},
			{Type: "med_thing", Severity: "medium", Detail: "medium detail"},
			{Type: "high_thing", Severity: "high", Detail: "high detail"},
		},
		Constraints:   IngestConstraints{MaxSteps: 5, Network: true, Sudo: true},
		ProposedGoals: []string{"Fix it"},
	}
	prompt := BuildPrompt(p)

	critIdx := strings.Index(prompt, "[CRITICAL]")
	highIdx := strings.Index(prompt, "[HIGH]")
	medIdx := strings.Index(prompt, "[MEDIUM]")
	lowIdx := strings.Index(prompt, "[LOW]")

	if critIdx == -1 || highIdx == -1 || medIdx == -1 || lowIdx == -1 {
		t.Fatal("missing severity labels in prompt")
	}
	if critIdx > highIdx {
		t.Error("CRITICAL should come before HIGH")
	}
	if highIdx > medIdx {
		t.Error("HIGH should come before MEDIUM")
	}
	if medIdx > lowIdx {
		t.Error("MEDIUM should come before LOW")
	}
}

func TestBuildPromptConstraintsFormatting(t *testing.T) {
	p := testPayload()
	p.Constraints.Network = false
	p.Constraints.Sudo = false
	prompt := BuildPrompt(p)

	if !strings.Contains(prompt, "Network access: no") {
		t.Error("should show 'Network access: no'")
	}
	if !strings.Contains(prompt, "Sudo access: no") {
		t.Error("should show 'Sudo access: no'")
	}

	p.Constraints.Network = true
	p.Constraints.Sudo = true
	prompt = BuildPrompt(p)

	if !strings.Contains(prompt, "Network access: yes") {
		t.Error("should show 'Network access: yes'")
	}
	if !strings.Contains(prompt, "Sudo access: yes") {
		t.Error("should show 'Sudo access: yes'")
	}
}

func TestBuildPromptMaxSteps(t *testing.T) {
	p := testPayload()
	p.Constraints.MaxSteps = 15
	prompt := BuildPrompt(p)

	if !strings.Contains(prompt, "Maximum steps: 15") {
		t.Error("should show max steps")
	}
	if !strings.Contains(prompt, "Do not exceed 15 commands") {
		t.Error("instructions should mention max steps")
	}
}

func TestBuildPromptGoals(t *testing.T) {
	p := testPayload()
	prompt := BuildPrompt(p)

	for _, goal := range p.ProposedGoals {
		if !strings.Contains(prompt, goal) {
			t.Errorf("prompt should contain goal %q", goal)
		}
	}
}

func TestBuildPromptAllowDenyPaths(t *testing.T) {
	p := testPayload()
	prompt := BuildPrompt(p)

	if !strings.Contains(prompt, "/var/www/site") {
		t.Error("should contain allowed path")
	}
	if !strings.Contains(prompt, "/etc") {
		t.Error("should contain denied path")
	}
}
