# Qwen Model Benchmark: Qwen3-Coder vs Qwen3.5-Plus

Date: 2026-03-14

## Methodology

We ran a controlled A/B test to determine which Qwen model produces better code in agentic coding tasks. The test used tokencontrol's parallel execution with worktree isolation to run identical tasks on both models simultaneously.

### Setup

- **Runner A**: `qwen` with `model: coder-model` (Qwen3-Coder)
- **Runner B**: `qwen-plus` with `model: qwen3.5-plus` (Qwen3.5-Plus)
- **Target repo**: [ancc](https://github.com/ppiankov/ancc) (Go CLI, ~5K LOC)
- **Execution**: `tokencontrol run` with `parallel_repo: true` — each model gets an isolated git worktree, so they cannot interfere with each other
- **Evaluation**: Post-run diff comparison of commits, scored on correctness, test quality, error handling, code style, scope discipline, and architecture

### Task Selection

Two real work orders with clear acceptance criteria:

| Task | WO | Scope |
|------|----|-------|
| Fix symlink handling + add cline/qwen home paths | WO-14 | Bug fix + feature addition |
| SKILL.md semantic quality validation | WO-16 | New validation pipeline |

Each WO was duplicated into two task entries — identical prompts, different runner assignments. Four tasks total, all running in parallel.

### Task File Structure

```json
{
  "parallel_repo": true,
  "tasks": [
    { "id": "wo14-qwen-coder", "runner": "qwen", "prompt": "..." },
    { "id": "wo14-qwen-plus",  "runner": "qwen-plus", "prompt": "..." },
    { "id": "wo16-qwen-coder", "runner": "qwen", "prompt": "..." },
    { "id": "wo16-qwen-plus",  "runner": "qwen-plus", "prompt": "..." }
  ]
}
```

### Evaluation Criteria

Each implementation was scored on six dimensions:

1. **Correctness** — Does the code actually implement all requirements? Any missing functionality?
2. **Test quality** — Comprehensive tests? Realistic test data? Coverage of happy path, error path, edge cases?
3. **Error handling** — Proper error wrapping? Nil checks? Idiomatic Go error patterns?
4. **Code style** — Idiomatic Go? Consistent with existing codebase patterns?
5. **Scope discipline** — Did the model stay focused, or did it add unnecessary changes?
6. **Architecture** — Clean integration with existing code? Extensible design?

## Results

### Execution Metrics

| Metric | Qwen3-Coder | Qwen3.5-Plus |
|--------|:-----------:|:------------:|
| WO-14 time | 6m 59s | 8m 13s |
| WO-14 tokens | 2.1M | 2.6M |
| WO-16 time | 12m 22s | 11m 1s |
| WO-16 tokens | 5.0M | 4.8M |
| **Total time** | **19m 21s** | **19m 14s** |
| **Total tokens** | **7.1M** | **7.4M** |
| Merge conflicts | 1 | 2 |

Execution speed and token usage are comparable. Both models completed all tasks successfully.

### WO-14: Symlink Handling (Bug Fix + Feature)

| Criterion | Qwen3-Coder | Qwen3.5-Plus | Winner |
|-----------|:-----------:|:------------:|--------|
| Completeness | 8/13 agents fixed | 13/13 agents fixed | **Plus** |
| Helper design | 3 helpers (over-engineered) | 1 helper (focused) | **Plus** |
| Test approach | Isolated unit + integration | Integration-focused | **Plus** |
| Error handling | Nil-on-interface antipattern | Idiomatic stdlib usage | **Plus** |
| Lines changed | +349 / -13 | +253 / -33 | **Plus** |

**Key finding**: Qwen3-Coder missed 5 out of 13 agents (Windsurf, Aider, Continue, Copilot, OpenClaw) that needed symlink resolution. It also introduced three helper functions where one sufficed, including a `statPath()` that returns `os.FileInfo` (nil on error) — an antipattern in Go where `(FileInfo, error)` pairs are conventional.

Qwen3.5-Plus delivered a complete implementation with a single focused `resolvePath()` helper, covering all 13 agents with fewer lines of code.

### WO-16: SKILL.md Semantic Validation (New Feature)

| Criterion | Qwen3-Coder | Qwen3.5-Plus | Winner |
|-----------|:-----------:|:------------:|--------|
| All 5 checks implemented | Yes | Yes | Tie |
| Test infrastructure | Inline test literals | Fixture files (realistic) | **Plus** |
| API consistency | Mixed path/SkillFile args | Consistent SkillFile API | **Plus** |
| Efficiency | Re-reads file per check | Pre-computed in parser | **Plus** |
| Section name matching | Case-insensitive | Exact case only | **Coder** |
| Duplicate detection | Working (scans filesystem) | No-op (passes nil context) | **Coder** |

**Key finding**: Both models implemented all five validation checks. Qwen3.5-Plus showed better engineering discipline — fixture-based tests, consistent function signatures, and efficient pre-computation. However, Qwen3-Coder had two practical advantages: case-insensitive section matching (more robust) and a working duplicate skill name detector (Plus's implementation was a no-op due to nil context).

### Overall Quality Grade

| Dimension | Qwen3-Coder | Qwen3.5-Plus |
|-----------|:-----------:|:------------:|
| Correctness | B | **A** |
| Test quality | B+ | **A-** |
| Error handling | B | **A-** |
| Code style | B+ | **A-** |
| Scope discipline | A- | A- |
| Architecture | B | **A** |
| **Overall** | **B** | **A-** |

## Conclusion

**Qwen3.5-Plus is the better model for agentic Go coding tasks.**

The models are comparable in speed and token cost, but Qwen3.5-Plus consistently produces:

- **More complete implementations** — covers all cases, not just the obvious ones
- **Cleaner architecture** — consistent APIs, fewer unnecessary abstractions
- **Better test infrastructure** — fixture files over inline literals, testing behavior over implementation
- **More idiomatic Go** — follows standard library patterns, avoids antipatterns

Qwen3-Coder's advantages (case-insensitive matching, working duplicate detection) are minor fixes that can be patched in minutes. Qwen3.5-Plus's advantages (complete coverage, architectural consistency) reflect deeper reasoning about the task.

### Recommendation

Switch the default qwen runner from `coder-model` (Qwen3-Coder) to `qwen3.5-plus` (Qwen3.5-Plus). Keep `qwen-coder` as a named profile for future benchmarking.

## Reproducing This Benchmark

1. Create a task file with identical prompts assigned to different runners
2. Set `parallel_repo: true` so both run on isolated worktrees
3. Run with `tokencontrol run <task-file>`
4. Compare branches with `git show <commit>` for each model's output
5. Score on the six criteria above

The key to fair comparison: same prompt, same repo state, same execution environment, different model. Tokencontrol's worktree isolation ensures neither model's changes affect the other.
