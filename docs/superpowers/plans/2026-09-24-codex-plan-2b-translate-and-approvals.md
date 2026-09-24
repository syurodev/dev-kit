# Codex trong Agent Hub — Kế hoạch 2b: DenyWritePaths, bộ dịch sự kiện, ánh xạ approval

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ba phần thuần (không cần tiến trình Codex) mà `CodexAdapter` (Kế hoạch 2c) dựa vào: sandbox chặn ghi các file chính sách bên trong home đọc-ghi; bộ dịch notification app-server → ACP `session/update`; ánh xạ approval app-server ↔ ACP `session/request_permission`.

**Architecture:** `sandbox.Policy.DenyWritePaths` (bwrap `--ro-bind` xếp sau bind RW nhờ sắp theo độ sâu; Seatbelt `deny file-write*` sau mọi allow + chặn rename tổ tiên). `codexapp/items.go` thêm kiểu item/turn/approval (chỉ trường cần dùng). `agenthub/codex_translate.go` là `codexTranslator` — bảng dispatch theo method, giữ tập item đang mở để đóng khi lượt bị ngắt; output đi qua `ParseSessionUpdate` sẵn có. `agenthub/codex_approvals.go` dựng nút từ `availableDecisions` của chính request, không bao giờ đưa execpolicy amendment.

**Tech Stack:** Go 1.27, bubblewrap (test runtime), capture thật `internal/agenthub/testdata/codex_session_capture.jsonl`.

**Spec:** `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md` §4.3 (file chính sách), §5 (ánh xạ), §7.2 (approval). **Trước đó:** Kế hoạch 2a. **Sau đó:** Kế hoạch 2c — runtime `CodexAdapter` (spawn/jail, Prompt/Cancel/Close, chế độ, resume, đăng nhập, verifier, nối `main.go`, live test).

**Cách plan này được viết:** toàn bộ code đã được viết và chạy trong một git worktree tạm tách từ commit `98697f2`: `go test ./internal/sandbox/ ./internal/codexapp/ ./internal/agenthub/ ./internal/arch/ -race` xanh, `go vet -tags server`, `gofmt`, `go build -tags server` sạch. Mỗi task nhúng **patch test** và **patch hiện thực** lấy thẳng từ đó; cả 8 patch đã `git apply --check` sạch trên nhánh.

**Quy ước chung**

- Chạy từ gốc repo `/home/ptvu3/Downloads/dev-kit-main`, nhánh `feat/codex-plan-1` (git cục bộ, **không push**).
- Commit kết thúc bằng `Co-Authored-By: <model đã làm> <noreply@anthropic.com>`.
- Hết kế hoạch controller chạy `.sync/export.sh`.

## Điểm khác spec cũ (đã cập nhật vào spec)

- File chính sách trong `$CODEX_HOME` (không chỉ `config.toml`): `rules/` (exec policy — luật `allow` tự duyệt lệnh, có thể ngay trong session), `AGENTS.md`/`AGENTS.override.md`, `skills/`. Seatbelt cũ không chặn ghi bên trong vùng đọc-ghi → cần `DenyWritePaths`.
- Không dịch `outputDelta`: Giai đoạn 0 không thấy trên dây; output lấy từ `aggregatedOutput` khi item xong.
- `fileChange` kiểu `add`: `diff` là cả file mới. Không đặt `truncated`.
- Nút approval dựng từ `availableDecisions` (đo được: lệnh shell dưới `untrusted` không có `decline`/`acceptForSession`).

### Task 1: `sandbox.Policy.DenyWritePaths`

**Vì sao:** Codex đọc exec policy (`rules/`), config và chỉ dẫn toàn cục từ `$CODEX_HOME`, mà home đó phải đọc-ghi được cho auth/sessions. Trên Linux một `ReadOnlyPaths` lồng trong vùng RW có tác dụng (bind sắp theo độ sâu), nhưng Seatbelt chỉ *thêm quyền đọc* — trên macOS agent vẫn ghi được `rules/`. Trường mới làm cả hai backend chặn ghi.

**Files:** Create `internal/sandbox/deny_write_test.go`; Modify `internal/sandbox/policy.go` (field + validate), `internal/sandbox/bwrap_args.go` (ro-bind, cả nhánh home riêng), `internal/sandbox/seatbelt_profile.go` (deny-write + chặn rename tổ tiên cho cả deny-write)

Mỗi bước "áp patch": lưu **nguyên văn** khối `diff` vào một file tạm (ví dụ `/tmp/t1-test.patch`, dùng công cụ ghi file — đừng gõ lại), rồi chạy `git apply --check <file> && git apply <file>`. Patch đã được sinh từ code đã biên dịch và chạy test xanh (kể cả `-race`) trên đúng commit gốc của nhánh; nó phải áp sạch. Nếu `git apply --check` báo lỗi: **dừng**, báo NEEDS_CONTEXT kèm output — không sửa tay.

- [ ] **Step 1: Áp patch test**

```diff
diff --git a/internal/sandbox/deny_write_test.go b/internal/sandbox/deny_write_test.go
new file mode 100644
index 0000000..4dc607a
--- /dev/null
+++ b/internal/sandbox/deny_write_test.go
@@ -0,0 +1,113 @@
+package sandbox
+
+import (
+	"context"
+	"errors"
+	"os"
+	"path/filepath"
+	"strings"
+	"testing"
+	"time"
+)
+
+func TestValidateRejectsRelativeDenyWritePath(t *testing.T) {
+	err := Policy{WorkDir: "/tmp/proj", DenyWritePaths: []string{"rules"}}.Validate()
+	if !errors.Is(err, ErrInvalidPolicy) {
+		t.Fatalf("Validate = %v, want ErrInvalidPolicy", err)
+	}
+}
+
+func TestBwrapArgsDenyWriteLandsOnTopOfReadWriteParent(t *testing.T) {
+	root := t.TempDir()
+	home := filepath.Join(root, "codex-home")
+	rules := filepath.Join(home, "rules")
+	if err := os.MkdirAll(rules, 0o755); err != nil {
+		t.Fatal(err)
+	}
+	args := buildBwrapArgs(Policy{
+		WorkDir:        filepath.Join(root, "proj"),
+		ReadWritePaths: []string{home},
+		DenyWritePaths: []string{rules},
+		Env:            map[string]string{"HOME": filepath.Join(root, "home")},
+		PrivateHome:    boolPtr(false),
+	}, Cmd{Path: "/bin/sh"})
+	rw, ro := bindIndex(args, "--bind", home), bindIndex(args, "--ro-bind", rules)
+	if rw < 0 || ro < 0 {
+		t.Fatalf("args %q lack the RW home bind or the RO rules bind", args)
+	}
+	if ro < rw {
+		t.Fatalf("--ro-bind of rules at %d comes before the RW home at %d: rules would end up writable", ro, rw)
+	}
+}
+
+func TestBwrapDenyWriteIsReadOnlyAtRuntime(t *testing.T) {
+	backend := NewBwrapBackend()
+	if err := backend.Available(context.Background()); err != nil {
+		t.Skipf("bwrap unavailable: %v", err)
+	}
+	root := t.TempDir()
+	home := filepath.Join(root, "codex-home")
+	rules := filepath.Join(home, "rules")
+	config := filepath.Join(home, "config.toml")
+	for _, dir := range []string{rules, filepath.Join(root, "proj")} {
+		if err := os.MkdirAll(dir, 0o755); err != nil {
+			t.Fatal(err)
+		}
+	}
+	if err := os.WriteFile(config, []byte("# devkit\n"), 0o644); err != nil {
+		t.Fatal(err)
+	}
+
+	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
+	defer cancel()
+	script := strings.Join([]string{
+		"printf ok > " + filepath.Join(home, "auth.json"),
+		"! printf x > " + filepath.Join(rules, "default.rules") + " 2>/dev/null",
+		"! printf x >> " + config + " 2>/dev/null",
+		"! mv " + rules + " " + rules + ".old 2>/dev/null",
+		"cat " + config + " >/dev/null",
+	}, " && ")
+	proc, err := backend.Start(ctx, Policy{
+		WorkDir:        filepath.Join(root, "proj"),
+		ReadWritePaths: []string{home},
+		DenyWritePaths: []string{rules, config},
+		Env:            map[string]string{"HOME": filepath.Join(root, "home")},
+		PrivateHome:    boolPtr(false),
+	}, Cmd{Path: "/bin/sh", Args: []string{"-c", script}})
+	if err != nil {
+		t.Fatal(err)
+	}
+	if err := proc.Wait(); err != nil {
+		t.Fatalf("sandboxed shell: %v (home writable; rules, config.toml and the rules rename must not be)", err)
+	}
+	if got, _ := os.ReadFile(filepath.Join(home, "auth.json")); string(got) != "ok" {
+		t.Fatalf("auth.json = %q, want the RW home to stay writable", got)
+	}
+	if _, err := os.Stat(filepath.Join(rules, "default.rules")); err == nil {
+		t.Fatal("a rule file was written under a deny-write path")
+	}
+	if got, _ := os.ReadFile(config); string(got) != "# devkit\n" {
+		t.Fatalf("config.toml = %q, want it untouched", got)
+	}
+}
+
+func TestSeatbeltProfileDenyWritePaths(t *testing.T) {
+	prof := buildSeatbeltProfile(Policy{
+		WorkDir:        "/tmp/proj",
+		ReadWritePaths: []string{"/Users/alice/Library/Application Support/DevKit/agent-homes/codex"},
+		DenyWritePaths: []string{"/Users/alice/Library/Application Support/DevKit/agent-homes/codex/rules"},
+		Env:            map[string]string{"HOME": "/Users/alice"},
+	}.withDefaults())
+	block := "(deny file-write*\n  (subpath \"/Users/alice/Library/Application Support/DevKit/agent-homes/codex/rules\")"
+	at := strings.Index(prof, block)
+	if at < 0 {
+		t.Fatalf("expected a deny-write block for rules: %s", prof)
+	}
+	if strings.LastIndex(prof, "(allow ") > at {
+		t.Fatalf("deny-write must come after every allow (the later rule wins): %s", prof)
+	}
+	want := `(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal "/Users/alice/Library/Application Support/DevKit/agent-homes/codex")))`
+	if !strings.Contains(prof, want) {
+		t.Fatalf("the deny-write path's parent must not be renamable: %s", prof)
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/sandbox/ -run DenyWrite -count=1`
Expected: FAIL build — `unknown field DenyWritePaths in struct literal of type Policy`.

- [ ] **Step 3: Áp patch hiện thực**

```diff
diff --git a/internal/sandbox/bwrap_args.go b/internal/sandbox/bwrap_args.go
index 63a6f00..9b501ec 100644
--- a/internal/sandbox/bwrap_args.go
+++ b/internal/sandbox/bwrap_args.go
@@ -117,6 +117,14 @@ func buildBwrapArgs(p Policy, c Cmd) []string {
 			binds = append(binds, bwrapBind{"--ro-bind", path})
 		}
 	}
+	// Appended last: at equal depth the stable sort keeps them after a RW
+	// bind of the same path, and deeper ones sort after their RW parent, so a
+	// deny-write path always lands read-only on top.
+	for _, path := range p.DenyWritePaths {
+		if pathExists(path) {
+			binds = append(binds, bwrapBind{"--ro-bind", path})
+		}
+	}
 	appendNestedBinds(&args, binds)
 
 	denyPaths := p.EffectiveDenyRead()
@@ -133,6 +141,7 @@ func buildBwrapArgs(p Policy, c Cmd) []string {
 		}
 		homeBinds = appendHomeAllowlist(homeBinds, home, denyPaths, p.ReadWritePaths, "--bind")
 		homeBinds = appendHomeAllowlist(homeBinds, home, denyPaths, p.ReadOnlyPaths, "--ro-bind")
+		homeBinds = appendHomeAllowlist(homeBinds, home, denyPaths, p.DenyWritePaths, "--ro-bind")
 		appendNestedBinds(&args, homeBinds)
 	}
 
diff --git a/internal/sandbox/policy.go b/internal/sandbox/policy.go
index b62f856..2c9446c 100644
--- a/internal/sandbox/policy.go
+++ b/internal/sandbox/policy.go
@@ -25,6 +25,14 @@ type Policy struct {
 	ReadOnlyPaths  []string
 	ReadWritePaths []string // extra RW beyond WorkDir
 	DenyReadPaths  []string // added on top of defaults
+	// DenyWritePaths are never writable, even inside WorkDir, a
+	// ReadWritePaths entry or a shared home. They stay readable only when
+	// another grant covers them. For the files and directories that steer a
+	// vendor's own policy from inside a home DevKit hands it read-write —
+	// Codex reads approval rules and config from $CODEX_HOME. A path that
+	// does not exist is skipped on Linux (bwrap binds only what exists), so
+	// the caller creates it first.
+	DenyWritePaths []string
 	// AllowUnixSockets are unix socket paths the process may connect to —
 	// the session's identity-bound DevKit MCP endpoint. Everything else stays
 	// closed (NetworkHost is IP-only).
@@ -60,6 +68,11 @@ func (p Policy) Validate() error {
 			return ErrInvalidPolicy
 		}
 	}
+	for _, path := range p.DenyWritePaths {
+		if !filepath.IsAbs(path) {
+			return ErrInvalidPolicy
+		}
+	}
 	if err := p.validateBindPaths(); err != nil {
 		return err
 	}
diff --git a/internal/sandbox/seatbelt_profile.go b/internal/sandbox/seatbelt_profile.go
index 9f6b301..ad17eea 100644
--- a/internal/sandbox/seatbelt_profile.go
+++ b/internal/sandbox/seatbelt_profile.go
@@ -143,6 +143,16 @@ func buildSeatbeltProfile(p Policy) string {
 	// read-only deny leaves the subtree writable whenever WorkDir or a RW path
 	// is an ancestor of it (WorkDir=$HOME made ~/.ssh/authorized_keys
 	// appendable). Emitted last because the later matching rule wins.
+	// Deny-write after every allow: SBPL's later matching rule wins, so these
+	// stay read-only inside WorkDir, a RW path or a shared home.
+	if len(p.DenyWritePaths) > 0 {
+		b.WriteString("(deny file-write*\n")
+		for _, path := range p.DenyWritePaths {
+			writeSBPLPath(&b, path)
+		}
+		b.WriteString(")\n")
+	}
+
 	denyPaths := p.EffectiveDenyRead()
 	if len(denyPaths) > 0 {
 		b.WriteString("(deny file-read* file-write*\n")
@@ -150,13 +160,15 @@ func buildSeatbeltProfile(p Policy) string {
 			writeSBPLPath(&b, path)
 		}
 		b.WriteString(")\n")
-		// (subpath X) stops protecting X once an ancestor is renamed: with
-		// WorkDir=/Users, `mv /Users/me /Users/x` makes /Users/x/.ssh writable
-		// (verified live). Deny rename/unlink of every ancestor directory,
-		// the same rule Codex uses for its protected paths.
-		for _, dir := range pathAncestors(denyPaths) {
-			b.WriteString("(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal \"" + escapeSBPL(dir) + "\")))\n")
-		}
+	}
+	// (subpath X) stops protecting X once an ancestor is renamed: with
+	// WorkDir=/Users, `mv /Users/me /Users/x` makes /Users/x/.ssh writable
+	// (verified live). Deny rename/unlink of every ancestor directory, the
+	// same rule Codex uses for its protected paths — for deny-write paths as
+	// much as deny-read ones.
+	protected := append(append([]string(nil), denyPaths...), p.DenyWritePaths...)
+	for _, dir := range pathAncestors(protected) {
+		b.WriteString("(deny file-write-unlink (require-all (vnode-type DIRECTORY) (literal \"" + escapeSBPL(dir) + "\")))\n")
 	}
 
 	return b.String()
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/sandbox/ -count=1 && go test ./internal/sandbox/ -run DenyWrite -count=1 -v`
Expected: PASS. Rồi `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/deny_write_test.go internal/sandbox/policy.go internal/sandbox/bwrap_args.go internal/sandbox/seatbelt_profile.go
git commit -m "feat(sandbox): DenyWritePaths — never writable, even inside a RW grant

bwrap binds them read-only on top of their RW parent; Seatbelt denies
file-write* after every allow and protects their ancestors from rename.
For the policy files Codex reads from a home it must be able to write.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

> Test runtime `TestBwrapDenyWriteIsReadOnlyAtRuntime` chạy bwrap thật (máy này có). Nó phải **PASS**, không SKIP; nếu SKIP vì bwrap không dùng được, ghi lại trong báo cáo.

### Task 2: Kiểu item/turn/approval trong `codexapp` (`items.go`)

**Vì sao:** Bộ dịch và bộ ánh xạ cần đọc item, delta, usage, plan, turn và các server request approval. Chỉ mô hình hoá trường dùng tới; item gốc vẫn đi tiếp dạng raw (FR-8). Test giải mã chính capture thật.

**Files:** Create `internal/codexapp/items.go`, `internal/codexapp/items_test.go`

Mỗi bước "áp patch": lưu **nguyên văn** khối `diff` vào một file tạm (ví dụ `/tmp/t2-test.patch`, dùng công cụ ghi file — đừng gõ lại), rồi chạy `git apply --check <file> && git apply <file>`. Patch đã được sinh từ code đã biên dịch và chạy test xanh (kể cả `-race`) trên đúng commit gốc của nhánh; nó phải áp sạch. Nếu `git apply --check` báo lỗi: **dừng**, báo NEEDS_CONTEXT kèm output — không sửa tay.

- [ ] **Step 1: Áp patch test**

```diff
diff --git a/internal/codexapp/items_test.go b/internal/codexapp/items_test.go
new file mode 100644
index 0000000..0096dbf
--- /dev/null
+++ b/internal/codexapp/items_test.go
@@ -0,0 +1,103 @@
+package codexapp
+
+import (
+	"encoding/json"
+	"testing"
+)
+
+// items returns the item of every item/completed in session, by type.
+func items(t *testing.T, lines []captureLine, session string) map[string][]json.RawMessage {
+	t.Helper()
+	out := map[string][]json.RawMessage{}
+	for _, l := range lines {
+		if l.Session != session || l.Msg.Method != MethodItemCompleted {
+			continue
+		}
+		var n ItemNotification
+		if err := json.Unmarshal(l.Msg.Params, &n); err != nil {
+			t.Fatal(err)
+		}
+		var h ItemHeader
+		if err := json.Unmarshal(n.Item, &h); err != nil || h.ID == "" {
+			t.Fatalf("item header %s: %v", n.Item, err)
+		}
+		out[h.Type] = append(out[h.Type], n.Item)
+	}
+	return out
+}
+
+func TestCapturedItemsDecode(t *testing.T) {
+	got := items(t, loadCapture(t), "main")
+
+	var cmd CommandExecutionItem
+	if err := json.Unmarshal(got[ItemCommandExecution][0], &cmd); err != nil {
+		t.Fatal(err)
+	}
+	if cmd.Status != ItemCompleted || cmd.ExitCode == nil || *cmd.ExitCode != 0 ||
+		cmd.AggregatedOutput == nil || len(cmd.CommandActions) == 0 || cmd.CommandActions[0].Command == "" {
+		t.Fatalf("command item = %+v", cmd)
+	}
+
+	var fc FileChangeItem
+	if err := json.Unmarshal(got[ItemFileChange][0], &fc); err != nil {
+		t.Fatal(err)
+	}
+	if fc.Status != ItemCompleted || len(fc.Changes) != 2 {
+		t.Fatalf("file change item = %+v", fc)
+	}
+	if fc.Changes[0].Kind.Type != "update" || fc.Changes[0].Diff != "@@ -2 +2,2 @@\n line two\n+line three\n" {
+		t.Fatalf("update change = %+v", fc.Changes[0])
+	}
+	if fc.Changes[1].Kind.Type != "add" || fc.Changes[1].Kind.MovePath != nil {
+		t.Fatalf("add change = %+v", fc.Changes[1])
+	}
+
+	var msg AgentMessageItem
+	if err := json.Unmarshal(got[ItemAgentMessage][0], &msg); err != nil || msg.Text == "" {
+		t.Fatalf("agent message = %+v, %v", msg, err)
+	}
+}
+
+func TestCapturedApprovalAndTurnShapes(t *testing.T) {
+	lines := loadCapture(t)
+	var sawCommand, sawFile, sawUsage, sawInterrupted bool
+	for _, l := range lines {
+		switch l.Msg.Method {
+		case MethodCommandApproval:
+			var p CommandApprovalParams
+			if err := json.Unmarshal(l.Msg.Params, &p); err != nil || p.ItemID == "" || len(p.AvailableDecisions) == 0 {
+				t.Fatalf("command approval = %+v, %v", p, err)
+			}
+			sawCommand = true
+		case MethodFileChangeApproval:
+			var p FileChangeApprovalParams
+			if err := json.Unmarshal(l.Msg.Params, &p); err != nil || p.ItemID == "" {
+				t.Fatalf("file change approval = %+v, %v", p, err)
+			}
+			sawFile = true
+		case MethodTokenUsageUpdated:
+			var n TokenUsageUpdatedNotification
+			if err := json.Unmarshal(l.Msg.Params, &n); err != nil || n.TokenUsage.Last.TotalTokens == 0 ||
+				n.TokenUsage.ModelContextWindow == nil || *n.TokenUsage.ModelContextWindow == 0 {
+				t.Fatalf("token usage = %+v, %v", n, err)
+			}
+			sawUsage = true
+		case MethodTurnCompleted:
+			var n TurnCompletedNotification
+			if err := json.Unmarshal(l.Msg.Params, &n); err != nil {
+				t.Fatal(err)
+			}
+			if n.Turn.Status == TurnInterrupted {
+				sawInterrupted = true
+			}
+		}
+	}
+	if !sawCommand || !sawFile || !sawUsage || !sawInterrupted {
+		t.Fatalf("capture coverage: command=%v file=%v usage=%v interrupted=%v", sawCommand, sawFile, sawUsage, sawInterrupted)
+	}
+}
+
+func TestApprovalResponsesEncode(t *testing.T) {
+	sameJSON(t, "approval", ApprovalResponse{Decision: DecisionDecline}, json.RawMessage(`{"decision":"decline"}`))
+	sameJSON(t, "permissions", PermissionsApprovalResponse{Scope: "turn"}, json.RawMessage(`{"permissions":{},"scope":"turn"}`))
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/codexapp/ -count=1`
Expected: FAIL build — `undefined: MethodItemCompleted` / `ItemNotification`.

- [ ] **Step 3: Áp patch hiện thực**

```diff
diff --git a/internal/codexapp/items.go b/internal/codexapp/items.go
new file mode 100644
index 0000000..7c3dfd6
--- /dev/null
+++ b/internal/codexapp/items.go
@@ -0,0 +1,237 @@
+package codexapp
+
+import "encoding/json"
+
+// Item and turn notifications, and the approval requests about items: what
+// agenthub's translator reads. Only the fields a translation uses are
+// modelled; the whole item still travels on as raw JSON (FR-8), so a field
+// not listed here is kept, just not interpreted. Shapes are from
+// `codex app-server generate-ts` for 0.147.0 and checked against the Phase 0
+// capture in items_test.go.
+
+const (
+	MethodItemStarted           = "item/started"
+	MethodItemCompleted         = "item/completed"
+	MethodAgentMessageDelta     = "item/agentMessage/delta"
+	MethodReasoningSummaryDelta = "item/reasoning/summaryTextDelta"
+	MethodTurnPlanUpdated       = "turn/plan/updated"
+	MethodTokenUsageUpdated     = "thread/tokenUsage/updated"
+	MethodThreadNameUpdated     = "thread/name/updated"
+	MethodTurnCompleted         = "turn/completed"
+	MethodError                 = "error"
+
+	// Server requests about an item.
+	MethodCommandApproval     = "item/commandExecution/requestApproval"
+	MethodFileChangeApproval  = "item/fileChange/requestApproval"
+	MethodPermissionsApproval = "item/permissions/requestApproval"
+)
+
+// Item types.
+const (
+	ItemAgentMessage     = "agentMessage"
+	ItemReasoning        = "reasoning"
+	ItemUserMessage      = "userMessage"
+	ItemCommandExecution = "commandExecution"
+	ItemFileChange       = "fileChange"
+	ItemMcpToolCall      = "mcpToolCall"
+	ItemWebSearch        = "webSearch"
+	ItemHookPrompt       = "hookPrompt"
+)
+
+// Statuses shared by command, file-change and MCP items.
+const (
+	ItemInProgress = "inProgress"
+	ItemCompleted  = "completed"
+	ItemFailed     = "failed"
+	ItemDeclined   = "declined"
+)
+
+// Approval decisions. acceptWithExecpolicyAmendment is deliberately absent:
+// it writes a permanent rule, and an approval must never do that.
+const (
+	DecisionAccept           = "accept"
+	DecisionAcceptForSession = "acceptForSession"
+	DecisionDecline          = "decline"
+	DecisionCancel           = "cancel"
+)
+
+// ItemNotification is item/started and item/completed.
+type ItemNotification struct {
+	ThreadID string          `json:"threadId"`
+	TurnID   string          `json:"turnId"`
+	Item     json.RawMessage `json:"item"`
+}
+
+// ItemHeader is what every item carries.
+type ItemHeader struct {
+	Type string `json:"type"`
+	ID   string `json:"id"`
+}
+
+type AgentMessageItem struct {
+	ID    string  `json:"id"`
+	Text  string  `json:"text"`
+	Phase *string `json:"phase"`
+}
+
+type ReasoningItem struct {
+	ID      string   `json:"id"`
+	Summary []string `json:"summary"`
+}
+
+// CommandAction is Codex's parse of what a command does. Its Command is the
+// command as the model wrote it; CommandExecutionItem.Command is that wrapped
+// in the shell invocation (`/usr/bin/zsh -lc "…"`).
+type CommandAction struct {
+	Type    string `json:"type"`
+	Command string `json:"command"`
+}
+
+type CommandExecutionItem struct {
+	ID               string          `json:"id"`
+	Command          string          `json:"command"`
+	Cwd              string          `json:"cwd"`
+	Status           string          `json:"status"`
+	CommandActions   []CommandAction `json:"commandActions"`
+	AggregatedOutput *string         `json:"aggregatedOutput"`
+	ExitCode         *int            `json:"exitCode"`
+	DurationMs       *int64          `json:"durationMs"`
+}
+
+type PatchChangeKind struct {
+	Type     string  `json:"type"` // add | delete | update
+	MovePath *string `json:"move_path"`
+}
+
+// FileUpdateChange is one file in a fileChange item. Diff is a unified diff's
+// hunks for an update, and the whole content for an add (measured: no @@).
+type FileUpdateChange struct {
+	Path string          `json:"path"`
+	Kind PatchChangeKind `json:"kind"`
+	Diff string          `json:"diff"`
+}
+
+type FileChangeItem struct {
+	ID      string             `json:"id"`
+	Changes []FileUpdateChange `json:"changes"`
+	Status  string             `json:"status"`
+}
+
+type McpToolCallError struct {
+	Message string `json:"message"`
+}
+
+type McpToolCallItem struct {
+	ID        string            `json:"id"`
+	Server    string            `json:"server"`
+	Tool      string            `json:"tool"`
+	Status    string            `json:"status"`
+	Arguments json.RawMessage   `json:"arguments"`
+	Result    json.RawMessage   `json:"result"`
+	Error     *McpToolCallError `json:"error"`
+}
+
+type WebSearchItem struct {
+	ID    string `json:"id"`
+	Query string `json:"query"`
+}
+
+// DeltaNotification is item/agentMessage/delta and
+// item/reasoning/summaryTextDelta.
+type DeltaNotification struct {
+	ThreadID string `json:"threadId"`
+	TurnID   string `json:"turnId"`
+	ItemID   string `json:"itemId"`
+	Delta    string `json:"delta"`
+}
+
+type TurnPlanStep struct {
+	Step   string `json:"step"`
+	Status string `json:"status"` // pending | inProgress | completed
+}
+
+type TurnPlanUpdatedNotification struct {
+	Explanation *string        `json:"explanation"`
+	Plan        []TurnPlanStep `json:"plan"`
+}
+
+type TokenUsageBreakdown struct {
+	TotalTokens           int64 `json:"totalTokens"`
+	InputTokens           int64 `json:"inputTokens"`
+	CachedInputTokens     int64 `json:"cachedInputTokens"`
+	OutputTokens          int64 `json:"outputTokens"`
+	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
+}
+
+type ThreadTokenUsage struct {
+	Total              TokenUsageBreakdown `json:"total"`
+	Last               TokenUsageBreakdown `json:"last"`
+	ModelContextWindow *int64              `json:"modelContextWindow"`
+}
+
+type TokenUsageUpdatedNotification struct {
+	ThreadID   string           `json:"threadId"`
+	TurnID     string           `json:"turnId"`
+	TokenUsage ThreadTokenUsage `json:"tokenUsage"`
+}
+
+type ThreadNameUpdatedNotification struct {
+	ThreadID   string `json:"threadId"`
+	ThreadName string `json:"threadName,omitempty"`
+}
+
+type TurnCompletedNotification struct {
+	ThreadID string `json:"threadId"`
+	Turn     Turn   `json:"turn"`
+}
+
+type ErrorNotification struct {
+	Error     TurnError `json:"error"`
+	WillRetry bool      `json:"willRetry"`
+	ThreadID  string    `json:"threadId"`
+	TurnID    string    `json:"turnId"`
+}
+
+// CommandApprovalParams is item/commandExecution/requestApproval.
+// AvailableDecisions lists what this request accepts, as strings or objects
+// (measured under untrusted: accept, an execpolicy amendment, cancel — no
+// decline, no acceptForSession). nil means the field was absent.
+type CommandApprovalParams struct {
+	ThreadID           string            `json:"threadId"`
+	TurnID             string            `json:"turnId"`
+	ItemID             string            `json:"itemId"`
+	Reason             *string           `json:"reason"`
+	Command            *string           `json:"command"`
+	Cwd                *string           `json:"cwd"`
+	CommandActions     []CommandAction   `json:"commandActions"`
+	AvailableDecisions []json.RawMessage `json:"availableDecisions"`
+}
+
+type FileChangeApprovalParams struct {
+	ThreadID  string  `json:"threadId"`
+	TurnID    string  `json:"turnId"`
+	ItemID    string  `json:"itemId"`
+	Reason    *string `json:"reason"`
+	GrantRoot *string `json:"grantRoot"`
+}
+
+type PermissionsApprovalParams struct {
+	ThreadID    string          `json:"threadId"`
+	TurnID      string          `json:"turnId"`
+	ItemID      string          `json:"itemId"`
+	Reason      *string         `json:"reason"`
+	Permissions json.RawMessage `json:"permissions"`
+}
+
+// ApprovalResponse answers a command or file-change approval.
+type ApprovalResponse struct {
+	Decision string `json:"decision"`
+}
+
+// PermissionsApprovalResponse answers item/permissions/requestApproval.
+// DevKit always grants nothing: under externalSandbox the jail, not Codex,
+// decides access.
+type PermissionsApprovalResponse struct {
+	Permissions struct{} `json:"permissions"`
+	Scope       string   `json:"scope"`
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/codexapp/ -count=1 -race && go test ./internal/arch/ -count=1`
Expected: PASS. Rồi `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/codexapp/items.go internal/codexapp/items_test.go
git commit -m "feat(codexapp): item, turn and approval types

Only the fields a translation reads, checked against the Phase 0
capture. acceptWithExecpolicyAmendment is deliberately not a decision.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 3: Bộ dịch `codexTranslator` (`codex_translate.go`)

**Vì sao:** Core, store và UI chỉ nói ACP (FR-7). Bộ dịch biến notification app-server thành đúng các `session/update` mà `ParseSessionUpdate` đọc. Golden test phát lại phiên thật: chữ stream về khớp message hoàn chỉnh; diff đúng; usage đúng; lệnh bị ngắt được đóng `failed` (Codex không bao giờ gửi `item/completed` cho nó).

**Files:** Create `internal/agenthub/codex_translate.go`, `internal/agenthub/codex_translate_test.go`

Mỗi bước "áp patch": lưu **nguyên văn** khối `diff` vào một file tạm (ví dụ `/tmp/t3-test.patch`, dùng công cụ ghi file — đừng gõ lại), rồi chạy `git apply --check <file> && git apply <file>`. Patch đã được sinh từ code đã biên dịch và chạy test xanh (kể cả `-race`) trên đúng commit gốc của nhánh; nó phải áp sạch. Nếu `git apply --check` báo lỗi: **dừng**, báo NEEDS_CONTEXT kèm output — không sửa tay.

- [ ] **Step 1: Áp patch test**

```diff
diff --git a/internal/agenthub/codex_translate_test.go b/internal/agenthub/codex_translate_test.go
new file mode 100644
index 0000000..8822fa0
--- /dev/null
+++ b/internal/agenthub/codex_translate_test.go
@@ -0,0 +1,302 @@
+package agenthub
+
+import (
+	"bufio"
+	"encoding/json"
+	"os"
+	"strings"
+	"testing"
+
+	"devkit/internal/codexapp"
+)
+
+type codexCaptureLine struct {
+	Dir     string           `json:"dir"`
+	Session string           `json:"session"`
+	Msg     codexapp.Message `json:"msg"`
+}
+
+func loadCodexCapture(t *testing.T, session string) []codexapp.Message {
+	t.Helper()
+	f, err := os.Open("testdata/codex_session_capture.jsonl")
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer f.Close()
+	var out []codexapp.Message
+	sc := bufio.NewScanner(f)
+	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
+	for sc.Scan() {
+		var l codexCaptureLine
+		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
+			t.Fatal(err)
+		}
+		if l.Session == session && l.Dir == "in" && l.Msg.IsNotification() {
+			out = append(out, l.Msg)
+		}
+	}
+	return out
+}
+
+// replay runs a captured session through a fresh translator and parses every
+// update the way the store and UI will.
+func replay(t *testing.T, session string) ([]SessionUpdate, []codexUpdate) {
+	t.Helper()
+	tr := newCodexTranslator()
+	var parsed []SessionUpdate
+	var raw []codexUpdate
+	for _, m := range loadCodexCapture(t, session) {
+		for _, u := range tr.Translate(m.Method, m.Params) {
+			su := ParseSessionUpdate(u.Update)
+			if su.Kind == UpdateUnknown {
+				t.Fatalf("translator emitted an update ParseSessionUpdate does not know: %s", u.Update)
+			}
+			parsed = append(parsed, su)
+			raw = append(raw, u)
+		}
+	}
+	return parsed, raw
+}
+
+func TestCodexReplayMainSession(t *testing.T) {
+	updates, raw := replay(t, "main")
+
+	// Every word the agent said reaches the transcript exactly once: the
+	// streamed chunks equal the completed messages, in order.
+	var streamed strings.Builder
+	for _, u := range updates {
+		if u.Kind == UpdateAgentMessageChunk {
+			streamed.WriteString(u.Content.Text)
+		}
+	}
+	var completed strings.Builder
+	for _, m := range loadCodexCapture(t, "main") {
+		if m.Method != codexapp.MethodItemCompleted {
+			continue
+		}
+		var n codexapp.ItemNotification
+		_ = json.Unmarshal(m.Params, &n)
+		var msg codexapp.AgentMessageItem
+		var h codexapp.ItemHeader
+		_ = json.Unmarshal(n.Item, &h)
+		if h.Type == codexapp.ItemAgentMessage && json.Unmarshal(n.Item, &msg) == nil {
+			completed.WriteString(msg.Text)
+		}
+	}
+	if streamed.String() == "" || streamed.String() != completed.String() {
+		t.Fatalf("streamed text %q\nwant the completed messages %q", streamed.String(), completed.String())
+	}
+
+	calls := map[string]*ToolCall{}
+	var order []string
+	for _, u := range updates {
+		if u.ToolCall == nil {
+			continue
+		}
+		c := u.ToolCall
+		if prev, ok := calls[c.ID]; ok {
+			if c.Status != "" {
+				prev.Status = c.Status
+			}
+			if len(c.Content) > 0 {
+				prev.Content = c.Content
+			}
+			continue
+		}
+		copied := *c
+		calls[c.ID] = &copied
+		order = append(order, c.ID)
+	}
+	var commands, edits int
+	for _, id := range order {
+		c := calls[id]
+		if c.Status != "completed" {
+			t.Fatalf("tool call %s ended %q, want completed", id, c.Status)
+		}
+		switch c.Kind {
+		case "execute":
+			commands++
+			if strings.HasPrefix(c.Title, "/usr/bin/zsh") {
+				t.Fatalf("command title %q still carries the shell wrapper", c.Title)
+			}
+		case "edit":
+			edits++
+			if c.Title != "Edit 2 files" || len(c.Content) != 2 {
+				t.Fatalf("file change = %+v", c)
+			}
+			notes, plan := c.Content[0], c.Content[1]
+			if notes.OldText == nil || *notes.OldText != "line two\n" || notes.NewText != "line two\nline three\n" {
+				t.Fatalf("notes.txt diff = old %v new %q", notes.OldText, notes.NewText)
+			}
+			if plan.OldText != nil || !strings.HasPrefix(plan.NewText, "- [ ] ") {
+				t.Fatalf("plan.md (new file) diff = old %v new %q", plan.OldText, plan.NewText)
+			}
+		}
+	}
+	if commands != 2 || edits != 1 {
+		t.Fatalf("tool calls: %d commands, %d edits; want 2 and 1", commands, edits)
+	}
+
+	var lastUsage *Usage
+	for _, u := range updates {
+		if u.Usage != nil {
+			lastUsage = u.Usage
+		}
+	}
+	if lastUsage == nil || lastUsage.Used != 12957 || lastUsage.Size != 258400 {
+		t.Fatalf("last usage = %+v, want used 12957 of 258400", lastUsage)
+	}
+
+	// FR-8: every completed item's raw Codex JSON rides along as meta.
+	var withMeta int
+	for _, u := range raw {
+		if len(u.Meta) > 0 {
+			withMeta++
+		}
+	}
+	if withMeta == 0 {
+		t.Fatal("no update carried the raw Codex item")
+	}
+}
+
+func TestCodexReplayInterruptClosesRunningCommand(t *testing.T) {
+	updates, _ := replay(t, "interrupt")
+	var id, final string
+	for _, u := range updates {
+		if u.ToolCall == nil || u.ToolCall.Kind != "execute" && id == "" {
+			continue
+		}
+		if id == "" {
+			id = u.ToolCall.ID
+		}
+		if u.ToolCall.ID == id && u.ToolCall.Status != "" {
+			final = u.ToolCall.Status
+		}
+	}
+	if id == "" || final != "failed" {
+		t.Fatalf("interrupted command %q ended %q, want failed (Codex never completes it)", id, final)
+	}
+}
+
+func translateOne(t *testing.T, tr *codexTranslator, method string, params any) []SessionUpdate {
+	t.Helper()
+	raw, err := json.Marshal(params)
+	if err != nil {
+		t.Fatal(err)
+	}
+	var out []SessionUpdate
+	for _, u := range tr.Translate(method, raw) {
+		out = append(out, ParseSessionUpdate(u.Update))
+	}
+	return out
+}
+
+func item(v any) map[string]any {
+	return map[string]any{"threadId": "th", "turnId": "tu", "item": v}
+}
+
+func TestCodexCompletedMessageWithoutDeltasIsSentWhole(t *testing.T) {
+	tr := newCodexTranslator()
+	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{"type": "agentMessage", "id": "m1", "text": "hello"}))
+	if len(got) != 1 || got[0].Kind != UpdateAgentMessageChunk || got[0].Content.Text != "hello" {
+		t.Fatalf("got %+v", got)
+	}
+	translateOne(t, tr, codexapp.MethodAgentMessageDelta, map[string]any{"itemId": "m2", "delta": "hi"})
+	if again := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{"type": "agentMessage", "id": "m2", "text": "hi"})); len(again) != 0 {
+		t.Fatalf("a streamed message was sent twice: %+v", again)
+	}
+}
+
+func TestCodexReasoningSummary(t *testing.T) {
+	tr := newCodexTranslator()
+	got := translateOne(t, tr, codexapp.MethodReasoningSummaryDelta, map[string]any{"itemId": "r1", "delta": "thinking"})
+	if len(got) != 1 || got[0].Kind != UpdateAgentThoughtChunk || got[0].Content.Text != "thinking" {
+		t.Fatalf("got %+v", got)
+	}
+	got = translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{"type": "reasoning", "id": "r2", "summary": []string{"a", "b"}}))
+	if len(got) != 1 || got[0].Content.Text != "a\n\nb" {
+		t.Fatalf("unstreamed summary = %+v", got)
+	}
+}
+
+func TestCodexDeclinedCommandFails(t *testing.T) {
+	tr := newCodexTranslator()
+	cmd := map[string]any{"type": "commandExecution", "id": "c1", "command": "rm -rf x", "cwd": "/p", "status": "inProgress"}
+	translateOne(t, tr, codexapp.MethodItemStarted, item(cmd))
+	cmd["status"] = "declined"
+	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(cmd))
+	last := got[len(got)-1].ToolCall
+	if last.Status != "failed" || len(last.Content) != 1 || last.Content[0].Content.Text != "Declined." {
+		t.Fatalf("declined command = %+v", last)
+	}
+}
+
+func TestCodexCommandOutputKeepsTheTail(t *testing.T) {
+	tr := newCodexTranslator()
+	out := strings.Repeat("x", codexOutputLimit) + "THE END"
+	exit := 1
+	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{
+		"type": "commandExecution", "id": "c1", "command": "make", "cwd": "/p", "status": "completed",
+		"aggregatedOutput": out, "exitCode": exit,
+	}))
+	if len(got) != 2 || got[0].ToolCall.Status != "in_progress" {
+		t.Fatalf("a command completed without a start must be shown first: %+v", got)
+	}
+	last := got[1].ToolCall
+	text := last.Content[0].Content.Text
+	if last.Status != "failed" || !strings.HasSuffix(text, "THE END") || len(text) > codexOutputLimit+8 {
+		t.Fatalf("status %q, output %d bytes ending %q", last.Status, len(text), text[len(text)-10:])
+	}
+}
+
+func TestCodexUnknownItemIsShownNotDropped(t *testing.T) {
+	tr := newCodexTranslator()
+	got := translateOne(t, tr, codexapp.MethodItemCompleted, item(map[string]any{"type": "imageGeneration", "id": "g1"}))
+	if len(got) != 1 || got[0].ToolCall == nil || got[0].ToolCall.Title != "imageGeneration" || got[0].ToolCall.Kind != "other" {
+		t.Fatalf("got %+v", got)
+	}
+}
+
+func TestCodexPlanNameAndIgnored(t *testing.T) {
+	tr := newCodexTranslator()
+	got := translateOne(t, tr, codexapp.MethodTurnPlanUpdated, map[string]any{"plan": []map[string]string{
+		{"step": "read", "status": "completed"}, {"step": "edit", "status": "inProgress"}, {"step": "test", "status": "pending"},
+	}})
+	if len(got) != 1 || len(got[0].Plan) != 3 || got[0].Plan[1].Status != "in_progress" || got[0].Plan[2].Status != "pending" {
+		t.Fatalf("plan = %+v", got)
+	}
+	got = translateOne(t, tr, codexapp.MethodThreadNameUpdated, map[string]any{"threadId": "th", "threadName": "Fix login"})
+	if len(got) != 1 || got[0].Title != "Fix login" {
+		t.Fatalf("name = %+v", got)
+	}
+	for _, m := range []string{"hook/started", "thread/status/changed", "remoteControl/status/changed", "turn/diff/updated"} {
+		if codexTranslates(m) {
+			t.Fatalf("%s must not be translated", m)
+		}
+	}
+}
+
+func TestCodexHunkTexts(t *testing.T) {
+	before, after := codexHunkTexts("--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n a\n-b\n+B\n@@ -9 +9 @@\n-x\n+y\n\\ No newline at end of file\n")
+	if before != "a\nb\n…\nx\n" || after != "a\nB\n…\ny\n" {
+		t.Fatalf("before %q after %q", before, after)
+	}
+}
+
+func TestCodexFileChangeKinds(t *testing.T) {
+	move := "/p/new.go"
+	cases := []struct {
+		changes []codexapp.FileUpdateChange
+		kind    string
+		title   string
+	}{
+		{[]codexapp.FileUpdateChange{{Path: "/p/a.go", Kind: codexapp.PatchChangeKind{Type: "delete"}}}, "delete", "Delete a.go"},
+		{[]codexapp.FileUpdateChange{{Path: "/p/old.go", Kind: codexapp.PatchChangeKind{Type: "update", MovePath: &move}}}, "move", "Move old.go"},
+		{[]codexapp.FileUpdateChange{{Path: "/p/n.go", Kind: codexapp.PatchChangeKind{Type: "add"}}}, "edit", "Create n.go"},
+	}
+	for _, c := range cases {
+		if k, title := codexFileChangeKind(c.changes), codexFileChangeTitle(c.changes); k != c.kind || title != c.title {
+			t.Fatalf("%+v → %q %q, want %q %q", c.changes, k, title, c.kind, c.title)
+		}
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agenthub/ -run Codex -count=1`
Expected: FAIL build — `undefined: newCodexTranslator`.

- [ ] **Step 3: Áp patch hiện thực**

```diff
diff --git a/internal/agenthub/codex_translate.go b/internal/agenthub/codex_translate.go
new file mode 100644
index 0000000..b79ab60
--- /dev/null
+++ b/internal/agenthub/codex_translate.go
@@ -0,0 +1,554 @@
+package agenthub
+
+import (
+	"encoding/json"
+	"fmt"
+	"path/filepath"
+	"strings"
+	"unicode/utf8"
+
+	"devkit/internal/codexapp"
+)
+
+// codexOutputLimit is how much of a command's output its tool call keeps: the
+// tail, where an error usually is. Codex sends the whole output once, on
+// item/completed — Phase 0 saw no outputDelta on the wire.
+const codexOutputLimit = 64 << 10
+
+// codexUpdate is one ACP session/update payload — the `update` object
+// RuntimeHandlers.OnUpdate takes — and the raw Codex item it came from, kept
+// for FR-8. Meta is nil for deltas: there are thousands, and each is a
+// fragment of an item whose completion carries the whole thing.
+type codexUpdate struct {
+	Update json.RawMessage
+	Meta   json.RawMessage
+}
+
+// codexOpenItem is an item whose tool call is on screen as in_progress. An
+// approval for it reuses its title and content, and an interrupted turn
+// closes it (Phase 0: Codex never completes an item it interrupts).
+type codexOpenItem struct {
+	Title     string
+	Kind      string
+	Content   []ToolCallContent
+	Locations []ToolCallLocation
+	RawInput  json.RawMessage
+}
+
+// codexTranslator turns one app-server session's notifications into ACP
+// updates, so core, store and UI never see Codex's shapes (FR-7). It holds
+// the little state a translation needs, so there is one per runtime. Not safe
+// for concurrent use: codexapp.Conn delivers notifications one at a time.
+type codexTranslator struct {
+	open          map[string]codexOpenItem
+	openOrder     []string
+	messageDeltas map[string]bool
+	thoughtDeltas map[string]bool
+}
+
+func newCodexTranslator() *codexTranslator {
+	return &codexTranslator{
+		open:          map[string]codexOpenItem{},
+		messageDeltas: map[string]bool{},
+		thoughtDeltas: map[string]bool{},
+	}
+}
+
+// codexNotifications is the dispatch table (spec §5.4). Supporting a new
+// notification is one entry here and one method; anything absent is not
+// translated, and the runtime logs it.
+var codexNotifications = map[string]func(*codexTranslator, json.RawMessage) []codexUpdate{
+	codexapp.MethodItemStarted:           (*codexTranslator).itemStarted,
+	codexapp.MethodItemCompleted:         (*codexTranslator).itemCompleted,
+	codexapp.MethodAgentMessageDelta:     (*codexTranslator).agentMessageDelta,
+	codexapp.MethodReasoningSummaryDelta: (*codexTranslator).reasoningDelta,
+	codexapp.MethodTurnPlanUpdated:       (*codexTranslator).planUpdated,
+	codexapp.MethodTokenUsageUpdated:     (*codexTranslator).tokenUsage,
+	codexapp.MethodThreadNameUpdated:     (*codexTranslator).threadName,
+	codexapp.MethodTurnCompleted:         (*codexTranslator).turnCompleted,
+}
+
+// codexTranslates reports whether method has a translation.
+func codexTranslates(method string) bool {
+	_, ok := codexNotifications[method]
+	return ok
+}
+
+// Translate returns the ACP updates for one notification, possibly none.
+func (t *codexTranslator) Translate(method string, params json.RawMessage) []codexUpdate {
+	handle, ok := codexNotifications[method]
+	if !ok {
+		return nil
+	}
+	return handle(t, params)
+}
+
+// openItem returns what the translator knows of an in-progress item.
+func (t *codexTranslator) openItem(id string) (codexOpenItem, bool) {
+	it, ok := t.open[id]
+	return it, ok
+}
+
+// codexToolCallWire is ACP's tool_call / tool_call_update, as
+// ParseSessionUpdate reads it.
+type codexToolCallWire struct {
+	SessionUpdate string             `json:"sessionUpdate"`
+	ToolCallID    string             `json:"toolCallId"`
+	Title         string             `json:"title,omitempty"`
+	Kind          string             `json:"kind,omitempty"`
+	Status        string             `json:"status,omitempty"`
+	Content       []ToolCallContent  `json:"content,omitempty"`
+	Locations     []ToolCallLocation `json:"locations,omitempty"`
+	RawInput      json.RawMessage    `json:"rawInput,omitempty"`
+	RawOutput     json.RawMessage    `json:"rawOutput,omitempty"`
+}
+
+func marshalUpdate(v any) json.RawMessage {
+	b, _ := json.Marshal(v)
+	return b
+}
+
+func codexTextChunk(kind UpdateKind, text string) json.RawMessage {
+	return marshalUpdate(map[string]any{
+		"sessionUpdate": string(kind),
+		"content":       ContentBlock{Type: "text", Text: text},
+	})
+}
+
+func codexTextContent(text string) ToolCallContent {
+	return ToolCallContent{Type: "content", Content: &ContentBlock{Type: "text", Text: text}}
+}
+
+func decodeCodexItem(params json.RawMessage) (json.RawMessage, codexapp.ItemHeader, bool) {
+	var n codexapp.ItemNotification
+	if json.Unmarshal(params, &n) != nil || len(n.Item) == 0 {
+		return nil, codexapp.ItemHeader{}, false
+	}
+	var h codexapp.ItemHeader
+	if json.Unmarshal(n.Item, &h) != nil || h.ID == "" {
+		return nil, codexapp.ItemHeader{}, false
+	}
+	return n.Item, h, true
+}
+
+func (t *codexTranslator) itemStarted(params json.RawMessage) []codexUpdate {
+	item, h, ok := decodeCodexItem(params)
+	if !ok {
+		return nil
+	}
+	it, ok := codexToolItem(h.Type, item)
+	if !ok {
+		// Messages and reasoning stream as deltas; anything else is shown
+		// when it completes.
+		return nil
+	}
+	if _, seen := t.open[h.ID]; !seen {
+		t.openOrder = append(t.openOrder, h.ID)
+	}
+	t.open[h.ID] = it
+	return []codexUpdate{{Update: marshalUpdate(codexToolCallWire{
+		SessionUpdate: string(UpdateToolCall),
+		ToolCallID:    h.ID,
+		Title:         it.Title,
+		Kind:          it.Kind,
+		Status:        "in_progress",
+		Content:       it.Content,
+		Locations:     it.Locations,
+		RawInput:      it.RawInput,
+	})}}
+}
+
+// codexToolItem describes the item types shown as tool calls.
+func codexToolItem(itemType string, item json.RawMessage) (codexOpenItem, bool) {
+	switch itemType {
+	case codexapp.ItemCommandExecution:
+		var c codexapp.CommandExecutionItem
+		if json.Unmarshal(item, &c) != nil {
+			return codexOpenItem{}, false
+		}
+		raw, _ := json.Marshal(map[string]string{"command": c.Command, "cwd": c.Cwd})
+		return codexOpenItem{Title: codexCommandTitle(c.Command, c.CommandActions), Kind: "execute", RawInput: raw}, true
+	case codexapp.ItemFileChange:
+		var f codexapp.FileChangeItem
+		if json.Unmarshal(item, &f) != nil {
+			return codexOpenItem{}, false
+		}
+		return codexOpenItem{
+			Title:     codexFileChangeTitle(f.Changes),
+			Kind:      codexFileChangeKind(f.Changes),
+			Content:   codexFileChangeContent(f.Changes),
+			Locations: codexFileChangeLocations(f.Changes),
+		}, true
+	case codexapp.ItemMcpToolCall:
+		var m codexapp.McpToolCallItem
+		if json.Unmarshal(item, &m) != nil {
+			return codexOpenItem{}, false
+		}
+		return codexOpenItem{Title: m.Server + "." + m.Tool, Kind: "other", RawInput: m.Arguments}, true
+	case codexapp.ItemWebSearch:
+		var w codexapp.WebSearchItem
+		if json.Unmarshal(item, &w) != nil {
+			return codexOpenItem{}, false
+		}
+		title := "Web search"
+		if w.Query != "" {
+			title += ": " + w.Query
+		}
+		raw, _ := json.Marshal(map[string]string{"query": w.Query})
+		return codexOpenItem{Title: title, Kind: "fetch", RawInput: raw}, true
+	}
+	return codexOpenItem{}, false
+}
+
+func (t *codexTranslator) itemCompleted(params json.RawMessage) []codexUpdate {
+	item, h, ok := decodeCodexItem(params)
+	if !ok {
+		return nil
+	}
+	meta := append(json.RawMessage(nil), item...)
+
+	switch h.Type {
+	case codexapp.ItemAgentMessage:
+		streamed := t.messageDeltas[h.ID]
+		delete(t.messageDeltas, h.ID)
+		var m codexapp.AgentMessageItem
+		if streamed || json.Unmarshal(item, &m) != nil || m.Text == "" {
+			return nil
+		}
+		return []codexUpdate{{Update: codexTextChunk(UpdateAgentMessageChunk, m.Text), Meta: meta}}
+	case codexapp.ItemReasoning:
+		streamed := t.thoughtDeltas[h.ID]
+		delete(t.thoughtDeltas, h.ID)
+		var r codexapp.ReasoningItem
+		if streamed || json.Unmarshal(item, &r) != nil {
+			return nil
+		}
+		text := strings.Join(r.Summary, "\n\n")
+		if text == "" {
+			return nil
+		}
+		return []codexUpdate{{Update: codexTextChunk(UpdateAgentThoughtChunk, text), Meta: meta}}
+	case codexapp.ItemUserMessage, codexapp.ItemHookPrompt:
+		// DevKit records the user's own message; hooks are disabled.
+		return nil
+	}
+
+	status, content, rawOutput, known := codexToolResult(h.Type, item)
+	if !known {
+		// A type this build does not translate is shown, not dropped (FR-8).
+		return []codexUpdate{{Update: marshalUpdate(codexToolCallWire{
+			SessionUpdate: string(UpdateToolCall),
+			ToolCallID:    h.ID,
+			Title:         h.Type,
+			Kind:          "other",
+			Status:        "completed",
+			RawOutput:     meta,
+		}), Meta: meta}}
+	}
+	var out []codexUpdate
+	if _, isOpen := t.open[h.ID]; !isOpen {
+		// Completed without a start we saw: show the call before closing it.
+		out = t.itemStarted(params)
+	}
+	t.forget(h.ID)
+	return append(out, codexUpdate{Update: marshalUpdate(codexToolCallWire{
+		SessionUpdate: string(UpdateToolCallUpdate),
+		ToolCallID:    h.ID,
+		Status:        status,
+		Content:       content,
+		RawOutput:     rawOutput,
+	}), Meta: meta})
+}
+
+// codexToolResult is how a completed tool item ends: its ACP status, content
+// to show (nil keeps what the start showed) and raw output.
+func codexToolResult(itemType string, item json.RawMessage) (status string, content []ToolCallContent, rawOutput json.RawMessage, known bool) {
+	switch itemType {
+	case codexapp.ItemCommandExecution:
+		var c codexapp.CommandExecutionItem
+		if json.Unmarshal(item, &c) != nil {
+			return "failed", nil, nil, true
+		}
+		status = "completed"
+		if c.Status == codexapp.ItemFailed || c.Status == codexapp.ItemDeclined || (c.ExitCode != nil && *c.ExitCode != 0) {
+			status = "failed"
+		}
+		output := ""
+		if c.AggregatedOutput != nil {
+			output = codexTail(*c.AggregatedOutput, codexOutputLimit)
+		}
+		if output == "" && c.Status == codexapp.ItemDeclined {
+			output = "Declined."
+		}
+		if output != "" {
+			content = []ToolCallContent{codexTextContent(output)}
+		}
+		rawOutput, _ = json.Marshal(struct {
+			ExitCode   *int   `json:"exitCode"`
+			DurationMs *int64 `json:"durationMs"`
+		}{c.ExitCode, c.DurationMs})
+		return status, content, rawOutput, true
+	case codexapp.ItemFileChange:
+		var f codexapp.FileChangeItem
+		if json.Unmarshal(item, &f) != nil || f.Status == codexapp.ItemFailed || f.Status == codexapp.ItemDeclined {
+			return "failed", nil, nil, true
+		}
+		return "completed", nil, nil, true
+	case codexapp.ItemMcpToolCall:
+		var m codexapp.McpToolCallItem
+		if json.Unmarshal(item, &m) != nil {
+			return "failed", nil, nil, true
+		}
+		if m.Status == codexapp.ItemFailed || m.Error != nil {
+			msg := "failed"
+			if m.Error != nil {
+				msg = m.Error.Message
+			}
+			rawOutput, _ = json.Marshal(map[string]string{"error": msg})
+			return "failed", nil, rawOutput, true
+		}
+		return "completed", nil, m.Result, true
+	case codexapp.ItemWebSearch:
+		return "completed", nil, nil, true
+	}
+	return "", nil, nil, false
+}
+
+func (t *codexTranslator) forget(id string) {
+	delete(t.open, id)
+	for i, open := range t.openOrder {
+		if open == id {
+			t.openOrder = append(t.openOrder[:i], t.openOrder[i+1:]...)
+			break
+		}
+	}
+}
+
+func (t *codexTranslator) agentMessageDelta(params json.RawMessage) []codexUpdate {
+	var d codexapp.DeltaNotification
+	if json.Unmarshal(params, &d) != nil || d.Delta == "" {
+		return nil
+	}
+	t.messageDeltas[d.ItemID] = true
+	return []codexUpdate{{Update: codexTextChunk(UpdateAgentMessageChunk, d.Delta)}}
+}
+
+func (t *codexTranslator) reasoningDelta(params json.RawMessage) []codexUpdate {
+	var d codexapp.DeltaNotification
+	if json.Unmarshal(params, &d) != nil || d.Delta == "" {
+		return nil
+	}
+	t.thoughtDeltas[d.ItemID] = true
+	return []codexUpdate{{Update: codexTextChunk(UpdateAgentThoughtChunk, d.Delta)}}
+}
+
+func (t *codexTranslator) planUpdated(params json.RawMessage) []codexUpdate {
+	var p codexapp.TurnPlanUpdatedNotification
+	if json.Unmarshal(params, &p) != nil {
+		return nil
+	}
+	entries := make([]PlanEntry, 0, len(p.Plan))
+	for _, step := range p.Plan {
+		status := "pending"
+		switch step.Status {
+		case "inProgress":
+			status = "in_progress"
+		case "completed":
+			status = "completed"
+		}
+		entries = append(entries, PlanEntry{Content: step.Step, Status: status, Priority: "medium"})
+	}
+	return []codexUpdate{{Update: marshalUpdate(map[string]any{
+		"sessionUpdate": string(UpdatePlan),
+		"entries":       entries,
+	})}}
+}
+
+// tokenUsage feeds the composer's context meter: `used` is the last request's
+// tokens against the model's window. Codex reports no cost (subscription).
+func (t *codexTranslator) tokenUsage(params json.RawMessage) []codexUpdate {
+	var n codexapp.TokenUsageUpdatedNotification
+	if json.Unmarshal(params, &n) != nil {
+		return nil
+	}
+	var size int64
+	if n.TokenUsage.ModelContextWindow != nil {
+		size = *n.TokenUsage.ModelContextWindow
+	}
+	return []codexUpdate{{Update: marshalUpdate(map[string]any{
+		"sessionUpdate": string(UpdateUsage),
+		"used":          n.TokenUsage.Last.TotalTokens,
+		"size":          size,
+	})}}
+}
+
+func (t *codexTranslator) threadName(params json.RawMessage) []codexUpdate {
+	var n codexapp.ThreadNameUpdatedNotification
+	if json.Unmarshal(params, &n) != nil || strings.TrimSpace(n.ThreadName) == "" {
+		return nil
+	}
+	return []codexUpdate{{Update: marshalUpdate(map[string]any{
+		"sessionUpdate": string(UpdateSessionInfo),
+		"title":         n.ThreadName,
+	})}}
+}
+
+// turnCompleted closes every tool call still open. An interrupted turn leaves
+// its running command without an item/completed (Phase 0), and a card left
+// in_progress would spin forever.
+func (t *codexTranslator) turnCompleted(params json.RawMessage) []codexUpdate {
+	var n codexapp.TurnCompletedNotification
+	_ = json.Unmarshal(params, &n)
+	reason := "Ended without a result."
+	switch n.Turn.Status {
+	case codexapp.TurnInterrupted:
+		reason = "Cancelled."
+	case codexapp.TurnFailed:
+		reason = "The turn failed."
+	}
+	var out []codexUpdate
+	for _, id := range t.openOrder {
+		out = append(out, codexUpdate{Update: marshalUpdate(codexToolCallWire{
+			SessionUpdate: string(UpdateToolCallUpdate),
+			ToolCallID:    id,
+			Status:        "failed",
+			Content:       []ToolCallContent{codexTextContent(reason)},
+		})})
+	}
+	t.open = map[string]codexOpenItem{}
+	t.openOrder = nil
+	t.messageDeltas = map[string]bool{}
+	t.thoughtDeltas = map[string]bool{}
+	return out
+}
+
+// codexCommandTitle is the command as the model wrote it — Codex wraps it in
+// the shell invocation (`/usr/bin/zsh -lc "…"`), which is noise on a card.
+func codexCommandTitle(command string, actions []codexapp.CommandAction) string {
+	for _, a := range actions {
+		if strings.TrimSpace(a.Command) != "" {
+			return a.Command
+		}
+	}
+	return command
+}
+
+func codexFileChangeKind(changes []codexapp.FileUpdateChange) string {
+	allDeletes := len(changes) > 0
+	for _, c := range changes {
+		if c.Kind.MovePath != nil && *c.Kind.MovePath != "" {
+			return "move"
+		}
+		if c.Kind.Type != "delete" {
+			allDeletes = false
+		}
+	}
+	if allDeletes {
+		return "delete"
+	}
+	return "edit"
+}
+
+func codexFileChangeTitle(changes []codexapp.FileUpdateChange) string {
+	if len(changes) != 1 {
+		return fmt.Sprintf("Edit %d files", len(changes))
+	}
+	c := changes[0]
+	verb := "Edit"
+	switch {
+	case c.Kind.MovePath != nil && *c.Kind.MovePath != "":
+		verb = "Move"
+	case c.Kind.Type == "add":
+		verb = "Create"
+	case c.Kind.Type == "delete":
+		verb = "Delete"
+	}
+	return verb + " " + filepath.Base(c.Path)
+}
+
+func codexFileChangeLocations(changes []codexapp.FileUpdateChange) []ToolCallLocation {
+	out := make([]ToolCallLocation, 0, len(changes))
+	for _, c := range changes {
+		path := c.Path
+		if c.Kind.MovePath != nil && *c.Kind.MovePath != "" {
+			path = *c.Kind.MovePath
+		}
+		out = append(out, ToolCallLocation{Path: path})
+	}
+	return out
+}
+
+// codexFileChangeContent builds ACP diff content from Codex's own diffs —
+// never from the file on disk: the adapter runs outside the jail and the path
+// is the agent's, so reading it could show the transcript a file the agent
+// could not read (spec §5.2).
+func codexFileChangeContent(changes []codexapp.FileUpdateChange) []ToolCallContent {
+	out := make([]ToolCallContent, 0, len(changes))
+	for _, c := range changes {
+		switch c.Kind.Type {
+		case "add":
+			// The diff is the whole new file (measured: no @@). No oldText
+			// means a new file.
+			out = append(out, ToolCallContent{Type: "diff", Path: c.Path, NewText: c.Diff})
+		case "delete":
+			before := c.Diff
+			out = append(out, ToolCallContent{Type: "diff", Path: c.Path, OldText: &before})
+		default:
+			before, after := codexHunkTexts(c.Diff)
+			path := c.Path
+			if c.Kind.MovePath != nil && *c.Kind.MovePath != "" {
+				path = *c.Kind.MovePath
+			}
+			out = append(out, ToolCallContent{Type: "diff", Path: path, OldText: &before, NewText: after})
+		}
+	}
+	return out
+}
+
+// codexHunkTexts splits a unified diff's hunks into the before and after text
+// of the lines they cover, "…" between hunks. Only the changed region: the
+// rest of the file is exactly what DevKit declines to read.
+func codexHunkTexts(diff string) (before, after string) {
+	var old, neu []string
+	hunks := 0
+	for _, line := range strings.Split(strings.TrimSuffix(diff, "\n"), "\n") {
+		switch {
+		case strings.HasPrefix(line, "@@"):
+			if hunks > 0 {
+				old, neu = append(old, "…"), append(neu, "…")
+			}
+			hunks++
+		case hunks == 0:
+			// File headers (--- / +++) before the first hunk.
+		case strings.HasPrefix(line, "+"):
+			neu = append(neu, line[1:])
+		case strings.HasPrefix(line, "-"):
+			old = append(old, line[1:])
+		case strings.HasPrefix(line, " "):
+			old, neu = append(old, line[1:]), append(neu, line[1:])
+		case line == "":
+			old, neu = append(old, ""), append(neu, "")
+		}
+		// "\ No newline at end of file" and anything else: skipped.
+	}
+	return codexJoinLines(old), codexJoinLines(neu)
+}
+
+func codexJoinLines(lines []string) string {
+	if len(lines) == 0 {
+		return ""
+	}
+	return strings.Join(lines, "\n") + "\n"
+}
+
+// codexTail keeps the last limit bytes of s on a rune boundary, marked as cut.
+func codexTail(s string, limit int) string {
+	if len(s) <= limit {
+		return s
+	}
+	s = s[len(s)-limit:]
+	for len(s) > 0 && !utf8.RuneStart(s[0]) {
+		s = s[1:]
+	}
+	return "…\n" + s
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ -run Codex -count=1 -v && go test ./internal/agenthub/ ./internal/arch/ -count=1`
Expected: PASS. Rồi `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/codex_translate.go internal/agenthub/codex_translate_test.go
git commit -m "feat(agenthub): translate codex app-server notifications to ACP

A dispatch table per method (spec §5.4). Agent text streams from
deltas, or is sent whole when none came; tool calls for commands, file
changes, MCP and web search; diffs built from Codex's own hunks, never
from disk; open tool calls closed when a turn ends interrupted. Golden
tests replay the Phase 0 capture through ParseSessionUpdate.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 4: Ánh xạ approval (`codex_approvals.go`)

**Vì sao:** Ba loại server request của Codex phải thành thẻ duyệt sẵn có, và câu trả lời phải thành đúng quyết định Codex chấp nhận. Nút dựng từ `availableDecisions` của request; Deny = `decline` nếu có, không thì `cancel`; trả lời hỏng/huỷ → fail closed. `item/permissions/requestApproval` không hỏi ai: cấp rỗng + một dòng transcript. Request khác bị từ chối ngay.

**Files:** Create `internal/agenthub/codex_approvals.go`, `internal/agenthub/codex_approvals_test.go`

Mỗi bước "áp patch": lưu **nguyên văn** khối `diff` vào một file tạm (ví dụ `/tmp/t4-test.patch`, dùng công cụ ghi file — đừng gõ lại), rồi chạy `git apply --check <file> && git apply <file>`. Patch đã được sinh từ code đã biên dịch và chạy test xanh (kể cả `-race`) trên đúng commit gốc của nhánh; nó phải áp sạch. Nếu `git apply --check` báo lỗi: **dừng**, báo NEEDS_CONTEXT kèm output — không sửa tay.

- [ ] **Step 1: Áp patch test**

```diff
diff --git a/internal/agenthub/codex_approvals_test.go b/internal/agenthub/codex_approvals_test.go
new file mode 100644
index 0000000..0b68323
--- /dev/null
+++ b/internal/agenthub/codex_approvals_test.go
@@ -0,0 +1,196 @@
+package agenthub
+
+import (
+	"encoding/json"
+	"os"
+	"strings"
+	"testing"
+
+	"devkit/internal/acp"
+	"devkit/internal/codexapp"
+)
+
+func capturedRequest(t *testing.T, method string) json.RawMessage {
+	t.Helper()
+	for _, l := range loadCodexCaptureAll(t) {
+		if l.Msg.Method == method && l.Msg.IsRequest() {
+			return l.Msg.Params
+		}
+	}
+	t.Fatalf("no %s in the capture", method)
+	return nil
+}
+
+func loadCodexCaptureAll(t *testing.T) []codexCaptureLine {
+	t.Helper()
+	raw, err := os.ReadFile("testdata/codex_session_capture.jsonl")
+	if err != nil {
+		t.Fatal(err)
+	}
+	var out []codexCaptureLine
+	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
+		var l codexCaptureLine
+		if err := json.Unmarshal([]byte(line), &l); err != nil {
+			t.Fatal(err)
+		}
+		out = append(out, l)
+	}
+	return out
+}
+
+type permissionParams struct {
+	acp.RequestPermissionParams
+	options  []codexPermissionOption
+	toolCall codexToolCallWire
+}
+
+func decodePermission(t *testing.T, raw json.RawMessage) permissionParams {
+	t.Helper()
+	var p permissionParams
+	if err := json.Unmarshal(raw, &p.RequestPermissionParams); err != nil {
+		t.Fatal(err)
+	}
+	if err := json.Unmarshal(p.Options, &p.options); err != nil {
+		t.Fatal(err)
+	}
+	if err := json.Unmarshal(p.ToolCall, &p.toolCall); err != nil {
+		t.Fatal(err)
+	}
+	return p
+}
+
+func selected(optionID string) json.RawMessage {
+	return json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"` + optionID + `"}}`)
+}
+
+func decision(t *testing.T, body json.RawMessage) string {
+	t.Helper()
+	var r codexapp.ApprovalResponse
+	if err := json.Unmarshal(body, &r); err != nil {
+		t.Fatal(err)
+	}
+	return r.Decision
+}
+
+func TestCodexCommandApprovalFollowsAvailableDecisions(t *testing.T) {
+	// The captured request (untrusted) offers accept, an execpolicy
+	// amendment and cancel only.
+	tr := newCodexTranslator()
+	a, ok := tr.approvalFor("sess-1", codexapp.MethodCommandApproval, capturedRequest(t, codexapp.MethodCommandApproval))
+	if !ok {
+		t.Fatal("command approval not recognised")
+	}
+	p := decodePermission(t, a.Params)
+	if p.SessionID != "sess-1" || p.toolCall.Kind != "execute" || p.toolCall.ToolCallID == "" {
+		t.Fatalf("params = %+v", p)
+	}
+	if strings.HasPrefix(p.toolCall.Title, "/usr/bin/zsh") {
+		t.Fatalf("title %q carries the shell wrapper", p.toolCall.Title)
+	}
+	var ids []string
+	for _, o := range p.options {
+		ids = append(ids, o.OptionID)
+		if o.Kind != o.OptionID {
+			t.Fatalf("option %+v: kind must equal id for the card to find it", o)
+		}
+	}
+	if strings.Join(ids, ",") != "allow_once,reject_once" {
+		t.Fatalf("options = %v, want allow_once,reject_once (no acceptForSession offered by Codex)", ids)
+	}
+	if strings.Contains(string(a.Params), "Execpolicy") || strings.Contains(string(a.Params), "execpolicy") {
+		t.Fatalf("an execpolicy amendment leaked into the request: %s", a.Params)
+	}
+	for option, want := range map[string]string{
+		codexOptionAllow: codexapp.DecisionAccept,
+		codexOptionDeny:  codexapp.DecisionCancel, // no decline available
+	} {
+		if got := decision(t, a.Response(selected(option))); got != want {
+			t.Fatalf("%s → %q, want %q", option, got, want)
+		}
+	}
+	if got := decision(t, a.Response(json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`))); got != codexapp.DecisionCancel {
+		t.Fatalf("cancelled → %q", got)
+	}
+	if got := decision(t, a.Response(json.RawMessage(`not json`))); got != codexapp.DecisionCancel {
+		t.Fatalf("unreadable → %q, want fail closed", got)
+	}
+	if got := decision(t, a.Response(selected("allow_always"))); got != codexapp.DecisionCancel {
+		t.Fatalf("an option that was never offered → %q, want fail closed", got)
+	}
+}
+
+func TestCodexCommandApprovalWithEveryDecision(t *testing.T) {
+	tr := newCodexTranslator()
+	params, _ := json.Marshal(map[string]any{
+		"threadId": "th", "turnId": "tu", "itemId": "c9", "command": "go test ./...", "cwd": "/p",
+		"availableDecisions": []any{"accept", "acceptForSession", "decline", "cancel"},
+	})
+	a, ok := tr.approvalFor("s", codexapp.MethodCommandApproval, params)
+	if !ok {
+		t.Fatal("not recognised")
+	}
+	if len(decodePermission(t, a.Params).options) != 3 {
+		t.Fatalf("params = %s", a.Params)
+	}
+	for option, want := range map[string]string{
+		codexOptionAllow:        codexapp.DecisionAccept,
+		codexOptionAllowSession: codexapp.DecisionAcceptForSession,
+		codexOptionDeny:         codexapp.DecisionDecline,
+	} {
+		if got := decision(t, a.Response(selected(option))); got != want {
+			t.Fatalf("%s → %q, want %q", option, got, want)
+		}
+	}
+}
+
+func TestCodexFileChangeApprovalShowsTheDiff(t *testing.T) {
+	tr := newCodexTranslator()
+	// The file change starts (and reaches the translator) before Codex asks
+	// about it, as in the capture.
+	for _, m := range loadCodexCapture(t, "main") {
+		if m.Method == codexapp.MethodItemStarted && strings.Contains(string(m.Params), `"fileChange"`) {
+			tr.Translate(m.Method, m.Params)
+		}
+	}
+	a, ok := tr.approvalFor("s", codexapp.MethodFileChangeApproval, capturedRequest(t, codexapp.MethodFileChangeApproval))
+	if !ok {
+		t.Fatal("not recognised")
+	}
+	p := decodePermission(t, a.Params)
+	if p.toolCall.Kind != "edit" || len(p.toolCall.Content) != 2 || p.toolCall.Content[0].Type != "diff" {
+		t.Fatalf("tool call = %+v", p.toolCall)
+	}
+	if len(p.options) != 3 || decision(t, a.Response(selected(codexOptionDeny))) != codexapp.DecisionDecline {
+		t.Fatalf("file change options = %+v", p.options)
+	}
+}
+
+func TestCodexPermissionsRequestGrantsNothing(t *testing.T) {
+	params, _ := json.Marshal(map[string]any{
+		"threadId": "th", "turnId": "tu", "itemId": "p1", "cwd": "/p",
+		"permissions": map[string]any{"network": map[string]any{"enabled": true}},
+	})
+	resp, notice := codexPermissionsRequest(params)
+	if string(resp) != `{"permissions":{},"scope":"turn"}` {
+		t.Fatalf("response = %s", resp)
+	}
+	if len(notice) != 1 {
+		t.Fatalf("notice = %+v", notice)
+	}
+	su := ParseSessionUpdate(notice[0].Update)
+	if su.ToolCall == nil || su.ToolCall.Status != "failed" || !strings.Contains(su.ToolCall.Content[0].Content.Text, "Settings → Agents → Sandbox") {
+		t.Fatalf("notice = %+v", su)
+	}
+}
+
+func TestCodexOtherRequestsAreRefused(t *testing.T) {
+	tr := newCodexTranslator()
+	for _, m := range []string{"item/tool/requestUserInput", "mcpServer/elicitation/request", "account/chatgptAuthTokens/refresh", "attestation/generate", "applyPatchApproval", "execCommandApproval"} {
+		if _, ok := tr.approvalFor("s", m, json.RawMessage(`{}`)); ok {
+			t.Fatalf("%s treated as an approval", m)
+		}
+		if e := codexUnsupportedRequest(m); e.Code != -32601 || !strings.Contains(e.Message, m) {
+			t.Fatalf("%s → %+v", m, e)
+		}
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agenthub/ -run Codex -count=1`
Expected: FAIL build — `tr.approvalFor undefined` / `undefined: codexPermissionsRequest`.

- [ ] **Step 3: Áp patch hiện thực**

```diff
diff --git a/internal/agenthub/codex_approvals.go b/internal/agenthub/codex_approvals.go
new file mode 100644
index 0000000..30ace59
--- /dev/null
+++ b/internal/agenthub/codex_approvals.go
@@ -0,0 +1,205 @@
+package agenthub
+
+import (
+	"encoding/json"
+	"strings"
+
+	"devkit/internal/codexapp"
+)
+
+// ACP permission option ids. Each doubles as the option's kind, which is what
+// the permission card looks for to draw its buttons.
+const (
+	codexOptionAllow        = "allow_once"
+	codexOptionAllowSession = "allow_always"
+	codexOptionDeny         = "reject_once"
+)
+
+// codexApproval is a Codex approval request put as an ACP
+// session/request_permission, and the way back from the person's answer to
+// the decision Codex expects.
+type codexApproval struct {
+	// Params is the ACP request_permission params for PermissionWaiter.
+	Params json.RawMessage
+	// decide maps an ACP option id to a Codex decision.
+	decide map[string]string
+	// onCancel is the decision for a cancelled, timed-out or unreadable
+	// answer: fail closed.
+	onCancel string
+}
+
+type codexPermissionOption struct {
+	OptionID string `json:"optionId"`
+	Name     string `json:"name"`
+	Kind     string `json:"kind"`
+}
+
+// approvalFor builds the permission request for an approval server request,
+// or ok=false when method is not an approval DevKit puts to a person.
+//
+// The options come from what Codex will accept. Measured under untrusted, a
+// command approval offers accept, an execpolicy amendment and cancel — no
+// decline, no acceptForSession — so a fixed button set would send decisions
+// Codex rejects. acceptWithExecpolicyAmendment is never offered: it writes a
+// permanent rule, and an approval must not.
+func (t *codexTranslator) approvalFor(sessionID, method string, params json.RawMessage) (codexApproval, bool) {
+	switch method {
+	case codexapp.MethodCommandApproval:
+		var p codexapp.CommandApprovalParams
+		if json.Unmarshal(params, &p) != nil || p.ItemID == "" {
+			return codexApproval{}, false
+		}
+		call, known := t.openItem(p.ItemID)
+		if !known {
+			command := ""
+			if p.Command != nil {
+				command = *p.Command
+			}
+			call = codexOpenItem{Title: codexCommandTitle(command, p.CommandActions), Kind: "execute"}
+		}
+		input := map[string]any{}
+		if p.Command != nil {
+			input["command"] = *p.Command
+		}
+		if p.Cwd != nil {
+			input["cwd"] = *p.Cwd
+		}
+		if p.Reason != nil {
+			input["reason"] = *p.Reason
+		}
+		call.RawInput, _ = json.Marshal(input)
+		return codexApprovalRequest(sessionID, p.ItemID, call, codexAvailable(p.AvailableDecisions)), true
+	case codexapp.MethodFileChangeApproval:
+		var p codexapp.FileChangeApprovalParams
+		if json.Unmarshal(params, &p) != nil || p.ItemID == "" {
+			return codexApproval{}, false
+		}
+		call, known := t.openItem(p.ItemID)
+		if !known {
+			call = codexOpenItem{Title: "Apply file changes", Kind: "edit"}
+		}
+		if p.Reason != nil {
+			call.RawInput, _ = json.Marshal(map[string]string{"reason": *p.Reason})
+		}
+		// FileChangeApprovalDecision is all four, always.
+		all := map[string]bool{
+			codexapp.DecisionAccept: true, codexapp.DecisionAcceptForSession: true,
+			codexapp.DecisionDecline: true, codexapp.DecisionCancel: true,
+		}
+		return codexApprovalRequest(sessionID, p.ItemID, call, all), true
+	}
+	return codexApproval{}, false
+}
+
+// codexAvailable reads availableDecisions: plain strings are decisions, and
+// objects (the execpolicy/network amendments) are skipped. Absent means every
+// plain decision.
+func codexAvailable(raw []json.RawMessage) map[string]bool {
+	if raw == nil {
+		return map[string]bool{
+			codexapp.DecisionAccept: true, codexapp.DecisionAcceptForSession: true,
+			codexapp.DecisionDecline: true, codexapp.DecisionCancel: true,
+		}
+	}
+	out := map[string]bool{}
+	for _, d := range raw {
+		var s string
+		if json.Unmarshal(d, &s) == nil {
+			out[s] = true
+		}
+	}
+	return out
+}
+
+func codexApprovalRequest(sessionID, itemID string, call codexOpenItem, available map[string]bool) codexApproval {
+	a := codexApproval{decide: map[string]string{}, onCancel: codexapp.DecisionCancel}
+	if !available[codexapp.DecisionCancel] {
+		a.onCancel = codexapp.DecisionDecline
+	}
+	var options []codexPermissionOption
+	if available[codexapp.DecisionAccept] {
+		options = append(options, codexPermissionOption{codexOptionAllow, "Allow", codexOptionAllow})
+		a.decide[codexOptionAllow] = codexapp.DecisionAccept
+	}
+	if available[codexapp.DecisionAcceptForSession] {
+		options = append(options, codexPermissionOption{codexOptionAllowSession, "Allow for this session", codexOptionAllowSession})
+		a.decide[codexOptionAllowSession] = codexapp.DecisionAcceptForSession
+	}
+	// Deny is decline when Codex takes it (the turn goes on without the
+	// action), else cancel (the turn stops) — there is always a way to say no.
+	deny := codexapp.DecisionDecline
+	if !available[deny] {
+		deny = codexapp.DecisionCancel
+	}
+	options = append(options, codexPermissionOption{codexOptionDeny, "Deny", codexOptionDeny})
+	a.decide[codexOptionDeny] = deny
+
+	a.Params = marshalUpdate(map[string]any{
+		"sessionId": sessionID,
+		"toolCall": codexToolCallWire{
+			ToolCallID: itemID,
+			Title:      call.Title,
+			Kind:       call.Kind,
+			Status:     "pending",
+			Content:    call.Content,
+			Locations:  call.Locations,
+			RawInput:   call.RawInput,
+		},
+		"options": options,
+	})
+	return a
+}
+
+// Response turns the ACP outcome — what the permission gate returned, even
+// alongside an error — into Codex's {"decision": …}.
+func (a codexApproval) Response(outcome json.RawMessage) json.RawMessage {
+	var wrap struct {
+		Outcome struct {
+			Outcome  string `json:"outcome"`
+			OptionID string `json:"optionId"`
+		} `json:"outcome"`
+	}
+	decision := a.onCancel
+	if json.Unmarshal(outcome, &wrap) == nil && strings.EqualFold(wrap.Outcome.Outcome, "selected") {
+		if d, ok := a.decide[wrap.Outcome.OptionID]; ok {
+			decision = d
+		}
+	}
+	return marshalUpdate(codexapp.ApprovalResponse{Decision: decision})
+}
+
+// codexPermissionsRequest answers item/permissions/requestApproval without
+// asking anyone. Under externalSandbox Codex granting itself more access
+// changes nothing — DevKit's jail decides — so an Allow button would be a
+// lie. It grants nothing and leaves a line in the transcript saying where
+// access is actually decided.
+func codexPermissionsRequest(params json.RawMessage) (response json.RawMessage, notice []codexUpdate) {
+	response = marshalUpdate(codexapp.PermissionsApprovalResponse{Scope: "turn"})
+	var p codexapp.PermissionsApprovalParams
+	if json.Unmarshal(params, &p) != nil {
+		return response, nil
+	}
+	asked := strings.TrimSpace(string(p.Permissions))
+	if asked == "" {
+		asked = "more access"
+	}
+	text := "Codex asked for " + asked + ". Access is decided by DevKit's sandbox — Settings → Agents → Sandbox."
+	return response, []codexUpdate{{Update: marshalUpdate(codexToolCallWire{
+		SessionUpdate: string(UpdateToolCall),
+		ToolCallID:    "permissions-" + p.ItemID,
+		Title:         "Codex asked for more access",
+		Kind:          "other",
+		Status:        "failed",
+		Content:       []ToolCallContent{codexTextContent(text)},
+		RawInput:      p.Permissions,
+	})}}
+}
+
+// codexUnsupportedRequest refuses a server request DevKit does not handle, at
+// once, so the turn is not left waiting: user-input questions, MCP
+// elicitation, token refresh, attestation, the v1 approval methods, and
+// anything new. Each becomes supported by one case in the runtime's request
+// dispatch (spec §5.4).
+func codexUnsupportedRequest(method string) *codexapp.RPCError {
+	return &codexapp.RPCError{Code: -32601, Message: "DevKit does not support " + method}
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ -run Codex -count=1 -v && go test ./internal/agenthub/ ./internal/arch/ -count=1 -race`
Expected: PASS. Rồi `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/codex_approvals.go internal/agenthub/codex_approvals_test.go
git commit -m "feat(agenthub): map codex approvals to ACP permission requests

Buttons follow the request's availableDecisions (measured: a shell
command under untrusted offers no decline and no acceptForSession).
Deny is decline where Codex takes it, else cancel; anything unreadable
fails closed. Execpolicy amendments are never offered. Permission
requests grant nothing and say where access is decided.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

## Kết thúc

- [ ] `go test ./internal/sandbox/ ./internal/codexapp/ ./internal/agenthub/ ./internal/bridge/ ./internal/arch/ -count=1 -race`, `go vet -tags server ./...`, `gofmt -l internal main.go`, `go build -tags server -o /dev/null .` — tất cả sạch.
- [ ] Review tổng (opus) cho khoảng commit của kế hoạch này.
- [ ] `.sync/export.sh "codex plan 2b: deny-write sandbox, translator, approvals"` — báo người dùng đường dẫn zip.
- [ ] Viết Kế hoạch 2c (runtime `CodexAdapter`).
