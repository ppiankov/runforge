package reporter

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ppiankov/runforge/internal/task"
)

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
