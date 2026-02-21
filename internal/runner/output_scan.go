package runner

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// secretPatterns match known API key and token formats in command output.
// These detect actual credential values, not variable names.
var secretPatterns = []*regexp.Regexp{
	// Groq keys: gsk_...
	regexp.MustCompile(`gsk_[a-zA-Z0-9]{20,}`),
	// OpenAI keys: sk-... (including sk-proj-... format)
	regexp.MustCompile(`sk-[a-zA-Z0-9\-]{20,}`),
	// Anthropic keys: sk-ant-...
	regexp.MustCompile(`sk-ant-[a-zA-Z0-9\-]{20,}`),
	// Generic long hex tokens (64+ chars) that look like API keys
	regexp.MustCompile(`\b[a-f0-9]{64,}\b`),
	// Bearer tokens
	regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9\-_.]{20,}`),
	// GitHub tokens
	regexp.MustCompile(`ghp_[a-zA-Z0-9]{36,}`),
	regexp.MustCompile(`gho_[a-zA-Z0-9]{36,}`),
	regexp.MustCompile(`github_pat_[a-zA-Z0-9_]{22,}`),
	// AWS keys
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
}

const redactPlaceholder = "[REDACTED]"

// ScanOutput checks text for leaked secrets and returns a redacted copy.
// The second return value is the number of secrets found.
func ScanOutput(output string) (string, int) {
	count := 0
	result := output
	for _, re := range secretPatterns {
		matches := re.FindAllString(result, -1)
		if len(matches) > 0 {
			count += len(matches)
			result = re.ReplaceAllString(result, redactPlaceholder)
		}
	}
	return result, count
}

// envKeyValuePattern matches KEY=VALUE lines where KEY is a known
// sensitive env var name. Catches output from set, export -p, declare -p.
var envKeyValuePattern = regexp.MustCompile(
	`(?im)^(?:declare -x |export )?` +
		`(NULLBOT_\w*|GROQ_\w*|OPENAI_\w*|ANTHROPIC_\w*|API_KEY|API_SECRET|CHAINWATCH_\w*|RUNFORGE_\w*|AWS_SECRET\w*|GITHUB_TOKEN)` +
		`[= ].*$`,
)

// ScanOutputFull runs both secret pattern scanning and env key=value scanning.
func ScanOutputFull(output string) (string, int) {
	result, count := ScanOutput(output)

	envMatches := envKeyValuePattern.FindAllString(result, -1)
	if len(envMatches) > 0 {
		count += len(envMatches)
		result = envKeyValuePattern.ReplaceAllString(result, redactPlaceholder)
	}

	// Collapse consecutive redacted lines.
	for strings.Contains(result, redactPlaceholder+"\n"+redactPlaceholder) {
		result = strings.ReplaceAll(result, redactPlaceholder+"\n"+redactPlaceholder, redactPlaceholder)
	}

	return result, count
}

// ScanOutputDir scans all output files in a directory and redacts secrets
// in place. Returns total number of secrets found across all files.
func ScanOutputDir(dir string) int {
	totalLeaks := 0

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Scan text output files only.
		if !strings.HasSuffix(name, ".log") &&
			!strings.HasSuffix(name, ".jsonl") &&
			!strings.HasSuffix(name, ".md") {
			continue
		}

		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		redacted, count := ScanOutputFull(string(data))
		if count > 0 {
			totalLeaks += count
			slog.Warn("output scan: secrets redacted",
				"file", path, "count", count)
			_ = os.WriteFile(path, []byte(redacted), 0o600)
		}
	}

	return totalLeaks
}
