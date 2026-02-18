package scan

import "strings"

// Severity represents the importance level of a finding.
type Severity int

const (
	SeverityCritical Severity = iota + 1
	SeverityWarning
	SeverityInfo
)

func (s Severity) String() string {
	switch s {
	case SeverityCritical:
		return "critical"
	case SeverityWarning:
		return "warning"
	case SeverityInfo:
		return "info"
	default:
		return "unknown"
	}
}

// ParseSeverity converts a string to Severity. Returns 0 if unrecognized.
func ParseSeverity(s string) Severity {
	switch strings.ToLower(s) {
	case "critical":
		return SeverityCritical
	case "warning":
		return SeverityWarning
	case "info":
		return SeverityInfo
	default:
		return 0
	}
}

// Finding represents a single actionable issue found in a repo.
type Finding struct {
	Repo       string   `json:"repo"`
	Check      string   `json:"check"`
	Category   string   `json:"category"`
	Severity   Severity `json:"severity"`
	Message    string   `json:"message"`
	Suggestion string   `json:"suggestion"`
	Prompt     string   `json:"prompt,omitempty"` // detailed prompt for autonomous agent execution
}

// TaskPrompt returns the detailed prompt for task generation.
// Falls back to Suggestion if no detailed Prompt is set.
func (f *Finding) TaskPrompt() string {
	if f.Prompt != "" {
		return f.Prompt
	}
	return f.Suggestion
}
