package reporter

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const maxTaskLines = 20

// LiveReporter provides a live-updating terminal display during task execution.
type LiveReporter struct {
	w          io.Writer
	color      bool
	graph      *task.Graph
	getResults func() map[string]*task.TaskResult
	stop       chan struct{}
	done       chan struct{}
	lastLines  int
	frame      int
	mu         sync.Mutex
}

// NewLiveReporter creates a live reporter that polls results via getResults.
func NewLiveReporter(w io.Writer, color bool, graph *task.Graph, getResults func() map[string]*task.TaskResult) *LiveReporter {
	return &LiveReporter{
		w:          w,
		color:      color,
		graph:      graph,
		getResults: getResults,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// Start begins the periodic refresh loop.
func (lr *LiveReporter) Start() {
	go lr.loop()
}

// Stop halts the refresh loop and clears the live display.
func (lr *LiveReporter) Stop() {
	close(lr.stop)
	<-lr.done
	lr.clearLastFrame()
}

func (lr *LiveReporter) loop() {
	defer close(lr.done)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-lr.stop:
			return
		case <-ticker.C:
			lr.render()
		}
	}
}

func (lr *LiveReporter) clearLastFrame() {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	if lr.lastLines > 0 {
		fmt.Fprintf(lr.w, "\033[%dA", lr.lastLines)
		for i := 0; i < lr.lastLines; i++ {
			fmt.Fprintf(lr.w, "\033[K\n")
		}
		fmt.Fprintf(lr.w, "\033[%dA", lr.lastLines)
	}
}

func (lr *LiveReporter) render() {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	results := lr.getResults()
	lines := lr.buildLines(results)

	// move cursor up to overwrite previous frame
	if lr.lastLines > 0 {
		fmt.Fprintf(lr.w, "\033[%dA", lr.lastLines)
	}

	for _, line := range lines {
		fmt.Fprintf(lr.w, "\033[K%s\n", line)
	}

	lr.lastLines = len(lines)
	lr.frame++
}

// Render produces the display lines for a given results snapshot.
// Exported for testing.
func (lr *LiveReporter) Render(results map[string]*task.TaskResult) []string {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	return lr.buildLines(results)
}

func (lr *LiveReporter) buildLines(results map[string]*task.TaskResult) []string {
	var failed, running, completed, rateLimited, queued []*task.TaskResult

	for _, id := range lr.graph.Order() {
		res := results[id]
		if res == nil {
			continue
		}
		switch res.State {
		case task.StateFailed:
			failed = append(failed, res)
		case task.StateRunning:
			running = append(running, res)
		case task.StateCompleted:
			completed = append(completed, res)
		case task.StateSkipped:
			failed = append(failed, res) // show skipped with failed
		case task.StateRateLimited:
			rateLimited = append(rateLimited, res)
		default:
			queued = append(queued, res)
		}
	}

	// sort completed by end time (most recent first)
	sort.Slice(completed, func(i, j int) bool {
		return completed[i].EndedAt.After(completed[j].EndedAt)
	})

	total := len(results)
	spinner := spinnerFrames[lr.frame%len(spinnerFrames)]

	var lines []string
	lines = append(lines, fmt.Sprintf("runforge — %d tasks", total))
	lines = append(lines, "")

	taskLines := 0

	// failed/skipped first
	for _, res := range failed {
		if taskLines >= maxTaskLines {
			break
		}
		lines = append(lines, lr.formatFailed(res))
		taskLines++
	}

	// running
	for _, res := range running {
		if taskLines >= maxTaskLines {
			break
		}
		lines = append(lines, lr.formatRunning(res, spinner))
		taskLines++
	}

	// completed (capped)
	shownCompleted := 0
	for _, res := range completed {
		if taskLines >= maxTaskLines {
			break
		}
		lines = append(lines, lr.formatCompleted(res))
		taskLines++
		shownCompleted++
	}
	if remaining := len(completed) - shownCompleted; remaining > 0 {
		lines = append(lines, fmt.Sprintf("  %s... %d more completed%s", lr.c(colorDim), remaining, lr.c(colorReset)))
		taskLines++
	}

	// rate-limited (capped)
	shownRateLimited := 0
	for _, res := range rateLimited {
		if taskLines >= maxTaskLines {
			break
		}
		lines = append(lines, lr.formatRateLimited(res))
		taskLines++
		shownRateLimited++
	}
	if remaining := len(rateLimited) - shownRateLimited; remaining > 0 {
		lines = append(lines, fmt.Sprintf("  %s... %d more rate-limited%s", lr.c(colorDim), remaining, lr.c(colorReset)))
		taskLines++
	}

	// queued (capped)
	shownQueued := 0
	for _, res := range queued {
		if taskLines >= maxTaskLines {
			break
		}
		lines = append(lines, lr.formatQueued(res))
		taskLines++
		shownQueued++
	}
	if remaining := len(queued) - shownQueued; remaining > 0 {
		lines = append(lines, fmt.Sprintf("  %s─ queued     %d more tasks%s", lr.c(colorDim), remaining, lr.c(colorReset)))
	}

	// progress line
	lines = append(lines, "")
	lines = append(lines, lr.progressLine(len(completed), len(running), len(failed), len(rateLimited), len(queued)))

	return lines
}

func (lr *LiveReporter) formatFailed(res *task.TaskResult) string {
	t := lr.graph.Task(res.TaskID)
	title := ""
	if t != nil {
		title = t.Title
	}
	icon := "✗"
	label := "FAILED"
	if res.State == task.StateSkipped {
		icon = "⊘"
		label = "skipped"
	}
	errMsg := res.Error
	if res.ConnectivityError != "" {
		errMsg = res.ConnectivityError
	}
	if len(errMsg) > 120 {
		errMsg = errMsg[:120] + "..."
	}
	return fmt.Sprintf("  %s%s %-10s %-25s %-30s %s%s",
		lr.c(colorRed), icon, label, res.TaskID, title, errMsg, lr.c(colorReset))
}

func (lr *LiveReporter) formatRunning(res *task.TaskResult, spinner string) string {
	t := lr.graph.Task(res.TaskID)
	title := ""
	runnerTag := ""
	if t != nil {
		title = t.Title
		if t.Runner != "" {
			runnerTag = " [" + t.Runner + "]"
		}
	}
	elapsed := time.Since(res.StartedAt).Truncate(time.Second)
	return fmt.Sprintf("  %s%s %-10s %-25s %-30s %s%s%s",
		lr.c(colorCyan), spinner, "running", res.TaskID, title, elapsed, runnerTag, lr.c(colorReset))
}

func (lr *LiveReporter) formatCompleted(res *task.TaskResult) string {
	t := lr.graph.Task(res.TaskID)
	title := ""
	if t != nil {
		title = t.Title
	}
	dur := res.Duration.Truncate(time.Second)
	suffix := ""
	if res.RunnerUsed != "" {
		if len(res.Attempts) > 1 {
			suffix = " [via " + res.RunnerUsed + "]"
		} else {
			suffix = " [" + res.RunnerUsed + "]"
		}
	}
	return fmt.Sprintf("  %s✓ %-10s %-25s %-30s %s%s%s",
		lr.c(colorGreen), "done", res.TaskID, title, dur, suffix, lr.c(colorReset))
}

func (lr *LiveReporter) formatRateLimited(res *task.TaskResult) string {
	t := lr.graph.Task(res.TaskID)
	title := ""
	if t != nil {
		title = t.Title
	}
	info := "rate limit"
	if !res.ResetsAt.IsZero() {
		remaining := time.Until(res.ResetsAt).Truncate(time.Minute)
		if remaining > 0 {
			info = fmt.Sprintf("resets in %s", remaining)
		}
	}
	return fmt.Sprintf("  %s⏸ %-10s %-25s %-30s %s%s",
		lr.c(colorYellow), "rate-limit", res.TaskID, title, info, lr.c(colorReset))
}

func (lr *LiveReporter) formatQueued(res *task.TaskResult) string {
	t := lr.graph.Task(res.TaskID)
	title := ""
	if t != nil {
		title = t.Title
	}
	dep := ""
	if t != nil && len(t.DependsOn) > 0 {
		dep = "waiting: " + strings.Join(t.DependsOn, ", ")
	}
	return fmt.Sprintf("  %s─ %-10s %-25s %-30s %s%s",
		lr.c(colorDim), "queued", res.TaskID, title, dep, lr.c(colorReset))
}

func (lr *LiveReporter) progressLine(done, running, failed, rateLimited, queued int) string {
	parts := []string{}
	if done > 0 {
		parts = append(parts, fmt.Sprintf("%s%d done%s", lr.c(colorGreen), done, lr.c(colorReset)))
	}
	if running > 0 {
		parts = append(parts, fmt.Sprintf("%s%d running%s", lr.c(colorCyan), running, lr.c(colorReset)))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%s%d failed%s", lr.c(colorRed), failed, lr.c(colorReset)))
	}
	if rateLimited > 0 {
		parts = append(parts, fmt.Sprintf("%s%d rate-limited%s", lr.c(colorYellow), rateLimited, lr.c(colorReset)))
	}
	if queued > 0 {
		parts = append(parts, fmt.Sprintf("%s%d queued%s", lr.c(colorDim), queued, lr.c(colorReset)))
	}
	return fmt.Sprintf("  progress: %s", strings.Join(parts, ", "))
}

func (lr *LiveReporter) c(code string) string {
	if !lr.color {
		return ""
	}
	return code
}
