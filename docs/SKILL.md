---
name: tokencontrol
description: Dependency-aware parallel task runner for AI coding agents with multi-provider fallback, portfolio auditing, and autonomous operation
user-invocable: false
metadata: {"requires":{"bins":["tokencontrol"]}}
---

# tokencontrol — AI Agent Task Runner & Portfolio Auditor

You have access to `tokencontrol`, a dependency-aware parallel task runner for AI coding agents. It reads JSON task files, builds a DAG, executes tasks across repos using seven pluggable runner backends with multi-provider fallback cascades, and produces structured JSON reports. No cloud, all local.

## Install

```bash
brew install ppiankov/tap/tokencontrol
```

Or via Go:

```bash
go install github.com/ppiankov/tokencontrol/cmd/tokencontrol@latest
```

## Setup

Create `.tokencontrol.yml` in your repos directory:

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
```

## Commands

| Command | What it does |
|---------|-------------|
| `tokencontrol scan` | Audit repos for structural, security, and quality issues |
| `tokencontrol scan --format tasks` | Generate agent-ready task file from scan findings |
| `tokencontrol generate` | Generate task file from work-orders.md in repos |
| `tokencontrol run` | Execute tasks in parallel with DAG scheduling |
| `tokencontrol run --dry-run` | Preview execution plan without running |
| `tokencontrol run --parallel-repo` | Enable git worktree isolation for same-repo parallelism |
| `tokencontrol rerun --run-dir DIR` | Retry failed/skipped tasks from a previous run |
| `tokencontrol status` | Show results of a completed run |
| `tokencontrol watch` | Monitor a running session in real-time (top-like TUI) |
| `tokencontrol verify --run-dir DIR` | Proofcheck a completed run (false positives, tests, lint) |
| `tokencontrol validate --tasks FILE` | Validate task file without running |
| `tokencontrol unlock --repo ORG/REPO` | Remove a stale repo lock |
| `tokencontrol graylist list` | Show graylisted runners |
| `tokencontrol graylist add NAME --model MODEL` | Graylist a runner:model pair |
| `tokencontrol graylist remove NAME --model MODEL` | Remove from graylist |
| `tokencontrol graylist clear` | Clear all graylist entries |
| `tokencontrol state list` | Show persistent task states |
| `tokencontrol state reset TASK-ID` | Reset a specific task state |
| `tokencontrol state clear` | Clear all task state |
| `tokencontrol sentinel loop` | Continuous daemon: scan, deduplicate, run, cooldown, repeat |
| `tokencontrol ingest --run-dir DIR` | Import external run results |
| `tokencontrol doctor` | Health check: runners, config, dependencies |
| `tokencontrol version` | Print version |

## Key Flags

### tokencontrol run

| Flag | Default | Description |
|------|---------|-------------|
| `--tasks FILE` | `tokencontrol.json` | Task file path (supports glob: `'tokencontrol-*.json'`) |
| `--workers N` | `4` | Max parallel runner processes |
| `--repos-dir DIR` | `.` | Base directory containing repos |
| `--config FILE` | `.tokencontrol.yml` | Settings file |
| `--filter PATTERN` | | Only run tasks matching ID glob |
| `--dry-run` | `false` | Show execution plan without running |
| `--verify` | `false` | Run `make test && make lint` per repo after completion |
| `--max-runtime DUR` | `30m` | Per-task timeout |
| `--idle-timeout DUR` | `5m` | Kill task after no stdout for this duration |
| `--fail-fast` | `false` | Stop spawning new tasks on first failure |
| `--tui MODE` | `auto` | Display: `full`, `minimal`, `off`, `auto` |
| `--allow-free` | `false` | Include free-tier runners in fallback cascade |
| `--retry` | `false` | Re-execute failed and interrupted tasks |
| `--no-auto-commit` | `false` | Disable post-task auto-commit |
| `--parallel-repo` | `false` | Enable worktree-based same-repo parallelism |

### tokencontrol scan

| Flag | Default | Description |
|------|---------|-------------|
| `--repos-dir DIR` | `.` | Base directory containing repos |
| `--format FMT` | `text` | Output: `text`, `json`, `tasks` |
| `--filter-repo NAME` | | Scan only this repo |
| `--severity LEVEL` | | Minimum: `critical`, `warning`, `info` |
| `--check CATS` | | Categories: structure, go, python, security, ci, quality |
| `--output FILE` | stdout | Write to file |

### tokencontrol generate

| Flag | Default | Description |
|------|---------|-------------|
| `--repos-dir DIR` | `.` | Base directory to scan |
| `--config FILE` | `.tokencontrol.yml` | Inject runner profiles from config |
| `--output FILE` | `tokencontrol-tasks.json` | Output task file path |
| `--owner ORG` | (inferred) | GitHub owner for repo slugs |
| `--runner NAME` | `codex` | Default runner for generated tasks |
| `--filter-repo NAME` | | Only scan this repo |

## Agent Usage Pattern

For programmatic use, always use `--format json` or parse `report.json`:

```bash
# Full pipeline: scan, generate, run
tokencontrol scan --repos-dir ~/dev/repos --format tasks --output scan-tasks.json
tokencontrol run --tasks scan-tasks.json --repos-dir ~/dev/repos --workers 6
```

### JSON Output — report.json

After each run, `report.json` is written to the run directory (`.tokencontrol/<timestamp>/report.json`):

```json
{
  "run_id": "a1b2c3d4e5f6",
  "timestamp": "2026-02-20T14:37:50Z",
  "tasks_files": ["tokencontrol-tasks.json"],
  "workers": 6,
  "repos_dir": "/path/to/repos",
  "results": {
    "repo1-WO01": {
      "task_id": "repo1-WO01",
      "state": 3,
      "duration": 200000000000,
      "runner_used": "codex",
      "attempts": [
        {"runner": "codex", "state": 3, "duration": 200000000000}
      ]
    }
  },
  "total_tasks": 5,
  "completed": 4,
  "failed": 1,
  "skipped": 0,
  "rate_limited": 0,
  "total_duration": 600000000000
}
```

Task states: 0=pending, 1=ready, 2=running, 3=completed, 4=failed, 5=skipped, 6=rate_limited.

### JSON Output — scan

```json
{
  "repos": [
    {
      "name": "org/repo1",
      "findings": [
        {
          "check": "missing_golangci_lint",
          "category": "go",
          "severity": "warning",
          "message": "No .golangci.yml found"
        }
      ]
    }
  ],
  "summary": {"repos_scanned": 12, "total_findings": 28, "critical": 3, "warning": 15, "info": 10}
}
```

### Parsing Examples

```bash
# List failed tasks from a run
cat .tokencontrol/latest/report.json | jq '[.results[] | select(.state == 4)] | .[].task_id'

# Get tasks that used fallback runners
cat .tokencontrol/latest/report.json | jq '[.results[] | select(.attempts | length > 1)] | .[].task_id'

# Scan for critical issues only
tokencontrol scan --repos-dir ~/dev/repos --format json --severity critical | jq '.summary'

# Generate fix tasks from scan and execute
tokencontrol scan --repos-dir ~/dev/repos --format tasks --output fixes.json
tokencontrol run --tasks fixes.json --repos-dir ~/dev/repos --workers 4

# Rerun failures from last run
tokencontrol rerun --run-dir .tokencontrol/latest

# Check graylist status
tokencontrol graylist list
```

## Task File Format

```json
{
  "default_runner": "codex",
  "default_fallbacks": ["zai", "claude"],
  "runners": {
    "codex": { "type": "codex" },
    "zai": { "type": "codex", "env": {"OPENAI_API_KEY": "env:ZAI_API_KEY"} },
    "claude": { "type": "claude" }
  },
  "parallel_repo": false,
  "merge_back": true,
  "tasks": [
    {
      "id": "repo1-WO01",
      "repo": "org/repo1",
      "priority": 1,
      "title": "Add unit tests",
      "prompt": "Add unit tests for all internal packages...",
      "depends_on": [],
      "runner": "codex",
      "fallbacks": ["zai"],
      "difficulty": "medium",
      "score": 8
    }
  ]
}
```

Runner types: `codex`, `claude`, `gemini`, `opencode`, `cline`, `qwen`, `script`.

## Typical Workflow

1. **Scan repos:** `tokencontrol scan --repos-dir ~/dev/repos --format tasks --output scan-tasks.json`
2. **Or generate from WOs:** `tokencontrol generate --repos-dir ~/dev/repos --config .tokencontrol.yml`
3. **Preview:** `tokencontrol run --dry-run --tasks tokencontrol-tasks.json`
4. **Execute:** `tokencontrol run --tasks tokencontrol-tasks.json --repos-dir ~/dev/repos --workers 6`
5. **Monitor:** `tokencontrol watch` (in another terminal)
6. **Review:** `tokencontrol status` then `tokencontrol verify --run-dir .tokencontrol/latest`
7. **Retry failures:** `tokencontrol rerun --run-dir .tokencontrol/latest`
8. **Continuous mode:** `tokencontrol sentinel loop --repos-dir ~/dev/repos --cooldown 30m`

## Runner Cascade

When a runner fails or is rate-limited, tokencontrol tries the next in the cascade:

```
codex (primary) ──fail──> zai (fallback 1) ──fail──> claude (fallback 2)
```

Cascade filtering removes unsuitable runners automatically:
- **Graylist** — runners that produced false positives (model-aware, persistent)
- **Free tier** — free models excluded by default (`--allow-free` to opt in)
- **Secret repos** — unsafe runners excluded for repos with detected secrets
- **Tier** — weak runners excluded from complex tasks (tier-based difficulty scoring)

## Exit Codes

- `0` — all tasks completed successfully
- `1` — one or more tasks failed
- `4` — all tasks hit API rate limits

## What tokencontrol Does NOT Do

- Does not provide its own AI — orchestrates external runners only
- Does not replace CI/CD — designed for AI agent task orchestration
- Does not run remotely — all processes execute locally
- Does not auto-push — tasks produce local changes and commits for review
- Does not use ML — all scoring and filtering is deterministic
- Does not require network for local repos — only runners need API access
