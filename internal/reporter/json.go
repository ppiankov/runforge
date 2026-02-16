package reporter

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ppiankov/runforge/internal/task"
)

// ReadJSONReport reads a run report from the given path.
func ReadJSONReport(path string) (*task.RunReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}

	var report task.RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("unmarshal report: %w", err)
	}

	return &report, nil
}

// WriteJSONReport writes the run report as JSON to the given path.
func WriteJSONReport(report *task.RunReport, path string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	return nil
}
