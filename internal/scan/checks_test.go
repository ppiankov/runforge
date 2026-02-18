package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeRepo creates a minimal git repo fixture with the given files.
// files is a map of relative path → content.
func makeRepo(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  Severity
	}{
		{"critical", SeverityCritical},
		{"CRITICAL", SeverityCritical},
		{"warning", SeverityWarning},
		{"info", SeverityInfo},
		{"bogus", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := ParseSeverity(tt.input)
		if got != tt.want {
			t.Errorf("ParseSeverity(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestSeverityString(t *testing.T) {
	tests := []struct {
		sev  Severity
		want string
	}{
		{SeverityCritical, "critical"},
		{SeverityWarning, "warning"},
		{SeverityInfo, "info"},
		{Severity(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.sev.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", tt.sev, got, tt.want)
		}
	}
}

func TestDetectRepo_NotGitDir(t *testing.T) {
	dir := t.TempDir()
	if got := DetectRepo(dir); got != nil {
		t.Errorf("DetectRepo on non-git dir returned %+v, want nil", got)
	}
}

func TestDetectRepo_GoRepo(t *testing.T) {
	dir := makeRepo(t, "myrepo", map[string]string{
		"go.mod": "module example.com/myrepo\n\ngo 1.24\n",
	})
	info := DetectRepo(dir)
	if info == nil {
		t.Fatal("DetectRepo returned nil for valid go repo")
	}
	if info.Language != LangGo {
		t.Errorf("Language = %v, want LangGo", info.Language)
	}
	if info.Name != "myrepo" {
		t.Errorf("Name = %q, want %q", info.Name, "myrepo")
	}
}

func TestDetectRepo_PythonRepo(t *testing.T) {
	dir := makeRepo(t, "pyrepo", map[string]string{
		"pyproject.toml": "[project]\nname = \"pyrepo\"\n",
	})
	info := DetectRepo(dir)
	if info == nil {
		t.Fatal("DetectRepo returned nil")
	}
	if info.Language != LangPython {
		t.Errorf("Language = %v, want LangPython", info.Language)
	}
}

func TestDetectRepo_MultiLang(t *testing.T) {
	dir := makeRepo(t, "multi", map[string]string{
		"go.mod":         "module example.com/multi\n\ngo 1.24\n",
		"pyproject.toml": "[project]\nname = \"multi\"\n",
	})
	info := DetectRepo(dir)
	if info == nil {
		t.Fatal("DetectRepo returned nil")
	}
	if info.Language != LangMulti {
		t.Errorf("Language = %v, want LangMulti", info.Language)
	}
}

func TestDetectRepo_HasCmd(t *testing.T) {
	dir := makeRepo(t, "cmdrepo", map[string]string{
		"go.mod":           "module example.com/cmdrepo\n\ngo 1.24\n",
		"cmd/main/main.go": "package main\n",
	})
	info := DetectRepo(dir)
	if info == nil {
		t.Fatal("DetectRepo returned nil")
	}
	if !info.HasCmd {
		t.Error("HasCmd = false, want true")
	}
}

func TestDetectRepo_HasDocs(t *testing.T) {
	dir := makeRepo(t, "docsrepo", map[string]string{
		"go.mod":              "module example.com/docsrepo\n\ngo 1.24\n",
		"docs/work-orders.md": "# WOs\n",
	})
	info := DetectRepo(dir)
	if info == nil {
		t.Fatal("DetectRepo returned nil")
	}
	if !info.HasDocs {
		t.Error("HasDocs = false, want true")
	}
}

// --- fileCheck tests ---

func TestFileCheck_MissingFile(t *testing.T) {
	dir := makeRepo(t, "bare", map[string]string{})
	repo := DetectRepo(dir)

	c := &fileCheck{id: "missing-readme", cat: "structure", file: "README.md", sev: SeverityWarning, msg: "No README.md"}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Check != "missing-readme" {
		t.Errorf("Check = %q, want %q", findings[0].Check, "missing-readme")
	}
}

func TestFileCheck_FilePresent(t *testing.T) {
	dir := makeRepo(t, "hasreadme", map[string]string{
		"README.md": "# Hello\n",
	})
	repo := DetectRepo(dir)

	c := &fileCheck{id: "missing-readme", cat: "structure", file: "README.md", sev: SeverityWarning, msg: "No README.md"}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestFileCheck_LangFilter(t *testing.T) {
	dir := makeRepo(t, "pyonly", map[string]string{
		"pyproject.toml": "[project]\n",
	})
	repo := DetectRepo(dir)

	goCheck := &fileCheck{id: "go-lint", cat: "go", file: ".golangci.yml", sev: SeverityWarning, langFilter: LangGo}
	if goCheck.Applies(repo) {
		t.Error("Go check should not apply to Python repo")
	}

	// multi-lang repo should match Go-filtered checks
	multiDir := makeRepo(t, "multi", map[string]string{
		"go.mod":         "module m\n\ngo 1.24\n",
		"pyproject.toml": "[project]\n",
	})
	multiRepo := DetectRepo(multiDir)
	if !goCheck.Applies(multiRepo) {
		t.Error("Go check should apply to multi-lang repo")
	}
}

// --- Go checks ---

func TestGoOutdatedVersion_Old(t *testing.T) {
	dir := makeRepo(t, "oldgo", map[string]string{
		"go.mod": "module example.com/oldgo\n\ngo 1.21\n",
	})
	repo := DetectRepo(dir)

	c := &goOutdatedCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for Go 1.21, got %d", len(findings))
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("Severity = %v, want warning", findings[0].Severity)
	}
}

func TestGoOutdatedVersion_Current(t *testing.T) {
	dir := makeRepo(t, "newgo", map[string]string{
		"go.mod": "module example.com/newgo\n\ngo 1.24\n",
	})
	repo := DetectRepo(dir)

	c := &goOutdatedCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for Go 1.24, got %d", len(findings))
	}
}

func TestParseGoVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"module foo\n\ngo 1.24\n", "1.24"},
		{"module foo\n\ngo 1.21.3\n", "1.21.3"},
		{"module foo\n", ""},
	}
	for _, tt := range tests {
		got := parseGoVersion(tt.input)
		if got != tt.want {
			t.Errorf("parseGoVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsGoVersionOutdated(t *testing.T) {
	tests := []struct {
		ver  string
		want bool
	}{
		{"1.21", true},
		{"1.22", true},
		{"1.23", true},
		{"1.24", false},
		{"1.25", false},
		{"1.24.1", false},
	}
	for _, tt := range tests {
		got := isGoVersionOutdated(tt.ver)
		if got != tt.want {
			t.Errorf("isGoVersionOutdated(%q) = %v, want %v", tt.ver, got, tt.want)
		}
	}
}

func TestGoNoTests_WithTests(t *testing.T) {
	dir := makeRepo(t, "hastests", map[string]string{
		"go.mod":        "module m\n\ngo 1.24\n",
		"foo_test.go":   "package main\n",
		"internal/a.go": "package internal\n",
	})
	repo := DetectRepo(dir)

	c := &goNoTestsCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestGoNoTests_NoTests(t *testing.T) {
	dir := makeRepo(t, "notests", map[string]string{
		"go.mod":  "module m\n\ngo 1.24\n",
		"main.go": "package main\n",
	})
	repo := DetectRepo(dir)

	c := &goNoTestsCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("Severity = %v, want critical", findings[0].Severity)
	}
}

func TestGoNoRace_MissingFlag(t *testing.T) {
	dir := makeRepo(t, "norace", map[string]string{
		"go.mod":   "module m\n\ngo 1.24\n",
		"Makefile": "test:\n\tgo test ./...\n",
	})
	repo := DetectRepo(dir)

	c := &goNoRaceCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestGoNoRace_HasFlag(t *testing.T) {
	dir := makeRepo(t, "hasrace", map[string]string{
		"go.mod":   "module m\n\ngo 1.24\n",
		"Makefile": "test:\n\tgo test -race ./...\n",
	})
	repo := DetectRepo(dir)

	c := &goNoRaceCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestGoMissingDockerfile_NoCmdDir(t *testing.T) {
	dir := makeRepo(t, "nobin", map[string]string{
		"go.mod": "module m\n\ngo 1.24\n",
	})
	repo := DetectRepo(dir)

	c := &goMissingDockerfileCheck{}
	if c.Applies(repo) {
		t.Error("should not apply to repo without cmd/")
	}
}

func TestGoMissingDockerfile_CmdNoDockerfile(t *testing.T) {
	dir := makeRepo(t, "nodf", map[string]string{
		"go.mod":           "module m\n\ngo 1.24\n",
		"cmd/main/main.go": "package main\n",
	})
	repo := DetectRepo(dir)

	c := &goMissingDockerfileCheck{}
	if !c.Applies(repo) {
		t.Fatal("should apply to Go repo with cmd/")
	}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestGoMissingDockerfile_HasDockerfile(t *testing.T) {
	dir := makeRepo(t, "hasdf", map[string]string{
		"go.mod":           "module m\n\ngo 1.24\n",
		"cmd/main/main.go": "package main\n",
		"Dockerfile":       "FROM golang:1.24\n",
	})
	repo := DetectRepo(dir)

	c := &goMissingDockerfileCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

// --- Python checks ---

func TestPyNoTests_WithTests(t *testing.T) {
	dir := makeRepo(t, "pytests", map[string]string{
		"pyproject.toml":     "[project]\n",
		"tests/test_main.py": "def test_hello(): pass\n",
	})
	repo := DetectRepo(dir)

	c := &pyNoTestsCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestPyNoTests_NoTests(t *testing.T) {
	dir := makeRepo(t, "pynotests", map[string]string{
		"pyproject.toml": "[project]\n",
		"src/main.py":    "print('hello')\n",
	})
	repo := DetectRepo(dir)

	c := &pyNoTestsCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

// --- Security checks ---

func TestSecEnvCommitted_EnvExistsNotIgnored(t *testing.T) {
	dir := makeRepo(t, "envleak", map[string]string{
		".env": "SECRET=hunter2\n",
	})
	repo := DetectRepo(dir)

	c := &secEnvCommittedCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("Severity = %v, want critical", findings[0].Severity)
	}
}

func TestSecEnvCommitted_EnvIgnored(t *testing.T) {
	dir := makeRepo(t, "envsafe", map[string]string{
		".env":       "SECRET=hunter2\n",
		".gitignore": ".env\n",
	})
	repo := DetectRepo(dir)

	c := &secEnvCommittedCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestSecEnvCommitted_NoEnv(t *testing.T) {
	dir := makeRepo(t, "noenv", map[string]string{})
	repo := DetectRepo(dir)

	c := &secEnvCommittedCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestSecHardcodedToken_Found(t *testing.T) {
	dir := makeRepo(t, "tokenrepo", map[string]string{
		"go.mod":  "module m\n\ngo 1.24\n",
		"main.go": "package main\nvar apiKey = \"sk-1234567890abcdef1234\"\n",
	})
	repo := DetectRepo(dir)

	c := &secHardcodedTokenCheck{}
	findings := c.Run(repo)
	if len(findings) < 1 {
		t.Fatal("expected at least 1 finding for hardcoded token")
	}
	if findings[0].Severity != SeverityCritical {
		t.Errorf("Severity = %v, want critical", findings[0].Severity)
	}
}

func TestSecHardcodedToken_FalsePositiveFiltered(t *testing.T) {
	dir := makeRepo(t, "saferepo", map[string]string{
		"go.mod":  "module m\n\ngo 1.24\n",
		"main.go": "package main\nvar apiKey = os.Getenv(\"API_KEY\")\n",
	})
	repo := DetectRepo(dir)

	c := &secHardcodedTokenCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for env var reference, got %d", len(findings))
	}
}

func TestSecHardcodedToken_SkipsTestFiles(t *testing.T) {
	dir := makeRepo(t, "testrepo", map[string]string{
		"go.mod":       "module m\n\ngo 1.24\n",
		"main_test.go": "package main\nvar token = \"test-token-1234567890abcdef\"\n",
	})
	repo := DetectRepo(dir)

	c := &secHardcodedTokenCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for test file, got %d", len(findings))
	}
}

func TestSecHardcodedToken_SkipsVendor(t *testing.T) {
	dir := makeRepo(t, "vendorrepo", map[string]string{
		"go.mod":               "module m\n\ngo 1.24\n",
		"vendor/dep/leaked.go": "package dep\nvar secret = \"aaaa-bbbb-cccc-dddddddd\"\n",
	})
	repo := DetectRepo(dir)

	c := &secHardcodedTokenCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for vendor dir, got %d", len(findings))
	}
}

// --- CI checks ---

func TestCiNoTestJob_HasTest(t *testing.T) {
	dir := makeRepo(t, "citest", map[string]string{
		"go.mod":                   "module m\n\ngo 1.24\n",
		".github/workflows/ci.yml": "jobs:\n  test:\n    run: make test\n",
	})
	repo := DetectRepo(dir)

	c := &ciNoTestJobCheck{}
	if !c.Applies(repo) {
		t.Fatal("should apply when ci.yml exists")
	}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestCiNoTestJob_Missing(t *testing.T) {
	dir := makeRepo(t, "cinotest", map[string]string{
		"go.mod":                   "module m\n\ngo 1.24\n",
		".github/workflows/ci.yml": "jobs:\n  build:\n    run: go build\n",
	})
	repo := DetectRepo(dir)

	c := &ciNoTestJobCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestCiNoLintJob_HasLint(t *testing.T) {
	dir := makeRepo(t, "cilint", map[string]string{
		"go.mod":                   "module m\n\ngo 1.24\n",
		".github/workflows/ci.yml": "jobs:\n  lint:\n    run: golangci-lint run\n",
	})
	repo := DetectRepo(dir)

	c := &ciNoLintJobCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestSecNoSecurityScan_HasTrivy(t *testing.T) {
	dir := makeRepo(t, "sectrivy", map[string]string{
		"go.mod":                   "module m\n\ngo 1.24\n",
		".github/workflows/ci.yml": "jobs:\n  security:\n    uses: trivy-action\n",
	})
	repo := DetectRepo(dir)

	c := &secNoSecurityScanCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestSecNoSecurityScan_Missing(t *testing.T) {
	dir := makeRepo(t, "nosec", map[string]string{
		"go.mod":                   "module m\n\ngo 1.24\n",
		".github/workflows/ci.yml": "jobs:\n  test:\n    run: go test\n",
	})
	repo := DetectRepo(dir)

	c := &secNoSecurityScanCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

// --- Quality checks ---

func TestQualityNoCoverage_HasCoverage(t *testing.T) {
	dir := makeRepo(t, "hascov", map[string]string{
		"go.mod":   "module m\n\ngo 1.24\n",
		"Makefile": "test:\n\tgo test -race -coverprofile=coverage.out ./...\n",
	})
	repo := DetectRepo(dir)

	c := &qualityNoCoverageCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestQualityNoCoverage_Missing(t *testing.T) {
	dir := makeRepo(t, "nocov", map[string]string{
		"go.mod":   "module m\n\ngo 1.24\n",
		"Makefile": "test:\n\tgo test -race ./...\n",
	})
	repo := DetectRepo(dir)

	c := &qualityNoCoverageCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestQualityStaleWO_Planned(t *testing.T) {
	dir := makeRepo(t, "stalwo", map[string]string{
		"go.mod":              "module m\n\ngo 1.24\n",
		"docs/work-orders.md": "# Work Orders\n### WO-01: Setup\n### WO-02: Feature X\n### WO-03: Bug fix [DONE]\n",
	})
	repo := DetectRepo(dir)

	c := &qualityStaleWOCheck{}
	if !c.Applies(repo) {
		t.Fatal("should apply when docs/work-orders.md exists")
	}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Message != "2 planned work orders in docs/work-orders.md" {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
}

func TestQualityStaleWO_AllDone(t *testing.T) {
	dir := makeRepo(t, "alldonwo", map[string]string{
		"go.mod":              "module m\n\ngo 1.24\n",
		"docs/work-orders.md": "# Work Orders\n### WO-01: Setup [DONE]\n### WO-02: Feature X [DONE]\n",
	})
	repo := DetectRepo(dir)

	c := &qualityStaleWOCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestQualityOrphanedTasks_Found(t *testing.T) {
	dir := makeRepo(t, "orphaned", map[string]string{
		"go.mod":             "module m\n\ngo 1.24\n",
		"runforge-wo71.json": `{"tasks": []}`,
		"runforge-wo72.json": `{"tasks": []}`,
	})
	repo := DetectRepo(dir)

	c := &qualityOrphanedTasksCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Severity != SeverityWarning {
		t.Errorf("Severity = %v, want warning", findings[0].Severity)
	}
	if !strings.Contains(findings[0].Message, "2 orphaned") {
		t.Errorf("unexpected message: %q", findings[0].Message)
	}
	if !strings.Contains(findings[0].Message, "runforge-wo71.json") {
		t.Errorf("message should list filenames: %q", findings[0].Message)
	}
}

func TestQualityOrphanedTasks_None(t *testing.T) {
	dir := makeRepo(t, "clean", map[string]string{
		"go.mod": "module m\n\ngo 1.24\n",
	})
	repo := DetectRepo(dir)

	c := &qualityOrphanedTasksCheck{}
	findings := c.Run(repo)
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestLanguageString(t *testing.T) {
	tests := []struct {
		lang Language
		want string
	}{
		{LangGo, "go"},
		{LangPython, "python"},
		{LangMulti, "multi"},
		{LangUnknown, "unknown"},
	}
	for _, tt := range tests {
		if got := tt.lang.String(); got != tt.want {
			t.Errorf("Language(%d).String() = %q, want %q", tt.lang, got, tt.want)
		}
	}
}

func TestAllCheckers_Count(t *testing.T) {
	checkers := AllCheckers()
	if len(checkers) != 26 {
		t.Errorf("AllCheckers() returned %d checks, want 26", len(checkers))
	}
}

func TestAllCheckers_UniqueIDs(t *testing.T) {
	checkers := AllCheckers()
	seen := make(map[string]bool)
	for _, c := range checkers {
		if seen[c.ID()] {
			t.Errorf("duplicate checker ID: %q", c.ID())
		}
		seen[c.ID()] = true
	}
}

// --- Prompt content tests ---

func TestFileCheck_PromptGenerated(t *testing.T) {
	dir := makeRepo(t, "noci", map[string]string{
		"go.mod": "module m\n\ngo 1.24\n",
	})
	repo := DetectRepo(dir)

	c := &fileCheck{
		id: "missing-ci", cat: "structure", file: ".github/workflows/ci.yml",
		sev: SeverityCritical, msg: "No CI", sug: "Create ci.yml",
		promptFn: promptCI,
	}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Prompt == "" {
		t.Fatal("Prompt should be non-empty when promptFn is set")
	}
	if !strings.Contains(f.Prompt, "actions/checkout") {
		t.Error("Prompt should contain CI action references")
	}
	if !strings.Contains(f.Prompt, "Do NOT") {
		t.Error("Prompt should contain constraints")
	}
	if len(f.Prompt) <= len(f.Suggestion) {
		t.Error("Prompt should be longer than Suggestion")
	}
}

func TestFileCheck_NoPromptFn(t *testing.T) {
	dir := makeRepo(t, "bare2", map[string]string{})
	repo := DetectRepo(dir)

	c := &fileCheck{
		id: "test-check", cat: "test", file: "MISSING",
		sev: SeverityInfo, msg: "missing", sug: "add it",
		// promptFn intentionally nil
	}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Prompt != "" {
		t.Error("Prompt should be empty when promptFn is nil")
	}
}

func TestGoNoRace_PromptContainsVerification(t *testing.T) {
	dir := makeRepo(t, "noracep", map[string]string{
		"go.mod":   "module m\n\ngo 1.24\n",
		"Makefile": "test:\n\tgo test ./...\n",
	})
	repo := DetectRepo(dir)

	c := &goNoRaceCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	f := findings[0]
	if f.Prompt == "" {
		t.Fatal("Prompt should be non-empty")
	}
	if !strings.Contains(f.Prompt, "make test") {
		t.Error("Prompt should contain verification command")
	}
	if !strings.Contains(f.Prompt, "-race") {
		t.Error("Prompt should mention -race flag")
	}
	if !strings.Contains(f.Prompt, "Do NOT") {
		t.Error("Prompt should contain constraints")
	}
}

func TestSecHardcodedToken_PromptContainsFilePath(t *testing.T) {
	dir := makeRepo(t, "tokenprompt", map[string]string{
		"go.mod":  "module m\n\ngo 1.24\n",
		"main.go": "package main\nvar apiKey = \"sk-1234567890abcdef1234\"\n",
	})
	repo := DetectRepo(dir)

	c := &secHardcodedTokenCheck{}
	findings := c.Run(repo)
	if len(findings) < 1 {
		t.Fatal("expected at least 1 finding")
	}

	f := findings[0]
	if f.Prompt == "" {
		t.Fatal("Prompt should be non-empty")
	}
	if !strings.Contains(f.Prompt, "main.go") {
		t.Error("Prompt should reference the specific file")
	}
	if !strings.Contains(f.Prompt, "os.Getenv") {
		t.Error("Prompt should suggest os.Getenv for Go files")
	}
}

func TestGoNoTests_PromptListsPackages(t *testing.T) {
	dir := makeRepo(t, "notestsp", map[string]string{
		"go.mod":          "module m\n\ngo 1.24\n",
		"main.go":         "package main\n",
		"internal/foo.go": "package internal\n",
	})
	repo := DetectRepo(dir)

	c := &goNoTestsCheck{}
	findings := c.Run(repo)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}

	f := findings[0]
	if f.Prompt == "" {
		t.Fatal("Prompt should be non-empty")
	}
	if !strings.Contains(f.Prompt, "Packages that need tests") {
		t.Error("Prompt should list packages")
	}
	if !strings.Contains(f.Prompt, "table-driven") {
		t.Error("Prompt should mention testing conventions")
	}
}
