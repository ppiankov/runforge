# Model Benchmark: Cross-Model Code Quality Comparison

## Overview

This document describes a methodology for benchmarking AI coding models head-to-head using tokencontrol's parallel execution. The same tasks are assigned to different models simultaneously, each in an isolated git worktree, and the results are compared on quality, speed, cost, and correctness.

## Model Pricing Reference

Prices as of March 2026. Per 1M tokens.

| Model | Provider | Input | Output | Notes |
|-------|----------|------:|-------:|-------|
| GPT-5.4 | OpenAI | $1.50 | $6.00 | Default for `codex` CLI |
| o3 | OpenAI | $2.00 | $8.00 | Reasoning model via `codex` CLI |
| Claude Sonnet 4 | Anthropic | $3.00 | $15.00 | Default for `claude` CLI |
| Claude Opus 4.6 | Anthropic | $15.00 | $75.00 | Max capability tier |
| Gemini 2.5 Pro | Google | $1.25 | $10.00 | $2.50/$15 above 200K ctx |
| Gemini 2.5 Flash | Google | $0.30 | $2.50 | Free tier available |
| Qwen3.5-Plus | Alibaba | $0.80 | $2.00 | Default for `qwen` CLI |
| Qwen3-Coder | Alibaba | $1.60 | $4.00 | Code-specialized |
| DeepSeek V3 | DeepSeek | $0.27 | $1.10 | Cache hits at $0.07 input |
| DeepSeek R1 | DeepSeek | $0.55 | $2.19 | Reasoning model, thinking tokens extra |
| GLM-4.7 | ZhipuAI | $0.60 | $2.20 | Pro subscription, via OpenCode runner |
| Kilo Gateway | Kilo | subscription | subscription | Subscription-based, no per-token pricing |
| Cline (DeepSeek) | Cline | varies | varies | Uses stored API key, gRPC runner |

## Methodology

### Principles

1. **Same prompt** — every model gets the identical task description
2. **Same repo state** — all run against the same commit (worktree isolation)
3. **Same environment** — same machine, same CLI versions, same timeout
4. **Independent execution** — worktrees prevent cross-contamination
5. **Blind scoring** — evaluate commits without knowing which model produced them (when possible)

### Benchmark Task Categories

Three task types test different coding skills:

| Category | What It Tests | Difficulty |
|----------|--------------|------------|
| **Bug fix** | Reading existing code, understanding intent, targeted changes, regression tests | Easy |
| **New feature** | Design decisions, API design, integration with existing code, comprehensive tests | Medium |
| **Refactoring** | Pattern recognition, abstraction design, behavior preservation, scope discipline | Medium |

### Task File Structure

Each task is duplicated across all runners with identical prompts. The `parallel_repo: true` flag ensures each runner gets an isolated git worktree, preventing cross-contamination. Each runner has `max_concurrent` limits to avoid provider rate-limiting.

```json
{
  "parallel_repo": true,
  "runners": {
    "codex":        { "type": "codex", "max_concurrent": 3 },
    "codex-o3":     { "type": "codex", "model": "o3", "max_concurrent": 3 },
    "claude":       { "type": "claude", "max_concurrent": 3 },
    "claude-opus":  { "type": "claude", "model": "opus", "max_concurrent": 2 },
    "gemini":       { "type": "gemini", "model": "gemini-2.5-pro", "max_concurrent": 5 },
    "qwen":         { "type": "qwen", "model": "qwen3.5-plus", "max_concurrent": 3 },
    "qwen-coder":   { "type": "qwen", "model": "coder-model", "max_concurrent": 3 },
    "deepseek":     { "type": "opencode", "model": "deepseek/deepseek-chat", "max_concurrent": 3 },
    "deepseek-r1":  { "type": "opencode", "model": "deepseek/deepseek-reasoner", "max_concurrent": 2 },
    "zai":          { "type": "opencode", "model": "zai/glm-4.7", "max_concurrent": 3 },
    "kilocode":     { "type": "kilocode", "max_concurrent": 3 },
    "cline":        { "type": "cline", "max_concurrent": 1 }
  },
  "tasks": [
    { "id": "bench-bugfix-codex", "runner": "codex", "prompt": "..." },
    { "id": "bench-bugfix-claude", "runner": "claude", "prompt": "..." },
    ...
  ]
}
```

### Concurrency Strategy

Provider concurrency limits prevent mass rate-limiting during large benchmark runs. Tasks share provider capacity:

| Provider | Runners | Total Concurrent Slots | Auth |
|----------|---------|:----------------------:|------|
| OpenAI | codex (3) + codex-o3 (3) | 6 | Pro subscription |
| Anthropic | claude (3) + claude-opus (2) | 5 | Pro subscription |
| Google | gemini (5) | 5 | Vertex API / OAuth |
| Alibaba | qwen (3) + qwen-coder (3) | 6 | API key |
| DeepSeek | deepseek (3) + deepseek-r1 (2) | 5 | API key |
| ZhipuAI | zai (3) | 3 | Pro subscription |
| Kilo | kilocode (3) | 3 | Subscription |
| Cline | cline (1) | 1 | Stored key (gRPC) |

Total: up to 34 concurrent tasks across all providers. In practice, the 20-worker pool in `.tokencontrol.yml` is the actual ceiling.

### Runner-to-CLI Mapping

Each tokencontrol runner type wraps a different AI coding CLI tool:

| Runner Type | CLI Binary | Model Flag | Event Format |
|-------------|-----------|------------|-------------|
| `codex` | `codex exec` | `--model` | JSON events on stdout |
| `claude` | `claude` | `--model` | JSON events on stdout |
| `gemini` | `gemini` | `--model` | JSON output |
| `qwen` | `qwen` | `--model` | JSON output |
| `opencode` | `opencode` | `--model` | JSON events on stdout |
| `kilocode` | `kilo run` | `--model` | JSON events (OpenCode format) |
| `cline` | `cline` (gRPC) | `--model` | gRPC response |

### Evaluation Criteria

Each implementation is scored on six dimensions (A/B/C/D/F):

| Criterion | What to Look For |
|-----------|-----------------|
| **Correctness** | All requirements implemented? Missing functionality? Bugs? |
| **Test quality** | Comprehensive? Realistic test data? Happy + error + edge cases? |
| **Error handling** | Proper wrapping? Nil checks? Idiomatic patterns? |
| **Code style** | Idiomatic for the language? Consistent with codebase? |
| **Scope discipline** | Stayed focused? No unnecessary changes or additions? |
| **Architecture** | Clean integration? Extensible? Minimal coupling? |

### Cost Calculation

Cost per task = (input_tokens * input_price + output_tokens * output_price) / 1,000,000

Tokencontrol reports token usage per task in the run report, making cost comparison straightforward.

### Running a Benchmark

```bash
# Generate or use existing benchmark task file
tokencontrol run /tmp/bench-cross-model.json

# After completion, compare branches
git log --oneline --all --graph

# Diff specific model outputs
git show <commit-hash-model-A>
git show <commit-hash-model-B>
```

## Results

### Round 1: Qwen3-Coder vs Qwen3.5-Plus (2026-03-14)

Target: [ancc](https://github.com/ppiankov/ancc) (Go CLI, ~5K LOC)

#### Execution Metrics

| Metric | Qwen3-Coder | Qwen3.5-Plus |
|--------|:-----------:|:------------:|
| WO-14 time | 6m 59s | 8m 13s |
| WO-14 tokens | 2.1M | 2.6M |
| WO-16 time | 12m 22s | 11m 1s |
| WO-16 tokens | 5.0M | 4.8M |
| **Total time** | **19m 21s** | **19m 14s** |
| **Total tokens** | **7.1M** | **7.4M** |
| **Est. cost** | **~$0.042** | **~$0.021** |

Cost advantage: Qwen3.5-Plus is ~50% cheaper per token ($0.40/$2.40 vs $1.00/$5.00) with similar token usage.

#### WO-14: Symlink Handling (Bug Fix)

| Criterion | Qwen3-Coder | Qwen3.5-Plus | Winner |
|-----------|:-----------:|:------------:|--------|
| Completeness | 8/13 agents | 13/13 agents | **Plus** |
| Helper design | 3 helpers (over-engineered) | 1 helper (focused) | **Plus** |
| Test approach | Isolated unit + integration | Integration-focused | **Plus** |
| Error handling | Nil-on-interface antipattern | Idiomatic stdlib | **Plus** |
| Lines changed | +349 / -13 | +253 / -33 | **Plus** |

Qwen3-Coder missed 5 agents. Qwen3.5-Plus covered all 13 with less code.

#### WO-16: Semantic Validation (New Feature)

| Criterion | Qwen3-Coder | Qwen3.5-Plus | Winner |
|-----------|:-----------:|:------------:|--------|
| All 5 checks | Yes | Yes | Tie |
| Test infrastructure | Inline literals | Fixture files | **Plus** |
| API consistency | Mixed signatures | Consistent API | **Plus** |
| Efficiency | Re-reads file | Pre-computed | **Plus** |
| Section matching | Case-insensitive | Exact case | **Coder** |
| Duplicate detection | Working | No-op (nil ctx) | **Coder** |

Both implemented all checks. Plus had better architecture; Coder had more robust edge case handling.

#### Quality Grades

| Dimension | Qwen3-Coder | Qwen3.5-Plus |
|-----------|:-----------:|:------------:|
| Correctness | B | **A** |
| Test quality | B+ | **A-** |
| Error handling | B | **A-** |
| Code style | B+ | **A-** |
| Scope discipline | A- | A- |
| Architecture | B | **A** |
| **Overall** | **B** | **A-** |

#### Round 1 Conclusion

Qwen3.5-Plus wins on quality, completeness, and cost. Comparable speed. Recommended as default qwen runner.

### Round 2: Full Cross-Model (2026-03-14)

12 runners × 3 task types = 36 tasks. Task file: `/tmp/bench-cross-model.json`

**Test environment:**
- Machine: MacBook Pro M-series, macOS Darwin 25.2.0
- Go: 1.25.7 (target repo), 1.26.0 (tokencontrol)
- tokencontrol: v0.24.6+ (post-kilocode commit)
- Target repo: [ancc](https://github.com/ppiankov/ancc) (Go CLI, ~5K LOC)
- Workers: 20 (from `.tokencontrol.yml`)
- Idle timeout: 5 minutes per task
- Max runtime: 30 minutes total

**Task prompts (identical across all runners):**

1. **Bug fix** (easy): Fix context cancellation in `ancc doctor` GitHub API check + fix string-based version comparison with numeric `compareVersions()`. Write tests for both.

2. **New feature** (medium): Add `ancc export` command — JSON/YAML export of agent configurations with `--format` and `--agent` flags. New file, Cobra registration, tests.

3. **Refactoring** (medium): Extract `scanAgentPaths` helper from 11 duplicated scan functions in `agents.go`. Reduce code by 30%+ while preserving behavior. Tests.

Results will populate this matrix after the run completes:

| Runner | Model | Bug Fix | Feature | Refactor | Avg Grade | Time | Tokens | Est. Cost |
|--------|-------|:-------:|:-------:|:--------:|:---------:|:----:|:------:|:---------:|
| codex | GPT-5.4 | | | | | | | |
| codex-o3 | o3 | | | | | | | |
| claude | Sonnet 4 | | | | | | | |
| claude-opus | Opus 4.6 | | | | | | | |
| gemini | Gemini 2.5 Pro | | | | | | | |
| qwen | Qwen3.5-Plus | | | | | | | |
| qwen-coder | Qwen3-Coder | | | | | | | |
| deepseek | DeepSeek V3 | | | | | | | |
| deepseek-r1 | DeepSeek R1 | | | | | | | |
| zai | GLM-4.7 | | | | | | | |
| kilocode | Kilo Gateway | | | | | | | |
| cline | Cline (DS) | | | | | | | |

## Interpreting Results

The best model depends on your priorities:

- **Quality-first**: Pick the highest average grade regardless of cost
- **Cost-optimized**: Pick the cheapest model that still achieves B+ or better
- **Speed-first**: Pick the fastest model that achieves B or better
- **Task-specific**: Some models excel at refactoring but struggle with new features — use the per-category grades to assign the right model to the right task type

Tokencontrol supports per-task runner assignment, so you can use different models for different task types in the same run.
