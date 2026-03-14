# Model Benchmark: Cross-Model Code Quality Comparison

## Overview

This document describes a methodology for benchmarking AI coding models head-to-head using tokencontrol's parallel execution. The same tasks are assigned to different models simultaneously, each in an isolated git worktree, and the results are compared on quality, speed, cost, and correctness.

## Model Pricing Reference

Prices as of March 2026. Per 1M tokens.

| Model | Provider | Input | Output | Notes |
|-------|----------|------:|-------:|-------|
| Codex Mini | OpenAI | $0.75 | $3.00 | Default for `codex` CLI |
| Claude Sonnet 4.6 | Anthropic | $3.00 | $15.00 | Default for `claude` CLI |
| Claude Opus 4.6 | Anthropic | $5.00 | $25.00 | Max mode |
| Gemini 2.5 Pro | Google | $1.25 | $10.00 | $2.50/$15 above 200K ctx |
| Gemini 2.5 Flash | Google | $0.30 | $2.50 | Free tier available |
| Qwen3.5-Plus | Alibaba | $0.40 | $2.40 | Tiers up at 256K+ ctx |
| Qwen3-Coder | Alibaba | $1.00 | $5.00 | Code-specialized, tiers up |
| DeepSeek Chat | DeepSeek | $0.27 | $1.10 | Cache hits at $0.07 input |
| GLM-4.7 (ZAI) | ZhipuAI | $0.60 | $2.20 | Via OpenCode runner |

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

Each task is duplicated across all runners. The `parallel_repo: true` flag ensures each gets an isolated worktree.

```json
{
  "parallel_repo": true,
  "runners": {
    "codex": { "type": "codex" },
    "claude": { "type": "claude" },
    "gemini": { "type": "gemini", "model": "gemini-2.5-pro" },
    "qwen": { "type": "qwen", "model": "qwen3.5-plus" },
    "deepseek": { "type": "opencode", "model": "deepseek/deepseek-chat" }
  },
  "tasks": [
    { "id": "bench-bugfix-codex", "runner": "codex", "prompt": "..." },
    { "id": "bench-bugfix-claude", "runner": "claude", "prompt": "..." },
    { "id": "bench-bugfix-gemini", "runner": "gemini", "prompt": "..." },
    ...
  ]
}
```

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

### Round 2: Full Cross-Model (pending)

7 runners x 3 task types = 21 tasks. Task file: `/tmp/bench-cross-model.json`

Results will populate this matrix after the run completes:

| Runner | Model | Bug Fix | Feature | Refactor | Avg Grade | Avg Time | Avg Tokens | Est. Cost |
|--------|-------|:-------:|:-------:|:--------:|:---------:|:--------:|:----------:|:---------:|
| codex | Codex Mini | | | | | | | |
| claude | Sonnet 4.6 | | | | | | | |
| gemini | Gemini 2.5 Pro | | | | | | | |
| qwen | Qwen3.5-Plus | | | | | | | |
| qwen-coder | Qwen3-Coder | | | | | | | |
| zai | GLM-4.7 | | | | | | | |
| deepseek | DeepSeek Chat | | | | | | | |

## Interpreting Results

The best model depends on your priorities:

- **Quality-first**: Pick the highest average grade regardless of cost
- **Cost-optimized**: Pick the cheapest model that still achieves B+ or better
- **Speed-first**: Pick the fastest model that achieves B or better
- **Task-specific**: Some models excel at refactoring but struggle with new features — use the per-category grades to assign the right model to the right task type

Tokencontrol supports per-task runner assignment, so you can use different models for different task types in the same run.
