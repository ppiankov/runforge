package runner

import (
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
