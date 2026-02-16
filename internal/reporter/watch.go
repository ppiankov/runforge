package reporter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ppiankov/runforge/internal/runner"
)

// TaskSnapshot holds the observed state of a single task from disk.
type TaskSnapshot struct {
	ID         string
	State      string // "running", "completed", "failed", "queued"
	EventCount int
	StartedAt  time.Time
	LastAction string
	Failed     bool
	fileOffset int64
}

// WatchReporter monitors run directories and renders a top-like display.
type WatchReporter struct {
	w         io.Writer
	color     bool
	runDir    string
	snapshots map[string]*TaskSnapshot
	lastLines int
	frame     int
	runStart  time.Time
}

// NewWatchReporter creates a watch reporter for the given run directory.
func NewWatchReporter(w io.Writer, color bool, runDir string) *WatchReporter {
	return &WatchReporter{
		w:         w,
		color:     color,
		runDir:    runDir,
		snapshots: make(map[string]*TaskSnapshot),
	}
}

// Run starts the watch loop, refreshing every 1s until stop is closed or the run completes.
func (wr *WatchReporter) Run(stop <-chan struct{}) error {
	if err := wr.discoverTasks(); err != nil {
		return err
	}
	wr.runStart = wr.earliestDirTime()
	wr.refresh()
	wr.render()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			fmt.Fprintln(wr.w)
			return nil
		case <-ticker.C:
			_ = wr.discoverTasks()
			wr.refresh()
			wr.render()
			if wr.runCompleted() {
				fmt.Fprintf(wr.w, "\n%srun completed%s\n", wr.c(colorGreen), wr.c(colorReset))
				return nil
			}
		}
	}
}

func (wr *WatchReporter) discoverTasks() error {
	entries, err := os.ReadDir(wr.runDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if _, exists := wr.snapshots[id]; !exists {
			snap := &TaskSnapshot{
				ID:    id,
				State: "queued",
			}
			// use events.jsonl mod time as start proxy
			evPath := filepath.Join(wr.runDir, id, "events.jsonl")
			if info, err := os.Stat(evPath); err == nil {
				snap.StartedAt = info.ModTime()
				snap.State = "running"
			}
			wr.snapshots[id] = snap
		}
	}
	return nil
}

func (wr *WatchReporter) earliestDirTime() time.Time {
	var earliest time.Time
	for _, snap := range wr.snapshots {
		if !snap.StartedAt.IsZero() && (earliest.IsZero() || snap.StartedAt.Before(earliest)) {
			earliest = snap.StartedAt
		}
	}
	if earliest.IsZero() {
		// fallback: stat the run directory itself
		if info, err := os.Stat(wr.runDir); err == nil {
			earliest = info.ModTime()
		} else {
			earliest = time.Now()
		}
	}
	return earliest
}

func (wr *WatchReporter) refresh() {
	for id, snap := range wr.snapshots {
		evPath := filepath.Join(wr.runDir, id, "events.jsonl")
		wr.readNewEvents(snap, evPath)
	}
}

func (wr *WatchReporter) readNewEvents(snap *TaskSnapshot, path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	if snap.fileOffset > 0 {
		if _, err := f.Seek(snap.fileOffset, io.SeekStart); err != nil {
			return
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	bytesRead := int64(0)

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesRead += int64(len(line)) + 1 // +1 for newline
		snap.EventCount++

		var ev runner.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		processEvent(snap, &ev)
	}

	snap.fileOffset += bytesRead

	// if we got events and start time was zero, set it now
	if snap.EventCount > 0 && snap.StartedAt.IsZero() {
		if info, err := os.Stat(path); err == nil {
			snap.StartedAt = info.ModTime()
		} else {
			snap.StartedAt = time.Now()
		}
	}
}

func processEvent(snap *TaskSnapshot, ev *runner.Event) {
	switch ev.Type {
	case runner.EventThreadStarted:
		snap.State = "running"
	case runner.EventItemStarted:
		snap.State = "running"
		if ev.Item != nil {
			snap.LastAction = formatAction(ev.Item)
		}
	case runner.EventItemCompleted:
		snap.State = "running"
		if ev.Item != nil {
			snap.LastAction = formatAction(ev.Item)
		}
	case runner.EventTurnCompleted:
		snap.State = "completed"
	case runner.EventTurnFailed:
		snap.State = "failed"
		snap.Failed = true
	}
}

func formatAction(item *runner.Item) string {
	const maxLen = 40

	switch item.Type {
	case "command_execution":
		cmd := item.Command
		// strip shell prefix: /bin/zsh -lc <actual command>
		if idx := strings.Index(cmd, " -lc "); idx >= 0 {
			cmd = cmd[idx+5:]
		}
		cmd = strings.Trim(cmd, "'\"")
		if len(cmd) > maxLen {
			cmd = cmd[:maxLen] + "..."
		}
		return "cmd: " + cmd
	case "reasoning":
		return "reasoning"
	case "agent_message":
		return "agent_message"
	default:
		return item.Type
	}
}

func (wr *WatchReporter) runCompleted() bool {
	_, err := os.Stat(filepath.Join(wr.runDir, "report.json"))
	return err == nil
}

func (wr *WatchReporter) render() {
	lines := wr.buildLines()

	if wr.lastLines > 0 {
		fmt.Fprintf(wr.w, "\033[%dA", wr.lastLines)
	}

	for _, line := range lines {
		fmt.Fprintf(wr.w, "\033[K%s\n", line)
	}

	wr.lastLines = len(lines)
	wr.frame++
}

func (wr *WatchReporter) buildLines() []string {
	var running, completed, failed, queued []*TaskSnapshot

	for _, snap := range wr.snapshots {
		switch snap.State {
		case "running":
			running = append(running, snap)
		case "completed":
			completed = append(completed, snap)
		case "failed":
			failed = append(failed, snap)
		default:
			queued = append(queued, snap)
		}
	}

	sortByID(running)
	sortByID(completed)
	sortByID(failed)
	sortByID(queued)

	runName := filepath.Base(wr.runDir)
	elapsed := time.Since(wr.runStart).Truncate(time.Second)
	runState := "running"
	if wr.runCompleted() {
		runState = "completed"
	}

	spinner := spinnerFrames[wr.frame%len(spinnerFrames)]

	var lines []string

	// header
	lines = append(lines, fmt.Sprintf("runforge watch — %s (%s %s)", runName, runState, elapsed))
	lines = append(lines, fmt.Sprintf("tasks: %s%d running%s, %s%d completed%s, %s%d failed%s, %s%d queued%s",
		wr.c(colorCyan), len(running), wr.c(colorReset),
		wr.c(colorGreen), len(completed), wr.c(colorReset),
		wr.c(colorRed), len(failed), wr.c(colorReset),
		wr.c(colorDim), len(queued), wr.c(colorReset)))
	lines = append(lines, "")

	// column headers
	lines = append(lines, fmt.Sprintf("  %-27s %-12s %6s %8s   %s", "TASK", "STATE", "EVENTS", "ELAPSED", "LAST ACTION"))

	// failed first
	for _, snap := range failed {
		lines = append(lines, wr.fmtFailed(snap))
	}

	// running
	for _, snap := range running {
		lines = append(lines, wr.fmtRunning(snap, spinner))
	}

	// completed (cap at 10)
	shown := 0
	for _, snap := range completed {
		if shown >= 10 {
			break
		}
		lines = append(lines, wr.fmtCompleted(snap))
		shown++
	}
	if remaining := len(completed) - shown; remaining > 0 {
		lines = append(lines, fmt.Sprintf("  %s... %d more completed%s", wr.c(colorDim), remaining, wr.c(colorReset)))
	}

	// queued (cap at 5)
	shown = 0
	for _, snap := range queued {
		if shown >= 5 {
			break
		}
		lines = append(lines, wr.fmtQueued(snap))
		shown++
	}
	if remaining := len(queued) - shown; remaining > 0 {
		lines = append(lines, fmt.Sprintf("  %s... %d more queued%s", wr.c(colorDim), remaining, wr.c(colorReset)))
	}

	// footer
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %sctrl+c to quit | refreshing every 1s%s", wr.c(colorDim), wr.c(colorReset)))

	return lines
}

func (wr *WatchReporter) fmtRunning(snap *TaskSnapshot, spinner string) string {
	elapsed := wr.elapsedStr(snap)
	action := snap.LastAction
	if len(action) > 45 {
		action = action[:45] + "..."
	}
	return fmt.Sprintf("  %s%s %-25s %-10s %6d %8s   %s%s",
		wr.c(colorCyan), spinner, snap.ID, "running", snap.EventCount, elapsed, action, wr.c(colorReset))
}

func (wr *WatchReporter) fmtCompleted(snap *TaskSnapshot) string {
	elapsed := wr.elapsedStr(snap)
	return fmt.Sprintf("  %s✓ %-25s %-10s %6d %8s   %s%s",
		wr.c(colorGreen), snap.ID, "done", snap.EventCount, elapsed, snap.LastAction, wr.c(colorReset))
}

func (wr *WatchReporter) fmtFailed(snap *TaskSnapshot) string {
	elapsed := wr.elapsedStr(snap)
	return fmt.Sprintf("  %s✗ %-25s %-10s %6d %8s   %s%s",
		wr.c(colorRed), snap.ID, "FAILED", snap.EventCount, elapsed, snap.LastAction, wr.c(colorReset))
}

func (wr *WatchReporter) fmtQueued(snap *TaskSnapshot) string {
	return fmt.Sprintf("  %s─ %-25s %-10s %6s %8s   %s%s",
		wr.c(colorDim), snap.ID, "queued", "-", "-", "-", wr.c(colorReset))
}

func (wr *WatchReporter) elapsedStr(snap *TaskSnapshot) string {
	if snap.StartedAt.IsZero() {
		return "-"
	}
	return time.Since(snap.StartedAt).Truncate(time.Second).String()
}

func (wr *WatchReporter) c(code string) string {
	if !wr.color {
		return ""
	}
	return code
}

func sortByID(snaps []*TaskSnapshot) {
	sort.Slice(snaps, func(i, j int) bool {
		return snaps[i].ID < snaps[j].ID
	})
}
