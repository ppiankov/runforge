package scan

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// promptBuilder constructs detailed prompts for autonomous agent execution.
type promptBuilder struct {
	buf strings.Builder
}

func newPrompt() *promptBuilder {
	return &promptBuilder{}
}

func (b *promptBuilder) line(s string) *promptBuilder {
	b.buf.WriteString(s)
	b.buf.WriteByte('\n')
	return b
}

func (b *promptBuilder) blank() *promptBuilder {
	b.buf.WriteByte('\n')
	return b
}

func (b *promptBuilder) verification(lang Language) *promptBuilder {
	b.line("Verification:")
	switch lang {
	case LangGo, LangMulti:
		b.line("  make test && make lint")
	case LangPython:
		b.line("  pytest && ruff check . && black --check .")
	default:
		b.line("  Run existing tests and lint checks.")
	}
	return b
}

func (b *promptBuilder) constraints() *promptBuilder {
	b.blank()
	b.line("Constraints:")
	b.line("- Do NOT add features, refactor code, or make improvements beyond what is described")
	b.line("- Do NOT modify files unrelated to this task")
	b.line("- Do NOT add docstrings or comments to code you did not change")
	return b
}

func (b *promptBuilder) String() string {
	return strings.TrimSpace(b.buf.String())
}

// fileCheck is a generic checker that tests for a missing file.
type fileCheck struct {
	id         string
	cat        string
	file       string
	sev        Severity
	msg        string
	sug        string
	promptFn   func(*RepoInfo) string // builds detailed prompt; nil = use sug
	langFilter Language               // 0 = applies to all
}

func (c *fileCheck) ID() string       { return c.id }
func (c *fileCheck) Category() string { return c.cat }
func (c *fileCheck) Applies(r *RepoInfo) bool {
	if c.langFilter == LangUnknown {
		return true
	}
	return r.Language == c.langFilter || r.Language == LangMulti
}

func (c *fileCheck) Run(r *RepoInfo) []Finding {
	if fileExists(filepath.Join(r.Path, c.file)) {
		return nil
	}
	f := Finding{
		Repo: r.Name, Check: c.id, Category: c.cat,
		Severity: c.sev, Message: c.msg, Suggestion: c.sug,
	}
	if c.promptFn != nil {
		f.Prompt = c.promptFn(r)
	}
	return []Finding{f}
}

// --- fileCheck prompt builders ---

func promptMakefile(r *RepoInfo) string {
	p := newPrompt()
	p.line(fmt.Sprintf("Create a Makefile for the %s project %s.", r.Language, r.Name))
	p.blank()
	p.line("Required targets:")
	switch r.Language {
	case LangGo, LangMulti:
		p.line(fmt.Sprintf("  build: go build -o bin/%s ./cmd/%s (adjust path if cmd/ layout differs)", r.Name, r.Name))
		p.line("  test: go test -race ./...")
		p.line("  lint: golangci-lint run ./...")
		p.line("  fmt: gofmt -w .")
		p.line("  clean: rm -rf bin/ coverage.out")
	case LangPython:
		p.line("  test: pytest")
		p.line("  lint: ruff check . && black --check .")
		p.line("  fmt: black . && ruff check --fix .")
		p.line("  clean: find . -type d -name __pycache__ -exec rm -rf {} +")
	default:
		p.line("  test: run project tests")
		p.line("  lint: run project linter")
		p.line("  clean: remove build artifacts")
	}
	p.blank()
	p.line("Use tabs for indentation (required by Make).")
	p.line("Add .PHONY declarations for all targets.")
	p.constraints()
	return p.String()
}

func promptReadme(r *RepoInfo) string {
	p := newPrompt()
	p.line(fmt.Sprintf("Create README.md for %s.", r.Name))
	p.blank()
	p.line("Required sections (in order):")
	p.line("- CI/build badges")
	p.line("- One-line project description")
	p.line("- What it is (2-3 sentences)")
	p.line("- What it is NOT (explicit scope boundaries)")
	p.line("- Quick start (install + first command)")
	p.line("- Usage examples")
	p.line("- Architecture overview")
	p.line("- Known limitations")
	p.line("- Roadmap")
	p.line("- License")
	p.blank()
	p.line("Keep descriptions factual and concise. No marketing language.")
	p.constraints()
	return p.String()
}

func promptClaudeMd(r *RepoInfo) string {
	p := newPrompt()
	p.line(fmt.Sprintf("Create CLAUDE.md for %s with project-specific instructions for AI coding agents.", r.Name))
	p.blank()
	p.line("Include:")
	p.line("- Project architecture overview (key packages, data flow)")
	p.line("- Build and test commands (make targets)")
	p.line("- Coding conventions specific to this project")
	p.line("- Common patterns and how to follow them")
	p.line("- What NOT to do (project-specific anti-patterns)")
	p.constraints()
	return p.String()
}

func promptContributing(r *RepoInfo) string {
	p := newPrompt()
	p.line(fmt.Sprintf("Create CONTRIBUTING.md for %s.", r.Name))
	p.blank()
	p.line("Include:")
	p.line("- Development environment setup")
	p.line("- How to run tests locally")
	p.line("- PR process and review expectations")
	p.line("- Coding standards and style guide references")
	p.line("- Commit message format (conventional commits)")
	p.constraints()
	return p.String()
}

func promptLicense(_ *RepoInfo) string {
	p := newPrompt()
	p.line("Create a LICENSE file with the MIT license.")
	p.blank()
	p.line("Use the current year and copyright holder 'ppiankov'.")
	p.line("Use the standard MIT license text from https://opensource.org/licenses/MIT.")
	p.constraints()
	return p.String()
}

func promptChangelog(_ *RepoInfo) string {
	p := newPrompt()
	p.line("Create CHANGELOG.md following the Keep a Changelog format.")
	p.blank()
	p.line("Structure:")
	p.line("  # Changelog")
	p.line("  ## [Unreleased]")
	p.line("  ### Added")
	p.line("  ### Changed")
	p.line("  ### Fixed")
	p.blank()
	p.line("Add an initial entry for the current state of the project.")
	p.constraints()
	return p.String()
}

func promptCI(r *RepoInfo) string {
	p := newPrompt()
	p.line("Create .github/workflows/ci.yml with a CI pipeline.")
	p.blank()
	switch r.Language {
	case LangGo, LangMulti:
		p.line("Trigger: push and pull_request on main branch.")
		p.blank()
		p.line("Jobs:")
		p.line("  test:")
		p.line("    - uses: actions/checkout@v4")
		p.line("    - uses: actions/setup-go@v5 with latest Go version")
		p.line("    - run: make test")
		p.line("  lint:")
		p.line("    - uses: actions/checkout@v4")
		p.line("    - uses: actions/setup-go@v5")
		p.line("    - uses: golangci/golangci-lint-action@v6")
		p.line("  build:")
		p.line("    - uses: actions/checkout@v4")
		p.line("    - uses: actions/setup-go@v5")
		p.line("    - run: make build")
	case LangPython:
		p.line("Trigger: push and pull_request on main branch.")
		p.blank()
		p.line("Jobs:")
		p.line("  test:")
		p.line("    - uses: actions/checkout@v4")
		p.line("    - uses: actions/setup-python@v5")
		p.line("    - run: pip install -e '.[dev]' && pytest")
		p.line("  lint:")
		p.line("    - run: ruff check . && black --check .")
	default:
		p.line("Create a basic CI pipeline with test and lint jobs.")
	}
	p.constraints()
	return p.String()
}

func promptGitignore(r *RepoInfo) string {
	p := newPrompt()
	p.line("Create .gitignore with appropriate patterns.")
	p.blank()
	switch r.Language {
	case LangGo, LangMulti:
		p.line("Include:")
		p.line(fmt.Sprintf("  %s", r.Name))
		p.line("  bin/")
		p.line("  *.exe")
		p.line("  coverage.out")
		p.line("  vendor/")
		p.line("  .env")
		p.line("  *.pem")
		p.line("  .DS_Store")
	case LangPython:
		p.line("Include:")
		p.line("  __pycache__/")
		p.line("  *.pyc")
		p.line("  .venv/")
		p.line("  venv/")
		p.line("  dist/")
		p.line("  *.egg-info/")
		p.line("  .env")
		p.line("  .DS_Store")
	default:
		p.line("Include:")
		p.line("  .env")
		p.line("  .DS_Store")
		p.line("  *.log")
	}
	p.constraints()
	return p.String()
}

func promptGolangciLint(_ *RepoInfo) string {
	p := newPrompt()
	p.line("Create .golangci.yml with linter configuration.")
	p.blank()
	p.line("Include these linters: errcheck, gosimple, govet, ineffassign, staticcheck, unused.")
	p.line("Configure errcheck to exclude fmt.Fprint, fmt.Fprintf, fmt.Fprintln.")
	p.line("Set timeout to 5m.")
	p.constraints()
	return p.String()
}

func promptGoreleaser(r *RepoInfo) string {
	p := newPrompt()
	p.line(fmt.Sprintf("Create .goreleaser.yml for %s.", r.Name))
	p.blank()
	p.line("Configure:")
	p.line("- Multi-platform builds: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64")
	p.line(fmt.Sprintf("- Binary name: %s", r.Name))
	p.line("- LDFLAGS: -s -w -X main.version={{.Version}} -X main.commit={{.Commit}}")
	p.line("- Homebrew tap: ppiankov/homebrew-tap")
	p.line("- Archives: tar.gz for linux, zip for darwin")
	p.line("- Changelog: use conventional commits")
	p.constraints()
	return p.String()
}

func promptRelease(r *RepoInfo) string {
	p := newPrompt()
	p.line("Create .github/workflows/release.yml triggered on version tags.")
	p.blank()
	switch r.Language {
	case LangGo, LangMulti:
		p.line("Trigger: push tags matching 'v*'.")
		p.blank()
		p.line("Jobs:")
		p.line("  release:")
		p.line("    runs-on: ubuntu-latest")
		p.line("    steps:")
		p.line("      - uses: actions/checkout@v4")
		p.line("        with: { fetch-depth: 0 }")
		p.line("      - uses: actions/setup-go@v5")
		p.line("        with:")
		p.line("          go-version-file: 'go.mod'")
		p.line("      - uses: goreleaser/goreleaser-action@v6")
		p.line("        with:")
		p.line("          args: release --clean")
		p.line("        env:")
		p.line("          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}")
	default:
		p.line("Create a release workflow that builds and publishes on tag push.")
	}
	p.constraints()
	return p.String()
}

func promptDependabot(r *RepoInfo) string {
	p := newPrompt()
	p.line("Create .github/dependabot.yml for automated dependency updates.")
	p.blank()
	p.line("version: 2")
	p.line("updates:")
	switch r.Language {
	case LangGo, LangMulti:
		p.line("  - package-ecosystem: gomod")
		p.line("    directory: /")
		p.line("    schedule:")
		p.line("      interval: weekly")
	case LangPython:
		p.line("  - package-ecosystem: pip")
		p.line("    directory: /")
		p.line("    schedule:")
		p.line("      interval: weekly")
	default:
		p.line("  - package-ecosystem: <detect>")
		p.line("    directory: /")
		p.line("    schedule:")
		p.line("      interval: weekly")
	}
	p.line("  - package-ecosystem: github-actions")
	p.line("    directory: /")
	p.line("    schedule:")
	p.line("      interval: weekly")
	p.constraints()
	return p.String()
}

// --- Go checks ---

type goOutdatedCheck struct{}

func (c *goOutdatedCheck) ID() string       { return "go-outdated-version" }
func (c *goOutdatedCheck) Category() string { return "go" }
func (c *goOutdatedCheck) Applies(r *RepoInfo) bool {
	return r.Language == LangGo || r.Language == LangMulti
}

func (c *goOutdatedCheck) Run(r *RepoInfo) []Finding {
	gomod := filepath.Join(r.Path, "go.mod")
	data, err := os.ReadFile(gomod)
	if err != nil {
		return nil
	}
	ver := parseGoVersion(string(data))
	if ver == "" {
		return nil
	}
	if isGoVersionOutdated(ver) {
		p := newPrompt()
		p.line(fmt.Sprintf("Update Go version in go.mod from %s to 1.24.", ver))
		p.blank()
		p.line("Steps:")
		p.line("  go mod edit -go=1.24")
		p.line("  go mod tidy")
		p.blank()
		p.line("If any dependencies require updates, run: go get -u ./...")
		p.line("Fix any compilation errors from API changes.")
		p.constraints()
		p.blank()
		p.verification(r.Language)

		return []Finding{{
			Repo: r.Name, Check: c.ID(), Category: c.Category(),
			Severity:   SeverityWarning,
			Message:    fmt.Sprintf("go.mod specifies Go %s (< 1.24)", ver),
			Suggestion: fmt.Sprintf("Update go.mod to use Go 1.24 or later. Run: go mod edit -go=1.24 && go mod tidy. Current version: %s.", ver),
			Prompt:     p.String(),
		}}
	}
	return nil
}

var goVersionRe = regexp.MustCompile(`(?m)^go\s+([\d.]+)`)

func parseGoVersion(modContent string) string {
	m := goVersionRe.FindStringSubmatch(modContent)
	if m == nil {
		return ""
	}
	return m[1]
}

func isGoVersionOutdated(ver string) bool {
	parts := strings.SplitN(ver, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major := parts[0]
	minor := parts[1]
	if major != "1" {
		return false
	}
	// 1.24+ is acceptable
	switch minor {
	case "24", "25", "26", "27", "28", "29", "30":
		return false
	}
	return true
}

type goNoTestsCheck struct{}

func (c *goNoTestsCheck) ID() string       { return "go-no-tests" }
func (c *goNoTestsCheck) Category() string { return "go" }
func (c *goNoTestsCheck) Applies(r *RepoInfo) bool {
	return r.Language == LangGo || r.Language == LangMulti
}

func (c *goNoTestsCheck) Run(r *RepoInfo) []Finding {
	found := false
	_ = filepath.WalkDir(r.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		base := d.Name()
		if d.IsDir() && (base == "vendor" || base == ".git" || base == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(base, "_test.go") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if found {
		return nil
	}

	// collect Go packages for context
	pkgs := listGoPackages(r.Path)
	p := newPrompt()
	p.line(fmt.Sprintf("Add test files for the Go project %s.", r.Name))
	p.blank()
	if len(pkgs) > 0 {
		p.line("Packages that need tests:")
		for _, pkg := range pkgs {
			p.line(fmt.Sprintf("  %s — create %s_test.go", pkg, filepath.Base(pkg)))
		}
		p.blank()
	}
	p.line("Testing conventions:")
	p.line("- Use table-driven tests with descriptive subtest names")
	p.line("- Always run with -race flag")
	p.line("- Target > 85% coverage")
	p.line("- Tests must be deterministic — no flaky or probabilistic assertions")
	p.line("- Test file naming: <source>_test.go alongside the source file")
	p.constraints()
	p.blank()
	p.verification(r.Language)

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityCritical,
		Message:    "No Go test files found",
		Suggestion: "Add test files (*_test.go) for all packages. Tests are mandatory — target > 85% coverage. Use table-driven tests, -race flag, and deterministic assertions.",
		Prompt:     p.String(),
	}}
}

// listGoPackages returns relative paths of Go packages in the repo (max 20).
func listGoPackages(root string) []string {
	const maxPkgs = 20
	seen := make(map[string]bool)
	var pkgs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		base := d.Name()
		if d.IsDir() && (base == "vendor" || base == ".git" || base == "node_modules" || base == "testdata") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(base, ".go") && !strings.HasSuffix(base, "_test.go") {
			dir, _ := filepath.Rel(root, filepath.Dir(path))
			if dir == "" {
				dir = "."
			}
			if !seen[dir] {
				seen[dir] = true
				pkgs = append(pkgs, dir)
				if len(pkgs) >= maxPkgs {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	return pkgs
}

type goNoRaceCheck struct{}

func (c *goNoRaceCheck) ID() string       { return "go-no-race-flag" }
func (c *goNoRaceCheck) Category() string { return "go" }
func (c *goNoRaceCheck) Applies(r *RepoInfo) bool {
	return r.Language == LangGo || r.Language == LangMulti
}

func (c *goNoRaceCheck) Run(r *RepoInfo) []Finding {
	makefile := filepath.Join(r.Path, "Makefile")
	data, err := os.ReadFile(makefile)
	if err != nil {
		return nil
	}
	content := string(data)
	if strings.Contains(content, "-race") {
		return nil
	}
	if !strings.Contains(content, "go test") && !strings.Contains(content, "test:") {
		return nil
	}

	p := newPrompt()
	p.line("Add -race flag to all go test commands in the Makefile.")
	p.blank()
	p.line("Open Makefile and find every line containing 'go test'.")
	p.line("Add the -race flag if not already present.")
	p.blank()
	p.line("Example:")
	p.line("  Before: go test ./...")
	p.line("  After:  go test -race ./...")
	p.blank()
	p.line("Data races are undefined behavior in Go. The race detector")
	p.line("must always be enabled during tests.")
	p.constraints()
	p.blank()
	p.verification(r.Language)

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityWarning,
		Message:    "Makefile test target missing -race flag",
		Suggestion: "Add -race flag to go test commands in Makefile. Data races are undefined behavior in Go — the race detector must always be enabled during tests.",
		Prompt:     p.String(),
	}}
}

type goMissingDockerfileCheck struct{}

func (c *goMissingDockerfileCheck) ID() string       { return "go-missing-dockerfile" }
func (c *goMissingDockerfileCheck) Category() string { return "go" }
func (c *goMissingDockerfileCheck) Applies(r *RepoInfo) bool {
	return (r.Language == LangGo || r.Language == LangMulti) && r.HasCmd
}

func (c *goMissingDockerfileCheck) Run(r *RepoInfo) []Finding {
	matches, _ := filepath.Glob(filepath.Join(r.Path, "Dockerfile*"))
	if len(matches) > 0 {
		return nil
	}

	p := newPrompt()
	p.line(fmt.Sprintf("Create a multi-stage Dockerfile for %s.", r.Name))
	p.blank()
	p.line("Stage 1 (build):")
	p.line("  FROM golang:1.24-alpine AS build")
	p.line("  WORKDIR /src")
	p.line("  COPY go.mod go.sum ./")
	p.line("  RUN go mod download")
	p.line("  COPY . .")
	p.line(fmt.Sprintf("  RUN CGO_ENABLED=0 go build -o /bin/%s ./cmd/%s", r.Name, r.Name))
	p.blank()
	p.line("Stage 2 (runtime):")
	p.line("  FROM alpine:latest")
	p.line("  RUN apk --no-cache add ca-certificates")
	p.line(fmt.Sprintf("  COPY --from=build /bin/%s /usr/local/bin/", r.Name))
	p.line(fmt.Sprintf("  ENTRYPOINT [\"%s\"]", r.Name))
	p.constraints()

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityInfo,
		Message:    "No Dockerfile found for Go binary project",
		Suggestion: "Create a multi-stage Dockerfile: build stage with golang:1.24-alpine, runtime stage with alpine:latest or scratch. Copy the compiled binary, set ENTRYPOINT.",
		Prompt:     p.String(),
	}}
}

// --- Python checks ---

type pyMissingProjectCheck struct{}

func (c *pyMissingProjectCheck) ID() string       { return "py-missing-pyproject" }
func (c *pyMissingProjectCheck) Category() string { return "python" }
func (c *pyMissingProjectCheck) Applies(r *RepoInfo) bool {
	return r.Language == LangPython || r.Language == LangMulti
}

func (c *pyMissingProjectCheck) Run(r *RepoInfo) []Finding {
	if fileExists(filepath.Join(r.Path, "pyproject.toml")) || fileExists(filepath.Join(r.Path, "setup.py")) {
		return nil
	}

	p := newPrompt()
	p.line(fmt.Sprintf("Create pyproject.toml for %s.", r.Name))
	p.blank()
	p.line("Include:")
	p.line("  [build-system]")
	p.line("  requires = [\"setuptools>=68.0\"]")
	p.line("  build-backend = \"setuptools.build_meta\"")
	p.blank()
	p.line("  [project]")
	p.line(fmt.Sprintf("  name = \"%s\"", r.Name))
	p.line("  version = \"0.1.0\"")
	p.blank()
	p.line("  [tool.black]")
	p.line("  line-length = 100")
	p.blank()
	p.line("  [tool.ruff]")
	p.line("  line-length = 100")
	p.blank()
	p.line("  [project.optional-dependencies]")
	p.line("  dev = [\"pytest\", \"ruff\", \"black\"]")
	p.constraints()
	p.blank()
	p.verification(r.Language)

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityWarning,
		Message:    "No pyproject.toml or setup.py found",
		Suggestion: "Create pyproject.toml with build system, project metadata, and tool configurations for Black (100 chars) and Ruff.",
		Prompt:     p.String(),
	}}
}

type pyNoTestsCheck struct{}

func (c *pyNoTestsCheck) ID() string       { return "py-no-tests" }
func (c *pyNoTestsCheck) Category() string { return "python" }
func (c *pyNoTestsCheck) Applies(r *RepoInfo) bool {
	return r.Language == LangPython || r.Language == LangMulti
}

func (c *pyNoTestsCheck) Run(r *RepoInfo) []Finding {
	found := false
	_ = filepath.WalkDir(r.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		base := d.Name()
		if d.IsDir() && (base == ".venv" || base == "venv" || base == ".git" || base == "__pycache__" || base == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && (strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")) && strings.HasSuffix(base, ".py") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if found {
		return nil
	}

	p := newPrompt()
	p.line(fmt.Sprintf("Add pytest test files for %s.", r.Name))
	p.blank()
	p.line("Create tests/ directory with:")
	p.line("  tests/__init__.py")
	p.line("  tests/conftest.py — shared fixtures")
	p.line("  tests/test_<module>.py — one file per source module")
	p.blank()
	p.line("Testing conventions:")
	p.line("- Name test files test_*.py")
	p.line("- Use pytest fixtures for setup/teardown")
	p.line("- Tests must be deterministic — no flaky assertions")
	p.line("- Target > 85% coverage")
	p.constraints()
	p.blank()
	p.verification(r.Language)

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityCritical,
		Message:    "No Python test files found",
		Suggestion: "Add test files in tests/ directory using pytest. Name files test_*.py. Use fixtures, deterministic assertions, and target > 85% coverage.",
		Prompt:     p.String(),
	}}
}

// --- Security checks ---

type secNoSecurityScanCheck struct{}

func (c *secNoSecurityScanCheck) ID() string       { return "sec-no-security-scan" }
func (c *secNoSecurityScanCheck) Category() string { return "security" }
func (c *secNoSecurityScanCheck) Applies(r *RepoInfo) bool {
	return isDir(filepath.Join(r.Path, ".github", "workflows"))
}

func (c *secNoSecurityScanCheck) Run(r *RepoInfo) []Finding {
	ciPath := filepath.Join(r.Path, ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(ciPath)
	if err != nil {
		return nil
	}
	content := strings.ToLower(string(data))
	if strings.Contains(content, "trivy") || strings.Contains(content, "security") || strings.Contains(content, "snyk") || strings.Contains(content, "codeql") {
		return nil
	}

	p := newPrompt()
	p.line("Add a security scanning job to .github/workflows/ci.yml.")
	p.blank()
	p.line("Add this job to the existing workflow (do NOT remove existing jobs):")
	p.blank()
	p.line("  security:")
	p.line("    runs-on: ubuntu-latest")
	p.line("    steps:")
	p.line("      - uses: actions/checkout@v4")
	p.line("      - uses: aquasecurity/trivy-action@master")
	p.line("        with:")
	p.line("          scan-type: 'fs'")
	p.line("          severity: 'HIGH,CRITICAL'")
	p.line("          format: 'sarif'")
	p.line("          output: 'trivy-results.sarif'")
	p.line("      - uses: github/codeql-action/upload-sarif@v3")
	p.line("        with:")
	p.line("          sarif_file: 'trivy-results.sarif'")
	p.blank()
	p.line("IMPORTANT: Do NOT remove or modify existing CI jobs.")
	p.constraints()

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityWarning,
		Message:    "CI has no security scanning job",
		Suggestion: "Add a Trivy filesystem scan job to .github/workflows/ci.yml. Use aquasecurity/trivy-action with severity HIGH,CRITICAL and upload results to GitHub Security tab via sarif format.",
		Prompt:     p.String(),
	}}
}

type secEnvCommittedCheck struct{}

func (c *secEnvCommittedCheck) ID() string               { return "sec-env-committed" }
func (c *secEnvCommittedCheck) Category() string         { return "security" }
func (c *secEnvCommittedCheck) Applies(_ *RepoInfo) bool { return true }

func (c *secEnvCommittedCheck) Run(r *RepoInfo) []Finding {
	envPath := filepath.Join(r.Path, ".env")
	if !fileExists(envPath) {
		return nil
	}
	// check if .env is in .gitignore
	gitignore := filepath.Join(r.Path, ".gitignore")
	if data, err := os.ReadFile(gitignore); err == nil {
		if strings.Contains(string(data), ".env") {
			return nil
		}
	}

	p := newPrompt()
	p.line("Fix committed .env file — this is a security risk.")
	p.blank()
	p.line("Steps:")
	p.line("1. Add .env to .gitignore (create .gitignore if missing)")
	p.line("2. Remove .env from git tracking: git rm --cached .env")
	p.line("3. Create .env.example with placeholder values (no real secrets)")
	p.blank()
	p.line("If .env contains real credentials, they are compromised and must be rotated.")
	p.constraints()

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityCritical,
		Message:    ".env file exists and is not in .gitignore",
		Suggestion: "Add .env to .gitignore and remove it from git tracking: git rm --cached .env. Check git history for leaked secrets and rotate any exposed credentials.",
		Prompt:     p.String(),
	}}
}

type secHardcodedTokenCheck struct{}

func (c *secHardcodedTokenCheck) ID() string               { return "sec-hardcoded-token" }
func (c *secHardcodedTokenCheck) Category() string         { return "security" }
func (c *secHardcodedTokenCheck) Applies(_ *RepoInfo) bool { return true }

var tokenPattern = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password)\s*[:=]\s*["']([^"']{16,})["']`)

// source file extensions to scan
var sourceExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".ts": true,
	".yaml": true, ".yml": true, ".toml": true, ".json": true,
}

func (c *secHardcodedTokenCheck) Run(r *RepoInfo) []Finding {
	var findings []Finding
	_ = filepath.WalkDir(r.Path, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		base := d.Name()
		if d.IsDir() {
			if base == "vendor" || base == ".git" || base == "node_modules" || base == "testdata" || base == ".venv" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(base)
		if !sourceExts[ext] {
			return nil
		}
		// skip test files
		if strings.HasSuffix(base, "_test.go") || strings.HasPrefix(base, "test_") {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if tokenPattern.MatchString(line) {
				// skip common false positives
				lower := strings.ToLower(line)
				if strings.Contains(lower, "example") || strings.Contains(lower, "placeholder") ||
					strings.Contains(lower, "xxx") || strings.Contains(lower, "todo") ||
					strings.Contains(lower, "env:") || strings.Contains(lower, "os.getenv") ||
					strings.Contains(lower, "${") || strings.Contains(lower, "$(") {
					continue
				}
				rel, _ := filepath.Rel(r.Path, path)

				p := newPrompt()
				p.line(fmt.Sprintf("Remove hardcoded secret from %s:%d.", rel, lineNum))
				p.blank()
				p.line("Replace the hardcoded value with an environment variable:")
				switch filepath.Ext(base) {
				case ".go":
					p.line(fmt.Sprintf("  value := os.Getenv(\"SECRET_NAME\")  // was hardcoded at %s:%d", rel, lineNum))
				case ".py":
					p.line(fmt.Sprintf("  value = os.environ[\"SECRET_NAME\"]  # was hardcoded at %s:%d", rel, lineNum))
				default:
					p.line(fmt.Sprintf("  Replace literal value at %s:%d with environment variable reference.", rel, lineNum))
				}
				p.blank()
				p.line("Add the variable name to .env.example with a placeholder value.")
				p.line("Never commit real credentials to source control.")
				p.constraints()

				findings = append(findings, Finding{
					Repo: r.Name, Check: c.ID(), Category: c.Category(),
					Severity:   SeverityCritical,
					Message:    fmt.Sprintf("Possible hardcoded secret at %s:%d", rel, lineNum),
					Suggestion: fmt.Sprintf("Move the secret in %s:%d to an environment variable or secrets manager. Never commit credentials to source control.", rel, lineNum),
					Prompt:     p.String(),
				})
				// one finding per file is enough
				return nil
			}
		}
		return nil
	})
	return findings
}

// --- CI checks ---

type ciNoTestJobCheck struct{}

func (c *ciNoTestJobCheck) ID() string       { return "ci-no-test-job" }
func (c *ciNoTestJobCheck) Category() string { return "ci" }
func (c *ciNoTestJobCheck) Applies(r *RepoInfo) bool {
	return fileExists(filepath.Join(r.Path, ".github", "workflows", "ci.yml"))
}

func (c *ciNoTestJobCheck) Run(r *RepoInfo) []Finding {
	data, err := os.ReadFile(filepath.Join(r.Path, ".github", "workflows", "ci.yml"))
	if err != nil {
		return nil
	}
	content := strings.ToLower(string(data))
	if strings.Contains(content, "make test") || strings.Contains(content, "go test") || strings.Contains(content, "pytest") {
		return nil
	}

	p := newPrompt()
	p.line("Add a test job to .github/workflows/ci.yml.")
	p.blank()
	p.line("Add this job to the existing workflow (do NOT remove existing jobs):")
	p.blank()
	switch r.Language {
	case LangGo, LangMulti:
		p.line("  test:")
		p.line("    runs-on: ubuntu-latest")
		p.line("    steps:")
		p.line("      - uses: actions/checkout@v4")
		p.line("      - uses: actions/setup-go@v5")
		p.line("        with:")
		p.line("          go-version-file: 'go.mod'")
		p.line("      - run: make test")
	case LangPython:
		p.line("  test:")
		p.line("    runs-on: ubuntu-latest")
		p.line("    steps:")
		p.line("      - uses: actions/checkout@v4")
		p.line("      - uses: actions/setup-python@v5")
		p.line("      - run: pip install -e '.[dev]' && pytest")
	default:
		p.line("  test:")
		p.line("    runs-on: ubuntu-latest")
		p.line("    steps:")
		p.line("      - uses: actions/checkout@v4")
		p.line("      - run: make test")
	}
	p.blank()
	p.line("IMPORTANT: Do NOT remove or modify existing CI jobs.")
	p.constraints()

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityWarning,
		Message:    "CI workflow has no test step",
		Suggestion: "Add a test job to .github/workflows/ci.yml that runs 'make test' or 'go test -race ./...' on every push and PR.",
		Prompt:     p.String(),
	}}
}

type ciNoLintJobCheck struct{}

func (c *ciNoLintJobCheck) ID() string       { return "ci-no-lint-job" }
func (c *ciNoLintJobCheck) Category() string { return "ci" }
func (c *ciNoLintJobCheck) Applies(r *RepoInfo) bool {
	return fileExists(filepath.Join(r.Path, ".github", "workflows", "ci.yml"))
}

func (c *ciNoLintJobCheck) Run(r *RepoInfo) []Finding {
	data, err := os.ReadFile(filepath.Join(r.Path, ".github", "workflows", "ci.yml"))
	if err != nil {
		return nil
	}
	content := strings.ToLower(string(data))
	if strings.Contains(content, "golangci-lint") || strings.Contains(content, "make lint") || strings.Contains(content, "ruff") || strings.Contains(content, "black") {
		return nil
	}

	p := newPrompt()
	p.line("Add a lint job to .github/workflows/ci.yml.")
	p.blank()
	p.line("Add this job to the existing workflow (do NOT remove existing jobs):")
	p.blank()
	switch r.Language {
	case LangGo, LangMulti:
		p.line("  lint:")
		p.line("    runs-on: ubuntu-latest")
		p.line("    steps:")
		p.line("      - uses: actions/checkout@v4")
		p.line("      - uses: actions/setup-go@v5")
		p.line("        with:")
		p.line("          go-version-file: 'go.mod'")
		p.line("      - uses: golangci/golangci-lint-action@v6")
	case LangPython:
		p.line("  lint:")
		p.line("    runs-on: ubuntu-latest")
		p.line("    steps:")
		p.line("      - uses: actions/checkout@v4")
		p.line("      - uses: actions/setup-python@v5")
		p.line("      - run: pip install ruff black && ruff check . && black --check .")
	default:
		p.line("  lint:")
		p.line("    runs-on: ubuntu-latest")
		p.line("    steps:")
		p.line("      - uses: actions/checkout@v4")
		p.line("      - run: make lint")
	}
	p.blank()
	p.line("IMPORTANT: Do NOT remove or modify existing CI jobs.")
	p.constraints()

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityWarning,
		Message:    "CI workflow has no lint step",
		Suggestion: "Add a lint job to .github/workflows/ci.yml. For Go: use golangci/golangci-lint-action. For Python: use ruff check and black --check.",
		Prompt:     p.String(),
	}}
}

// --- Quality checks ---

type qualityNoCoverageCheck struct{}

func (c *qualityNoCoverageCheck) ID() string       { return "quality-no-coverage" }
func (c *qualityNoCoverageCheck) Category() string { return "quality" }
func (c *qualityNoCoverageCheck) Applies(r *RepoInfo) bool {
	return r.Language == LangGo || r.Language == LangPython || r.Language == LangMulti
}

func (c *qualityNoCoverageCheck) Run(r *RepoInfo) []Finding {
	// check CI
	ciPath := filepath.Join(r.Path, ".github", "workflows", "ci.yml")
	if data, err := os.ReadFile(ciPath); err == nil {
		content := strings.ToLower(string(data))
		if strings.Contains(content, "cover") || strings.Contains(content, "coverage") || strings.Contains(content, "codecov") {
			return nil
		}
	}
	// check Makefile
	mkPath := filepath.Join(r.Path, "Makefile")
	if data, err := os.ReadFile(mkPath); err == nil {
		if strings.Contains(string(data), "-cover") {
			return nil
		}
	}

	p := newPrompt()
	p.line("Add test coverage tracking.")
	p.blank()
	switch r.Language {
	case LangGo, LangMulti:
		p.line("In Makefile, update the test target:")
		p.line("  test:")
		p.line("  \tgo test -race -coverprofile=coverage.out ./...")
		p.blank()
		p.line("Add a coverage target:")
		p.line("  coverage:")
		p.line("  \tgo tool cover -html=coverage.out -o coverage.html")
	case LangPython:
		p.line("Add pytest-cov to dev dependencies and update test command:")
		p.line("  pytest --cov=src --cov-report=html")
	}
	p.blank()
	p.line("Target > 85% coverage.")
	p.constraints()

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityInfo,
		Message:    "No test coverage tracking found",
		Suggestion: "Add -coverprofile=coverage.out to go test commands in Makefile and CI. Consider uploading to Codecov for trend tracking. Target > 85% coverage.",
		Prompt:     p.String(),
	}}
}

type qualityStaleWOCheck struct{}

func (c *qualityStaleWOCheck) ID() string       { return "quality-stale-wo" }
func (c *qualityStaleWOCheck) Category() string { return "quality" }
func (c *qualityStaleWOCheck) Applies(r *RepoInfo) bool {
	return fileExists(filepath.Join(r.Path, "docs", "work-orders.md"))
}

func (c *qualityStaleWOCheck) Run(r *RepoInfo) []Finding {
	data, err := os.ReadFile(filepath.Join(r.Path, "docs", "work-orders.md"))
	if err != nil {
		return nil
	}
	content := string(data)
	// count planned (not done) WOs by looking for headers without [DONE] or ✅
	planned := 0
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "### WO-") && !strings.HasPrefix(line, "## WO-") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "[done]") || strings.Contains(lower, "✅") || strings.Contains(lower, "done") {
			continue
		}
		planned++
	}
	if planned == 0 {
		return nil
	}

	p := newPrompt()
	p.line(fmt.Sprintf("Execute %d pending work orders from docs/work-orders.md.", planned))
	p.blank()
	p.line("Steps:")
	p.line(fmt.Sprintf("1. Run: runforge generate --repos-dir <dir> --filter-repo %s", r.Name))
	p.line("2. Review generated task file")
	p.line("3. Run: runforge run --tasks <generated-file>.json")
	p.blank()
	p.line("Or manually review each WO and mark completed ones as [DONE].")
	p.constraints()

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityInfo,
		Message:    fmt.Sprintf("%d planned work orders in docs/work-orders.md", planned),
		Suggestion: fmt.Sprintf("Review %d pending work orders in docs/work-orders.md. Consider generating runforge tasks with 'runforge generate' to execute them.", planned),
		Prompt:     p.String(),
	}}
}

type qualityOrphanedTasksCheck struct{}

func (c *qualityOrphanedTasksCheck) ID() string               { return "quality-orphaned-tasks" }
func (c *qualityOrphanedTasksCheck) Category() string         { return "quality" }
func (c *qualityOrphanedTasksCheck) Applies(_ *RepoInfo) bool { return true }

func (c *qualityOrphanedTasksCheck) Run(r *RepoInfo) []Finding {
	matches, _ := filepath.Glob(filepath.Join(r.Path, "runforge-*.json"))
	if len(matches) == 0 {
		return nil
	}
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = filepath.Base(m)
	}

	p := newPrompt()
	p.line(fmt.Sprintf("Process %d orphaned runforge task file(s) in %s.", len(matches), r.Name))
	p.blank()
	p.line("Files found:")
	for _, name := range names {
		p.line(fmt.Sprintf("  %s", name))
	}
	p.blank()
	p.line("Options:")
	p.line(fmt.Sprintf("  Run:    runforge run --tasks '%s/runforge-*.json'", r.Name))
	p.line("  Clean:  rm <file>.json  (if tasks are already completed)")
	p.constraints()

	return []Finding{{
		Repo: r.Name, Check: c.ID(), Category: c.Category(),
		Severity:   SeverityWarning,
		Message:    fmt.Sprintf("%d orphaned runforge task file(s): %s", len(matches), strings.Join(names, ", ")),
		Suggestion: fmt.Sprintf("Run pending tasks with 'runforge run --tasks %s/runforge-*.json' or remove completed task files. Orphaned task files indicate work that was planned but not executed.", r.Name),
		Prompt:     p.String(),
	}}
}
