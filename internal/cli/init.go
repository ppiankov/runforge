package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// initEnv provides dependency injection for testability.
type initEnv struct {
	lookPath  func(string) (string, error)
	getenv    func(string) string
	numCPU    func() int
	stat      func(string) (os.FileInfo, error)
	writeFile func(string, []byte, os.FileMode) error
}

// initResult holds the probed environment summary.
type initResult struct {
	Runners     []string
	Credentials map[string]bool // env var name → set
	Workers     int
	CPUs        int
	ReposDir    string
	ConfigPath  string
	Written     bool
	DryRun      bool
}

func newInitCmd() *cobra.Command {
	var (
		force    bool
		reposDir string
		dryRun   bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize tokencontrol configuration",
		Long:  "Probe the environment for available runners and credentials, then generate a .tokencontrol.yml config file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			env := &initEnv{
				lookPath:  execLookPath,
				getenv:    os.Getenv,
				numCPU:    runtime.NumCPU,
				stat:      os.Stat,
				writeFile: os.WriteFile,
			}
			return runInit(env, cmd.ErrOrStderr(), cmd.OutOrStdout(), configFile, reposDir, force, dryRun)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config file")
	cmd.Flags().StringVar(&reposDir, "repos-dir", ".", "base directory containing repos")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print generated config to stdout without writing")
	return cmd
}

// runnerBinaries is the ordered list of runners to probe.
var runnerBinaries = []string{"codex", "claude", "gemini", "opencode", "cline", "qwen"}

// credentialVars is the ordered list of credential env vars to check.
var credentialVars = []string{
	"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY",
	"DEEPSEEK_API_KEY", "GROQ_API_KEY", "MINIMAX_API_KEY", "ZAI_API_KEY",
}

func runInit(env *initEnv, stderr io.Writer, stdout io.Writer, cfgPath, reposDir string, force, dryRun bool) error {
	// Check if config already exists (unless --force or --dry-run).
	if !force && !dryRun {
		if _, err := env.stat(cfgPath); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", cfgPath)
		}
	}

	// Probe runners.
	runners := probeRunners(env)

	// Probe credentials.
	creds := probeCredentials(env)

	// Compute workers.
	cpus := env.numCPU()
	workers := clampWorkers(cpus)

	// Build config.
	cfg := buildInitConfig(runners, reposDir, workers)

	// Marshal to YAML.
	data, err := marshalConfig(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	result := &initResult{
		Runners:     runners,
		Credentials: creds,
		Workers:     workers,
		CPUs:        cpus,
		ReposDir:    reposDir,
		ConfigPath:  cfgPath,
		DryRun:      dryRun,
	}

	if dryRun {
		fmt.Fprint(stdout, string(data))
	} else {
		if err := env.writeFile(cfgPath, data, 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		result.Written = true
	}

	printInitSummary(stderr, result)
	return nil
}

func probeRunners(env *initEnv) []string {
	var found []string
	for _, bin := range runnerBinaries {
		if _, err := env.lookPath(bin); err == nil {
			found = append(found, bin)
		}
	}
	return found
}

func probeCredentials(env *initEnv) map[string]bool {
	creds := make(map[string]bool, len(credentialVars))
	for _, v := range credentialVars {
		creds[v] = env.getenv(v) != ""
	}
	return creds
}

func clampWorkers(cpus int) int {
	w := cpus / 2
	if w < 2 {
		w = 2
	}
	if w > 8 {
		w = 8
	}
	return w
}

// initConfig is the minimal output struct for generated YAML.
// Only includes fields that init sets — no zero-value noise.
type initConfig struct {
	ReposDir         string                        `yaml:"repos_dir"`
	Workers          int                           `yaml:"workers"`
	FailFast         bool                          `yaml:"fail_fast"`
	MaxRuntime       string                        `yaml:"max_runtime"`
	DefaultRunner    string                        `yaml:"default_runner"`
	DefaultFallbacks []string                      `yaml:"default_fallbacks,omitempty"`
	Runners          map[string]*initRunnerProfile `yaml:"runners"`
}

type initRunnerProfile struct {
	Type         string `yaml:"type"`
	FallbackOnly bool   `yaml:"fallback_only,omitempty"`
}

func buildInitConfig(runners []string, reposDir string, workers int) *initConfig {
	cfg := &initConfig{
		ReposDir:   reposDir,
		Workers:    workers,
		FailFast:   true,
		MaxRuntime: "30m",
		Runners:    make(map[string]*initRunnerProfile),
	}

	if len(runners) == 0 {
		cfg.DefaultRunner = "script"
		cfg.Runners["script"] = &initRunnerProfile{Type: "script"}
		return cfg
	}

	cfg.DefaultRunner = runners[0]
	for _, r := range runners {
		profile := &initRunnerProfile{Type: r}
		// claude code runs on Opus — expensive, use as fallback only
		if r == "claude" {
			profile.FallbackOnly = true
		}
		cfg.Runners[r] = profile
	}
	if len(runners) > 1 {
		cfg.DefaultFallbacks = runners[1:]
	}
	return cfg
}

func marshalConfig(cfg *initConfig) ([]byte, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	header := "# tokencontrol configuration\n# Generated by: tokencontrol init\n# Docs: https://github.com/ppiankov/tokencontrol\n\n"
	return append([]byte(header), data...), nil
}

func printInitSummary(w io.Writer, r *initResult) {
	if len(r.Runners) > 0 {
		fmt.Fprintf(w, "Detected runners: %s\n", strings.Join(r.Runners, ", "))
	} else {
		fmt.Fprintln(w, "No runners detected (using script as default)")
	}

	// Credentials.
	var parts []string
	for _, v := range credentialVars {
		mark := "not set"
		if r.Credentials[v] {
			mark = "set"
		}
		parts = append(parts, fmt.Sprintf("%s %s", v, mark))
	}
	fmt.Fprintf(w, "Credentials: %s\n", strings.Join(parts, ", "))

	fmt.Fprintf(w, "Workers: %d (based on %d CPUs)\n", r.Workers, r.CPUs)

	if r.DryRun {
		fmt.Fprintln(w, "Dry run — no file written")
	} else if r.Written {
		fmt.Fprintf(w, "Config written to %s\n", r.ConfigPath)
	}
}
