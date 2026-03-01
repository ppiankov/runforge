package task

import "testing"

func TestScoreTask_Simple(t *testing.T) {
	score, diff := ScoreTask("Update README", "Add badges and fix typos in README.md", 2)
	if diff != DifficultySimple {
		t.Errorf("expected simple, got %q (score %d)", diff, score)
	}
	if score > 6 {
		t.Errorf("simple task should score ≤6, got %d", score)
	}
}

func TestScoreTask_Medium(t *testing.T) {
	score, diff := ScoreTask("Add validation to config loader",
		"Implement config file validation with schema checks",
		8) // 8 acceptance criteria
	if diff != DifficultyMedium {
		t.Errorf("expected medium, got %q (score %d)", diff, score)
	}
	if score < ThresholdMedium || score >= ThresholdComplex {
		t.Errorf("medium task should score %d-%d, got %d", ThresholdMedium, ThresholdComplex-1, score)
	}
}

func TestScoreTask_Complex(t *testing.T) {
	score, diff := ScoreTask("Refactor TUI architecture",
		"Redesign the TUI scheduler with parallel DAG execution and migration to new framework",
		5) // 5 criteria + heavy keywords
	if diff != DifficultyComplex {
		t.Errorf("expected complex, got %q (score %d)", diff, score)
	}
	if score < ThresholdComplex {
		t.Errorf("complex task should score ≥%d, got %d", ThresholdComplex, score)
	}
}

func TestScoreTask_KeywordWeights(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		prompt string
		minAdd int // minimum keyword contribution
	}{
		{"refactor +3", "Refactor module", "", 3},
		{"tui +3", "Build TUI", "", 3},
		{"architecture +3", "Fix architecture", "", 3},
		{"redesign +3", "Redesign API", "", 3},
		{"migration +3", "Database migration", "", 3},
		{"multi-file +2", "Multi-file changes", "", 2},
		{"parallel +2", "Parallel execution", "", 2},
		{"concurrent +2", "Concurrent workers", "", 2},
		{"dag +2", "DAG scheduler", "", 2},
		{"scheduler +2", "Fix scheduler", "", 2},
		{"test +1", "Add test coverage", "", 1},
		{"config +1", "Update config", "", 1},
		{"validation +1", "Input validation", "", 1},
		{"api +1", "REST API endpoint", "", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, _ := ScoreTask(tt.title, tt.prompt, 0)
			if score < tt.minAdd {
				t.Errorf("expected score ≥%d for keyword %q, got %d", tt.minAdd, tt.title, score)
			}
		})
	}
}

func TestScoreTask_KeywordsDeduplicated(t *testing.T) {
	// "refactor" appears in both title and prompt — should only count once (+3)
	score, _ := ScoreTask("Refactor module", "refactor the entire codebase", 0)
	if score != 3 {
		t.Errorf("duplicate keyword should count once, expected 3, got %d", score)
	}
}

func TestScoreTask_ZeroCriteria(t *testing.T) {
	score, diff := ScoreTask("Simple task", "Do something simple", 0)
	if score != 0 {
		t.Errorf("no criteria and no keywords should give score 0, got %d", score)
	}
	if diff != DifficultySimple {
		t.Errorf("expected simple, got %q", diff)
	}
}

func TestDefaultTier(t *testing.T) {
	tests := []struct {
		runnerType string
		want       int
	}{
		{"codex", 1},
		{"claude", 1},
		{"gemini", 2},
		{"opencode", 3},
		{"cline", 3},
		{"qwen", 3},
		{"script", 3},
		{"unknown", 3},
	}

	for _, tt := range tests {
		t.Run(tt.runnerType, func(t *testing.T) {
			got := DefaultTier(tt.runnerType)
			if got != tt.want {
				t.Errorf("DefaultTier(%q) = %d, want %d", tt.runnerType, got, tt.want)
			}
		})
	}
}

func TestMinTier(t *testing.T) {
	tests := []struct {
		difficulty string
		want       int
	}{
		{DifficultyComplex, 1},
		{DifficultyMedium, 2},
		{DifficultySimple, 3},
		{"", 3},        // empty = no restriction
		{"unknown", 3}, // unknown = no restriction
	}

	for _, tt := range tests {
		t.Run(tt.difficulty, func(t *testing.T) {
			got := MinTier(tt.difficulty)
			if got != tt.want {
				t.Errorf("MinTier(%q) = %d, want %d", tt.difficulty, got, tt.want)
			}
		})
	}
}
