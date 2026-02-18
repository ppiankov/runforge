package generate

import (
	"testing"
)

func TestParseWorkOrders_KafkaspectreFmt(t *testing.T) {
	// Format: ## WO-01: Title / **Goal:** text / ### Acceptance / - bullet
	// No status line, no priority line — defaults to planned + priority 2.
	input := `# Work Orders — kafkaspectre

## WO-01: CLI Entry Point

**Goal:** Create cmd/kafkaspectre/main.go with Cobra wiring.

### Details
- Wire existing internal/kafka.Inspector into Cobra

### Acceptance
- make build produces working binary
- make test passes with -race

---

## WO-02: Tests

**Goal:** Add unit tests for inspector and reporter.

### Acceptance
- Coverage > 85% on reporter
- make test passes with -race
`

	wos := ParseWorkOrders(input)
	if len(wos) != 2 {
		t.Fatalf("expected 2 WOs, got %d", len(wos))
	}

	wo := wos[0]
	if wo.RawID != "01" {
		t.Errorf("RawID = %q, want %q", wo.RawID, "01")
	}
	if wo.Title != "CLI Entry Point" {
		t.Errorf("Title = %q, want %q", wo.Title, "CLI Entry Point")
	}
	if wo.Status != StatusPlanned {
		t.Errorf("Status = %d, want StatusPlanned", wo.Status)
	}
	if wo.Priority != 2 {
		t.Errorf("Priority = %d, want 2", wo.Priority)
	}
	if wo.Summary != "Create cmd/kafkaspectre/main.go with Cobra wiring." {
		t.Errorf("Summary = %q", wo.Summary)
	}
	if len(wo.Acceptance) != 2 {
		t.Errorf("Acceptance count = %d, want 2", len(wo.Acceptance))
	}
}

func TestParseWorkOrders_RunforgeFmt(t *testing.T) {
	// Format: **Status:** + **Priority:** + ### Summary + ### Acceptance criteria + - [ ] bullet
	input := `# Work Orders — runforge

## WO-1: Multi-dependency support

**Status:** ` + "`[ ]`" + ` planned
**Priority:** high — foundational change

### Summary

Change depends_on from a single string to a string array so tasks can depend on multiple predecessors.

### Acceptance criteria

- [ ] Task file accepts array and string
- [ ] Graph builds correct in-degrees
- [ ] make test && make lint pass

---

## WO-2: Script runner backend

**Status:** ` + "`[ ]`" + ` planned
**Priority:** high — enables non-AI orchestration

### Summary

Add a script runner that executes arbitrary shell commands.

### Acceptance criteria

- [ ] runner: script executes prompt as sh -c
- [ ] Non-zero exit code marks StateFailed
`

	wos := ParseWorkOrders(input)
	if len(wos) != 2 {
		t.Fatalf("expected 2 WOs, got %d", len(wos))
	}

	wo := wos[0]
	if wo.RawID != "1" {
		t.Errorf("RawID = %q, want %q", wo.RawID, "1")
	}
	if wo.Priority != 1 {
		t.Errorf("Priority = %d, want 1 (high)", wo.Priority)
	}
	if wo.Summary != "Change depends_on from a single string to a string array so tasks can depend on multiple predecessors." {
		t.Errorf("Summary = %q", wo.Summary)
	}
	if len(wo.Acceptance) != 3 {
		t.Errorf("Acceptance count = %d, want 3", len(wo.Acceptance))
	}
	if wo.Acceptance[0] != "Task file accepts array and string" {
		t.Errorf("Acceptance[0] = %q", wo.Acceptance[0])
	}
}

func TestParseWorkOrders_ClickspectreFmt(t *testing.T) {
	// Phase headings must be skipped. WO IDs have letter prefix.
	input := `# Work Orders — clickspectre

## Phase 1: Core (v1.0.0) ✅

All Phase 1 work shipped.

---

## Phase 2: Hardening

---

## WO-C01: Test coverage push

**Goal:** Coverage is 0% — no test files exist.

### Acceptance
- Coverage > 60% overall
- make test passes with -race

---

## WO-C02: Structured logging

**Goal:** Replace ad-hoc print statements with log/slog.

### Acceptance
- Silent by default
- make test passes with -race
`

	wos := ParseWorkOrders(input)
	if len(wos) != 2 {
		t.Fatalf("expected 2 WOs (phase headings skipped), got %d", len(wos))
	}

	if wos[0].RawID != "C01" {
		t.Errorf("RawID = %q, want %q", wos[0].RawID, "C01")
	}
	if wos[0].Summary != "Coverage is 0% — no test files exist." {
		t.Errorf("Summary = %q", wos[0].Summary)
	}
}

func TestParseWorkOrders_ChainwatchDoneFmt(t *testing.T) {
	// Completed WOs with ✅ in heading should be marked done.
	input := `# Work Orders — chainwatch

## WO-CW01: Monotonic State Machine ✅

**Goal:** Implement two-stage boundary enforcement.

### Implementation
- internal/model/boundary.go — BoundaryZone constants

---

## WO-CW12: Hash-chained audit log

**Goal:** Create internal/audit/log.go — AuditLog with JSONL.

### Acceptance
- Verify exits 0 on clean log
- 10K entries verify in less than 1s
`

	wos := ParseWorkOrders(input)
	if len(wos) != 2 {
		t.Fatalf("expected 2 WOs, got %d", len(wos))
	}

	if wos[0].Status != StatusDone {
		t.Errorf("CW01 status = %d, want StatusDone (checkmark in heading)", wos[0].Status)
	}
	if wos[1].Status != StatusPlanned {
		t.Errorf("CW12 status = %d, want StatusPlanned", wos[1].Status)
	}
	if wos[1].RawID != "CW12" {
		t.Errorf("RawID = %q, want %q", wos[1].RawID, "CW12")
	}
}

func TestParseWorkOrders_DoneBracketInHeading(t *testing.T) {
	// Logtap-style [DONE] marker in heading should be detected.
	input := `### WO-01: HTTP receiver with Loki push API [DONE]

**Goal:** Accept log streams via POST /loki/api/v1/push.

---

### WO-65: Add TLS support to forwarder push path

**Goal:** Enable https:// in push.go.
`

	wos := ParseWorkOrders(input)
	if len(wos) != 2 {
		t.Fatalf("expected 2 WOs, got %d", len(wos))
	}

	if wos[0].Status != StatusDone {
		t.Errorf("WO-01 status = %d, want StatusDone ([DONE] in heading)", wos[0].Status)
	}
	if wos[0].Title != "HTTP receiver with Loki push API" {
		t.Errorf("WO-01 title = %q, want title without [DONE] suffix", wos[0].Title)
	}
	if wos[1].Status != StatusPlanned {
		t.Errorf("WO-65 status = %d, want StatusPlanned", wos[1].Status)
	}
}

func TestParseWorkOrders_StatusExplicitDone(t *testing.T) {
	input := "## WO-V01: Test coverage\n\n**Status:** `[x]` COMPLETE — abc123\n\n**Goal:** Add tests.\n"

	wos := ParseWorkOrders(input)
	if len(wos) != 1 {
		t.Fatalf("expected 1 WO, got %d", len(wos))
	}
	if wos[0].Status != StatusDone {
		t.Errorf("Status = %d, want StatusDone", wos[0].Status)
	}
}

func TestParseWorkOrders_StatusInProgress(t *testing.T) {
	input := "## WO-S01: Scanner\n\n**Status:** `[~]` in progress\n\n**Goal:** Add scanning.\n"

	wos := ParseWorkOrders(input)
	if len(wos) != 1 {
		t.Fatalf("expected 1 WO, got %d", len(wos))
	}
	if wos[0].Status != StatusInProgress {
		t.Errorf("Status = %d, want StatusInProgress", wos[0].Status)
	}
}

func TestParseWorkOrders_PriorityMapping(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{"**Priority:** high — foundational", 1},
		{"**Priority:** medium", 2},
		{"**Priority:** low — nice to have", 3},
	}

	for _, tt := range tests {
		input := "## WO-1: Test\n\n" + tt.line + "\n\n**Goal:** Testing.\n"
		wos := ParseWorkOrders(input)
		if len(wos) != 1 {
			t.Fatalf("expected 1 WO for %q, got %d", tt.line, len(wos))
		}
		if wos[0].Priority != tt.want {
			t.Errorf("Priority for %q = %d, want %d", tt.line, wos[0].Priority, tt.want)
		}
	}
}

func TestParseWorkOrders_PriorityDefault(t *testing.T) {
	input := "## WO-01: No priority\n\n**Goal:** Testing.\n"
	wos := ParseWorkOrders(input)
	if len(wos) != 1 {
		t.Fatalf("expected 1 WO, got %d", len(wos))
	}
	if wos[0].Priority != 2 {
		t.Errorf("Priority = %d, want 2 (default medium)", wos[0].Priority)
	}
}

func TestParseWorkOrders_DependencyDetection(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		wantD string
	}{
		{"depends on", "This depends on WO-01 being done first.", "01"},
		{"requires", "Requires WO-CW06 to be complete.", "CW06"},
		{"builds on", "Builds on WO-V01 foundation.", "V01"},
		{"after", "Run after WO-S01 completes.", "S01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := "## WO-99: Dep test\n\n**Goal:** Test deps.\n\n### Details\n" + tt.text + "\n"
			wos := ParseWorkOrders(input)
			if len(wos) != 1 {
				t.Fatalf("expected 1 WO, got %d", len(wos))
			}
			if wos[0].DependsOn != tt.wantD {
				t.Errorf("DependsOn = %q, want %q", wos[0].DependsOn, tt.wantD)
			}
		})
	}
}

func TestParseWorkOrders_EmptyFile(t *testing.T) {
	wos := ParseWorkOrders("")
	if len(wos) != 0 {
		t.Errorf("expected 0 WOs for empty file, got %d", len(wos))
	}
}

func TestParseWorkOrders_NoWOs(t *testing.T) {
	input := "# Work Orders\n\n## Phase 1: Core ✅\n\nAll done.\n\n## Non-Goals\n\n- No web UI\n"
	wos := ParseWorkOrders(input)
	if len(wos) != 0 {
		t.Errorf("expected 0 WOs, got %d", len(wos))
	}
}

func TestParseWorkOrders_GoalTakesPriorityOverSummary(t *testing.T) {
	input := "## WO-01: Test\n\n**Goal:** The goal text.\n\n### Summary\n\nThe summary text.\n"
	wos := ParseWorkOrders(input)
	if len(wos) != 1 {
		t.Fatalf("expected 1 WO, got %d", len(wos))
	}
	// Goal was found first, should take priority.
	if wos[0].Summary != "The goal text." {
		t.Errorf("Summary = %q, want %q", wos[0].Summary, "The goal text.")
	}
}

func TestParseWorkOrders_AcceptanceCriteriaWithCheckboxes(t *testing.T) {
	input := `## WO-1: Test

**Goal:** Testing.

### Acceptance criteria

- [ ] First criterion
- [ ] Second criterion
- [x] Third completed criterion
`

	wos := ParseWorkOrders(input)
	if len(wos) != 1 {
		t.Fatalf("expected 1 WO, got %d", len(wos))
	}
	if len(wos[0].Acceptance) != 3 {
		t.Errorf("Acceptance count = %d, want 3", len(wos[0].Acceptance))
	}
	if wos[0].Acceptance[0] != "First criterion" {
		t.Errorf("Acceptance[0] = %q, want %q", wos[0].Acceptance[0], "First criterion")
	}
}

func TestParseWorkOrders_H3WOHeading(t *testing.T) {
	// Some repos use ### for WO headings under ## Phase sections.
	input := `## Phase 1: Foundation

### WO-T01: Initial setup

**Goal:** Set up project structure.

### Acceptance
- Project builds
`

	wos := ParseWorkOrders(input)
	if len(wos) != 1 {
		t.Fatalf("expected 1 WO, got %d", len(wos))
	}
	if wos[0].RawID != "T01" {
		t.Errorf("RawID = %q, want %q", wos[0].RawID, "T01")
	}
}

func TestParseWorkOrders_FallbackTitleAsSummary(t *testing.T) {
	// WO with no Goal and no Summary section — title is used as-is, summary empty.
	input := "## WO-01: Create CLI entry point\n\n### Details\n- Wire Cobra.\n"
	wos := ParseWorkOrders(input)
	if len(wos) != 1 {
		t.Fatalf("expected 1 WO, got %d", len(wos))
	}
	// Summary should be empty — no Goal or Summary section found.
	if wos[0].Summary != "" {
		t.Errorf("Summary = %q, want empty", wos[0].Summary)
	}
}

func TestParseWorkOrders_CodeBlockSkipped(t *testing.T) {
	// WO headings inside fenced code blocks should be ignored.
	input := "## WO-9: Generate command\n\n**Goal:** Generate task files.\n\n### Notes\n\nThe format:\n\n```markdown\n## WO-1: Title here\n\n**Status:** `[ ]` planned\n```\n"

	wos := ParseWorkOrders(input)
	if len(wos) != 1 {
		t.Fatalf("expected 1 WO (code block example skipped), got %d", len(wos))
	}
	if wos[0].RawID != "9" {
		t.Errorf("RawID = %q, want %q", wos[0].RawID, "9")
	}
}

func TestParseWorkOrders_DependencyOnDoneWO(t *testing.T) {
	// Dependency regex should still detect refs to completed WOs.
	// The CLI layer handles dropping them — parser just reports what it finds.
	input := "## WO-17: Feature\n\n**Goal:** Depends on WO-26 being done.\n"

	wos := ParseWorkOrders(input)
	if len(wos) != 1 {
		t.Fatalf("expected 1 WO, got %d", len(wos))
	}
	if wos[0].DependsOn != "26" {
		t.Errorf("DependsOn = %q, want %q", wos[0].DependsOn, "26")
	}
}

func TestNormalizeID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"01", "WO01"},
		{"1", "WO1"},
		{"19", "WO19"},
		{"C01", "WO-C01"},
		{"V01", "WO-V01"},
		{"S01", "WO-S01"},
		{"CW01", "WO-CW01"},
		{"CW12", "WO-CW12"},
		{"T01", "WO-T01"},
		{"23.1", "WO-23.1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeID(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTaskID(t *testing.T) {
	tests := []struct {
		repo  string
		rawID string
		want  string
	}{
		{"kafkaspectre", "01", "kafkaspectre-WO01"},
		{"kafkaspectre", "02", "kafkaspectre-WO02"},
		{"clickspectre", "C01", "clickspectre-WO-C01"},
		{"vaultspectre", "V01", "vaultspectre-WO-V01"},
		{"s3spectre", "S01", "s3spectre-WO-S01"},
		{"chainwatch", "CW12", "chainwatch-WO-CW12"},
		{"spectrehub", "02", "spectrehub-WO02"},
		{"pgspectre", "19", "pgspectre-WO19"},
		{"pgspectre", "23.1", "pgspectre-WO-23.1"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := TaskID(tt.repo, tt.rawID)
			if got != tt.want {
				t.Errorf("TaskID(%q, %q) = %q, want %q", tt.repo, tt.rawID, got, tt.want)
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	wo := WorkOrder{
		RawID:      "01",
		Summary:    "Create CLI entry point.",
		Acceptance: []string{"make build works", "make test passes"},
	}
	got := BuildPrompt(wo)

	if !containsSubstr(got, "Create CLI entry point.") {
		t.Error("prompt missing summary")
	}
	if !containsSubstr(got, "make build works; make test passes") {
		t.Error("prompt missing acceptance criteria")
	}
	if !containsSubstr(got, "Read docs/work-orders.md WO-01") {
		t.Error("prompt missing WO reference")
	}
}

func TestBuildPrompt_NoAcceptance(t *testing.T) {
	wo := WorkOrder{
		RawID:   "C01",
		Summary: "Add tests.",
	}
	got := BuildPrompt(wo)

	if containsSubstr(got, "Acceptance:") {
		t.Error("prompt should not contain Acceptance section when empty")
	}
	if !containsSubstr(got, "Read docs/work-orders.md WO-C01") {
		t.Error("prompt missing WO reference")
	}
}

func containsSubstr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsCheck(s, substr))
}

func containsCheck(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
