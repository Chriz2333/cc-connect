package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListCodexSessionsWithOptions_IncludesChildWorkdirsWhenRecursive(t *testing.T) {
	base := t.TempDir()
	codexHome := filepath.Join(base, ".codex")
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "05", "24")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	rootID := "root-session"
	childID := "child-session"
	otherID := "other-session"
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-root.jsonl"), rootID, base, "root prompt")
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-child.jsonl"), childID, filepath.Join(base, "2026-05-24", "s4-s4"), "child prompt")
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-other.jsonl"), otherID, filepath.Join(t.TempDir(), "elsewhere"), "other prompt")

	sessions, err := listCodexSessionsWithOptions(base, codexHome, true)
	if err != nil {
		t.Fatalf("listCodexSessionsWithOptions: %v", err)
	}

	got := make(map[string]bool)
	for _, s := range sessions {
		got[s.ID] = true
	}
	if !got[rootID] {
		t.Fatalf("recursive list missing root cwd session: got IDs %#v", got)
	}
	if !got[childID] {
		t.Fatalf("recursive list missing child cwd session: got IDs %#v", got)
	}
	if got[otherID] {
		t.Fatalf("recursive list included unrelated cwd session: got IDs %#v", got)
	}
}

func TestListCodexSessionsWithOptions_ExactModeExcludesChildWorkdirs(t *testing.T) {
	base := t.TempDir()
	codexHome := filepath.Join(base, ".codex")
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "05", "24")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	rootID := "root-session"
	childID := "child-session"
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-root.jsonl"), rootID, base, "root prompt")
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-child.jsonl"), childID, filepath.Join(base, "2026-05-24", "s4-s4"), "child prompt")

	sessions, err := listCodexSessionsWithOptions(base, codexHome, false)
	if err != nil {
		t.Fatalf("listCodexSessionsWithOptions: %v", err)
	}

	got := make(map[string]bool)
	for _, s := range sessions {
		got[s.ID] = true
	}
	if !got[rootID] {
		t.Fatalf("exact list missing root cwd session: got IDs %#v", got)
	}
	if got[childID] {
		t.Fatalf("exact list included child cwd session: got IDs %#v", got)
	}
}

func writeCodexListSession(t *testing.T, path, id, cwd, prompt string) {
	t.Helper()
	content := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + filepath.ToSlash(cwd) + `"}}` + "\n" +
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"` + prompt + `"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session %s: %v", id, err)
	}
}
