package scan

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ppiankov/runforge/internal/task"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorGreen  = "\033[32m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
)

// --- Text Formatter ---

// TextFormatter writes human-readable scan output.
type TextFormatter struct {
	color bool
}

// NewTextFormatter creates a text formatter with optional ANSI color.
func NewTextFormatter(color bool) *TextFormatter {
	return &TextFormatter{color: color}
}

func (f *TextFormatter) Format(w io.Writer, result *ScanResult) error {
	critCount, warnCount, infoCount := countSeverities(result.Findings)

	fmt.Fprintf(w, "%srunforge scan%s — %d repos, %d findings\n\n",
		f.c(colorBold), f.c(colorReset), len(result.ReposScanned),
		len(result.Findings))

	// group by repo
	byRepo := groupByRepo(result.Findings)
	repos := sortedKeys(byRepo)

	for _, repo := range repos {
		findings := byRepo[repo]
		rc, rw, ri := countSeverities(findings)

		parts := []string{}
		if rc > 0 {
			parts = append(parts, fmt.Sprintf("%s%d critical%s", f.c(colorRed), rc, f.c(colorReset)))
		}
		if rw > 0 {
			parts = append(parts, fmt.Sprintf("%s%d warning%s", f.c(colorYellow), rw, f.c(colorReset)))
		}
		if ri > 0 {
			parts = append(parts, fmt.Sprintf("%s%d info%s", f.c(colorDim), ri, f.c(colorReset)))
		}

		fmt.Fprintf(w, "%s%s%s (%d findings: %s)\n", f.c(colorBold), repo, f.c(colorReset),
			len(findings), strings.Join(parts, ", "))

		for _, finding := range findings {
			sevLabel := f.severityLabel(finding.Severity)
			fmt.Fprintf(w, "  %s  %-25s %s\n", sevLabel, finding.Check, finding.Message)
		}
		fmt.Fprintln(w)
	}

	// summary
	fmt.Fprintf(w, "Summary: %d repos scanned", len(result.ReposScanned))
	if critCount > 0 {
		fmt.Fprintf(w, ", %s%d critical%s", f.c(colorRed), critCount, f.c(colorReset))
	}
	if warnCount > 0 {
		fmt.Fprintf(w, ", %s%d warning%s", f.c(colorYellow), warnCount, f.c(colorReset))
	}
	if infoCount > 0 {
		fmt.Fprintf(w, ", %s%d info%s", f.c(colorDim), infoCount, f.c(colorReset))
	}
	if len(result.Skipped) > 0 {
		fmt.Fprintf(w, ", %d skipped", len(result.Skipped))
	}
	fmt.Fprintln(w)

	return nil
}

func (f *TextFormatter) c(code string) string {
	if !f.color {
		return ""
	}
	return code
}

func (f *TextFormatter) severityLabel(s Severity) string {
	switch s {
	case SeverityCritical:
		return fmt.Sprintf("%sCRITICAL%s", f.c(colorRed), f.c(colorReset))
	case SeverityWarning:
		return fmt.Sprintf("%sWARNING %s", f.c(colorYellow), f.c(colorReset))
	case SeverityInfo:
		return fmt.Sprintf("%sinfo    %s", f.c(colorDim), f.c(colorReset))
	default:
		return "unknown "
	}
}

// --- JSON Formatter ---

// JSONFormatter writes scan results as JSON.
type JSONFormatter struct{}

func NewJSONFormatter() *JSONFormatter { return &JSONFormatter{} }

func (f *JSONFormatter) Format(w io.Writer, result *ScanResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// --- Task Formatter ---

// TaskFormatter generates a runforge task JSON file from findings.
type TaskFormatter struct {
	owner         string
	defaultRunner string
}

func NewTaskFormatter(owner, defaultRunner string) *TaskFormatter {
	return &TaskFormatter{owner: owner, defaultRunner: defaultRunner}
}

func (f *TaskFormatter) Format(w io.Writer, result *ScanResult) error {
	tf := task.TaskFile{
		Description: "Auto-generated from runforge scan",
	}
	if f.defaultRunner != "" {
		tf.DefaultRunner = f.defaultRunner
	}

	for _, finding := range result.Findings {
		// skip info-level findings for task generation
		if finding.Severity == SeverityInfo {
			continue
		}

		owner := f.owner
		if owner == "" {
			owner = "ppiankov"
		}

		priority := 3
		switch finding.Severity {
		case SeverityCritical:
			priority = 1
		case SeverityWarning:
			priority = 2
		}

		t := task.Task{
			ID:       fmt.Sprintf("%s-scan-%s", finding.Repo, finding.Check),
			Repo:     fmt.Sprintf("%s/%s", owner, finding.Repo),
			Priority: priority,
			Title:    finding.Message,
			Prompt:   finding.TaskPrompt(),
		}
		tf.Tasks = append(tf.Tasks, t)
	}

	if len(tf.Tasks) == 0 {
		fmt.Fprintln(w, "No actionable findings (critical/warning) to generate tasks for.")
		return nil
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(tf)
}

// --- helpers ---

func countSeverities(findings []Finding) (crit, warn, info int) {
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			crit++
		case SeverityWarning:
			warn++
		case SeverityInfo:
			info++
		}
	}
	return
}

func groupByRepo(findings []Finding) map[string][]Finding {
	m := make(map[string][]Finding)
	for _, f := range findings {
		m[f.Repo] = append(m[f.Repo], f)
	}
	return m
}

func sortedKeys(m map[string][]Finding) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
