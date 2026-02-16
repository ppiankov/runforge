package reporter

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/runforge/internal/task"
)

func TestLiveReporter_Render(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "Build"},
		{ID: "t2", Repo: "org/r", Priority: 1, Title: "Test", DependsOn: []string{"t1"}},
		{ID: "t3", Repo: "org/r", Priority: 1, Title: "Deploy", DependsOn: []string{"t2"}},
	}

	g, err := task.BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	results := map[string]*task.TaskResult{
		"t1": {TaskID: "t1", State: task.StateCompleted, Duration: 30 * time.Second, EndedAt: time.Now()},
		"t2": {TaskID: "t2", State: task.StateRunning, StartedAt: time.Now().Add(-10 * time.Second)},
		"t3": {TaskID: "t3", State: task.StatePending},
	}

	var buf bytes.Buffer
	lr := NewLiveReporter(&buf, false, g, func() map[string]*task.TaskResult { return results })

	lines := lr.Render(results)
	output := strings.Join(lines, "\n")

	if !strings.Contains(output, "t1") {
		t.Error("expected t1 in output")
	}
	if !strings.Contains(output, "t2") {
		t.Error("expected t2 in output")
	}
	if !strings.Contains(output, "t3") {
		t.Error("expected t3 in output")
	}
	if !strings.Contains(output, "running") {
		t.Error("expected 'running' label in output")
	}
	if !strings.Contains(output, "done") {
		t.Error("expected 'done' label in output")
	}
	if !strings.Contains(output, "queued") {
		t.Error("expected 'queued' label in output")
	}
	if !strings.Contains(output, "progress:") {
		t.Error("expected progress line in output")
	}
	if !strings.Contains(output, "waiting: t2") {
		t.Error("expected dependency info for t3")
	}
}

func TestLiveReporter_SpinnerAdvances(t *testing.T) {
	tasks := []task.Task{
		{ID: "t1", Repo: "org/r", Priority: 1, Title: "Build"},
	}

	g, err := task.BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	results := map[string]*task.TaskResult{
		"t1": {TaskID: "t1", State: task.StateRunning, StartedAt: time.Now()},
	}

	var buf bytes.Buffer
	lr := NewLiveReporter(&buf, false, g, func() map[string]*task.TaskResult { return results })

	lines1 := lr.Render(results)
	lr.frame = 1
	lines2 := lr.Render(results)

	// find the running line in each
	var run1, run2 string
	for _, l := range lines1 {
		if strings.Contains(l, "running") {
			run1 = l
			break
		}
	}
	for _, l := range lines2 {
		if strings.Contains(l, "running") {
			run2 = l
			break
		}
	}

	if run1 == run2 {
		t.Error("expected spinner to change between frames")
	}
}

func TestLiveReporter_Overflow(t *testing.T) {
	var tasks []task.Task
	for i := 0; i < 30; i++ {
		tasks = append(tasks, task.Task{
			ID:       fmt.Sprintf("t%02d", i),
			Repo:     "org/r",
			Priority: 1,
			Title:    fmt.Sprintf("Task %d", i),
			Prompt:   "do stuff",
		})
	}

	g, err := task.BuildGraph(tasks)
	if err != nil {
		t.Fatal(err)
	}

	results := make(map[string]*task.TaskResult)
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("t%02d", i)
		results[id] = &task.TaskResult{
			TaskID:  id,
			State:   task.StateCompleted,
			EndedAt: time.Now(),
		}
	}

	var buf bytes.Buffer
	lr := NewLiveReporter(&buf, false, g, func() map[string]*task.TaskResult { return results })

	lines := lr.Render(results)
	output := strings.Join(lines, "\n")

	if !strings.Contains(output, "more completed") {
		t.Error("expected 'more completed' overflow indicator")
	}
}
