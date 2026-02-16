[![CI](https://github.com/ppiankov/runforge/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/runforge/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

# runforge

Dependency-aware parallel task runner for AI coding agents. Reads a task spec, builds a dependency DAG, spawns parallel processes, detects failures from JSONL output, and reports results.

## What This Is

- Reads a JSON task file with dependency declarations
- Builds a directed acyclic graph and executes tasks in topological order
- Runs multiple tasks in parallel with configurable worker count
- Detects Codex failures from JSONL event stream (exit code 0 is unreliable)
- Skips all transitive dependents on failure — no wasted compute
- Per-repo verification via `make test && make lint`
- Produces JSON reports for post-run analysis

## What This Is NOT

- Not a CI/CD system — no retries, no webhooks, no persistence between runs
- Not a general-purpose task runner — designed for AI agent orchestration
- Not a process supervisor — runs once, exits when done
- Not a build system — delegates builds to Makefiles in target repos
- Does not provide its own AI — orchestrates external runners (Codex, etc.)

## Quick Start

### Install

```bash
# Homebrew
brew install ppiankov/tap/runforge

# Go install
go install github.com/ppiankov/runforge/cmd/runforge@latest

# Binary download
# See https://github.com/ppiankov/runforge/releases
```

### Run

```bash
# Show execution plan
runforge run --dry-run --tasks runforge.json --repos-dir ~/dev/repos

# Execute with 4 parallel workers
runforge run --tasks runforge.json --repos-dir ~/dev/repos --workers 4

# Execute with verification
runforge run --tasks runforge.json --repos-dir ~/dev/repos --verify

# Filter to specific tasks
runforge run --tasks runforge.json --repos-dir ~/dev/repos --filter "myproject-*"

# Fail fast on first error
runforge run --tasks runforge.json --repos-dir ~/dev/repos --fail-fast
```

## Usage

### `runforge run`

Execute tasks with dependency-aware parallelism.

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks FILE` | `runforge.json` | Path to tasks JSON file |
| `--workers N` | `4` | Max parallel runner processes |
| `--repos-dir DIR` | `.` | Base directory containing repos |
| `--filter PATTERN` | | Only run tasks matching ID glob |
| `--dry-run` | `false` | Show execution plan without running |
| `--verify` | `false` | Run `make test && make lint` per repo after completion |
| `--max-runtime DUR` | `30m` | Per-task timeout duration |
| `--fail-fast` | `false` | Stop spawning new tasks on first failure |
| `--verbose` | `false` | Enable debug logging |

### `runforge status`

Inspect results of a completed run.

```bash
runforge status --run-dir .runforge/20260216-143000
```

### `runforge verify`

Run `make test && make lint` for repos in a completed run.

```bash
runforge verify --run-dir .runforge/20260216-143000 --repos-dir ~/dev/repos
```

### `runforge version`

Print version information.

## Task Spec

The task file is a JSON document describing work orders with optional dependencies.

```json
{
  "description": "Sprint 42 work orders",
  "allowed_repos": ["org/repo1", "org/repo2"],
  "tasks": [
    {
      "id": "repo1-WO01",
      "repo": "org/repo1",
      "priority": 1,
      "title": "Add unit tests",
      "prompt": "Add unit tests for all internal packages...",
      "runner": "codex"
    },
    {
      "id": "repo1-WO02",
      "repo": "org/repo1",
      "priority": 1,
      "depends_on": "repo1-WO01",
      "title": "Add integration tests",
      "prompt": "Add integration tests that depend on..."
    }
  ]
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `id` | Yes | Unique task identifier |
| `repo` | Yes | Repository in `owner/name` format |
| `priority` | Yes | Execution priority (lower = first) |
| `title` | Yes | Short description |
| `prompt` | Yes | Full prompt for the AI agent |
| `depends_on` | No | ID of task that must complete first |
| `runner` | No | Runner backend (default: `codex`) |
| `allowed_repos` | No | Top-level repo allowlist for safety |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | All tasks completed successfully |
| `1` | One or more tasks failed |
| `2` | Configuration error (invalid task file, missing repo, cycle) |

## Failure Model

Codex can exit with code 0 even when a turn fails. runforge detects failure by parsing the JSONL event stream for `turn.failed` events. When a task fails:

1. All transitive dependents are marked SKIPPED
2. Independent tasks continue executing
3. With `--fail-fast`, no new tasks are spawned (running tasks finish)

## Architecture

```
cmd/runforge/main.go        -- CLI entry point (minimal)
internal/
  cli/                      -- Cobra commands (run, status, verify, version)
  config/                   -- Task file parsing and validation
  task/
    model.go                -- Task, TaskResult, RunReport types
    graph.go                -- Dependency DAG, topological sort (Kahn's algorithm)
    scheduler.go            -- Worker pool with dependency-aware scheduling
  runner/
    runner.go               -- Runner interface
    codex.go                -- Codex exec backend (JSONL parsing)
    verifier.go             -- Per-repo make test/lint
  reporter/
    text.go                 -- Terminal output (ANSI when TTY)
    json.go                 -- JSON report writer
```

## Building from Source

```bash
git clone https://github.com/ppiankov/runforge.git
cd runforge
make build    # produces bin/runforge
make test     # run tests with -race
make lint     # golangci-lint
```

## Known Limitations

- Single `depends_on` per task (no multi-dependency DAG yet)
- No retry mechanism (intentional — retries hide root causes)
- No remote execution — runs processes locally
- Codex is the only runner backend (runner interface is ready for extensions)
- No live status refresh in terminal (prints final state)

## Roadmap

- [ ] Claude runner backend
- [ ] Script runner backend (arbitrary commands)
- [ ] Multi-dependency support (`depends_on` as array)
- [ ] Live terminal status with refresh
- [ ] SARIF report output

## License

[MIT](LICENSE)
