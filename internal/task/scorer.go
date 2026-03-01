package task

import "strings"

// Difficulty labels for task scoring.
const (
	DifficultySimple  = "simple"
	DifficultyMedium  = "medium"
	DifficultyComplex = "complex"

	ThresholdMedium  = 7
	ThresholdComplex = 15
)

// keywordWeights maps complexity keywords to their score contributions.
// Scanned case-insensitively in task title and prompt.
var keywordWeights = []struct {
	keyword string
	weight  int
}{
	// high complexity (+3)
	{"refactor", 3},
	{"tui", 3},
	{"architecture", 3},
	{"redesign", 3},
	{"migration", 3},
	// medium complexity (+2)
	{"multi-file", 2},
	{"parallel", 2},
	{"concurrent", 2},
	{"dag", 2},
	{"scheduler", 2},
	// low complexity (+1)
	{"test", 1},
	{"config", 1},
	{"validation", 1},
	{"api", 1},
}

// ScoreTask computes a deterministic difficulty score from task metadata.
// score = criteriaCount + keyword_weight (from title + prompt).
func ScoreTask(title, prompt string, criteriaCount int) (score int, difficulty string) {
	score = criteriaCount

	text := strings.ToLower(title + " " + prompt)
	seen := make(map[string]bool)
	for _, kw := range keywordWeights {
		if !seen[kw.keyword] && strings.Contains(text, kw.keyword) {
			score += kw.weight
			seen[kw.keyword] = true
		}
	}

	switch {
	case score >= ThresholdComplex:
		difficulty = DifficultyComplex
	case score >= ThresholdMedium:
		difficulty = DifficultyMedium
	default:
		difficulty = DifficultySimple
	}
	return score, difficulty
}

// DefaultTier returns the default capability tier for a runner type.
// Tier 1 = complex-capable, Tier 2 = medium, Tier 3 = simple only.
func DefaultTier(runnerType string) int {
	switch runnerType {
	case "codex", "claude":
		return 1
	case "gemini":
		return 2
	default:
		return 3
	}
}

// MinTier returns the minimum runner tier required for a difficulty level.
// complex → 1 (only tier 1), medium → 2 (tier 1 or 2), simple → 3 (any).
func MinTier(difficulty string) int {
	switch difficulty {
	case DifficultyComplex:
		return 1
	case DifficultyMedium:
		return 2
	default:
		return 3
	}
}
