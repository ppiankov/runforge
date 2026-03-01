package reporter

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorDim    = "\033[2m"
)

// ModelResolution describes a model that was auto-resolved during validation.
// Mirrors runner.ModelResolution to avoid a circular import.
type ModelResolution struct {
	RunnerProfile string
	Original      string
	Resolved      string
}

// TextReporter writes human-readable output to a writer.
type TextReporter struct {
	w     io.Writer
	color bool
}

// NewTextReporter creates a text reporter.
// If w is nil, defaults to os.Stdout.
// color enables ANSI codes.
func NewTextReporter(w io.Writer, color bool) *TextReporter {
	if w == nil {
		w = os.Stdout
	}
	return &TextReporter{w: w, color: color}
}

// PrintHeader writes the initial banner.
func (r *TextReporter) PrintHeader(totalTasks, workers int) {
	fmt.Fprintf(r.w, "runforge — %d tasks, %d workers\n\n", totalTasks, workers)
}

// PrintStatus writes a snapshot of all task states.
func (r *TextReporter) PrintStatus(graph *task.Graph, results map[string]*task.TaskResult) {
	var running, completed, failed, skipped, rateLimited, pending []*task.TaskResult

	for _, id := range graph.Order() {
		res := results[id]
		if res == nil {
			continue
		}
		switch res.State {
		case task.StateRunning:
			running = append(running, res)
		case task.StateCompleted:
			completed = append(completed, res)
		case task.StateFailed:
			failed = append(failed, res)
		case task.StateSkipped:
			skipped = append(skipped, res)
		case task.StateRateLimited:
			rateLimited = append(rateLimited, res)
		default:
			pending = append(pending, res)
		}
	}

	total := len(results)

	r.printSection("RUNNING", colorCyan, running, total, graph, func(res *task.TaskResult) string {
		elapsed := time.Since(res.StartedAt).Truncate(time.Second)
		t := graph.Task(res.TaskID)
		title := ""
		if t != nil {
			title = t.Title
		}
		return fmt.Sprintf("    %-25s %-35s %s", res.TaskID, title, elapsed)
	})

	r.printSection("COMPLETED", colorGreen, completed, total, graph, func(res *task.TaskResult) string {
		t := graph.Task(res.TaskID)
		title := ""
		if t != nil {
			title = t.Title
		}
		dur := res.Duration.Truncate(time.Second)
		via := runnerSuffix(res)
		return fmt.Sprintf("    %-25s %-35s %s  ✓%s", res.TaskID, title, dur, via)
	})

	r.printSection("FAILED", colorRed, failed, total, graph, func(res *task.TaskResult) string {
		t := graph.Task(res.TaskID)
		title := ""
		if t != nil {
			title = t.Title
		}
		dur := res.Duration.Truncate(time.Second)
		via := runnerSuffix(res)
		errDisplay := res.Error
		if res.ConnectivityError != "" {
			errDisplay = res.ConnectivityError
		}
		return fmt.Sprintf("    %-25s %-35s %s  ✗ %s%s", res.TaskID, title, dur, errDisplay, via)
	})

	if len(skipped) > 0 {
		fmt.Fprintf(r.w, "  %sSKIPPED  [%d/%d]%s\n", r.c(colorYellow), len(skipped), total, r.c(colorReset))
		for _, res := range skipped {
			fmt.Fprintf(r.w, "    %s%-25s%s  (%s)\n", r.c(colorDim), res.TaskID, r.c(colorReset), res.Error)
		}
		fmt.Fprintln(r.w)
	}

	if len(rateLimited) > 0 {
		fmt.Fprintf(r.w, "  %sRATE LIMITED  [%d/%d]%s\n", r.c(colorYellow), len(rateLimited), total, r.c(colorReset))
		for _, res := range rateLimited {
			info := "rate limit reached"
			if !res.ResetsAt.IsZero() {
				remaining := time.Until(res.ResetsAt).Truncate(time.Minute)
				if remaining > 0 {
					info = fmt.Sprintf("resets in %s", remaining)
				} else {
					info = fmt.Sprintf("resets at %s", res.ResetsAt.Format(time.Kitchen))
				}
			}
			fmt.Fprintf(r.w, "    %-25s  %s⏸ %s%s\n", res.TaskID, r.c(colorYellow), info, r.c(colorReset))
		}
		fmt.Fprintln(r.w)
	}

	if len(pending) > 0 {
		fmt.Fprintf(r.w, "  %sBLOCKED  [%d/%d]%s\n", r.c(colorDim), len(pending), total, r.c(colorReset))
		for _, res := range pending {
			t := graph.Task(res.TaskID)
			dep := ""
			if t != nil && len(t.DependsOn) > 0 {
				dep = fmt.Sprintf("  (waiting: %s)", strings.Join(t.DependsOn, ", "))
			}
			fmt.Fprintf(r.w, "    %s%-25s%s%s\n", r.c(colorDim), res.TaskID, dep, r.c(colorReset))
		}
		fmt.Fprintln(r.w)
	}
}

// PrintSummary writes the final summary line.
func (r *TextReporter) PrintSummary(report *task.RunReport) {
	fmt.Fprintf(r.w, "\n%s--- Summary ---%s\n", r.c(colorCyan), r.c(colorReset))
	fmt.Fprintf(r.w, "Total: %d  ", report.TotalTasks)
	fmt.Fprintf(r.w, "%sCompleted: %d%s  ", r.c(colorGreen), report.Completed, r.c(colorReset))
	fmt.Fprintf(r.w, "%sFailed: %d%s  ", r.c(colorRed), report.Failed, r.c(colorReset))
	fmt.Fprintf(r.w, "%sSkipped: %d%s  ", r.c(colorYellow), report.Skipped, r.c(colorReset))
	if report.RateLimited > 0 {
		fmt.Fprintf(r.w, "%sRate limited: %d%s  ", r.c(colorYellow), report.RateLimited, r.c(colorReset))
	}
	if report.FalsePositives > 0 {
		fmt.Fprintf(r.w, "%sFalse positive: %d%s  ", r.c(colorRed), report.FalsePositives, r.c(colorReset))
	}
	fallbackCount := countFallbacks(report)
	if fallbackCount > 0 {
		fmt.Fprintf(r.w, "%sFallback: %d%s  ", r.c(colorYellow), fallbackCount, r.c(colorReset))
	}
	reviewed, rPassed, rFailed := reviewStats(report)
	if reviewed > 0 {
		fmt.Fprintf(r.w, "Reviewed: %d (%s%d passed%s, %s%d failed%s)  ",
			reviewed,
			r.c(colorGreen), rPassed, r.c(colorReset),
			r.c(colorRed), rFailed, r.c(colorReset))
	}
	fmt.Fprintf(r.w, "Duration: %s", report.TotalDuration.Truncate(time.Second))
	if report.RateLimited > 0 && !report.ResetsAt.IsZero() {
		remaining := time.Until(report.ResetsAt).Truncate(time.Minute)
		if remaining > 0 {
			fmt.Fprintf(r.w, "  (quota resets in %s)", remaining)
		}
	}
	fmt.Fprintln(r.w)
}

// PrintModelResolutions writes model auto-resolution information.
func (r *TextReporter) PrintModelResolutions(resolutions []ModelResolution) {
	fmt.Fprint(r.w, "Model resolutions:\n")
	for _, res := range resolutions {
		fmt.Fprintf(r.w, "  %s[%s]%s %s → %s\n",
			r.c(colorYellow), res.RunnerProfile, r.c(colorReset),
			res.Original, res.Resolved)
	}
	fmt.Fprintln(r.w)
}

// SkippedInfo describes a task skipped due to persistent state.
type SkippedInfo struct {
	ID     string
	Reason string
}

// PrintSkippedByState writes the list of tasks skipped due to persistent state.
func (r *TextReporter) PrintSkippedByState(skipped []SkippedInfo) {
	fmt.Fprintf(r.w, "%sSkipped by state:%s\n", r.c(colorDim), r.c(colorReset))
	for _, s := range skipped {
		fmt.Fprintf(r.w, "  %s%-30s%s  %s\n", r.c(colorDim), s.ID, r.c(colorReset), s.Reason)
	}
	fmt.Fprintln(r.w)
}

// PrintSecretRepos writes repos where secrets were detected during pre-scan.
func (r *TextReporter) PrintSecretRepos(repos []string) {
	fmt.Fprintf(r.w, "%sRepos with secrets (unsafe runners excluded from fallbacks):%s\n", r.c(colorYellow), r.c(colorReset))
	for _, repo := range repos {
		fmt.Fprintf(r.w, "  %s%s%s\n", r.c(colorYellow), repo, r.c(colorReset))
	}
	fmt.Fprintln(r.w)
}

// PrintDryRun writes the execution plan without running anything.
func (r *TextReporter) PrintDryRun(graph *task.Graph, reposDir string) {
	fmt.Fprint(r.w, "Execution plan (dry-run):\n\n")

	for i, id := range graph.Order() {
		t := graph.Task(id)
		dep := ""
		if len(t.DependsOn) > 0 {
			dep = fmt.Sprintf(" (after %s)", strings.Join(t.DependsOn, ", "))
		}
		fmt.Fprintf(r.w, "  %d. [P%d] %s — %s%s\n", i+1, t.Priority, id, t.Title, dep)
		fmt.Fprintf(r.w, "     repo: %s\n", t.Repo)
		// truncate prompt to first 100 chars
		prompt := t.Prompt
		if len(prompt) > 100 {
			prompt = prompt[:100] + "..."
		}
		prompt = strings.ReplaceAll(prompt, "\n", " ")
		fmt.Fprintf(r.w, "     prompt: %s\n\n", prompt)
	}
}

func (r *TextReporter) printSection(label, color string, items []*task.TaskResult, total int, graph *task.Graph, formatter func(*task.TaskResult) string) {
	fmt.Fprintf(r.w, "  %s%s  [%d/%d]%s\n", r.c(color), label, len(items), total, r.c(colorReset))
	for _, res := range items {
		fmt.Fprintln(r.w, formatter(res))
	}
	fmt.Fprintln(r.w)
}

func (r *TextReporter) c(code string) string {
	if !r.color {
		return ""
	}
	return code
}

// runnerSuffix returns runner and review info for display.
// Completed on primary: " (codex)"
// Completed with fallback: " (via zai, reviewed ✓)"
// Failed after cascade: " (tried codex→zai→claude)"
func runnerSuffix(res *task.TaskResult) string {
	var parts []string
	if len(res.Attempts) > 1 {
		if res.State == task.StateCompleted && res.RunnerUsed != "" {
			parts = append(parts, fmt.Sprintf("via %s", res.RunnerUsed))
		} else {
			var tried []string
			for _, a := range res.Attempts {
				tried = append(tried, a.Runner)
			}
			parts = append(parts, fmt.Sprintf("tried %s", strings.Join(tried, "→")))
		}
	} else if res.RunnerUsed != "" {
		parts = append(parts, res.RunnerUsed)
	}
	if res.Review != nil {
		if res.Review.Passed {
			parts = append(parts, "reviewed ✓")
		} else {
			parts = append(parts, "review ✗")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// countFallbacks counts tasks that used a non-primary runner.
func countFallbacks(report *task.RunReport) int {
	n := 0
	for _, r := range report.Results {
		if len(r.Attempts) > 1 {
			n++
		}
	}
	return n
}

// reviewStats returns (total reviewed, passed, failed).
func reviewStats(report *task.RunReport) (int, int, int) {
	var total, passed, failed int
	for _, r := range report.Results {
		if r.Review != nil {
			total++
			if r.Review.Passed {
				passed++
			} else {
				failed++
			}
		}
	}
	return total, passed, failed
}
