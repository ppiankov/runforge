[![CI](https://github.com/ppiankov/runforge/actions/workflows/ci.yml/badge.svg)](https://github.com/ppiankov/runforge/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![ANCC](https://img.shields.io/badge/ANCC-compliant-brightgreen)](https://ancc.dev)

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
- **Runner fallback cascade** — if codex rate-limits, falls to z.ai, then claude, with tier-based filtering
- **Seven runner backends** — codex, claude, gemini, opencode, cline, qwen, script
- **Multi-file glob** — `--tasks 'pattern*.json'` loads and merges multiple task files
- **Worktree isolation** — `--parallel-repo` enables true same-repo parallelism via git worktrees
- **Secret scanning** — pre-dispatch scan excludes unsafe runners from repos with secrets
- **Auto-commit** — commits changes that agents leave unstaged
- **Runner graylist** — auto-detects false positives and excludes low-quality runners
- **Task state tracking** — persistent state prevents duplicate runs across sessions
- **Live TUI** — real-time task status with `--tui full|minimal|off|auto`
- **Sentinel loop** — continuous daemon for autonomous scan-run cycles
- **Post-run hooks** — auto-import results to forgeaware for cost tracking
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
┌─────────────┐    ┌─────────────┐     ┌──────────────┐     ┌──────────────┐     ┌─────────────┐
│    scan     │───▶│  generate   │────▶│   audit      │────▶│    run       │────▶│  review     │
│             │    │             │     │              │     │              │     │             │
│26 checks    │    │parse WOs    │     │remove done   │     │DAG schedule  │     │status report│
│6 categories │    │inject config│     │narrow partial│     │runner cascade│     │forgeaware   │
│task output  │    │merge files  │     │validate      │     │live TUI      │     │rerun failed │
└─────────────┘    └─────────────┘     └──────────────┘     └──────────────┘     └─────────────┘
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

Seven built-in runner types: `codex`, `claude`, `gemini`, `opencode`, `cline`, `qwen`, `script`.

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
| `--idle-timeout DUR` | `5m` | Kill task after no stdout for this duration (only stdout events reset the timer; stderr does not) |
| `--fail-fast` | `false` | Stop spawning new tasks on first failure |
| `--tui MODE` | `auto` | Display mode: `full` (interactive TUI), `minimal` (live status), `off` (no live display), `auto` (detect TTY) |
| `--allow-free` | `false` | Include free-tier runners in fallback cascade |
| `--retry` | `false` | Re-execute failed and interrupted tasks (skips completed) |
| `--no-auto-commit` | `false` | Disable post-task auto-commit |
| `--parallel-repo` | `false` | Enable worktree-based parallel execution for same-repo tasks |

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

### `runforge watch`

Monitor a running runforge session in real-time (top-like TUI).

```bash
runforge watch                                      # auto-detect latest run
runforge watch --run-dir .runforge/20260217-143750   # specific run
```

### `runforge verify`

Proofcheck a completed run: detect false positives, verify tests and lint pass.

| Flag | Default | Description |
|------|---------|-------------|
| `--run-dir DIR` | (required) | Run directory to verify |
| `--repos-dir DIR` | `.` | Base directory containing repos |
| `--mark-done` | `false` | Mark verified tasks as done in state tracker |

```bash
runforge verify --run-dir .runforge/20260217-143750 --repos-dir ~/dev/repos
```

### `runforge validate`

Validate a task file without running anything. Checks JSON structure, dependency cycles, repo references, and runner profiles.

```bash
runforge validate --tasks runforge-tasks.json --repos-dir ~/dev/repos
```

### `runforge unlock`

Remove a stale repo lock file. Use when a task crashes with a lock held, preventing other tasks from accessing the repo.

```bash
runforge unlock --repos-dir ~/dev/repos --repo org/my-repo
```

### `runforge graylist`

Manage the runner quality graylist. Graylisted runners are excluded from fallback cascade positions (but still used when explicitly assigned as primary runner).

```bash
runforge graylist list                              # show all graylisted runners
runforge graylist add deepseek --model deepseek-chat --reason "low quality"
runforge graylist remove deepseek --model deepseek-chat
runforge graylist clear                             # remove all entries
```

### `runforge state`

Manage persistent task state. State tracking prevents duplicate runs across sessions.

```bash
runforge state list                                 # show task states
runforge state reset task-id                        # reset a specific task
runforge state clear                                # clear all state
```

### `runforge sentinel loop`

Continuous daemon: scan repos, deduplicate completed tasks, run, cooldown, repeat.

```bash
runforge sentinel loop --repos-dir ~/dev/repos --cooldown 30m --workers 6
```

### `runforge ingest`

Import external run results into forgeaware.

```bash
runforge ingest --run-dir .runforge/20260217-143750
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
| `depends_on` | No | IDs of tasks that must complete first (string or array) |
| `runner` | No | Runner backend (default: from config or `codex`) |
| `fallbacks` | No | Runner profiles to try on failure/rate-limit |
| `difficulty` | No | Task difficulty: `simple`, `medium`, `complex` (auto-scored at generate time) |
| `score` | No | Numeric difficulty score (auto-scored at generate time) |

Task file top-level fields:

| Field | Description |
|-------|-------------|
| `default_runner` | Runner for tasks without explicit `runner` field |
| `default_fallbacks` | Fallback cascade applied to tasks without `fallbacks` |
| `runners` | Named runner profiles with type, model, env overrides |
| `parallel_repo` | Enable worktree isolation for same-repo tasks |
| `merge_back` | Auto-merge worktree branch back to main (default: true, FF-only) |
| `review` | Auto-review config: `enabled`, `runner`, `fallback_only` |

## Signal Handling

Ctrl+C sends SIGINT to runforge, which:

1. Cancels all running tasks via process group kill (entire process tree, not just the runner)
2. Skips pending tasks that haven't started
3. Writes a partial `report.json` with results collected so far
4. Releases all repo locks

In-flight tasks are killed immediately. The report is always written, even on interrupt.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | All tasks completed successfully |
| `1` | One or more tasks failed |
| `4` | All tasks hit API rate limits |

## Secret Scanning

Before dispatching tasks, runforge scans each repo for secrets using `pastewatch-cli`. Repos with detected secrets are flagged, and unsafe runners (those without structural secret protection hooks) are excluded from the fallback cascade for those repos. Runners with built-in safety hooks (claude, cline) are always allowed.

After task completion, output files are scanned and any detected secrets are redacted in place.

## Runner Graylist

Runforge auto-detects false positives — tasks that complete with exit code 0 but produce no real work (no git commits, no output events). Runners that produce false positives are automatically graylisted with their specific model.

Graylisting is model-aware: graylisting `deepseek:deepseek-chat` does not block `deepseek:deepseek-reasoner`. Graylisted runners are excluded from fallback cascade positions but still used when explicitly assigned as primary runner.

The graylist persists across runs at `~/.runforge/graylist.json`. Manage manually with `runforge graylist`.

## Architecture

```
cmd/runforge/main.go        -- CLI entry point (exit codes: 0, 1, 4)
internal/
  cli/
    run.go                  -- run command: DAG scheduling, worktree/lock, TUI, post-run hook
    cascade.go              -- runner cascade, fallback filtering (graylist, free, secret, tier)
    generate.go             -- generate command: scan repos, inject runner profiles
    scan.go                 -- scan command: portfolio auditor
    rerun.go                -- rerun command: retry failed tasks with preserved config
    status.go               -- status command: auto-detects latest run dir
    graylist.go             -- graylist CLI subcommands (list, add, remove, clear)
    state_cmd.go            -- state CLI subcommands (list, reset, clear)
    root.go                 -- Cobra root, version vars, global flags
  config/
    settings.go             -- .runforge.yml loading, runner profile config
    loader.go               -- task file loading, glob resolution, multi-file merge
  task/
    model.go                -- Task, TaskFile, TaskResult, RunReport, RunnerProfileConfig
    graph.go                -- Dependency DAG, topological sort (Kahn's algorithm)
    scheduler.go            -- Worker pool with dependency-aware scheduling
    scorer.go               -- Task difficulty scoring, runner tier defaults
  runner/
    runner.go               -- Runner interface and registry
    codex.go                -- Codex exec backend (JSONL parsing, env resolution)
    claude.go               -- Claude Code CLI backend (stream-json parsing)
    gemini.go               -- Gemini CLI backend (stream-json parsing, headless mode)
    opencode.go             -- OpenCode CLI backend (JSON output, multi-provider)
    cline.go                -- Cline CLI backend (headless, structural safety hooks)
    qwen.go                 -- Qwen Code CLI backend (stream-json, model override)
    lock.go                 -- Per-repo file locking with wait-and-retry
    worktree.go             -- Git worktree isolation for same-repo parallelism
    blacklist.go            -- Runner blacklist with TTL for rate-limited providers
    graylist.go             -- Model-aware runner graylist with persistence
    prescan.go              -- Pre-dispatch secret scan (pastewatch-cli)
    autocommit.go           -- Post-task auto-commit with deterministic messages
    validate.go             -- Model pre-validation (OpenCode config parsing)
    health.go               -- Connectivity error detection (TLS/DNS/connection)
    profile.go              -- Runner profile resolution (env: prefix → os.Getenv)
  scan/
    scanner.go              -- Scan() entry point: walk repos, run checks, sort findings
    checker.go              -- Checker interface, AllCheckers() registry (26 checks)
    checks.go               -- Check implementations + promptBuilder for autonomous prompts
    finding.go              -- Finding, Severity, TaskPrompt() (prompt vs suggestion)
    repo.go                 -- RepoInfo, DetectRepo(), language detection
    format.go               -- TextFormatter, JSONFormatter, TaskFormatter
  reporter/
    tui.go                  -- Bubbletea interactive TUI (full mode)
    live.go                 -- ANSI live status (minimal mode)
    text.go                 -- Text reporter with cascade attempt display
    json.go                 -- JSON report writer
  state/
    state.go                -- Persistent task state tracker (.runforge/state.json)
    filter.go               -- Task filtering based on state (skip completed, --retry)
  sentinel/
    loop.go                 -- Sentinel loop daemon (scan → dedup → run → cooldown)
    tracker.go              -- CompletionTracker (dedup across cycles)
    state.go                -- SentinelState (thread-safe shared state)
    tui.go                  -- Mission Control TUI (3-tab Bubbletea dashboard)
  generate/
    workorder.go            -- Work-order parser
    generate.go             -- Task file generator with difficulty scoring
  ingest/
    ingest.go               -- Forgeaware result import
```

## Known Limitations

- No remote execution — runs processes locally
- No pre-flight quota check — if a runner rate-limits, fallback cascade handles it
- Worktree merge-back is fast-forward only — divergent branches require manual resolution

## Roadmap

- [x] Portfolio scanner with 26 checks across 6 categories
- [x] Scan-to-task pipeline with autonomous agent prompts
- [x] Multi-file glob support for task loading
- [x] TUI mode selection (full/minimal/off/auto)
- [x] Per-task git worktrees for same-repo parallelism
- [x] Script runner backend (arbitrary commands)
- [x] Rate limit detection with reset countdown and runner blacklisting
- [x] Claude Code runner backend
- [x] Qwen Code runner backend
- [x] Cline runner backend
- [x] Runner graylist with false positive auto-detection
- [x] Pre-dispatch secret scanning
- [x] Post-task auto-commit
- [x] Task difficulty scoring with tier-based cascade filtering
- [x] Persistent task state tracking
- [x] Sentinel loop for continuous autonomous operation

## License

[MIT](LICENSE)
