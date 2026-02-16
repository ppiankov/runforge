# End-to-End Workflow

Generate tasks from work orders, dispatch them across parallel AI runners, and import results into forgeaware — all as one flow.

## Prerequisites

- `runforge` built and in PATH
- `forgeaware` installed (`forge-import-run.sh` available)
- Repos with `docs/work-orders.md` cloned under a common directory

## Step 1: Generate Tasks

Scan repos for pending work orders and produce a task file:

```bash
runforge generate --repos-dir ~/dev/ppiankov-github --output runforge-tasks.json
```

This reads `docs/work-orders.md` from each repo, extracts WOs with status `[ ]` or `[~]`, and builds a runforge-compatible JSON file.

## Step 2: Configure Settings

Create `.runforge.yml` for persistent defaults:

```yaml
workers: 6
repos_dir: ~/dev/ppiankov-github
max_runtime: 45m
fail_fast: false
verify: true
post_run: forge-import-run.sh "$RUNFORGE_RUN_DIR"
```

Key fields:
- `post_run` — shell command executed after each run's report is written. `$RUNFORGE_RUN_DIR` is set to the absolute path of the run directory. Use this to auto-import results into forgeaware.
- `verify` — run `make test && make lint` per repo after tasks complete.

## Step 3: Run Tasks

```bash
runforge run --tasks runforge-tasks.json
```

What happens:
1. Tasks are scheduled in dependency order across `workers` parallel slots
2. Each task tries runners in cascade order (e.g., codex → z.ai → claude)
3. Rate-limited runners are blacklisted with TTL until their quota resets
4. Live TUI shows running tasks with elapsed time and runner info
5. Status written to `/tmp/runforge-status` for external consumers (e.g., Claude Code statusline)
6. Completed tasks trigger async review if `review.enabled: true` in the task file
7. Report written to `.runforge/<timestamp>/report.json`
8. `post_run` hook fires (e.g., forgeaware import)

## Step 4: Monitor

During execution, the live TUI shows:
- Running tasks with elapsed time and assigned runner
- Completed tasks with duration and fallback indicator
- Failed and rate-limited tasks with error details

For detached monitoring (e.g., from another terminal):

```bash
runforge watch --run-dir .runforge/20260217-140000
```

Claude Code's status line also shows runforge progress when a run is active.

## Step 5: Review Results

After the run, check the report:

```bash
cat .runforge/20260217-140000/report.json | jq '.results | to_entries[] | {id: .key, state: .value.state, runner: .value.runner_used}'
```

Rerun failed tasks:

```bash
runforge rerun --run-dir .runforge/20260217-140000
```

## Step 6: Forgeaware Metrics

If `post_run` is configured, results are already imported. Otherwise, manually:

```bash
forge-import-run.sh .runforge/20260217-140000
```

This appends entries to `~/.forgeaware/metrics.log`:
- Task result (pass/fail/partial)
- Estimated tokens
- Wall time
- Review status (if review was enabled)

## Runner Profiles

Define custom runner profiles in the task file for different API endpoints or models:

```json
{
  "runners": {
    "zai": {
      "type": "codex",
      "model": "o3",
      "env": {
        "OPENAI_API_KEY": "env:ZAI_API_KEY",
        "OPENAI_BASE_URL": "https://api.z.ai/v1"
      }
    }
  },
  "default_runner": "codex",
  "default_fallbacks": ["zai", "claude"]
}
```

## Auto-Review

Enable cross-review of fallback tasks:

```json
{
  "review": {
    "enabled": true,
    "runner": "codex",
    "fallback_only": true
  }
}
```

When a task completes via a fallback runner (e.g., z.ai), the review runner (e.g., codex) verifies the output. Review results appear in `report.json` and forgeaware metrics.

## File Layout

```
.runforge/
  20260217-140000/
    report.json            # full run report
    report.sarif           # SARIF for GitHub Code Scanning
    task-WO01/
      events.jsonl         # attempt 1 (codex)
      stderr.log
      attempt-2-zai/       # attempt 2 (fallback)
        events.jsonl
        output.md
      review/              # auto-review output
        events.jsonl
```
