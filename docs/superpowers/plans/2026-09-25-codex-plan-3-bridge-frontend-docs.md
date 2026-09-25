# Codex trong Agent Hub — Kế hoạch 3: bridge, frontend, tài liệu

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Codex dùng được từ UI như Claude: đăng nhập trong app (ChatGPT qua trình duyệt, device code, hoặc API key từ Accounts), chọn Codex trong bản nháp session, card Codex ở Settings → Agents, hộp xác nhận Full auto nói rõ jail còn mở gì, thẻ duyệt tool MCP hiện đúng; lỗi nói đúng vendor; tài liệu theo kịp.

**Architecture:** Backend thêm ba mảnh nhỏ quanh `InAppSignIn` đã có từ 2c: `agenthub.VendorError` (lỗi nêu vendor), năm method bridge `AgentHub*SignIn*` chỉ cho UI (service từ chối caller agent/terminal MCP), và `app.CodexAPIKey` nối `CodexOptions.APIKey` với `accounts` qua id lưu trong `preferences`. Frontend thêm **bảng mô tả vendor** (`modules/agenthub/vendors.ts`) mà mọi chỗ từng so với `"claude"` đọc, panel đăng nhập Codex (`CodexSignIn`), phần tài khoản Codex trong Settings (`CodexAccountSection`), và hai chỉnh ở composer/thẻ duyệt.

**Tech Stack:** Go 1.27 (bridge Wails v3.0.0-beta.25, capability gateway, `accounts`, `preferences`), React/TypeScript (bun test + `renderToString`), `wails3 generate bindings -clean=true -ts -i`.

**Spec:** `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md` §6.2–6.4, §7.1–7.2, §9 Giai đoạn 5–7. **Trước đó:** Kế hoạch 1, 2a, 2b, 2c (PR syurodev/dev-kit#148, đã merge vào `main`). **Sau đó:** smoke thủ công trên app desktop (checklist ở Kết thúc).

**Cách plan này được viết:** toàn bộ code đã được viết và chạy trong một git worktree tạm tách từ `main` `05ef2f1` (merge của #148), mỗi task một commit nháp. Trên Linux (container cloud, bwrap 0.9.0): `go test ./internal/{sandbox,codexapp,agentruntime,agenthub,bridge,mcp,arch,app,accounts,preferences}/ -race` xanh, `go test ./internal/... ./cmd/...` xanh, `go vet -tags server ./...`, `gofmt`, `go build -tags server` sạch; `cd frontend && bun run build && bun test` xanh. Mỗi task nhúng **patch test** và **patch hiện thực** lấy thẳng từ đó (`git diff --full-index --no-renames`). Trước khi giao, mọi patch được trích lại **từ chính file này**, áp lên worktree sạch tách từ `05ef2f1`, và `git write-tree` trùng với cây nháp cuối.

**Quy ước chung**

- Chạy từ gốc repo, trên nhánh làm việc của kế hoạch này (tách từ `main` sau khi #148 merge). **Không push.**
- Commit kết thúc bằng các dòng attribution mà session điều phối đưa cho (ở dưới ghi `<attribution>`).
- `frontend/dist` phải tồn tại cho mọi lệnh Go chạm `internal/arch` hay gói gốc (`main.go` embed nó): `cd frontend && bun run build` một lần trước.
- **Áp patch — không gõ lại, không sửa tay.** Mỗi khối `diff` có một marker `<!-- patch tN-test -->` / `<!-- patch tN-impl -->` ngay trên. Trích nguyên văn bằng:

```bash
P=docs/superpowers/plans/2026-09-25-codex-plan-3-bridge-frontend-docs.md
extract() { awk -v m="<!-- patch $1 -->" '$0==m{f=1;next} f&&/^```diff$/{p=1;next} p&&/^```$/{exit} p' "$P" > "/tmp/$1.patch"; }
extract t1-test && git apply --check /tmp/t1-test.patch && git apply /tmp/t1-test.patch
```

  Nếu `git apply --check` báo lỗi: **dừng**, báo NEEDS_CONTEXT kèm output.

## Điểm mới so với spec (Task 10 ghi vào spec §6.4)

- **"Chỉ UI" có cả phía service**: `Service.inAppSignIn` từ chối caller mà gateway đã đóng dấu là agent hoặc CLI trên socket MCP chung. Không có tool là một lớp; lớp này để một cửa mở nhầm về sau không dẫn tới đâu.
- **Lỗi nêu vendor bằng kiểu, không bằng chuỗi**: `agenthub.VendorError` gói `ErrAgentNotInstalled`/`ErrAgentAuthRequired` tại adapter; `Error()`/`errors.Is` nhìn xuyên qua nên không có gì đang khớp sentinel bị đổi. Sentinel trần (không vendor) nhận câu trung tính thay vì "Claude".
- **API key**: `Choose` từ chối account mà spawn không resolve được, kể cả account giới hạn env (spawn không có env để đưa). Accounts không cho xoá account đang được chọn (`UsedBy`).
- **Dòng `external` "Codex (ChatGPT)"** sống theo sign-in: ghi khi `account/login/completed` báo thành công, xoá khi Sign out. Nó là vault item nên sync — đúng như package doc của `accounts` mô tả (ghi chú "tôi đăng nhập gì", không giữ secret).
- **Ratchet ranh giới module** (`frontend/src/moduleBoundaryRatchet.test.ts`) chặn cạnh settings→agenthub mới, nên phần tài khoản Codex sống trong `modules/agenthub` như `ClaudeSignInSection`.

### Task 1: Lỗi `not installed` / `auth required` nói đúng vendor

**Model:** haiku

**Vì sao:** `internal/bridge/errors.go` luôn nói "Claude isn't signed in… ANTHROPIC_API_KEY" và "Claude Agent is not installed", kể cả khi session là Codex. Bridge chỉ thấy error, không biết vendor. `agenthub.VendorError{Agent, Err}` gói hai sentinel ở chỗ adapter tạo ra chúng (Claude 4 chỗ, Codex 6 chỗ); `Error()` và `errors.Is` nhìn xuyên qua nên chuỗi lỗi và mọi kiểm tra cũ không đổi. `ErrorVendor(err)` đọc lại tên; bridge viết thông báo theo vendor, vendor lạ nhận câu không đoán.

**Files:** Create `internal/agenthub/vendor_error_test.go`; Modify `internal/agenthub/adapter_claude.go`, `internal/agenthub/adapter_codex.go`, `internal/agenthub/errors.go`, `internal/bridge/errors.go`, `internal/bridge/errors_agenthub_test.go`

- [ ] **Step 1: Áp patch test** (`extract t1-test`)

<!-- patch t1-test -->
```diff
diff --git a/internal/agenthub/vendor_error_test.go b/internal/agenthub/vendor_error_test.go
new file mode 100644
index 0000000000000000000000000000000000000000..5343f93ec7f4c879e7abfe19b2c96f210e8c1c86
--- /dev/null
+++ b/internal/agenthub/vendor_error_test.go
@@ -0,0 +1,73 @@
+package agenthub
+
+import (
+	"context"
+	"errors"
+	"fmt"
+	"testing"
+
+	"devkit/internal/agentruntime"
+	"devkit/internal/codexapp"
+)
+
+// The two errors a person fixes in Settings — not installed, not signed in —
+// say which agent they are about, and stay the same errors underneath.
+func TestVendorErrorsNameTheirAgent(t *testing.T) {
+	notInstalled := func() (agentruntime.Resolved, error) {
+		return agentruntime.Resolved{}, agentruntime.ErrNotInstalled
+	}
+	_, _, claudeNone := NewClaudeAdapter(ClaudeOptions{}).buildSandbox(OpenSpec{Cwd: t.TempDir()})
+	_, _, claudeRuntime := NewClaudeAdapter(ClaudeOptions{Runtime: notInstalled}).buildSandbox(OpenSpec{Cwd: t.TempDir()})
+	_, codexNone := NewCodexAdapter(CodexOptions{}).resolveCommand()
+	_, codexRuntime := NewCodexAdapter(CodexOptions{Runtime: notInstalled}).resolveCommand()
+	_, codexKey := NewCodexAdapter(CodexOptions{APIKey: func(context.Context) (string, error) {
+		return "", errors.New("vault locked")
+	}}).apiKey(context.Background())
+	claudeSignedOut := NewClaudeAdapter(ClaudeOptions{Command: fakeAgentBinary(t), Runner: scriptedAgentRunner{replies: []string{
+		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}`,
+		`{"jsonrpc":"2.0","id":2,"error":{"code":-32000,"message":"Authentication required"}}`,
+	}}})
+	_, claudeAuth := claudeSignedOut.Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
+	f := newCodexFixture(t, "signedout")
+	_, codexAuth := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
+	codexTurn := codexTurnError(&codexapp.TurnError{Message: "token expired", CodexErrorInfo: []byte(`"unauthorized"`)})
+
+	cases := []struct {
+		name     string
+		err      error
+		sentinel error
+		agent    AgentType
+	}{
+		{"claude, nothing to run", claudeNone, ErrAgentNotInstalled, AgentTypeClaude},
+		{"claude, runtime not installed", claudeRuntime, ErrAgentNotInstalled, AgentTypeClaude},
+		{"claude, signed out", claudeAuth, ErrAgentAuthRequired, AgentTypeClaude},
+		{"codex, nothing to run", codexNone, ErrAgentNotInstalled, AgentTypeCodex},
+		{"codex, runtime not installed", codexRuntime, ErrAgentNotInstalled, AgentTypeCodex},
+		{"codex, api key unreadable", codexKey, ErrAgentAuthRequired, AgentTypeCodex},
+		{"codex, signed out", codexAuth, ErrAgentAuthRequired, AgentTypeCodex},
+		{"codex, unauthorized turn", codexTurn, ErrAgentAuthRequired, AgentTypeCodex},
+	}
+	for _, tc := range cases {
+		if !errors.Is(tc.err, tc.sentinel) {
+			t.Errorf("%s: %v is not %v", tc.name, tc.err, tc.sentinel)
+		}
+		if got := ErrorVendor(tc.err); got != tc.agent {
+			t.Errorf("%s: ErrorVendor = %q, want %q", tc.name, got, tc.agent)
+		}
+		// Wrapped again on the way up, it still says whose it is.
+		if got := ErrorVendor(fmt.Errorf("invoke: %w", tc.err)); got != tc.agent {
+			t.Errorf("%s: wrapped ErrorVendor = %q, want %q", tc.name, got, tc.agent)
+		}
+	}
+	if got := ErrorVendor(ErrAgentAuthRequired); got != "" {
+		t.Errorf("a bare sentinel names no agent, got %q", got)
+	}
+	if vendorError(AgentTypeCodex, nil) != nil {
+		t.Error("vendorError(nil) must stay nil")
+	}
+	// The message is the error's own: naming the vendor adds nothing to it.
+	plain := fmt.Errorf("%w: Codex is not signed in", ErrAgentAuthRequired)
+	if got := vendorError(AgentTypeCodex, plain).Error(); got != plain.Error() {
+		t.Errorf("Error() = %q, want %q", got, plain.Error())
+	}
+}
diff --git a/internal/bridge/errors_agenthub_test.go b/internal/bridge/errors_agenthub_test.go
index 7578d1987a1b0335ba8645fddcd5dcea4cc3247c..61fa1abcf71c679e4be0ea1b531f3101a215aa95 100644
--- a/internal/bridge/errors_agenthub_test.go
+++ b/internal/bridge/errors_agenthub_test.go
@@ -31,9 +31,11 @@ func TestPublicErrorMapsAgentHubSentinels(t *testing.T) {
 		{agenthub.ErrPermissionDenied, "permission_denied: Permission denied."},
 		{agenthub.ErrCancelled, "operation_cancelled: Operation cancelled."},
 		{agenthub.ErrProcessFailed, "connection_failed: Agent process failed."},
-		{agenthub.ErrAgentAuthRequired, "agent_auth_required: Claude isn't signed in, or the sign-in expired. Sign in to Claude, or set ANTHROPIC_API_KEY."},
-		{fmt.Errorf("%w: session/new: acp: rpc error -32000: Authentication required", agenthub.ErrAgentAuthRequired),
+		{agenthub.ErrAgentAuthRequired, "agent_auth_required: The agent isn't signed in, or the sign-in expired."},
+		{&agenthub.VendorError{Agent: agenthub.AgentTypeClaude, Err: fmt.Errorf("%w: session/new: acp: rpc error -32000: Authentication required", agenthub.ErrAgentAuthRequired)},
 			"agent_auth_required: Claude isn't signed in, or the sign-in expired. Sign in to Claude, or set ANTHROPIC_API_KEY."},
+		{fmt.Errorf("invoke: %w", &agenthub.VendorError{Agent: agenthub.AgentTypeCodex, Err: fmt.Errorf("%w: Codex is not signed in", agenthub.ErrAgentAuthRequired)}),
+			"agent_auth_required: Codex isn't signed in, or the sign-in expired. Sign in under Settings → Agents → Codex, or check the API key chosen there."},
 		{agenthub.ErrAuthMethodUnknown, "validation_failed: That sign-in method isn't offered by this agent."},
 		{agenthub.ErrSandboxUnavailable, "connection_failed: Agent sandbox is unavailable."},
 		{agenthub.ErrUnavailableInServerMode, "validation_failed: Agent Hub needs the desktop app. In server mode an agent could answer its own permission prompts."},
@@ -48,7 +50,8 @@ func TestPublicErrorMapsAgentHubSentinels(t *testing.T) {
 }
 
 func TestPublicErrorMapsAgentRuntimeErrors(t *testing.T) {
-	notInstalledSession := fmt.Errorf("%w: %w", agenthub.ErrAgentNotInstalled, agentruntime.ErrNotInstalled)
+	notInstalledSession := &agenthub.VendorError{Agent: agenthub.AgentTypeClaude, Err: fmt.Errorf("%w: %w", agenthub.ErrAgentNotInstalled, agentruntime.ErrNotInstalled)}
+	codexNotInstalled := &agenthub.VendorError{Agent: agenthub.AgentTypeCodex, Err: agenthub.ErrAgentNotInstalled}
 	missingNode := fmt.Errorf("%w: %w", agenthub.ErrProcessFailed,
 		&agentruntime.Error{Kind: agentruntime.ErrRequirementNotMet, Reason: "Node.js 20.1.0 is older than 22"})
 	cases := []struct {
@@ -56,6 +59,8 @@ func TestPublicErrorMapsAgentRuntimeErrors(t *testing.T) {
 		want string
 	}{
 		{notInstalledSession, "agent_not_installed: Claude Agent is not installed. Install it in Settings → Agents."},
+		{codexNotInstalled, "agent_not_installed: Codex is not installed. Install it in Settings → Agents."},
+		{agenthub.ErrAgentNotInstalled, "agent_not_installed: The agent is not installed. Install it in Settings → Agents."},
 		{missingNode, "runtime_requirement: A requirement for the agent is not met: Node.js 20.1.0 is older than 22"},
 		{&agentruntime.Error{Kind: agentruntime.ErrInstallFailed, Reason: "npm install failed: 404 Not Found"},
 			"install_failed: Installing the agent failed: npm install failed: 404 Not Found"},
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agenthub/ ./internal/bridge/ -run 'TestVendorErrorsNameTheirAgent|TestPublicErrorMapsAgent' -count=1`
Expected: FAIL build — `undefined: ErrorVendor`, `undefined: vendorError` (agenthub), `undefined: agenthub.VendorError` (bridge).

- [ ] **Step 3: Áp patch hiện thực** (`extract t1-impl`)

<!-- patch t1-impl -->
```diff
diff --git a/internal/agenthub/adapter_claude.go b/internal/agenthub/adapter_claude.go
index 95689d64527a8b48140f91bde28c3110d07be6c5..8f417570afec9917703f2149da93418f9266e2c4 100644
--- a/internal/agenthub/adapter_claude.go
+++ b/internal/agenthub/adapter_claude.go
@@ -197,11 +197,11 @@ func (a *ClaudeAdapter) resolveCommand() (launch, error) {
 		}, nil
 	}
 	if a.opts.Runtime == nil {
-		return launch{}, ErrAgentNotInstalled
+		return launch{}, vendorError(AgentTypeClaude, ErrAgentNotInstalled)
 	}
 	rt, err := a.opts.Runtime()
 	if errors.Is(err, agentruntime.ErrNotInstalled) {
-		return launch{}, fmt.Errorf("%w: %w", ErrAgentNotInstalled, err)
+		return launch{}, vendorError(AgentTypeClaude, fmt.Errorf("%w: %w", ErrAgentNotInstalled, err))
 	}
 	if err != nil {
 		// Both sentinels stay reachable: the bridge names a missing Node
@@ -652,7 +652,7 @@ func (a *ClaudeAdapter) open(ctx context.Context, spec OpenSpec, plan openPlan)
 		// A signed-out agent answers session/new or session/load with an error
 		// and stays alive; that is a sign-in to offer, not a crash.
 		if isAuthRequired(err) {
-			return fmt.Errorf("%w: %s: %v", ErrAgentAuthRequired, stage, err)
+			return vendorError(AgentTypeClaude, fmt.Errorf("%w: %s: %v", ErrAgentAuthRequired, stage, err))
 		}
 		// A grace period rather than a peek. A vendor that died mid-handshake
 		// fails this call two ways at once — the write hits a closed pipe, and
@@ -1269,7 +1269,7 @@ func (r *claudeRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, m
 		}
 		log.Printf("agenthub: claude session/prompt (session %s): %v%s", sid, err, reason)
 		if isAuthRequired(err) {
-			return PromptOutcome{}, fmt.Errorf("%w: %v", ErrAgentAuthRequired, err)
+			return PromptOutcome{}, vendorError(AgentTypeClaude, fmt.Errorf("%w: %v", ErrAgentAuthRequired, err))
 		}
 		return PromptOutcome{}, fmt.Errorf("%w: session/prompt: %v%s", ErrProcessFailed, err, reason)
 	}
diff --git a/internal/agenthub/adapter_codex.go b/internal/agenthub/adapter_codex.go
index 1a6840024a7164308fa020ed210b1ef4e6cf4486..20d4bee52f0275dba261363d13137430c8b46b12 100644
--- a/internal/agenthub/adapter_codex.go
+++ b/internal/agenthub/adapter_codex.go
@@ -141,11 +141,11 @@ func (a *CodexAdapter) resolveCommand() (launch, error) {
 		return launch{path: path, ro: uniqueAbsPaths(append([]string{path}, codexPackageDir(path)...)...)}, nil
 	}
 	if a.opts.Runtime == nil {
-		return launch{}, ErrAgentNotInstalled
+		return launch{}, vendorError(AgentTypeCodex, ErrAgentNotInstalled)
 	}
 	rt, err := a.opts.Runtime()
 	if errors.Is(err, agentruntime.ErrNotInstalled) {
-		return launch{}, fmt.Errorf("%w: %w", ErrAgentNotInstalled, err)
+		return launch{}, vendorError(AgentTypeCodex, fmt.Errorf("%w: %w", ErrAgentNotInstalled, err))
 	}
 	if err != nil {
 		return launch{}, fmt.Errorf("%w: %w", ErrProcessFailed, err)
@@ -432,7 +432,7 @@ func (a *CodexAdapter) apiKey(ctx context.Context) (string, error) {
 	}
 	key, err := a.opts.APIKey(ctx)
 	if err != nil {
-		return "", fmt.Errorf("%w: the Codex API key could not be read: %v", ErrAgentAuthRequired, err)
+		return "", vendorError(AgentTypeCodex, fmt.Errorf("%w: the Codex API key could not be read: %v", ErrAgentAuthRequired, err))
 	}
 	return strings.TrimSpace(key), nil
 }
@@ -512,7 +512,7 @@ func (a *CodexAdapter) open(ctx context.Context, spec OpenSpec, resume string) (
 			Type: codexapp.LoginAPIKey, APIKey: key,
 		}, &login); err != nil {
 			// The server's message, never the key: it is not part of err.
-			return fail(fmt.Errorf("%w: Codex refused the API key: %v", ErrAgentAuthRequired, err))
+			return fail(vendorError(AgentTypeCodex, fmt.Errorf("%w: Codex refused the API key: %v", ErrAgentAuthRequired, err)))
 		}
 	}
 	var account codexapp.GetAccountResponse
@@ -520,7 +520,7 @@ func (a *CodexAdapter) open(ctx context.Context, spec OpenSpec, resume string) (
 		return fail(p.failure("account/read", err))
 	}
 	if account.RequiresOpenAIAuth && account.Account == nil {
-		return fail(fmt.Errorf("%w: Codex is not signed in", ErrAgentAuthRequired))
+		return fail(vendorError(AgentTypeCodex, fmt.Errorf("%w: Codex is not signed in", ErrAgentAuthRequired)))
 	}
 
 	var thread codexapp.ThreadStartResponse
@@ -1096,7 +1096,7 @@ func codexTurnError(e *codexapp.TurnError) error {
 	}
 	switch codexErrorKind(e.CodexErrorInfo) {
 	case "unauthorized":
-		return fmt.Errorf("%w: %s", ErrAgentAuthRequired, e.Message)
+		return vendorError(AgentTypeCodex, fmt.Errorf("%w: %s", ErrAgentAuthRequired, e.Message))
 	case "usageLimitExceeded":
 		return fmt.Errorf("%w: Codex usage limit reached: %s", ErrProcessFailed, e.Message)
 	case "contextWindowExceeded":
diff --git a/internal/agenthub/errors.go b/internal/agenthub/errors.go
index 64b029a6c674f3f3f5075ad8c5c755c82fc534ec..3d75c97bb7b0c696640304fb6c956d54e8f24b77 100644
--- a/internal/agenthub/errors.go
+++ b/internal/agenthub/errors.go
@@ -47,10 +47,42 @@ var (
 	ErrCallerMismatch = errors.New("agenthub.callerMismatch")
 	ErrProxyNotFound  = errors.New("agenthub.proxyNotFound")
 	// ErrAgentAuthRequired is an agent with no working sign-in: an expired or
-	// missing Claude login, or no API key. DevKit never signs anyone in; the
-	// user does that in Claude Code itself.
+	// missing login, or an API key that is gone or refused. An adapter returns
+	// it inside a VendorError, so the message can say whose sign-in to fix.
 	ErrAgentAuthRequired = errors.New("agenthub.authRequired")
 	// ErrAgentNotInstalled is an adapter with no runtime DevKit installed and
-	// no DEVKIT_CLAUDE_ACP_COMMAND override. Never a reason to try PATH or npx.
+	// no DEVKIT_*_COMMAND override. Never a reason to try PATH or npx. Like
+	// ErrAgentAuthRequired, it arrives inside a VendorError.
 	ErrAgentNotInstalled = errors.New("agenthub.notInstalled")
 )
+
+// VendorError is an error that is about one vendor rather than about Agent
+// Hub: which agent is not installed, whose sign-in expired. The error itself
+// is unchanged — Error and errors.Is see straight through — so the only thing
+// it adds is the name the person has to act on. Without it the bridge could
+// only guess, and it guessed Claude.
+type VendorError struct {
+	Agent AgentType
+	Err   error
+}
+
+func (e *VendorError) Error() string { return e.Err.Error() }
+
+func (e *VendorError) Unwrap() error { return e.Err }
+
+// vendorError names the vendor err is about; nil stays nil.
+func vendorError(agent AgentType, err error) error {
+	if err == nil {
+		return nil
+	}
+	return &VendorError{Agent: agent, Err: err}
+}
+
+// ErrorVendor is the vendor an error names, or "" when it names none.
+func ErrorVendor(err error) AgentType {
+	var v *VendorError
+	if errors.As(err, &v) {
+		return v.Agent
+	}
+	return ""
+}
diff --git a/internal/bridge/errors.go b/internal/bridge/errors.go
index 18b7820e20a039ba1d0295f509f0f44768154320..e523d45cb4f7c043a6a41f00a24281b6e39395a4 100644
--- a/internal/bridge/errors.go
+++ b/internal/bridge/errors.go
@@ -202,7 +202,7 @@ func publicError(subject string, err error) *apperrors.AppError {
 		case errors.Is(err, errAgentRuntimesUnavailable):
 			return apperrors.New(codeInternalError, "Agent runtimes are not available.", true)
 		case errors.Is(err, agenthub.ErrAgentNotInstalled):
-			return apperrors.New(codeAgentNotInstalled, "Claude Agent is not installed. Install it in Settings → Agents.", true)
+			return apperrors.New(codeAgentNotInstalled, agentNotInstalledMessage(agenthub.ErrorVendor(err)), true)
 		case errors.Is(err, agentruntime.ErrNotInstalled):
 			return apperrors.New(codeValidationFailed, "That agent version is not installed.", true)
 		case errors.Is(err, agentruntime.ErrRequirementNotMet):
@@ -254,9 +254,7 @@ func publicError(subject string, err error) *apperrors.AppError {
 		case errors.Is(err, agenthub.ErrSandboxUnavailable):
 			return apperrors.New(codeConnectionFailed, "Agent sandbox is unavailable.", true)
 		case errors.Is(err, agenthub.ErrAgentAuthRequired):
-			// Pointed at Claude Code's own sign-in, never a form of ours: third-party
-			// apps may not offer a Claude login.
-			return apperrors.New(codeAgentAuthRequired, "Claude isn't signed in, or the sign-in expired. Sign in to Claude, or set ANTHROPIC_API_KEY.", true)
+			return apperrors.New(codeAgentAuthRequired, agentAuthRequiredMessage(agenthub.ErrorVendor(err)), true)
 		case errors.Is(err, agenthub.ErrAuthMethodUnknown):
 			return apperrors.New(codeValidationFailed, "That sign-in method isn't offered by this agent.", true)
 		case errors.Is(err, agenthub.ErrProcessFailed):
@@ -406,3 +404,30 @@ func unexpectedError(subject string, err error) *apperrors.AppError {
 	log.Printf("bridge: %s: unclassified error: %v", subject, err)
 	return apperrors.New(codeInternalError, "An unexpected error occurred.", false)
 }
+
+// agentNotInstalledMessage names the agent to install. An error that names
+// none gets a sentence that does not guess: it used to say Claude whatever
+// the session was.
+func agentNotInstalledMessage(agent agenthub.AgentType) string {
+	switch agent {
+	case agenthub.AgentTypeClaude:
+		return "Claude Agent is not installed. Install it in Settings → Agents."
+	case agenthub.AgentTypeCodex:
+		return "Codex is not installed. Install it in Settings → Agents."
+	}
+	return "The agent is not installed. Install it in Settings → Agents."
+}
+
+// agentAuthRequiredMessage says whose sign-in to fix and where. Claude's
+// points at Claude Code's own login, never a form of ours: third-party apps
+// may not offer a Claude login. Codex signs in inside DevKit, in its own
+// home, so its message points at the card that does it.
+func agentAuthRequiredMessage(agent agenthub.AgentType) string {
+	switch agent {
+	case agenthub.AgentTypeClaude:
+		return "Claude isn't signed in, or the sign-in expired. Sign in to Claude, or set ANTHROPIC_API_KEY."
+	case agenthub.AgentTypeCodex:
+		return "Codex isn't signed in, or the sign-in expired. Sign in under Settings → Agents → Codex, or check the API key chosen there."
+	}
+	return "The agent isn't signed in, or the sign-in expired."
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ -count=1`
Expected: PASS. `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/adapter_claude.go internal/agenthub/adapter_codex.go internal/agenthub/errors.go internal/agenthub/vendor_error_test.go internal/bridge/errors.go internal/bridge/errors_agenthub_test.go
git commit -m "fix(agenthub): not-installed and signed-out errors name their vendor

The bridge worded both for Claude whatever the session was. Adapters
now wrap the two sentinels in agenthub.VendorError where they make them;
Error and errors.Is see straight through, so nothing that matched them
changes, and bridge/errors.go words the message per vendor.

<attribution>"
```

### Task 2: Bridge: năm method đăng nhập trong app, chỉ cho người dùng

**Model:** haiku

**Vì sao:** Spec §6.4. `AgentHubSignInStatus` (`agenthub:read`), `AgentHubStartBrowserSignIn` / `AgentHubStartDeviceSignIn` / `AgentHubCancelSignIn` / `AgentHubSignOut` (`agenthub:execute`), scope `auth:<agent>` (grant `auth:*` đã có — test app chốt `auth:codex`). Audit ghi `action`, `agent`, `method`; không bao giờ ghi trang, mã hay login id. "Chỉ UI" không dừng ở việc không có tool: `Service.inAppSignIn` từ chối caller đã được đóng dấu là agent hoặc CLI trên socket MCP chung (`ErrPermissionDenied`) — một agent bắt đầu được sign-in là đặt được tài khoản của nó vào home mà mọi session sau dùng. `internal/mcp` thêm test không cho tool/lời gọi sign-in nào xuất hiện. `ErrUnsupported` có thông báo riêng thay vì rơi xuống "unexpected error".

**Files:** Create `internal/agenthub/signin_service_test.go`, `internal/bridge/agenthub_signin_test.go`; Modify `internal/agenthub/codex_auth.go`, `internal/app/app_test.go`, `internal/app/container.go`, `internal/bridge/agenthub.go`, `internal/bridge/errors.go`, `internal/mcp/tool_names_test.go`

- [ ] **Step 1: Áp patch test** (`extract t2-test`)

<!-- patch t2-test -->
```diff
diff --git a/internal/agenthub/signin_service_test.go b/internal/agenthub/signin_service_test.go
new file mode 100644
index 0000000000000000000000000000000000000000..b7854cef423e0eaf7162a9f947cb2b39a4f029e6
--- /dev/null
+++ b/internal/agenthub/signin_service_test.go
@@ -0,0 +1,98 @@
+package agenthub
+
+import (
+	"context"
+	"errors"
+	"testing"
+
+	"devkit/internal/identity"
+)
+
+// fakeInAppSignIn is a vendor that signs in inside DevKit and records what
+// it was asked, so a test can tell a refused call from one that ran.
+type fakeInAppSignIn struct {
+	*MockAdapter
+	calls []string
+}
+
+func (f *fakeInAppSignIn) Type() AgentType { return AgentTypeCodex }
+
+func (f *fakeInAppSignIn) SignInStatus(context.Context) (SignInStatus, error) {
+	f.calls = append(f.calls, "status")
+	return SignInStatus{SignedIn: true, Method: "chatgpt"}, nil
+}
+
+func (f *fakeInAppSignIn) StartBrowserSignIn(context.Context) (SignInFlow, error) {
+	f.calls = append(f.calls, "browser")
+	return SignInFlow{LoginID: "l1", Method: SignInBrowser, AuthURL: "https://auth.example/x"}, nil
+}
+
+func (f *fakeInAppSignIn) StartDeviceSignIn(context.Context) (SignInFlow, error) {
+	f.calls = append(f.calls, "device")
+	return SignInFlow{LoginID: "l2", Method: SignInDeviceCode, UserCode: "ABCD-1234"}, nil
+}
+
+func (f *fakeInAppSignIn) CancelSignIn(_ context.Context, loginID string) error {
+	f.calls = append(f.calls, "cancel:"+loginID)
+	return nil
+}
+
+func (f *fakeInAppSignIn) SignOut(context.Context) error {
+	f.calls = append(f.calls, "signout")
+	return nil
+}
+
+// Signing in is the person's: an agent, or a CLI on the shared MCP socket,
+// is refused before the vendor hears of it — status included, so an agent
+// cannot even learn which account the home holds.
+func TestServiceSignInIsForThePersonOnly(t *testing.T) {
+	calls := func(svc *Service, ctx context.Context) []error {
+		_, statusErr := svc.SignInStatus(ctx, AgentTypeCodex)
+		_, browserErr := svc.StartSignIn(ctx, AgentTypeCodex, SignInBrowser)
+		_, deviceErr := svc.StartSignIn(ctx, AgentTypeCodex, SignInDeviceCode)
+		return []error{
+			statusErr, browserErr, deviceErr,
+			svc.CancelSignIn(ctx, AgentTypeCodex, "l1"),
+			svc.SignOut(ctx, AgentTypeCodex),
+		}
+	}
+	for name, ctx := range map[string]context.Context{
+		"agent":         identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111")),
+		"user terminal": identity.WithCaller(context.Background(), identity.UserTerminal()),
+	} {
+		vendor := &fakeInAppSignIn{MockAdapter: NewMockAdapter()}
+		reg := NewRegistry()
+		reg.Register(vendor)
+		for i, err := range calls(&Service{reg: reg}, ctx) {
+			if !errors.Is(err, ErrPermissionDenied) {
+				t.Errorf("%s: call %d = %v, want ErrPermissionDenied", name, i, err)
+			}
+		}
+		if len(vendor.calls) != 0 {
+			t.Errorf("%s: the vendor was reached: %v", name, vendor.calls)
+		}
+	}
+	for name, ctx := range map[string]context.Context{
+		"ui":         identity.WithCaller(context.Background(), identity.UI()),
+		"in process": context.Background(),
+	} {
+		vendor := &fakeInAppSignIn{MockAdapter: NewMockAdapter()}
+		reg := NewRegistry()
+		reg.Register(vendor)
+		for i, err := range calls(&Service{reg: reg}, ctx) {
+			if err != nil {
+				t.Errorf("%s: call %d = %v", name, i, err)
+			}
+		}
+		want := []string{"status", "browser", "device", "cancel:l1", "signout"}
+		if len(vendor.calls) != len(want) {
+			t.Fatalf("%s: calls = %v, want %v", name, vendor.calls, want)
+		}
+		for i := range want {
+			if vendor.calls[i] != want[i] {
+				t.Errorf("%s: calls = %v, want %v", name, vendor.calls, want)
+				break
+			}
+		}
+	}
+}
diff --git a/internal/app/app_test.go b/internal/app/app_test.go
index b7344de339638590eca86f12a2704ac0ca0eadf7..fea1704dff4d3646ff54ed3cd136fa2ee170e30a 100644
--- a/internal/app/app_test.go
+++ b/internal/app/app_test.go
@@ -64,6 +64,9 @@ func TestNewContainerGrantsAgentHubScopes(t *testing.T) {
 		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "runtime:claude-acp"},
 		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "auth:claude"},
 		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "auth:claude"},
+		// Codex signs in inside the app: status, browser or code, cancel, sign out.
+		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "auth:codex"},
+		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "auth:codex"},
 		{SubjectID: "builtin.agenthub", Capability: "terminal:io", Scope: "session:abc"},
 	} {
 		decision := container.Permissions.Check(permissions.Request{
diff --git a/internal/bridge/agenthub_signin_test.go b/internal/bridge/agenthub_signin_test.go
new file mode 100644
index 0000000000000000000000000000000000000000..69bd4a55f6896ac4b78a267c76b6aec34f61d0b5
--- /dev/null
+++ b/internal/bridge/agenthub_signin_test.go
@@ -0,0 +1,158 @@
+package bridge
+
+import (
+	"context"
+	"encoding/json"
+	"path/filepath"
+	"strings"
+	"testing"
+
+	"devkit/internal/agenthub"
+	"devkit/internal/app"
+	"devkit/internal/identity"
+	"devkit/internal/storage"
+)
+
+// codexSignInStub stands in for the Codex adapter: a vendor that signs in
+// inside DevKit, with a page and a code no audit line may carry.
+type codexSignInStub struct {
+	*agenthub.MockAdapter
+	cancelled []string
+	signedOut int
+}
+
+const (
+	stubAuthURL  = "https://auth.openai.example/oauth/authorize?state=secret-state"
+	stubUserCode = "WXYZ-9876"
+)
+
+func (c *codexSignInStub) Type() agenthub.AgentType { return agenthub.AgentTypeCodex }
+
+func (c *codexSignInStub) SignInStatus(context.Context) (agenthub.SignInStatus, error) {
+	return agenthub.SignInStatus{SignedIn: true, Method: "chatgpt", Email: "a@example.com", Plan: "plus"}, nil
+}
+
+func (c *codexSignInStub) StartBrowserSignIn(context.Context) (agenthub.SignInFlow, error) {
+	return agenthub.SignInFlow{LoginID: "login-1", Method: agenthub.SignInBrowser, AuthURL: stubAuthURL}, nil
+}
+
+func (c *codexSignInStub) StartDeviceSignIn(context.Context) (agenthub.SignInFlow, error) {
+	return agenthub.SignInFlow{LoginID: "login-2", Method: agenthub.SignInDeviceCode,
+		VerificationURL: "https://auth.openai.example/device", UserCode: stubUserCode}, nil
+}
+
+func (c *codexSignInStub) CancelSignIn(_ context.Context, loginID string) error {
+	c.cancelled = append(c.cancelled, loginID)
+	return nil
+}
+
+func (c *codexSignInStub) SignOut(context.Context) error {
+	c.signedOut++
+	return nil
+}
+
+func newBridgeWithSignIn(t *testing.T) (*AppService, *app.Container, *codexSignInStub) {
+	t.Helper()
+	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
+	if err != nil {
+		t.Fatalf("open: %v", err)
+	}
+	t.Cleanup(func() { _ = db.Close() })
+	if err := storage.Migrate(context.Background(), db); err != nil {
+		t.Fatalf("migrate: %v", err)
+	}
+	container := app.NewContainer()
+	reg := agenthub.NewRegistry()
+	stub := &codexSignInStub{MockAdapter: agenthub.NewMockAdapter()}
+	reg.Register(stub)
+	reg.Register(agenthub.NewMockAdapter())
+	container.AgentHub = agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
+	return NewAppService(container), container, stub
+}
+
+// The five sign-in methods reach the vendor through the auth:<agent> grant,
+// and the audit log says what was done and how — never the page or the code.
+func TestAgentHubInAppSignInThroughTheGateway(t *testing.T) {
+	svc, container, stub := newBridgeWithSignIn(t)
+	ctx := context.Background()
+
+	status, err := svc.AgentHubSignInStatus(ctx, "codex")
+	if err != nil || status.Email != "a@example.com" || status.Method != "chatgpt" {
+		t.Fatalf("status = %+v, %v", status, err)
+	}
+	browser, err := svc.AgentHubStartBrowserSignIn(ctx, "codex")
+	if err != nil || browser.AuthURL != stubAuthURL || browser.LoginID != "login-1" {
+		t.Fatalf("browser = %+v, %v", browser, err)
+	}
+	device, err := svc.AgentHubStartDeviceSignIn(ctx, "codex")
+	if err != nil || device.UserCode != stubUserCode {
+		t.Fatalf("device = %+v, %v", device, err)
+	}
+	if err := svc.AgentHubCancelSignIn(ctx, "codex", "login-2"); err != nil || len(stub.cancelled) != 1 || stub.cancelled[0] != "login-2" {
+		t.Fatalf("cancel: %v, %v", err, stub.cancelled)
+	}
+	if err := svc.AgentHubSignOut(ctx, "codex"); err != nil || stub.signedOut != 1 {
+		t.Fatalf("sign out: %v, %d", err, stub.signedOut)
+	}
+
+	actions := map[string]string{}
+	for _, event := range container.Audit.Events() {
+		if event.SubjectID != "builtin.agenthub" || event.Scope != "auth:codex" {
+			continue
+		}
+		action, _ := event.Metadata["action"].(string)
+		method, _ := event.Metadata["method"].(string)
+		actions[action] = method
+		if event.Outcome != "allowed" || event.Caller != "ui" {
+			t.Errorf("%s: outcome %q caller %q", action, event.Outcome, event.Caller)
+		}
+		raw, _ := json.Marshal(event.Metadata)
+		for _, secret := range []string{stubAuthURL, "secret-state", stubUserCode, "login-1", "login-2"} {
+			if strings.Contains(string(raw), secret) {
+				t.Errorf("%s: audit metadata carries %q: %s", action, secret, raw)
+			}
+		}
+	}
+	want := map[string]string{
+		"sign_in_status":        "",
+		"start_in_app_sign_in":  agenthub.SignInDeviceCode, // the later of the two starts
+		"cancel_in_app_sign_in": "",
+		"sign_out":              "",
+	}
+	for action, method := range want {
+		got, ok := actions[action]
+		if !ok || got != method {
+			t.Errorf("audit %s: method %q (recorded %v), want %q", action, got, ok, method)
+		}
+	}
+}
+
+// A caller the gateway already knows is an agent — what an MCP tool handler
+// carries — is refused, whichever door it found.
+func TestAgentHubInAppSignInRefusesAnAgentCaller(t *testing.T) {
+	svc, _, stub := newBridgeWithSignIn(t)
+	ctx := identity.WithCaller(context.Background(), identity.Agent("11111111-1111-1111-1111-111111111111"))
+	if _, err := svc.AgentHubStartBrowserSignIn(ctx, "codex"); err == nil || !strings.HasPrefix(err.Error(), "permission_denied") {
+		t.Fatalf("agent start = %v, want permission_denied", err)
+	}
+	if err := svc.AgentHubSignOut(ctx, "codex"); err == nil || !strings.HasPrefix(err.Error(), "permission_denied") {
+		t.Fatalf("agent sign out = %v, want permission_denied", err)
+	}
+	if stub.signedOut != 0 {
+		t.Fatal("an agent signed the person out")
+	}
+}
+
+// Claude signs in in a terminal; asking it for an in-app sign-in says so
+// rather than failing as an internal error.
+func TestAgentHubInAppSignInUnsupportedVendor(t *testing.T) {
+	svc, _, _ := newBridgeWithSignIn(t)
+	_, err := svc.AgentHubSignInStatus(context.Background(), "mock")
+	if err == nil || err.Error() != "validation_failed: This agent doesn't support that." {
+		t.Fatalf("mock status = %v", err)
+	}
+	if _, err := svc.AgentHubStartBrowserSignIn(context.Background(), "nobody"); err == nil ||
+		err.Error() != "validation_failed: Unknown agent vendor." {
+		t.Fatalf("unknown vendor = %v", err)
+	}
+}
diff --git a/internal/mcp/tool_names_test.go b/internal/mcp/tool_names_test.go
index a0ae4c0e7db04d567bf988d8ca46df56fd15e161..4b6a05875862d51cba694608845f4e7d9c7c35d6 100644
--- a/internal/mcp/tool_names_test.go
+++ b/internal/mcp/tool_names_test.go
@@ -58,3 +58,36 @@ func TestToolNamesNoWireCollision(t *testing.T) {
 		t.Fatal("found no tool Name literals — toolNamePattern or the glob is broken")
 	}
 }
+
+// TestNoToolSignsAgentsIn: signing an agent in or out is the person's, from
+// the app (Codex spec §6.4). A tool for it would let one session put its own
+// account in the home every later session uses, or sign the person out
+// mid-work. The service refuses an agent caller too; this keeps the door from
+// being built in the first place.
+func TestNoToolSignsAgentsIn(t *testing.T) {
+	files, err := filepath.Glob("*.go")
+	if err != nil {
+		t.Fatalf("glob: %v", err)
+	}
+	signInName := regexp.MustCompile(`(?i)sign.?in|sign.?out|log.?in|log.?out`)
+	signInCall := regexp.MustCompile(`\b(SignInStatus|StartSignIn|StartBrowserSignIn|StartDeviceSignIn|CancelSignIn|SignOut|SignInCommand)\b`)
+	for _, path := range files {
+		if strings.HasSuffix(path, "_test.go") {
+			continue
+		}
+		data, err := os.ReadFile(path)
+		if err != nil {
+			t.Fatalf("read %s: %v", path, err)
+		}
+		if strings.HasPrefix(path, "tools_") {
+			for _, m := range toolNamePattern.FindAllStringSubmatch(string(data), -1) {
+				if signInName.MatchString(m[1]) {
+					t.Errorf("%s declares tool %q: sign-in is UI only", path, m[1])
+				}
+			}
+		}
+		if loc := signInCall.FindString(string(data)); loc != "" {
+			t.Errorf("%s calls %s: sign-in is UI only", path, loc)
+		}
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agenthub/ ./internal/bridge/ -run 'SignIn' -count=1`
Expected: FAIL build ở bridge — `svc.AgentHubSignInStatus undefined` (và bốn method kia); agenthub `TestServiceSignInIsForThePersonOnly` FAIL (caller agent không bị từ chối).

- [ ] **Step 3: Áp patch hiện thực** (`extract t2-impl`)

<!-- patch t2-impl -->
```diff
diff --git a/internal/agenthub/codex_auth.go b/internal/agenthub/codex_auth.go
index d6d21f6dfc53c80581b36d431579a0d276b3ced0..8bb4c290e8daaed245d58191a832a51ba7745343 100644
--- a/internal/agenthub/codex_auth.go
+++ b/internal/agenthub/codex_auth.go
@@ -11,6 +11,7 @@ import (
 	"time"
 
 	"devkit/internal/codexapp"
+	"devkit/internal/identity"
 )
 
 // SignInStatus is a vendor's sign-in as the in-app panel shows it.
@@ -60,7 +61,16 @@ type InAppSignIn interface {
 
 var _ InAppSignIn = (*CodexAdapter)(nil)
 
-func (s *Service) inAppSignIn(t AgentType) (InAppSignIn, error) {
+// inAppSignIn finds t's in-app sign-in, for the person only. There is no MCP
+// tool for any of it, and this is why that is not the only guard: a caller
+// the gateway stamped as an agent or a terminal MCP client is refused here,
+// so a door added later by mistake still opens onto nothing. An agent that
+// could start a sign-in could put its own account in the home every later
+// session uses; one that could sign out could lock the person out mid-work.
+func (s *Service) inAppSignIn(ctx context.Context, t AgentType) (InAppSignIn, error) {
+	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
+		return nil, fmt.Errorf("%w: signing in is the person's, from the app", ErrPermissionDenied)
+	}
 	adapter, ok := s.reg.Get(t)
 	if !ok {
 		return nil, ErrVendorUnknown
@@ -74,7 +84,7 @@ func (s *Service) inAppSignIn(t AgentType) (InAppSignIn, error) {
 
 // SignInStatus reads an agent type's sign-in.
 func (s *Service) SignInStatus(ctx context.Context, t AgentType) (SignInStatus, error) {
-	signIn, err := s.inAppSignIn(t)
+	signIn, err := s.inAppSignIn(ctx, t)
 	if err != nil {
 		return SignInStatus{}, err
 	}
@@ -83,7 +93,7 @@ func (s *Service) SignInStatus(ctx context.Context, t AgentType) (SignInStatus,
 
 // StartSignIn begins a sign-in by browser or device code.
 func (s *Service) StartSignIn(ctx context.Context, t AgentType, method string) (SignInFlow, error) {
-	signIn, err := s.inAppSignIn(t)
+	signIn, err := s.inAppSignIn(ctx, t)
 	if err != nil {
 		return SignInFlow{}, err
 	}
@@ -98,7 +108,7 @@ func (s *Service) StartSignIn(ctx context.Context, t AgentType, method string) (
 
 // CancelSignIn abandons a sign-in in progress.
 func (s *Service) CancelSignIn(ctx context.Context, t AgentType, loginID string) error {
-	signIn, err := s.inAppSignIn(t)
+	signIn, err := s.inAppSignIn(ctx, t)
 	if err != nil {
 		return err
 	}
@@ -107,7 +117,7 @@ func (s *Service) CancelSignIn(ctx context.Context, t AgentType, loginID string)
 
 // SignOut removes an agent type's stored sign-in.
 func (s *Service) SignOut(ctx context.Context, t AgentType) error {
-	signIn, err := s.inAppSignIn(t)
+	signIn, err := s.inAppSignIn(ctx, t)
 	if err != nil {
 		return err
 	}
diff --git a/internal/app/container.go b/internal/app/container.go
index 27bcdcb7b77178cef8951ec48a61f792db914930..a01da3bb34729cd3e559c7c0881b17e7f2fb29f9 100644
--- a/internal/app/container.go
+++ b/internal/app/container.go
@@ -311,9 +311,12 @@ func NewContainerWithStores(
 		// switching or removing an adapter version. Scoped per agent id.
 		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "runtime:*"},
 		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "runtime:*"},
-		// Sign-in: listing an agent's login methods, and running one in a
-		// terminal the user sees. The terminal grant is what lets that tab be
-		// typed into, resized and closed, the same grant CodeGraph's shells have.
+		// Sign-in: listing an agent's login methods and running one in a
+		// terminal the user sees (Claude), or signing in inside the app —
+		// status, browser or code, cancel, sign out (Codex). The terminal grant
+		// is what lets that tab be typed into, resized and closed, the same
+		// grant CodeGraph's shells have. No MCP tool uses auth:*, and the
+		// service refuses an agent caller for the in-app sign-in.
 		{SubjectID: "builtin.agenthub", Capability: "agenthub:read", Scope: "auth:*"},
 		{SubjectID: "builtin.agenthub", Capability: "agenthub:execute", Scope: "auth:*"},
 		{SubjectID: "builtin.agenthub", Capability: "terminal:io", Scope: "session:*"},
diff --git a/internal/bridge/agenthub.go b/internal/bridge/agenthub.go
index 754f1f69a07a386f07b883b6480bb7bffffde15c..88301f14c2b90673b52190a41fb29863069045c6 100644
--- a/internal/bridge/agenthub.go
+++ b/internal/bridge/agenthub.go
@@ -84,6 +84,101 @@ func (s *AppService) AgentHubStartSignIn(ctx context.Context, agent, methodID st
 	return result.(string), nil
 }
 
+// AgentHubSignInStatus reads an agent's in-app sign-in ("codex"): ChatGPT
+// with its email and plan, an API key, or none. Claude signs in in a terminal
+// (AgentHubAuthMethods) and answers "not supported" here.
+//
+// These five are the person's only. There is no MCP tool for any of them, and
+// the service refuses a caller stamped as an agent or a terminal MCP client,
+// so the capability below is not the only thing between an agent and the
+// account every later session signs in with.
+func (s *AppService) AgentHubSignInStatus(ctx context.Context, agent string) (agenthub.SignInStatus, error) {
+	id := agentRuntimeID(agent)
+	result, err := s.call(ctx, "builtin.agenthub", "agenthub:read", "auth:"+id, map[string]any{
+		"action": "sign_in_status",
+		"agent":  id,
+	}, func(callCtx context.Context) (any, error) {
+		svc, err := s.agentHubService()
+		if err != nil {
+			return nil, err
+		}
+		return svc.SignInStatus(callCtx, agenthub.AgentType(agent))
+	})
+	if err != nil {
+		return agenthub.SignInStatus{}, err
+	}
+	return result.(agenthub.SignInStatus), nil
+}
+
+// AgentHubStartBrowserSignIn starts the default sign-in: the vendor serves a
+// login page and its callback from inside the jail, and the UI opens the page.
+// How it ends arrives as an agenthub:signInCompleted event.
+func (s *AppService) AgentHubStartBrowserSignIn(ctx context.Context, agent string) (agenthub.SignInFlow, error) {
+	return s.agentHubStartInAppSignIn(ctx, agent, agenthub.SignInBrowser)
+}
+
+// AgentHubStartDeviceSignIn starts a sign-in by code, for a machine that
+// cannot open a browser. Off by default per ChatGPT account; the UI says how
+// to turn it on.
+func (s *AppService) AgentHubStartDeviceSignIn(ctx context.Context, agent string) (agenthub.SignInFlow, error) {
+	return s.agentHubStartInAppSignIn(ctx, agent, agenthub.SignInDeviceCode)
+}
+
+// agentHubStartInAppSignIn records how, never what: the page and the code in
+// the answer are the person's to use once and stay out of the audit log.
+func (s *AppService) agentHubStartInAppSignIn(ctx context.Context, agent, method string) (agenthub.SignInFlow, error) {
+	id := agentRuntimeID(agent)
+	result, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "auth:"+id, map[string]any{
+		"action": "start_in_app_sign_in",
+		"agent":  id,
+		"method": method,
+	}, func(callCtx context.Context) (any, error) {
+		svc, err := s.agentHubService()
+		if err != nil {
+			return nil, err
+		}
+		return svc.StartSignIn(callCtx, agenthub.AgentType(agent), method)
+	})
+	if err != nil {
+		return agenthub.SignInFlow{}, err
+	}
+	return result.(agenthub.SignInFlow), nil
+}
+
+// AgentHubCancelSignIn abandons the sign-in in progress. A login id that is
+// no longer current is already over, and cancelling it is not an error.
+func (s *AppService) AgentHubCancelSignIn(ctx context.Context, agent, loginID string) error {
+	id := agentRuntimeID(agent)
+	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "auth:"+id, map[string]any{
+		"action": "cancel_in_app_sign_in",
+		"agent":  id,
+	}, func(callCtx context.Context) (any, error) {
+		svc, err := s.agentHubService()
+		if err != nil {
+			return nil, err
+		}
+		return nil, svc.CancelSignIn(callCtx, agenthub.AgentType(agent), loginID)
+	})
+	return err
+}
+
+// AgentHubSignOut removes the vendor's own sign-in from DevKit's home for it.
+// An API key chosen in Settings is not signed out here; it is unlinked there.
+func (s *AppService) AgentHubSignOut(ctx context.Context, agent string) error {
+	id := agentRuntimeID(agent)
+	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "auth:"+id, map[string]any{
+		"action": "sign_out",
+		"agent":  id,
+	}, func(callCtx context.Context) (any, error) {
+		svc, err := s.agentHubService()
+		if err != nil {
+			return nil, err
+		}
+		return nil, svc.SignOut(callCtx, agenthub.AgentType(agent))
+	})
+	return err
+}
+
 var errAgentHubUnavailable = errors.New("agenthub is not available")
 
 func (s *AppService) agentHubService() (*agenthub.Service, error) {
diff --git a/internal/bridge/errors.go b/internal/bridge/errors.go
index e523d45cb4f7c043a6a41f00a24281b6e39395a4..c0f4f38843539d94a24b975dbde743b074655498 100644
--- a/internal/bridge/errors.go
+++ b/internal/bridge/errors.go
@@ -255,6 +255,8 @@ func publicError(subject string, err error) *apperrors.AppError {
 			return apperrors.New(codeConnectionFailed, "Agent sandbox is unavailable.", true)
 		case errors.Is(err, agenthub.ErrAgentAuthRequired):
 			return apperrors.New(codeAgentAuthRequired, agentAuthRequiredMessage(agenthub.ErrorVendor(err)), true)
+		case errors.Is(err, agenthub.ErrUnsupported):
+			return apperrors.New(codeValidationFailed, "This agent doesn't support that.", true)
 		case errors.Is(err, agenthub.ErrAuthMethodUnknown):
 			return apperrors.New(codeValidationFailed, "That sign-in method isn't offered by this agent.", true)
 		case errors.Is(err, agenthub.ErrProcessFailed):
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ ./internal/app/ -count=1`
Expected: PASS. `go vet ./internal/bridge/ ./internal/agenthub/` sạch.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/codex_auth.go internal/agenthub/signin_service_test.go internal/app/app_test.go internal/app/container.go internal/bridge/agenthub.go internal/bridge/agenthub_signin_test.go internal/bridge/errors.go internal/mcp/tool_names_test.go
git commit -m "feat(bridge): in-app agent sign-in, for the person only

Status, browser or code, cancel and sign out under auth:<agent>, and
the audit line says how but never the page, the code or the login id.
There is no MCP tool for any of it, a test keeps it that way, and the
service refuses a caller stamped as an agent or a terminal MCP client.

<attribution>"
```

### Task 3: API key của Codex từ Accounts; dòng `external` "Codex (ChatGPT)"

**Model:** sonnet

**Vì sao:** Spec §6.2–6.3. `preferences` lưu **id** account (`agent_codex_api_key_account`), không lưu key. `app.CodexAPIKey{C}` đọc container lúc gọi (adapter Codex được dựng trong `main.go` trước khi Accounts có): `Key` resolve token lúc spawn (đó là `CodexOptions.APIKey`), `Choose` từ chối account không dùng được (không phải `token`, scoped, giới hạn env — spawn không có env nên `Resolve` sẽ luôn từ chối — hoặc không tồn tại) để lỗi hiện ở Settings chứ không ở session sau, `UsedBy` làm Accounts không cho xoá key đang chọn. Accounts thêm `RecordExternal`/`ForgetExternal` (một dòng cho mỗi provider+label, gọi lại không nhân bản); provider `openai` nhận `KindExternal`. `main.go` ghi dòng khi `account/login/completed` báo thành công; `AgentHubSignOut` xoá nó (lỗi xoá chỉ log — credential đã rời home). Bridge: `PreferencesGet/SetCodexAPIKeyAccount` (grant `agent_codex_api_key`), audit theo id.

**Files:** Create `internal/app/agentcodex.go`, `internal/app/agentcodex_test.go`, `internal/bridge/codex_api_key_test.go`; Modify `internal/accounts/accounts_test.go`, `internal/accounts/provider.go`, `internal/accounts/service.go`, `internal/app/container.go`, `internal/app/wiring.go`, `internal/bridge/agenthub.go`, `internal/bridge/errors.go`, `internal/bridge/preferences.go`, `internal/preferences/memory.go`, `internal/preferences/preferences.go`, `internal/preferences/sqlrepository.go`, `internal/preferences/sqlrepository_test.go`, `main.go`

- [ ] **Step 1: Áp patch test** (`extract t3-test`)

<!-- patch t3-test -->
```diff
diff --git a/internal/accounts/accounts_test.go b/internal/accounts/accounts_test.go
index 0eb1556c5a136cc2a62e379aa4865273606023fb..da7cdfcd6b8cc3d7092e6d0a2aedc824730a4479 100644
--- a/internal/accounts/accounts_test.go
+++ b/internal/accounts/accounts_test.go
@@ -465,3 +465,51 @@ func TestRevealIgnoresTheEnvironmentRestriction(t *testing.T) {
 		t.Fatalf("revealed = %+v", credential)
 	}
 }
+
+// A vendor sign-in DevKit does not hold is one row however often it happens,
+// and goes when the sign-in does.
+func TestExternalAccountIsRecordedOnceAndForgotten(t *testing.T) {
+	svc, _ := newService(t)
+	first, err := svc.RecordExternal("openai", "Codex (ChatGPT)", "Settings → Agents → Codex")
+	if err != nil {
+		t.Fatal(err)
+	}
+	again, err := svc.RecordExternal("openai", "Codex (ChatGPT)", "Settings → Agents → Codex")
+	if err != nil || again.ID != first.ID {
+		t.Fatalf("second record = %+v, %v; want the first row back", again, err)
+	}
+	if first.Kind != KindExternal || first.Ref != "Settings → Agents → Codex" {
+		t.Fatalf("row = %+v", first)
+	}
+	// A token under the same provider and label is not the note.
+	if _, err := svc.Create(CreateInput{Provider: "openai", Kind: KindToken, Label: "Codex (ChatGPT)", Secret: "sk-x"}); err != nil {
+		t.Fatal(err)
+	}
+	list, _ := svc.List()
+	if n := countKind(list, KindExternal); n != 1 {
+		t.Fatalf("%d external rows, want 1", n)
+	}
+	if err := svc.ForgetExternal("openai", "Codex (ChatGPT)"); err != nil {
+		t.Fatal(err)
+	}
+	if err := svc.ForgetExternal("openai", "Codex (ChatGPT)"); err != nil {
+		t.Fatalf("forgetting twice = %v", err)
+	}
+	list, _ = svc.List()
+	if n := countKind(list, KindExternal); n != 0 {
+		t.Fatalf("%d external rows after forgetting, want 0", n)
+	}
+	if n := countKind(list, KindToken); n != 1 {
+		t.Fatalf("forgetting the note took the token with it: %+v", list)
+	}
+}
+
+func countKind(list []Account, kind Kind) int {
+	n := 0
+	for _, a := range list {
+		if a.Kind == kind {
+			n++
+		}
+	}
+	return n
+}
diff --git a/internal/app/agentcodex_test.go b/internal/app/agentcodex_test.go
new file mode 100644
index 0000000000000000000000000000000000000000..01e201d3c25c470ad9c3c9dc4eb6975244db32df
--- /dev/null
+++ b/internal/app/agentcodex_test.go
@@ -0,0 +1,118 @@
+package app
+
+import (
+	"context"
+	"errors"
+	"testing"
+
+	"devkit/internal/accounts"
+	"devkit/internal/agenthub"
+)
+
+// The key Codex signs in with is an Accounts token chosen by id: resolved at
+// spawn, rotated in one place, and never a value preferences hold.
+func TestCodexAPIKeyResolvesTheChosenToken(t *testing.T) {
+	c := credentialContainer(t)
+	c.Accounts.RegisterUsage(CodexAPIKey{C: c})
+	key := CodexAPIKey{C: c}
+
+	if got, err := key.Key(context.Background()); err != nil || got != "" {
+		t.Fatalf("nothing chosen = %q, %v; want ChatGPT", got, err)
+	}
+	token, err := c.Accounts.Create(accounts.CreateInput{Provider: "openai", Kind: accounts.KindToken, Label: "OpenAI", Secret: "sk-first"})
+	if err != nil {
+		t.Fatal(err)
+	}
+	if err := key.Choose(token.ID); err != nil {
+		t.Fatal(err)
+	}
+	if stored, _ := c.Preferences.CodexAPIKeyAccount(); stored != token.ID {
+		t.Fatalf("preferences hold %q, want the account id", stored)
+	}
+	if got, err := key.Key(context.Background()); err != nil || got != "sk-first" {
+		t.Fatalf("Key = %q, %v", got, err)
+	}
+	if err := c.Accounts.Rotate(token.ID, "", "sk-second"); err != nil {
+		t.Fatal(err)
+	}
+	if got, _ := key.Key(context.Background()); got != "sk-second" {
+		t.Fatalf("after rotate Key = %q, want the new value", got)
+	}
+
+	// Deleting it from under Codex is refused and says why.
+	var inUse *accounts.InUseError
+	if err := c.Accounts.Delete(token.ID); !errors.As(err, &inUse) || inUse.UsedBy[0] != codexAPIKeyUsage {
+		t.Fatalf("delete = %v, want InUseError naming Codex", err)
+	}
+	if err := key.Choose(""); err != nil {
+		t.Fatal(err)
+	}
+	if got, err := key.Key(context.Background()); err != nil || got != "" {
+		t.Fatalf("cleared = %q, %v", got, err)
+	}
+	if err := c.Accounts.Delete(token.ID); err != nil {
+		t.Fatalf("delete after unlinking = %v", err)
+	}
+}
+
+// Choose refuses what a spawn could not use, so the mistake shows in
+// Settings rather than as a failed session later.
+func TestCodexAPIKeyRefusesAnAccountItCannotUse(t *testing.T) {
+	c := credentialContainer(t)
+	key := CodexAPIKey{C: c}
+	basic, _ := c.Accounts.Create(accounts.CreateInput{Provider: "generic", Kind: accounts.KindBasic, Label: "basic", User: "u", Secret: "p"})
+	limited, _ := c.Accounts.Create(accounts.CreateInput{Provider: "openai", Kind: accounts.KindToken, Label: "prod only", Secret: "sk", Env: []string{"prod"}})
+	scoped, _ := c.Accounts.Create(accounts.CreateInput{Provider: "database", Kind: accounts.KindSecret, Label: "db", Secret: "pw", ScopeOwner: "0123456789abcdef01234567"})
+	for name, id := range map[string]string{
+		"basic":      basic.ID,
+		"env-bound":  limited.ID,
+		"scoped":     scoped.ID,
+		"missing":    "fedcba9876543210fedcba98",
+		"not an id!": "../../etc/passwd",
+	} {
+		if err := key.Choose(id); !errors.Is(err, ErrCodexAPIKeyAccount) {
+			t.Errorf("%s: Choose = %v, want ErrCodexAPIKeyAccount", name, err)
+		}
+	}
+	if stored, _ := c.Preferences.CodexAPIKeyAccount(); stored != "" {
+		t.Fatalf("a refused choice was stored: %q", stored)
+	}
+	// Chosen while it was fine, then gone: the session is told, not handed "".
+	good, _ := c.Accounts.Create(accounts.CreateInput{Provider: "openai", Kind: accounts.KindToken, Label: "ok", Secret: "sk"})
+	if err := key.Choose(good.ID); err != nil {
+		t.Fatal(err)
+	}
+	if err := c.Preferences.SetCodexAPIKeyAccount("fedcba9876543210fedcba98"); err != nil {
+		t.Fatal(err)
+	}
+	if _, err := key.Key(context.Background()); err == nil {
+		t.Fatal("a chosen key that is gone must be an error, not a silent ChatGPT fallback")
+	}
+}
+
+// Codex's ChatGPT sign-in shows in Accounts as one external row, and goes
+// with the sign-in. Other agents record nothing.
+func TestAgentSignInIsNotedInAccounts(t *testing.T) {
+	c := credentialContainer(t)
+	for range 2 {
+		if err := RecordAgentSignIn(c, agenthub.AgentTypeCodex); err != nil {
+			t.Fatal(err)
+		}
+	}
+	if err := RecordAgentSignIn(c, agenthub.AgentTypeClaude); err != nil {
+		t.Fatal(err)
+	}
+	list, _ := c.Accounts.List()
+	if len(list) != 1 || list[0].Kind != accounts.KindExternal || list[0].Label != CodexExternalLabel || list[0].Provider != "openai" {
+		t.Fatalf("accounts = %+v, want one Codex (ChatGPT) note", list)
+	}
+	if err := ForgetAgentSignIn(c, agenthub.AgentTypeCodex); err != nil {
+		t.Fatal(err)
+	}
+	if list, _ := c.Accounts.List(); len(list) != 0 {
+		t.Fatalf("accounts after sign-out = %+v", list)
+	}
+	if RecordAgentSignIn(nil, agenthub.AgentTypeCodex) != nil || ForgetAgentSignIn(&Container{}, agenthub.AgentTypeCodex) != nil {
+		t.Fatal("no Accounts is nothing to record, not an error")
+	}
+}
diff --git a/internal/bridge/codex_api_key_test.go b/internal/bridge/codex_api_key_test.go
new file mode 100644
index 0000000000000000000000000000000000000000..01447df40a680912d4f4ee9973b461b87bbf34e0
--- /dev/null
+++ b/internal/bridge/codex_api_key_test.go
@@ -0,0 +1,101 @@
+package bridge
+
+import (
+	"context"
+	"encoding/json"
+	"path/filepath"
+	"strings"
+	"testing"
+
+	"devkit/internal/accounts"
+	"devkit/internal/agenthub"
+	"devkit/internal/app"
+	"devkit/internal/storage"
+)
+
+// Choosing Codex's API key crosses the bridge as an account id: stored,
+// read back, audited by id, and the key itself never in the audit log.
+func TestCodexAPIKeyAccountThroughTheGateway(t *testing.T) {
+	svc, container := newBridgeWithAccounts(t)
+	token, err := container.Accounts.Create(accounts.CreateInput{
+		Provider: "openai", Kind: accounts.KindToken, Label: "OpenAI", Secret: "sk-live-secret",
+	})
+	if err != nil {
+		t.Fatal(err)
+	}
+	if got, err := svc.PreferencesGetCodexAPIKeyAccount(); err != nil || got != "" {
+		t.Fatalf("unset = %q, %v", got, err)
+	}
+	if err := svc.PreferencesSetCodexAPIKeyAccount(token.ID); err != nil {
+		t.Fatal(err)
+	}
+	if got, err := svc.PreferencesGetCodexAPIKeyAccount(); err != nil || got != token.ID {
+		t.Fatalf("stored = %q, %v", got, err)
+	}
+	if got, err := (app.CodexAPIKey{C: container}).Key(context.Background()); err != nil || got != "sk-live-secret" {
+		t.Fatalf("resolved = %q, %v", got, err)
+	}
+
+	var audited bool
+	for _, event := range container.Audit.Events() {
+		raw, _ := json.Marshal(event.Metadata)
+		if strings.Contains(string(raw), "sk-live-secret") {
+			t.Fatalf("the key reached the audit log: %s", raw)
+		}
+		if event.Scope == "agent_codex_api_key" && event.Metadata["action"] == "set_codex_api_key_account" {
+			audited = event.Metadata["account_id"] == token.ID
+		}
+	}
+	if !audited {
+		t.Fatal("choosing the key was not audited by account id")
+	}
+}
+
+// An account a session could not use is refused with a sentence, not an
+// internal error.
+func TestCodexAPIKeyAccountRefusesANonToken(t *testing.T) {
+	svc, container := newBridgeWithAccounts(t)
+	basic, err := container.Accounts.Create(accounts.CreateInput{
+		Provider: "generic", Kind: accounts.KindBasic, Label: "basic", User: "u", Secret: "p",
+	})
+	if err != nil {
+		t.Fatal(err)
+	}
+	err = svc.PreferencesSetCodexAPIKeyAccount(basic.ID)
+	if err == nil || err.Error() != "validation_failed: "+app.ErrCodexAPIKeyAccount.Error() {
+		t.Fatalf("basic account = %v", err)
+	}
+}
+
+// Signing Codex out removes the Accounts note its sign-in left.
+func TestAgentHubSignOutForgetsTheAccountsNote(t *testing.T) {
+	svc, container := newBridgeWithAccounts(t)
+	db, err := storage.Open(filepath.Join(t.TempDir(), "hub.db"))
+	if err != nil {
+		t.Fatal(err)
+	}
+	t.Cleanup(func() { _ = db.Close() })
+	if err := storage.Migrate(context.Background(), db); err != nil {
+		t.Fatal(err)
+	}
+	reg := agenthub.NewRegistry()
+	stub := &codexSignInStub{MockAdapter: agenthub.NewMockAdapter()}
+	reg.Register(stub)
+	container.AgentHub = agenthub.NewService(reg, agenthub.NewSQLStore(db), nil)
+
+	if err := app.RecordAgentSignIn(container, agenthub.AgentTypeCodex); err != nil {
+		t.Fatal(err)
+	}
+	if list, _ := container.Accounts.List(); len(list) != 1 {
+		t.Fatalf("accounts = %+v, want the note", list)
+	}
+	if err := svc.AgentHubSignOut(context.Background(), "codex"); err != nil {
+		t.Fatal(err)
+	}
+	if stub.signedOut != 1 {
+		t.Fatal("the vendor was not signed out")
+	}
+	if list, _ := container.Accounts.List(); len(list) != 0 {
+		t.Fatalf("accounts after sign-out = %+v", list)
+	}
+}
diff --git a/internal/preferences/sqlrepository_test.go b/internal/preferences/sqlrepository_test.go
index e13216338957a20653d41d55feb9e1814b89b74d..babd6bb8b6189c0509f81b281a81325fab67d2a8 100644
--- a/internal/preferences/sqlrepository_test.go
+++ b/internal/preferences/sqlrepository_test.go
@@ -182,3 +182,25 @@ func TestSQLRepositoryUnreadableAgentSandboxIsStrict(t *testing.T) {
 		t.Fatalf("settings = %+v, want StrictAgentSandbox", got)
 	}
 }
+
+// The Codex API key is an Accounts id, never the key; unset and cleared both
+// read as "use the ChatGPT sign-in".
+func TestSQLRepositoryCodexAPIKeyAccountRoundTrip(t *testing.T) {
+	repo, _ := checkpointRepo(t)
+	service := preferences.NewService(repo)
+	if got, err := service.CodexAPIKeyAccount(); err != nil || got != "" {
+		t.Fatalf("unset = %q, %v", got, err)
+	}
+	if err := service.SetCodexAPIKeyAccount(" 0123456789abcdef01234567 "); err != nil {
+		t.Fatal(err)
+	}
+	if got, err := service.CodexAPIKeyAccount(); err != nil || got != "0123456789abcdef01234567" {
+		t.Fatalf("stored = %q, %v", got, err)
+	}
+	if err := service.SetCodexAPIKeyAccount(""); err != nil {
+		t.Fatal(err)
+	}
+	if got, err := service.CodexAPIKeyAccount(); err != nil || got != "" {
+		t.Fatalf("cleared = %q, %v", got, err)
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/accounts/ ./internal/preferences/ ./internal/app/ ./internal/bridge/ -run 'External|CodexAPIKey|AgentSignIn|SignOutForgets' -count=1`
Expected: FAIL build — `svc.RecordExternal undefined`, `service.CodexAPIKeyAccount undefined`, `undefined: CodexAPIKey`, `svc.PreferencesSetCodexAPIKeyAccount undefined`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t3-impl`)

<!-- patch t3-impl -->
```diff
diff --git a/internal/accounts/provider.go b/internal/accounts/provider.go
index fb562f37d4712fed4b10932320099cc0ad3b879c..ea442bdb25d58107122d183ffe3119f59db4b87d 100644
--- a/internal/accounts/provider.go
+++ b/internal/accounts/provider.go
@@ -79,7 +79,8 @@ func DefaultProviders() []Provider {
 			Hosts: []string{"bitbucket.org", "api.bitbucket.org"}},
 		{ID: "anthropic", Name: "Anthropic", Kinds: []Kind{KindToken}, Verify: anthropic,
 			Hosts: []string{"api.anthropic.com"}},
-		{ID: "openai", Name: "OpenAI", Kinds: []Kind{KindToken}, Hosts: []string{"api.openai.com"}},
+		// External: Codex's ChatGPT sign-in, kept in Codex's own home.
+		{ID: "openai", Name: "OpenAI", Kinds: []Kind{KindToken, KindExternal}, Hosts: []string{"api.openai.com"}},
 		{ID: "google", Name: "Google", Kinds: []Kind{KindToken, KindOAuth}},
 		{ID: "kong", Name: "Kong Admin", Kinds: []Kind{KindToken, KindBasic}},
 		{ID: ProviderDatabase, Name: "Database", Kinds: []Kind{KindSecret, KindBasic}},
diff --git a/internal/accounts/service.go b/internal/accounts/service.go
index 05037242b491051f2d8e257fb091f3eb0c158263..49a49819346bc1ce7ecc20d6a429b0fa56eb37b9 100644
--- a/internal/accounts/service.go
+++ b/internal/accounts/service.go
@@ -494,6 +494,51 @@ func (s *Service) SweepOrphans(liveOwners []string) (int, error) {
 	return len(rows), nil
 }
 
+// RecordExternal notes that an identity exists somewhere DevKit does not hold
+// it — a vendor's own sign-in — and how to sign in again. One row per
+// provider and label: recording it again changes nothing, so a person who
+// signs in every week does not collect a list of them.
+func (s *Service) RecordExternal(provider, label, ref string) (Account, error) {
+	existing, err := s.findExternal(provider, label)
+	if err == nil {
+		return existing, nil
+	}
+	if !errors.Is(err, ErrNotFound) {
+		return Account{}, err
+	}
+	return s.Create(CreateInput{Provider: provider, Kind: KindExternal, Label: label, Ref: ref})
+}
+
+// ForgetExternal removes the note RecordExternal made, after the sign-in it
+// describes is gone. Nothing recorded is not an error.
+func (s *Service) ForgetExternal(provider, label string) error {
+	existing, err := s.findExternal(provider, label)
+	if errors.Is(err, ErrNotFound) {
+		return nil
+	}
+	if err != nil {
+		return err
+	}
+	return s.Delete(existing.ID)
+}
+
+func (s *Service) findExternal(provider, label string) (Account, error) {
+	records, err := s.items.ListByType(vaultitem.ItemTypeAccount)
+	if err != nil {
+		return Account{}, err
+	}
+	for _, record := range records {
+		account, err := decodeMetadata(record)
+		if err != nil {
+			continue
+		}
+		if account.Kind == KindExternal && account.Provider == provider && account.Label == label {
+			return s.decorate(account), nil
+		}
+	}
+	return Account{}, ErrNotFound
+}
+
 // Providers lists the registry, for the UI's New Account form.
 func (s *Service) Providers() []Provider { return s.providers.List() }
 
diff --git a/internal/app/agentcodex.go b/internal/app/agentcodex.go
new file mode 100644
index 0000000000000000000000000000000000000000..435a5ab6448f9d4347716bb2c012cea06b10751d
--- /dev/null
+++ b/internal/app/agentcodex.go
@@ -0,0 +1,111 @@
+package app
+
+import (
+	"context"
+	"errors"
+	"strings"
+
+	"devkit/internal/accounts"
+	"devkit/internal/agenthub"
+)
+
+// ErrCodexAPIKeyAccount is an account that cannot be Codex's API key: gone,
+// not a token, a single connection's secret, or limited to an environment
+// (a spawn has none to offer, so Resolve would refuse it every time).
+var ErrCodexAPIKeyAccount = errors.New("Choose an API key (a token account) that is not limited to an environment.")
+
+// The note Accounts keeps of Codex's ChatGPT sign-in. The credential itself
+// is in Codex's own home; this row only answers "what am I signed in to".
+const (
+	codexExternalProvider = "openai"
+	CodexExternalLabel    = "Codex (ChatGPT)"
+	codexExternalRef      = "Sign in again in Settings → Agents → Codex."
+)
+
+// codexAPIKeyUsage is what Accounts says binds the chosen key.
+const codexAPIKeyUsage = "Agent Hub: Codex API key"
+
+// CodexAPIKey is the API key Codex sessions sign in with instead of ChatGPT:
+// an Accounts token, chosen in Settings, named in preferences by id and
+// resolved here at spawn (Codex spec §6.2). The key lives in Accounts and
+// nowhere else — rotating it there is all it takes — and it reaches Codex
+// over the wire after initialize, never on disk, in the environment or argv.
+//
+// It holds the container rather than the two services because main builds
+// the Codex adapter before the database and Accounts exist; both are read at
+// call time.
+type CodexAPIKey struct{ C *Container }
+
+// Key is CodexOptions.APIKey: the chosen key, or "" for ChatGPT.
+func (k CodexAPIKey) Key(context.Context) (string, error) {
+	id, err := k.chosen()
+	if err != nil || id == "" {
+		return "", err
+	}
+	if k.C.Accounts == nil {
+		return "", accounts.ErrNotFound
+	}
+	credential, err := k.C.Accounts.Resolve(accounts.ResolveInput{AccountID: id, By: "agenthub.codex"})
+	if err != nil {
+		return "", err
+	}
+	if credential.Kind != accounts.KindToken || strings.TrimSpace(credential.Secret) == "" {
+		return "", ErrCodexAPIKeyAccount
+	}
+	return credential.Secret, nil
+}
+
+// Choose stores which account is the key; "" goes back to ChatGPT. It
+// refuses an account a spawn could not resolve, so the mistake is made here
+// and not at the next session.
+func (k CodexAPIKey) Choose(accountID string) error {
+	if k.C.Preferences == nil {
+		return errors.New("preferences are not available")
+	}
+	accountID = strings.TrimSpace(accountID)
+	if accountID != "" {
+		if k.C.Accounts == nil {
+			return ErrCodexAPIKeyAccount
+		}
+		account, err := k.C.Accounts.Get(accountID)
+		if err != nil || account.Kind != accounts.KindToken || account.Scoped() || len(account.Env) > 0 {
+			return ErrCodexAPIKeyAccount
+		}
+	}
+	return k.C.Preferences.SetCodexAPIKeyAccount(accountID)
+}
+
+// UsedBy is accounts.Usage: the chosen key cannot be deleted from under
+// Codex without saying so.
+func (k CodexAPIKey) UsedBy(accountID string) []string {
+	id, err := k.chosen()
+	if err != nil || id == "" || id != accountID {
+		return nil
+	}
+	return []string{codexAPIKeyUsage}
+}
+
+func (k CodexAPIKey) chosen() (string, error) {
+	if k.C == nil || k.C.Preferences == nil {
+		return "", nil
+	}
+	return k.C.Preferences.CodexAPIKeyAccount()
+}
+
+// RecordAgentSignIn notes in Accounts that an agent signed in inside DevKit
+// (a KindExternal row: no secret, how to sign in again). Only Codex does.
+func RecordAgentSignIn(c *Container, agent agenthub.AgentType) error {
+	if agent != agenthub.AgentTypeCodex || c == nil || c.Accounts == nil {
+		return nil
+	}
+	_, err := c.Accounts.RecordExternal(codexExternalProvider, CodexExternalLabel, codexExternalRef)
+	return err
+}
+
+// ForgetAgentSignIn removes that note after the agent signed out.
+func ForgetAgentSignIn(c *Container, agent agenthub.AgentType) error {
+	if agent != agenthub.AgentTypeCodex || c == nil || c.Accounts == nil {
+		return nil
+	}
+	return c.Accounts.ForgetExternal(codexExternalProvider, CodexExternalLabel)
+}
diff --git a/internal/app/container.go b/internal/app/container.go
index a01da3bb34729cd3e559c7c0881b17e7f2fb29f9..f18e18582c8252847e830949420e378a1b07f84a 100644
--- a/internal/app/container.go
+++ b/internal/app/container.go
@@ -351,6 +351,9 @@ func NewContainerWithStores(
 		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_checkpoints"},
 		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_sandbox"},
 		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_sandbox"},
+		// Which Accounts token Codex signs in with: the id, never the key.
+		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "agent_codex_api_key"},
+		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "agent_codex_api_key"},
 		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "accounts_reveal"},
 		// Accounts: read and write are the module's; reveal is a capability the
 		// MCP subject is never granted, which is how "an agent cannot read a
diff --git a/internal/app/wiring.go b/internal/app/wiring.go
index 8744b1c6297afe46d3e078763a8733ca0205d930..1ab7c3be6cc267bc5eec1fbb6b648e8d4c2be6c6 100644
--- a/internal/app/wiring.go
+++ b/internal/app/wiring.go
@@ -95,6 +95,7 @@ func WireCredentialsAndWork(c *Container, db *sql.DB, vaultService *vault.Servic
 	// module that binds to answer for itself.
 	c.Accounts.RegisterUsage(apiUsage{api: c.APIClient})
 	c.Accounts.RegisterUsage(c.GitBindings)
+	c.Accounts.RegisterUsage(CodexAPIKey{C: c})
 
 	// One store for every credential DevKit holds. From here a new database,
 	// SSH or Kafka profile keeps its metadata in vault_items — where it syncs
diff --git a/internal/bridge/agenthub.go b/internal/bridge/agenthub.go
index 88301f14c2b90673b52190a41fb29863069045c6..72abd275cf99736adff52ecc1aef9b9478e768c8 100644
--- a/internal/bridge/agenthub.go
+++ b/internal/bridge/agenthub.go
@@ -5,6 +5,7 @@ import (
 	"encoding/json"
 	"errors"
 	"fmt"
+	"log"
 	"os"
 	"path/filepath"
 	"strings"
@@ -174,7 +175,16 @@ func (s *AppService) AgentHubSignOut(ctx context.Context, agent string) error {
 		if err != nil {
 			return nil, err
 		}
-		return nil, svc.SignOut(callCtx, agenthub.AgentType(agent))
+		if err := svc.SignOut(callCtx, agenthub.AgentType(agent)); err != nil {
+			return nil, err
+		}
+		// The note in Accounts goes with the sign-in. Failing to remove it is
+		// not a failed sign-out: the credential, the thing that matters, is
+		// already gone from the vendor's home.
+		if err := app.ForgetAgentSignIn(s.container, agenthub.AgentType(agent)); err != nil {
+			log.Printf("agenthub: sign-out: accounts note: %v", err)
+		}
+		return nil, nil
 	})
 	return err
 }
diff --git a/internal/bridge/errors.go b/internal/bridge/errors.go
index c0f4f38843539d94a24b975dbde743b074655498..a8d9bac52f1f03869996e0b36a679d2572244a38 100644
--- a/internal/bridge/errors.go
+++ b/internal/bridge/errors.go
@@ -2,6 +2,7 @@ package bridge
 
 import (
 	"context"
+	"devkit/internal/app"
 	"devkit/internal/capabilities"
 	"errors"
 	"io/fs"
@@ -136,6 +137,9 @@ func publicError(subject string, err error) *apperrors.AppError {
 	if errors.Is(err, notes.ErrInvalidColor) || errors.Is(err, notes.ErrTagTooLong) || errors.Is(err, notes.ErrTooManyTags) {
 		return apperrors.New(codeValidationFailed, err.Error(), true)
 	}
+	if errors.Is(err, app.ErrCodexAPIKeyAccount) {
+		return apperrors.New(codeValidationFailed, app.ErrCodexAPIKeyAccount.Error(), true)
+	}
 	if errors.Is(err, preferences.ErrInvalidTheme) {
 		return apperrors.New(codeValidationFailed, preferences.ErrInvalidTheme.Error(), true)
 	}
diff --git a/internal/bridge/preferences.go b/internal/bridge/preferences.go
index 18427ad9a4a612cfca3aeefa48a3086eeb1a9627..a2efd19b63b13b44a5934fa508f2d4926a6daf2c 100644
--- a/internal/bridge/preferences.go
+++ b/internal/bridge/preferences.go
@@ -5,6 +5,7 @@ import (
 	"errors"
 
 	"devkit/internal/agenthub"
+	"devkit/internal/app"
 	"devkit/internal/preferences"
 )
 
@@ -239,6 +240,41 @@ func (s *AppService) PreferencesSetAgentSandbox(settings preferences.AgentSandbo
 	return err
 }
 
+// PreferencesGetCodexAPIKeyAccount is the id of the Accounts token Codex
+// signs in with, or "" for its own ChatGPT sign-in. An id, never the key.
+func (s *AppService) PreferencesGetCodexAPIKeyAccount() (string, error) {
+	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "agent_codex_api_key", map[string]any{
+		"action": "get_codex_api_key_account",
+	}, func(context.Context) (any, error) {
+		if s.container.Preferences == nil {
+			return nil, errPreferencesUnavailable
+		}
+		return s.container.Preferences.CodexAPIKeyAccount()
+	})
+	if err != nil {
+		return "", err
+	}
+	return result.(string), nil
+}
+
+// PreferencesSetCodexAPIKeyAccount chooses the token Codex signs in with; ""
+// goes back to ChatGPT. The account is checked here, so one a session could
+// not use is refused now rather than at the next spawn. The audit line names
+// the account, never its value — this method never sees the value at all.
+func (s *AppService) PreferencesSetCodexAPIKeyAccount(accountID string) error {
+	audited := ""
+	if accountID != "" {
+		audited = recordID(accountID)
+	}
+	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "agent_codex_api_key", map[string]any{
+		"action":     "set_codex_api_key_account",
+		"account_id": audited,
+	}, func(context.Context) (any, error) {
+		return nil, app.CodexAPIKey{C: s.container}.Choose(accountID)
+	})
+	return err
+}
+
 func (s *AppService) PreferencesSetThemeFamily(family preferences.ThemeFamily) error {
 	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "theme_family", map[string]any{
 		"action": "set_theme_family",
diff --git a/internal/preferences/memory.go b/internal/preferences/memory.go
index 171a1a63f72939985a499c987a3b108312685ca9..667113a4cb07c7f80afd47401425ff6ac0a32fd4 100644
--- a/internal/preferences/memory.go
+++ b/internal/preferences/memory.go
@@ -15,6 +15,8 @@ type MemoryRepository struct {
 	hasAgentCheckpoints   bool
 	agentSandbox          AgentSandboxSettings
 	hasAgentSandbox       bool
+	codexAPIKeyAccount    string
+	hasCodexAPIKeyAccount bool
 
 	revealRequiresPassword    bool
 	hasRevealRequiresPassword bool
@@ -107,3 +109,17 @@ func (r *MemoryRepository) SetThemeFamily(family ThemeFamily) error {
 	r.hasFamily = true
 	return nil
 }
+
+func (r *MemoryRepository) GetCodexAPIKeyAccount() (string, bool, error) {
+	r.mu.RLock()
+	defer r.mu.RUnlock()
+	return r.codexAPIKeyAccount, r.hasCodexAPIKeyAccount, nil
+}
+
+func (r *MemoryRepository) SetCodexAPIKeyAccount(accountID string) error {
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	r.codexAPIKeyAccount = accountID
+	r.hasCodexAPIKeyAccount = true
+	return nil
+}
diff --git a/internal/preferences/preferences.go b/internal/preferences/preferences.go
index d42b1af3f6b183d3cb06d70d07d10055e12c7895..9806433851f651a6717b68e51cc5d7c6b83bdf8b 100644
--- a/internal/preferences/preferences.go
+++ b/internal/preferences/preferences.go
@@ -235,6 +235,8 @@ type Repository interface {
 	SetAgentCheckpoints(settings AgentCheckpointSettings) error
 	GetAgentSandbox() (AgentSandboxSettings, bool, error)
 	SetAgentSandbox(settings AgentSandboxSettings) error
+	GetCodexAPIKeyAccount() (string, bool, error)
+	SetCodexAPIKeyAccount(accountID string) error
 }
 
 type Service struct {
@@ -372,3 +374,22 @@ func (s *Service) AgentSandbox() (AgentSandboxSettings, error) {
 func (s *Service) SetAgentSandbox(settings AgentSandboxSettings) error {
 	return s.repo.SetAgentSandbox(settings.Normalized())
 }
+
+// CodexAPIKeyAccount is the Accounts id of the API key Codex sessions sign in
+// with, or "" for Codex's own ChatGPT sign-in. Only the id is kept here: the
+// key stays in Accounts and is resolved at spawn, so rotating it there is
+// all it takes.
+func (s *Service) CodexAPIKeyAccount() (string, error) {
+	id, _, err := s.repo.GetCodexAPIKeyAccount()
+	if err != nil {
+		return "", err
+	}
+	return id, nil
+}
+
+// SetCodexAPIKeyAccount stores the id; "" goes back to the ChatGPT sign-in.
+// Whether the id names a usable account is the caller's to check — this
+// package does not know accounts.
+func (s *Service) SetCodexAPIKeyAccount(accountID string) error {
+	return s.repo.SetCodexAPIKeyAccount(strings.TrimSpace(accountID))
+}
diff --git a/internal/preferences/sqlrepository.go b/internal/preferences/sqlrepository.go
index dbf9de23d72c6de75036162440a0822e7ceb159c..fc94bd6ad5715b9ab3740b0cc33e46f1d168d545 100644
--- a/internal/preferences/sqlrepository.go
+++ b/internal/preferences/sqlrepository.go
@@ -13,6 +13,7 @@ const (
 	agentNotificationsKey = "agent_notifications"
 	agentCheckpointsKey   = "agent_checkpoints"
 	agentSandboxKey       = "agent_sandbox"
+	codexAPIKeyAccountKey = "agent_codex_api_key_account"
 	revealPasswordKey     = "accounts_reveal_requires_password"
 )
 
@@ -141,6 +142,26 @@ func (r *SQLRepository) SetAgentSandbox(settings AgentSandboxSettings) error {
 	return err
 }
 
+func (r *SQLRepository) GetCodexAPIKeyAccount() (string, bool, error) {
+	var value string
+	err := r.db.QueryRowContext(context.Background(),
+		"SELECT value FROM app_preferences WHERE key = ?", codexAPIKeyAccountKey).Scan(&value)
+	if err == sql.ErrNoRows {
+		return "", false, nil
+	}
+	if err != nil {
+		return "", false, err
+	}
+	return value, true, nil
+}
+
+func (r *SQLRepository) SetCodexAPIKeyAccount(accountID string) error {
+	_, err := r.db.ExecContext(context.Background(),
+		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
+		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, codexAPIKeyAccountKey, accountID)
+	return err
+}
+
 type SQLRepository struct {
 	db *sql.DB
 }
diff --git a/main.go b/main.go
index 53e47fffbdeb0bdc397c28cedcff24bc6912220f..ec232812a582bef9434008bdfa8c76578f5a3c5e 100644
--- a/main.go
+++ b/main.go
@@ -329,7 +329,19 @@ func main() {
 		Runtime: func() (agentruntime.Resolved, error) {
 			return runtimes.Resolve(agentruntime.CodexCLI)
 		},
+		// An Accounts token chosen in Settings → Agents → Codex, resolved at
+		// spawn; none means Codex's own ChatGPT sign-in. Read through the
+		// container because Accounts is wired further down, once the database
+		// is open.
+		APIKey: app.CodexAPIKey{C: container}.Key,
 		OnSignInCompleted: func(r agenthub.SignInResult) {
+			if r.Success {
+				// Accounts notes the sign-in so its list answers "what am I
+				// signed in to". The credential stays in Codex's home.
+				if err := app.RecordAgentSignIn(container, r.Agent); err != nil {
+					log.Printf("agenthub: sign-in: accounts note: %v", err)
+				}
+			}
 			emitAppEvent("agenthub:signInCompleted", map[string]any{
 				"agent":   string(r.Agent),
 				"loginId": r.LoginID,
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/accounts/ ./internal/preferences/ ./internal/app/ ./internal/bridge/ ./internal/arch/ -count=1 && go build -tags server -o /dev/null . && go vet -tags server . ./internal/app/ ./internal/bridge/`
Expected: PASS, build và vet sạch.

- [ ] **Step 5: Commit**

```bash
git add internal/accounts/accounts_test.go internal/accounts/provider.go internal/accounts/service.go internal/app/agentcodex.go internal/app/agentcodex_test.go internal/app/container.go internal/app/wiring.go internal/bridge/agenthub.go internal/bridge/codex_api_key_test.go internal/bridge/errors.go internal/bridge/preferences.go internal/preferences/memory.go internal/preferences/preferences.go internal/preferences/sqlrepository.go internal/preferences/sqlrepository_test.go main.go
git commit -m "feat: Codex signs in with an Accounts token, and Accounts notes ChatGPT

Preferences keep the account id; app.CodexAPIKey resolves the token at
spawn, refuses an account a session could not use, and keeps Accounts
from deleting the one chosen. A successful ChatGPT sign-in leaves an
external \"Codex (ChatGPT)\" row, and signing out removes it.

<attribution>"
```

### Task 4: Regenerate bindings

**Model:** haiku

**Vì sao:** Sau T2–T3 bridge có bảy method mới và `agenthub.SignInFlow`/`SignInStatus`; enum `AgentType` còn thiếu `codex` từ 2c. Patch là đúng output của `wails3 generate bindings -clean=true -ts -i` (v3.0.0-beta.25) trên cây sau T3 — áp patch, không cần chạy wails3. Nếu có wails3 thì chạy lại lệnh đó phải cho `git status` sạch.

**Files:** Modify `frontend/bindings/devkit/internal/agenthub/index.ts`, `frontend/bindings/devkit/internal/agenthub/models.ts`, `frontend/bindings/devkit/internal/bridge/appservice.ts`

- [ ] **Step 1: Áp patch hiện thực** (`extract t4-impl`)

<!-- patch t4-impl -->
```diff
diff --git a/frontend/bindings/devkit/internal/agenthub/index.ts b/frontend/bindings/devkit/internal/agenthub/index.ts
index dfe7ea1066d1851b2f7e562cd7a7b3d3e144d598..b27dfa13bc36053203e938ab848a0f4e8a38dd1f 100644
--- a/frontend/bindings/devkit/internal/agenthub/index.ts
+++ b/frontend/bindings/devkit/internal/agenthub/index.ts
@@ -36,6 +36,8 @@ export type {
     RewindPreview,
     RewindRequest,
     RewindResult,
+    SignInFlow,
+    SignInStatus,
     TranscriptEntry,
     TranscriptPage
 } from "./models.js";
diff --git a/frontend/bindings/devkit/internal/agenthub/models.ts b/frontend/bindings/devkit/internal/agenthub/models.ts
index 28ff43440ca5cbf4e3cd8ea340beacfa770bd04e..4cfcbdf038cf7fb3102a5e37c94112795e7b8aee 100644
--- a/frontend/bindings/devkit/internal/agenthub/models.ts
+++ b/frontend/bindings/devkit/internal/agenthub/models.ts
@@ -32,6 +32,7 @@ export enum AgentType {
 
     AgentTypeMock = "mock",
     AgentTypeClaude = "claude",
+    AgentTypeCodex = "codex",
 };
 
 /**
@@ -664,6 +665,32 @@ export enum SessionStatus {
     SessionStatusClosed = "closed",
 };
 
+/**
+ * SignInFlow is a sign-in in progress: a page to open (browser) or a code to
+ * type on one (device code). LoginID names it to CancelSignIn.
+ */
+export interface SignInFlow {
+    "loginId": string;
+    "method": string;
+    "authUrl"?: string;
+    "verificationUrl"?: string;
+    "userCode"?: string;
+}
+
+/**
+ * SignInStatus is a vendor's sign-in as the in-app panel shows it.
+ */
+export interface SignInStatus {
+    "signedIn": boolean;
+
+    /**
+     * Method is "chatgpt" or "apiKey".
+     */
+    "method"?: string;
+    "email"?: string;
+    "plan"?: string;
+}
+
 /**
  * TranscriptEntry is one persisted row of a session's conversation.
  * 
diff --git a/frontend/bindings/devkit/internal/bridge/appservice.ts b/frontend/bindings/devkit/internal/bridge/appservice.ts
index fe6b7c5bf4dcca0f0f1077eab0743f54a7fa9ccd..ced6f0196846ef20831d1e275b55fb501b662828 100644
--- a/frontend/bindings/devkit/internal/bridge/appservice.ts
+++ b/frontend/bindings/devkit/internal/bridge/appservice.ts
@@ -228,6 +228,14 @@ export function AgentHubCancel(id: string): $CancellablePromise<void> {
     return $Call.ByID(952653156, id);
 }
 
+/**
+ * AgentHubCancelSignIn abandons the sign-in in progress. A login id that is
+ * no longer current is already over, and cancelling it is not an error.
+ */
+export function AgentHubCancelSignIn(agent: string, loginID: string): $CancellablePromise<void> {
+    return $Call.ByID(2776279486, agent, loginID);
+}
+
 /**
  * AgentHubCheckpointAvailability is what Settings shows beside the switch:
  * whether the machine can do this at all, and which projects it could not.
@@ -453,6 +461,46 @@ export function AgentHubSetMode(id: string, modeID: string): $CancellablePromise
     return $Call.ByID(2776585725, id, modeID);
 }
 
+/**
+ * AgentHubSignInStatus reads an agent's in-app sign-in ("codex"): ChatGPT
+ * with its email and plan, an API key, or none. Claude signs in in a terminal
+ * (AgentHubAuthMethods) and answers "not supported" here.
+ * 
+ * These five are the person's only. There is no MCP tool for any of them, and
+ * the service refuses a caller stamped as an agent or a terminal MCP client,
+ * so the capability below is not the only thing between an agent and the
+ * account every later session signs in with.
+ */
+export function AgentHubSignInStatus(agent: string): $CancellablePromise<agenthub$0.SignInStatus> {
+    return $Call.ByID(3016855882, agent);
+}
+
+/**
+ * AgentHubSignOut removes the vendor's own sign-in from DevKit's home for it.
+ * An API key chosen in Settings is not signed out here; it is unlinked there.
+ */
+export function AgentHubSignOut(agent: string): $CancellablePromise<void> {
+    return $Call.ByID(453233837, agent);
+}
+
+/**
+ * AgentHubStartBrowserSignIn starts the default sign-in: the vendor serves a
+ * login page and its callback from inside the jail, and the UI opens the page.
+ * How it ends arrives as an agenthub:signInCompleted event.
+ */
+export function AgentHubStartBrowserSignIn(agent: string): $CancellablePromise<agenthub$0.SignInFlow> {
+    return $Call.ByID(1534464996, agent);
+}
+
+/**
+ * AgentHubStartDeviceSignIn starts a sign-in by code, for a machine that
+ * cannot open a browser. Off by default per ChatGPT account; the UI says how
+ * to turn it on.
+ */
+export function AgentHubStartDeviceSignIn(agent: string): $CancellablePromise<agenthub$0.SignInFlow> {
+    return $Call.ByID(3146585458, agent);
+}
+
 /**
  * AgentHubStartSignIn runs one of an agent's sign-in methods in a new terminal
  * session and returns its id, for the Terminal module's tab to attach to. The
@@ -1653,6 +1701,14 @@ export function PreferencesGetAgentSandbox(): $CancellablePromise<preferences$0.
     return $Call.ByID(197792724);
 }
 
+/**
+ * PreferencesGetCodexAPIKeyAccount is the id of the Accounts token Codex
+ * signs in with, or "" for its own ChatGPT sign-in. An id, never the key.
+ */
+export function PreferencesGetCodexAPIKeyAccount(): $CancellablePromise<string> {
+    return $Call.ByID(1650256823);
+}
+
 export function PreferencesGetTheme(): $CancellablePromise<preferences$0.Theme> {
     return $Call.ByID(1151267161);
 }
@@ -1702,6 +1758,16 @@ export function PreferencesSetAgentSandbox(settings: preferences$0.AgentSandboxS
     return $Call.ByID(3347387056, settings);
 }
 
+/**
+ * PreferencesSetCodexAPIKeyAccount chooses the token Codex signs in with; ""
+ * goes back to ChatGPT. The account is checked here, so one a session could
+ * not use is refused now rather than at the next spawn. The audit line names
+ * the account, never its value — this method never sees the value at all.
+ */
+export function PreferencesSetCodexAPIKeyAccount(accountID: string): $CancellablePromise<void> {
+    return $Call.ByID(1822378715, accountID);
+}
+
 export function PreferencesSetTheme(theme: preferences$0.Theme): $CancellablePromise<void> {
     return $Call.ByID(1801576341, theme);
 }
```

- [ ] **Step 2: Chạy, xác nhận qua**

Run: `cd frontend && bun run build`
Expected: `tsc` + `vite build` qua (bindings mới chưa được dùng nên không có gì khác đổi).

- [ ] **Step 3: Commit**

```bash
git add frontend/bindings/devkit/internal/agenthub/index.ts frontend/bindings/devkit/internal/agenthub/models.ts frontend/bindings/devkit/internal/bridge/appservice.ts
git commit -m "chore(bindings): regenerate for in-app sign-in and the Codex API key

<attribution>"
```

### Task 5: Bảng mô tả vendor và wrapper bridge đăng nhập

**Model:** haiku

**Vì sao:** Spec §9 Giai đoạn 6: một bảng thay cho các chỗ hard-code `"claude"`. `modules/agenthub/vendors.ts`: `VendorInfo{id, name, runtimeId, runtimeName, signIn: terminal|inApp|none, beam}`, `vendorInfo(id)` — vendor lạ giữ tên của nó và không có gì để cài hay đăng nhập, **không** bao giờ nhận hàng của Claude. `bridge/agentSignIn.ts`: năm method, `onAgentSignInCompleted` (event `agenthub:signInCompleted`), `parseSignInResult`, `signInStatusLine`, get/save id API key. `AgentVendor` thêm `codex`, `CODEX_VENDOR`, `CODEX_AGENT`; `runtimeStatusLine(status, name)`.

**Files:** Create `frontend/src/bridge/agentSignIn.test.ts`, `frontend/src/bridge/agentSignIn.ts`, `frontend/src/modules/agenthub/vendors.test.ts`, `frontend/src/modules/agenthub/vendors.ts`; Modify `frontend/src/bridge/agentRuntime.test.ts`, `frontend/src/bridge/agentRuntime.ts`, `frontend/src/bridge/agenthub.ts`

- [ ] **Step 1: Áp patch test** (`extract t5-test`)

<!-- patch t5-test -->
```diff
diff --git a/frontend/src/bridge/agentRuntime.test.ts b/frontend/src/bridge/agentRuntime.test.ts
index 04690e85e78fad81684d1bf668a4155c0b5ba4a3..2e16f9cd95c5db2786eab3997d1268f09e57379d 100644
--- a/frontend/src/bridge/agentRuntime.test.ts
+++ b/frontend/src/bridge/agentRuntime.test.ts
@@ -1,8 +1,9 @@
 import { describe, expect, test } from "bun:test";
 import { AgentType } from "../../bindings/devkit/internal/agenthub/models";
-import { CLAUDE_VENDOR } from "./agenthub";
+import { CLAUDE_VENDOR, CODEX_VENDOR } from "./agenthub";
 import {
   CLAUDE_AGENT,
+  CODEX_AGENT,
   formatBytes,
   hintCommands,
   isAgentAuthRequiredError,
@@ -132,3 +133,20 @@ describe("the two Claude ids", () => {
     expect(CLAUDE_AGENT).not.toBe(CLAUDE_VENDOR);
   });
 });
+
+describe("the two Codex ids", () => {
+  test("a session is created with the vendor the backend registers", () => {
+    expect(CODEX_VENDOR).toBe(AgentType.AgentTypeCodex);
+  });
+
+  test("the runtime id is the agentruntime registry's", () => {
+    expect(CODEX_AGENT).toBe("codex");
+  });
+});
+
+describe("runtimeStatusLine names the agent", () => {
+  test("Codex's runtime is not called Claude Agent", () => {
+    const line = runtimeStatusLine(status({ agent: "codex", testedVersion: "0.147.0", current: installed("0.147.0") }), "Codex");
+    expect(line.headline).toBe("Codex 0.147.0 · tested");
+  });
+});
diff --git a/frontend/src/bridge/agentSignIn.test.ts b/frontend/src/bridge/agentSignIn.test.ts
new file mode 100644
index 0000000000000000000000000000000000000000..6da248bb7911df4c4adc2a4b993d398882373d4c
--- /dev/null
+++ b/frontend/src/bridge/agentSignIn.test.ts
@@ -0,0 +1,46 @@
+import { describe, expect, test } from "bun:test";
+import { parseSignInResult, signInStatusLine } from "./agentSignIn";
+
+describe("parseSignInResult", () => {
+  test("reads the event main.go emits", () => {
+    expect(parseSignInResult({ agent: "codex", loginId: "l1", success: true, error: "" })).toEqual({
+      agent: "codex",
+      loginId: "l1",
+      success: true,
+      error: "",
+    });
+  });
+
+  test("Wails may wrap the payload in an array", () => {
+    expect(parseSignInResult([{ agent: "codex", loginId: "l2", success: false, error: "Sign-in timed out." }])).toEqual({
+      agent: "codex",
+      loginId: "l2",
+      success: false,
+      error: "Sign-in timed out.",
+    });
+  });
+
+  test("anything else is not a result, and success must be said", () => {
+    expect(parseSignInResult(null)).toBeNull();
+    expect(parseSignInResult({ agent: "codex" })).toBeNull();
+    expect(parseSignInResult({ agent: "codex", loginId: "l3", success: "yes" })?.success).toBe(false);
+  });
+});
+
+describe("signInStatusLine", () => {
+  test("ChatGPT with what Codex said about the account", () => {
+    expect(signInStatusLine({ signedIn: true, method: "chatgpt", email: "a@b.c", plan: "plus" })).toBe(
+      "ChatGPT · a@b.c · plus",
+    );
+    expect(signInStatusLine({ signedIn: true, method: "chatgpt" })).toBe("ChatGPT");
+  });
+
+  test("an API key is named by its account", () => {
+    expect(signInStatusLine({ signedIn: true, method: "apiKey" }, "OpenAI work")).toBe("API key · OpenAI work");
+    expect(signInStatusLine({ signedIn: true, method: "apiKey" })).toBe("API key");
+  });
+
+  test("not signed in", () => {
+    expect(signInStatusLine({ signedIn: false })).toBe("Not signed in");
+  });
+});
diff --git a/frontend/src/modules/agenthub/vendors.test.ts b/frontend/src/modules/agenthub/vendors.test.ts
new file mode 100644
index 0000000000000000000000000000000000000000..6afe07ef63525bde958caf556534539c01c8144c
--- /dev/null
+++ b/frontend/src/modules/agenthub/vendors.test.ts
@@ -0,0 +1,40 @@
+import { describe, expect, test } from "bun:test";
+import { AgentType } from "../../../bindings/devkit/internal/agenthub/models";
+import { CLAUDE_AGENT, CODEX_AGENT } from "@/bridge/agentRuntime";
+import { CLAUDE_INFO, CODEX_INFO, SHIPPED_VENDORS, vendorInfo } from "./vendors";
+
+describe("vendor table", () => {
+  test("every shipped vendor is one the backend registers", () => {
+    const registered = Object.values(AgentType) as string[];
+    for (const id of SHIPPED_VENDORS) expect(registered).toContain(id);
+    expect(SHIPPED_VENDORS).not.toContain(AgentType.AgentTypeMock);
+  });
+
+  test("each vendor names the runtime DevKit installs for it", () => {
+    expect(vendorInfo("claude")).toBe(CLAUDE_INFO);
+    expect(vendorInfo("claude").runtimeId).toBe(CLAUDE_AGENT);
+    expect(vendorInfo("codex")).toBe(CODEX_INFO);
+    expect(vendorInfo("codex").runtimeId).toBe(CODEX_AGENT);
+  });
+
+  test("Claude signs in in a terminal, Codex inside DevKit", () => {
+    expect(vendorInfo("claude").signIn).toBe("terminal");
+    expect(vendorInfo("codex").signIn).toBe("inApp");
+  });
+
+  test("an unknown vendor is itself, never Claude", () => {
+    const mock = vendorInfo("mock");
+    expect(mock.name).toBe("Mock");
+    expect(mock.runtimeId).toBe("");
+    expect(mock.signIn).toBe("none");
+    expect(vendorInfo("").name).toBe("Agent");
+    expect(vendorInfo(undefined).signIn).toBe("none");
+  });
+
+  test("names are what the person reads", () => {
+    expect(vendorInfo("claude").name).toBe("Claude");
+    expect(vendorInfo("codex").name).toBe("Codex");
+    expect(vendorInfo("claude").beam).toBe("sunset");
+    expect(vendorInfo("codex").beam).toBe("ocean");
+  });
+});
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `cd frontend && bun test src/modules/agenthub/vendors.test.ts src/bridge/agentSignIn.test.ts src/bridge/agentRuntime.test.ts`
Expected: FAIL — `Cannot find module './vendors'`, `Cannot find module './agentSignIn'`, `Export named 'CODEX_VENDOR' not found`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t5-impl`)

<!-- patch t5-impl -->
```diff
diff --git a/frontend/src/bridge/agentRuntime.ts b/frontend/src/bridge/agentRuntime.ts
index 8c47f7189fc061cd09d25c7dfeefdd284363c134..2bffe9b8a1883b0ab7eed237a8467c343d88a86a 100644
--- a/frontend/src/bridge/agentRuntime.ts
+++ b/frontend/src/bridge/agentRuntime.ts
@@ -16,9 +16,12 @@ export type AgentRuntimeStatus = Omit<Status, "requirements"> & { requirements:
 
 export type ClaudeBinarySource = "bundled" | "machine";
 
-/** The only agent with a managed runtime today. */
+/** Claude's adapter, the runtime DevKit installs for the "claude" vendor. */
 export const CLAUDE_AGENT = "claude-acp";
 
+/** OpenAI's Codex CLI, run as `codex app-server` for the "codex" vendor. */
+export const CODEX_AGENT = "codex";
+
 function normalize(status: Status): AgentRuntimeStatus {
   return { ...status, requirements: status.requirements ?? [] };
 }
@@ -134,14 +137,17 @@ export function runtimeBlocked(status: AgentRuntimeStatus): boolean {
  * The one line that says where an agent stands. `latest` is set only when the
  * registry has something newer than both the tested and the installed version.
  */
-export function runtimeStatusLine(status: AgentRuntimeStatus): {
+export function runtimeStatusLine(
+  status: AgentRuntimeStatus,
+  name = "Claude Agent",
+): {
   headline: string;
   latest: string | null;
 } {
   const { current, testedVersion, latestVersion } = status;
   const headline = !current
     ? "Not installed"
-    : `Claude Agent ${current.version}${current.version === testedVersion ? " · tested" : " · not tested by DevKit"}`;
+    : `${name} ${current.version}${current.version === testedVersion ? " · tested" : " · not tested by DevKit"}`;
   const latest =
     latestVersion && latestVersion !== testedVersion && latestVersion !== current?.version
       ? `${latestVersion} available (not tested by DevKit)`
diff --git a/frontend/src/bridge/agentSignIn.ts b/frontend/src/bridge/agentSignIn.ts
new file mode 100644
index 0000000000000000000000000000000000000000..02b89ddf179fcd53978f2dba9336ced30beda735
--- /dev/null
+++ b/frontend/src/bridge/agentSignIn.ts
@@ -0,0 +1,86 @@
+import { Events } from "@wailsio/runtime";
+import { AppService } from "../../bindings/devkit/internal/bridge";
+import type { SignInFlow, SignInStatus } from "../../bindings/devkit/internal/agenthub/models";
+import { callBridge } from "./client";
+
+export type { SignInFlow, SignInStatus };
+
+/**
+ * Signing in inside DevKit, for a vendor that offers it (Codex). Claude's
+ * sign-in runs its own login in a terminal instead — bridge/agentRuntime.
+ *
+ * DevKit starts the vendor's login and shows its page or code; the credential
+ * lands in the vendor's own home, never here. There is no MCP tool for any of
+ * it, and the backend refuses an agent caller.
+ */
+
+/** How a sign-in ended, from the agenthub:signInCompleted event. */
+export type SignInResult = {
+  agent: string;
+  loginId: string;
+  success: boolean;
+  error: string;
+};
+
+const SIGN_IN_COMPLETED_EVENT = "agenthub:signInCompleted";
+
+export function agentHubSignInStatus(agent: string): Promise<SignInStatus> {
+  return callBridge(() => AppService.AgentHubSignInStatus(agent));
+}
+
+/** Starts the default sign-in; open `authUrl` for the person. */
+export function agentHubStartBrowserSignIn(agent: string): Promise<SignInFlow> {
+  return callBridge(() => AppService.AgentHubStartBrowserSignIn(agent));
+}
+
+/** Starts a sign-in by code; show `verificationUrl` and `userCode`. */
+export function agentHubStartDeviceSignIn(agent: string): Promise<SignInFlow> {
+  return callBridge(() => AppService.AgentHubStartDeviceSignIn(agent));
+}
+
+export function agentHubCancelSignIn(agent: string, loginId: string): Promise<void> {
+  return callBridge(() => AppService.AgentHubCancelSignIn(agent, loginId) as Promise<void>);
+}
+
+export function agentHubSignOut(agent: string): Promise<void> {
+  return callBridge(() => AppService.AgentHubSignOut(agent) as Promise<void>);
+}
+
+/** The Accounts token Codex signs in with, or "" for its ChatGPT sign-in. */
+export function getCodexApiKeyAccount(): Promise<string> {
+  return callBridge(async () => (await AppService.PreferencesGetCodexAPIKeyAccount()) ?? "");
+}
+
+/** Chooses that token by id; "" goes back to ChatGPT. The key never crosses. */
+export function saveCodexApiKeyAccount(accountId: string): Promise<void> {
+  return callBridge(() => AppService.PreferencesSetCodexAPIKeyAccount(accountId) as Promise<void>);
+}
+
+/** Reads the event's payload; anything malformed is not a result. */
+export function parseSignInResult(data: unknown): SignInResult | null {
+  const record = (Array.isArray(data) ? data[0] : data) as Record<string, unknown> | null;
+  if (!record || typeof record !== "object") return null;
+  if (typeof record.agent !== "string" || typeof record.loginId !== "string") return null;
+  return {
+    agent: record.agent,
+    loginId: record.loginId,
+    success: record.success === true,
+    error: typeof record.error === "string" ? record.error : "",
+  };
+}
+
+/** Every sign-in that ends — succeeded, failed, cancelled, timed out. Returns unsubscribe. */
+export function onAgentSignInCompleted(handler: (result: SignInResult) => void): () => void {
+  return Events.On(SIGN_IN_COMPLETED_EVENT, (ev) => {
+    const result = parseSignInResult(ev?.data);
+    if (result) handler(result);
+  });
+}
+
+/** "ChatGPT · a@b.c · plus", "API key", or "Not signed in". */
+export function signInStatusLine(status: SignInStatus, apiKeyLabel = ""): string {
+  if (!status.signedIn) return "Not signed in";
+  if (status.method === "apiKey") return apiKeyLabel ? `API key · ${apiKeyLabel}` : "API key";
+  const parts = ["ChatGPT", status.email, status.plan].filter((part): part is string => Boolean(part));
+  return parts.join(" · ");
+}
diff --git a/frontend/src/bridge/agenthub.ts b/frontend/src/bridge/agenthub.ts
index 586d683028becda5f45ffbdecd0badccc2f34e4f..3462a7e50bcbce60a428ee88e9b74652dccf06e2 100644
--- a/frontend/src/bridge/agenthub.ts
+++ b/frontend/src/bridge/agenthub.ts
@@ -19,7 +19,7 @@ import type {
 } from "../../bindings/devkit/internal/agenthub/models";
 import type { SessionUpdate } from "../modules/agenthub/types";
 
-export type AgentVendor = "claude";
+export type AgentVendor = "claude" | "codex";
 
 /**
  * The Claude agent as the hub knows it — the `agentType` a session is created
@@ -29,6 +29,13 @@ export type AgentVendor = "claude";
  */
 export const CLAUDE_VENDOR: AgentVendor = "claude";
 
+/**
+ * Codex as the hub knows it. Its runtime id (CODEX_AGENT) happens to be the
+ * same string; the vendor table (modules/agenthub/vendors) keeps the two jobs
+ * apart anyway, so nothing depends on that.
+ */
+export const CODEX_VENDOR: AgentVendor = "codex";
+
 export type SessionStatus =
   | "idle"
   | "running"
diff --git a/frontend/src/modules/agenthub/vendors.ts b/frontend/src/modules/agenthub/vendors.ts
new file mode 100644
index 0000000000000000000000000000000000000000..ffbbe69b393ff5090696940ed569d332cfd3f148
--- /dev/null
+++ b/frontend/src/modules/agenthub/vendors.ts
@@ -0,0 +1,67 @@
+import { CLAUDE_VENDOR, CODEX_VENDOR } from "@/bridge/agenthub";
+import { CLAUDE_AGENT, CODEX_AGENT } from "@/bridge/agentRuntime";
+
+/**
+ * How a vendor signs in. Claude runs its own login in a terminal the person
+ * watches; Codex signs in inside DevKit (a page or a code, then an event);
+ * anything else — the mock adapter — has nothing to sign in to.
+ */
+export type VendorSignIn = "terminal" | "inApp" | "none";
+
+/**
+ * What the UI needs to know about an agent it did not hard-code. Every place
+ * that used to compare against "claude" reads this instead, so a second
+ * vendor is a row here rather than a search through the module.
+ */
+export type VendorInfo = {
+  /** The agentType a session is created with. */
+  id: string;
+  /** What the person calls it: "Ask Claude…", "Codex isn't signed in". */
+  name: string;
+  /** The runtime DevKit installs for it (bridge/agentRuntime), or "" for none. */
+  runtimeId: string;
+  /** The runtime's name in Settings and the install panel. */
+  runtimeName: string;
+  signIn: VendorSignIn;
+  /** The streaming border's colour — Claude's is warm, like its mark. */
+  beam: "sunset" | "ocean";
+};
+
+export const CLAUDE_INFO: VendorInfo = {
+  id: CLAUDE_VENDOR,
+  name: "Claude",
+  runtimeId: CLAUDE_AGENT,
+  runtimeName: "Claude Agent",
+  signIn: "terminal",
+  beam: "sunset",
+};
+
+export const CODEX_INFO: VendorInfo = {
+  id: CODEX_VENDOR,
+  name: "Codex",
+  runtimeId: CODEX_AGENT,
+  runtimeName: "Codex",
+  signIn: "inApp",
+  beam: "ocean",
+};
+
+const KNOWN: Record<string, VendorInfo> = {
+  [CLAUDE_INFO.id]: CLAUDE_INFO,
+  [CODEX_INFO.id]: CODEX_INFO,
+};
+
+/** The vendors DevKit ships, in the order the picker offers them. */
+export const SHIPPED_VENDORS: readonly string[] = [CLAUDE_INFO.id, CODEX_INFO.id];
+
+/**
+ * A vendor's row. One DevKit does not know gets its id for a name and nothing
+ * to install or sign in to — never Claude's row, which is what defaulting to
+ * "claude" used to do.
+ */
+export function vendorInfo(id: string | null | undefined): VendorInfo {
+  const key = (id ?? "").trim();
+  const known = KNOWN[key.toLowerCase()];
+  if (known) return known;
+  const name = key ? key.charAt(0).toUpperCase() + key.slice(1) : "Agent";
+  return { id: key, name, runtimeId: "", runtimeName: name, signIn: "none", beam: "ocean" };
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `cd frontend && bun test src/modules/agenthub/vendors.test.ts src/bridge/agentSignIn.test.ts src/bridge/agentRuntime.test.ts && bunx tsc --noEmit -p .`
Expected: PASS, tsc sạch.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/bridge/agentRuntime.test.ts frontend/src/bridge/agentRuntime.ts frontend/src/bridge/agentSignIn.test.ts frontend/src/bridge/agentSignIn.ts frontend/src/bridge/agenthub.ts frontend/src/modules/agenthub/vendors.test.ts frontend/src/modules/agenthub/vendors.ts
git commit -m "feat(agenthub): a vendor table, and the sign-in bridge for the UI

Every place that compared against \"claude\" can read one row instead;
an unknown vendor keeps its own name and never borrows Claude's.

<attribution>"
```

### Task 6: Panel đăng nhập Codex

**Model:** haiku

**Vì sao:** Spec §6.1. `useCodexSignIn(onDone)` — cùng hình với `useClaudeSignIn` để Agent Hub thử lại hành động đang chờ. Trình duyệt là nút đầu (mở `authUrl` bằng `Browser.OpenURL`); "Use a code instead" hiện `userCode` + link + dòng `DEVICE_CODE_HINT` (bật device code trong ChatGPT → Settings → Security — tắt sẵn theo tài khoản, Codex không báo). Chỉ event của đúng `loginId` hook này bắt đầu mới tính (flow bị thay/huỷ cũng báo về). Rời panel giữa chừng thì huỷ flow (nó giữ cổng callback tới hết 15 phút). Không có ô nhập credential nào.

**Files:** Create `frontend/src/modules/agenthub/CodexSignIn.test.tsx`, `frontend/src/modules/agenthub/CodexSignIn.tsx`

- [ ] **Step 1: Áp patch test** (`extract t6-test`)

<!-- patch t6-test -->
```diff
diff --git a/frontend/src/modules/agenthub/CodexSignIn.test.tsx b/frontend/src/modules/agenthub/CodexSignIn.test.tsx
new file mode 100644
index 0000000000000000000000000000000000000000..d27a151080af8b97f40b2ccdc3ca7eb19ebb03ff
--- /dev/null
+++ b/frontend/src/modules/agenthub/CodexSignIn.test.tsx
@@ -0,0 +1,55 @@
+import { describe, expect, it } from "bun:test";
+import { renderToString } from "react-dom/server";
+import { CodexSignInPanel, DEVICE_CODE_HINT, codexSignInOutcome } from "./CodexSignIn";
+
+const noop = () => {};
+const panel = (over: Partial<Parameters<typeof CodexSignInPanel>[0]> = {}) =>
+  renderToString(
+    <CodexSignInPanel flow={null} starting="" outcome={null} startError="" onStart={noop} onCancel={noop} {...over} />,
+  );
+
+describe("Codex sign-in panel", () => {
+  it("offers the browser first and a code as the alternative", () => {
+    const html = panel();
+    expect(html).toContain("Sign in with ChatGPT");
+    expect(html).toContain("Use a code instead");
+    expect(html.indexOf("Sign in with ChatGPT")).toBeLessThan(html.indexOf("Use a code instead"));
+    // No field to type a credential into, ever.
+    expect(html).not.toContain("<input");
+  });
+
+  it("while the browser is open, says where to finish and how to stop", () => {
+    const html = panel({ flow: { loginId: "l1", method: "browser", authUrl: "https://auth.example/x" } });
+    expect(html).toContain("Finish signing in in your browser.");
+    expect(html).toContain("Open the page again");
+    expect(html).toContain("Cancel");
+    expect(html).not.toContain("Sign in with ChatGPT");
+  });
+
+  it("shows the code with the one step Codex never mentions", () => {
+    const html = panel({
+      flow: { loginId: "l2", method: "deviceCode", verificationUrl: "https://auth.example/device", userCode: "ABCD-1234" },
+    });
+    expect(html).toContain("ABCD-1234");
+    expect(html).toContain("Open the page");
+    expect(html).toContain("ChatGPT → Settings → Security");
+    expect(DEVICE_CODE_HINT).toContain("device code");
+  });
+
+  it("says how it ended", () => {
+    expect(panel({ outcome: { signedIn: true, message: "Signed in to Codex." } })).toContain("Signed in to Codex.");
+    expect(panel({ outcome: { signedIn: false, message: "Sign-in timed out." } })).toContain("Sign-in timed out.");
+    expect(panel({ startError: "Codex is not installed." })).toContain("Codex is not installed.");
+  });
+});
+
+describe("codexSignInOutcome", () => {
+  it("only a success retries what was waiting", () => {
+    expect(codexSignInOutcome({ success: true, error: "" }).signedIn).toBe(true);
+    expect(codexSignInOutcome({ success: false, error: "Sign-in cancelled." })).toEqual({
+      signedIn: false,
+      message: "Sign-in cancelled.",
+    });
+    expect(codexSignInOutcome({ success: false, error: "" }).message).toBe("Sign-in did not finish.");
+  });
+});
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `cd frontend && bun test src/modules/agenthub/CodexSignIn.test.tsx`
Expected: FAIL — `Cannot find module './CodexSignIn'`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t6-impl`)

<!-- patch t6-impl -->
```diff
diff --git a/frontend/src/modules/agenthub/CodexSignIn.tsx b/frontend/src/modules/agenthub/CodexSignIn.tsx
new file mode 100644
index 0000000000000000000000000000000000000000..62cc6d3dbaea5ad5a62605e357bf6bcb687da0d6
--- /dev/null
+++ b/frontend/src/modules/agenthub/CodexSignIn.tsx
@@ -0,0 +1,293 @@
+import { useEffect, useRef, useState } from "react";
+import { useQueryClient } from "@tanstack/react-query";
+import { Browser } from "@wailsio/runtime";
+import { Check, Copy, ExternalLink, KeyRound, Loader2, LogIn, X } from "lucide-react";
+
+import { Button } from "@/components/ui/button";
+import { CODEX_VENDOR } from "@/bridge/agenthub";
+import type { SignInOutcome } from "@/bridge/agentRuntime";
+import {
+  agentHubCancelSignIn,
+  agentHubStartBrowserSignIn,
+  agentHubStartDeviceSignIn,
+  onAgentSignInCompleted,
+  type SignInFlow,
+  type SignInResult,
+} from "@/bridge/agentSignIn";
+import { copyText } from "@/bridge/clipboard";
+import { errorMessage } from "@/bridge/errors";
+
+export const agentSignInStatusQueryKey = (agent: string) => ["agentSignInStatus", agent] as const;
+
+export type CodexSignInMethod = "browser" | "deviceCode";
+
+/**
+ * What the person has to do before a code works. Device code sign-in is off
+ * by default for every ChatGPT account, and Codex does not say so: it hands
+ * out a code as usual and the refusal appears only on OpenAI's page. So the
+ * UI says it, next to the code, every time.
+ */
+export const DEVICE_CODE_HINT =
+  "Turn on device code sign-in first: ChatGPT → Settings → Security → device code authorization for Codex.";
+
+/** What a finished sign-in means for the action waiting on it. */
+export function codexSignInOutcome(result: Pick<SignInResult, "success" | "error">): SignInOutcome {
+  if (result.success) return { signedIn: true, message: "Signed in to Codex." };
+  return { signedIn: false, message: result.error || "Sign-in did not finish." };
+}
+
+/**
+ * Sign in to Codex, as a state its owner keeps — the same shape as
+ * useClaudeSignIn, so Agent Hub can retry the action that needed it once the
+ * sign-in lands.
+ *
+ * The flow runs in the backend (a short-lived Codex in the jail, holding the
+ * login page's callback port) and ends with an agenthub:signInCompleted event.
+ * Only the event of the flow this hook started counts: a flow it replaced or
+ * cancelled reports too, and must not read as this one finishing.
+ */
+export function useCodexSignIn(onDone?: (outcome: SignInOutcome) => void) {
+  const client = useQueryClient();
+  const [flow, setFlow] = useState<SignInFlow | null>(null);
+  const [starting, setStarting] = useState<CodexSignInMethod | "">("");
+  const [outcome, setOutcome] = useState<SignInOutcome | null>(null);
+  const [startError, setStartError] = useState("");
+  // Read by the event handler and by cleanup, which see neither render's state.
+  const flowRef = useRef<SignInFlow | null>(null);
+  const onDoneRef = useRef(onDone);
+  onDoneRef.current = onDone;
+
+  useEffect(
+    () =>
+      onAgentSignInCompleted((result) => {
+        const current = flowRef.current;
+        if (result.agent !== CODEX_VENDOR || !current || result.loginId !== current.loginId) return;
+        flowRef.current = null;
+        setFlow(null);
+        const next = codexSignInOutcome(result);
+        setOutcome(next);
+        void client.invalidateQueries({ queryKey: agentSignInStatusQueryKey(CODEX_VENDOR) });
+        onDoneRef.current?.(next);
+      }),
+    [client],
+  );
+
+  // A flow nobody is looking at any more holds the callback port until it
+  // times out. Leaving withdraws it.
+  useEffect(
+    () => () => {
+      const current = flowRef.current;
+      flowRef.current = null;
+      if (current) void agentHubCancelSignIn(CODEX_VENDOR, current.loginId).catch(() => {});
+    },
+    [],
+  );
+
+  async function start(method: CodexSignInMethod) {
+    setStarting(method);
+    setOutcome(null);
+    setStartError("");
+    try {
+      const next =
+        method === "browser"
+          ? await agentHubStartBrowserSignIn(CODEX_VENDOR)
+          : await agentHubStartDeviceSignIn(CODEX_VENDOR);
+      flowRef.current = next;
+      setFlow(next);
+      if (next.authUrl) void Browser.OpenURL(next.authUrl);
+    } catch (e) {
+      setStartError(errorMessage(e));
+    } finally {
+      setStarting("");
+    }
+  }
+
+  async function cancel() {
+    const current = flowRef.current;
+    if (!current) return;
+    flowRef.current = null;
+    setFlow(null);
+    try {
+      await agentHubCancelSignIn(CODEX_VENDOR, current.loginId);
+    } catch (e) {
+      setStartError(errorMessage(e));
+    }
+  }
+
+  return { flow, starting, outcome, startError, start, cancel };
+}
+
+export type CodexSignInController = ReturnType<typeof useCodexSignIn>;
+
+/**
+ * Codex's sign-in: ChatGPT in the browser by default, a code for a machine
+ * that cannot open one. There is no credential field: the page is OpenAI's,
+ * the token lands in the Codex home only DevKit uses, and DevKit sees whether
+ * it worked.
+ */
+export function CodexSignIn({
+  signIn,
+  onStart,
+}: {
+  signIn: CodexSignInController;
+  /** Called as a sign-in starts, e.g. to get a dialog out of its way. */
+  onStart?: () => void;
+}) {
+  return (
+    <CodexSignInPanel
+      flow={signIn.flow}
+      starting={signIn.starting}
+      outcome={signIn.outcome}
+      startError={signIn.startError}
+      onStart={(method) => {
+        void signIn.start(method);
+        onStart?.();
+      }}
+      onCancel={() => void signIn.cancel()}
+    />
+  );
+}
+
+/** The panel itself, stateless so every state renders the same in a test. */
+export function CodexSignInPanel({
+  flow,
+  starting,
+  outcome,
+  startError,
+  onStart,
+  onCancel,
+}: {
+  flow: SignInFlow | null;
+  starting: CodexSignInMethod | "";
+  outcome: SignInOutcome | null;
+  startError: string;
+  onStart: (method: CodexSignInMethod) => void;
+  onCancel: () => void;
+}) {
+  return (
+    <div data-testid="codex-sign-in" className="flex w-full flex-col gap-2 text-left">
+      {flow ? (
+        <CodexSignInWaiting flow={flow} onCancel={onCancel} />
+      ) : (
+        <>
+          <div className="flex flex-wrap items-center gap-2">
+            <Button
+              type="button"
+              size="sm"
+              disabled={Boolean(starting)}
+              data-testid="codex-sign-in-browser"
+              onClick={() => onStart("browser")}
+            >
+              {starting === "browser" ? <Loader2 className="animate-spin" /> : <LogIn />}
+              Sign in with ChatGPT
+            </Button>
+            <Button
+              type="button"
+              variant="ghost"
+              size="sm"
+              disabled={Boolean(starting)}
+              data-testid="codex-sign-in-code"
+              onClick={() => onStart("deviceCode")}
+            >
+              {starting === "deviceCode" ? <Loader2 className="animate-spin" /> : <KeyRound />}
+              Use a code instead
+            </Button>
+          </div>
+          <p className="text-caption-11 text-text-tertiary">
+            Codex&apos;s own ChatGPT login, into a Codex home only DevKit uses — your ~/.codex is
+            left alone. DevKit never sees your password.
+          </p>
+        </>
+      )}
+      {outcome?.signedIn ? (
+        <p className="flex items-center gap-1.5 text-body-12 text-text-primary">
+          <Check className="size-4 shrink-0 text-status-success" />
+          {outcome.message}
+        </p>
+      ) : outcome ? (
+        <p className="text-body-12 text-status-danger">{outcome.message}</p>
+      ) : null}
+      {startError ? <p className="text-body-12 text-status-danger">{startError}</p> : null}
+    </div>
+  );
+}
+
+function CodexSignInWaiting({ flow, onCancel }: { flow: SignInFlow; onCancel: () => void }) {
+  const [copied, setCopied] = useState(false);
+  const cancel = (
+    <Button type="button" variant="ghost" size="sm" data-testid="codex-sign-in-cancel" onClick={onCancel}>
+      <X />
+      Cancel
+    </Button>
+  );
+
+  if (flow.userCode) {
+    return (
+      <div data-testid="codex-sign-in-code-flow" className="flex flex-col gap-2">
+        <p className="text-body-12 text-text-secondary">
+          Open the page, sign in, and enter this code:
+        </p>
+        <div className="flex flex-wrap items-center gap-2">
+          <code
+            data-testid="codex-sign-in-user-code"
+            className="rounded-md border border-border-subtle bg-bg-canvas px-2.5 py-1 text-code-12 tracking-widest text-text-primary"
+          >
+            {flow.userCode}
+          </code>
+          <Button
+            type="button"
+            variant="outline"
+            size="sm"
+            onClick={() => {
+              copyText(flow.userCode ?? "")
+                .then(() => {
+                  setCopied(true);
+                  window.setTimeout(() => setCopied(false), 1500);
+                })
+                // Clipboard can be unavailable in a webview; the code is selectable.
+                .catch(() => {});
+            }}
+          >
+            {copied ? <Check className="text-status-success" /> : <Copy />}
+            {copied ? "Copied" : "Copy"}
+          </Button>
+          {flow.verificationUrl ? (
+            <Button
+              type="button"
+              size="sm"
+              onClick={() => void Browser.OpenURL(flow.verificationUrl ?? "")}
+            >
+              <ExternalLink />
+              Open the page
+            </Button>
+          ) : null}
+          {cancel}
+        </div>
+        <p className="text-caption-11 text-text-tertiary">{DEVICE_CODE_HINT}</p>
+      </div>
+    );
+  }
+
+  return (
+    <div data-testid="codex-sign-in-browser-flow" className="flex flex-col gap-2">
+      <p className="flex items-center gap-1.5 text-body-12 text-text-secondary">
+        <Loader2 className="size-3.5 animate-spin" />
+        Finish signing in in your browser.
+      </p>
+      <div className="flex flex-wrap items-center gap-2">
+        {flow.authUrl ? (
+          <Button
+            type="button"
+            variant="outline"
+            size="sm"
+            onClick={() => void Browser.OpenURL(flow.authUrl ?? "")}
+          >
+            <ExternalLink />
+            Open the page again
+          </Button>
+        ) : null}
+        {cancel}
+      </div>
+    </div>
+  );
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `cd frontend && bun test src/modules/agenthub/CodexSignIn.test.tsx && bunx tsc --noEmit -p .`
Expected: PASS, tsc sạch.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/agenthub/CodexSignIn.test.tsx frontend/src/modules/agenthub/CodexSignIn.tsx
git commit -m "feat(agenthub): Codex sign-in panel — browser first, a code if not

The code comes with the step Codex never mentions: device code sign-in
is off per ChatGPT account until turned on under Settings → Security.

<attribution>"
```

### Task 7: Agent Hub đọc bảng vendor

**Model:** sonnet

**Vì sao:** Thay các chỗ hard-code trong `AgentHubView`, `AgentComposer` (tên vendor, màu viền), `AgentHubContent`, `AgentRail`, `TranscriptStream`. Bản nháp hỏi runtime của **vendor đang chọn** (`draftRuntime`), panel cài đặt thành `AgentRuntimeSetup` (đổi tên từ `ClaudeAgentSetup`, tên theo vendor). `createFailure(vendor, error)` (hàm thuần, có test) quyết: re-đọc runtime nào, panel cài đã nói chưa, có hiện đăng nhập + thử lại không, có gợi ý sandbox không. `signInFor(vendor)`: Claude → `ClaudeSignIn`, Codex → `CodexSignIn`; cả hai dùng chung `retryAfterSignIn` (authRetry cho Codex). Action `login` của vendor failure: Claude một phương thức thì chạy luôn, còn lại hiện panel.

**Files:** Create `frontend/src/modules/agenthub/AgentRuntimeSetup.tsx`, `frontend/src/modules/agenthub/createFailure.test.ts`; Modify `frontend/src/modules/agenthub/AgentHubView.tsx`, `frontend/src/modules/agenthub/surfaces/AgentComposer.tsx`, `frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx`, `frontend/src/modules/agenthub/surfaces/AgentRail.tsx`, `frontend/src/modules/agenthub/surfaces/TranscriptStream.tsx`; Delete `frontend/src/modules/agenthub/ClaudeAgentSetup.tsx`

- [ ] **Step 1: Áp patch test** (`extract t7-test`)

<!-- patch t7-test -->
```diff
diff --git a/frontend/src/modules/agenthub/createFailure.test.ts b/frontend/src/modules/agenthub/createFailure.test.ts
new file mode 100644
index 0000000000000000000000000000000000000000..cc4f6892f6fe0e286bda54bdb76a7be9607c3fe2
--- /dev/null
+++ b/frontend/src/modules/agenthub/createFailure.test.ts
@@ -0,0 +1,36 @@
+import { describe, expect, it } from "bun:test";
+import { createFailure } from "./AgentHubView";
+import { AppError } from "../../bridge/errors";
+
+const coded = (code: string, message = "x") => new AppError({ code, message, recoverable: true });
+
+describe("a failed create, per vendor", () => {
+  it("a signed-out Codex shows Codex's sign-in and retries, like Claude", () => {
+    for (const vendor of ["claude", "codex"]) {
+      const failure = createFailure(vendor, coded("agent_auth_required"));
+      expect(failure.signIn).toBe(true);
+      expect(failure.refreshRuntime).toBe("");
+    }
+  });
+
+  it("an agent with nothing to sign in to gets the error card", () => {
+    expect(createFailure("mock", coded("agent_auth_required")).signIn).toBe(false);
+  });
+
+  it("not installed re-reads that vendor's runtime, not Claude's", () => {
+    const codex = createFailure("codex", coded("agent_not_installed"));
+    expect(codex.refreshRuntime).toBe("codex");
+    expect(codex.shownByInstallPanel).toBe(true);
+    expect(createFailure("claude", coded("agent_not_installed")).refreshRuntime).toBe("claude-acp");
+    const requirement = createFailure("codex", coded("runtime_requirement"));
+    expect(requirement.refreshRuntime).toBe("codex");
+    expect(requirement.shownByInstallPanel).toBe(false);
+    // No runtime to install: the card says it.
+    expect(createFailure("mock", coded("agent_not_installed")).shownByInstallPanel).toBe(false);
+  });
+
+  it("the sandbox hint is for any jailed vendor", () => {
+    expect(createFailure("codex", coded("connection_failed", "Agent sandbox is unavailable.")).sandboxHint).toBe(true);
+    expect(createFailure("mock", coded("connection_failed", "Agent sandbox is unavailable.")).sandboxHint).toBe(false);
+  });
+});
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `cd frontend && bun test src/modules/agenthub/createFailure.test.ts`
Expected: FAIL — `Export named 'createFailure' not found in module '…/AgentHubView.tsx'`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t7-impl`)

<!-- patch t7-impl -->
```diff
diff --git a/frontend/src/modules/agenthub/AgentHubView.tsx b/frontend/src/modules/agenthub/AgentHubView.tsx
index 1935ec07f1ea8016fc58aba94cb280ce7a792ab2..e9c8aab69a9de087633d190e33c504f8c4525c2d 100644
--- a/frontend/src/modules/agenthub/AgentHubView.tsx
+++ b/frontend/src/modules/agenthub/AgentHubView.tsx
@@ -1,7 +1,6 @@
 import { useEffect, useMemo, useRef, useState } from "react";
 import { useQuery, useQueryClient } from "@tanstack/react-query";
 import {
-  CLAUDE_AGENT,
   agentRuntimeStatus,
   isAgentAuthRequiredError,
   isAgentNotInstalledError,
@@ -10,8 +9,10 @@ import {
 } from "../../bridge/agentRuntime";
 import { agentRuntimeQueryKey } from "../settings/AgentRuntimeSetting";
 import { requestSettings } from "../../workbench/settingsRequest";
-import { ClaudeAgentSetup } from "./ClaudeAgentSetup";
+import { AgentRuntimeSetup } from "./AgentRuntimeSetup";
 import { ClaudeSignIn, useClaudeSignIn } from "./ClaudeSignIn";
+import { CodexSignIn, useCodexSignIn } from "./CodexSignIn";
+import { SHIPPED_VENDORS, vendorInfo } from "./vendors";
 import type { SessionErrorCause } from "./surfaces";
 import {
   agentHubCancel,
@@ -32,7 +33,6 @@ import {
   isSandboxUnavailableError,
   sandboxUnavailableMessage,
   type AgentSession,
-  type AgentVendor,
   type AgentContextFile,
   type AgentImageAttachment,
 } from "../../bridge/agenthub";
@@ -73,9 +73,9 @@ import {
   AlertDialogTitle,
 } from "@/components/ui/alert-dialog";
 
-// Until the backend answers. Claude is the one agent DevKit ships; the mock
-// adapter is a test double and is not offered.
-const DEFAULT_VENDORS: AgentVendor[] = ["claude"];
+// Until the backend answers: the agents DevKit ships. The mock adapter is a
+// test double and is not offered.
+const DEFAULT_VENDORS: string[] = [...SHIPPED_VENDORS];
 
 /** A session to create, and the first message that is the reason for it. */
 type CreateParams = {
@@ -95,7 +95,7 @@ type TurnInput = {
   images: AgentImageAttachment[];
   refs: EntityRef[];
 };
-/** The action that failed on sign-in, retried once a login exits 0. */
+/** The action that failed on sign-in, retried once the sign-in succeeds. */
 type AuthRetry = { kind: "create"; params: CreateParams } | ({ kind: "turn" } & TurnInput);
 
 type ShortcutKey = Pick<
@@ -161,6 +161,26 @@ export async function renameAgentSession(
   if (session) upsertSession({ ...session, title });
 }
 
+/**
+ * What a failed create turns into, for the vendor that failed — read from the
+ * vendor table, so the install panel, the sign-in and the sandbox hint are
+ * Codex's as much as Claude's, and an agent with none of them (the mock) gets
+ * the plain error card.
+ */
+export function createFailure(vendor: string, error: unknown) {
+  const info = vendorInfo(vendor);
+  const runtime = info.runtimeId !== "" && (isAgentNotInstalledError(error) || isRuntimeRequirementError(error));
+  return {
+    /** The runtime whose status to re-read, or "". */
+    refreshRuntime: runtime ? info.runtimeId : "",
+    /** Nothing to add: the install panel already says it. */
+    shownByInstallPanel: runtime && isAgentNotInstalledError(error),
+    /** Show the vendor's own sign-in, and retry the create once it lands. */
+    signIn: info.signIn !== "none" && isAgentAuthRequiredError(error),
+    sandboxHint: info.runtimeId !== "" && isSandboxUnavailableError(error),
+  };
+}
+
 export function AgentHubView() {
   // Where the app is working, for a new draft to start in.
   const { folder: workFolder } = useModuleFolder("agenthub");
@@ -205,12 +225,12 @@ export function AgentHubView() {
   const [errorReason, setErrorReason] = useState("");
   // Not installed / missing Node are not crashes; the card offers Settings.
   const [errorCause, setErrorCause] = useState<SessionErrorCause | undefined>(undefined);
-  // What to do again once Claude is signed in. A failed create keeps its
-  // params here while the dialog steps aside for the terminal.
+  // What to do again once the agent is signed in. A failed create keeps its
+  // params here while the draft shows the sign-in instead of sending.
   const [authRetry, setAuthRetry] = useState<AuthRetry | null>(null);
   // The last turn sent, so Retry resends it: the composer is cleared on send.
   const lastTurn = useRef<TurnInput | null>(null);
-  const signIn = useClaudeSignIn((outcome) => {
+  const retryAfterSignIn = (outcome: { signedIn: boolean }) => {
     if (authRetry?.kind === "create") {
       // The draft is still on screen with its message, so a successful login
       // carries straight on: the session is created and the message sent.
@@ -218,14 +238,20 @@ export function AgentHubView() {
     } else if (authRetry?.kind === "turn" && outcome.signedIn) {
       void handleSend(authRetry);
     }
-  });
+  };
+  // One per way of signing in (vendors.ts): Claude's runs its login in a
+  // terminal, Codex's inside DevKit. Both retry the same way.
+  const claudeSignIn = useClaudeSignIn(retryAfterSignIn);
+  const codexSignIn = useCodexSignIn(retryAfterSignIn);
   const queryClient = useQueryClient();
-  // Asked while the draft has Claude picked, not after a send fails: the draft
-  // shows the install panel as soon as Claude is chosen and cannot start.
-  const claudeRuntime = useQuery({
-    queryKey: agentRuntimeQueryKey(CLAUDE_AGENT),
-    queryFn: () => agentRuntimeStatus(CLAUDE_AGENT),
-    enabled: draft !== null && draftAgent === CLAUDE_VENDOR,
+  // Asked while the draft has an agent picked, not after a send fails: the
+  // draft shows the install panel as soon as an agent that cannot start is
+  // chosen.
+  const draftVendor = vendorInfo(draftAgent);
+  const draftRuntime = useQuery({
+    queryKey: agentRuntimeQueryKey(draftVendor.runtimeId),
+    queryFn: () => agentRuntimeStatus(draftVendor.runtimeId),
+    enabled: draft !== null && draftVendor.runtimeId !== "",
   });
   // A vendor failure the user waved off. Keyed by id and revision: the agent
   // republishes a failure with a new revision when something about it changed,
@@ -469,20 +495,21 @@ export function AgentHubView() {
         );
       }
     } catch (e) {
-      if (isAgentNotInstalledError(e) || isRuntimeRequirementError(e)) {
+      const failure = createFailure(params.vendor, e);
+      if (failure.refreshRuntime) {
         // The status changed under the dialog (removed, Node uninstalled):
         // re-read it and the install panel takes the form's place.
-        void queryClient.invalidateQueries({ queryKey: agentRuntimeQueryKey(CLAUDE_AGENT) });
-        // The panel already says "not installed"; a requirement's reason stays.
-        if (isAgentNotInstalledError(e)) return;
+        void queryClient.invalidateQueries({ queryKey: agentRuntimeQueryKey(failure.refreshRuntime) });
       }
+      // The panel already says "not installed"; a requirement's reason stays.
+      if (failure.shownByInstallPanel) return;
       // The sign-in panel takes the form's place and says why.
-      if (params.vendor === "claude" && isAgentAuthRequiredError(e)) {
+      if (failure.signIn) {
         setAuthRetry({ kind: "create", params });
         return;
       }
       reportError(e);
-      if (params.vendor === "claude" && isSandboxUnavailableError(e)) {
+      if (failure.sandboxHint) {
         setSandboxHint(sandboxUnavailableMessage());
       }
     } finally {
@@ -737,11 +764,15 @@ export function AgentHubView() {
       return;
     }
     if (action === "login") {
-      const methods = signIn.methods.data ?? [];
+      const methods = claudeSignIn.methods.data ?? [];
       // One way in: take it. Several: the error card's panel names them, and
-      // picking for the user would be guessing.
-      if (methods.length === 1) signIn.start(methods[0]);
-      else setErrorCause("authRequired");
+      // picking for the user would be guessing. An agent that signs in
+      // inside DevKit shows its panel, where the browser is the first button.
+      if (vendorInfo(activeSession?.agentType).signIn === "terminal" && methods.length === 1) {
+        claudeSignIn.start(methods[0]);
+      } else {
+        setErrorCause("authRequired");
+      }
     }
   }
 
@@ -832,19 +863,32 @@ export function AgentHubView() {
     }
   }
 
+  /** The sign-in a vendor offers, wired to retry what was waiting on it. */
+  function signInFor(vendor: string) {
+    switch (vendorInfo(vendor).signIn) {
+      case "terminal":
+        return <ClaudeSignIn signIn={claudeSignIn} />;
+      case "inApp":
+        return <CodexSignIn signIn={codexSignIn} />;
+      default:
+        return undefined;
+    }
+  }
+
   // What stands between a draft and its first message, in the draft's own view.
   const draftBlocked =
-    draftAgent === CLAUDE_VENDOR && Boolean(claudeRuntime.data && runtimeBlocked(claudeRuntime.data));
+    draftVendor.runtimeId !== "" && Boolean(draftRuntime.data && runtimeBlocked(draftRuntime.data));
   const draftNotice = !draft ? null : draftBlocked ? (
-    <ClaudeAgentSetup onOpenSettings={() => requestSettings("agents")} />
+    <AgentRuntimeSetup vendor={draftAgent} onOpenSettings={() => requestSettings("agents")} />
   ) : authRetry?.kind === "create" ? (
-    <div data-testid="claude-sign-in-panel" className="flex w-full flex-col gap-3 rounded-sm bg-(--state-soft) p-4">
-      <p className="text-body-13-medium text-text-primary">Claude isn&apos;t signed in</p>
+    <div data-testid="agent-sign-in-panel" className="flex w-full flex-col gap-3 rounded-sm bg-(--state-soft) p-4">
+      <p className="text-body-13-medium text-text-primary">{`${draftVendor.name} isn't signed in`}</p>
       <p className="text-caption-11 text-text-tertiary">
-        Sign in in the terminal that opens. The session starts and your message is sent as soon as
-        the login finishes.
+        {draftVendor.signIn === "terminal"
+          ? "Sign in in the terminal that opens. The session starts and your message is sent as soon as the login finishes."
+          : "Sign in below. The session starts and your message is sent as soon as the sign-in finishes."}
       </p>
-      <ClaudeSignIn signIn={signIn} />
+      {signInFor(authRetry.params.vendor)}
     </div>
   ) : null;
 
@@ -1034,9 +1078,7 @@ export function AgentHubView() {
             onNewSession={() => void startNewDraft()}
             onOpenSettings={() => requestSettings("agents")}
             errorDetail={
-              errorCause === "authRequired" && activeSession?.agentType === "claude" ? (
-                <ClaudeSignIn signIn={signIn} />
-              ) : undefined
+              errorCause === "authRequired" && activeSession ? signInFor(activeSession.agentType) : undefined
             }
             attachments={activeSession ? attachments[activeSession.id] ?? [] : []}
             imageAttachments={draft ? draft.images : activeSession ? images[activeSession.id] ?? [] : []}
diff --git a/frontend/src/modules/agenthub/AgentRuntimeSetup.tsx b/frontend/src/modules/agenthub/AgentRuntimeSetup.tsx
new file mode 100644
index 0000000000000000000000000000000000000000..7279f3a41a650039af23718b5365981de1eb0cda
--- /dev/null
+++ b/frontend/src/modules/agenthub/AgentRuntimeSetup.tsx
@@ -0,0 +1,58 @@
+import { Download, X } from "lucide-react";
+
+import { Button } from "@/components/ui/button";
+import {
+  RuntimeInstallLog,
+  RuntimeRequirements,
+  useAgentRuntime,
+} from "@/modules/settings/AgentRuntimeSetting";
+import { vendorInfo } from "./vendors";
+
+/**
+ * What the draft shows instead of sending when the picked agent cannot be
+ * started: not installed, or installed but missing what its install needs
+ * (Node/npm). The install runs right here, with the same log as Settings →
+ * Agents. Any vendor with a runtime (vendors.ts) gets it, named for itself.
+ */
+export function AgentRuntimeSetup({ vendor, onOpenSettings }: { vendor: string; onOpenSettings: () => void }) {
+  const info = vendorInfo(vendor);
+  const rt = useAgentRuntime(info.runtimeId);
+  const s = rt.status;
+  if (!s) return null;
+  const unmet = s.requirements.filter((r) => !r.met);
+
+  return (
+    <div
+      data-testid="agent-runtime-setup"
+      className="flex w-full flex-col items-center gap-3 rounded-sm bg-(--state-soft) p-4 text-center"
+    >
+      <p className="text-body-13-medium text-text-primary">
+        {s.current ? `${info.runtimeName} can't start` : `${info.runtimeName} isn't installed`}
+      </p>
+      <p className="text-caption-11 text-text-tertiary">
+        {s.current
+          ? "Something it needs is missing from this machine."
+          : `DevKit installs the version it has tested (${s.testedVersion}) into its own folder — nothing goes on your PATH.`}
+      </p>
+      {unmet.length > 0 ? <RuntimeRequirements requirements={unmet} /> : null}
+      <div className="flex items-center gap-2">
+        {rt.installing ? (
+          <Button type="button" variant="outline" size="sm" disabled={!rt.canCancel} onClick={rt.cancel}>
+            <X />
+            Cancel install
+          </Button>
+        ) : !s.current ? (
+          <Button type="button" size="sm" disabled={rt.busy} onClick={() => rt.install()}>
+            <Download />
+            {`Install ${info.runtimeName}`}
+          </Button>
+        ) : null}
+        <Button type="button" variant="ghost" size="sm" onClick={onOpenSettings}>
+          Open agent settings
+        </Button>
+      </div>
+      {rt.error ? <p className="text-caption-11 text-status-danger">{rt.error}</p> : null}
+      <RuntimeInstallLog lines={rt.lines} installing={rt.installing} failed={Boolean(rt.error)} />
+    </div>
+  );
+}
diff --git a/frontend/src/modules/agenthub/ClaudeAgentSetup.tsx b/frontend/src/modules/agenthub/ClaudeAgentSetup.tsx
deleted file mode 100644
index 08565e8d1d9f973635abb902eb2c2dc840150237..0000000000000000000000000000000000000000
--- a/frontend/src/modules/agenthub/ClaudeAgentSetup.tsx
+++ /dev/null
@@ -1,55 +0,0 @@
-import { Download, X } from "lucide-react";
-
-import { Button } from "@/components/ui/button";
-import {
-  RuntimeInstallLog,
-  RuntimeRequirements,
-  useAgentRuntime,
-} from "@/modules/settings/AgentRuntimeSetting";
-
-/**
- * What the new-session dialog shows instead of its form when Claude cannot be
- * started: not installed, or installed but missing Node/npm. The install runs
- * right here, with the same log as Settings → Agents.
- */
-export function ClaudeAgentSetup({ onOpenSettings }: { onOpenSettings: () => void }) {
-  const rt = useAgentRuntime();
-  const s = rt.status;
-  if (!s) return null;
-  const unmet = s.requirements.filter((r) => !r.met);
-
-  return (
-    <div
-      data-testid="claude-agent-setup"
-      className="flex w-full flex-col items-center gap-3 rounded-sm bg-(--state-soft) p-4 text-center"
-    >
-      <p className="text-body-13-medium text-text-primary">
-        {s.current ? "Claude Agent can't start" : "Claude Agent isn't installed"}
-      </p>
-      <p className="text-caption-11 text-text-tertiary">
-        {s.current
-          ? "Something it needs is missing from this machine."
-          : `DevKit installs the version it has tested (${s.testedVersion}) into its own folder — nothing goes on your PATH.`}
-      </p>
-      {unmet.length > 0 ? <RuntimeRequirements requirements={unmet} /> : null}
-      <div className="flex items-center gap-2">
-        {rt.installing ? (
-          <Button type="button" variant="outline" size="sm" disabled={!rt.canCancel} onClick={rt.cancel}>
-            <X />
-            Cancel install
-          </Button>
-        ) : !s.current ? (
-          <Button type="button" size="sm" disabled={rt.busy} onClick={() => rt.install()}>
-            <Download />
-            Install Claude Agent
-          </Button>
-        ) : null}
-        <Button type="button" variant="ghost" size="sm" onClick={onOpenSettings}>
-          Open agent settings
-        </Button>
-      </div>
-      {rt.error ? <p className="text-caption-11 text-status-danger">{rt.error}</p> : null}
-      <RuntimeInstallLog lines={rt.lines} installing={rt.installing} failed={Boolean(rt.error)} />
-    </div>
-  );
-}
diff --git a/frontend/src/modules/agenthub/surfaces/AgentComposer.tsx b/frontend/src/modules/agenthub/surfaces/AgentComposer.tsx
index c8e349151b2605f8e6056cc5728f10d198116952..fe026e07c83aa1a522cc29a0bea316138b904a69 100644
--- a/frontend/src/modules/agenthub/surfaces/AgentComposer.tsx
+++ b/frontend/src/modules/agenthub/surfaces/AgentComposer.tsx
@@ -43,6 +43,7 @@ import type { AvailableCommand, ConfigOption, ConfigSelectOption, SessionGoal, S
 import type { QueuedAgentPrompt } from "@/stores/agentSessions";
 import type { AgentImageAttachment } from "@/bridge/agenthub";
 import type { EntityRef } from "@/workbench/intents";
+import { vendorInfo } from "../vendors";
 
 export type ComposerSize = "wide" | "rail";
 export type ComposerState = "idle" | "streaming" | "attached";
@@ -336,7 +337,7 @@ export function AgentComposer({
   goal,
   onSetGoal,
   turnCount = 0,
-  vendor = "claude",
+  vendor = "",
   attachedFiles = [],
   onRemoveAttachment,
   imageAttachments = [],
@@ -357,7 +358,7 @@ export function AgentComposer({
   onBrowseContext,
   onCompact,
   agents = [],
-  agent = "claude",
+  agent = "",
   onAgentChange,
   commands = [],
   contextFiles = [],
@@ -679,7 +680,7 @@ export function AgentComposer({
                   className="gap-1.5 px-2 text-text-secondary"
                 >
                   <VendorIcon vendor={agent} className="size-3.5" />
-                  <span className="text-body-13 capitalize">{agent}</span>
+                  <span className="text-body-13">{vendorInfo(agent).name}</span>
                   <ChevronDown className="size-3 text-text-tertiary" aria-hidden="true" />
                 </Button>
               </DropdownMenuTrigger>
@@ -693,7 +694,7 @@ export function AgentComposer({
                     detail={option === agent ? <Check className="size-3" aria-hidden="true" /> : undefined}
                     onSelect={() => onAgentChange(option)}
                   >
-                    <span className="capitalize">{option}</span>
+                    <span>{vendorInfo(option).name}</span>
                   </DropdownMenuItem>
                 ))}
               </DropdownMenuContent>
@@ -836,7 +837,7 @@ export function AgentComposer({
           <BorderBeam
             active={isStreaming && !reduce}
             size="sm"
-            colorVariant={vendor.toLowerCase().startsWith("claude") ? "sunset" : "ocean"}
+            colorVariant={vendorInfo(vendor).beam}
             theme={theme}
             className="shrink-0 rounded-full"
           >
diff --git a/frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx b/frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx
index af7c90097a6716658f1eeaa06203f82221431c64..daa3628ee6404817d90ea5cd1fb20df9a574716f 100644
--- a/frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx
+++ b/frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx
@@ -33,6 +33,7 @@ import { AgentSessionFailure } from "./AgentSessionFailure";
 import { AgentPermissionRequest } from "./AgentPermissionRequest";
 import { AgentQuestionCard } from "./AgentQuestionCard";
 import { PermissionPresence } from "./permissionCard";
+import { vendorInfo } from "../vendors";
 
 export type AgentHubContentState = "Conversation" | "Empty" | "Error";
 
@@ -251,7 +252,7 @@ export function AgentHubContent({
   workspace = null,
   onChooseWorkspace,
   agents = [],
-  agent = "claude",
+  agent = "",
   onAgentChange,
   draftNotice,
   sendDisabled = false,
@@ -319,7 +320,7 @@ export function AgentHubContent({
   const displayTitle = draft
     ? "New session"
     : session?.title || (session ? `Session ${session.id.slice(0, 8)}` : "");
-  const vendor = draft ? agent : session?.agentType || "claude";
+  const vendor = draft ? agent : session?.agentType ?? "";
 
   return (
     <main
@@ -561,7 +562,7 @@ export function AgentHubContent({
               vendor={vendor}
               // A draft is not named yet; its placeholder addresses the agent
               // that will answer — "Ask Claude…", not "Ask New session…".
-              agentName={draft ? agent.charAt(0).toUpperCase() + agent.slice(1) : displayTitle}
+              agentName={draft ? vendorInfo(agent).name : displayTitle}
               // A closed session says what sending will do, since it does one
               // thing more than usual: it wakes the agent's process back up.
               placeholder={composerPlaceholder(session?.status)}
diff --git a/frontend/src/modules/agenthub/surfaces/AgentRail.tsx b/frontend/src/modules/agenthub/surfaces/AgentRail.tsx
index 76a1a0622326d2bc0f86252b68a22100f5d2d063..c3220d3d7a6de6ed878a55385ac4899f80323668 100644
--- a/frontend/src/modules/agenthub/surfaces/AgentRail.tsx
+++ b/frontend/src/modules/agenthub/surfaces/AgentRail.tsx
@@ -194,7 +194,7 @@ export function AgentRail({
   const displayTitle =
     activeSession?.title ||
     (activeSession ? `Session ${activeSession.id.slice(0, 8)}` : "");
-  const vendor = activeSession?.agentType || "claude";
+  const vendor = activeSession?.agentType ?? "";
 
   const isPendingForActiveSession =
     pendingPermission && activeSession && pendingPermission.sessionId === activeSession.id;
diff --git a/frontend/src/modules/agenthub/surfaces/TranscriptStream.tsx b/frontend/src/modules/agenthub/surfaces/TranscriptStream.tsx
index 91f7f951024808a61af19149359acb77e9a20664..bb105beffe03ef4cb401f9ef39b5ff5eb95d9ba2 100644
--- a/frontend/src/modules/agenthub/surfaces/TranscriptStream.tsx
+++ b/frontend/src/modules/agenthub/surfaces/TranscriptStream.tsx
@@ -14,6 +14,7 @@ import { AgentTurn } from "./AgentTurn";
 import { EMPTY_CHECKPOINTS, type CheckpointIndex } from "../checkpoints";
 import type { FileDiff } from "../../../../bindings/devkit/internal/agenthub/models";
 import { TranscriptTicks, ticksOf } from "./TranscriptTicks";
+import { vendorInfo } from "../vendors";
 
 export type TranscriptStreamProps = {
   items: TranscriptItem[];
@@ -135,7 +136,7 @@ export const TRANSCRIPT_OVERSCAN = 6;
 export function TranscriptStream({
   items,
   streamKey,
-  vendor = "Claude",
+  vendor = "",
   checkpoints = EMPTY_CHECKPOINTS,
   loadFileDiff,
   onRewind,
@@ -587,7 +588,7 @@ export function TranscriptStream({
         onOpenChange={(next) => {
           if (!next) setInspecting(null);
         }}
-        agentName={vendor}
+        agentName={vendorInfo(vendor).name}
         stopReason={metaField(inspecting, "stopReason")}
         cost={metaNumber(inspecting, "cost")}
         inputTokens={metaNumber(inspecting, "inputTokens")}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `cd frontend && bun test src/modules/agenthub/ && bunx tsc --noEmit -p .`
Expected: PASS, tsc sạch; `grep -rn '"claude"' src/modules/agenthub --include=*.tsx | grep -v test` không còn so sánh nào.

- [ ] **Step 5: Commit**

```bash
git add -A -- frontend/src/modules/agenthub/AgentHubView.tsx frontend/src/modules/agenthub/AgentRuntimeSetup.tsx frontend/src/modules/agenthub/ClaudeAgentSetup.tsx frontend/src/modules/agenthub/createFailure.test.ts frontend/src/modules/agenthub/surfaces/AgentComposer.tsx frontend/src/modules/agenthub/surfaces/AgentHubContent.tsx frontend/src/modules/agenthub/surfaces/AgentRail.tsx frontend/src/modules/agenthub/surfaces/TranscriptStream.tsx
git commit -m "feat(agenthub): the hub reads the vendor table instead of \"claude\"

The draft asks the picked vendor's runtime, a failed create shows that
vendor's install panel or sign-in and retries after it, and the
composer names the agent and colours it from the table.

<attribution>"
```

### Task 8: Card Codex trong Settings → Agents

**Model:** sonnet

**Vì sao:** Spec §6.3. `AgentRuntimeSetting({vendor})` — Claude là mặc định; khối "CLAUDE CODE" chỉ cho Claude; binary native không có dòng SDK/Node; mục SIGN IN theo `signIn` của bảng. `CodexAccountSection` (trong `modules/agenthub`, cạnh sign-in nó bọc — như `ClaudeSignInSection`; đặt ở `settings/` thì `moduleBoundaryRatchet.test.ts` báo hai cạnh settings→agenthub mới): dòng trạng thái (`ChatGPT · email · plan`, `API key · <tên account>`, `Not signed in`), Sign out chỉ khi đang ChatGPT, panel đăng nhập khi chưa đăng nhập, chọn API key từ các account `token` dùng được (`codexApiKeyChoices`) hoặc "None — use the ChatGPT sign-in"; id đã chọn mà account không còn thì cảnh báo. `SettingsView` thêm nhóm CODEX.

**Files:** Create `frontend/src/modules/agenthub/CodexAccountSection.test.ts`, `frontend/src/modules/agenthub/CodexAccountSection.tsx`; Modify `frontend/src/modules/settings/AgentRuntimeSetting.tsx`, `frontend/src/modules/settings/SettingsView.tsx`

- [ ] **Step 1: Áp patch test** (`extract t8-test`)

<!-- patch t8-test -->
```diff
diff --git a/frontend/src/modules/agenthub/CodexAccountSection.test.ts b/frontend/src/modules/agenthub/CodexAccountSection.test.ts
new file mode 100644
index 0000000000000000000000000000000000000000..0212720f91612beaaedc52a96cba0136cc2f4c42
--- /dev/null
+++ b/frontend/src/modules/agenthub/CodexAccountSection.test.ts
@@ -0,0 +1,44 @@
+import { describe, expect, test } from "bun:test";
+import type { Account } from "@/bridge/accounts";
+import { CHATGPT_CHOICE, codexApiKeyChoices } from "./CodexAccountSection";
+
+function account(over: Partial<Account>): Account {
+  return {
+    id: "0123456789abcdef01234567",
+    provider: "openai",
+    kind: "token",
+    label: "OpenAI",
+    subject: "",
+    scopes: [],
+    env: [],
+    scopeOwner: "",
+    state: "valid",
+    expiresAt: "",
+    verifiedAt: "",
+    ref: "",
+    ...over,
+  };
+}
+
+describe("which accounts can be Codex's API key", () => {
+  test("a shared token with no environment restriction", () => {
+    const ok = account({ id: "a".repeat(24) });
+    const list = [
+      ok,
+      account({ id: "b".repeat(24), kind: "basic" }),
+      account({ id: "c".repeat(24), kind: "external", label: "Codex (ChatGPT)" }),
+      account({ id: "d".repeat(24), env: ["prod"] }),
+      account({ id: "e".repeat(24), kind: "secret", scopeOwner: "f".repeat(24) }),
+    ];
+    expect(codexApiKeyChoices(list)).toEqual([ok]);
+  });
+
+  test("any provider's token may be one: the key is what matters", () => {
+    expect(codexApiKeyChoices([account({ provider: "generic" })])).toHaveLength(1);
+  });
+
+  test("'none' is a value Radix accepts and no account id can be", () => {
+    expect(CHATGPT_CHOICE).not.toBe("");
+    expect(CHATGPT_CHOICE).not.toMatch(/^[0-9a-f]{24}$/);
+  });
+});
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `cd frontend && bun test src/modules/agenthub/CodexAccountSection.test.ts`
Expected: FAIL — `Cannot find module './CodexAccountSection'`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t8-impl`)

<!-- patch t8-impl -->
```diff
diff --git a/frontend/src/modules/agenthub/CodexAccountSection.tsx b/frontend/src/modules/agenthub/CodexAccountSection.tsx
new file mode 100644
index 0000000000000000000000000000000000000000..cf144e0a57a1c2fcbb2de812980796fda58e05b1
--- /dev/null
+++ b/frontend/src/modules/agenthub/CodexAccountSection.tsx
@@ -0,0 +1,140 @@
+import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
+import { Check, CircleDashed, LogOut, TriangleAlert } from "lucide-react";
+
+import { Button } from "@/components/ui/button";
+import { Skeleton } from "@/components/ui/skeleton";
+import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
+import { CODEX_VENDOR } from "@/bridge/agenthub";
+import { listAccounts, type Account } from "@/bridge/accounts";
+import {
+  agentHubSignInStatus,
+  agentHubSignOut,
+  getCodexApiKeyAccount,
+  saveCodexApiKeyAccount,
+  signInStatusLine,
+} from "@/bridge/agentSignIn";
+import { errorMessage } from "@/bridge/errors";
+import { CodexSignIn, agentSignInStatusQueryKey, useCodexSignIn } from "./CodexSignIn";
+
+const codexApiKeyQueryKey = ["codexApiKeyAccount"] as const;
+
+/** The Select's value for "no API key": Radix refuses an empty item value. */
+export const CHATGPT_CHOICE = "chatgpt";
+
+/**
+ * The accounts that can be Codex's API key: a token, shared rather than one
+ * connection's secret, and not limited to an environment — a session has none
+ * to offer, so the backend would refuse it at every spawn.
+ */
+export function codexApiKeyChoices(accounts: Account[]): Account[] {
+  return accounts.filter((a) => a.kind === "token" && !a.scopeOwner && a.env.length === 0);
+}
+
+/**
+ * Settings → Agents → Codex: who Codex signs in as, and how. Kept beside the
+ * sign-in it wraps, as Claude's is (ClaudeSignInSection).
+ *
+ * Two ways, and the API key wins when one is chosen: Codex's own ChatGPT
+ * sign-in, kept in the Codex home only DevKit uses, or a token from Accounts,
+ * chosen here by id and resolved when a session starts. Signing out of ChatGPT
+ * and unlinking the key are different buttons because they are different
+ * things — one clears Codex's home, the other only this choice.
+ */
+export function CodexAccountSection() {
+  const client = useQueryClient();
+  const status = useQuery({
+    queryKey: agentSignInStatusQueryKey(CODEX_VENDOR),
+    // Each read starts a short-lived Codex in the jail: once per visit.
+    queryFn: () => agentHubSignInStatus(CODEX_VENDOR),
+    staleTime: 60_000,
+  });
+  const chosen = useQuery({ queryKey: codexApiKeyQueryKey, queryFn: getCodexApiKeyAccount });
+  const accounts = useQuery({ queryKey: ["accounts"], queryFn: listAccounts });
+  const signIn = useCodexSignIn();
+
+  const refresh = () => {
+    void client.invalidateQueries({ queryKey: agentSignInStatusQueryKey(CODEX_VENDOR) });
+    void client.invalidateQueries({ queryKey: codexApiKeyQueryKey });
+  };
+  const choose = useMutation({ mutationFn: saveCodexApiKeyAccount, onSuccess: refresh });
+  const signOut = useMutation({ mutationFn: () => agentHubSignOut(CODEX_VENDOR), onSuccess: refresh });
+
+  const tokens = codexApiKeyChoices(accounts.data ?? []);
+  const chosenId = chosen.data ?? "";
+  const chosenAccount = (accounts.data ?? []).find((a) => a.id === chosenId);
+  // An id with no account behind it: deleted on another device, or before
+  // Accounts learned to refuse deleting a bound key.
+  const chosenMissing = chosenId !== "" && accounts.isSuccess && !chosenAccount;
+  const s = status.data;
+  const onChatGPT = Boolean(s?.signedIn && s.method !== "apiKey");
+
+  return (
+    <div data-testid="codex-account" className="flex flex-col gap-3">
+      {status.isPending ? (
+        <Skeleton className="h-5 w-56 rounded-control" />
+      ) : status.isError ? (
+        <p className="text-body-12 text-status-danger">{errorMessage(status.error)}</p>
+      ) : s ? (
+        <div className="flex flex-wrap items-center gap-2">
+          <span data-testid="codex-account-status" className="flex items-center gap-1.5 text-body-13 text-text-primary">
+            {s.signedIn ? (
+              <Check className="size-4 shrink-0 text-status-success" />
+            ) : (
+              <CircleDashed className="size-4 shrink-0 text-text-tertiary" />
+            )}
+            {signInStatusLine(s, chosenAccount?.label)}
+          </span>
+          {onChatGPT ? (
+            <Button
+              type="button"
+              variant="ghost"
+              size="sm"
+              disabled={signOut.isPending}
+              data-testid="codex-sign-out"
+              onClick={() => signOut.mutate()}
+            >
+              <LogOut />
+              Sign out
+            </Button>
+          ) : null}
+        </div>
+      ) : null}
+      {signOut.error ? <p className="text-body-12 text-status-danger">{errorMessage(signOut.error)}</p> : null}
+
+      {s && !s.signedIn ? <CodexSignIn signIn={signIn} /> : null}
+
+      <label className="flex flex-col gap-1">
+        <span className="text-caption-11 text-text-tertiary">API key instead of ChatGPT</span>
+        <Select
+          value={chosenId || CHATGPT_CHOICE}
+          disabled={choose.isPending || chosen.isPending}
+          onValueChange={(value) => choose.mutate(value === CHATGPT_CHOICE ? "" : value)}
+        >
+          <SelectTrigger aria-label="codex-api-key" className="w-full">
+            <SelectValue />
+          </SelectTrigger>
+          <SelectContent>
+            <SelectItem value={CHATGPT_CHOICE}>None — use the ChatGPT sign-in</SelectItem>
+            {tokens.map((a) => (
+              <SelectItem key={a.id} value={a.id}>
+                {`${a.label} · ${a.provider}`}
+              </SelectItem>
+            ))}
+            {chosenMissing ? <SelectItem value={chosenId}>Missing account</SelectItem> : null}
+          </SelectContent>
+        </Select>
+        <span className="text-caption-11 text-text-tertiary">
+          A token from Accounts. Codex gets it when a session starts; it is never written to
+          Codex&apos;s home. Add one in Accounts first.
+        </span>
+      </label>
+      {chosenMissing ? (
+        <p className="flex items-center gap-1.5 text-body-12 text-status-warning">
+          <TriangleAlert className="size-4 shrink-0" />
+          The chosen API key is no longer in Accounts. Choose another, or go back to ChatGPT.
+        </p>
+      ) : null}
+      {choose.error ? <p className="text-body-12 text-status-danger">{errorMessage(choose.error)}</p> : null}
+    </div>
+  );
+}
diff --git a/frontend/src/modules/settings/AgentRuntimeSetting.tsx b/frontend/src/modules/settings/AgentRuntimeSetting.tsx
index 788e83f4391fb3291acf4dfbdccd7040075cd5e8..ebe993d00cce6c05a97f8f29fe5d70a3ee7398bc 100644
--- a/frontend/src/modules/settings/AgentRuntimeSetting.tsx
+++ b/frontend/src/modules/settings/AgentRuntimeSetting.tsx
@@ -52,6 +52,8 @@ import {
   compareVersions,
 } from "@/bridge/agentRuntime";
 import { ClaudeSignInSection } from "@/modules/agenthub/ClaudeSignIn";
+import { CodexAccountSection } from "@/modules/agenthub/CodexAccountSection";
+import { CLAUDE_INFO, vendorInfo, type VendorInfo } from "@/modules/agenthub/vendors";
 
 export const agentRuntimeQueryKey = (agent: string) => ["agentRuntime", agent] as const;
 
@@ -126,9 +128,15 @@ export function useAgentRuntime(agent: string = CLAUDE_AGENT) {
 
 export type AgentRuntimeController = ReturnType<typeof useAgentRuntime>;
 
-/** Settings → Agents: the Claude agent's runtime. */
-export function AgentRuntimeSetting() {
-  const rt = useAgentRuntime();
+/**
+ * Settings → Agents: one agent's runtime and sign-in — Claude's by default,
+ * any vendor in the table by `vendor`. What differs between them is read from
+ * the table: Claude's adapter runs on Node and can use a Claude Code binary
+ * of the machine's; Codex is one native binary and signs in inside DevKit.
+ */
+export function AgentRuntimeSetting({ vendor = CLAUDE_INFO.id }: { vendor?: string } = {}) {
+  const info: VendorInfo = vendorInfo(vendor);
+  const rt = useAgentRuntime(info.runtimeId);
   const [confirmLatest, setConfirmLatest] = useState(false);
   const s = rt.status;
 
@@ -140,7 +148,7 @@ export function AgentRuntimeSetting() {
     );
   }
 
-  const { headline, latest } = runtimeStatusLine(s);
+  const { headline, latest } = runtimeStatusLine(s, info.runtimeName);
   const { current, previous, testedVersion, latestVersion } = s;
   const machine = s.claudeBinary.machine;
 
@@ -155,7 +163,10 @@ export function AgentRuntimeSetting() {
             {latest ? <span className="text-body-12 text-status-warning">{latest}</span> : null}
             <span className="text-caption-11 text-text-tertiary">
               {current
-                ? `SDK ${current.sdkVersion} · Node ${current.nodeVersion} · ${formatBytes(s.diskBytes)} on disk`
+                ? current.nodeVersion
+                  ? `SDK ${current.sdkVersion} · Node ${current.nodeVersion} · ${formatBytes(s.diskBytes)} on disk`
+                  : // A native binary (Codex): no SDK, and Node was only for the install.
+                    `${formatBytes(s.diskBytes)} on disk`
                 : `DevKit has tested ${testedVersion}. ${formatBytes(s.diskBytes)} on disk.`}
             </span>
           </div>
@@ -244,42 +255,44 @@ export function AgentRuntimeSetting() {
         <RuntimeRequirements requirements={s.requirements} />
       </div>
 
-      <div className="flex flex-col gap-2.5">
-        <SubLabel>CLAUDE CODE</SubLabel>
-        <RadioGroup
-          value={s.claudeBinary.source}
-          disabled={rt.busy}
-          onValueChange={(value) => rt.setClaudeBinary(value as ClaudeBinarySource)}
-          className="gap-3"
-        >
-          <label className="flex cursor-pointer items-start gap-2.5">
-            <RadioGroupItem value="bundled" className="mt-0.5" />
-            <span className="flex min-w-0 flex-col gap-0.5">
-              <span className="text-body-13 text-text-primary">Bundled with the agent (recommended)</span>
-              <span className="text-caption-11 text-text-tertiary">
-                Always the Claude Code version the agent&apos;s SDK was built with.
-              </span>
-            </span>
-          </label>
-          <label className={cn("flex items-start gap-2.5", machine ? "cursor-pointer" : "opacity-60")}>
-            <RadioGroupItem value="machine" disabled={!machine} className="mt-0.5" />
-            <span className="flex min-w-0 flex-col gap-0.5">
-              <span className="text-body-13 text-text-primary">Claude Code on this machine</span>
-              <span className="truncate text-code-11 text-text-tertiary" title={machine?.path}>
-                {machine ? `${machine.path} · ${machine.version}` : "Not found"}
+      {info.id === CLAUDE_INFO.id ? (
+        <div className="flex flex-col gap-2.5">
+          <SubLabel>CLAUDE CODE</SubLabel>
+          <RadioGroup
+            value={s.claudeBinary.source}
+            disabled={rt.busy}
+            onValueChange={(value) => rt.setClaudeBinary(value as ClaudeBinarySource)}
+            className="gap-3"
+          >
+            <label className="flex cursor-pointer items-start gap-2.5">
+              <RadioGroupItem value="bundled" className="mt-0.5" />
+              <span className="flex min-w-0 flex-col gap-0.5">
+                <span className="text-body-13 text-text-primary">Bundled with the agent (recommended)</span>
+                <span className="text-caption-11 text-text-tertiary">
+                  Always the Claude Code version the agent&apos;s SDK was built with.
+                </span>
               </span>
-              <span className="text-caption-11 text-text-tertiary">
-                Its version may not match the agent&apos;s SDK.
+            </label>
+            <label className={cn("flex items-start gap-2.5", machine ? "cursor-pointer" : "opacity-60")}>
+              <RadioGroupItem value="machine" disabled={!machine} className="mt-0.5" />
+              <span className="flex min-w-0 flex-col gap-0.5">
+                <span className="text-body-13 text-text-primary">Claude Code on this machine</span>
+                <span className="truncate text-code-11 text-text-tertiary" title={machine?.path}>
+                  {machine ? `${machine.path} · ${machine.version}` : "Not found"}
+                </span>
+                <span className="text-caption-11 text-text-tertiary">
+                  Its version may not match the agent&apos;s SDK.
+                </span>
               </span>
-            </span>
-          </label>
-        </RadioGroup>
-      </div>
+            </label>
+          </RadioGroup>
+        </div>
+      ) : null}
 
-      {current ? (
+      {current && info.signIn !== "none" ? (
         <div className="flex flex-col gap-2.5">
           <SubLabel>SIGN IN</SubLabel>
-          <ClaudeSignInSection />
+          {info.signIn === "inApp" ? <CodexAccountSection /> : <ClaudeSignInSection />}
         </div>
       ) : null}
 
diff --git a/frontend/src/modules/settings/SettingsView.tsx b/frontend/src/modules/settings/SettingsView.tsx
index 119f22e6205d73af7d2a6af54a9b85a2523d8a23..83ed185c2289d82c910f3f8c7bba2ff45b7c5fff 100644
--- a/frontend/src/modules/settings/SettingsView.tsx
+++ b/frontend/src/modules/settings/SettingsView.tsx
@@ -34,6 +34,7 @@ import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/comp
 import { McpInstallPanel } from "./McpInstallPanel";
 import { ConsoleStorageSetting } from "./ConsoleStorageSetting";
 import { AgentRuntimeSetting } from "./AgentRuntimeSetting";
+import { CODEX_VENDOR } from "@/bridge/agenthub";
 import { AgentNotificationSetting } from "./AgentNotificationSetting";
 import { CodexSessionsSetting } from "./CodexSessionsSetting";
 import { RevealPasswordSetting } from "./RevealPasswordSetting";
@@ -785,6 +786,10 @@ function AgentsTab() {
         <GroupLabel>CLAUDE AGENT</GroupLabel>
         <AgentRuntimeSetting />
       </div>
+      <div className="flex flex-col gap-3">
+        <GroupLabel>CODEX</GroupLabel>
+        <AgentRuntimeSetting vendor={CODEX_VENDOR} />
+      </div>
       <div className="flex flex-col gap-3">
         <GroupLabel>FILE CHECKPOINTS</GroupLabel>
         <AgentCheckpointSetting />
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `cd frontend && bun test src/modules/agenthub/ src/modules/settings/ src/moduleBoundaryRatchet.test.ts && bunx tsc --noEmit -p .`
Expected: PASS (kể cả ratchet ranh giới module), tsc sạch.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/agenthub/CodexAccountSection.test.ts frontend/src/modules/agenthub/CodexAccountSection.tsx frontend/src/modules/settings/AgentRuntimeSetting.tsx frontend/src/modules/settings/SettingsView.tsx
git commit -m "feat(settings): a Codex card in Settings → Agents

Install and versions like Claude's, and who Codex signs in as:
ChatGPT, signed in and out here, or a token chosen from Accounts.

<attribution>"
```

### Task 9: Hộp xác nhận Full auto cảnh báo Docker/`$HOME`; thẻ duyệt tool MCP

**Model:** sonnet

**Vì sao:** Spec §7.1, §7.2. (1) `bypassSandboxWarning(settings)` — hộp xác nhận bypass (dùng chung Claude) đọc Settings → Agents → Sandbox **lúc mở** và thêm dòng khi `$HOME` và/hoặc Docker đang mở; đọc hỏng thì coi như đang mở. Không dùng `useQuery` trong composer: test render composer không có `QueryClientProvider`. (2) Test chốt `isPermissionBypassChoice` với id của Codex (`bypassPermissions`/"Full auto" là bypass; `auto`, `ask` không). (3) Thẻ duyệt tool MCP: `permissionDetail(params)` lấy câu hỏi của Codex (text content) và đối số (`rawInput`, một dòng, cắt ở 240) cho call không phải edit/read — kể cả khi `toolCallId` là `mcp-approval-<n>` không gắn được vào lệnh gọi nào; nút always đọc `allowAlwaysLabel` → "Allow for session" khi option tự gọi mình là session (Codex), không bao giờ hứa "always".

**Files:** Create `frontend/src/modules/agenthub/surfaces/codexPermission.test.tsx`; Modify `frontend/src/bridge/agenthub.ts`, `frontend/src/modules/agenthub/surfaces/AgentComposer.tsx`, `frontend/src/modules/agenthub/surfaces/AgentPermissionRequest.tsx`, `frontend/src/modules/agenthub/surfaces/permissionCard.tsx`

- [ ] **Step 1: Áp patch test** (`extract t9-test`)

<!-- patch t9-test -->
```diff
diff --git a/frontend/src/modules/agenthub/surfaces/codexPermission.test.tsx b/frontend/src/modules/agenthub/surfaces/codexPermission.test.tsx
new file mode 100644
index 0000000000000000000000000000000000000000..b091d41d4e66a1bc9deea2c9ec1d04ac5e1e89d1
--- /dev/null
+++ b/frontend/src/modules/agenthub/surfaces/codexPermission.test.tsx
@@ -0,0 +1,121 @@
+import { describe, expect, it } from "bun:test";
+import { renderToString as renderRaw } from "react-dom/server";
+import { TooltipProvider } from "@/components/ui/tooltip";
+import { permissionDetail, type AgentSession, type PendingPermission } from "@/bridge/agenthub";
+import { AgentPermissionRequest } from "./AgentPermissionRequest";
+import { allowAlwaysLabel } from "./permissionCard";
+import { bypassSandboxWarning, isPermissionBypassChoice } from "./AgentComposer";
+
+const render = (node: React.ReactNode) => renderRaw(<TooltipProvider>{node}</TooltipProvider>);
+
+const session: AgentSession = {
+  id: "sess-codex",
+  agentType: "codex",
+  projectPath: "/workspace/app",
+  status: "awaiting_permission",
+  parentSessionId: null,
+  depth: 0,
+  title: "Codex",
+  acpSessionId: "th-1",
+};
+
+// The request internal/agenthub/codex_approvals.go (mcpToolApprovalFor) puts
+// to the person when Codex asks before running a DevKit MCP tool and no open
+// call can be matched to it: it stands on its own as "mcp-approval-<n>".
+function mcpApproval(over: Record<string, unknown> = {}): PendingPermission {
+  return {
+    id: "perm-mcp",
+    sessionId: session.id,
+    caller: "agent:sess-codex",
+    title: "Codex",
+    depth: 0,
+    createdAt: new Date().toISOString(),
+    expiresAt: new Date(Date.now() + 60_000).toISOString(),
+    params: {
+      sessionId: session.id,
+      toolCall: {
+        toolCallId: "mcp-approval-3",
+        title: "db.query",
+        kind: "other",
+        status: "pending",
+        content: [{ type: "content", content: { type: "text", text: 'Allow the devkit MCP server to run tool "db.query"?' } }],
+        rawInput: { profileId: "0123456789abcdef01234567", sql: "select 1" },
+        ...over,
+      },
+      options: [
+        { optionId: "allow_once", name: "Allow", kind: "allow_once" },
+        { optionId: "allow_always", name: "Allow for this session", kind: "allow_always" },
+        { optionId: "reject_once", name: "Deny", kind: "reject_once" },
+      ],
+    },
+  };
+}
+
+describe("Codex's MCP tool approval card", () => {
+  it("says which tool, what Codex asked, and with what arguments", () => {
+    const html = render(<AgentPermissionRequest pending={mcpApproval()} session={session} onResolve={() => {}} size="wide" />);
+    expect(html).toContain("db.query");
+    expect(html).toContain('data-testid="permission-message"');
+    expect(html).toContain("Allow the devkit MCP server to run tool");
+    expect(html).toContain('data-testid="permission-args"');
+    expect(html).toContain("select 1");
+  });
+
+  it("offers the session grant as what it is, never as always", () => {
+    const html = render(<AgentPermissionRequest pending={mcpApproval()} session={session} onResolve={() => {}} />);
+    expect(html).toContain('data-testid="permission-allow-always"');
+    expect(html).toContain("Allow for session");
+    expect(html).not.toContain("Allow always");
+    expect(allowAlwaysLabel({ optionId: "allow_always", name: "Always allow", kind: "allow_always" })).toBe("Allow always");
+    expect(allowAlwaysLabel(undefined)).toBe("Allow always");
+  });
+
+  it("a card attached to the open call reads the same", () => {
+    const html = render(
+      <AgentPermissionRequest
+        pending={mcpApproval({ toolCallId: "call-7", title: "devkit.db.query" })}
+        session={session}
+        onResolve={() => {}}
+      />,
+    );
+    expect(html).toContain("devkit.db.query");
+    expect(html).toContain("select 1");
+  });
+});
+
+describe("permissionDetail", () => {
+  it("leaves an edit to its diff and a command to its well", () => {
+    expect(permissionDetail({ toolCall: { kind: "edit", rawInput: { file_path: "a.ts", content: "x".repeat(5000) } } })).toEqual({});
+    expect(permissionDetail({ toolCall: { kind: "read", rawInput: { path: "a.ts" } } })).toEqual({});
+  });
+
+  it("elides long arguments and ignores empty ones", () => {
+    const long = permissionDetail({ toolCall: { kind: "other", rawInput: { sql: "x".repeat(1000) } } });
+    expect(long.args?.length).toBe(240);
+    expect(long.args?.endsWith("…")).toBe(true);
+    expect(permissionDetail({ toolCall: { rawInput: {} } })).toEqual({});
+    expect(permissionDetail(null)).toEqual({});
+  });
+});
+
+describe("Codex's Full auto", () => {
+  // internal/agenthub/adapter_codex.go codexModes: the ids are ACP's, and
+  // bypassPermissions is the one that must open the confirmation.
+  it("is the bypass the confirmation guards; Ask and Auto are not", () => {
+    expect(isPermissionBypassChoice({ value: "bypassPermissions", name: "Full auto" })).toBe(true);
+    expect(isPermissionBypassChoice({ value: "auto", name: "Auto" })).toBe(false);
+    expect(isPermissionBypassChoice({ value: "ask", name: "Ask" })).toBe(false);
+  });
+
+  it("the confirmation says what the jail leaves open", () => {
+    expect(bypassSandboxWarning({})).toContain("Docker socket and your $HOME");
+    expect(bypassSandboxWarning(null)).toContain("Docker socket and your $HOME");
+    expect(bypassSandboxWarning({ shareHome: false })).toBe(
+      "The Docker socket is open to this session — the sandbox does not stop the agent from using it.",
+    );
+    expect(bypassSandboxWarning({ allowDocker: false })).toBe(
+      "Your $HOME is open to this session — the sandbox does not stop the agent from using it.",
+    );
+    expect(bypassSandboxWarning({ shareHome: false, allowDocker: false })).toBe("");
+  });
+});
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `cd frontend && bun test src/modules/agenthub/surfaces/codexPermission.test.tsx`
Expected: FAIL — `Export named 'permissionDetail' not found in module '…/bridge/agenthub.ts'`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t9-impl`)

<!-- patch t9-impl -->
```diff
diff --git a/frontend/src/bridge/agenthub.ts b/frontend/src/bridge/agenthub.ts
index 3462a7e50bcbce60a428ee88e9b74652dccf06e2..ba91f37c3e188c4ca93cbdd13edacee989ab11fe 100644
--- a/frontend/src/bridge/agenthub.ts
+++ b/frontend/src/bridge/agenthub.ts
@@ -907,3 +907,41 @@ export function permissionCommand(params: unknown): string | undefined {
   const command = (rawInput as Record<string, unknown>).command;
   return typeof command === "string" && command.trim() ? command : undefined;
 }
+
+/** How much of a call's arguments a permission card shows; the rest is elided. */
+const PERMISSION_ARGS_LIMIT = 240;
+
+/**
+ * What a request without a command is about: the question the agent put
+ * (Codex asks before an MCP tool runs, and says which tool, as text content)
+ * and the arguments of the call, as one compact line. Only for a call that is
+ * not an edit or a read — an edit shows its diff, a read its path — so a
+ * file's whole content never lands in the card as JSON.
+ */
+export function permissionDetail(params: unknown): { message?: string; args?: string } {
+  if (!params || typeof params !== "object") return {};
+  const toolCall = (params as Record<string, unknown>).toolCall;
+  if (!toolCall || typeof toolCall !== "object") return {};
+  const tc = toolCall as Record<string, unknown>;
+  const kind = typeof tc.kind === "string" ? tc.kind : "";
+  if (kind && kind !== "other" && kind !== "fetch") return {};
+  const out: { message?: string; args?: string } = {};
+  if (Array.isArray(tc.content)) {
+    for (const entry of tc.content) {
+      if (!entry || typeof entry !== "object") continue;
+      const block = (entry as Record<string, unknown>).content;
+      if (!block || typeof block !== "object") continue;
+      const text = (block as Record<string, unknown>).text;
+      if (typeof text === "string" && text.trim()) {
+        out.message = text.trim();
+        break;
+      }
+    }
+  }
+  const raw = tc.rawInput;
+  if (raw && typeof raw === "object" && !Array.isArray(raw) && Object.keys(raw).length > 0) {
+    const line = JSON.stringify(raw);
+    out.args = line.length > PERMISSION_ARGS_LIMIT ? `${line.slice(0, PERMISSION_ARGS_LIMIT - 1)}…` : line;
+  }
+  return out;
+}
diff --git a/frontend/src/modules/agenthub/surfaces/AgentComposer.tsx b/frontend/src/modules/agenthub/surfaces/AgentComposer.tsx
index fe026e07c83aa1a522cc29a0bea316138b904a69..9f420879fee1baf5b430169d5020d1819c61f5ae 100644
--- a/frontend/src/modules/agenthub/surfaces/AgentComposer.tsx
+++ b/frontend/src/modules/agenthub/surfaces/AgentComposer.tsx
@@ -42,6 +42,8 @@ import { AgentContextMeter } from "./AgentContextMeter";
 import type { AvailableCommand, ConfigOption, ConfigSelectOption, SessionGoal, SessionMode, Usage } from "../types";
 import type { QueuedAgentPrompt } from "@/stores/agentSessions";
 import type { AgentImageAttachment } from "@/bridge/agenthub";
+import { getAgentSandbox } from "@/bridge/preferences";
+import type { AgentSandboxSettings } from "../../../../bindings/devkit/internal/preferences/models";
 import type { EntityRef } from "@/workbench/intents";
 import { vendorInfo } from "../vendors";
 
@@ -108,6 +110,27 @@ export function isPermissionBypassChoice(
   return Boolean(option && (BYPASS.test(option.id) || BYPASS.test(option.name ?? "")) && !OFF.test(choice.value));
 }
 
+/**
+ * The line the bypass confirmation adds when the jail is widened (Settings →
+ * Agents → Sandbox): with prompts off, nothing stands between the agent and
+ * what the sandbox lets it reach, and the two widenings reach a lot — the
+ * Docker socket is a way out of the jail altogether. They stay on because
+ * DevKit is useless for many workflows without them (Codex spec §7.1, §8.3);
+ * the person is told instead. Unset reads as on, like the setting itself, and
+ * so does settings that could not be read: the warning errs towards showing.
+ */
+export function bypassSandboxWarning(
+  settings: Pick<AgentSandboxSettings, "shareHome" | "allowDocker"> | null | undefined,
+): string {
+  const home = settings?.shareHome !== false;
+  const docker = settings?.allowDocker !== false;
+  const tail = "the sandbox does not stop the agent from using";
+  if (home && docker) return `The Docker socket and your $HOME are open to this session — ${tail} them.`;
+  if (docker) return `The Docker socket is open to this session — ${tail} it.`;
+  if (home) return `Your $HOME is open to this session — ${tail} it.`;
+  return "";
+}
+
 /** Applies a choice, or hands a bypass to `ask` to apply only once confirmed. */
 export function requestComposerChoice(isBypass: boolean, apply: () => void, ask: (apply: () => void) => void): void {
   if (isBypass) ask(apply);
@@ -451,6 +474,19 @@ export function AgentComposer({
   // A bypass waiting on the dialog. Not window.confirm: WKWebView (macOS) has
   // no confirm panel, so it answered false and bypass could never be enabled.
   const [pendingBypass, setPendingBypass] = useState<{ apply: () => void } | null>(null);
+  // Read when the confirmation opens, not before: the setting can change in
+  // Settings between one bypass and the next.
+  const [sandboxWarning, setSandboxWarning] = useState("");
+  useEffect(() => {
+    if (!pendingBypass) return;
+    let live = true;
+    getAgentSandbox()
+      .then((settings) => live && setSandboxWarning(bypassSandboxWarning(settings)))
+      .catch(() => live && setSandboxWarning(bypassSandboxWarning(null)));
+    return () => {
+      live = false;
+    };
+  }, [pendingBypass]);
   // Picked in a menu, so it waits for that menu to close; see heldChoice.
   const [heldBypass] = useState(heldChoice);
   /**
@@ -873,6 +909,11 @@ export function AgentComposer({
               <AlertDialogDescription>
                 {`${agentName ?? "The agent"} will edit files and run commands without asking you first, until you change this back.`}
               </AlertDialogDescription>
+              {sandboxWarning ? (
+                <p data-testid="bypass-sandbox-warning" className="text-body-12 text-status-warning">
+                  {sandboxWarning}
+                </p>
+              ) : null}
             </AlertDialogHeader>
             <AlertDialogFooter>
               <AlertDialogCancel>Cancel</AlertDialogCancel>
diff --git a/frontend/src/modules/agenthub/surfaces/AgentPermissionRequest.tsx b/frontend/src/modules/agenthub/surfaces/AgentPermissionRequest.tsx
index fed493c20406ac05b2883eb4b010b85fd42c2415..98c401e6f2bd3b828d77806a1c8f2e3aa7a65428 100644
--- a/frontend/src/modules/agenthub/surfaces/AgentPermissionRequest.tsx
+++ b/frontend/src/modules/agenthub/surfaces/AgentPermissionRequest.tsx
@@ -7,6 +7,7 @@ import {
   PermissionAsk,
   PermissionIdentity,
   PermissionLockedNote,
+  allowAlwaysLabel,
   permissionChoices,
   permissionEnv,
   type PermissionEnv,
@@ -111,7 +112,7 @@ export function AgentPermissionRequest({
           onClick={() => void onResolve(always.optionId)}
           className={cn("border-border-subtle", pad)}
         >
-          Allow always
+          {allowAlwaysLabel(always)}
         </Button>
       ) : null}
     </div>
diff --git a/frontend/src/modules/agenthub/surfaces/permissionCard.tsx b/frontend/src/modules/agenthub/surfaces/permissionCard.tsx
index 1407e3aab38242747a01bba20eaddafd1125b773..97de9d1106597e673813ef210226afac0974594a 100644
--- a/frontend/src/modules/agenthub/surfaces/permissionCard.tsx
+++ b/frontend/src/modules/agenthub/surfaces/permissionCard.tsx
@@ -5,7 +5,7 @@ import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
 import { EASE_OUT_STRONG, MOTION_BASE, MOTION_SLOW, noMotion } from "@/lib/motion";
 import { cn } from "@/lib/utils";
 import type { AgentSession, PendingPermission, PermissionOption } from "@/bridge/agenthub";
-import { permissionCommand, permissionToolTitle } from "@/bridge/agenthub";
+import { permissionCommand, permissionDetail, permissionToolTitle } from "@/bridge/agenthub";
 import { AgentState } from "./AgentState";
 
 export type PermissionEnv = "open" | "locked";
@@ -200,6 +200,7 @@ export function PermissionAsk({
 }) {
   const toolTitle = permissionToolTitle(pending.params);
   const command = permissionCommand(pending.params);
+  const detail = command ? {} : permissionDetail(pending.params);
   // claude-agent-acp titles a Bash call with the command in backticks; showing
   // it twice, once as a title and once in the well, says nothing new.
   const titleRepeatsCommand =
@@ -220,10 +221,29 @@ export function PermissionAsk({
           {command}
         </p>
       ) : null}
+      {detail.message ? (
+        <p data-testid="permission-message" className="line-clamp-2 text-body-12 text-text-primary">
+          {detail.message}
+        </p>
+      ) : null}
+      {detail.args ? (
+        <p data-testid="permission-args" className="line-clamp-2 break-all text-code-11 text-text-secondary" title={detail.args}>
+          {detail.args}
+        </p>
+      ) : null}
     </div>
   );
 }
 
+/**
+ * What the always-grant button says. Codex's lasts for the session and names
+ * itself so ("Allow for this session"); calling that "Allow always" would
+ * promise a rule nobody wrote. Anything else keeps the old words.
+ */
+export function allowAlwaysLabel(option: PermissionOption | undefined): string {
+  return option && /session/i.test(option.name) ? "Allow for session" : "Allow always";
+}
+
 /** The option the vendor means by "just this once", and the always-grant. */
 export function permissionChoices(options: PermissionOption[]) {
   const once =
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `cd frontend && bun test && bun run build`
Expected: Toàn bộ `bun test` PASS (xem ghi chú flake ở Kết thúc), build qua ngân sách hiệu năng.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/bridge/agenthub.ts frontend/src/modules/agenthub/surfaces/AgentComposer.tsx frontend/src/modules/agenthub/surfaces/AgentPermissionRequest.tsx frontend/src/modules/agenthub/surfaces/codexPermission.test.tsx frontend/src/modules/agenthub/surfaces/permissionCard.tsx
git commit -m "feat(agenthub): Full auto warns what the jail leaves open; MCP tool cards say what runs

The bypass confirmation adds a line when Settings leaves \$HOME or the
Docker socket open. A tool approval shows Codex's question and the
call's arguments, and a session grant is labelled as one.

<attribution>"
```

### Task 10: Tài liệu

**Model:** haiku

**Vì sao:** Spec §9 Giai đoạn 7: `docs/dev-kit/agent-hub.mdx` (callout, Known ceiling, Codex Lớp 2 → Implemented, runtime, open question), `implementation-status.mdx`, `AGENTS.md` (bảng vendor, sign-in chỉ UI, API key theo id, `VendorError`), `README.md`, và một đoạn "Đã làm ở Kế hoạch 3" trong spec §6.4.

**Files:** Modify `AGENTS.md`, `README.md`, `docs/dev-kit/agent-hub.mdx`, `docs/dev-kit/implementation-status.mdx`, `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md`

- [ ] **Step 1: Áp patch hiện thực** (`extract t10-impl`)

<!-- patch t10-impl -->
```diff
diff --git a/AGENTS.md b/AGENTS.md
index e52d52fdb13ba3a7562b29c076c427da6bec981d..9e4a96610de6b6186a28efc03211e2fb7f654ac6 100644
--- a/AGENTS.md
+++ b/AGENTS.md
@@ -93,12 +93,28 @@ as production-verified against a real Claude Code session in cloud CI.
 session service (SQLite migration 13), bridge/UI multi-tab, MCP `agenthub.*`.
 Claude vendor spawn uses `sandbox.DefaultRunner().StartPiped` (fail-closed).
 A Codex adapter (`adapter_codex*.go`, `codex_*.go`) drives `codex app-server` 0.147.0
-under the same jail with its own `CODEX_HOME` (`<data>/agent-homes/codex`); it is
-registered in `main.go`, but the UI for it (sign-in panel, vendor picker) is not built
-yet — design in `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md`, state in
+under the same jail with its own `CODEX_HOME` (`<data>/agent-homes/codex`) — design in
+`docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md`, state in
 `docs/superpowers/HANDOFF-codex.md`. Gemini is **not** shipped. Product design + status:
 `syurodev/base` → `/dev-kit/agent-hub`, `/dev-kit/implementation-status`.
 
+- **The frontend reads a vendor table** (`frontend/src/modules/agenthub/vendors.ts`:
+  name, runtime id, how it signs in, colour). Do not compare against `"claude"`; an
+  unknown vendor gets its own name and nothing to install or sign in to.
+- **Codex signs in inside DevKit, UI only.** Bridge `AgentHubSignInStatus` /
+  `AgentHubStart{Browser,Device}SignIn` / `AgentHubCancelSignIn` / `AgentHubSignOut`
+  (`auth:codex`), event `agenthub:signInCompleted`. There is no MCP tool for it
+  (`TestNoToolSignsAgentsIn`), and `Service` refuses a caller stamped as an agent or a
+  user-terminal MCP client. Audit lines carry the method and account id, never the
+  login page, code or key.
+- **Codex API key = an Accounts token, by id.** `preferences` stores only the id
+  (`agent_codex_api_key_account`); `app.CodexAPIKey` resolves it at spawn and Accounts
+  refuses to delete it while chosen. A successful ChatGPT sign-in adds a `KindExternal`
+  "Codex (ChatGPT)" row to Accounts; signing out removes it.
+- `agent_not_installed` / `agent_auth_required` name the vendor: adapters wrap those
+  sentinels in `agenthub.VendorError`, and `bridge/errors.go` words the message per
+  vendor.
+
 **Not in server mode**: `main.go` registers `agenthub.ServerModeAdapter` stubs there, and
 every session fails with `ErrUnavailableInServerMode` — the jail shares the host loopback
 and server mode's bridge has no authentication, so a session could approve itself.
diff --git a/README.md b/README.md
index 6509eca19b3440ced864d10782e28b90b8947702..f0d7d7989d23906c09ec8515874dbe298ebcad16 100644
--- a/README.md
+++ b/README.md
@@ -32,7 +32,7 @@ GUI (cgo) — `libgtk-4-dev` + `libwebkitgtk-6.0-dev`.
 | MCP stdio proxy (needs desktop + unlocked vault) | `task mcp` |
 | Install DevKit MCP into an agent | `task mcp:install AGENT=claude-code\|codex SCOPE=global\|project PROJECT=.` |
 | Uninstall DevKit MCP from an agent | `task mcp:uninstall AGENT=claude-code\|codex SCOPE=global\|project PROJECT=.` |
-| Agent Hub (Partial: mock + Claude; see `AGENTS.md`) | `go test ./internal/acp/ ./internal/agenthub/` |
+| Agent Hub (Partial: Claude + Codex; see `AGENTS.md`) | `go test ./internal/acp/ ./internal/codexapp/ ./internal/agenthub/` |
 
 Cloud/CI setup and non-obvious run caveats are documented in `AGENTS.md`.
 
diff --git a/docs/dev-kit/agent-hub.mdx b/docs/dev-kit/agent-hub.mdx
index 5ab8cc00f6ddc9eac2fbc70a73c6480e8df52828..a13429258e9266fec577026a2673252ae50ed179 100644
--- a/docs/dev-kit/agent-hub.mdx
+++ b/docs/dev-kit/agent-hub.mdx
@@ -13,11 +13,12 @@ tự gọi sang agent khác lấy kết quả — không cần con người đi
 
 import { Callout } from 'fumadocs-ui/components/callout';
 
-<Callout type="info" title="Implemented — Phase 4 v1 closed (Claude-only)">
+<Callout type="info" title="Implemented — Phase 4 v1 closed; Codex là vendor thứ hai (2026-09-25)">
 Phase 4 đóng với runtime: `internal/acp`, `internal/agenthub` (Claude; mock chỉ là test double),
 bridge/UI multi-tab, MCP `agenthub.*`, spawn qua
-`sandbox.DefaultRunner().StartPiped(...)` (fail-closed). **Codex / Gemini
-adapters vẫn Design** — thêm sau, không chặn đóng Phase 4. Chưa claim
+`sandbox.DefaultRunner().StartPiped(...)` (fail-closed). **Codex chạy trong Agent Hub**
+(Lớp 2, `codex app-server` 0.147.0 — xem mục Codex dưới **Known ceiling (v1)**).
+**Gemini vẫn Design** — thêm sau theo cùng `VendorAdapter`. Chưa claim
 production-verified vs Claude Code thật trong CI — smoke desktop. **Next:**
 [Workbench UI (Phase 5)](/dev-kit/roadmap#phase-5--workbench-ui) theo
 `dev-kit-app/DESIGN.md` (UI đủ flex cho vendor/tab). Sandbox:
@@ -699,10 +700,14 @@ Không cái nào bị protocol bắt buộc; thêm khi có nhu cầu thật.
 ### Known ceiling (v1)
 
 - Chỉ target ACP protocol version 1; agent nào chỉ hỗ trợ v2 chưa dùng được.
-- Adapter shipped **trong Agent Hub** (spawn, ACP, `VendorAdapter`): **Claude**
-  (qua `sandbox.StartPiped`). Mock adapter chỉ là test double cho test
-  `agenthub`/`bridge` — không đăng ký trong app, không có trong bộ chọn agent.
-  Gemini vẫn Design — thêm theo cùng `VendorAdapter`, không rework core.
+- Adapter shipped **trong Agent Hub** (spawn, `VendorAdapter`): **Claude** (ACP) và
+  **Codex** (app-server, dịch sang ACP trong adapter), cả hai qua `sandbox.StartPiped`.
+  Mock adapter chỉ là test double cho test `agenthub`/`bridge` — không đăng ký trong
+  app, không có trong bộ chọn agent. Gemini vẫn Design — thêm theo cùng
+  `VendorAdapter`, không rework core. Frontend đọc **bảng mô tả vendor**
+  (`frontend/src/modules/agenthub/vendors.ts`: tên, runtime, cách đăng nhập, màu) thay
+  cho các chỗ từng so sánh với `"claude"`; vendor lạ hiện đúng tên của nó, không bao
+  giờ mượn hàng của Claude.
 - **Codex — hai lớp tách rời, không phải một adapter còn thiếu.** DevKit không
   spawn Codex và không cần `VendorAdapter` để *thấy* nó:
 
@@ -723,8 +728,41 @@ Không cái nào bị protocol bắt buộc; thêm khi có nhu cầu thật.
     lưu — đúng lý do `api_request_history` seal URL: DevKit không viết ra
     chúng, cả hai đều có thể mang secret. UI: Settings → Agents → "Codex
     (observed)", chỉ xem transcript, không gửi được prompt từ đó.
-  - **Lớp 2 (chạy trong Agent Hub) — Design, Phase 0 đã đo một phần
-    (2026-09-20).** Cần một `VendorAdapter` thật, và đường vào giờ nghiêng hẳn
+  - **Lớp 2 (chạy trong Agent Hub) — Implemented (2026-09-25).** Thiết kế:
+    `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md`; trạng thái và việc
+    còn treo: `docs/superpowers/HANDOFF-codex.md`. Tóm tắt:
+
+    - `CodexAdapter` (`internal/agenthub/adapter_codex.go`) chạy `codex app-server`
+      **0.147.0** (DevKit tự cài qua `internal/agentruntime`, binary native; npm chỉ cần
+      lúc cài) trong jail của DevKit, với `CODEX_HOME` riêng `<data>/agent-homes/codex` —
+      `~/.codex` thật bị ẩn. Năm file mang chính sách trong home (`config.toml`, `rules/`,
+      `AGENTS.md`, `AGENTS.override.md`, `skills/`) bị reset trước mỗi spawn và chỉ-đọc
+      trong jail.
+    - Ba chế độ: Ask (`untrusted`, mặc định, resume luôn về Ask) / Auto (`on-request`) /
+      Full auto (`never`, id `bypassPermissions` nên hộp xác nhận bypass tự bật; hộp đó
+      thêm dòng cảnh báo khi Settings → Agents → Sandbox đang mở `$HOME`/Docker). Cổng
+      env prod của DevKit nằm trong tool MCP nên vẫn hỏi ở Full auto.
+    - Duyệt quyền qua thẻ sẵn có: lệnh, sửa file, và **mỗi lần gọi tool MCP của DevKit**
+      ở Ask/Auto (Allow / Allow for session / Deny — không bao giờ "always"). Thẻ tool MCP
+      hiện tên tool, câu hỏi của Codex và đối số.
+    - **Đăng nhập trong app** (Settings → Agents → Codex, và ngay trong bản nháp session
+      khi Codex báo chưa đăng nhập): ChatGPT qua trình duyệt là mặc định; "Use a code
+      instead" cho máy không mở được trình duyệt, kèm dòng nhắc bật device code trong
+      ChatGPT → Settings → Security (tắt sẵn theo tài khoản, Codex không báo). Hoặc chọn
+      một **API key** — account `token` trong Accounts; preferences chỉ lưu **id**, key
+      được resolve trong Go lúc spawn và gửi qua dây sau `initialize` (store
+      `ephemeral`), không nằm trên đĩa, env hay argv. Accounts ghi một dòng `external`
+      "Codex (ChatGPT)" khi đăng nhập ChatGPT thành công và xoá nó khi đăng xuất.
+    - Bridge: `AgentHubSignInStatus`, `AgentHubStartBrowserSignIn`,
+      `AgentHubStartDeviceSignIn`, `AgentHubCancelSignIn`, `AgentHubSignOut` (capability
+      `agenthub:read`/`agenthub:execute` trên `auth:codex`), event
+      `agenthub:signInCompleted`. **Chỉ UI**: không có tool MCP nào, và service từ chối
+      caller là agent hoặc CLI trên socket MCP chung. Audit ghi phương thức và id
+      account, không bao giờ ghi trang đăng nhập, mã hay key.
+    - Lỗi `agent_not_installed` / `agent_auth_required` nêu **đúng vendor**
+      (`agenthub.VendorError`), không còn luôn nói "Claude".
+
+    Lịch sử — Phase 0 (2026-09-20), trước khi có adapter: đường vào nghiêng hẳn
     về app-server JSON-RPC của chính Codex thay vì `@zed-industries/codex-acp`
     — package đó không release gì từ 2026-06-08 trong khi `@openai/codex` vẫn
     ship hằng ngày, và giờ có thêm lý do kỹ thuật, không chỉ lý do bus-factor.
@@ -790,11 +828,10 @@ Không cái nào bị protocol bắt buộc; thêm khi có nhu cầu thật.
       toàn bộ `v2`, chưa rõ đúng phần nào. Chưa chốt được vì chưa cần đến
       method nào đòi cờ.
 
-    **Còn thiếu để chốt hẳn**: một turn chạy trọn vẹn với auth thật (cần máy
-    có `codex login`, môi trường build này không cho phép — proxy chặn domain
-    OpenAI), hình dạng lỗi auth thật khi không có credential nhưng network
-    thông, và một vòng approval thật (chưa kích hoạt được vì chưa có turn nào
-    chạy tới bước cần duyệt). Việc còn lại này giao cho máy có login Codex.
+    **Đã chốt sau đó** (2026-09-24/25, macOS, tài khoản ChatGPT, home nháp): một lượt
+    thật với approval sửa file, duyệt tool MCP trên dây thật, usage sau resume, và
+    đăng nhập trình duyệt qua chính `StartBrowserSignIn` từ jail Seatbelt. Chưa chạy
+    live trên Linux (bwrap) ngoài Phase 0.
 - **Không chạy ở server mode** (2026-09-24). Server mode phục vụ toàn bộ bridge qua HTTP
   loopback không xác thực, còn jail dùng `NetworkHost` nên đi chung loopback: một session có thể
   tự gọi `AgentHubResolvePermission`/`InteractionComplete`. `main.go` đăng ký
@@ -804,9 +841,10 @@ Không cái nào bị protocol bắt buộc; thêm khi có nhu cầu thật.
 - Router tự động chọn agent theo intent chưa có — v1 chỉ gọi tường minh theo `sessionId`.
 - Chưa có multi-client đồng bộ xem 1 session (kiểu AHP) — ngoài scope v1.
 - Claude adapter chưa được claim production-verified trong CI với Claude Code thật.
-- Runtime Claude chỉ cài được khi máy đã có Node ≥ 22 — DevKit không tải Node. Mới
-  có một agent cài được (`claude-acp`); Codex/Gemini dùng lại `agentruntime` khi có
-  adapter. UI Settings → Agents cho runtime đang làm.
+- Runtime cài được khi máy đã có Node ≥ 22 và npm — DevKit không tải Node. Hai agent
+  cài được: `claude-acp` (chạy trên Node) và `codex` (binary native; Node chỉ cần lúc
+  cài). Mỗi agent có card riêng ở Settings → Agents; Gemini sẽ dùng lại `agentruntime`
+  khi có adapter.
 - UI runtime đã có `Agent / Rail`, permission toast, dock badge và companion;
   subscriber shell giữ transcript chạy khi đổi module. **Đóng là cất đi, không
   phải khoá:** session đóng vẫn đọc và đổi tên được, và prompt gửi tay mở lại nó
@@ -936,7 +974,8 @@ T0 trước vì nó rẻ và nó quyết định `fs/*` có đáng làm không.
   `sandbox.StartPiped` jail — xem mục Lớp 2 dưới **Known ceiling (v1)** ở
   trên. Còn thiếu: một turn hoàn chỉnh với auth thật và một vòng approval
   thật, chặn bởi domain OpenAI không nằm trong allowlist proxy của môi
-  trường build này — cần máy có `codex login`.
+  trường build này — cần máy có `codex login`. **Đóng (2026-09-25):** Lớp 2 đã
+  ship qua app-server; lượt thật và approval thật đã chạy trên macOS.
 - `fs/*` và `terminal/*` có thực sự được `claude-agent-acp` gọi không, hay chỉ là plumbing chưa dùng? Quyết định cả việc Capability Gateway có mediate được file access của agent hay không — đo bằng T0 trước khi cam kết.
 - ~~Ai chịu trách nhiệm theo dõi version bump từ `agentclientprotocol` org theo thời gian?~~ **Đã chốt (2026-09-13):** DevKit khoá `TestedVersion`; bản mới nhất của registry chỉ hiện như cập nhật chưa kiểm thử, và nâng mặc định theo [quy trình Nâng `TestedVersion`](#nâng-testedversion). Guard CI (`agent-acp-guard.yml`) vẫn chạy latest để báo trước release làm hỏng kênh `_meta`.
 
diff --git a/docs/dev-kit/implementation-status.mdx b/docs/dev-kit/implementation-status.mdx
index fb5579e9562fd817451b2bcfe163434902d335e1..6df1b0ca8ff476b73b635e897bbc4396d05fec10 100644
--- a/docs/dev-kit/implementation-status.mdx
+++ b/docs/dev-kit/implementation-status.mdx
@@ -36,7 +36,7 @@ thể thay đổi theo commit; kiến thức kiến trúc ổn định nằm ở
 | Sync / Replication client | Implemented | Phase 3B transport/pull/apply, Phase 3C local mutation flow và **Git-style conflict resolve** đã có. `internal/auth` dùng Authorization Code + PKCE S256 qua system browser + loopback callback, refresh-token rotation trong OS keychain và access token chỉ in-memory. `syncclient.ConnectWithTokenSource` lấy token mới trước request; `SyncConnect` pin Gateway `https://api.synx.io.vn`. Enrollment token gửi trên sync session handshake. Deployed matrix G1–G4 pass qua `cmd/synce2e/` trên Compose stack. Evidence: `internal/auth/{service,keyring_store}.go`, `internal/syncclient/{service,connect,syncnow}.go`, `internal/bridge/sync.go`, `cmd/synce2e/`; tests `internal/auth/service_test.go`, `internal/bridge/sync_test.go`, `go test ./cmd/synce2e/ -count=1 -timeout 15m`. | 3-pane merge UI đã có (`frontend/src/modules/sync/ResolvePanel.tsx`, Figma page 12: Local / Merged editable ⌘S / Remote; secret và delete chỉ chọn một bên). Còn device-management UI. |
 | Sync SaaS backend + auth gateway | Implemented | Companion checkout `dev-kit-backend/` (branch `feat/phase-b-device-revoke`, `2060f39`): Spring Boot `session/push/pull`, PostgreSQL Liquibase schema, account/device registration + revoke guard, atomic arbitration/audit. `gateway/` validate Keycloak JWT, strip identity/proxy headers, ký JWT nội bộ ≤45s, publish JWKS, `GET /v1/desktop/config`, Redis denylist early reject (fail-open). **BE Phase B**: `GET/POST /v1/sync/devices` list+revoke, last-ACTIVE `409`, Redis `sync:revoked-device:{sub}:{deviceId}` TTL 60s. Desktop bridge tối thiểu (`02afaaa`). Evidence: `./gradlew :test --tests com.synx.devkit.e2e.SyncApiIT`, `./gradlew :gateway:test`, desktop `go test ./internal/sync ./internal/bridge ./internal/app ./internal/arch`. G4 rewritten to API revoke; re-run `go test ./cmd/synce2e/ -run TestG4RevokedDeviceRejected` against Compose when verifying deployed matrix. | UI device management, key-rotation runbook, retention/backup vẫn later. Spec: `docs/superpowers/specs/2026-08-11-be-phase-b-device-revoke-design.md`. |
 | MCP | Implemented | `internal/mcp` (socket host + tools), `cmd/devkit-mcp` (stdio proxy + install), `internal/interaction` (selection/confirmation; headless fail-fast); install `claude-code`/`codex` × `global`/`project`; env-wins over `allow_always`. Tool sets: `notes.*`, `db.*`, `ssh.*`, `kafka.*`, `codegraph.*`, `agenthub.*`. | Resources / Streamable HTTP deferred. Not production-verified vs real Claude Code in CI — desktop smoke recommended. |
-| Agent Hub | Implemented | Phase 4 v1 **Claude-only closed**: ACP/service/sandbox, bridge + ModuleManifest, frontend `frontend/src/modules/agenthub/`, MCP `agenthub.*`. Runtime có shell-owned shared transcript, prompt queue (drain chỉ khi completed), file/context và paste/drop image transient ≤8 MiB, live plan, câu hỏi của agent qua ACP form elicitation (FR-13), **checkpoint file mỗi lượt + khôi phục code + rewind hội thoại** (FR-14…19, `internal/checkpoint` + `internal/agenthub/checkpoint*.go`, `rewind*.go`), session history sau restart và rename thủ công. Tests: `go test ./internal/agenthub/ ./internal/checkpoint/ ./internal/bridge/`; `cd frontend && bunx tsc --noEmit && bun test`. **Codex — Layer 1 (quan sát, không spawn) xong**: `internal/codexhook` là hợp đồng với 12 hook lifecycle của Codex (`codex-rs/hooks/src/lib.rs`), DevKit đăng ký 6 cái (`SessionStart`/`UserPromptSubmit`/`PreToolUse`/`PostToolUse`/`Stop`/`SessionEnd`) — cố ý **không** có `PermissionRequest`: DevKit chưa có UI để trả lời, và trả lời tự động nghĩa là thay luôn cửa duyệt của Codex bằng allow-luôn. `task mcp:install AGENT=codex` giờ còn ghi `[hooks]` vào `config.toml`, mỗi event một handler `mcp_tool` trỏ về tool `codex.hookEvent` — merge an toàn với hook người dùng tự viết (chỉ xoá đúng group mà DevKit từng ghi, nhận diện bằng chữ ký `{type,server,tool}` chứ không phải theo tên server). `internal/codexsessions` lưu transcript local-only (`codex_hook_events`, m27, không sync): prompt/tool input/output **seal bằng DEK** (giống URL của `api_request_history` — cùng lý do: DevKit không viết ra chúng, cả hai đều có thể mang secret), `summary` thì plaintext để list được khi vault khoá. UI: Settings → Agents → "Codex (observed)" — chỉ xem, không gửi prompt được từ đây. | Codex chưa chạy được **trong** Agent Hub (Layer 2 — Phase 0 (2026-09-20) đo app-server JSON-RPC thật không cần login, chạy được trong đúng `sandbox.StartPiped` jail; đường vào giờ nghiêng hẳn về app-server thay vì `codex-acp` npm im lặng từ 2026-06-08. Còn thiếu một turn hoàn chỉnh với auth thật và một vòng approval thật, chặn bởi proxy môi trường build không cho domain OpenAI — xem `docs/dev-kit/agent-hub.mdx`). Gemini adapter deferred; checkpoint/rewind chưa chạy end-to-end với Claude thật (spike S1–S7 đã đo, kịch bản đầu-cuối chưa); theo dõi thư mục ngoài project, khôi phục ảnh/attachment khi sửa prompt, rewind do agent gọi, đồng bộ checkpoint giữa máy đều ngoài phạm vi v1; tìm kiếm trong transcript deferred (xuất Markdown và syntax highlighting đã có). CI real Claude và Wails/WKWebView thật chưa được claim. |
+| Agent Hub | Implemented | Phase 4 v1 **Claude-only closed**: ACP/service/sandbox, bridge + ModuleManifest, frontend `frontend/src/modules/agenthub/`, MCP `agenthub.*`. Runtime có shell-owned shared transcript, prompt queue (drain chỉ khi completed), file/context và paste/drop image transient ≤8 MiB, live plan, câu hỏi của agent qua ACP form elicitation (FR-13), **checkpoint file mỗi lượt + khôi phục code + rewind hội thoại** (FR-14…19, `internal/checkpoint` + `internal/agenthub/checkpoint*.go`, `rewind*.go`), session history sau restart và rename thủ công. Tests: `go test ./internal/agenthub/ ./internal/checkpoint/ ./internal/bridge/`; `cd frontend && bunx tsc --noEmit && bun test`. **Codex — Layer 1 (quan sát, không spawn) xong**: `internal/codexhook` là hợp đồng với 12 hook lifecycle của Codex (`codex-rs/hooks/src/lib.rs`), DevKit đăng ký 6 cái (`SessionStart`/`UserPromptSubmit`/`PreToolUse`/`PostToolUse`/`Stop`/`SessionEnd`) — cố ý **không** có `PermissionRequest`: DevKit chưa có UI để trả lời, và trả lời tự động nghĩa là thay luôn cửa duyệt của Codex bằng allow-luôn. `task mcp:install AGENT=codex` giờ còn ghi `[hooks]` vào `config.toml`, mỗi event một handler `mcp_tool` trỏ về tool `codex.hookEvent` — merge an toàn với hook người dùng tự viết (chỉ xoá đúng group mà DevKit từng ghi, nhận diện bằng chữ ký `{type,server,tool}` chứ không phải theo tên server). `internal/codexsessions` lưu transcript local-only (`codex_hook_events`, m27, không sync): prompt/tool input/output **seal bằng DEK** (giống URL của `api_request_history` — cùng lý do: DevKit không viết ra chúng, cả hai đều có thể mang secret), `summary` thì plaintext để list được khi vault khoá. UI: Settings → Agents → "Codex (observed)" — chỉ xem, không gửi prompt được từ đây. **Codex — Layer 2 (chạy trong Agent Hub) xong (2026-09-25)**: `CodexAdapter` nói `codex app-server` 0.147.0 trong jail với `CODEX_HOME` riêng; Ask / Auto / Full auto; duyệt lệnh, sửa file và tool MCP qua thẻ sẵn có; resume; đăng nhập trong app (ChatGPT qua trình duyệt, device code, hoặc API key từ Accounts theo id); bridge sign-in chỉ UI; frontend đọc bảng vendor (`modules/agenthub/vendors.ts`); card Codex ở Settings → Agents. Tests: `go test ./internal/codexapp/ ./internal/agenthub/ ./internal/bridge/ ./internal/app/ ./internal/accounts/ ./internal/preferences/`; `cd frontend && bun test`. | Codex: live chỉ mới chạy trên macOS (Linux/bwrap mới có Phase 0); smoke thủ công trên app desktop chưa làm; hình dạng lỗi token hết hạn giữa lượt chưa đo; device code từ jail chưa chạy thật. Gemini adapter deferred; checkpoint/rewind chưa chạy end-to-end với Claude thật (spike S1–S7 đã đo, kịch bản đầu-cuối chưa); theo dõi thư mục ngoài project, khôi phục ảnh/attachment khi sửa prompt, rewind do agent gọi, đồng bộ checkpoint giữa máy đều ngoài phạm vi v1; tìm kiếm trong transcript deferred (xuất Markdown và syntax highlighting đã có). CI real Claude và Wails/WKWebView thật chưa được claim. |
 | Agent identity | Implemented | `internal/identity`, `internal/mcp/session_host.go`, `internal/audit/sqlsink.go`, `internal/agenthub/service.go`; tests: `internal/agenthub/service_identity_test.go`, `internal/mcp/identity_test.go`, `internal/interaction/hub_identity_test.go`, `internal/sandbox/backend_seatbelt_test.go` (`TestSeatbeltAllowUnixSocketsConnects`). Unix domain socket riêng per-session, sandbox isolation ở kernel, caller propagation trong context, per-caller pending queues, audit log persistent vào SQLite. **Verify end-to-end 2026-09-05**: session Claude thật gọi `env.listTags`, dòng audit `caller = agent:<uuid>` join đúng `agent_sessions`. | Loopback TCP vẫn kết nối được (cần egress proxy) và `codegraph.*` vẫn ở mức `project:active` (cần Workspace). |
 | `internal/notify` (leaf) | Implemented | Tách khỏi `agenthub` thành bước refactor riêng. `Notifier` + `Gate` + `Notifications`; khoá theo `Target{Module, Kind, ID}` — struct so sánh được nên `Gate` dùng thẳng làm map key và hai module trùng id không còn đụng nhau. `agenthub` là consumer đầu, `tasks` là thứ hai. Tests: `go test ./internal/notify/`; `internal/arch` pin nó là leaf. | Target cố ý **không** phải `EntityRef` (khái niệm của workbench ở frontend); bridge dịch giữa hai bên. |
 | `internal/gitparse` (leaf) | Implemented | Tách parser porcelain + diff-tree raw/numstat ra khỏi `checkpoint` và `codegraph`, nơi chúng đang tồn tại hai bản. `checkpoint.Change` giữ alias nên mọi call site cũ không đổi. `internal/arch` pin leaf, và luật của `checkpoint` được sửa để phân biệt "không biết gì về session/storage" với "không phụ thuộc gì". | — |
@@ -88,7 +88,7 @@ mà chỉ giữ evidence riêng của mình và trỏ về đây:
 
 - Checked on: 2026-09-17 (Agent Hub checkpoint / restore / rewind, `claude-agent-acp` 0.78)
 - DevKit checkout branch: `feat/agent-runtime-install`
-- DevKit checkout status: Phase 2–4 + CodeGraph `Implemented`. Vault UX overhaul `Implemented`. Agent Hub **Implemented** (Claude-only v1; Codex/Gemini Design), gồm checkpoint file, khôi phục code và rewind hội thoại trên `claude-agent-acp` 0.78. Sandbox **Implemented** including `StartPiped` and live-verified `NetworkHost` on macOS.
+- DevKit checkout status: Phase 2–4 + CodeGraph `Implemented`. Vault UX overhaul `Implemented`. Agent Hub **Implemented** (Claude v1; Codex Layer 2 thêm 2026-09-25; Gemini Design), gồm checkpoint file, khôi phục code và rewind hội thoại trên `claude-agent-acp` 0.78. Sandbox **Implemented** including `StartPiped` and live-verified `NetworkHost` on macOS.
 - Core hardening & features verified this pass:
   - Agent Hub daily flows: shared per-session prompt queue drains only after a completed turn; paste/drop image attachments are validated (PNG/JPEG/GIF/WebP, 8 MiB) and sent transiently; plans update in place; closed sessions remain readable after reload and can be renamed.
   - Removed unused AgentImage transcript leaf and terminal output prop; rate-limit/invoke-result note branches no longer render unsupported backend events.
@@ -144,14 +144,12 @@ Phase 2–4 + CodeGraph đều `Implemented`. Vault onboarding UX và Sync confl
 - MCP: Implemented runtime (socket host, stdio proxy, tools, install for
   `claude-code`/`codex` × `global`/`project`). Resources / Streamable HTTP
   deferred. Not production-verified against a real Claude Code session in CI.
-- Agent Hub is **Implemented** (Phase 4 Claude-only): ACP + service +
-  Claude + bridge/UI + `agenthub.*` MCP tools. Codex Layer 1 (hooks → MCP,
-  observation only, no spawn, no permission answers) is Implemented —
-  `internal/codexhook` + `internal/codexsessions`; running Codex *inside*
-  Agent Hub (Layer 2) is still Design, now leaning toward Codex's own
-  app-server JSON-RPC after a partial Phase 0 measurement (2026-09-20, no
-  full authenticated turn yet). Gemini adapter still Design (neither
-  required to close Phase 4). Not claimed production-verified against
+- Agent Hub is **Implemented**: ACP + service + Claude + bridge/UI +
+  `agenthub.*` MCP tools. Codex Layer 1 (hooks → MCP, observation only) is
+  Implemented — `internal/codexhook` + `internal/codexsessions`; Codex Layer 2
+  (running *inside* Agent Hub through `codex app-server` 0.147.0, in-app
+  sign-in, API key from Accounts) is Implemented as of 2026-09-25, live-tested
+  on macOS only. Gemini adapter still Design. Not claimed production-verified against
   real Claude Code in CI.
 - External plugin runtime is still design-only (**Phase 6**; marketplace
   **Phase 7** — after Workbench UI Phase 5).
diff --git a/docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md b/docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md
index f2bcde647c2f0fbc9aa4371084ed14e1e0aba920..26514ac7348a8cb18cf34a0b282c4edb25520f2b 100644
--- a/docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md
+++ b/docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md
@@ -260,6 +260,15 @@ OpenAI. Đăng nhập trình duyệt thì chạy được ngay từ trong bwrap.
   `AgentHubCancelSignIn`, `AgentHubSignOut`; event `agenthub:signInCompleted`.
 - Capability `agenthub:execute` trên `auth:codex`, **chỉ UI**. Không có tool MCP đăng nhập.
 - Audit ghi id account + phương thức; **không bao giờ** ghi mã một lần hay key.
+- **Đã làm ở Kế hoạch 3:** "chỉ UI" không chỉ là thiếu tool — `Service` từ chối caller đã được
+  gateway đóng dấu là agent hoặc CLI trên socket MCP chung (`ErrPermissionDenied`), nên một cửa
+  mở nhầm về sau vẫn không dẫn tới đâu; `internal/mcp` có test chặn tool/lời gọi sign-in.
+  API key: `preferences` lưu id (`agent_codex_api_key_account`), `app.CodexAPIKey` resolve lúc
+  spawn, chọn một account không dùng được (không phải `token`, scoped, giới hạn env, không tồn tại)
+  bị từ chối ngay ở Settings, và Accounts không cho xoá account đang được chọn. Dòng `external`
+  "Codex (ChatGPT)" (provider `openai`) được ghi khi `account/login/completed` báo thành công và
+  xoá khi Sign out. `agent_not_installed`/`agent_auth_required` nói đúng vendor qua
+  `agenthub.VendorError`.
 
 ## 7. Duyệt quyền và chế độ
 
```

- [ ] **Step 2: Chạy, xác nhận qua**

Run: `git diff --stat HEAD~1`
Expected: Chỉ năm file tài liệu.

- [ ] **Step 3: Commit**

```bash
git add AGENTS.md README.md docs/dev-kit/agent-hub.mdx docs/dev-kit/implementation-status.mdx docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md
git commit -m "docs: Codex runs in Agent Hub — sign-in, API key, vendor table

<attribution>"
```

## Kết thúc

- [ ] `go test ./internal/sandbox/ ./internal/codexapp/ ./internal/agentruntime/ ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ ./internal/arch/ ./internal/app/ ./internal/accounts/ ./internal/preferences/ -count=1 -race`, `go test ./internal/... ./cmd/... -count=1`, `go vet -tags server ./...`, `gofmt -l internal main.go` (bỏ qua `internal/crypto/keyring.go`), `go build -tags server -o /dev/null .` — sạch.
- [ ] `cd frontend && bun run build && bun test` — xanh. Ghi chú: `src/lib/debouncedWriter.test.ts` "the last write to a key inside the window wins" thỉnh thoảng hỏng khi chạy cả bộ (`window is not defined`) và qua khi chạy riêng — có sẵn trên `main` `05ef2f1`, không do kế hoạch này.
- [ ] So cây: `git diff --stat <commit nháp N> <commit task N>` không khác gì ngoài file plan.
- [ ] Review tổng (opus) cho khoảng commit của kế hoạch này; sửa những gì review tìm ra (kèm test, thử phá từng sửa).
- [ ] Cập nhật `docs/superpowers/HANDOFF-codex.md`.
- [ ] Smoke thủ công trên app desktop (người dùng làm; tốn quota Codex — hỏi trước): đăng nhập; một lượt Ask; Full auto + một lệnh prod qua MCP phải bị hỏi; resume sau khởi động lại.
