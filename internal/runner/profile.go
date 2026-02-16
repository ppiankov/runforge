package runner

import (
	"fmt"
	"os"
	"strings"
)

// ResolveEnv resolves "env:VAR_NAME" references in an env map to actual values.
// Returns error if a referenced env var is empty or unset.
func ResolveEnv(env map[string]string) (map[string]string, error) {
	if len(env) == 0 {
		return nil, nil
	}
	resolved := make(map[string]string, len(env))
	for k, v := range env {
		if strings.HasPrefix(v, "env:") {
			envKey := strings.TrimPrefix(v, "env:")
			envVal := os.Getenv(envKey)
			if envVal == "" {
				return nil, fmt.Errorf("env var %q (referenced by %q) is not set", envKey, k)
			}
			resolved[k] = envVal
		} else {
			resolved[k] = v
		}
	}
	return resolved, nil
}

// MapToEnvSlice converts a map of env vars to a slice of "K=V" strings.
func MapToEnvSlice(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	s := make([]string, 0, len(env))
	for k, v := range env {
		s = append(s, k+"="+v)
	}
	return s
}
