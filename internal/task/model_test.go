package task

import (
	"encoding/json"
	"testing"
)

func TestRunReport_UnmarshalNewFormat(t *testing.T) {
	data := `{
		"tasks_files": ["a.json", "b.json"],
		"workers": 4,
		"repos_dir": "/tmp/repos",
		"results": {},
		"total_tasks": 0,
		"timestamp": "2025-01-01T00:00:00Z"
	}`

	var r RunReport
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.TasksFiles) != 2 {
		t.Fatalf("expected 2 tasks_files, got %d", len(r.TasksFiles))
	}
	if r.TasksFiles[0] != "a.json" || r.TasksFiles[1] != "b.json" {
		t.Errorf("unexpected tasks_files: %v", r.TasksFiles)
	}
}

func TestRunReport_UnmarshalOldFormat(t *testing.T) {
	data := `{
		"tasks_file": "legacy.json",
		"workers": 2,
		"repos_dir": "/tmp/repos",
		"results": {},
		"total_tasks": 0,
		"timestamp": "2025-01-01T00:00:00Z"
	}`

	var r RunReport
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.TasksFiles) != 1 {
		t.Fatalf("expected 1 tasks_files, got %d", len(r.TasksFiles))
	}
	if r.TasksFiles[0] != "legacy.json" {
		t.Errorf("expected tasks_files [legacy.json], got %v", r.TasksFiles)
	}
}

func TestRunReport_UnmarshalNewOverridesOld(t *testing.T) {
	data := `{
		"tasks_file": "old.json",
		"tasks_files": ["new1.json", "new2.json"],
		"workers": 1,
		"repos_dir": "/tmp",
		"results": {},
		"total_tasks": 0,
		"timestamp": "2025-01-01T00:00:00Z"
	}`

	var r RunReport
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.TasksFiles) != 2 {
		t.Fatalf("expected 2 tasks_files (new format wins), got %d", len(r.TasksFiles))
	}
	if r.TasksFiles[0] != "new1.json" {
		t.Errorf("expected new1.json, got %v", r.TasksFiles)
	}
}

func TestTask_UnmarshalDependsOn_String(t *testing.T) {
	data := `{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": "p", "depends_on": "dep1"}`
	var task Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != "dep1" {
		t.Errorf("expected [dep1], got %v", task.DependsOn)
	}
}

func TestTask_UnmarshalDependsOn_Array(t *testing.T) {
	data := `{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": "p", "depends_on": ["a", "b"]}`
	var task Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(task.DependsOn) != 2 || task.DependsOn[0] != "a" || task.DependsOn[1] != "b" {
		t.Errorf("expected [a b], got %v", task.DependsOn)
	}
}

func TestTask_UnmarshalDependsOn_Null(t *testing.T) {
	data := `{"id": "t1", "repo": "org/r", "priority": 1, "title": "A", "prompt": "p", "depends_on": null}`
	var task Task
	if err := json.Unmarshal([]byte(data), &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(task.DependsOn) != 0 {
		t.Errorf("expected empty depends_on, got %v", task.DependsOn)
	}
}
