# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

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
