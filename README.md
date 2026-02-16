[![CI](https://github.com/ppiankov/runforge/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/runforge/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

# runforge

Dependency-aware parallel task runner for AI coding agents. Reads a task spec, builds a dependency DAG, spawns parallel processes, detects failures from JSONL output, and reports results.

## Why This Exists

AI coding agents (Claude Code, Codex, Copilot) are powerful but suffer from three efficiency problems when used interactively:

- **Context pollution** — each `/codex` invocation reads files into Claude Code's context window that have nothing to do with the current task. After 10 invocations across 6 repos, the context is full of irrelevant code.
- **Context drift** — as conversation history grows, the agent gradually loses focus on the current task. By tool call 50+, it's re-reading files it already read and making decisions based on stale context.
- **Token bleed** — tokens spent generating one command at a time, copy-pasting, waiting, then generating the next. Each round-trip costs planning tokens that produce no code.

Runforge eliminates all three: define all tasks once in a JSON file, execute them in parallel outside the interactive session, review results when done. The interactive agent stays clean for architecture and review work.

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

## Workflow

The full cycle: generate a task file from work orders, audit it against repo state, execute, review results.

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   generate   │────▶│    audit     │────▶│     run      │────▶│   review    │
│              │     │              │     │              │     │              │
│ scan repos   │     │ remove done  │     │ DAG schedule │     │ status report│
│ parse WOs    │     │ narrow partial│    │ parallel exec│     │ verify repos │
│ emit JSON    │     │ validate     │     │ fail-fast    │     │ rerun failed │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

**Step 1: Generate** — scan repos for `docs/work-orders.md`, extract pending WOs, produce task file.

```bash
runforge generate --repos-dir ~/dev/repos --output runforge-tasks.json
```

**Step 2: Audit** — verify task file against actual repo state, remove completed tasks. See [Task File Maintenance](#task-file-maintenance).

```bash
runforge run --dry-run --tasks runforge-tasks.json --repos-dir ~/dev/repos
```

**Step 3: Run** — execute tasks in parallel with dependency ordering.

```bash
runforge run --tasks runforge-tasks.json --repos-dir ~/dev/repos --workers 6 --fail-fast
```

**Step 4: Review** — inspect results, verify repos, rerun failures.

```bash
runforge status --run-dir .runforge/<latest>
runforge verify --run-dir .runforge/<latest> --repos-dir ~/dev/repos
runforge rerun --run-dir .runforge/<latest>    # failed/skipped only
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
| `priority` | Yes | Execution priority (lower = first; 1=high, 2=medium, 3=low, 99=run last) |
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

## Task File Maintenance

Task files go stale as work completes. Audit before each run to remove done tasks and update partial ones.

### Manual Audit

For each task, check if the described files already exist in the target repo:

```bash
# Quick check: does the file exist?
ls ~/dev/repos/kafkaspectre/cmd/kafkaspectre/main.go

# Check test coverage: do test files exist?
find ~/dev/repos/clickspectre/internal -name '*_test.go'

# Check work-orders.md status markers
grep -E '^\[x\]|^- \[x\]' ~/dev/repos/kafkaspectre/docs/work-orders.md
```

Remove completed tasks. Narrow partial tasks to only what remains.

### Automated Audit via Codex

Offload the audit itself to Codex:

```bash
codex exec --full-auto --json --output-last-message /tmp/codex-task-audit.md \
  -C ~/dev/ppiankov-github \
  "Audit the file codex-tasks.json against the actual state of each repo.

For each task:
1. Check if the described files/features already exist in the repo
2. Check the repo's docs/work-orders.md for [x] (done) status markers
3. If fully done: remove the task from the JSON
4. If partially done: update the prompt to reflect only what remains
5. If not done: keep as-is

Check patterns:
- 'Create X' tasks: does file X exist?
- 'Add tests' tasks: do *_test.go files exist in target packages?
- 'Add slog' tasks: does internal/logging/logging.go exist?
- 'Add SARIF' tasks: does internal/report/sarif.go exist?
- 'JSON header' tasks: do tool/version/timestamp fields exist in reporter?
- 'CONTRIBUTING.md' tasks: does the file exist in repo root?

Write updated codex-tasks.json back. Write change summary to /tmp/codex-task-audit.md.
Verify output is valid JSON: cat codex-tasks.json | jq .
Do NOT change task IDs or repos. Only remove completed and narrow partial tasks."
```

Review the output before running:

```bash
cat /tmp/codex-task-audit.md    # what changed
jq '.tasks | length' codex-tasks.json  # task count
runforge run --dry-run --tasks codex-tasks.json --repos-dir ~/dev/repos
```

## Known Limitations

- Single `depends_on` per task (no multi-dependency DAG yet)
- No retry mechanism (intentional — retries hide root causes)
- No remote execution — runs processes locally
- Codex is the only runner backend (runner interface is ready for extensions)
- No live status refresh in terminal (prints final state)
- No pre-flight quota check — Codex/Claude APIs don't expose remaining quota. If the runner hits a rate limit (`usage_limit_reached`), all subsequent tasks fail with the same error. Workaround: use fewer workers (`--workers 2`) for large batches to stay under rate limits. See WO-11 for planned rate limit detection that will stop spawning on first 429.

## Roadmap

- [ ] Rate limit detection — stop on first 429, show reset countdown (WO-11)
- [ ] Live terminal status with refresh (WO-10)
- [ ] Claude runner backend
- [ ] Script runner backend (arbitrary commands)
- [ ] Multi-dependency support (`depends_on` as array)
- [ ] SARIF report output

## License

[MIT](LICENSE)
