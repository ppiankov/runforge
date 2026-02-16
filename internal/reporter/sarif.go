package reporter

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/ppiankov/runforge/internal/task"
)

const (
	sarifSchema  = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json"
	sarifVersion = "2.1.0"
)

type sarifReport struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name string `json:"name"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

// WriteSARIFReport writes a SARIF v2.1.0 report for failed/skipped/rate-limited tasks.
func WriteSARIFReport(report *task.RunReport, graph *task.Graph, path string) error {
	var results []sarifResult

	// sort task IDs for deterministic output
	ids := make([]string, 0, len(report.Results))
	for id := range report.Results {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		r := report.Results[id]
		var level string
		switch r.State {
		case task.StateFailed:
			level = "error"
		case task.StateSkipped:
			level = "warning"
		case task.StateRateLimited:
			level = "warning"
		default:
			continue // only include non-success states
		}

		msg := r.Error
		if msg == "" {
			msg = r.State.String()
		}

		sr := sarifResult{
			RuleID:  id,
			Level:   level,
			Message: sarifMessage{Text: msg},
		}

		// add repo as artifact location if available
		t := graph.Task(id)
		if t != nil && t.Repo != "" {
			sr.Locations = []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: t.Repo},
				},
			}}
		}

		results = append(results, sr)
	}

	sarif := sarifReport{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{{
			Tool: sarifTool{
				Driver: sarifDriver{Name: "runforge"},
			},
			Results: results,
		}},
	}

	data, err := json.MarshalIndent(sarif, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sarif: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}

	return nil
}
