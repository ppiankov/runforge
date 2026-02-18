package scan

// Checker is the interface all scan checks implement.
type Checker interface {
	ID() string
	Category() string
	Applies(repo *RepoInfo) bool
	Run(repo *RepoInfo) []Finding
}

// AllCheckers returns the complete list of check implementations.
func AllCheckers() []Checker {
	return []Checker{
		// structure
		&fileCheck{id: "missing-makefile", cat: "structure", file: "Makefile", sev: SeverityWarning,
			msg: "No Makefile found", sug: "Create a Makefile with standard targets: build, test, lint, fmt, clean. Follow the project's Go convention with go build, go test -race, and golangci-lint run targets.",
			promptFn: promptMakefile},
		&fileCheck{id: "missing-readme", cat: "structure", file: "README.md", sev: SeverityWarning,
			msg: "No README.md found", sug: "Create a README.md with: badges, one-line description, what it is, what it is NOT, philosophy, quick start, usage, architecture, known limitations, roadmap, license.",
			promptFn: promptReadme},
		&fileCheck{id: "missing-claude-md", cat: "structure", file: "CLAUDE.md", sev: SeverityInfo,
			msg: "No CLAUDE.md found", sug: "Create a CLAUDE.md with project-specific coding conventions, architecture notes, and constraints for AI coding agents.",
			promptFn: promptClaudeMd},
		&fileCheck{id: "missing-contributing", cat: "structure", file: "CONTRIBUTING.md", sev: SeverityInfo,
			msg: "No CONTRIBUTING.md found", sug: "Create a CONTRIBUTING.md with development setup, PR process, coding standards, and testing requirements.",
			promptFn: promptContributing},
		&fileCheck{id: "missing-license", cat: "structure", file: "LICENSE", sev: SeverityWarning,
			msg: "No LICENSE file found", sug: "Add a LICENSE file. The project typically uses MIT license.",
			promptFn: promptLicense},
		&fileCheck{id: "missing-changelog", cat: "structure", file: "CHANGELOG.md", sev: SeverityInfo,
			msg: "No CHANGELOG.md found", sug: "Create a CHANGELOG.md following Keep a Changelog format. Document notable changes per version.",
			promptFn: promptChangelog},
		&fileCheck{id: "missing-ci", cat: "structure", file: ".github/workflows/ci.yml", sev: SeverityCritical,
			msg: "No CI pipeline found", sug: "Create .github/workflows/ci.yml with jobs for: test (go test -race), lint (golangci-lint), fmt (gofmt check), and build (multi-platform).",
			promptFn: promptCI},
		&fileCheck{id: "missing-gitignore", cat: "structure", file: ".gitignore", sev: SeverityWarning,
			msg: "No .gitignore found", sug: "Create a .gitignore with standard Go ignores: binary name, *.exe, vendor/, coverage.out, .env, *.pem.",
			promptFn: promptGitignore},

		// go
		&fileCheck{id: "go-missing-golangci", cat: "go", file: ".golangci.yml", sev: SeverityWarning,
			msg: "No golangci-lint config found", sug: "Create .golangci.yml with linter settings. At minimum configure errcheck exclude-functions for fmt.Fprint variants.",
			langFilter: LangGo, promptFn: promptGolangciLint},
		&fileCheck{id: "go-missing-goreleaser", cat: "go", file: ".goreleaser.yml", sev: SeverityInfo,
			msg: "No GoReleaser config found", sug: "Create .goreleaser.yml for automated releases with multi-platform builds, Docker images, and Homebrew tap updates.",
			langFilter: LangGo, promptFn: promptGoreleaser},

		&goOutdatedCheck{},
		&goNoTestsCheck{},
		&goNoRaceCheck{},
		&goMissingDockerfileCheck{},

		// python
		&pyMissingProjectCheck{},
		&pyNoTestsCheck{},

		// security
		&secNoSecurityScanCheck{},
		&secEnvCommittedCheck{},
		&secHardcodedTokenCheck{},

		// ci
		&ciNoTestJobCheck{},
		&ciNoLintJobCheck{},
		&fileCheck{id: "ci-no-release", cat: "ci", file: ".github/workflows/release.yml", sev: SeverityInfo,
			msg: "No release workflow found", sug: "Create .github/workflows/release.yml triggered on tags. Use GoReleaser for multi-platform builds and Homebrew tap updates.",
			promptFn: promptRelease},
		&fileCheck{id: "ci-no-dependabot", cat: "ci", file: ".github/dependabot.yml", sev: SeverityInfo,
			msg: "No Dependabot config found", sug: "Create .github/dependabot.yml for automated dependency updates. Configure for gomod ecosystem with weekly schedule.",
			promptFn: promptDependabot},

		// quality
		&qualityNoCoverageCheck{},
		&qualityStaleWOCheck{},
		&qualityOrphanedTasksCheck{},
	}
}
