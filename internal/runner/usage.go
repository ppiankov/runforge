package runner

import "github.com/ppiankov/tokencontrol/internal/task"

// addUsage accumulates token counts into a running total.
// Returns nil if both total and the new values are zero.
func addUsage(total *task.TokenUsage, input, output, tokens int) *task.TokenUsage {
	if input == 0 && output == 0 && tokens == 0 {
		return total
	}
	if total == nil {
		total = &task.TokenUsage{}
	}
	total.InputTokens += input
	total.OutputTokens += output
	total.TotalTokens += tokens
	return total
}
