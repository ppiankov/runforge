package runner

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// VerifyResult holds the outcome of a per-repo verification.
type VerifyResult struct {
	Repo     string        `json:"repo"`
	RepoDir  string        `json:"repo_dir"`
	Passed   bool          `json:"passed"`
	TestOut  string        `json:"test_output,omitempty"`
	LintOut  string        `json:"lint_output,omitempty"`
	Error    string        `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// Verify runs make test && make lint in the given repo directory.
func Verify(ctx context.Context, repo, repoDir, runDir string) *VerifyResult {
	start := time.Now()
	result := &VerifyResult{
		Repo:    repo,
		RepoDir: repoDir,
	}

	logDir := filepath.Join(runDir, "verify")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		slog.Warn("cannot create verify log dir", "path", logDir, "error", err)
	}

	slog.Info("verifying repo", "repo", repo, "dir", repoDir)

	// run make test
	testOut, testErr := runMake(ctx, repoDir, "test")
	result.TestOut = testOut

	// save test output
	testLog := filepath.Join(logDir, repoName(repo)+"-test.log")
	_ = os.WriteFile(testLog, []byte(testOut), 0o644)

	if testErr != nil {
		result.Error = fmt.Sprintf("make test failed: %v", testErr)
		result.Duration = time.Since(start)
		return result
	}

	// run make lint
	lintOut, lintErr := runMake(ctx, repoDir, "lint")
	result.LintOut = lintOut

	// save lint output
	lintLog := filepath.Join(logDir, repoName(repo)+"-lint.log")
	_ = os.WriteFile(lintLog, []byte(lintOut), 0o644)

	if lintErr != nil {
		result.Error = fmt.Sprintf("make lint failed: %v", lintErr)
		result.Duration = time.Since(start)
		return result
	}

	result.Passed = true
	result.Duration = time.Since(start)
	return result
}

func runMake(ctx context.Context, dir, target string) (string, error) {
	cmd := exec.CommandContext(ctx, "make", target)
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	return out.String(), err
}

// repoName extracts the repo name from "owner/name".
func repoName(repo string) string {
	for i := len(repo) - 1; i >= 0; i-- {
		if repo[i] == '/' {
			return repo[i+1:]
		}
	}
	return repo
}
