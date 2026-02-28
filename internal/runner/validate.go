package runner

import (
	"fmt"
	"log/slog"
	"sort"

	"github.com/ppiankov/runforge/internal/task"
)

// ModelValidator is an optional interface that runners can implement to
// support pre-spawn model validation and auto-resolution.
type ModelValidator interface {
	// ValidateModel checks whether the given model identifier is valid
	// for this runner. Returns the resolved model (may differ from input
	// if auto-resolved) and any error. If model is empty, returns empty
	// string and nil (use runner default).
	ValidateModel(model string) (resolved string, err error)
}

// ModelResolution records a model that was auto-resolved during validation.
type ModelResolution struct {
	RunnerProfile string // profile name (e.g., "opencode", "pickle")
	Original      string // what the task file specified
	Resolved      string // what it was resolved to
	Reason        string // human-readable explanation
}

// ValidateModels checks all runner profiles against their model validators.
// It mutates profiles in-place (updating Model to the resolved value) and
// returns a list of resolutions applied and a list of runner profile names
// that should be skipped (validation failed entirely).
func ValidateModels(
	runners map[string]Runner,
	profiles map[string]*task.RunnerProfileConfig,
) ([]ModelResolution, []string) {
	var resolutions []ModelResolution
	var warnings []string

	// sort profile names for deterministic order
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		profile := profiles[name]
		if profile.Model == "" {
			continue
		}

		r, ok := runners[name]
		if !ok {
			continue
		}

		validator, ok := r.(ModelValidator)
		if !ok {
			continue // runner does not support validation
		}

		resolved, err := validator.ValidateModel(profile.Model)
		if err != nil {
			warnings = append(warnings, name)
			slog.Warn("model validation failed, runner will be skipped in cascade",
				"runner", name, "model", profile.Model, "error", err)
			continue
		}

		if resolved != profile.Model {
			reason := fmt.Sprintf("model %q not found, resolved to %q", profile.Model, resolved)
			resolutions = append(resolutions, ModelResolution{
				RunnerProfile: name,
				Original:      profile.Model,
				Resolved:      resolved,
				Reason:        reason,
			})
			slog.Info("model auto-resolved",
				"runner", name, "from", profile.Model, "to", resolved)
			profile.Model = resolved
		}
	}

	return resolutions, warnings
}
