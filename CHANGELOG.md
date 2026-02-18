# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- `runforge scan` command — portfolio auditor with 26 filesystem-based checks across 6 categories (structure, go, python, security, ci, quality)
- Scan output formats: text (human-readable with ANSI color), JSON (machine), tasks (runforge task file)
- Scan-to-task pipeline: `--format tasks` generates agent-ready prompts with file paths, code patterns, verification commands, and constraints
- `Finding.Prompt` field with `TaskPrompt()` helper for dual prompt system (short suggestion vs detailed autonomous prompt)
- `promptBuilder` for composing structured multi-line agent prompts with verification and constraints
- Language-aware prompt generation (Go, Python, multi) at check time via `promptFn` closures
- Multi-file glob support: `--tasks 'pattern*.json'` resolves, loads, and merges multiple task files
- `--tui` flag for run command: `full` (interactive Bubbletea TUI), `minimal` (live status), `off` (no live display), `auto` (detect TTY)
- `--idle-timeout` flag for killing tasks with no stdout activity
- Orphaned task file detection (`quality-orphaned-tasks` check) for `runforge-*.json` files left in repos
- Scan config in `.runforge.yml`: `scan.exclude_repos` for skipping repos during scan
- Gemini CLI runner backend (`gemini --approval-mode=yolo --output-format stream-json`)
- OpenCode CLI runner backend (`opencode run --format json`) with multi-provider model support

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
- Renamed project from codexrun to runforge
- Default task file: `runforge.json`
- Run directory: `.runforge/`
