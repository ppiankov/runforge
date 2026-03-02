# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- `tokencontrol doctor` command — health check for runners, config, and dependencies
- `tokencontrol init` command — scaffold `.tokencontrol.yml` and task file with interactive setup
- `tokencontrol pr` command — create GitHub PRs from completed worktree tasks via `gh`
- Token usage tracking — per-task and aggregate token counts in run reports (codex, claude, opencode, qwen)
- Actionable cascade error messages — per-filter breakdown with fix suggestions when all runners eliminated
- CI/CD integration guide with example GitHub Actions workflows for nightly scan and PR verification
- `tokencontrol scan` command — portfolio auditor with 26 filesystem-based checks across 6 categories (structure, go, python, security, ci, quality)
- Scan output formats: text (human-readable with ANSI color), JSON (machine), tasks (tokencontrol task file)
- Scan-to-task pipeline: `--format tasks` generates agent-ready prompts with file paths, code patterns, verification commands, and constraints
- Multi-file glob support: `--tasks 'pattern*.json'` resolves, loads, and merges multiple task files
- `--tui` flag for run command: `full` (interactive Bubbletea TUI), `minimal` (live status), `off` (no live display), `auto` (detect TTY)
- `--idle-timeout` flag for killing tasks with no stdout activity
- Gemini CLI runner backend with headless mode
- OpenCode CLI runner backend with multi-provider model support
- Claude Code CLI runner backend with stream-json parsing
- Cline CLI runner backend with structural safety hooks
- Qwen Code CLI runner backend with model override support
- Runner fallback cascade with automatic provider failover on rate-limit or failure
- Runner blacklist with TTL for rate-limited providers (persists across runs)
- Runner graylist with model-aware false positive auto-detection (persists at `~/.tokencontrol/graylist.json`)
- Free-tier runner exclusion: `free: true` tag on profiles, `--allow-free` to opt in
- Pre-dispatch secret scanning via pastewatch-cli — unsafe runners excluded from cascade for repos with secrets
- Post-task auto-commit: commits changes agents leave unstaged (`--no-auto-commit` to disable)
- Task difficulty scoring: deterministic keyword + criteria scoring with tier-based cascade filtering
- Task state tracking: `.tokencontrol/state.json` prevents duplicate runs, `--retry` for failed tasks
- Worktree-based parallel execution: `--parallel-repo` enables git worktree isolation for same-repo tasks
- Configurable merge-back: `merge_back: true|false` with FF-only auto-merge (default: true)
- `tokencontrol watch` command for real-time monitoring of running sessions
- `tokencontrol verify` command for post-run proofchecking (false positive detection, test/lint verification)
- `tokencontrol validate` command for task file validation without execution
- `tokencontrol unlock` command for stale repo lock removal
- `tokencontrol graylist` command with list, add, remove, clear subcommands
- `tokencontrol state` command with list, reset, clear subcommands
- `tokencontrol sentinel loop` command for continuous autonomous scan-run cycles
- `tokencontrol ingest` command for importing external run results
- Mission Control TUI for sentinel mode (3-tab Bubbletea dashboard)
- Model pre-validation: auto-resolves mismatched OpenCode models before spawning
- Connectivity error detection (TLS/DNS/connection) with auto-blacklisting
- Process group management: all runners kill entire process tree on cancel
- Deterministic run IDs (SHA256 of timestamp + task files)
- Scan config in `.tokencontrol.yml`: `scan.exclude_repos` for skipping repos during scan

### Changed
- Renamed project from runforge to tokencontrol (module, binary, config files, run directory)
- License changed from MIT to BSL 1.1 (non-compete, 4-year change date to Apache 2.0)

### Fixed
- Auto-commit sets git identity so commits work in CI environments without global git config

## [0.1.0] - 2026-02-16

### Added
- Parallel task orchestration with dependency-aware scheduling (Kahn's algorithm)
- JSONL output parsing for failure detection (Codex exits 0 on failure)
- Runner interface for pluggable execution backends
- Codex runner backend (`codex exec --full-auto --json`)
- `--max-runtime` flag for per-task timeout (default: 30m)
- `--fail-fast` flag to stop spawning on first failure
- `--filter` flag for glob-based task selection
- `--dry-run` mode for execution plan preview
- `--verify` flag for per-repo `make test && make lint`
- `allowed_repos` field in task file for repo allowlisting
- JSON and text report output
- GoReleaser with Homebrew tap and Docker images
- CI and release GitHub Actions workflows

### Changed
- Renamed project from codexrun to tokencontrol
- Default task file: `tokencontrol.json`
- Run directory: `.tokencontrol/`
