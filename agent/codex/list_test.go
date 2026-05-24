package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-root.jsonl"), rootID, base, "root prompt", "")
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-child.jsonl"), childID, filepath.Join(base, "2026-05-24", "s4-s4"), "child prompt", "")
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-other.jsonl"), otherID, filepath.Join(t.TempDir(), "elsewhere"), "other prompt", "")

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
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-root.jsonl"), rootID, base, "root prompt", "")
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-child.jsonl"), childID, filepath.Join(base, "2026-05-24", "s4-s4"), "child prompt", "")

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

func TestListCodexSessionsWithOptions_HidesSubagentSessions(t *testing.T) {
	base := t.TempDir()
	codexHome := filepath.Join(base, ".codex")
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "05", "24")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	userID := "user-session"
	subagentSourceID := "subagent-source-session"
	subagentThreadID := "subagent-thread-session"
	childDir := filepath.Join(base, "2026-05-24", "s4-s4")
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-user.jsonl"), userID, childDir, "user prompt", `"source":"cli"`)
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-subagent-source.jsonl"), subagentSourceID, childDir, "subagent prompt", `"source":{"subagent":{"thread_spawn":{"parent_thread_id":"`+userID+`","depth":1}}}`)
	writeCodexListSession(t, filepath.Join(sessionDir, "rollout-subagent-thread.jsonl"), subagentThreadID, childDir, "subagent prompt", `"thread_source":"subagent"`)

	sessions, err := listCodexSessionsWithOptions(base, codexHome, true)
	if err != nil {
		t.Fatalf("listCodexSessionsWithOptions: %v", err)
	}

	got := make(map[string]bool)
	for _, s := range sessions {
		got[s.ID] = true
	}
	if !got[userID] {
		t.Fatalf("list missing user-started session: got IDs %#v", got)
	}
	if got[subagentSourceID] {
		t.Fatalf("list included subagent source session: got IDs %#v", got)
	}
	if got[subagentThreadID] {
		t.Fatalf("list included subagent thread session: got IDs %#v", got)
	}
}

func TestListCodexSessionsWithOptions_DeduplicatesSessionID(t *testing.T) {
	base := t.TempDir()
	codexHome := filepath.Join(base, ".codex")
	sessionDir := filepath.Join(codexHome, "sessions", "2026", "05", "24")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	sessionID := "same-session"
	oldPath := filepath.Join(sessionDir, "rollout-old.jsonl")
	newPath := filepath.Join(sessionDir, "rollout-new.jsonl")
	writeCodexListSession(t, oldPath, sessionID, base, "old delegated task fragment", `"source":"cli"`)
	writeCodexListSession(t, newPath, sessionID, base, "new user-visible session", `"source":"cli"`)
	oldTime := time.Date(2026, 5, 24, 13, 30, 0, 0, time.Local)
	newTime := time.Date(2026, 5, 24, 18, 53, 0, 0, time.Local)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}

	sessions, err := listCodexSessionsWithOptions(base, codexHome, true)
	if err != nil {
		t.Fatalf("listCodexSessionsWithOptions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions len = %d, want 1: %#v", len(sessions), sessions)
	}
	if sessions[0].Summary != "new user-visible session" {
		t.Fatalf("summary = %q, want newest transcript summary", sessions[0].Summary)
	}
}

func writeCodexListSession(t *testing.T, path, id, cwd, prompt, extraMeta string) {
	t.Helper()
	if extraMeta != "" {
		extraMeta = "," + extraMeta
	}
	content := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q%s}}`, id, filepath.ToSlash(cwd), extraMeta) + "\n" +
		`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"` + prompt + `"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session %s: %v", id, err)
	}
}
