package scan

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScanOptions controls scanner behavior.
type ScanOptions struct {
	ReposDir     string
	FilterRepo   string
	MinSeverity  Severity
	Categories   []string
	ExcludeRepos []string
}

// ScanResult holds all findings from a scan.
type ScanResult struct {
	ReposScanned []string  `json:"repos_scanned"`
	Findings     []Finding `json:"findings"`
	Skipped      []string  `json:"skipped"`
}

// Scan walks ReposDir and runs all applicable checks on each repo.
func Scan(opts ScanOptions) (*ScanResult, error) {
	reposDir, err := filepath.Abs(opts.ReposDir)
	if err != nil {
		return nil, fmt.Errorf("resolve repos dir: %w", err)
	}

	entries, err := os.ReadDir(reposDir)
	if err != nil {
		return nil, fmt.Errorf("read repos dir: %w", err)
	}

	excludeSet := make(map[string]bool, len(opts.ExcludeRepos))
	for _, r := range opts.ExcludeRepos {
		excludeSet[r] = true
	}

	catSet := make(map[string]bool, len(opts.Categories))
	for _, c := range opts.Categories {
		catSet[c] = true
	}

	checkers := AllCheckers()

	result := &ScanResult{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		// skip hidden directories
		if strings.HasPrefix(name, ".") {
			continue
		}

		if opts.FilterRepo != "" && name != opts.FilterRepo {
			continue
		}

		if excludeSet[name] {
			slog.Debug("excluded repo", "repo", name)
			result.Skipped = append(result.Skipped, name)
			continue
		}

		repoPath := filepath.Join(reposDir, name)
		repo := DetectRepo(repoPath)
		if repo == nil {
			slog.Debug("not a git repo", "dir", name)
			result.Skipped = append(result.Skipped, name)
			continue
		}

		result.ReposScanned = append(result.ReposScanned, name)

		for _, checker := range checkers {
			if len(catSet) > 0 && !catSet[checker.Category()] {
				continue
			}
			if !checker.Applies(repo) {
				continue
			}
			findings := checker.Run(repo)
			for _, f := range findings {
				if opts.MinSeverity > 0 && f.Severity > opts.MinSeverity {
					continue
				}
				result.Findings = append(result.Findings, f)
			}
		}
	}

	// sort findings: critical first, then by repo
	sort.Slice(result.Findings, func(i, j int) bool {
		if result.Findings[i].Severity != result.Findings[j].Severity {
			return result.Findings[i].Severity < result.Findings[j].Severity
		}
		return result.Findings[i].Repo < result.Findings[j].Repo
	})

	return result, nil
}
