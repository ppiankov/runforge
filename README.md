[![CI](https://github.com/ppiankov/runforge/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/runforge/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

# runforge

Dependency-aware parallel task runner and portfolio auditor for AI coding agents with multi-provider fallback and cost tracking.

## Why This Exists

AI coding agents (Claude Code, Codex, z.ai) are powerful but suffer from three efficiency problems when used interactively:

- **Context pollution** — each task invocation pollutes the conversation with irrelevant file reads. After 10 tasks across 6 repos, the context window is full of code from other projects.
- **Context drift** — as history grows, the agent loses focus. By tool call 50+, it re-reads files and makes decisions based on stale context.
- **Token bleed** — tokens spent on planning and copy-pasting between tasks. Each round-trip costs planning tokens that produce no code.

Runforge eliminates all three: define tasks once in JSON, execute in parallel across multiple LLM providers, track results with forgeaware. The interactive session stays clean for architecture and review.

## What This Is

- Reads a JSON task file with dependency declarations
- Builds a DAG and executes tasks in topological order with configurable parallelism
- **Portfolio scanner** — 26 checks across 6 categories audit repos for structural, security, and quality issues
- **Scan-to-task pipeline** — `scan --format tasks` generates agent-ready task files with detailed prompts
- **Runner fallback cascade** — if codex rate-limits, falls to z.ai, then claude
- **Multi-provider execution** — assign tasks to different LLM providers for parallel utilization
- **Multi-file glob** — `--tasks 'pattern*.json'` loads and merges multiple task files
- **Repo-level locking** — prevents two agents from modifying the same repo simultaneously
- **Live TUI** — real-time task status with `--tui full|minimal|off|auto`
- **Post-run hooks** — auto-import results to forgeaware for cost tracking
- **Per-task metadata** — each output dir contains `task.json` making runs self-contained
- Produces JSON reports for post-run analysis and rerun of failures

## What This Is NOT

- Not a CI/CD system — designed for AI agent orchestration, not build pipelines
- Not Airflow — orchestrates LLM coding agents, not ETL data jobs
- Not a process supervisor — runs once, exits when done
- Not a build system — delegates builds to Makefiles in target repos
- Does not provide its own AI — orchestrates external runners (Codex, z.ai, Claude)

## Quick Start

### Install

```bash
# Homebrew
brew install ppiankov/tap/runforge

# Go install
go install github.com/ppiankov/runforge/cmd/runforge@latest
```

### Configure

Create `.runforge.yml` in your repos directory:

```yaml
repos_dir: /path/to/repos
workers: 6
fail_fast: true
max_runtime: 30m

default_runner: codex
default_fallbacks:
  - zai

runners:
  codex:
    type: codex
  zai:
    type: codex
    env:
      OPENAI_API_KEY: "env:ZAI_API_KEY"
      OPENAI_BASE_URL: "https://api.zai.zhipu.ai/v1"

post_run: /path/to/forgeaware/scripts/forge-import-run.sh $RUNFORGE_RUN_DIR
```

### Generate and Run

```bash
# Generate task file from work orders
runforge generate --repos-dir ~/dev/repos --config .runforge.yml

# Preview execution plan
runforge run --dry-run --tasks runforge-tasks.json --repos-dir ~/dev/repos

# Execute with live TUI
runforge run --tasks runforge-tasks.json --repos-dir ~/dev/repos --config .runforge.yml --workers 6
```

## Workflow

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│     scan     │────▶│   generate   │────▶│    audit     │────▶│     run      │────▶│   review    │
│              │     │              │     │              │     │              │     │              │
│ 26 checks    │     │ parse WOs    │     │ remove done  │     │ DAG schedule │     │ status report│
│ 6 categories │     │ inject config│     │ narrow partial│    │ runner cascade│    │ forgeaware   │
│ task output  │     │ merge files  │     │ validate     │     │ live TUI     │     │ rerun failed │
└─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘     └─────────────┘
```

**Step 1: Scan** — audit all repos for structural, security, and quality issues. Optionally generate task files for autonomous fixing.

```bash
runforge scan --repos-dir ~/dev/repos --format tasks --output scan-tasks.json
```

**Step 2: Generate** — scan repos for `docs/work-orders.md`, extract pending WOs, inject runner profiles from `.runforge.yml`.

```bash
runforge generate --repos-dir ~/dev/repos --config .runforge.yml
```

**Step 3: Run** — execute tasks in parallel with dependency ordering and live TUI. Supports glob patterns for multi-file loading.

```bash
runforge run --tasks 'runforge-*.json' --repos-dir ~/dev/repos --config .runforge.yml --workers 6
```

**Step 4: Review** — inspect results, verify repos, rerun failures.

```bash
runforge status --run-dir .runforge/<latest>
runforge rerun --run-dir .runforge/<latest>    # failed/skipped only
```

## Runner Cascade

When a runner fails or is rate-limited, runforge automatically tries the next provider in the cascade:

```
gemini (primary) ──fail──▶ codex (fallback 1) ──fail──▶ claude (fallback 2)
```

Five built-in runner types: `codex`, `claude`, `gemini`, `opencode`, `script`.

Configure per-task or globally:

```json
{
  "default_runner": "gemini",
  "default_fallbacks": ["codex", "claude"],
  "runners": {
    "gemini": { "type": "gemini" },
    "codex": { "type": "codex" },
    "zai": {
      "type": "codex",
      "env": {
        "OPENAI_API_KEY": "env:ZAI_API_KEY",
        "OPENAI_BASE_URL": "https://api.zai.zhipu.ai/v1"
      }
    }
  }
}
```

The TUI shows which runner completed each task:

```
✓ COMPLETED  repo-WO01  Add unit tests           3m20s  [codex]
✓ COMPLETED  repo-WO02  Add SARIF output         4m10s  [via zai]    ← fell back from codex
✗ FAILED     repo-WO03  Add TUI                  0s     (tried codex→zai→claude)
```

Assign tasks to specific runners for parallel provider utilization:

```json
{ "id": "repo-WO01", "runner": "codex", ... }
{ "id": "repo-WO02", "runner": "zai", ... }
```

## Forgeaware Integration

Runforge auto-imports run results to [forgeaware](https://forgeaware.dev) via the `post_run` hook. After each run:

1. `forge-import-run.sh` reads `report.json` from the run directory
2. Extracts per-task: repo, result (pass/fail), token count, wall time, runner used
3. Logs to `~/.forgeaware/metrics.log` for aggregation and dashboards

This creates an accountability pipeline: every AI agent task is tracked — which provider, how many tokens, how long, pass or fail.

## Usage

### `runforge run`

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks FILE` | `runforge.json` | Path to tasks JSON file (supports glob patterns like `'runforge-*.json'`) |
| `--workers N` | `4` | Max parallel runner processes |
| `--repos-dir DIR` | `.` | Base directory containing repos |
| `--config FILE` | `.runforge.yml` | Settings file (workers, runners, post_run) |
| `--filter PATTERN` | | Only run tasks matching ID glob |
| `--dry-run` | `false` | Show execution plan without running |
| `--verify` | `false` | Run `make test && make lint` per repo after completion |
| `--max-runtime DUR` | `30m` | Per-task timeout duration |
| `--idle-timeout DUR` | `5m` | Kill task after no stdout for this duration |
| `--fail-fast` | `false` | Stop spawning new tasks on first failure |
| `--tui MODE` | `auto` | Display mode: `full` (interactive TUI), `minimal` (live status), `off` (no live display), `auto` (detect TTY) |

### `runforge scan`

Audit all repos for structural, security, and quality issues. Runs 26 filesystem-based checks across 6 categories: structure, go, python, security, ci, quality.

| Flag | Default | Description |
|------|---------|-------------|
| `--repos-dir DIR` | `.` | Base directory containing repos |
| `--format FMT` | `text` | Output format: `text` (human-readable), `json` (machine), `tasks` (runforge task file) |
| `--filter-repo NAME` | | Scan only this repo |
| `--severity LEVEL` | | Minimum severity: `critical`, `warning`, `info` |
| `--check CATEGORIES` | | Check categories to run (comma-separated: structure,go,python,security,ci,quality) |
| `--owner ORG` | (inferred) | GitHub owner for task format output |
| `--runner NAME` | `codex` | Default runner for task format output |
| `--output FILE` | (stdout) | Write output to file instead of stdout |

The `tasks` format generates agent-ready prompts with file paths, code patterns, verification commands, and constraints — designed to be immediately runnable via `runforge run` without editing.

```bash
# text summary
runforge scan --repos-dir ~/dev/repos

# generate task file for autonomous fixing
runforge scan --repos-dir ~/dev/repos --format tasks --output scan-tasks.json

# then run the fixes in parallel
runforge run --tasks scan-tasks.json --repos-dir ~/dev/repos --workers 6
```

### `runforge generate`

| Flag | Default | Description |
|------|---------|-------------|
| `--repos-dir DIR` | `.` | Base directory to scan for repos |
| `--config FILE` | `.runforge.yml` | Inject runner profiles from config |
| `--output FILE` | `<repos-dir>/runforge-tasks.json` | Output task file path |
| `--owner ORG` | (inferred from git) | GitHub owner/org for repo slugs |
| `--runner NAME` | `codex` | Default runner for generated tasks |
| `--filter-repo NAME` | | Only scan this repo |

### `runforge rerun`

Rerun failed and skipped tasks from a previous run. Preserves runner profiles and settings.

```bash
runforge rerun --run-dir .runforge/20260217-143750
```

### `runforge status`

```bash
runforge status --run-dir .runforge/20260217-143750
```

### `runforge version`

```bash
runforge version
# runforge 0.2.0 (commit: 817391b, built: 2026-02-17T04:35:23Z, go: go1.26.0)
```

## Task Spec

```json
{
  "description": "Sprint 42 work orders",
  "default_runner": "codex",
  "default_fallbacks": ["zai"],
  "runners": {
    "codex": { "type": "codex" },
    "zai": { "type": "codex", "env": { "OPENAI_API_KEY": "env:ZAI_API_KEY" } }
  },
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
      "depends_on": ["repo1-WO01"],
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
| `priority` | Yes | Execution priority (1=high, 2=medium, 3=low, 99=last) |
| `title` | Yes | Short description |
| `prompt` | Yes | Full prompt for the AI agent |
| `depends_on` | No | IDs of tasks that must complete first |
| `runner` | No | Runner backend (default: from config or `codex`) |

## Architecture

```
cmd/runforge/main.go        -- CLI entry point
internal/
  cli/
    run.go                  -- run command: DAG scheduling, lock, TUI, post-run hook
    generate.go             -- generate command: scan repos, inject runner profiles
    scan.go                 -- scan command: portfolio auditor
    rerun.go                -- rerun command: retry failed tasks with preserved config
    root.go                 -- Cobra root, version vars, global flags
  config/
    settings.go             -- .runforge.yml loading, runner profile config
    loader.go               -- task file loading, glob resolution, multi-file merge
  task/
    model.go                -- Task, TaskFile, TaskResult, RunReport, RunnerProfileConfig
    graph.go                -- Dependency DAG, topological sort (Kahn's algorithm)
    scheduler.go            -- Worker pool with dependency-aware scheduling
  runner/
    runner.go               -- Runner interface and registry
    codex.go                -- Codex exec backend (JSONL parsing, env resolution)
    claude.go               -- Claude Code CLI backend (stream-json parsing)
    gemini.go               -- Gemini CLI backend (stream-json parsing, yolo mode)
    opencode.go             -- OpenCode CLI backend (JSON output parsing, multi-provider)
    lock.go                 -- Per-repo file locking with wait-and-retry
    blacklist.go            -- Global runner blacklist with TTL for rate-limited providers
    profile.go              -- Runner profile resolution (env: prefix → os.Getenv)
  scan/
    scanner.go              -- Scan() entry point: walk repos, run checks, sort findings
    checker.go              -- Checker interface, AllCheckers() registry (26 checks)
    checks.go               -- Check implementations + promptBuilder for autonomous prompts
    finding.go              -- Finding, Severity, TaskPrompt() (prompt vs suggestion)
    repo.go                 -- RepoInfo, DetectRepo(), language detection
    format.go               -- TextFormatter, JSONFormatter, TaskFormatter
  reporter/
    live.go                 -- Live TUI (Bubbletea) with runner tags and fallback indicators
    text.go                 -- Text reporter with cascade attempt display
    json.go                 -- JSON report writer
```

## Known Limitations

- No remote execution — runs processes locally
- Same-repo tasks serialize (repo lock); different repos run in parallel
- No pre-flight quota check — if a runner rate-limits, fallback cascade handles it
- No git branching per task — same-repo parallelism requires repo locking (branching is planned)

## Roadmap

- [x] Portfolio scanner with 26 checks across 6 categories
- [x] Scan-to-task pipeline with autonomous agent prompts
- [x] Multi-file glob support for task loading
- [x] TUI mode selection (full/minimal/off/auto)
- [ ] Per-task git branches — enable true same-repo parallelism
- [ ] SARIF report output
- [ ] Script runner backend (arbitrary commands)
- [ ] Rate limit detection — stop on first 429, show reset countdown
- [ ] Claude runner backend (direct API)
- [ ] Qwen Code runner backend

## License

[MIT](LICENSE)
