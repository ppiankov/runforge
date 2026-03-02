# CI/CD Integration Guide

Tokencontrol is not a CI/CD system — it runs inside your existing pipelines. This guide shows how to integrate tokencontrol with GitHub Actions for automated scanning, fixing, and verification.

## Prerequisites

- Go 1.22+ (for `go install`)
- `gh` CLI (for `tokencontrol pr`)
- API keys for your runner backends (codex, claude, gemini)

## Installing in CI

```yaml
- uses: actions/setup-go@v5
  with:
    go-version-file: go.mod

- run: go install github.com/ppiankov/tokencontrol/cmd/tokencontrol@latest
```

For a pinned version:

```yaml
- run: go install github.com/ppiankov/tokencontrol/cmd/tokencontrol@v0.13.0
```

## Secrets Configuration

Add these as repository or organization secrets:

| Secret | Runner | Required |
|--------|--------|----------|
| `OPENAI_API_KEY` | codex | Yes (if using codex) |
| `ANTHROPIC_API_KEY` | claude | Yes (if using claude) |
| `GEMINI_API_KEY` | gemini | Yes (if using gemini) |
| `GITHUB_TOKEN` | pr creation | Automatic |

The `GITHUB_TOKEN` is provided automatically by GitHub Actions. For `tokencontrol pr`, set it as `GH_TOKEN`:

```yaml
env:
  GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

## Workflow Patterns

### Nightly Scan & Fix

Scan repos for issues, generate fix tasks, run agents, and open draft PRs.

See [`examples/github-actions/nightly-scan.yml`](../examples/github-actions/nightly-scan.yml) for a complete workflow.

```bash
# 1. Scan repos and generate tasks
tokencontrol scan --repos-dir repos --format tasks --output tasks.json

# 2. Run tasks with AI agents
tokencontrol run --tasks tasks.json --repos-dir repos --tui off

# 3. Push branches and create draft PRs
tokencontrol pr --repos-dir repos --draft
```

Key flags for CI:
- `--tui off` — disable interactive TUI (no terminal in CI)
- `--draft` — create draft PRs for human review
- `--dry-run` — preview what would happen without executing

### PR Verification

Verify that agent-generated changes pass quality gates.

See [`examples/github-actions/verify-pr.yml`](../examples/github-actions/verify-pr.yml) for a complete workflow.

```bash
# Verify the latest run's results
tokencontrol verify
```

Verification checks per task:
1. **Events** — non-empty output (detects false positives)
2. **Git diff** — no uncommitted changes left behind
3. **Tests** — `make test` or `go test ./...` passes
4. **Lint** — `make lint` or `golangci-lint run ./...` passes

### On-Demand Run

Trigger a run manually with `workflow_dispatch`:

```yaml
on:
  workflow_dispatch:
    inputs:
      tasks:
        description: 'Path to tasks JSON file'
        required: true
        default: 'tokencontrol.json'
```

```bash
tokencontrol run --tasks ${{ inputs.tasks }} --repos-dir repos --tui off
tokencontrol status
```

## Saving Artifacts

Save run reports for debugging and audit trails:

```yaml
- uses: actions/upload-artifact@v4
  if: always()
  with:
    name: tokencontrol-report
    path: .tokencontrol/
    retention-days: 30
```

## Runner Installation in CI

Most runners are CLI tools that need separate installation:

```yaml
# Codex CLI
- run: npm install -g @anthropic/codex

# Claude Code CLI
- run: npm install -g @anthropic-ai/claude-code

# Gemini CLI
- run: pip install google-generativeai
```

Check what's available with `tokencontrol doctor`.

## Troubleshooting

### No runners detected

Run `tokencontrol doctor` to see what's available. Ensure runner CLIs are installed and API keys are set as secrets.

### Rate limiting

Tokencontrol handles rate limits with fallback cascades. Configure multiple runners:

```json
{
  "default_runner": "codex",
  "default_fallbacks": ["claude", "gemini"]
}
```

If all runners are rate-limited, the task fails with exit code 4. Schedule retries with `tokencontrol run --retry`.

### TUI errors in CI

Always use `--tui off` in CI environments. The interactive TUI requires a terminal.

### PR creation fails

Ensure `GH_TOKEN` is set (not `GITHUB_TOKEN` — `gh` CLI reads `GH_TOKEN`):

```yaml
env:
  GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

Ensure the workflow has `pull-requests: write` permission.
