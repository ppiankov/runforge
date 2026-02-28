package runner

import (
	"context"
	"testing"

	"github.com/ppiankov/runforge/internal/task"
)

// mockValidatingRunner implements both Runner and ModelValidator.
type mockValidatingRunner struct {
	name       string
	resolveMap map[string]string // input model → resolved model
	errModel   string            // model that triggers error
}

func (r *mockValidatingRunner) Name() string { return r.name }

func (r *mockValidatingRunner) Run(_ context.Context, _ *task.Task, _, _ string) *task.TaskResult {
	return nil
}

func (r *mockValidatingRunner) ValidateModel(model string) (string, error) {
	if model == r.errModel {
		return "", &modelNotFoundError{model: model}
	}
	if resolved, ok := r.resolveMap[model]; ok {
		return resolved, nil
	}
	return model, nil
}

type modelNotFoundError struct {
	model string
}

func (e *modelNotFoundError) Error() string {
	return "model not found: " + e.model
}

// mockPlainRunner implements Runner but NOT ModelValidator.
type mockPlainRunner struct {
	name string
}

func (r *mockPlainRunner) Name() string { return r.name }

func (r *mockPlainRunner) Run(_ context.Context, _ *task.Task, _, _ string) *task.TaskResult {
	return nil
}

func TestValidateModels_NoValidators(t *testing.T) {
	runners := map[string]Runner{
		"plain": &mockPlainRunner{name: "plain"},
	}
	profiles := map[string]*task.RunnerProfileConfig{
		"plain": {Type: "codex", Model: "some-model"},
	}

	resolutions, warnings := ValidateModels(runners, profiles)
	if len(resolutions) != 0 {
		t.Errorf("expected 0 resolutions, got %d", len(resolutions))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(warnings))
	}
	// profile should be unchanged
	if profiles["plain"].Model != "some-model" {
		t.Errorf("profile model mutated to %q", profiles["plain"].Model)
	}
}

func TestValidateModels_ExactMatch(t *testing.T) {
	runners := map[string]Runner{
		"oc": &mockValidatingRunner{
			name:       "opencode",
			resolveMap: map[string]string{},
		},
	}
	profiles := map[string]*task.RunnerProfileConfig{
		"oc": {Type: "opencode", Model: "provider/valid-model"},
	}

	resolutions, warnings := ValidateModels(runners, profiles)
	if len(resolutions) != 0 {
		t.Errorf("expected 0 resolutions for exact match, got %d", len(resolutions))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(warnings))
	}
}

func TestValidateModels_AutoResolves(t *testing.T) {
	runners := map[string]Runner{
		"oc": &mockValidatingRunner{
			name: "opencode",
			resolveMap: map[string]string{
				"provider/bad-model": "provider/good-model",
			},
		},
	}
	profiles := map[string]*task.RunnerProfileConfig{
		"oc": {Type: "opencode", Model: "provider/bad-model"},
	}

	resolutions, warnings := ValidateModels(runners, profiles)
	if len(resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(resolutions))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(warnings))
	}

	res := resolutions[0]
	if res.RunnerProfile != "oc" {
		t.Errorf("runner profile = %q, want %q", res.RunnerProfile, "oc")
	}
	if res.Original != "provider/bad-model" {
		t.Errorf("original = %q, want %q", res.Original, "provider/bad-model")
	}
	if res.Resolved != "provider/good-model" {
		t.Errorf("resolved = %q, want %q", res.Resolved, "provider/good-model")
	}

	// profile should be mutated
	if profiles["oc"].Model != "provider/good-model" {
		t.Errorf("profile not mutated: model = %q", profiles["oc"].Model)
	}
}

func TestValidateModels_ValidationError(t *testing.T) {
	runners := map[string]Runner{
		"oc": &mockValidatingRunner{
			name:     "opencode",
			errModel: "provider/broken-model",
		},
	}
	profiles := map[string]*task.RunnerProfileConfig{
		"oc": {Type: "opencode", Model: "provider/broken-model"},
	}

	resolutions, warnings := ValidateModels(runners, profiles)
	if len(resolutions) != 0 {
		t.Errorf("expected 0 resolutions on error, got %d", len(resolutions))
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	if warnings[0] != "oc" {
		t.Errorf("warning runner = %q, want %q", warnings[0], "oc")
	}
	// profile should NOT be mutated on error
	if profiles["oc"].Model != "provider/broken-model" {
		t.Errorf("profile unexpectedly mutated to %q", profiles["oc"].Model)
	}
}

func TestValidateModels_EmptyModel_Skipped(t *testing.T) {
	runners := map[string]Runner{
		"oc": &mockValidatingRunner{
			name:     "opencode",
			errModel: "anything", // would error if called
		},
	}
	profiles := map[string]*task.RunnerProfileConfig{
		"oc": {Type: "opencode", Model: ""},
	}

	resolutions, warnings := ValidateModels(runners, profiles)
	if len(resolutions) != 0 {
		t.Errorf("expected 0 resolutions, got %d", len(resolutions))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(warnings))
	}
}

func TestValidateModels_RunnerNotInRegistry(t *testing.T) {
	runners := map[string]Runner{} // empty
	profiles := map[string]*task.RunnerProfileConfig{
		"oc": {Type: "opencode", Model: "provider/model"},
	}

	resolutions, warnings := ValidateModels(runners, profiles)
	if len(resolutions) != 0 {
		t.Errorf("expected 0 resolutions, got %d", len(resolutions))
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d", len(warnings))
	}
}
