package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIsSafeRunner(t *testing.T) {
	tests := []struct {
		name string
		safe bool
	}{
		{"claude", true},
		{"cline", true},
		{"codex", false},
		{"opencode", false},
		{"gemini", false},
		{"script", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		if got := IsSafeRunner(tt.name); got != tt.safe {
			t.Errorf("IsSafeRunner(%q) = %v, want %v", tt.name, got, tt.safe)
		}
	}
}

func TestPreScan_CleanDir(t *testing.T) {
	if _, err := lookupPastewatch(); err != nil {
		t.Skip("pastewatch-cli not available")
	}

	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644)

	found, err := preScan(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("clean dir should not have secrets")
	}
}

func TestPreScan_DirWithSecrets(t *testing.T) {
	if _, err := lookupPastewatch(); err != nil {
		t.Skip("pastewatch-cli not available")
	}

	dir := t.TempDir()
	// write a file with a fake API key that pastewatch should detect;
	// assembled at runtime to avoid tripping the pre-commit secret scanner
	fakeKey := "sk-proj-" + "abc123def456ghi789jkl012mno345pqr678stu901vwx234"
	_ = os.WriteFile(filepath.Join(dir, ".env"), []byte("OPENAI_API_KEY="+fakeKey+"\n"), 0o644)

	found, err := preScan(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("dir with API key should have secrets detected")
	}
}
