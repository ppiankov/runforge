package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBlacklist_Empty(t *testing.T) {
	bl := NewRunnerBlacklist()
	if bl.IsBlocked("codex") {
		t.Fatal("empty blacklist should not block any runner")
	}
}

func TestBlacklist_BlockAndCheck(t *testing.T) {
	bl := NewRunnerBlacklist()
	bl.Block("codex", time.Now().Add(1*time.Hour))

	if !bl.IsBlocked("codex") {
		t.Fatal("codex should be blocked")
	}
	if bl.IsBlocked("claude") {
		t.Fatal("claude should not be blocked")
	}
}

func TestBlacklist_Expired(t *testing.T) {
	bl := NewRunnerBlacklist()
	bl.Block("codex", time.Now().Add(-1*time.Second))

	if bl.IsBlocked("codex") {
		t.Fatal("expired block should not block runner")
	}
}

func TestBlacklist_ExtendOnly(t *testing.T) {
	bl := NewRunnerBlacklist()

	later := time.Now().Add(2 * time.Hour)
	earlier := time.Now().Add(1 * time.Hour)

	bl.Block("codex", later)
	bl.Block("codex", earlier) // should not shorten

	// verify still blocked (would be unblocked if shortened to `earlier` in the past)
	if !bl.IsBlocked("codex") {
		t.Fatal("block should not be shortened")
	}
}

func TestBlacklist_MultipleRunners(t *testing.T) {
	bl := NewRunnerBlacklist()
	bl.Block("codex", time.Now().Add(1*time.Hour))
	bl.Block("zai", time.Now().Add(30*time.Minute))

	if !bl.IsBlocked("codex") {
		t.Fatal("codex should be blocked")
	}
	if !bl.IsBlocked("zai") {
		t.Fatal("zai should be blocked")
	}
	if bl.IsBlocked("claude-api") {
		t.Fatal("claude-api should not be blocked")
	}
}

func TestBlacklist_PersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blacklist.json")

	// create and persist
	bl := LoadBlacklist(path)
	future := time.Now().Add(2 * time.Hour)
	bl.Block("codex", future)
	bl.Block("expired-runner", time.Now().Add(-1*time.Second))

	// load from disk in a new instance
	bl2 := LoadBlacklist(path)

	if !bl2.IsBlocked("codex") {
		t.Fatal("codex should be blocked after reload")
	}
	if bl2.IsBlocked("expired-runner") {
		t.Fatal("expired entries should not be loaded")
	}
}

func TestBlacklist_LoadMissingFile(t *testing.T) {
	bl := LoadBlacklist("/nonexistent/path/blacklist.json")
	if bl.IsBlocked("anything") {
		t.Fatal("missing file should produce empty blacklist")
	}
}

func TestBlacklist_LoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blacklist.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)

	bl := LoadBlacklist(path)
	if bl.IsBlocked("anything") {
		t.Fatal("corrupt file should produce empty blacklist")
	}
}

func TestBlacklist_Clear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blacklist.json")

	bl := LoadBlacklist(path)
	bl.Block("codex", time.Now().Add(1*time.Hour))

	if !bl.IsBlocked("codex") {
		t.Fatal("codex should be blocked before clear")
	}

	bl.Clear()

	if bl.IsBlocked("codex") {
		t.Fatal("codex should not be blocked after clear")
	}

	// file should be removed
	if _, err := os.Stat(path); err == nil {
		t.Fatal("blacklist file should be removed after clear")
	}
}

func TestBlacklist_Entries(t *testing.T) {
	bl := NewRunnerBlacklist()
	bl.Block("codex", time.Now().Add(1*time.Hour))
	bl.Block("expired", time.Now().Add(-1*time.Second))

	entries := bl.Entries()
	if _, ok := entries["codex"]; !ok {
		t.Fatal("entries should contain active codex")
	}
	if _, ok := entries["expired"]; ok {
		t.Fatal("entries should not contain expired runner")
	}
}
