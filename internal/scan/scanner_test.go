package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan_BasicGoRepo(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "myapp")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte("module m\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Scan(ScanOptions{ReposDir: base})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ReposScanned) != 1 {
		t.Fatalf("ReposScanned = %d, want 1", len(result.ReposScanned))
	}
	if result.ReposScanned[0] != "myapp" {
		t.Errorf("ReposScanned[0] = %q, want %q", result.ReposScanned[0], "myapp")
	}
	if len(result.Findings) == 0 {
		t.Error("expected at least some findings for bare Go repo")
	}
}

func TestScan_FilterRepo(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"app1", "app2", "app3"} {
		d := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module m\n\ngo 1.24\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Scan(ScanOptions{ReposDir: base, FilterRepo: "app2"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ReposScanned) != 1 {
		t.Fatalf("ReposScanned = %d, want 1", len(result.ReposScanned))
	}
	if result.ReposScanned[0] != "app2" {
		t.Errorf("ReposScanned[0] = %q, want %q", result.ReposScanned[0], "app2")
	}
}

func TestScan_ExcludeRepos(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"keep", "skip"} {
		d := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module m\n\ngo 1.24\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Scan(ScanOptions{ReposDir: base, ExcludeRepos: []string{"skip"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ReposScanned) != 1 {
		t.Fatalf("ReposScanned = %d, want 1", len(result.ReposScanned))
	}
	if result.ReposScanned[0] != "keep" {
		t.Errorf("ReposScanned[0] = %q, want %q", result.ReposScanned[0], "keep")
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "skip" {
		t.Errorf("Skipped = %v, want [skip]", result.Skipped)
	}
}

func TestScan_MinSeverity(t *testing.T) {
	base := t.TempDir()
	d := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module m\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// scan with critical-only filter
	result, err := Scan(ScanOptions{ReposDir: base, MinSeverity: SeverityCritical})
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range result.Findings {
		if f.Severity != SeverityCritical {
			t.Errorf("finding %q has severity %v, want critical (min severity filter)", f.Check, f.Severity)
		}
	}
}

func TestScan_Categories(t *testing.T) {
	base := t.TempDir()
	d := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module m\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Scan(ScanOptions{ReposDir: base, Categories: []string{"security"}})
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range result.Findings {
		if f.Category != "security" {
			t.Errorf("finding %q has category %q, want security (category filter)", f.Check, f.Category)
		}
	}
}

func TestScan_SkipsNonGitDirs(t *testing.T) {
	base := t.TempDir()
	// dir without .git
	d := filepath.Join(base, "notrepo")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module m\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Scan(ScanOptions{ReposDir: base})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ReposScanned) != 0 {
		t.Errorf("ReposScanned = %d, want 0 (not a git repo)", len(result.ReposScanned))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("Skipped = %d, want 1", len(result.Skipped))
	}
}

func TestScan_SkipsHiddenDirs(t *testing.T) {
	base := t.TempDir()
	d := filepath.Join(base, ".hidden")
	if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := Scan(ScanOptions{ReposDir: base})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ReposScanned) != 0 {
		t.Errorf("ReposScanned = %d, want 0 (hidden dir)", len(result.ReposScanned))
	}
}

func TestScan_SortsCriticalFirst(t *testing.T) {
	base := t.TempDir()
	d := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// bare Go repo — will get both critical and non-critical findings
	if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module m\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Scan(ScanOptions{ReposDir: base})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Findings) < 2 {
		t.Skip("not enough findings to test sort order")
	}

	for i := 1; i < len(result.Findings); i++ {
		if result.Findings[i].Severity < result.Findings[i-1].Severity {
			t.Errorf("findings not sorted: [%d].Severity=%v > [%d].Severity=%v",
				i-1, result.Findings[i-1].Severity, i, result.Findings[i].Severity)
		}
	}
}

func TestScan_InvalidDir(t *testing.T) {
	_, err := Scan(ScanOptions{ReposDir: "/nonexistent/path/that/does/not/exist"})
	if err == nil {
		t.Error("expected error for nonexistent dir")
	}
}

func TestScan_SkipsFiles(t *testing.T) {
	base := t.TempDir()
	// create a file (not a dir) in the base
	if err := os.WriteFile(filepath.Join(base, "README.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Scan(ScanOptions{ReposDir: base})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.ReposScanned) != 0 {
		t.Errorf("ReposScanned = %d, want 0", len(result.ReposScanned))
	}
}
