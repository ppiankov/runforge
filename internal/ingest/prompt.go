package ingest

import (
	"fmt"
	"sort"
	"strings"
)

// severityRank maps severity strings to sort order (higher = more urgent).
var severityRank = map[string]int{
	"critical": 4,
	"high":     3,
	"medium":   2,
	"low":      1,
}

// BuildPrompt assembles the cloud agent prompt from an IngestPayload.
// Observations are sorted by severity (critical first).
func BuildPrompt(p *IngestPayload) string {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "You are a remediation agent. Work order %s for incident %s on host %s, scope: %s.\n\n",
		p.WOID, p.IncidentID, p.Target.Host, p.Target.Scope)

	// Observations — sorted by severity descending
	b.WriteString("## Observations\n\n")
	sorted := make([]IngestObservation, len(p.Observations))
	copy(sorted, p.Observations)
	sort.Slice(sorted, func(i, j int) bool {
		return severityRank[sorted[i].Severity] > severityRank[sorted[j].Severity]
	})
	for _, obs := range sorted {
		fmt.Fprintf(&b, "- [%s] %s: %s\n", strings.ToUpper(obs.Severity), obs.Type, obs.Detail)
	}

	// Goals
	b.WriteString("\n## Goals\n\n")
	for i, goal := range p.ProposedGoals {
		fmt.Fprintf(&b, "%d. %s\n", i+1, goal)
	}

	// Constraints
	b.WriteString("\n## Constraints\n\n")
	if len(p.Constraints.AllowPaths) > 0 {
		fmt.Fprintf(&b, "- Allowed paths: %s\n", strings.Join(p.Constraints.AllowPaths, ", "))
	}
	if len(p.Constraints.DenyPaths) > 0 {
		fmt.Fprintf(&b, "- Denied paths: %s\n", strings.Join(p.Constraints.DenyPaths, ", "))
	}
	fmt.Fprintf(&b, "- Network access: %s\n", boolYesNo(p.Constraints.Network))
	fmt.Fprintf(&b, "- Sudo access: %s\n", boolYesNo(p.Constraints.Sudo))
	fmt.Fprintf(&b, "- Maximum steps: %d\n", p.Constraints.MaxSteps)

	// Instructions
	b.WriteString("\n## Instructions\n\n")
	b.WriteString("Analyze the observations and remediate within the constraints.\n")
	b.WriteString("All commands execute through chainwatch exec which enforces the constraints.\n")
	fmt.Fprintf(&b, "Do not exceed %d commands. Stay within allowed paths.\n", p.Constraints.MaxSteps)

	return b.String()
}

func boolYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
