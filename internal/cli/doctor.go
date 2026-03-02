package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/ppiankov/tokencontrol/internal/config"
	"github.com/ppiankov/tokencontrol/internal/runner"
	"github.com/spf13/cobra"
)

// execLookPath is the production LookPath implementation.
var execLookPath = exec.LookPath

// DoctorCheck is one health-check result.
type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// DoctorResult aggregates all checks and the overall status.
type DoctorResult struct {
	Status string        `json:"status"`
	Checks []DoctorCheck `json:"checks"`
}

// doctorEnv provides dependency injection for testability.
type doctorEnv struct {
	version    string
	lookPath   func(string) (string, error)
	getenv     func(string) string
	loadConfig func(string) (*config.Settings, error)
}

func newDoctorCmd() *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check environment health",
		Long:  "Validate runners, credentials, config, and dependencies.",
		RunE: func(cmd *cobra.Command, args []string) error {
			env := &doctorEnv{
				version:    Version,
				lookPath:   execLookPath,
				getenv:     os.Getenv,
				loadConfig: config.LoadSettings,
			}
			result := runDoctor(env, configFile)

			switch format {
			case "json":
				if err := formatDoctorJSON(cmd.OutOrStdout(), result); err != nil {
					return err
				}
			default:
				formatDoctorText(cmd.OutOrStdout(), result)
			}

			if result.Status == "error" {
				return fmt.Errorf("doctor found errors")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "output format: text, json")
	return cmd
}

func runDoctor(env *doctorEnv, configPath string) *DoctorResult {
	result := &DoctorResult{Status: "ok"}

	checks := []DoctorCheck{
		checkVersion(env),
	}

	// Runner binaries
	runners := []struct{ name, bin string }{
		{"codex", "codex"},
		{"claude", "claude"},
		{"gemini", "gemini"},
		{"opencode", "opencode"},
		{"cline", "cline"},
		{"qwen", "qwen"},
	}
	for _, r := range runners {
		checks = append(checks, checkRunner(env, r.name, r.bin))
	}

	// Credentials
	creds := []struct{ name, envVar string }{
		{"OPENAI_API_KEY", "OPENAI_API_KEY"},
		{"ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY"},
		{"GEMINI_API_KEY", "GEMINI_API_KEY"},
	}
	for _, c := range creds {
		checks = append(checks, checkCredential(env, c.name, c.envVar))
	}

	// Config
	checks = append(checks, checkConfig(env, configPath))

	// Graylist
	checks = append(checks, checkGraylist(runner.DefaultGraylistPath()))

	// Companions
	companions := []struct{ name, bin string }{
		{"pastewatch-cli", "pastewatch-cli"},
		{"gh", "gh"},
	}
	for _, c := range companions {
		checks = append(checks, checkCompanion(env, c.name, c.bin))
	}

	// Git (required)
	checks = append(checks, checkGit(env))

	// Aggregate status: error > warn > ok
	for _, c := range checks {
		switch c.Status {
		case "error":
			result.Status = "error"
		case "warn":
			if result.Status != "error" {
				result.Status = "warn"
			}
		}
	}
	result.Checks = checks
	return result
}

func checkVersion(env *doctorEnv) DoctorCheck {
	return DoctorCheck{Name: "tokencontrol-version", Status: "ok", Message: env.version}
}

func checkRunner(env *doctorEnv, name, binary string) DoctorCheck {
	checkName := "runner-" + name
	path, err := env.lookPath(binary)
	if err != nil {
		return DoctorCheck{Name: checkName, Status: "warn", Message: "not found in PATH"}
	}
	return DoctorCheck{Name: checkName, Status: "ok", Message: path}
}

func checkCredential(env *doctorEnv, name, envVar string) DoctorCheck {
	checkName := "cred-" + strings.ToLower(strings.ReplaceAll(name, "_", "-"))
	val := env.getenv(envVar)
	if val == "" {
		return DoctorCheck{Name: checkName, Status: "warn", Message: "not set"}
	}
	return DoctorCheck{Name: checkName, Status: "ok", Message: "set"}
}

func checkConfig(env *doctorEnv, path string) DoctorCheck {
	settings, err := env.loadConfig(path)
	if err != nil {
		return DoctorCheck{Name: "config", Status: "warn", Message: err.Error()}
	}
	runners := len(settings.Runners)
	fallbacks := len(settings.DefaultFallbacks)
	if runners == 0 && settings.DefaultRunner == "" {
		return DoctorCheck{Name: "config", Status: "ok", Message: "no config (using defaults)"}
	}
	return DoctorCheck{
		Name:    "config",
		Status:  "ok",
		Message: fmt.Sprintf("valid (%d runners, %d fallbacks)", runners, fallbacks),
	}
}

func checkGraylist(path string) DoctorCheck {
	gl := runner.LoadGraylist(path)
	entries := gl.Entries()
	n := len(entries)
	if n == 0 {
		return DoctorCheck{Name: "graylist", Status: "ok", Message: "empty"}
	}
	noun := "entries"
	if n == 1 {
		noun = "entry"
	}
	return DoctorCheck{Name: "graylist", Status: "ok", Message: fmt.Sprintf("%d %s", n, noun)}
}

func checkCompanion(env *doctorEnv, name, binary string) DoctorCheck {
	checkName := "companion-" + name
	path, err := env.lookPath(binary)
	if err != nil {
		return DoctorCheck{Name: checkName, Status: "warn", Message: "not found in PATH"}
	}
	return DoctorCheck{Name: checkName, Status: "ok", Message: path}
}

func checkGit(env *doctorEnv) DoctorCheck {
	path, err := env.lookPath("git")
	if err != nil {
		return DoctorCheck{Name: "git", Status: "error", Message: "not found in PATH (required)"}
	}
	return DoctorCheck{Name: "git", Status: "ok", Message: path}
}

const doctorLabelWidth = 35

func formatDoctorText(w io.Writer, result *DoctorResult) {
	for _, c := range result.Checks {
		label := formatLabel(c.Name)
		dots := doctorLabelWidth - len(label)
		if dots < 3 {
			dots = 3
		}
		status := strings.ToUpper(c.Status)
		fmt.Fprintf(w, "  %s %s %s  %s\n", label, strings.Repeat(".", dots), status, c.Message)
	}
	fmt.Fprintf(w, "\n  Result: %s\n", strings.ToUpper(result.Status))
}

func formatDoctorJSON(w io.Writer, result *DoctorResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// formatLabel converts check names to human-readable labels.
func formatLabel(name string) string {
	switch {
	case name == "tokencontrol-version":
		return "tokencontrol version"
	case strings.HasPrefix(name, "runner-"):
		return "Runner: " + strings.TrimPrefix(name, "runner-")
	case strings.HasPrefix(name, "cred-"):
		return credLabel(strings.TrimPrefix(name, "cred-"))
	case name == "config":
		return "Config (.tokencontrol.yml)"
	case name == "graylist":
		return "Graylist"
	case strings.HasPrefix(name, "companion-"):
		return "Companion: " + strings.TrimPrefix(name, "companion-")
	case name == "git":
		return "Git"
	default:
		return name
	}
}

// credLabel maps cred check names back to env var style.
func credLabel(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}
