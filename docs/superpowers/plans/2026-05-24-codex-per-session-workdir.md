# Codex Per-Session Workdir Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an optional cc-connect mode where each newly created Codex conversation gets its own directory under `C:\Users\Administrator\Documents\Codex`, then build/install the patched binary locally and push the finished version to GitHub.

**Architecture:** Keep the current fixed `work_dir` behavior as the default. Add a project-level `work_dir_mode = "per_session"` plus `work_dir_base = ".../Documents/Codex"` configuration path; when enabled, `SessionManager` assigns a stable `WorkDir` to each new `Session`, persists it in the session store, and `Engine` starts the agent in that directory for the session. Directory creation is deterministic and local to session creation, using date folders plus a unique cc-connect session folder.

**Tech Stack:** Go 1.22+, TOML config parsing in `config/config.go`, cc-connect core session/engine code, Windows PowerShell launcher, Git/GitHub fork `https://github.com/Chriz2333/cc-connect`.

---

## File Structure

- Modify `config/config.go`
  - Add project-level `WorkDirMode` and `WorkDirBase` fields.
  - Validate `work_dir_mode` values and make `work_dir_base` required for `per_session`.
  - Reject `work_dir_mode = "per_session"` when `mode = "multi-workspace"` because both features want to own workspace routing.
- Modify `cmd/cc-connect/main.go`
  - Wire `proj.WorkDirMode` and `proj.WorkDirBase` into `Engine`.
  - Resolve `~` and relative paths for `work_dir_base` the same way existing base/work dirs are resolved.
- Modify `core/session.go`
  - Add `WorkDir string json:"work_dir,omitempty"` to `Session`.
  - Add `SessionManager.ConfigurePerSessionWorkDir(mode, base string)` and helper functions to allocate per-session directories.
  - Update `GetOrCreateActive`, `NewSession`, and `NewSideSession` so new sessions get a directory when the mode is enabled.
  - Persist `WorkDir` through `saveLocked` and `load`.
- Modify `core/engine.go`
  - Add `SetPerSessionWorkDir(mode, base string)`.
  - Before starting an interactive agent session, if `session.WorkDir` is set, create or reuse a workspace-specific agent for that directory.
  - Make command workdir reporting prefer `session.WorkDir` for the current active session.
- Modify `core/session_test.go`
  - Add unit tests for per-session directory allocation, persistence, uniqueness, and disabled/default behavior.
- Modify `core/engine_test.go`
  - Add an integration-style core test that a normal message in per-session mode starts a workspace agent whose `work_dir` equals the session `WorkDir`.
- Modify `config/config_test.go`
  - Add validation tests for accepted/rejected `work_dir_mode` combinations.
- Modify `config.example.toml`
  - Document `work_dir_mode = "per_session"` and `work_dir_base`.
- Modify `docs/codex-cloud-deploy-summary.zh-CN.md`
  - Add the new cloud/server deployment config snippet.
- Modify local runtime file `C:\Users\Administrator\.cc-connect\config.toml` after code passes
  - Set this machine to the new mode:
    ```toml
    work_dir_mode = "per_session"
    work_dir_base = "C:\\Users\\Administrator\\Documents\\Codex"
    ```
  - Remove the old fixed `work_dir` from `[projects.agent.options]` for this project.

---

### Task 1: Config Schema And Validation

**Files:**
- Modify: `C:\Users\Administrator\Documents\Codex\2026-05-23\cc-connect\cc-connect-src\config\config.go`
- Test: `C:\Users\Administrator\Documents\Codex\2026-05-23\cc-connect\cc-connect-src\config\config_test.go`

- [ ] **Step 1: Write failing config validation tests**

Add tests near existing `Validate` project tests in `config/config_test.go`:

```go
func TestValidateProject_PerSessionWorkDirRequiresBase(t *testing.T) {
	cfg := Config{
		Projects: []ProjectConfig{{
			Name:        "codex",
			WorkDirMode: "per_session",
			Agent:       AgentConfig{Type: "codex", Options: map[string]any{}},
			Platforms:   []PlatformConfig{{Type: "dingtalk", Options: map[string]any{}}},
		}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `project "codex": work_dir_mode "per_session" requires work_dir_base`) {
		t.Fatalf("Validate() error = %v, want missing work_dir_base error", err)
	}
}

func TestValidateProject_PerSessionWorkDirRejectsMultiWorkspace(t *testing.T) {
	cfg := Config{
		Projects: []ProjectConfig{{
			Name:        "codex",
			Mode:        "multi-workspace",
			BaseDir:     "/tmp/workspaces",
			WorkDirMode: "per_session",
			WorkDirBase: "/tmp/codex",
			Agent:       AgentConfig{Type: "codex", Options: map[string]any{}},
			Platforms:   []PlatformConfig{{Type: "dingtalk", Options: map[string]any{}}},
		}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `project "codex": work_dir_mode "per_session" cannot be used with multi-workspace mode`) {
		t.Fatalf("Validate() error = %v, want mode conflict error", err)
	}
}

func TestValidateProject_PerSessionWorkDirAcceptsValidConfig(t *testing.T) {
	cfg := Config{
		Projects: []ProjectConfig{{
			Name:        "codex",
			WorkDirMode: "per_session",
			WorkDirBase: "/tmp/codex",
			Agent:       AgentConfig{Type: "codex", Options: map[string]any{}},
			Platforms:   []PlatformConfig{{Type: "dingtalk", Options: map[string]any{}}},
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestValidateProject_RejectsUnknownWorkDirMode(t *testing.T) {
	cfg := Config{
		Projects: []ProjectConfig{{
			Name:        "codex",
			WorkDirMode: "daily",
			WorkDirBase: "/tmp/codex",
			Agent:       AgentConfig{Type: "codex", Options: map[string]any{}},
			Platforms:   []PlatformConfig{{Type: "dingtalk", Options: map[string]any{}}},
		}},
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `project "codex": unsupported work_dir_mode "daily"`) {
		t.Fatalf("Validate() error = %v, want unsupported mode error", err)
	}
}
```

- [ ] **Step 2: Run config tests and verify they fail**

Run:

```powershell
go test ./config -run 'TestValidateProject_PerSessionWorkDir|TestValidateProject_RejectsUnknownWorkDirMode' -count=1
```

Expected: FAIL because `ProjectConfig.WorkDirMode` and `ProjectConfig.WorkDirBase` do not exist.

- [ ] **Step 3: Add config fields and validation**

In `config/config.go`, extend `ProjectConfig` near `Mode` and `BaseDir`:

```go
	WorkDirMode string `toml:"work_dir_mode,omitempty"` // "" or "per_session"
	WorkDirBase string `toml:"work_dir_base,omitempty"` // parent dir for per-session work dirs
```

In `Config.Validate()`, inside the per-project loop after the existing multi-workspace validation block, add:

```go
		switch strings.TrimSpace(proj.WorkDirMode) {
		case "":
		case "per_session":
			if proj.Mode == "multi-workspace" {
				return fmt.Errorf("project %q: work_dir_mode \"per_session\" cannot be used with multi-workspace mode", proj.Name)
			}
			if strings.TrimSpace(proj.WorkDirBase) == "" {
				return fmt.Errorf("project %q: work_dir_mode \"per_session\" requires work_dir_base", proj.Name)
			}
		default:
			return fmt.Errorf("project %q: unsupported work_dir_mode %q", proj.Name, proj.WorkDirMode)
		}
```

- [ ] **Step 4: Run config tests and verify they pass**

Run:

```powershell
go test ./config -run 'TestValidateProject_PerSessionWorkDir|TestValidateProject_RejectsUnknownWorkDirMode' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add config/config.go config/config_test.go
git commit -m "feat: add per-session workdir config"
```

---

### Task 2: Session Workdir Allocation And Persistence

**Files:**
- Modify: `C:\Users\Administrator\Documents\Codex\2026-05-23\cc-connect\cc-connect-src\core\session.go`
- Test: `C:\Users\Administrator\Documents\Codex\2026-05-23\cc-connect\cc-connect-src\core\session_test.go`

- [ ] **Step 1: Write failing session tests**

Add tests to `core/session_test.go`:

```go
func TestSessionManager_PerSessionWorkDirAllocation(t *testing.T) {
	base := t.TempDir()
	sm := NewSessionManager("")
	sm.ConfigurePerSessionWorkDir("per_session", base)

	s1 := sm.NewSession("dingtalk:group", "first")
	s2 := sm.NewSession("dingtalk:group", "second")

	if s1.WorkDir == "" || s2.WorkDir == "" {
		t.Fatalf("WorkDir should be assigned, got %q and %q", s1.WorkDir, s2.WorkDir)
	}
	if s1.WorkDir == s2.WorkDir {
		t.Fatalf("WorkDir should be unique per session, got %q", s1.WorkDir)
	}
	if !strings.HasPrefix(filepath.Clean(s1.WorkDir), filepath.Clean(base)+string(os.PathSeparator)) {
		t.Fatalf("WorkDir %q should be under base %q", s1.WorkDir, base)
	}
	if _, err := os.Stat(s1.WorkDir); err != nil {
		t.Fatalf("allocated WorkDir should exist: %v", err)
	}
}

func TestSessionManager_PerSessionWorkDirPersistence(t *testing.T) {
	root := t.TempDir()
	store := filepath.Join(root, "sessions.json")
	base := filepath.Join(root, "codex")

	sm := NewSessionManager(store)
	sm.ConfigurePerSessionWorkDir("per_session", base)
	s := sm.NewSession("dingtalk:group", "persisted")
	want := s.WorkDir

	sm2 := NewSessionManager(store)
	got := sm2.GetOrCreateActive("dingtalk:group")
	if got.WorkDir != want {
		t.Fatalf("reloaded WorkDir = %q, want %q", got.WorkDir, want)
	}
}

func TestSessionManager_PerSessionWorkDirDisabledByDefault(t *testing.T) {
	sm := NewSessionManager("")
	s := sm.NewSession("dingtalk:group", "default")
	if s.WorkDir != "" {
		t.Fatalf("WorkDir = %q, want empty when per-session mode is disabled", s.WorkDir)
	}
}
```

- [ ] **Step 2: Run session tests and verify they fail**

Run:

```powershell
go test ./core -run 'TestSessionManager_PerSessionWorkDir' -count=1
```

Expected: FAIL because `ConfigurePerSessionWorkDir` and `Session.WorkDir` do not exist.

- [ ] **Step 3: Add session fields and manager configuration**

In `core/session.go`, add imports if missing:

```go
	"strings"
```

Extend `Session`:

```go
	WorkDir             string         `json:"work_dir,omitempty"`
```

Extend `SessionManager`:

```go
	perSessionWorkDir bool
	workDirBase       string
```

Add methods:

```go
func (sm *SessionManager) ConfigurePerSessionWorkDir(mode, base string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.perSessionWorkDir = strings.TrimSpace(mode) == "per_session"
	sm.workDirBase = strings.TrimSpace(base)
}

func (sm *SessionManager) allocateWorkDirLocked(id, name string, now time.Time) string {
	if !sm.perSessionWorkDir || sm.workDirBase == "" {
		return ""
	}
	dateDir := filepath.Join(sm.workDirBase, now.Format("2006-01-02"))
	label := sanitizeSessionDirLabel(name)
	if label == "" {
		label = id
	}
	for i := 0; i < 1000; i++ {
		suffix := now.Format("150405")
		if i > 0 {
			suffix = fmt.Sprintf("%s-%03d", suffix, i)
		}
		dir := filepath.Join(dateDir, fmt.Sprintf("cc-connect-%s-%s", suffix, label))
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return dir
		}
	}
	return ""
}

func sanitizeSessionDirLabel(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
		if b.Len() >= 48 {
			break
		}
	}
	return strings.Trim(b.String(), "-_")
}
```

Update `createLocked` and `NewSideSession` to set `WorkDir`:

```go
		WorkDir:   sm.allocateWorkDirLocked(id, name, now),
```

- [ ] **Step 4: Persist WorkDir in snapshots**

In `saveLocked`, include:

```go
			WorkDir:             s.WorkDir,
```

No special load code is needed because `json.Unmarshal` fills `Session.WorkDir`; existing nil-map guards remain enough.

- [ ] **Step 5: Run session tests and verify they pass**

Run:

```powershell
go test ./core -run 'TestSessionManager_PerSessionWorkDir' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add core/session.go core/session_test.go
git commit -m "feat: persist per-session workdirs"
```

---

### Task 3: Engine Uses Session Workdir

**Files:**
- Modify: `C:\Users\Administrator\Documents\Codex\2026-05-23\cc-connect\cc-connect-src\core\engine.go`
- Modify: `C:\Users\Administrator\Documents\Codex\2026-05-23\cc-connect\cc-connect-src\cmd\cc-connect\main.go`
- Test: `C:\Users\Administrator\Documents\Codex\2026-05-23\cc-connect\cc-connect-src\core\engine_test.go`

- [ ] **Step 1: Write failing engine test**

Add a test in `core/engine_test.go` near multi-workspace tests:

```go
func TestEngine_PerSessionWorkDirStartsWorkspaceAgent(t *testing.T) {
	base := t.TempDir()
	ag := newStubAgent()
	p := newMockPlatform()
	e := NewEngine("codex", ag, []Platform{p}, "", LangEnglish)
	e.SetPerSessionWorkDir("per_session", base)

	msg := &Message{
		Platform:   p.Name(),
		SessionKey: "dingtalk:group",
		UserID:     "user1",
		UserName:   "User",
		Content:    "hello",
		ReplyCtx:   "ctx",
	}

	e.HandleMessage(p, msg)
	waitForCondition(t, time.Second, func() bool {
		return len(ag.startedWorkDirs()) > 0
	})

	dirs := ag.startedWorkDirs()
	if len(dirs) == 0 {
		t.Fatal("agent was not started")
	}
	if !strings.HasPrefix(filepath.Clean(dirs[0]), filepath.Clean(base)+string(os.PathSeparator)) {
		t.Fatalf("started work_dir = %q, want under %q", dirs[0], base)
	}
}
```

If the existing stub agent does not expose started work dirs, add a minimal helper method to the local test stub used in this file:

```go
func (a *stubAgent) startedWorkDirs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.workDirs...)
}
```

and record `opts["work_dir"]` in the stub constructor or `StartSession` path already used by workspace tests. Use the existing test stub names in the file rather than introducing a second stub type.

- [ ] **Step 2: Run engine test and verify it fails**

Run:

```powershell
go test ./core -run TestEngine_PerSessionWorkDirStartsWorkspaceAgent -count=1
```

Expected: FAIL because `SetPerSessionWorkDir` does not exist or the engine still starts the base agent.

- [ ] **Step 3: Add Engine wiring**

In `core/engine.go`, add:

```go
func (e *Engine) SetPerSessionWorkDir(mode, base string) {
	e.sessions.ConfigurePerSessionWorkDir(mode, base)
	if strings.TrimSpace(mode) == "per_session" && strings.TrimSpace(base) != "" && e.workspacePool == nil {
		e.workspacePool = newWorkspacePool(DefaultWorkspaceIdleTimeout)
	}
}
```

In `HandleMessage`, after the active session is loaded and after idle rotation, but before `ensureInteractiveStateForQueueing`, add:

```go
	if dir := strings.TrimSpace(session.WorkDir); dir != "" {
		wsAgent, wsSessions, err := e.getOrCreateWorkspaceAgent(normalizeWorkspacePath(dir))
		if err != nil {
			session.UnlockWithoutUpdate()
			e.reply(p, msg.ReplyCtx, e.i18n.Tf(MsgWsResolutionError, err))
			return
		}
		agent = wsAgent
		sessions = wsSessions
		resolvedWorkspace = normalizeWorkspacePath(dir)
		interactiveKey = resolvedWorkspace + ":" + msg.SessionKey
	}
```

This intentionally reuses `workspacePool` for per-session directory-specific agents without enabling the full multi-workspace command flow.

- [ ] **Step 4: Wire config in main**

In `cmd/cc-connect/main.go`, after `engine.SetBaseWorkDir(workDir)`, add:

```go
		if strings.TrimSpace(proj.WorkDirMode) == "per_session" {
			base := strings.TrimSpace(proj.WorkDirBase)
			if strings.HasPrefix(base, "~/") {
				home, _ := os.UserHomeDir()
				base = filepath.Join(home, base[2:])
			}
			if !filepath.IsAbs(base) {
				if abs, err := filepath.Abs(base); err == nil {
					base = abs
				}
			}
			if err := os.MkdirAll(base, 0o755); err != nil {
				slog.Error("failed to create work_dir_base", "project", proj.Name, "path", base, "err", err)
				continue
			}
			engine.SetPerSessionWorkDir(proj.WorkDirMode, base)
		}
```

- [ ] **Step 5: Make command workdir reporting session-aware**

In `core/engine.go`, add helper:

```go
func (e *Engine) sessionWorkDirForMessage(msg *Message) string {
	if msg == nil || e.sessions == nil {
		return ""
	}
	s := e.sessions.GetOrCreateActive(msg.SessionKey)
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.WorkDir)
}
```

At the start of `commandWorkDir`, add:

```go
	if dir := e.sessionWorkDirForMessage(msg); dir != "" {
		return normalizeWorkspacePath(dir)
	}
```

- [ ] **Step 6: Run engine test and targeted core tests**

Run:

```powershell
go test ./core -run 'TestEngine_PerSessionWorkDirStartsWorkspaceAgent|TestSessionManager_PerSessionWorkDir' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```powershell
git add core/engine.go cmd/cc-connect/main.go core/engine_test.go
git commit -m "feat: start codex sessions in per-session workdirs"
```

---

### Task 4: Documentation And Example Config

**Files:**
- Modify: `C:\Users\Administrator\Documents\Codex\2026-05-23\cc-connect\cc-connect-src\config.example.toml`
- Modify: `C:\Users\Administrator\Documents\Codex\2026-05-23\cc-connect\cc-connect-src\docs\codex-cloud-deploy-summary.zh-CN.md`

- [ ] **Step 1: Update example config**

In `config.example.toml`, near the project options block that documents `work_dir`, add:

```toml
# Optional: create a separate working directory for each new cc-connect session.
# 可选：每个新的 cc-connect 会话自动创建独立工作目录。
# work_dir_mode = "per_session"
# work_dir_base = "~/Documents/Codex"
```

Also add a note under `[projects.agent.options]`:

```toml
# Do not set work_dir when project-level work_dir_mode = "per_session";
# cc-connect will assign a session-specific work_dir automatically.
# 当项目级 work_dir_mode = "per_session" 时，不要再设置 work_dir；
# cc-connect 会自动给每个会话分配独立工作目录。
```

- [ ] **Step 2: Update Chinese deployment handoff**

In `docs/codex-cloud-deploy-summary.zh-CN.md`, add a “每会话独立目录” section:

```markdown
## 每会话独立目录

如果希望钉钉里每次 `/new` 后都像 Codex App 一样落到一个新的目录，不要在 `[projects.agent.options]` 里写固定 `work_dir`。改用项目级配置：

```toml
[[projects]]
name = "codex-dingtalk"
work_dir_mode = "per_session"
work_dir_base = "/home/codex/Documents/Codex"

[projects.agent]
type = "codex"

[projects.agent.options]
mode = "suggest"
reasoning_effort = "medium"
```

Windows 本机可使用：

```toml
work_dir_base = "C:\\Users\\Administrator\\Documents\\Codex"
```

cc-connect 会在 `work_dir_base/YYYY-MM-DD/` 下为每个新会话创建 `cc-connect-HHmmss-...` 目录，并把该目录持久化到 session store。重启 cc-connect 后，旧会话仍回到原目录。
```

- [ ] **Step 3: Commit docs**

```powershell
git add config.example.toml docs/codex-cloud-deploy-summary.zh-CN.md
git commit -m "docs: document per-session codex workdirs"
```

---

### Task 5: Full Verification, Local Install, And GitHub Sync

**Files:**
- Modify after build: `C:\Users\Administrator\.cc-connect\config.toml`
- Build output: `C:\Users\Administrator\.cc-connect\bin\cc-connect.exe`

- [ ] **Step 1: Run targeted and regression tests**

Run:

```powershell
go test ./config ./core ./daemon ./platform/dingtalk ./tests/release_local/media_pipeline -count=1
```

Expected: PASS.

- [ ] **Step 2: Build Windows binary**

Run:

```powershell
go build -o C:\Users\Administrator\.cc-connect\bin\cc-connect.exe .\cmd\cc-connect
```

Expected: command exits 0 and `C:\Users\Administrator\.cc-connect\bin\cc-connect.exe` timestamp updates.

- [ ] **Step 3: Update local config**

Edit `C:\Users\Administrator\.cc-connect\config.toml` so the project block becomes:

```toml
[[projects]]
name = "codex-dingtalk"
work_dir_mode = "per_session"
work_dir_base = "C:\\Users\\Administrator\\Documents\\Codex"

[projects.agent]
type = "codex"

[projects.agent.options]
mode = "suggest"
reasoning_effort = "medium"
```

Keep existing `[display]`, DingTalk `client_id`, `client_secret`, `allow_from`, and `share_session_in_channel = true` unchanged.

- [ ] **Step 4: Restart local cc-connect**

Run:

```powershell
Stop-Process -Name cc-connect -ErrorAction SilentlyContinue
& C:\Users\Administrator\.cc-connect\launcher\start-cc-connect.ps1
```

Expected: process starts from `C:\Users\Administrator\.cc-connect\bin\cc-connect.exe`.

- [ ] **Step 5: Verify local runtime**

Run:

```powershell
cc-connect --version
Get-Process -Name cc-connect | Select-Object Id,Path,StartTime
Select-String -LiteralPath C:\Users\Administrator\.cc-connect\config.toml -Pattern 'work_dir_mode|work_dir_base|work_dir ='
```

Expected:
- `cc-connect dev`
- process path is `C:\Users\Administrator\.cc-connect\bin\cc-connect.exe`
- config contains `work_dir_mode = "per_session"` and `work_dir_base = "C:\\Users\\Administrator\\Documents\\Codex"`
- config does not contain the old fixed `work_dir = "C:\\Users\\Administrator\\Documents\\Codex\\2026-05-23\\cc-connect\\cc-connect-src"`

- [ ] **Step 6: Manual DingTalk verification**

From DingTalk, send:

```text
/new 测试目录
```

Then send:

```text
请告诉我你当前工作目录
```

Expected:
- cc-connect replies normally.
- A new directory exists under `C:\Users\Administrator\Documents\Codex\2026-05-24\`.
- The reported directory is that new `cc-connect-...` directory or the reply footer/status shows that directory.

- [ ] **Step 7: Push to GitHub**

Run:

```powershell
git status --short --branch
git push fork fix/dingtalk-group-file-send
```

Expected:
- `git status` shows the branch ahead only by the new commits before push, with no uncommitted source changes except the intentionally local `C:\Users\Administrator\.cc-connect\config.toml` outside the repo.
- Push succeeds to `https://github.com/Chriz2333/cc-connect/tree/fix/dingtalk-group-file-send`.

---

## Self-Review

- Spec coverage: The plan covers GitHub sync, local complete install, Windows config, cloud/server docs, per-session directory creation, session persistence, and verification.
- Placeholder scan: No `TBD`, `TODO`, “add appropriate”, or unspecified test requests remain.
- Type consistency: The plan consistently uses project-level `work_dir_mode` / `work_dir_base`, session-level `WorkDir`, and engine method `SetPerSessionWorkDir`.
- Risk note: Task 3 may need small adaptation to existing `core/engine_test.go` test stubs; the implementation requirement is explicit: record and assert the work dir used to start the workspace agent.
