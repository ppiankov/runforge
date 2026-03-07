package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ppiankov/tokencontrol/internal/task"
)

const (
	defaultQuotaSafetyFactor    = 1.30
	defaultQuotaLookbackRuns    = 20
	defaultScoreTokenUnit       = 300_000
	minimumTaskTokenEstimate    = 600_000
	defaultSimpleTokenEstimate  = 1_800_000
	defaultMediumTokenEstimate  = 3_000_000
	defaultComplexTokenEstimate = 5_000_000
	defaultUnknownTokenEstimate = 2_200_000
)

type quotaPreflightConfig struct {
	RemainingTokens int
	ReserveTokens   int
	SafetyFactor    float64
	Enforce         bool
	LookbackRuns    int
}

func (c quotaPreflightConfig) enabled() bool {
	return c.RemainingTokens > 0
}

type quotaPreflightResult struct {
	CodexTasks      int
	HistoricalTasks int
	HeuristicTasks  int
	EstimatedTokens int
	RequiredTokens  int
	AvailableTokens int
	RemainingTokens int
	ReserveTokens   int
	ShortfallTokens int
	SafetyFactor    float64
	Enforce         bool
}

func runCodexQuotaPreflight(
	tasks []task.Task,
	profiles map[string]*task.RunnerProfileConfig,
	cfg quotaPreflightConfig,
	historyDir string,
) (*quotaPreflightResult, error) {
	if !cfg.enabled() {
		return nil, nil
	}

	cfg = normalizeQuotaPreflightConfig(cfg)
	codexTasks := collectPrimaryCodexTasks(tasks, profiles)
	if len(codexTasks) == 0 {
		return &quotaPreflightResult{
			CodexTasks:      0,
			AvailableTokens: maxInt(cfg.RemainingTokens-cfg.ReserveTokens, 0),
			RemainingTokens: cfg.RemainingTokens,
			ReserveTokens:   cfg.ReserveTokens,
			SafetyFactor:    cfg.SafetyFactor,
			Enforce:         cfg.Enforce,
		}, nil
	}

	historical := loadHistoricalCodexUsage(historyDir, cfg.LookbackRuns, profiles)

	totalEstimate := 0
	historyHits := 0
	for _, t := range codexTasks {
		estimate, usedHistory := estimateCodexTaskTokens(t, historical)
		totalEstimate += estimate
		if usedHistory {
			historyHits++
		}
	}

	required := int(math.Ceil(float64(totalEstimate) * cfg.SafetyFactor))
	available := maxInt(cfg.RemainingTokens-cfg.ReserveTokens, 0)
	shortfall := maxInt(required-available, 0)

	result := &quotaPreflightResult{
		CodexTasks:      len(codexTasks),
		HistoricalTasks: historyHits,
		HeuristicTasks:  len(codexTasks) - historyHits,
		EstimatedTokens: totalEstimate,
		RequiredTokens:  required,
		AvailableTokens: available,
		RemainingTokens: cfg.RemainingTokens,
		ReserveTokens:   cfg.ReserveTokens,
		ShortfallTokens: shortfall,
		SafetyFactor:    cfg.SafetyFactor,
		Enforce:         cfg.Enforce,
	}

	if shortfall > 0 && cfg.Enforce {
		return result, fmt.Errorf(
			"codex quota preflight failed: need %s for %d codex tasks (estimate %s × %.2f safety), available %s (remaining %s, reserve %s), shortfall %s",
			formatTokenCount(required),
			len(codexTasks),
			formatTokenCount(totalEstimate),
			cfg.SafetyFactor,
			formatTokenCount(available),
			formatTokenCount(cfg.RemainingTokens),
			formatTokenCount(cfg.ReserveTokens),
			formatTokenCount(shortfall),
		)
	}

	return result, nil
}

func normalizeQuotaPreflightConfig(cfg quotaPreflightConfig) quotaPreflightConfig {
	if cfg.ReserveTokens < 0 {
		cfg.ReserveTokens = 0
	}
	if cfg.SafetyFactor <= 0 {
		cfg.SafetyFactor = defaultQuotaSafetyFactor
	}
	if cfg.LookbackRuns <= 0 {
		cfg.LookbackRuns = defaultQuotaLookbackRuns
	}
	return cfg
}

func collectPrimaryCodexTasks(tasks []task.Task, profiles map[string]*task.RunnerProfileConfig) []task.Task {
	out := make([]task.Task, 0, len(tasks))
	for _, t := range tasks {
		if isCodexRunner(t.Runner, profiles) {
			out = append(out, t)
		}
	}
	return out
}

func isCodexRunner(name string, profiles map[string]*task.RunnerProfileConfig) bool {
	if strings.EqualFold(name, "codex") {
		return true
	}
	if p, ok := profiles[name]; ok {
		return strings.EqualFold(p.Type, "codex")
	}
	return strings.Contains(strings.ToLower(name), "codex")
}

func estimateCodexTaskTokens(t task.Task, historical map[string]int) (int, bool) {
	if tokens := historical[t.ID]; tokens > 0 {
		return tokens, true
	}
	return heuristicCodexTokens(t), false
}

func heuristicCodexTokens(t task.Task) int {
	estimate := 0
	if t.Score > 0 {
		estimate = t.Score * defaultScoreTokenUnit
	} else {
		switch strings.ToLower(strings.TrimSpace(t.Difficulty)) {
		case "simple":
			estimate = defaultSimpleTokenEstimate
		case "medium":
			estimate = defaultMediumTokenEstimate
		case "complex":
			estimate = defaultComplexTokenEstimate
		default:
			estimate = defaultUnknownTokenEstimate
		}
	}
	return maxInt(estimate, minimumTaskTokenEstimate)
}

func loadHistoricalCodexUsage(
	historyDir string,
	lookbackRuns int,
	profiles map[string]*task.RunnerProfileConfig,
) map[string]int {
	usage := make(map[string]int)

	entries, err := os.ReadDir(historyDir)
	if err != nil {
		return usage
	}

	runDirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			runDirs = append(runDirs, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(runDirs)))

	seenRuns := 0
	for _, name := range runDirs {
		if seenRuns >= lookbackRuns {
			break
		}
		reportPath := filepath.Join(historyDir, name, "report.json")
		data, err := os.ReadFile(reportPath)
		if err != nil {
			continue
		}

		var report task.RunReport
		if err := json.Unmarshal(data, &report); err != nil {
			continue
		}
		seenRuns++

		for taskID, result := range report.Results {
			if result == nil || result.TokensUsed == nil || result.TokensUsed.TotalTokens <= 0 {
				continue
			}
			if !isCodexRunner(result.RunnerUsed, profiles) {
				continue
			}
			if _, exists := usage[taskID]; exists {
				continue
			}
			usage[taskID] = result.TokensUsed.TotalTokens
		}
	}

	return usage
}

func formatTokenCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM tokens", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK tokens", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d tokens", n)
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
