package runner

import (
	"os"
	"strings"
)

// sensitiveEnvPrefixes are env var name prefixes stripped from subprocess
// environments. Prevents credential exfiltration via shell builtins or
// agent tool calls that dump environment.
var sensitiveEnvPrefixes = []string{
	"NULLBOT_",
	"GROQ_API",
	"OPENAI_API",
	"ANTHROPIC_API",
	"CHAINWATCH_",
	"RUNFORGE_",
	"AWS_SECRET",
	"AWS_SESSION",
	"GITHUB_TOKEN",
}

// sensitiveEnvExact are env var names stripped by exact match.
var sensitiveEnvExact = []string{
	"API_KEY",
	"API_SECRET",
	"SECRET_KEY",
}

// SanitizedEnv returns os.Environ() with sensitive variables removed.
// All runners must use this instead of os.Environ() directly.
func SanitizedEnv() []string {
	return sanitizeEnv(os.Environ())
}

// sanitizeEnv filters sensitive environment variables from the list.
func sanitizeEnv(environ []string) []string {
	clean := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			clean = append(clean, entry)
			continue
		}
		upper := strings.ToUpper(name)
		skip := false
		for _, prefix := range sensitiveEnvPrefixes {
			if strings.HasPrefix(upper, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			for _, exact := range sensitiveEnvExact {
				if upper == exact {
					skip = true
					break
				}
			}
		}
		if !skip {
			clean = append(clean, entry)
		}
	}
	return clean
}
