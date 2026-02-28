package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGraylist_Empty(t *testing.T) {
	gl := NewRunnerGraylist()
	if gl.IsGraylisted("codex", "") {
		t.Fatal("empty graylist should not flag any runner")
	}
}

func TestGraylist_AddAndCheck(t *testing.T) {
	gl := NewRunnerGraylist()
	gl.Add("deepseek", "deepseek-chat", "false positive: 0 events")

	if !gl.IsGraylisted("deepseek", "deepseek-chat") {
		t.Fatal("deepseek:deepseek-chat should be graylisted")
	}
	if gl.IsGraylisted("deepseek", "deepseek-reasoner") {
		t.Fatal("deepseek:deepseek-reasoner should NOT be graylisted")
	}
	if gl.IsGraylisted("claude", "") {
		t.Fatal("claude should not be graylisted")
	}
}

func TestGraylist_WildcardModel(t *testing.T) {
	gl := NewRunnerGraylist()
	// empty model = wildcard, blocks all models for this runner
	gl.Add("minimax-free", "", "all models are bad")

	if !gl.IsGraylisted("minimax-free", "some-model") {
		t.Fatal("wildcard should match any model")
	}
	if !gl.IsGraylisted("minimax-free", "") {
		t.Fatal("wildcard should match empty model too")
	}
}

func TestGraylist_ExactDoesNotMatchOtherModels(t *testing.T) {
	gl := NewRunnerGraylist()
	gl.Add("deepseek", "deepseek-chat", "cheap model")

	if gl.IsGraylisted("deepseek", "deepseek-reasoner") {
		t.Fatal("exact model entry should not match different model")
	}
}

func TestGraylist_Remove(t *testing.T) {
	gl := NewRunnerGraylist()
	gl.Add("minimax-free", "model-a", "test reason")
	gl.Remove("minimax-free", "model-a")

	if gl.IsGraylisted("minimax-free", "model-a") {
		t.Fatal("should not be graylisted after removal")
	}
}

func TestGraylist_RemoveNonExistent(t *testing.T) {
	gl := NewRunnerGraylist()
	// should not panic
	gl.Remove("nonexistent", "")
}

func TestGraylist_PersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graylist.json")

	gl := LoadGraylist(path)
	gl.Add("deepseek", "deepseek-chat", "cheap model")
	gl.Add("minimax-free", "", "all models bad")

	// load from disk in a new instance
	gl2 := LoadGraylist(path)

	if !gl2.IsGraylisted("deepseek", "deepseek-chat") {
		t.Fatal("deepseek:deepseek-chat should be graylisted after reload")
	}
	if gl2.IsGraylisted("deepseek", "deepseek-reasoner") {
		t.Fatal("deepseek:deepseek-reasoner should NOT be graylisted after reload")
	}
	if !gl2.IsGraylisted("minimax-free", "any-model") {
		t.Fatal("minimax-free wildcard should be graylisted after reload")
	}
}

func TestGraylist_LoadMissingFile(t *testing.T) {
	gl := LoadGraylist("/nonexistent/path/graylist.json")
	if gl.IsGraylisted("anything", "") {
		t.Fatal("missing file should produce empty graylist")
	}
}

func TestGraylist_LoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graylist.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)

	gl := LoadGraylist(path)
	if gl.IsGraylisted("anything", "") {
		t.Fatal("corrupt file should produce empty graylist")
	}
}

func TestGraylist_Clear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graylist.json")

	gl := LoadGraylist(path)
	gl.Add("minimax-free", "", "test")

	if !gl.IsGraylisted("minimax-free", "") {
		t.Fatal("should be graylisted before clear")
	}

	gl.Clear()

	if gl.IsGraylisted("minimax-free", "") {
		t.Fatal("should not be graylisted after clear")
	}

	if _, err := os.Stat(path); err == nil {
		t.Fatal("graylist file should be removed after clear")
	}
}

func TestGraylist_Entries(t *testing.T) {
	gl := NewRunnerGraylist()
	gl.Add("deepseek", "deepseek-chat", "false positive")
	gl.Add("minimax-free", "", "0 commits")

	entries := gl.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if info, ok := entries["deepseek:deepseek-chat"]; !ok {
		t.Fatal("entries should contain deepseek:deepseek-chat")
	} else if info.Reason != "false positive" {
		t.Errorf("expected reason 'false positive', got %q", info.Reason)
	}
	if _, ok := entries["minimax-free"]; !ok {
		t.Fatal("entries should contain minimax-free (wildcard)")
	}
}

func TestGraylist_AddOverwrite(t *testing.T) {
	gl := NewRunnerGraylist()
	gl.Add("minimax-free", "model-a", "first reason")
	gl.Add("minimax-free", "model-a", "updated reason")

	entries := gl.Entries()
	if info := entries["minimax-free:model-a"]; info.Reason != "updated reason" {
		t.Errorf("expected updated reason, got %q", info.Reason)
	}
}
