# Contributing to tokencontrol

## Prerequisites

- Go 1.25+ (see `go.mod` for exact version)
- [golangci-lint](https://golangci-lint.run/) for linting
- `make` for build commands

## Setup

```bash
git clone https://github.com/ppiankov/tokencontrol.git
cd tokencontrol
make build
make test
```

## Development Workflow

1. Create a branch from `main`
2. Make changes
3. Run verification:
   ```bash
   make build    # compile
   make test     # tests with -race
   make lint     # golangci-lint
   ```
4. Commit with conventional commit messages
5. Open a pull request

## Commit Messages

Format: `type: concise imperative statement`

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `perf`, `ci`, `build`

- One line, max 72 characters
- Lowercase after colon, no trailing period
- Say **what** changed, not every detail of how

Good: `feat: add user authentication`
Bad: `feat: add user authentication with JWT tokens, bcrypt hashing, middleware validation, rate limiting, and session management`

## Testing

- Tests are mandatory for all new code
- All tests run with `-race` flag
- Run scheduler-related changes with `-race -count=3`
- Test files go alongside source (Go convention)

## Code Style

- `gofmt` and `goimports` for formatting
- `golangci-lint` for static analysis — must pass with zero issues
- Comments explain "why", not "what"
- No magic numbers — name constants
- Early returns over deep nesting

## Project Structure

```
cmd/tokencontrol/       Entry point
internal/cli/           CLI commands (Cobra)
internal/config/        Config and task file loading
internal/task/          Task model, DAG, scheduler, scorer
internal/runner/        Runner backends and shared utilities
internal/scan/          Portfolio scanner
internal/reporter/      Output formatters (TUI, text, JSON)
internal/sentinel/      Sentinel loop daemon
internal/state/         Persistent task state
internal/generate/      Task file generator
internal/ingest/        Forgeaware import
```

## License

By contributing, you agree that your contributions will be licensed under the [BSL 1.1](LICENSE).
