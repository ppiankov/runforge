package runner

import "github.com/ppiankov/tokencontrol/internal/task"

// addUsage accumulates token counts into a running total.
// Returns nil if both total and the new values are zero.
// If total_tokens is 0 but input/output are set, computes total from input+output.
func addUsage(total *task.TokenUsage, input, output, tokens int) *task.TokenUsage {
	if input == 0 && output == 0 && tokens == 0 {
		return total
	}
	if total == nil {
		total = &task.TokenUsage{}
	}
	// compute total from input+output when not provided
	if tokens == 0 && (input > 0 || output > 0) {
		tokens = input + output
	}
	total.InputTokens += input
	total.OutputTokens += output
	total.TotalTokens += tokens
	return total
}
