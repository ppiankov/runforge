package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/runforge/internal/config"
	"github.com/ppiankov/runforge/internal/task"
)

func newVerifyCmd() *cobra.Command {
	var (
		runDir   string
		reposDir string
		markDone bool
	)

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Proofcheck a completed run: detect false positives, verify tests and lint",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadSettings(configFile)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if !cmd.Flags().Changed("repos-dir") && cfg.ReposDir != "" {
				reposDir = cfg.ReposDir
			}
			return verifyRun(runDir, reposDir, markDone)
		},
	}

	cmd.Flags().StringVar(&runDir, "run-dir", "", "path to .runforge/<timestamp> directory (required)")
	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	cmd.Flags().BoolVar(&markDone, "mark-done", false, "update work-orders.md for verified tasks")
	_ = cmd.MarkFlagRequired("run-dir")

	return cmd
}

// verifyResult holds the proofcheck outcome for a single task.
type verifyResult struct {
	TaskID string
	Repo   string
	Status string // PASS, FAIL, SKIP
	Reason string
	Checks []checkResult
}

type checkResult struct {
	Name   string
	Passed bool
	Detail string
}

func verifyRun(runDir, reposDir string, markDone bool) error {
	reportPath := filepath.Join(runDir, "report.json")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}

	var report task.RunReport
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("parse report: %w", err)
	}

	// load task files to get repo mapping and task metadata
	taskFiles, err := config.LoadMulti(report.TasksFiles)
	if err != nil {
		return fmt.Errorf("load tasks files: %w", err)
	}
	tf, err := config.MergeTaskFiles(taskFiles)
	if err != nil {
		return fmt.Errorf("merge tasks files: %w", err)
	}

	// build task lookup
	taskMap := make(map[string]*task.Task, len(tf.Tasks))
	for i := range tf.Tasks {
		taskMap[tf.Tasks[i].ID] = &tf.Tasks[i]
	}

	var results []verifyResult
	anyFailed := false

	for taskID, res := range report.Results {
		t := taskMap[taskID]
		if t == nil {
			results = append(results, verifyResult{
				TaskID: taskID,
				Status: "SKIP",
				Reason: "task not found in task file",
			})
			continue
		}

		if res.State != task.StateCompleted {
			results = append(results, verifyResult{
				TaskID: taskID,
				Repo:   t.Repo,
				Status: "SKIP",
				Reason: fmt.Sprintf("state: %s", res.State),
			})
			continue
		}

		vr := proofcheckTask(taskID, t, res, runDir, reposDir)
		results = append(results, vr)
		if vr.Status == "FAIL" {
			anyFailed = true
		}
	}

	// print results table
	fmt.Printf("\n%-30s %-20s %-6s %s\n", "TASK", "REPO", "STATUS", "REASON")
	fmt.Println(strings.Repeat("─", 90))
	for _, vr := range results {
		repo := vr.Repo
		if idx := strings.LastIndex(repo, "/"); idx >= 0 {
			repo = repo[idx+1:]
		}
		var icon string
		switch vr.Status {
		case "FAIL":
			icon = "✗"
		case "SKIP":
			icon = "─"
		default:
			icon = "✓"
		}
		fmt.Printf("%s %-29s %-20s %-6s %s\n", icon, vr.TaskID, repo, vr.Status, vr.Reason)
	}

	// summary
	var pass, fail, skip int
	for _, vr := range results {
		switch vr.Status {
		case "PASS":
			pass++
		case "FAIL":
			fail++
		case "SKIP":
			skip++
		}
	}
	fmt.Printf("\nVerified: %d pass, %d fail, %d skip\n", pass, fail, skip)

	// suggest graylist candidates: runners that reported success with 0 events
	suggestGraylistCandidates(results, report)

	if markDone && pass > 0 {
		fmt.Println("\n--mark-done is not yet implemented")
	}

	if anyFailed {
		return fmt.Errorf("verification failed for %d tasks", fail)
	}
	return nil
}

// proofcheckTask runs verification checks against a single completed task.
func proofcheckTask(taskID string, t *task.Task, res *task.TaskResult, runDir, reposDir string) verifyResult {
	vr := verifyResult{
		TaskID: taskID,
		Repo:   t.Repo,
	}

	// determine the output directory for this task
	taskOutputDir := filepath.Join(runDir, taskID)

	// Check 1: non-empty events
	eventsCheck := checkEvents(taskOutputDir, res)
	vr.Checks = append(vr.Checks, eventsCheck)

	// Check 2: git diff (repo has changes or commits from this task)
	repoPath := config.RepoPath(t.Repo, reposDir)
	diffCheck := checkGitDiff(repoPath)
	vr.Checks = append(vr.Checks, diffCheck)

	// Check 3: tests pass
	testCheck := checkTests(repoPath)
	vr.Checks = append(vr.Checks, testCheck)

	// Check 4: lint clean
	lintCheck := checkLint(repoPath)
	vr.Checks = append(vr.Checks, lintCheck)

	// determine overall status
	var failures []string
	for _, c := range vr.Checks {
		if !c.Passed {
			failures = append(failures, c.Name+": "+c.Detail)
		}
	}

	if len(failures) == 0 {
		vr.Status = "PASS"
		vr.Reason = "all checks passed"
	} else {
		vr.Status = "FAIL"
		vr.Reason = strings.Join(failures, "; ")
	}

	return vr
}

// checkEvents verifies that the task produced non-empty event output.
func checkEvents(taskOutputDir string, _ *task.TaskResult) checkResult {
	// find events.jsonl — could be in main dir or in an attempt subdirectory
	eventsPath := filepath.Join(taskOutputDir, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		// check attempt directories
		entries, _ := os.ReadDir(taskOutputDir)
		for _, e := range entries {
			if e.IsDir() && strings.HasPrefix(e.Name(), "attempt-") {
				candidate := filepath.Join(taskOutputDir, e.Name(), "events.jsonl")
				if _, err := os.Stat(candidate); err == nil {
					eventsPath = candidate
					break
				}
			}
		}
	}

	f, err := os.Open(eventsPath)
	if err != nil {
		return checkResult{Name: "events", Passed: false, Detail: "events.jsonl not found"}
	}
	defer func() { _ = f.Close() }()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			count++
		}
	}

	if count == 0 {
		return checkResult{Name: "events", Passed: false, Detail: "events.jsonl is empty (false positive)"}
	}
	return checkResult{Name: "events", Passed: true, Detail: fmt.Sprintf("%d events", count)}
}

// checkGitDiff verifies the repo has no uncommitted changes left behind.
func checkGitDiff(repoPath string) checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return checkResult{Name: "git-diff", Passed: false, Detail: fmt.Sprintf("git status failed: %v", err)}
	}

	if len(strings.TrimSpace(string(out))) > 0 {
		return checkResult{Name: "git-diff", Passed: false, Detail: "uncommitted changes (agent did not commit)"}
	}

	return checkResult{Name: "git-diff", Passed: true, Detail: "clean"}
}

// checkTests runs make test or go test and checks exit code.
func checkTests(repoPath string) checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// try make test first
	if _, err := os.Stat(filepath.Join(repoPath, "Makefile")); err == nil {
		cmd := exec.CommandContext(ctx, "make", "test")
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			lastLine := lastNonEmptyLine(string(out))
			return checkResult{Name: "tests", Passed: false, Detail: fmt.Sprintf("make test failed: %s", lastLine)}
		}
		return checkResult{Name: "tests", Passed: true, Detail: "make test passed"}
	}

	// fallback to go test
	cmd := exec.CommandContext(ctx, "go", "test", "./...", "-count=1")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		lastLine := lastNonEmptyLine(string(out))
		return checkResult{Name: "tests", Passed: false, Detail: fmt.Sprintf("go test failed: %s", lastLine)}
	}
	return checkResult{Name: "tests", Passed: true, Detail: "go test passed"}
}

// checkLint runs make lint or golangci-lint and checks exit code.
func checkLint(repoPath string) checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// try make lint first
	if _, err := os.Stat(filepath.Join(repoPath, "Makefile")); err == nil {
		cmd := exec.CommandContext(ctx, "make", "lint")
		cmd.Dir = repoPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			lastLine := lastNonEmptyLine(string(out))
			return checkResult{Name: "lint", Passed: false, Detail: fmt.Sprintf("make lint failed: %s", lastLine)}
		}
		return checkResult{Name: "lint", Passed: true, Detail: "make lint passed"}
	}

	// fallback to golangci-lint
	cmd := exec.CommandContext(ctx, "golangci-lint", "run", "./...")
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		lastLine := lastNonEmptyLine(string(out))
		return checkResult{Name: "lint", Passed: false, Detail: fmt.Sprintf("lint failed: %s", lastLine)}
	}
	return checkResult{Name: "lint", Passed: true, Detail: "lint clean"}
}

// suggestGraylistCandidates prints advisory graylist add commands for runners
// that produced false positives (reported success but failed verification).
func suggestGraylistCandidates(results []verifyResult, report task.RunReport) {
	// collect runners that had false-positive tasks
	candidates := make(map[string]int) // runner → count of false positives
	for _, vr := range results {
		if vr.Status != "FAIL" {
			continue
		}
		// check if any check indicates a false positive (empty events or uncommitted changes)
		falsePositive := false
		for _, c := range vr.Checks {
			if !c.Passed && (c.Name == "events" || c.Name == "git-diff") {
				falsePositive = true
				break
			}
		}
		if !falsePositive {
			continue
		}
		res := report.Results[vr.TaskID]
		if res == nil || res.RunnerUsed == "" {
			continue
		}
		candidates[res.RunnerUsed]++
	}

	if len(candidates) == 0 {
		return
	}

	fmt.Println("\nGraylist candidates (reported success with 0 events or uncommitted changes):")
	for name, count := range candidates {
		fmt.Printf("  runforge graylist add %s --reason \"false positive: %d tasks in run %s\"\n",
			name, count, report.RunID)
	}
	fmt.Println("  Tip: use --model <model> to graylist a specific model instead of all models for that runner")
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			if len(line) > 80 {
				return line[:80] + "..."
			}
			return line
		}
	}
	return "(no output)"
}
