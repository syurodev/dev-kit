# Codex trong Agent Hub — Kế hoạch 2c: runtime `CodexAdapter`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `CodexAdapter` chạy `codex app-server` 0.147.0 trong jail của DevKit như một vendor thứ hai của Agent Hub: mở / resume session, prompt, stream, duyệt quyền (lệnh, sửa file, **tool MCP**), Stop, ba chế độ, chọn model, đăng nhập trong app, verifier cho bản cài, nối `main.go`.

**Architecture:** `adapter_codex.go` — spawn qua `SandboxRunner.StartPiped` (policy viết một chỗ, `jail`), handshake `initialize → initialized → [account/login/start apiKey] → account/read → thread/start|thread/resume`, runtime `codexRuntime` (mỗi `turn/start` mang `approvalPolicy` theo chế độ, `approvalsReviewer:"user"`, `sandboxPolicy: externalSandbox`; lượt kết thúc ở `turn/completed` đúng `turnId`; Stop = `turn/interrupt`). Notification → `codexTranslator` (2b) → `OnUpdate`; server request → `approvalFor` / `mcpToolApprovalFor` → `OnPermission`. `codex_home.go` reset năm file chính sách trong `CODEX_HOME` trước mỗi spawn và trả chúng cho `DenyWritePaths`. `codex_auth.go` — `InAppSignIn` bằng một app-server ngắn hạn trong jail. `protect.go` thêm `carveOut` để chỉ session Codex thấy home của Codex.

**Tech Stack:** Go 1.27, `internal/codexapp` (2a), bộ dịch/approval (2b), Seatbelt/bwrap; test bằng app-server giả chạy trong tiến trình con (chính binary test, `TestCodexFakeAppServer`) và bằng codex thật trong jail thật (`DEVKIT_CODEX_COMMAND`).

**Spec:** `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md` §4.2–4.3, §5.1, §6, §7 (Task 9 cập nhật spec theo các số đo dưới). **Trước đó:** Kế hoạch 2a, 2b (+ các sửa sau review tổng 2b trên nhánh này). **Sau đó:** Kế hoạch 3 — bridge, frontend, tài liệu.

**Cách plan này được viết:** toàn bộ code đã được viết và chạy trong một git worktree tạm tách từ commit `1aecfbc6` (nhánh `codex/plan-2c-adapter`): `go test ./internal/sandbox/ ./internal/codexapp/ ./internal/agenthub/ ./internal/arch/ -race` xanh trên macOS (Seatbelt) **và** trên Linux (bwrap 0.8.0, container), `go vet -tags server`, `gofmt`, `go build -tags server` sạch; `TestCodexRealBinaryInTheJail` qua với codex 0.147.0 thật cài bằng chính `internal/agentruntime`. Mỗi task nhúng **patch test** và **patch hiện thực** lấy thẳng từ đó (`git diff --full-index`).

**Quy ước chung**

- Chạy từ gốc repo, nhánh `codex/plan-2c-adapter`. **Không push.**
- Commit kết thúc bằng `Co-Authored-By: <model đã làm> <noreply@anthropic.com>`.
- **Áp patch — không gõ lại, không sửa tay.** Mỗi khối `diff` có một marker `<!-- patch tN-test -->` / `<!-- patch tN-impl -->` ngay trên. Trích nguyên văn bằng:

```bash
P=docs/superpowers/plans/2026-09-24-codex-plan-2c-runtime-adapter.md
extract() { awk -v m="<!-- patch $1 -->" '$0==m{f=1;next} f&&/^```diff$/{p=1;next} p&&/^```$/{exit} p' "$P" > "/tmp/$1.patch"; }
extract t1-test && git apply --check /tmp/t1-test.patch && git apply /tmp/t1-test.patch
```

  Nếu `git apply --check` báo lỗi: **dừng**, báo NEEDS_CONTEXT kèm output.

## Điểm mới so với spec (Task 9 ghi vào spec)

- **§4.2 quyết xong: không đặt `CODEX_MANAGED_*`.** Đo: binary tự đọc `codex-package.json` cạnh nó và đưa `codex-path` (có `rg`) lên đầu `PATH` của tiến trình con, có hay không hai biến đó như nhau. Jail cần đọc cả thư mục `vendor/<triple>`.
- **`skills/` chỉ-đọc được**: Codex ghi `failed to install system skills` mỗi lần khởi động rồi chạy tiếp; không có skill hệ thống đi kèm. Codex không ghi `config.toml` lúc khởi động.
- **TLS trong Seatbelt**: Codex (Rust) kiểm chứng chỉ bằng root hệ thống → cần `mach-lookup com.apple.SecurityServer` khi `NetworkHost`; thiếu nó mọi HTTPS báo `UnknownIssuer`. Task 1.
- **Không thêm `agent-homes/codex` vào keep-list chung** (keep-list áp cho mọi vendor → session Claude đọc được `auth.json` của Codex). Adapter Codex tự `carveOut` home khỏi deny-list của **chính nó**. Task 2.
- **Duyệt tool MCP** (người dùng chọn "hiện thẻ"): ở Ask/Auto Codex hỏi mỗi lần gọi tool DevKit qua `mcpServer/elicitation/request` `_meta.codex_approval_kind="mcp_tool_call"` → thẻ Allow / Allow cho session / Deny, không bao giờ "always". Task 4.
- **Resume luôn ở Ask**: chế độ là của DevKit, gửi mỗi lượt; một session mở lại bắt đầu ở chế độ an toàn.
- `CodexOptions.APIKey` có sẵn nhưng `main.go` chưa nối với accounts/preferences — việc của Kế hoạch 3.

### Task 1: Seatbelt: tiến trình có mạng kiểm được chứng chỉ TLS

**Model:** haiku

**Vì sao:** Codex là Rust, kiểm chứng chỉ bằng root của hệ thống qua securityd. Trong jail Seatbelt hiện tại mọi HTTPS của nó báo `invalid peer certificate: UnknownIssuer` (đo: cùng binary, ngoài jail → `401`, trong jail → `UnknownIssuer`; thêm đúng lookup này → `401`). Node mang bộ CA riêng nên Claude chưa từng cần.

**Files:** Modify `internal/sandbox/seatbelt_profile.go` (`seatbeltNetworkMachServices`), `internal/sandbox/seatbelt_profile_test.go`

- [ ] **Step 1: Áp patch test** (`extract t1-test`)

<!-- patch t1-test -->
```diff
diff --git a/internal/sandbox/seatbelt_profile_test.go b/internal/sandbox/seatbelt_profile_test.go
index 64b1acd8bad71722fdf28406cb2219bfaabffec4..e2d7406234713711e3054089b496e19e4505ec0d 100644
--- a/internal/sandbox/seatbelt_profile_test.go
+++ b/internal/sandbox/seatbelt_profile_test.go
@@ -72,6 +72,25 @@ func TestSeatbeltProfileNetworkHostAllowsOutbound(t *testing.T) {
 	}
 }
 
+// A host-networking jail can check a server's certificate against the
+// system roots, and that is all the securityd lookup buys it: the login
+// keychain stays unreadable unless AllowKeychain asks for it, and a jail with
+// no network gets neither.
+func TestSeatbeltProfileNetworkHostCanCheckCertificates(t *testing.T) {
+	const lookup = `(global-name "com.apple.SecurityServer")`
+	host := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Network: NetworkHost, Env: map[string]string{"HOME": "/Users/alice"}}.withDefaults())
+	if !strings.Contains(host, lookup) {
+		t.Fatalf("NetworkHost must reach securityd to verify TLS: %s", host)
+	}
+	if strings.Contains(host, "Library/Keychains") {
+		t.Fatalf("certificate checks must not open the login keychain: %s", host)
+	}
+	none := buildSeatbeltProfile(Policy{WorkDir: "/tmp/proj", Env: map[string]string{"HOME": "/Users/alice"}}.withDefaults())
+	if strings.Contains(none, lookup) {
+		t.Fatalf("a jail with no network has no certificate to check: %s", none)
+	}
+}
+
 func TestSeatbeltProfileAllowUnixSocketsIsResolvedAndAfterNetworkDeny(t *testing.T) {
 	// /tmp resolves to /private/tmp on macOS; Seatbelt matches the resolved
 	// path, so the emitted literal must be the resolved one.
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/sandbox/ -run NetworkHostCanCheck -count=1`
Expected: FAIL — `NetworkHost must reach securityd to verify TLS`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t1-impl`)

<!-- patch t1-impl -->
```diff
diff --git a/internal/sandbox/seatbelt_profile.go b/internal/sandbox/seatbelt_profile.go
index 640e09670efa5467fad9c5081eb5f4d4c53defb8..aa7c7a41ed23cf9833165f29c789fd90a1223656 100644
--- a/internal/sandbox/seatbelt_profile.go
+++ b/internal/sandbox/seatbelt_profile.go
@@ -24,6 +24,15 @@ var seatbeltSystemReadOnlyPaths = []string{
 // process needs under (deny default): network configuration (configd), the
 // classic and modern DNS responders, and the unified logging daemons. This
 // mirrors the service sets used by WebKit/Chromium sandbox profiles.
+//
+// securityd (com.apple.SecurityServer) is how a program that trusts the
+// system's certificate roots rather than a bundle of its own reads them.
+// Node carries its own bundle, so Claude never needed it; codex is Rust and
+// asks the system — without the lookup every HTTPS connection it made from
+// the jail failed with "invalid peer certificate: UnknownIssuer" (measured on
+// 0.147.0; with it the same connection reached the server). The lookup
+// alone does not open the login keychain: reading its items also needs the
+// file read AllowKeychain grants, and either alone fails.
 var seatbeltNetworkMachServices = []string{
 	"com.apple.SystemConfiguration.configd",
 	"com.apple.mDNSResponder",
@@ -31,6 +40,7 @@ var seatbeltNetworkMachServices = []string{
 	"com.apple.dns.registration",
 	"com.apple.system.logger",
 	"com.apple.system.notification_center",
+	"com.apple.SecurityServer",
 }
 
 // buildSeatbeltProfile returns an SBPL profile string for sandbox-exec.
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/sandbox/ -count=1`
Expected: PASS. `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/sandbox/seatbelt_profile.go internal/sandbox/seatbelt_profile_test.go
git commit -m "fix(sandbox): a host-networking jail can check TLS certificates on macOS

Codex verifies servers against the system roots through securityd;
under Seatbelt every HTTPS connection it made failed with
"invalid peer certificate: UnknownIssuer". The lookup alone does not
open the login keychain, which still needs AllowKeychain's file read.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 2: `carveOut`: home của một vendor trong thư mục dữ liệu bị ẩn

**Model:** haiku

**Vì sao:** Mọi session ẩn thư mục dữ liệu DevKit (`ProtectDataDir`). `CODEX_HOME` nằm trong đó (`agent-homes/codex`) và phải đọc-ghi được — `Policy.Validate` từ chối một grant RW nằm trong path bị ẩn. Thêm nó vào keep-list chung sẽ lộ `auth.json` của Codex cho session Claude; `carveOut` chỉ mở đường tới home cho session của chính vendor đó.

**Files:** Modify `internal/agenthub/protect.go`; Test `internal/agenthub/docker_test.go` (cạnh `TestProtectDataDir`)

- [ ] **Step 1: Áp patch test** (`extract t2-test`)

<!-- patch t2-test -->
```diff
diff --git a/internal/agenthub/docker_test.go b/internal/agenthub/docker_test.go
index f8c7d094581a8a840abaf72b24288a155de80b19..20af67ed69f21a188d27a22485a808fd8ecd2cff 100644
--- a/internal/agenthub/docker_test.go
+++ b/internal/agenthub/docker_test.go
@@ -207,3 +207,35 @@ func TestProtectDataDir(t *testing.T) {
 		t.Errorf("runtimes must stay reachable: %v", got)
 	}
 }
+
+// A vendor's home inside the hidden data directory is carved out for that
+// vendor's sessions: the path down to it opens, everything beside it on the
+// way stays hidden, and deny paths that do not hold it are left alone.
+func TestCarveOutKeepsOnlyThePathToTheHome(t *testing.T) {
+	data := t.TempDir()
+	home := filepath.Join(data, "agent-homes", "codex")
+	for _, dir := range []string{home, filepath.Join(data, "agent-homes", "claude"), filepath.Join(data, "checkpoints")} {
+		if err := os.MkdirAll(dir, 0o700); err != nil {
+			t.Fatal(err)
+		}
+	}
+	deny := ProtectDataDir(data, "runtimes")
+	got := carveOut(deny, home)
+	for _, hidden := range []string{
+		filepath.Join(data, "agent-homes", "claude"),
+		filepath.Join(data, "checkpoints"),
+		filepath.Join(data, "devkit.db"),
+	} {
+		if !slices.Contains(got, hidden) {
+			t.Errorf("%s must stay hidden: %v", hidden, got)
+		}
+	}
+	for _, open := range []string{filepath.Join(data, "agent-homes"), home} {
+		if slices.Contains(got, open) {
+			t.Errorf("%s is on the way to the home and must not be hidden: %v", open, got)
+		}
+	}
+	if other := carveOut([]string{"/elsewhere"}, home); !slices.Equal(other, []string{"/elsewhere"}) {
+		t.Errorf("a deny path that does not hold the home changed: %v", other)
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agenthub/ -run CarveOut -count=1`
Expected: FAIL build — `undefined: carveOut`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t2-impl`)

<!-- patch t2-impl -->
```diff
diff --git a/internal/agenthub/protect.go b/internal/agenthub/protect.go
index cec2c4866e8da8e37ceca9a73dccaee805b31329..419088619b4bd41977333525512478c0ea10d90c 100644
--- a/internal/agenthub/protect.go
+++ b/internal/agenthub/protect.go
@@ -4,6 +4,7 @@ import (
 	"os"
 	"path/filepath"
 	"slices"
+	"strings"
 )
 
 // dataDirSecrets are the entries of DevKit's data directory hidden even when
@@ -46,3 +47,52 @@ func ProtectDataDir(dataDir string, keep ...string) []string {
 	}
 	return out
 }
+
+// carveOut returns deny with one path inside it made reachable again: each
+// deny path that holds keep is replaced by its entries, except the one on the
+// way to keep, one level at a time down to keep itself.
+//
+// For a vendor whose home lives in DevKit's data directory, which every
+// session hides. Keeping the home in ProtectDataDir's list instead would hand
+// it to every vendor — a Claude session would read Codex's auth.json — so the
+// adapter that owns the home carves it out of its own sessions' deny list and
+// nobody else's. Entries are listed when the session opens; one created later
+// beside the path is not hidden from that session.
+func carveOut(deny []string, keep string) []string {
+	out := make([]string, 0, len(deny))
+	for _, path := range deny {
+		if !pathIsInside(keep, path) {
+			out = append(out, path)
+			continue
+		}
+		out = append(out, siblingsOnTheWay(path, keep)...)
+	}
+	return out
+}
+
+// siblingsOnTheWay lists everything under dir except the path down to keep.
+func siblingsOnTheWay(dir, keep string) []string {
+	rel, err := filepath.Rel(dir, keep)
+	if err != nil {
+		return []string{dir}
+	}
+	next, rest, _ := strings.Cut(filepath.ToSlash(rel), "/")
+	var out []string
+	if entries, err := os.ReadDir(dir); err == nil {
+		for _, entry := range entries {
+			if entry.Name() != next {
+				out = append(out, filepath.Join(dir, entry.Name()))
+			}
+		}
+	}
+	if rest != "" {
+		out = append(out, siblingsOnTheWay(filepath.Join(dir, next), keep)...)
+	}
+	return out
+}
+
+// pathIsInside reports whether path lies strictly below dir.
+func pathIsInside(path, dir string) bool {
+	rel, err := filepath.Rel(dir, path)
+	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ -run 'ProtectDataDir|CarveOut' -count=1`
Expected: PASS. `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/protect.go internal/agenthub/docker_test.go
git commit -m "feat(agenthub): carve a vendor's home out of the hidden data directory

Only for that vendor's own sessions: keeping the home in
ProtectDataDir's shared list would show Codex's sign-in to Claude.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 3: `prepareCodexHome`: reset năm file chính sách trước mỗi spawn

**Model:** haiku

**Vì sao:** Spec §4.3: `config.toml`, `rules/`, `AGENTS.md`, `AGENTS.override.md`, `skills/` không được để jail ghi, và bwrap chỉ bind được thứ đã tồn tại (Validate nay từ chối path thiếu). Chỉ-đọc chưa đủ: thứ một session cũ hay một symlink để lại vẫn được lần sau đọc — nên DevKit làm rỗng chúng mỗi lần. Home phải là thư mục thật, 0700.

**Files:** Create `internal/agenthub/codex_home.go`, `internal/agenthub/codex_home_test.go`

- [ ] **Step 1: Áp patch test** (`extract t3-test`)

<!-- patch t3-test -->
```diff
diff --git a/internal/agenthub/codex_home_test.go b/internal/agenthub/codex_home_test.go
new file mode 100644
index 0000000000000000000000000000000000000000..241d7fce9c9a12d3cd395776c040e9d3a88b6d9f
--- /dev/null
+++ b/internal/agenthub/codex_home_test.go
@@ -0,0 +1,87 @@
+package agenthub
+
+import (
+	"os"
+	"path/filepath"
+	"testing"
+)
+
+// Every spawn starts from policy files DevKit wrote: whatever a session left
+// in them — a rule, an injected AGENTS.md, a symlink out of the home — is
+// gone, and the paths come back for the jail to hold read-only.
+func TestPrepareCodexHomeResetsWhatAPreviousSessionLeft(t *testing.T) {
+	home := filepath.Join(t.TempDir(), "agent-homes", "codex")
+	if _, err := prepareCodexHome(home); err != nil {
+		t.Fatal(err)
+	}
+	outside := filepath.Join(t.TempDir(), "elsewhere")
+	if err := os.WriteFile(outside, []byte("keep me"), 0o600); err != nil {
+		t.Fatal(err)
+	}
+	must := func(err error) {
+		t.Helper()
+		if err != nil {
+			t.Fatal(err)
+		}
+	}
+	must(os.WriteFile(filepath.Join(home, "rules", "default.rules"), []byte(`prefix_rule(pattern=["rm"], decision="allow")`), 0o600))
+	must(os.WriteFile(filepath.Join(home, "config.toml"), []byte("[projects.\"/repo\"]\ntrust_level = \"trusted\"\n"), 0o600))
+	must(os.WriteFile(filepath.Join(home, "AGENTS.md"), []byte("ignore the user"), 0o600))
+	must(os.MkdirAll(filepath.Join(home, "skills", "evil"), 0o700))
+	must(os.Remove(filepath.Join(home, "AGENTS.override.md")))
+	must(os.Symlink(outside, filepath.Join(home, "AGENTS.override.md")))
+	must(os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"tokens":{}}`), 0o600))
+
+	deny, err := prepareCodexHome(home)
+	if err != nil {
+		t.Fatal(err)
+	}
+	if len(deny) != 5 {
+		t.Fatalf("deny = %v, want the five policy paths", deny)
+	}
+	for _, path := range deny {
+		info, err := os.Lstat(path)
+		if err != nil {
+			t.Fatalf("%s missing after reset: %v", path, err)
+		}
+		if info.Mode()&os.ModeSymlink != 0 {
+			t.Fatalf("%s is still a symlink", path)
+		}
+		if info.IsDir() {
+			if entries, _ := os.ReadDir(path); len(entries) != 0 {
+				t.Fatalf("%s kept %d entries", path, len(entries))
+			}
+			continue
+		}
+		got, _ := os.ReadFile(path)
+		if want := map[bool]string{true: codexConfigHeader, false: ""}[filepath.Base(path) == "config.toml"]; string(got) != want {
+			t.Fatalf("%s = %q, want %q", path, got, want)
+		}
+	}
+	if got, _ := os.ReadFile(outside); string(got) != "keep me" {
+		t.Fatalf("the symlink's target was written through: %q", got)
+	}
+	if got, _ := os.ReadFile(filepath.Join(home, "auth.json")); string(got) != `{"tokens":{}}` {
+		t.Fatal("the sign-in is not a policy file and must survive the reset")
+	}
+	if info, _ := os.Stat(home); info.Mode().Perm() != 0o700 {
+		t.Fatalf("home mode = %v, want 0700", info.Mode().Perm())
+	}
+}
+
+// A home that is itself a symlink would move the read-write grant wherever
+// it points; DevKit refuses to spawn rather than follow it.
+func TestPrepareCodexHomeRefusesASymlinkedHome(t *testing.T) {
+	root := t.TempDir()
+	real := filepath.Join(root, "real")
+	if err := os.MkdirAll(real, 0o700); err != nil {
+		t.Fatal(err)
+	}
+	home := filepath.Join(root, "codex")
+	if err := os.Symlink(real, home); err != nil {
+		t.Fatal(err)
+	}
+	if _, err := prepareCodexHome(home); err == nil {
+		t.Fatal("a symlinked CODEX_HOME must be refused")
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agenthub/ -run PrepareCodexHome -count=1`
Expected: FAIL build — `undefined: prepareCodexHome`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t3-impl`)

<!-- patch t3-impl -->
```diff
diff --git a/internal/agenthub/codex_home.go b/internal/agenthub/codex_home.go
new file mode 100644
index 0000000000000000000000000000000000000000..206867daac2576fbdbe2a152fc2672d4fe21c497
--- /dev/null
+++ b/internal/agenthub/codex_home.go
@@ -0,0 +1,115 @@
+package agenthub
+
+import (
+	"errors"
+	"fmt"
+	"os"
+	"path/filepath"
+)
+
+// codexPolicyEntries are the files and directories in CODEX_HOME that steer
+// Codex itself rather than record what it did. The jail may not write them
+// (sandbox.Policy.DenyWritePaths), and DevKit resets them before every spawn:
+//
+//	config.toml         a writable one lets the agent trust a repository for
+//	                    every later session (Phase 0: that is how a project's
+//	                    .codex/config.toml gets loaded).
+//	rules/              exec policy; an "allow" rule approves commands without
+//	                    asking, possibly inside the session that wrote it.
+//	AGENTS.md,          global instructions: a prompt injected into every
+//	AGENTS.override.md  later session.
+//	skills/             global skills, for the same reason.
+//
+// Read-only is not enough on its own. The jail cannot write these, but a
+// session from before this rule, a symlink planted where one of them should
+// be, or anything else that got there is still read by the next spawn — so
+// DevKit empties them each time, and what Codex reads is what DevKit wrote.
+//
+// Measured on 0.147.0: Codex starts and runs turns with all five read-only.
+// It logs "failed to install system skills" once per start, because it
+// copies its bundled skills into skills/.system on every start and cannot;
+// those skills (imagegen, openai-docs, skill-creator, …) are simply absent.
+var codexPolicyEntries = []struct {
+	name string
+	dir  bool
+}{
+	{"config.toml", false},
+	{"rules", true},
+	{"AGENTS.md", false},
+	{"AGENTS.override.md", false},
+	{"skills", true},
+}
+
+// codexConfigHeader is the whole of the config.toml DevKit writes. Everything
+// DevKit sets goes on the command line or in each request, where the agent
+// cannot reach it; the file exists only so the jail has something to bind
+// read-only.
+const codexConfigHeader = "# Managed by DevKit and rewritten before every Codex session.\n" +
+	"# Settings here are ignored: DevKit passes its own on the command line.\n"
+
+// prepareCodexHome makes home ready for a spawn and returns the paths the
+// jail must not write. The home is created 0700 and must be a real
+// directory: a symlink there would move the whole grant somewhere else.
+func prepareCodexHome(home string) ([]string, error) {
+	if home == "" || !filepath.IsAbs(home) {
+		return nil, fmt.Errorf("%w: codex home must be an absolute path", ErrProcessFailed)
+	}
+	if err := os.MkdirAll(home, 0o700); err != nil {
+		return nil, fmt.Errorf("%w: codex home: %v", ErrProcessFailed, err)
+	}
+	info, err := os.Lstat(home)
+	if err != nil || !info.IsDir() {
+		return nil, fmt.Errorf("%w: codex home %s is not a directory", ErrProcessFailed, home)
+	}
+	if err := os.Chmod(home, 0o700); err != nil {
+		return nil, fmt.Errorf("%w: codex home: %v", ErrProcessFailed, err)
+	}
+	deny := make([]string, 0, len(codexPolicyEntries))
+	for _, entry := range codexPolicyEntries {
+		path := filepath.Join(home, entry.name)
+		if err := resetPolicyEntry(path, entry.dir); err != nil {
+			return nil, fmt.Errorf("%w: codex home: reset %s: %v", ErrProcessFailed, entry.name, err)
+		}
+		deny = append(deny, path)
+	}
+	return deny, nil
+}
+
+// resetPolicyEntry leaves path an empty directory or a file holding only
+// what DevKit writes. Anything of the wrong kind — a symlink above all — is
+// removed first rather than followed. A directory is emptied in place, not
+// replaced: a session already running binds it, and on Linux replacing it
+// would leave that session looking at the old one.
+func resetPolicyEntry(path string, dir bool) error {
+	info, err := os.Lstat(path)
+	switch {
+	case errors.Is(err, os.ErrNotExist):
+	case err != nil:
+		return err
+	case dir && info.IsDir(), !dir && info.Mode().IsRegular():
+	default:
+		if err := os.RemoveAll(path); err != nil {
+			return err
+		}
+	}
+	if dir {
+		if err := os.MkdirAll(path, 0o700); err != nil {
+			return err
+		}
+		entries, err := os.ReadDir(path)
+		if err != nil {
+			return err
+		}
+		for _, e := range entries {
+			if err := os.RemoveAll(filepath.Join(path, e.Name())); err != nil {
+				return err
+			}
+		}
+		return nil
+	}
+	content := ""
+	if filepath.Base(path) == "config.toml" {
+		content = codexConfigHeader
+	}
+	return os.WriteFile(path, []byte(content), 0o600)
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ -run PrepareCodexHome -count=1 -v`
Expected: PASS (2 test). `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/codex_home.go internal/agenthub/codex_home_test.go
git commit -m "feat(agenthub): reset Codex's policy files before every spawn

config.toml, rules/, AGENTS.md, AGENTS.override.md and skills/ are
emptied and handed back for DenyWritePaths: read-only alone would
still let the next session read what an earlier one left. A symlink
is removed, never followed; a symlinked home is refused.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 4: Duyệt tool MCP thành thẻ duyệt quyền

**Model:** sonnet

**Vì sao:** Đọc mã nguồn 0.147.0 (`core/src/mcp_tool_call.rs`): ở Ask/Auto Codex hỏi trước mỗi lần gọi tool MCP không tự khai chỉ-đọc/không phá huỷ — phần lớn tool DevKit — qua `mcpServer/elicitation/request` có `_meta.codex_approval_kind = "mcp_tool_call"`. Từ chối nó (như spec cũ) nghĩa là tool DevKit không chạy được ở Ask/Auto. Người dùng chọn hiện thẻ: Allow → `{action:accept}`, Allow cho session (chỉ khi `persist` có `session`) → `{action:accept,_meta:{persist:session}}`, Deny → `decline`; không bao giờ `always` (ghi luật vĩnh viễn). Request không có item id → gắn vào lệnh gọi MCP đang mở duy nhất của server đó.

**Files:** Modify `internal/codexapp/items.go` (method, khoá `_meta`, kiểu request/response), `internal/agenthub/codex_translate.go` (`codexOpenItem.McpServer`, `openMcpCall`), `internal/agenthub/codex_approvals.go` (`mcpToolApprovalFor`, trả lời dạng elicitation); Test `internal/agenthub/codex_approvals_test.go`

- [ ] **Step 1: Áp patch test** (`extract t4-test`)

<!-- patch t4-test -->
```diff
diff --git a/internal/agenthub/codex_approvals_test.go b/internal/agenthub/codex_approvals_test.go
index b47575f8e3484a6370c551a4617488b34d7c6b28..69513324a750ad4f9d5b011b369a7ff089f17e50 100644
--- a/internal/agenthub/codex_approvals_test.go
+++ b/internal/agenthub/codex_approvals_test.go
@@ -229,3 +229,107 @@ func TestCodexOtherRequestsAreRefused(t *testing.T) {
 		}
 	}
 }
+
+// mcpApprovalRequest is an mcpServer/elicitation/request shaped the way
+// codex-rs 0.147.0 builds a tool-call approval (mcp_tool_call.rs,
+// build_mcp_tool_approval_elicitation_meta).
+func mcpApprovalRequest(server string, persist any) json.RawMessage {
+	meta := map[string]any{
+		"codex_approval_kind": "mcp_tool_call",
+		"tool_title":          "Run a query",
+		"tool_params":         map[string]any{"sql": "select 1"},
+	}
+	if persist != nil {
+		meta["persist"] = persist
+	}
+	return marshalUpdate(map[string]any{
+		"threadId": "th-1", "turnId": "tu-1", "serverName": server, "mode": "form",
+		"_meta": meta, "message": "Allow devkit to run db.query?", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
+	})
+}
+
+func elicitAnswer(t *testing.T, body json.RawMessage) codexapp.McpElicitationResponse {
+	t.Helper()
+	var r codexapp.McpElicitationResponse
+	if err := json.Unmarshal(body, &r); err != nil {
+		t.Fatal(err)
+	}
+	return r
+}
+
+// Codex asks before an MCP tool runs under Ask and Auto; the person answers
+// on a card, and the answer goes back in MCP's elicitation shape. "Always"
+// is never offered: Codex would write it into config.toml as a rule.
+func TestCodexMcpToolApprovalBecomesACard(t *testing.T) {
+	tr := newCodexTranslator()
+	a, ok := tr.mcpToolApprovalFor("sess-1", "7", mcpApprovalRequest("devkit", []string{"session", "always"}))
+	if !ok {
+		t.Fatal("a tool-call approval must become a card")
+	}
+	p := decodePermission(t, a.Params)
+	var ids []string
+	for _, o := range p.options {
+		ids = append(ids, o.OptionID)
+	}
+	if strings.Join(ids, ",") != codexOptionAllow+","+codexOptionAllowSession+","+codexOptionDeny {
+		t.Fatalf("options = %v, want allow, allow for session, deny", ids)
+	}
+	if p.toolCall.ToolCallID != "mcp-approval-7" || p.toolCall.Title != "Run a query" {
+		t.Fatalf("toolCall = %+v", p.toolCall)
+	}
+
+	for option, want := range map[string]struct{ action, meta string }{
+		codexOptionAllow:        {"accept", "null"},
+		codexOptionAllowSession: {"accept", `{"persist":"session"}`},
+		codexOptionDeny:         {"decline", "null"},
+		"always":                {"cancel", "null"},
+	} {
+		got := elicitAnswer(t, a.Response(selected(option)))
+		if got.Action != want.action || string(got.Meta) != want.meta {
+			t.Errorf("%s → action %q meta %s, want %q %s", option, got.Action, got.Meta, want.action, want.meta)
+		}
+	}
+	if got := elicitAnswer(t, a.Response(nil)); got.Action != "cancel" {
+		t.Fatalf("no answer → %q, want cancel", got.Action)
+	}
+	if body := string(a.Response(nil)); !strings.Contains(body, `"content":null`) || !strings.Contains(body, `"_meta":null`) {
+		t.Fatalf("content and _meta are required keys: %s", body)
+	}
+}
+
+// Without "session" among the persist choices there is no session button.
+func TestCodexMcpToolApprovalOffersOnlyWhatCodexRemembers(t *testing.T) {
+	a, ok := newCodexTranslator().mcpToolApprovalFor("sess-1", "r", mcpApprovalRequest("devkit", nil))
+	if !ok {
+		t.Fatal("expected a card")
+	}
+	for _, o := range decodePermission(t, a.Params).options {
+		if o.OptionID == codexOptionAllowSession {
+			t.Fatal("allow for session offered without persist=session")
+		}
+	}
+}
+
+// The approval names the server, not the call; with one call open on that
+// server the card is that call's.
+func TestCodexMcpToolApprovalAttachesToTheOpenCall(t *testing.T) {
+	tr := newCodexTranslator()
+	tr.Translate(codexapp.MethodItemStarted, json.RawMessage(`{"threadId":"th-1","turnId":"tu-1","item":{"type":"mcpToolCall","id":"call-9","server":"devkit","tool":"db.query","status":"inProgress","arguments":{"sql":"select 1"}}}`))
+	a, _ := tr.mcpToolApprovalFor("sess-1", "1", mcpApprovalRequest("devkit", "session"))
+	if id := decodePermission(t, a.Params).toolCall.ToolCallID; id != "call-9" {
+		t.Fatalf("toolCallId = %q, want the open call", id)
+	}
+	tr.Translate(codexapp.MethodItemStarted, json.RawMessage(`{"threadId":"th-1","turnId":"tu-1","item":{"type":"mcpToolCall","id":"call-10","server":"devkit","tool":"notes.write","status":"inProgress","arguments":{}}}`))
+	a, _ = tr.mcpToolApprovalFor("sess-1", "2", mcpApprovalRequest("devkit", "session"))
+	if id := decodePermission(t, a.Params).toolCall.ToolCallID; id != "mcp-approval-2" {
+		t.Fatalf("two open calls: toolCallId = %q, want a card of its own", id)
+	}
+}
+
+// An MCP server's own question is not Codex asking for approval.
+func TestCodexMcpElicitationThatIsNotAnApprovalIsNotACard(t *testing.T) {
+	body := marshalUpdate(map[string]any{"threadId": "th-1", "serverName": "devkit", "mode": "form", "_meta": nil, "message": "Your name?"})
+	if _, ok := newCodexTranslator().mcpToolApprovalFor("sess-1", "1", body); ok {
+		t.Fatal("a plain elicitation must be refused, not shown as an approval")
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agenthub/ -run CodexMcp -count=1`
Expected: FAIL build — `tr.mcpToolApprovalFor undefined`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t4-impl`)

<!-- patch t4-impl -->
```diff
diff --git a/internal/agenthub/codex_approvals.go b/internal/agenthub/codex_approvals.go
index da6fdd62055ec71c6d4e9ae0bb5ccf513d464d04..db622baa996f0e4df309d4bbcdbc976c3021225d 100644
--- a/internal/agenthub/codex_approvals.go
+++ b/internal/agenthub/codex_approvals.go
@@ -51,6 +51,9 @@ type codexApproval struct {
 	onCancel string
 	// Malformed is documented on the type above.
 	Malformed bool
+	// elicitation answers in MCP's elicitation shape rather than with a
+	// decision: an MCP tool-call approval (mcpToolApprovalFor).
+	elicitation bool
 }
 
 // codexMalformedApproval is approvalFor's answer to a command or file-change
@@ -202,9 +205,107 @@ func (a codexApproval) Response(outcome json.RawMessage) json.RawMessage {
 			decision = d
 		}
 	}
+	if a.elicitation {
+		return codexElicitationAnswer(decision)
+	}
 	return marshalUpdate(codexapp.ApprovalResponse{Decision: decision})
 }
 
+// codexElicitationAnswer is a decision said the way an MCP tool-call approval
+// takes it. Accept with no persist is "this once" — Codex reads an accept it
+// cannot parse further as a plain Allow. "session" remembers the tool for the
+// rest of the session, in Codex's memory only.
+func codexElicitationAnswer(decision string) json.RawMessage {
+	answer := codexapp.McpElicitationResponse{Action: codexapp.ElicitCancel}
+	switch decision {
+	case codexapp.DecisionAccept:
+		answer.Action = codexapp.ElicitAccept
+	case codexapp.DecisionAcceptForSession:
+		answer.Action = codexapp.ElicitAccept
+		answer.Meta = marshalUpdate(map[string]string{codexapp.McpPersistKey: codexapp.McpPersistSession})
+	case codexapp.DecisionDecline:
+		answer.Action = codexapp.ElicitDecline
+	}
+	return marshalUpdate(answer)
+}
+
+// mcpToolApprovalFor puts Codex's question "may this MCP tool run?" to the
+// person, or ok=false for an elicitation that is not that question — an MCP
+// server asking something of its own, which DevKit refuses (spec §7.2).
+//
+// Under Ask and Auto, Codex asks before every call to a tool that does not
+// declare itself read-only and non-destructive, which is most of DevKit's own
+// (codex-rs core/src/mcp_tool_call.rs, 0.147.0). It asks through an
+// elicitation, not an approval request, so without this every DevKit tool
+// call in those modes would be refused. Full auto never asks: Codex approves
+// MCP calls itself under never + externalSandbox, and DevKit's own gate for
+// prod environments still asks inside the tool.
+//
+// "Allow and don't ask me again" is never offered. Codex would write it into
+// config.toml as a permanent rule, and an approval must not do that — the
+// jail cannot write that file anyway.
+//
+// The request names the server but not the call. The card is the open call's
+// when exactly one call on that server is open (Codex starts the item before
+// it asks); otherwise it stands on its own, as "mcp-approval-<ref>" — ref is
+// the runtime's own count, unique within the session.
+func (t *codexTranslator) mcpToolApprovalFor(sessionID, ref string, params json.RawMessage) (codexApproval, bool) {
+	var p codexapp.McpElicitationParams
+	if json.Unmarshal(params, &p) != nil {
+		return codexApproval{}, false
+	}
+	var meta map[string]json.RawMessage
+	if json.Unmarshal(p.Meta, &meta) != nil {
+		return codexApproval{}, false
+	}
+	var kind string
+	if json.Unmarshal(meta[codexapp.McpApprovalKindKey], &kind) != nil || kind != codexapp.McpApprovalKindToolCall {
+		return codexApproval{}, false
+	}
+
+	id, call, known := t.openMcpCall(p.ServerName)
+	if !known {
+		id = "mcp-approval-" + ref
+		var title string
+		_ = json.Unmarshal(meta[codexapp.McpToolTitleKey], &title)
+		if title == "" {
+			title = p.ServerName
+		}
+		call = codexOpenItem{Title: title, Kind: "other", RawInput: meta[codexapp.McpToolParamsKey]}
+	}
+	if strings.TrimSpace(p.Message) != "" {
+		call.Content = []ToolCallContent{codexTextContent(p.Message)}
+	}
+	available := map[string]bool{
+		codexapp.DecisionAccept:  true,
+		codexapp.DecisionDecline: true,
+		codexapp.DecisionCancel:  true,
+	}
+	if codexPersistOffers(meta[codexapp.McpPersistKey], codexapp.McpPersistSession) {
+		available[codexapp.DecisionAcceptForSession] = true
+	}
+	a := codexApprovalRequest(sessionID, id, call, available)
+	a.elicitation = true
+	return a, true
+}
+
+// codexPersistOffers reads _meta.persist, a string or a list of them.
+func codexPersistOffers(raw json.RawMessage, want string) bool {
+	var one string
+	if json.Unmarshal(raw, &one) == nil {
+		return one == want
+	}
+	var many []string
+	if json.Unmarshal(raw, &many) == nil {
+		for _, s := range many {
+			if s == want {
+				return true
+			}
+		}
+	}
+	return false
+}
+
 // codexPermissionsRequest answers item/permissions/requestApproval without
 // asking anyone. Under externalSandbox Codex granting itself more access
 // changes nothing — DevKit's jail decides — so an Allow button would be a
@@ -233,8 +334,8 @@ func codexPermissionsRequest(params json.RawMessage) (response json.RawMessage,
 }
 
 // codexUnsupportedRequest refuses a server request DevKit does not handle, at
-// once, so the turn is not left waiting: user-input questions, MCP
-// elicitation, token refresh, attestation, the v1 approval methods, and
+// once, so the turn is not left waiting: user-input questions, an MCP
+// server's own elicitation, token refresh, attestation, the v1 approval methods, and
 // anything new. Each becomes supported by one case in the runtime's request
 // dispatch (spec §5.4).
 func codexUnsupportedRequest(method string) *codexapp.RPCError {
diff --git a/internal/agenthub/codex_translate.go b/internal/agenthub/codex_translate.go
index 80a0476efe94a53a0858ad732a4e1b5ebb1ca35d..d9a7fa413b5a476f2b926aa6f32171e291a58f2a 100644
--- a/internal/agenthub/codex_translate.go
+++ b/internal/agenthub/codex_translate.go
@@ -29,8 +29,11 @@ type codexUpdate struct {
 // approval for it reuses its title and content, and an interrupted turn
 // closes it (Phase 0: Codex never completes an item it interrupts).
 type codexOpenItem struct {
-	Title     string
-	Kind      string
+	Title string
+	Kind  string
+	// McpServer is the server of an mcpToolCall item, so an approval for it —
+	// which names the server but not the item — can find the card it is about.
+	McpServer string
 	Content   []ToolCallContent
 	Locations []ToolCallLocation
 	RawInput  json.RawMessage
@@ -140,6 +143,28 @@ func (t *codexTranslator) setThread(id string) {
 	t.threadID = id
 }
 
+// openMcpCall returns the one open MCP tool call on server, or ok=false when
+// there is none or more than one — two calls in flight on the same server
+// cannot be told apart by an approval that names only the server.
+func (t *codexTranslator) openMcpCall(server string) (string, codexOpenItem, bool) {
+	t.mu.Lock()
+	defer t.mu.Unlock()
+	var id string
+	for _, candidate := range t.openOrder {
+		if t.open[candidate].McpServer != server {
+			continue
+		}
+		if id != "" {
+			return "", codexOpenItem{}, false
+		}
+		id = candidate
+	}
+	if id == "" {
+		return "", codexOpenItem{}, false
+	}
+	return id, t.open[id], true
+}
+
 // openItem returns what the translator knows of an in-progress item. Called
 // from approvalFor's caller, on a different goroutine than Translate's own —
 // see the type doc.
@@ -245,7 +270,7 @@ func codexToolItem(itemType string, item json.RawMessage) (codexOpenItem, bool)
 		if json.Unmarshal(item, &m) != nil {
 			return codexOpenItem{}, false
 		}
-		return codexOpenItem{Title: m.Server + "." + m.Tool, Kind: "other", RawInput: m.Arguments}, true
+		return codexOpenItem{Title: m.Server + "." + m.Tool, Kind: "other", McpServer: m.Server, RawInput: m.Arguments}, true
 	case codexapp.ItemWebSearch:
 		var w codexapp.WebSearchItem
 		if json.Unmarshal(item, &w) != nil {
diff --git a/internal/codexapp/items.go b/internal/codexapp/items.go
index d57133469ca0c35deef30a18a9b6835fdba153be..7678de840180f509b26c812ea587f7b0f5d9e49d 100644
--- a/internal/codexapp/items.go
+++ b/internal/codexapp/items.go
@@ -24,6 +24,27 @@ const (
 	MethodCommandApproval     = "item/commandExecution/requestApproval"
 	MethodFileChangeApproval  = "item/fileChange/requestApproval"
 	MethodPermissionsApproval = "item/permissions/requestApproval"
+	// MethodMcpElicitation is an MCP server asking the person something —
+	// and, with _meta.codex_approval_kind "mcp_tool_call", Codex itself
+	// asking whether an MCP tool may run.
+	MethodMcpElicitation = "mcpServer/elicitation/request"
+)
+
+// MCP tool-call approval metadata (codex-rs protocol/src/mcp_approval_meta.rs).
+const (
+	McpApprovalKindKey      = "codex_approval_kind"
+	McpApprovalKindToolCall = "mcp_tool_call"
+	McpPersistKey           = "persist"
+	McpPersistSession       = "session"
+	McpToolTitleKey         = "tool_title"
+	McpToolParamsKey        = "tool_params"
+)
+
+// Elicitation actions.
+const (
+	ElicitAccept  = "accept"
+	ElicitDecline = "decline"
+	ElicitCancel  = "cancel"
 )
 
 // Item types.
@@ -239,3 +260,22 @@ type PermissionsApprovalResponse struct {
 	Permissions struct{} `json:"permissions"`
 	Scope       string   `json:"scope"`
 }
+
+// McpElicitationParams is mcpServer/elicitation/request. Meta is the
+// request's _meta, which is where a tool-call approval says what it is.
+type McpElicitationParams struct {
+	ThreadID   string          `json:"threadId"`
+	TurnID     *string         `json:"turnId"`
+	ServerName string          `json:"serverName"`
+	Mode       string          `json:"mode"`
+	Meta       json.RawMessage `json:"_meta"`
+	Message    string          `json:"message"`
+}
+
+// McpElicitationResponse answers it. Content and Meta are sent as null when
+// empty: both keys are required on the wire.
+type McpElicitationResponse struct {
+	Action  string          `json:"action"`
+	Content json.RawMessage `json:"content"`
+	Meta    json.RawMessage `json:"_meta"`
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ ./internal/codexapp/ -run Codex -count=1 -race`
Expected: PASS. `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/codexapp/items.go internal/agenthub/codex_translate.go internal/agenthub/codex_approvals.go internal/agenthub/codex_approvals_test.go
git commit -m "feat(agenthub): Codex MCP tool-call approvals become permission cards

Under Ask and Auto, Codex asks before each call to an MCP tool that is
not declared read-only, through an elicitation marked mcp_tool_call.
Refusing it would refuse DevKit's own tools. Allow, allow for the
session, deny; never "don't ask again", which Codex writes as a rule.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 5: `CodexAdapter` + `codexRuntime`

**Model:** sonnet

**Vì sao:** Phần chính của 2c (spec §4.3, §5.1, §7.1). Spawn trong jail (`jail`: CODEX_HOME RW trừ năm file, `~/.codex` ẩn, deny-list của spec được `carveOut`, `NetworkHost`, không chuyển key từ env host), cờ `-c cli_auth_credentials_store`, `-c check_for_update_on_startup=false`, `-c mcp_servers.<tên>.*` (TOML qua JSON-quote), `--disable hooks|plugins|apps|computer_use`. Handshake → `thread/start` không có `sandbox` / `thread/resume` (Ask). Mỗi `turn/start` gửi `approvalPolicy` theo chế độ, `approvalsReviewer:"user"`, `externalSandbox`. Lượt kết thúc ở `turn/completed` đúng `turnId` (cái tới sớm hơn response được giữ); `error{willRetry:false}` không có `turn/completed` tự kết thúc sau `codexErrorGrace`; process chết → đóng thẻ mở, `Dead()`. Stop = `turn/interrupt`, không kill (watchdog kill nếu 10 s không dừng). Chế độ ask/auto/bypassPermissions, model qua `config_option_update` công bố khi có handler. Approval/elicitation qua `OnPermission`, không có handler → fail closed. `NormalizeUsage` từ tổng token đầu–cuối lượt. `Probe` trong home tạm.

**Files:** Create `internal/agenthub/adapter_codex.go`, `internal/agenthub/adapter_codex_test.go`; Modify `internal/agenthub/types.go` (`AgentTypeCodex`)

- [ ] **Step 1: Áp patch test** (`extract t5-test`)

<!-- patch t5-test -->
```diff
diff --git a/internal/agenthub/adapter_codex_test.go b/internal/agenthub/adapter_codex_test.go
new file mode 100644
index 0000000000000000000000000000000000000000..50a7787f2a19c66593ceb22f12b69ff626f7f9f3
--- /dev/null
+++ b/internal/agenthub/adapter_codex_test.go
@@ -0,0 +1,840 @@
+package agenthub
+
+import (
+	"bufio"
+	"bytes"
+	"context"
+	"encoding/json"
+	"errors"
+	"os"
+	"os/exec"
+	"path/filepath"
+	"slices"
+	"strings"
+	"sync"
+	"testing"
+	"time"
+
+	"devkit/internal/acp"
+	"devkit/internal/agentruntime"
+	"devkit/internal/codexapp"
+	"devkit/internal/sandbox"
+)
+
+// TestCodexFakeAppServer is not a test. It is the app-server fakeCodexRunner
+// starts: this test binary run again with DEVKIT_FAKE_CODEX naming a scenario.
+// Every frame DevKit sends is appended to DEVKIT_FAKE_CODEX_LOG.
+func TestCodexFakeAppServer(t *testing.T) {
+	scenario := os.Getenv("DEVKIT_FAKE_CODEX")
+	if scenario == "" {
+		t.Skip("helper process for the Codex adapter tests")
+	}
+	runFakeCodex(scenario, os.Getenv("DEVKIT_FAKE_CODEX_LOG"))
+	os.Exit(0)
+}
+
+func runFakeCodex(scenario, logPath string) {
+	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
+	if err != nil {
+		os.Exit(2)
+	}
+	var outMu sync.Mutex
+	send := func(v any) {
+		b, _ := json.Marshal(v)
+		outMu.Lock()
+		defer outMu.Unlock()
+		_, _ = os.Stdout.Write(append(b, '\n'))
+	}
+	notify := func(method string, params any) { send(map[string]any{"method": method, "params": params}) }
+
+	var mu sync.Mutex
+	answers := map[string]chan json.RawMessage{}
+	ask := func(id, method string, params any) json.RawMessage {
+		ch := make(chan json.RawMessage, 1)
+		mu.Lock()
+		answers[id] = ch
+		mu.Unlock()
+		send(map[string]any{"id": id, "method": method, "params": params})
+		return <-ch
+	}
+	var thread, turn string
+
+	playTurn := func() {
+		cmdItem := map[string]any{"type": "commandExecution", "id": "cmd-1", "command": "/bin/zsh -lc 'touch x'",
+			"cwd": "/work", "status": "inProgress", "commandActions": []any{map[string]any{"type": "unknown", "command": "touch x"}}}
+		done := func(status string) {
+			notify("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": status, "error": nil}})
+		}
+		switch scenario {
+		case "approval":
+			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
+			answer := ask("srv-1", "item/commandExecution/requestApproval", map[string]any{"threadId": thread, "turnId": turn,
+				"itemId": "cmd-1", "command": "touch x", "availableDecisions": []any{"accept", "cancel"}})
+			var d codexapp.ApprovalResponse
+			_ = json.Unmarshal(answer, &d)
+			cmdItem["status"] = map[bool]string{true: "completed", false: "declined"}[d.Decision == "accept"]
+			notify("item/completed", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
+			done("completed")
+		case "mcp":
+			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{
+				"type": "mcpToolCall", "id": "call-1", "server": "devkit", "tool": "db.query", "status": "inProgress", "arguments": map[string]any{}}})
+			ask("srv-2", "mcpServer/elicitation/request", map[string]any{"threadId": thread, "turnId": turn, "serverName": "devkit",
+				"mode": "form", "message": "Allow db.query?", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}},
+				"_meta": map[string]any{"codex_approval_kind": "mcp_tool_call", "persist": []string{"session", "always"}}})
+			done("completed")
+		case "interrupt":
+			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
+			// Nothing more until turn/interrupt: the command "runs".
+		case "unauthorized":
+			notify("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "failed",
+				"error": map[string]any{"message": "401 Unauthorized", "codexErrorInfo": "unauthorized", "additionalDetails": nil}}})
+		case "errorOnly":
+			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
+			notify("error", map[string]any{"threadId": thread, "turnId": turn, "willRetry": true,
+				"error": map[string]any{"message": "reconnecting", "codexErrorInfo": nil}})
+			notify("error", map[string]any{"threadId": thread, "turnId": turn, "willRetry": false,
+				"error": map[string]any{"message": "usage limit", "codexErrorInfo": "usageLimitExceeded"}})
+		case "die":
+			os.Exit(3)
+		default:
+			// Another thread on the same connection finishing first must not end
+			// this one.
+			notify("turn/completed", map[string]any{"threadId": "th-other", "turn": map[string]any{"id": turn, "status": "completed"}})
+			notify("item/started", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
+			cmdItem["status"], cmdItem["exitCode"], cmdItem["aggregatedOutput"] = "completed", 0, ""
+			notify("item/completed", map[string]any{"threadId": thread, "turnId": turn, "item": cmdItem})
+			notify("item/agentMessage/delta", map[string]any{"threadId": thread, "turnId": turn, "itemId": "msg-1", "delta": "done"})
+			notify("item/completed", map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"type": "agentMessage", "id": "msg-1", "text": "done"}})
+			notify("thread/tokenUsage/updated", map[string]any{"threadId": thread, "turnId": turn, "tokenUsage": map[string]any{
+				"total": map[string]any{"inputTokens": 100, "outputTokens": 20, "totalTokens": 120},
+				"last":  map[string]any{"inputTokens": 100, "outputTokens": 20, "totalTokens": 120}, "modelContextWindow": 1000}})
+			notify("remoteControl/status/changed", map[string]any{"serverName": "secret-host", "installationId": "id"})
+			done("completed")
+		}
+	}
+
+	in := bufio.NewReader(os.Stdin)
+	turns := 0
+	for {
+		line, err := in.ReadBytes('\n')
+		if err != nil {
+			return
+		}
+		_, _ = logFile.Write(line)
+		var m codexapp.Message
+		if json.Unmarshal(line, &m) != nil {
+			continue
+		}
+		if m.IsResponse() {
+			mu.Lock()
+			ch := answers[strings.Trim(string(m.ID), `"`)]
+			mu.Unlock()
+			if ch != nil {
+				ch <- m.Result
+			}
+			continue
+		}
+		if m.IsNotification() {
+			continue
+		}
+		reply := func(result any) { send(map[string]any{"id": m.ID, "result": result}) }
+		var p struct {
+			ThreadID string `json:"threadId"`
+		}
+		_ = json.Unmarshal(m.Params, &p)
+		switch m.Method {
+		case "initialize":
+			if scenario == "dieAtInitialize" {
+				os.Exit(3)
+			}
+			reply(map[string]any{"userAgent": "fake", "codexHome": os.Getenv("CODEX_HOME")})
+		case "account/read":
+			if scenario == "signedout" {
+				reply(map[string]any{"account": nil, "requiresOpenaiAuth": true})
+			} else {
+				reply(map[string]any{"account": map[string]any{"type": "chatgpt"}, "requiresOpenaiAuth": true})
+			}
+		case "account/login/start":
+			reply(map[string]any{"type": "apiKey"})
+		case "thread/start":
+			thread = "th-new"
+			reply(map[string]any{"thread": map[string]any{"id": thread}, "model": "gpt-5.5"})
+		case "thread/resume":
+			thread = p.ThreadID
+			reply(map[string]any{"thread": map[string]any{"id": thread}, "model": "gpt-5.5"})
+		case "model/list":
+			reply(map[string]any{"data": []any{
+				map[string]any{"id": "gpt-5.5", "model": "gpt-5.5", "displayName": "GPT-5.5"},
+				map[string]any{"id": "gpt-5.5-mini", "model": "gpt-5.5-mini", "displayName": "GPT-5.5 mini"},
+				map[string]any{"id": "internal", "model": "internal", "hidden": true},
+			}, "nextCursor": nil})
+		case "turn/start":
+			turns++
+			turn = "tu-" + string(rune('0'+turns))
+			reply(map[string]any{"turn": map[string]any{"id": turn, "status": "inProgress"}})
+			go playTurn()
+		case "turn/interrupt":
+			reply(map[string]any{})
+			notify("turn/completed", map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "interrupted"}})
+		default:
+			send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32601, "message": "fake: " + m.Method}})
+		}
+	}
+}
+
+// fakeCodexRunner starts TestCodexFakeAppServer in place of the jail, after
+// checking the policy the way the real runner would.
+type fakeCodexRunner struct {
+	scenario string
+	log      string
+
+	mu     sync.Mutex
+	policy sandbox.Policy
+	cmd    sandbox.Cmd
+}
+
+func (r *fakeCodexRunner) StartPiped(_ context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
+	if err := p.ValidateCmd(c); err != nil {
+		return nil, err
+	}
+	r.mu.Lock()
+	r.policy, r.cmd = p, c
+	r.mu.Unlock()
+	cmd := exec.Command(os.Args[0], "-test.run=^TestCodexFakeAppServer$")
+	cmd.Env = append(os.Environ(), "DEVKIT_FAKE_CODEX="+r.scenario, "DEVKIT_FAKE_CODEX_LOG="+r.log, "CODEX_HOME="+p.Env["CODEX_HOME"])
+	stdin, err := cmd.StdinPipe()
+	if err != nil {
+		return nil, err
+	}
+	stdout, err := cmd.StdoutPipe()
+	if err != nil {
+		return nil, err
+	}
+	if err := cmd.Start(); err != nil {
+		return nil, err
+	}
+	return sandbox.NewProcPiped(cmd, stdin, stdout, &bytes.Buffer{}), nil
+}
+
+func (r *fakeCodexRunner) started() (sandbox.Policy, sandbox.Cmd) {
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	return r.policy, r.cmd
+}
+
+// frames reads back what DevKit sent the fake, in order.
+func (r *fakeCodexRunner) frames(t *testing.T) []codexapp.Message {
+	t.Helper()
+	raw, err := os.ReadFile(r.log)
+	if err != nil {
+		t.Fatalf("nothing was sent: %v", err)
+	}
+	var out []codexapp.Message
+	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
+		var m codexapp.Message
+		if err := json.Unmarshal([]byte(line), &m); err != nil {
+			t.Fatalf("frame %q: %v", line, err)
+		}
+		out = append(out, m)
+	}
+	return out
+}
+
+func (r *fakeCodexRunner) sent(t *testing.T, method string) []json.RawMessage {
+	t.Helper()
+	var out []json.RawMessage
+	for _, m := range r.frames(t) {
+		if m.Method == method {
+			out = append(out, m.Params)
+		}
+	}
+	return out
+}
+
+type codexFixture struct {
+	adapter *CodexAdapter
+	runner  *fakeCodexRunner
+	home    string
+	cwd     string
+}
+
+func newCodexFixture(t *testing.T, scenario string) codexFixture {
+	t.Helper()
+	dir := t.TempDir()
+	bin := filepath.Join(dir, "codex")
+	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
+		t.Fatal(err)
+	}
+	cwd := filepath.Join(dir, "project")
+	if err := os.Mkdir(cwd, 0o755); err != nil {
+		t.Fatal(err)
+	}
+	runner := &fakeCodexRunner{scenario: scenario, log: filepath.Join(dir, "sent.jsonl")}
+	home := filepath.Join(dir, "data", "agent-homes", "codex")
+	return codexFixture{
+		adapter: NewCodexAdapter(CodexOptions{Command: bin, Home: home, Runner: runner, Version: "1.2.3"}),
+		runner:  runner,
+		home:    home,
+		cwd:     cwd,
+	}
+}
+
+func (f codexFixture) open(t *testing.T) SessionRuntime {
+	t.Helper()
+	rt, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
+	if err != nil {
+		t.Fatalf("Open: %v", err)
+	}
+	t.Cleanup(func() { _ = rt.Close(context.Background()) })
+	return rt
+}
+
+// updateLog collects what a runtime hands its OnUpdate.
+type updateLog struct {
+	mu      sync.Mutex
+	updates []json.RawMessage
+}
+
+func (u *updateLog) add(update, _ json.RawMessage) {
+	u.mu.Lock()
+	defer u.mu.Unlock()
+	u.updates = append(u.updates, append(json.RawMessage(nil), update...))
+}
+
+func (u *updateLog) kinds() []string {
+	u.mu.Lock()
+	defer u.mu.Unlock()
+	var out []string
+	for _, up := range u.updates {
+		out = append(out, string(ParseSessionUpdate(up).Kind))
+	}
+	return out
+}
+
+func (u *updateLog) joined() string {
+	u.mu.Lock()
+	defer u.mu.Unlock()
+	var b strings.Builder
+	for _, up := range u.updates {
+		b.Write(up)
+		b.WriteByte('\n')
+	}
+	return b.String()
+}
+
+// The jail and the command line a session gets (spec §4.3): DevKit's own
+// CODEX_HOME, read-write except the policy files; the user's ~/.codex
+// hidden; the data directory hidden but for this home; hooks, plugins, apps
+// and computer use off; the session's MCP server on the command line.
+func TestCodexOpenJailsTheAppServer(t *testing.T) {
+	f := newCodexFixture(t, "turn")
+	data := filepath.Dir(filepath.Dir(f.home))
+	secret := filepath.Join(data, "devkit.db")
+	if err := os.MkdirAll(filepath.Join(data, "agent-homes", "claude"), 0o700); err != nil {
+		t.Fatal(err)
+	}
+	proxy := filepath.Join(t.TempDir(), "devkit-mcp")
+	rt, err := f.adapter.Open(context.Background(), OpenSpec{
+		Cwd:           f.cwd,
+		DenyReadPaths: []string{secret, filepath.Join(data, "agent-homes")},
+		McpServers:    []acp.McpServerStdio{{Name: "devkit", Command: proxy, Args: []string{"--socket", "/tmp/s.sock"}, Env: []acp.EnvVariable{}}},
+	})
+	if err != nil {
+		t.Fatalf("Open: %v", err)
+	}
+	defer rt.Close(context.Background())
+	pol, cmd := f.runner.started()
+
+	if pol.Env["CODEX_HOME"] != f.home {
+		t.Errorf("CODEX_HOME = %q, want %q", pol.Env["CODEX_HOME"], f.home)
+	}
+	if !slices.Contains(pol.ReadWritePaths, f.home) {
+		t.Errorf("the home must be writable: %v", pol.ReadWritePaths)
+	}
+	for _, name := range []string{"config.toml", "rules", "AGENTS.md", "AGENTS.override.md", "skills"} {
+		if !slices.Contains(pol.DenyWritePaths, filepath.Join(f.home, name)) {
+			t.Errorf("%s must be deny-write: %v", name, pol.DenyWritePaths)
+		}
+	}
+	host, _ := os.UserHomeDir()
+	for _, hidden := range []string{filepath.Join(host, ".codex"), secret, filepath.Join(data, "agent-homes", "claude")} {
+		if !slices.Contains(pol.DenyReadPaths, hidden) {
+			t.Errorf("%s must be hidden: %v", hidden, pol.DenyReadPaths)
+		}
+	}
+	if slices.Contains(pol.DenyReadPaths, filepath.Join(data, "agent-homes")) {
+		t.Errorf("agent-homes must be carved out for Codex's own home: %v", pol.DenyReadPaths)
+	}
+	if pol.Network != sandbox.NetworkHost {
+		t.Errorf("network = %q", pol.Network)
+	}
+	for _, key := range []string{"OPENAI_API_KEY", "CODEX_API_KEY"} {
+		if _, ok := pol.Env[key]; ok {
+			t.Errorf("%s must not reach the jail", key)
+		}
+	}
+
+	argv := strings.Join(cmd.Args, " ")
+	for _, want := range []string{
+		"app-server",
+		`-c cli_auth_credentials_store="file"`,
+		"-c check_for_update_on_startup=false",
+		"--disable hooks --disable plugins --disable apps --disable computer_use",
+		`-c mcp_servers.devkit.command="` + proxy + `"`,
+		`-c mcp_servers.devkit.args=["--socket","/tmp/s.sock"]`,
+	} {
+		if !strings.Contains(argv, want) {
+			t.Errorf("argv lacks %s: %s", want, argv)
+		}
+	}
+
+	var init codexapp.InitializeParams
+	_ = json.Unmarshal(f.runner.sent(t, "initialize")[0], &init)
+	if init.ClientInfo.Name != "devkit" || init.ClientInfo.Version != "1.2.3" {
+		t.Errorf("clientInfo = %+v", init.ClientInfo)
+	}
+	if len(f.runner.sent(t, "initialized")) != 1 {
+		t.Error("initialized must follow initialize")
+	}
+	start := f.runner.sent(t, "thread/start")
+	if len(start) != 1 {
+		t.Fatalf("thread/start sent %d times", len(start))
+	}
+	if strings.Contains(string(start[0]), "sandbox") {
+		t.Errorf("thread/start must not send a sandbox mode: %s", start[0])
+	}
+	var ts codexapp.ThreadStartParams
+	_ = json.Unmarshal(start[0], &ts)
+	if ts.ApprovalPolicy != "untrusted" || ts.ApprovalsReviewer != "user" || ts.Cwd != f.cwd {
+		t.Errorf("thread/start = %+v", ts)
+	}
+	if rt.ACPSessionID() != "th-new" {
+		t.Errorf("ACPSessionID = %q", rt.ACPSessionID())
+	}
+}
+
+func TestCodexOpenRefusesWhenSignedOut(t *testing.T) {
+	f := newCodexFixture(t, "signedout")
+	_, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
+	if !errors.Is(err, ErrAgentAuthRequired) {
+		t.Fatalf("Open = %v, want ErrAgentAuthRequired", err)
+	}
+	if len(f.runner.sent(t, "thread/start")) != 0 {
+		t.Fatal("a signed-out session must not start a thread")
+	}
+}
+
+// An API key signs in over the wire, with nothing kept: the store is
+// ephemeral, and the key is on neither the command line nor in the env.
+func TestCodexAPIKeyGoesOverTheWireOnly(t *testing.T) {
+	f := newCodexFixture(t, "turn")
+	f.adapter.opts.APIKey = func(context.Context) (string, error) { return "sk-test-123", nil }
+	f.open(t)
+	pol, cmd := f.runner.started()
+	argv := strings.Join(cmd.Args, " ")
+	if !strings.Contains(argv, `cli_auth_credentials_store="ephemeral"`) {
+		t.Errorf("an API-key session must keep no credential: %s", argv)
+	}
+	if strings.Contains(argv, "sk-test-123") {
+		t.Fatal("the key is on the command line")
+	}
+	for k, v := range pol.Env {
+		if strings.Contains(v, "sk-test-123") {
+			t.Fatalf("the key is in the jail's env as %s", k)
+		}
+	}
+	login := f.runner.sent(t, "account/login/start")
+	if len(login) != 1 || !strings.Contains(string(login[0]), `"apiKey":"sk-test-123"`) || !strings.Contains(string(login[0]), `"type":"apiKey"`) {
+		t.Fatalf("account/login/start = %s", login)
+	}
+}
+
+// One turn end to end: the translated updates arrive before Prompt returns,
+// every turn/start carries the per-turn policy (spec §5.1), and the turn's
+// token use comes back as its usage.
+func TestCodexPromptRunsATurn(t *testing.T) {
+	f := newCodexFixture(t, "turn")
+	rt := f.open(t)
+	var log updateLog
+	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
+
+	out, err := rt.Prompt(context.Background(), TextPrompt("make x"), nil)
+	if err != nil {
+		t.Fatalf("Prompt: %v", err)
+	}
+	if out.StopReason != "end_turn" {
+		t.Fatalf("StopReason = %q", out.StopReason)
+	}
+	kinds := log.kinds()
+	for _, want := range []string{string(UpdateToolCall), string(UpdateToolCallUpdate), string(UpdateAgentMessageChunk), string(UpdateUsage)} {
+		if !slices.Contains(kinds, want) {
+			t.Errorf("no %s among %v", want, kinds)
+		}
+	}
+	if strings.Contains(log.joined(), "secret-host") {
+		t.Fatal("remoteControl/status/changed reached the transcript")
+	}
+
+	var ts codexapp.TurnStartParams
+	_ = json.Unmarshal(f.runner.sent(t, "turn/start")[0], &ts)
+	if ts.ThreadID != "th-new" || ts.ApprovalPolicy != "untrusted" || ts.ApprovalsReviewer != "user" {
+		t.Errorf("turn/start = %+v", ts)
+	}
+	if ts.SandboxPolicy == nil || ts.SandboxPolicy.Type != "externalSandbox" || ts.SandboxPolicy.NetworkAccess != "enabled" {
+		t.Errorf("sandboxPolicy = %+v, want externalSandbox with network", ts.SandboxPolicy)
+	}
+	if len(ts.Input) != 1 || !strings.Contains(string(ts.Input[0]), `"text":"make x"`) {
+		t.Errorf("input = %s", ts.Input)
+	}
+
+	usage, err := f.adapter.NormalizeUsage(out.Meta)
+	if err != nil || usage.InputTokens != 100 || usage.OutputTokens != 20 {
+		t.Fatalf("usage = %+v, %v (meta %s)", usage, err, out.Meta)
+	}
+}
+
+// Modes are DevKit's: each is an approvalPolicy on the next turn/start, and
+// Full auto keeps the id the composer's bypass confirmation keys on.
+func TestCodexModesSetTheNextTurnsPolicy(t *testing.T) {
+	f := newCodexFixture(t, "turn")
+	rt := f.open(t)
+	withModes := rt.(RuntimeModes)
+	if m := withModes.Modes(); m.CurrentModeID != "ask" || len(m.AvailableModes) != 3 || m.AvailableModes[2].ID != "bypassPermissions" {
+		t.Fatalf("Modes = %+v", m)
+	}
+	var log updateLog
+	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
+	configurator := rt.(RuntimeConfigurator)
+	for _, mode := range []string{"auto", "bypassPermissions", "ask"} {
+		if err := configurator.SetMode(context.Background(), mode); err != nil {
+			t.Fatalf("SetMode(%s): %v", mode, err)
+		}
+		if _, err := rt.Prompt(context.Background(), TextPrompt("go"), nil); err != nil {
+			t.Fatalf("Prompt: %v", err)
+		}
+	}
+	var policies []string
+	for _, raw := range f.runner.sent(t, "turn/start") {
+		var ts codexapp.TurnStartParams
+		_ = json.Unmarshal(raw, &ts)
+		policies = append(policies, ts.ApprovalPolicy)
+	}
+	if strings.Join(policies, ",") != "on-request,never,untrusted" {
+		t.Fatalf("policies = %v", policies)
+	}
+	if !strings.Contains(log.joined(), `"currentModeId":"bypassPermissions"`) {
+		t.Fatal("a mode change must be announced as current_mode_update")
+	}
+	if err := configurator.SetMode(context.Background(), "yolo"); !errors.Is(err, ErrInvalidConfigOption) {
+		t.Fatalf("unknown mode: %v", err)
+	}
+}
+
+// The model picker is announced once handlers arrive, lists what model/list
+// offers (hidden ones left out), and a choice rides the next turn/start.
+func TestCodexModelPicker(t *testing.T) {
+	f := newCodexFixture(t, "turn")
+	rt := f.open(t)
+	var log updateLog
+	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
+	announced := log.joined()
+	if !strings.Contains(announced, `"id":"model"`) || !strings.Contains(announced, "gpt-5.5-mini") || strings.Contains(announced, `"internal"`) {
+		t.Fatalf("config options = %s", announced)
+	}
+	opt := ParseSessionUpdate(log.updates[0]).ConfigOptions
+	if len(opt) != 1 || string(opt[0].CurrentValue) != `"gpt-5.5"` {
+		t.Fatalf("parsed options = %+v", opt)
+	}
+	configurator := rt.(RuntimeConfigurator)
+	if err := configurator.SetConfigOption(context.Background(), "model", "gpt-5.5-mini"); err != nil {
+		t.Fatal(err)
+	}
+	if err := configurator.SetConfigOption(context.Background(), "model", "internal"); !errors.Is(err, ErrInvalidConfigOption) {
+		t.Fatalf("a hidden model: %v", err)
+	}
+	if _, err := rt.Prompt(context.Background(), TextPrompt("go"), nil); err != nil {
+		t.Fatal(err)
+	}
+	var ts codexapp.TurnStartParams
+	_ = json.Unmarshal(f.runner.sent(t, "turn/start")[0], &ts)
+	if ts.Model != "gpt-5.5-mini" {
+		t.Fatalf("model = %q", ts.Model)
+	}
+}
+
+// A command approval reaches the permission gate as a card, and the answer
+// goes back as Codex's decision; with no decline on offer, Deny is cancel.
+func TestCodexApprovalGoesThroughThePermissionGate(t *testing.T) {
+	for option, want := range map[string]string{codexOptionAllow: "accept", codexOptionDeny: "cancel"} {
+		t.Run(option, func(t *testing.T) {
+			f := newCodexFixture(t, "approval")
+			rt := f.open(t)
+			var asked json.RawMessage
+			rt.SetHandlers(RuntimeHandlers{
+				OnUpdate: func(json.RawMessage, json.RawMessage) {},
+				OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
+					asked = params
+					return selected(option), nil
+				},
+			})
+			if _, err := rt.Prompt(context.Background(), TextPrompt("touch x"), nil); err != nil {
+				t.Fatal(err)
+			}
+			if p := decodePermission(t, asked); p.toolCall.ToolCallID != "cmd-1" || p.SessionID != "th-new" {
+				t.Fatalf("card = %+v", p)
+			}
+			var got string
+			for _, m := range f.runner.frames(t) {
+				if m.IsResponse() && string(m.ID) == `"srv-1"` {
+					var d codexapp.ApprovalResponse
+					_ = json.Unmarshal(m.Result, &d)
+					got = d.Decision
+				}
+			}
+			if got != want {
+				t.Fatalf("decision = %q, want %q", got, want)
+			}
+		})
+	}
+}
+
+// With nobody to ask — no permission handler — an approval fails closed.
+func TestCodexApprovalWithNobodyToAskIsRefused(t *testing.T) {
+	f := newCodexFixture(t, "approval")
+	rt := f.open(t)
+	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(json.RawMessage, json.RawMessage) {}})
+	if _, err := rt.Prompt(context.Background(), TextPrompt("touch x"), nil); err != nil {
+		t.Fatal(err)
+	}
+	for _, m := range f.runner.frames(t) {
+		if m.IsResponse() && string(m.ID) == `"srv-1"` && !strings.Contains(string(m.Result), `"cancel"`) {
+			t.Fatalf("answer = %s, want cancel", m.Result)
+		}
+	}
+}
+
+// Codex asks before a DevKit MCP tool runs; the person answers on a card.
+func TestCodexMcpToolApprovalGoesThroughThePermissionGate(t *testing.T) {
+	f := newCodexFixture(t, "mcp")
+	rt := f.open(t)
+	var card json.RawMessage
+	rt.SetHandlers(RuntimeHandlers{
+		OnUpdate: func(json.RawMessage, json.RawMessage) {},
+		OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
+			card = params
+			return selected(codexOptionAllowSession), nil
+		},
+	})
+	if _, err := rt.Prompt(context.Background(), TextPrompt("query"), nil); err != nil {
+		t.Fatal(err)
+	}
+	if id := decodePermission(t, card).toolCall.ToolCallID; id != "call-1" {
+		t.Fatalf("card is for %q, want the open call", id)
+	}
+	for _, m := range f.runner.frames(t) {
+		if m.IsResponse() && string(m.ID) == `"srv-2"` {
+			if want := `{"action":"accept","content":null,"_meta":{"persist":"session"}}`; string(m.Result) != want {
+				t.Fatalf("answer = %s, want %s", m.Result, want)
+			}
+			return
+		}
+	}
+	t.Fatal("the elicitation was never answered")
+}
+
+// Stop is turn/interrupt. Codex ends the turn interrupted without completing
+// the command it was running, so DevKit closes that card itself.
+func TestCodexCancelInterruptsTheTurn(t *testing.T) {
+	f := newCodexFixture(t, "interrupt")
+	rt := f.open(t)
+	var log updateLog
+	started := make(chan struct{}, 1)
+	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(u, m json.RawMessage) {
+		log.add(u, m)
+		if ParseSessionUpdate(u).Kind == UpdateToolCall {
+			started <- struct{}{}
+		}
+	}})
+	type result struct {
+		out PromptOutcome
+		err error
+	}
+	done := make(chan result, 1)
+	go func() {
+		out, err := rt.Prompt(context.Background(), TextPrompt("sleep"), nil)
+		done <- result{out, err}
+	}()
+	select {
+	case <-started:
+	case <-time.After(5 * time.Second):
+		t.Fatal("the turn never started")
+	}
+	if err := rt.Cancel(context.Background()); err != nil {
+		t.Fatal(err)
+	}
+	var r result
+	select {
+	case r = <-done:
+	case <-time.After(5 * time.Second):
+		t.Fatal("Prompt did not return after Cancel")
+	}
+	if r.err != nil || r.out.StopReason != "cancelled" {
+		t.Fatalf("Prompt = %+v, %v; want a cancelled stop", r.out, r.err)
+	}
+	if len(f.runner.sent(t, "turn/interrupt")) != 1 {
+		t.Fatal("Cancel must send turn/interrupt")
+	}
+	if !strings.Contains(log.joined(), `"toolCallId":"cmd-1","status":"failed"`) {
+		t.Fatalf("the running command's card was left open:\n%s", log.joined())
+	}
+	if dead, _ := rt.(RuntimeLiveness).Dead(); dead {
+		t.Fatal("an interrupt must not kill the process")
+	}
+}
+
+func TestCodexFailedTurnAsksForSignIn(t *testing.T) {
+	f := newCodexFixture(t, "unauthorized")
+	rt := f.open(t)
+	if _, err := rt.Prompt(context.Background(), TextPrompt("go"), nil); !errors.Is(err, ErrAgentAuthRequired) {
+		t.Fatalf("Prompt = %v, want ErrAgentAuthRequired", err)
+	}
+}
+
+// An error Codex will retry is only logged; one it will not ends the turn,
+// even when no turn/completed follows it.
+func TestCodexErrorWithoutCompletionEndsTheTurn(t *testing.T) {
+	defer func(old time.Duration) { codexErrorGrace = old }(codexErrorGrace)
+	codexErrorGrace = 100 * time.Millisecond
+	f := newCodexFixture(t, "errorOnly")
+	rt := f.open(t)
+	var log updateLog
+	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
+	_, err := rt.Prompt(context.Background(), TextPrompt("go"), nil)
+	if !errors.Is(err, ErrProcessFailed) || !strings.Contains(err.Error(), "usage limit") {
+		t.Fatalf("Prompt = %v", err)
+	}
+	if !strings.Contains(log.joined(), `"toolCallId":"cmd-1","status":"failed"`) {
+		t.Fatalf("the open command was left open:\n%s", log.joined())
+	}
+}
+
+func TestCodexLoadResumesTheThread(t *testing.T) {
+	f := newCodexFixture(t, "turn")
+	rt, err := f.adapter.Load(context.Background(), OpenSpec{Cwd: f.cwd}, "th-old")
+	if err != nil {
+		t.Fatal(err)
+	}
+	defer rt.Close(context.Background())
+	if rt.ACPSessionID() != "th-old" {
+		t.Fatalf("ACPSessionID = %q", rt.ACPSessionID())
+	}
+	resume := f.runner.sent(t, "thread/resume")
+	if len(resume) != 1 || len(f.runner.sent(t, "thread/start")) != 0 {
+		t.Fatalf("resume = %s", resume)
+	}
+	var p codexapp.ThreadResumeParams
+	_ = json.Unmarshal(resume[0], &p)
+	if p.ThreadID != "th-old" || p.ApprovalPolicy != "untrusted" || p.ApprovalsReviewer != "user" {
+		t.Fatalf("thread/resume = %+v", p)
+	}
+	if _, err := f.adapter.Load(context.Background(), OpenSpec{Cwd: f.cwd}, ""); !errors.Is(err, ErrSessionNotFound) {
+		t.Fatalf("Load with no id = %v", err)
+	}
+}
+
+// A process that dies mid-turn fails the turn and reports itself dead, so
+// Service resumes the session on a fresh one.
+func TestCodexDeadProcessFailsTheTurn(t *testing.T) {
+	f := newCodexFixture(t, "die")
+	rt := f.open(t)
+	_, err := rt.Prompt(context.Background(), TextPrompt("go"), nil)
+	if !errors.Is(err, ErrProcessFailed) {
+		t.Fatalf("Prompt = %v, want ErrProcessFailed", err)
+	}
+	deadline := time.Now().Add(5 * time.Second)
+	for {
+		if dead, _ := rt.(RuntimeLiveness).Dead(); dead {
+			break
+		}
+		if time.Now().After(deadline) {
+			t.Fatal("the runtime never reported itself dead")
+		}
+		time.Sleep(20 * time.Millisecond)
+	}
+}
+
+func TestCodexOpenFailsFastWhenTheProcessDies(t *testing.T) {
+	f := newCodexFixture(t, "dieAtInitialize")
+	start := time.Now()
+	_, err := f.adapter.Open(context.Background(), OpenSpec{Cwd: f.cwd})
+	if !errors.Is(err, ErrProcessFailed) {
+		t.Fatalf("Open = %v", err)
+	}
+	if time.Since(start) > 10*time.Second {
+		t.Fatal("Open waited out the handshake timeout for a dead process")
+	}
+}
+
+// Probe verifies an install in a throwaway home: the real one — and the
+// sign-in in it — is never touched.
+func TestCodexProbeUsesAThrowawayHome(t *testing.T) {
+	f := newCodexFixture(t, "turn")
+	bin := f.adapter.opts.Command
+	f.adapter.opts.Command = ""
+	if err := f.adapter.Probe(context.Background(), agentruntimeResolved(bin)); err != nil {
+		t.Fatalf("Probe: %v", err)
+	}
+	if _, err := os.Stat(f.home); !os.IsNotExist(err) {
+		t.Fatalf("Probe touched the real home: %v", err)
+	}
+	pol, _ := f.runner.started()
+	if pol.Env["CODEX_HOME"] == f.home {
+		t.Fatal("Probe ran with the real CODEX_HOME")
+	}
+}
+
+// Settings reach Codex as TOML on the command line: quoted, so a path with
+// spaces or quotes cannot break out into another key.
+func TestCodexArgsQuoteTOML(t *testing.T) {
+	args := strings.Join(codexArgs("file", []acp.McpServerStdio{
+		{Name: "devkit", Command: `/Applications/Dev Kit.app/bin/devkit-mcp`, Args: []string{`--socket`, `/tmp/a "b".sock`},
+			Env: []acp.EnvVariable{{Name: "B", Value: "2"}, {Name: "A", Value: `x"y`}}},
+		{Name: "bad.name", Command: "/bin/x"},
+	}), "\n")
+	for _, want := range []string{
+		`mcp_servers.devkit.command="/Applications/Dev Kit.app/bin/devkit-mcp"`,
+		`mcp_servers.devkit.args=["--socket","/tmp/a \"b\".sock"]`,
+		`mcp_servers.devkit.env={"A"="x\"y","B"="2"}`,
+	} {
+		if !strings.Contains(args, want) {
+			t.Errorf("missing %s in\n%s", want, args)
+		}
+	}
+	if strings.Contains(args, "bad.name") {
+		t.Fatal("a server name that is not a bare key must not be passed")
+	}
+}
+
+func TestCodexInputCarriesTextImagesAndFiles(t *testing.T) {
+	in := codexInput([]acp.ContentBlock{
+		{Type: "text", Text: "look"},
+		{Type: "image", Data: "aGk=", MimeType: "image/png"},
+		{Type: "resource", Resource: &acp.ResourceContents{URI: "file:///a.go", Text: "package a"}},
+	})
+	if len(in) != 2 {
+		t.Fatalf("input = %s", in)
+	}
+	if !strings.Contains(string(in[0]), `"text":"look\n\nfile:///a.go:\npackage a"`) {
+		t.Errorf("text = %s", in[0])
+	}
+	if string(in[1]) != `{"type":"image","url":"data:image/png;base64,aGk="}` {
+		t.Errorf("image = %s", in[1])
+	}
+	if codexInput([]acp.ContentBlock{{Type: "image"}}) != nil {
+		t.Error("nothing readable must be nothing")
+	}
+}
+
+func agentruntimeResolved(bin string) agentruntime.Resolved {
+	return agentruntime.Resolved{Command: bin, ReadOnlyPaths: []string{filepath.Dir(bin)}}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agenthub/ -run 'TestCodex(Open|Prompt)' -count=1`
Expected: FAIL build — `undefined: NewCodexAdapter` (và các symbol khác của adapter).

- [ ] **Step 3: Áp patch hiện thực** (`extract t5-impl`)

<!-- patch t5-impl -->
```diff
diff --git a/internal/agenthub/adapter_codex.go b/internal/agenthub/adapter_codex.go
new file mode 100644
index 0000000000000000000000000000000000000000..cb6a663e8807374fda82480336cbcd66725adab9
--- /dev/null
+++ b/internal/agenthub/adapter_codex.go
@@ -0,0 +1,1237 @@
+package agenthub
+
+import (
+	"context"
+	"encoding/json"
+	"errors"
+	"fmt"
+	"log"
+	"os"
+	"os/user"
+	"path/filepath"
+	"regexp"
+	"sort"
+	"strconv"
+	"strings"
+	"sync"
+	"sync/atomic"
+	"time"
+
+	"devkit/internal/acp"
+	"devkit/internal/agentruntime"
+	"devkit/internal/codexapp"
+	"devkit/internal/sandbox"
+)
+
+// Codex's credential stores (cli_auth_credentials_store). "file" keeps a
+// ChatGPT sign-in in CODEX_HOME/auth.json; "ephemeral" keeps nothing, for a
+// session signed in with an API key DevKit hands over the wire.
+const (
+	codexStoreFile      = "file"
+	codexStoreEphemeral = "ephemeral"
+)
+
+// The three modes (spec §7.1). The ids are ACP's: bypassPermissions is the id
+// the composer already treats as "confirm before choosing", so Full auto gets
+// the same confirmation Claude's bypass does.
+const (
+	codexModeAsk  = "ask"
+	codexModeAuto = "auto"
+	codexModeFull = "bypassPermissions"
+)
+
+var codexModes = []acp.SessionMode{
+	{ID: codexModeAsk, Name: "Ask", Description: "Asks before editing files or running a command that changes anything."},
+	{ID: codexModeAuto, Name: "Auto", Description: "Works on its own and asks only when it decides it needs to."},
+	{ID: codexModeFull, Name: "Full auto", Description: "Never asks. DevKit's sandbox and its prod checks still apply."},
+}
+
+// codexModelOption is the one config option a Codex session has.
+const codexModelOption = "model"
+
+// codexDisabledFeatures keep outside code out of a session: hooks could
+// approve a request before it reaches DevKit (a project's .codex/config.toml
+// can declare them), and plugins, apps and computer use pull in tools, MCP
+// servers and hooks DevKit never sees (spec §7.3).
+var codexDisabledFeatures = []string{"hooks", "plugins", "apps", "computer_use"}
+
+const (
+	// codexInterruptWait is how long a turn gets to end after turn/interrupt.
+	// Longer than that and the process is not answering: Cancel kills it, and
+	// the session resumes on a fresh one.
+	codexInterruptWait = 10 * time.Second
+	// codexModelPages bounds model/list paging.
+	codexModelPages = 5
+)
+
+// codexErrorGrace is how long an error that will not be retried waits for
+// the turn/completed that normally follows it before it ends the turn
+// itself. A var so a test need not wait it out.
+var codexErrorGrace = 10 * time.Second
+
+// CodexOptions configures the Codex vendor adapter.
+type CodexOptions struct {
+	// Command is an absolute codex binary that overrides the managed runtime —
+	// DEVKIT_CODEX_COMMAND, for development and CI.
+	Command string
+	// Runtime resolves the @openai/codex DevKit installed (agentruntime).
+	// Called on every Open and Load, like ClaudeOptions.Runtime.
+	Runtime func() (agentruntime.Resolved, error)
+	// Home is CODEX_HOME: DevKit's own, never ~/.codex (spec §6). The jail
+	// may write it, except the policy files prepareCodexHome resets.
+	Home string
+	// APIKey returns the key a session signs in with, or "" to use the
+	// ChatGPT sign-in kept in Home. The key goes over the wire after
+	// initialize; it is never written to disk, the environment or argv.
+	APIKey func(ctx context.Context) (string, error)
+	// Runner overrides sandbox.DefaultRunner(); nil uses the platform default.
+	Runner SandboxRunner
+	// Version is DevKit's, sent as clientInfo.version.
+	Version string
+}
+
+// CodexAdapter runs `codex app-server` inside DevKit's jail and speaks ACP to
+// the rest of Agent Hub: notifications through codexTranslator, approvals
+// through PermissionWaiter.
+type CodexAdapter struct {
+	opts CodexOptions
+}
+
+// NewCodexAdapter returns the Codex adapter. Every process it starts goes
+// through the SandboxRunner — never an unsandboxed exec.
+func NewCodexAdapter(opts CodexOptions) *CodexAdapter {
+	return &CodexAdapter{opts: opts}
+}
+
+var (
+	_ VendorAdapter       = (*CodexAdapter)(nil)
+	_ SessionLoader       = (*CodexAdapter)(nil)
+	_ SessionRuntime      = (*codexRuntime)(nil)
+	_ RuntimeConfigurator = (*codexRuntime)(nil)
+	_ RuntimeModes        = (*codexRuntime)(nil)
+	_ RuntimeLiveness     = (*codexRuntime)(nil)
+)
+
+func (a *CodexAdapter) Type() AgentType { return AgentTypeCodex }
+
+func (a *CodexAdapter) Extensions() []string { return nil }
+
+func (a *CodexAdapter) runner() SandboxRunner {
+	if a.opts.Runner != nil {
+		return a.opts.Runner
+	}
+	return sandbox.DefaultRunner()
+}
+
+// resolveCommand picks the codex binary: the Command override, else the
+// installed runtime, else ErrAgentNotInstalled. Same rules as Claude's.
+func (a *CodexAdapter) resolveCommand() (launch, error) {
+	if cmd := a.opts.Command; cmd != "" {
+		if !filepath.IsAbs(cmd) {
+			return launch{}, fmt.Errorf("%w: command must be absolute", ErrProcessFailed)
+		}
+		path := filepath.Clean(cmd)
+		return launch{path: path, ro: uniqueAbsPaths(append([]string{path}, codexPackageDir(path)...)...)}, nil
+	}
+	if a.opts.Runtime == nil {
+		return launch{}, ErrAgentNotInstalled
+	}
+	rt, err := a.opts.Runtime()
+	if errors.Is(err, agentruntime.ErrNotInstalled) {
+		return launch{}, fmt.Errorf("%w: %w", ErrAgentNotInstalled, err)
+	}
+	if err != nil {
+		return launch{}, fmt.Errorf("%w: %w", ErrProcessFailed, err)
+	}
+	return runtimeLaunch(rt), nil
+}
+
+// codexPackageDir is the platform package directory a codex binary sits in
+// (vendor/<triple>/bin/codex → vendor/<triple>), which also holds
+// codex-package.json and codex-path/ with the bundled rg. The binary finds
+// both relative to itself and puts codex-path on its children's PATH
+// (measured on 0.147.0), so the jail has to be able to read the directory.
+func codexPackageDir(bin string) []string {
+	resolved, err := filepath.EvalSymlinks(bin)
+	if err != nil {
+		return nil
+	}
+	dir := filepath.Dir(filepath.Dir(resolved))
+	if _, err := os.Stat(filepath.Join(dir, "codex-package.json")); err != nil {
+		return nil
+	}
+	return []string{dir}
+}
+
+// codexEnv is the jail's environment. No provider key passes through from
+// the host: an API key reaches Codex only over the wire (CodexOptions.APIKey).
+// CODEX_MANAGED_BY_NPM and CODEX_MANAGED_PACKAGE_ROOT, which the npm launcher
+// sets, are left out on purpose: they only tell Codex how to update itself,
+// DevKit pins and updates it, and without them the binary still finds its
+// bundled rg (spec §4.2, measured). CODEX_HOME is set last so nothing can
+// move it.
+func codexEnv(l launch, spec OpenSpec, home string) map[string]string {
+	env := make(map[string]string)
+	for _, key := range []string{"USER", "LOGNAME", "LANG", "LC_ALL"} {
+		if v, ok := os.LookupEnv(key); ok && v != "" {
+			env[key] = v
+		}
+	}
+	if env["USER"] == "" {
+		if u, err := user.Current(); err == nil && u.Username != "" {
+			env["USER"] = u.Username
+		}
+	}
+	path := os.Getenv("PATH")
+	if path == "" {
+		path = "/usr/local/bin:/usr/bin:/bin"
+	}
+	env["PATH"] = path
+	for key, value := range l.env {
+		env[key] = value
+	}
+	for key, value := range spec.ExtraEnv {
+		env[key] = value
+	}
+	env["CODEX_HOME"] = home
+	return env
+}
+
+// jail is the one place a Codex policy is written, shared by sessions,
+// sign-in and Probe, so each runs under the jail a session gets.
+func (a *CodexAdapter) jail(workDir string, l launch, spec OpenSpec, store string) (sandbox.Policy, sandbox.Cmd, error) {
+	home := a.opts.Home
+	denyWrite, err := prepareCodexHome(home)
+	if err != nil {
+		return sandbox.Policy{}, sandbox.Cmd{}, err
+	}
+	// DevKit's data directory is hidden from every session; this vendor's own
+	// home inside it is carved back out for this vendor's sessions only.
+	denyRead := carveOut(spec.DenyReadPaths, home)
+	// The user's own Codex — its sign-in, config and rules — is not this
+	// session's. Hidden even under a shared home.
+	if host, err := os.UserHomeDir(); err == nil {
+		denyRead = append(denyRead, filepath.Join(host, ".codex"))
+	}
+	pol := sandbox.Policy{
+		WorkDir:          workDir,
+		Network:          sandbox.NetworkHost,
+		Env:              codexEnv(l, spec, home),
+		ReadOnlyPaths:    uniqueAbsPaths(append(append([]string(nil), l.ro...), spec.ReadOnlyPaths...)...),
+		ReadWritePaths:   uniqueAbsPaths(append([]string{home}, spec.ExtraReadWritePaths...)...),
+		DenyReadPaths:    denyRead,
+		DenyWritePaths:   denyWrite,
+		AllowUnixSockets: append([]string(nil), spec.AllowUnixSockets...),
+		PrivateHome:      spec.PrivateHome,
+	}
+	args := append(append([]string(nil), l.args...), codexArgs(store, spec.McpServers)...)
+	return pol, sandbox.Cmd{Path: l.path, Args: args}, nil
+}
+
+// codexArgs is `codex app-server` with DevKit's settings. They go on the
+// command line, not in config.toml: the jail cannot change a process's argv,
+// and config.toml is read-only to it and rewritten on every spawn anyway.
+//
+// The session's MCP servers are TOML values under mcp_servers.<name>. Their
+// env lands on argv, which any local process can read; DevKit's own endpoint
+// carries none (its identity is the socket, mode 0600).
+func codexArgs(store string, servers []acp.McpServerStdio) []string {
+	args := []string{
+		"app-server",
+		"-c", "cli_auth_credentials_store=" + tomlString(store),
+		"-c", "check_for_update_on_startup=false",
+	}
+	for _, srv := range servers {
+		if !tomlBareKey.MatchString(srv.Name) {
+			log.Printf("agenthub: codex: MCP server name %q cannot be a config key; not passed", srv.Name)
+			continue
+		}
+		key := "mcp_servers." + srv.Name
+		args = append(args,
+			"-c", key+".command="+tomlString(srv.Command),
+			"-c", key+".args="+tomlStrings(srv.Args),
+		)
+		if len(srv.Env) > 0 {
+			args = append(args, "-c", key+".env="+tomlEnv(srv.Env))
+		}
+	}
+	for _, feature := range codexDisabledFeatures {
+		args = append(args, "--disable", feature)
+	}
+	return args
+}
+
+// tomlBareKey is what `-c` can take as one segment of a dotted key: Codex
+// splits the key on dots, so a quoted segment would not survive.
+var tomlBareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
+
+// tomlString quotes s as a TOML basic string. JSON's escapes are a subset of
+// TOML's, and encoding/json never emits the one JSON has that TOML lacks
+// (\/).
+func tomlString(s string) string {
+	b, _ := json.Marshal(s)
+	return string(b)
+}
+
+func tomlStrings(list []string) string {
+	if list == nil {
+		list = []string{}
+	}
+	b, _ := json.Marshal(list)
+	return string(b)
+}
+
+func tomlEnv(env []acp.EnvVariable) string {
+	sorted := append([]acp.EnvVariable(nil), env...)
+	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
+	parts := make([]string, 0, len(sorted))
+	for _, v := range sorted {
+		parts = append(parts, tomlString(v.Name)+"="+tomlString(v.Value))
+	}
+	return "{" + strings.Join(parts, ",") + "}"
+}
+
+// codexProcess is one app-server under the jail and the connection to it.
+type codexProcess struct {
+	proc        *sandbox.Proc
+	conn        *codexapp.Conn
+	cancelServe context.CancelFunc
+	serveDone   chan struct{}
+	serveErr    error // written before serveDone closes
+}
+
+// start spawns the app-server and serves its connection with h. The process
+// outlives ctx on purpose, as Claude's does: a session's lifetime belongs to
+// its runtime, not to the request that opened it.
+func (a *CodexAdapter) start(workDir string, l launch, spec OpenSpec, store string, h codexapp.Handler) (*codexProcess, error) {
+	pol, cmd, err := a.jail(workDir, l, spec, store)
+	if err != nil {
+		return nil, err
+	}
+	// Paths only: the env is never logged.
+	log.Printf("agenthub: codex: starting %s in %s (read-only %v, sockets %v)",
+		cmd.Path, pol.WorkDir, pol.ReadOnlyPaths, pol.AllowUnixSockets)
+	proc, err := a.runner().StartPiped(context.Background(), pol, cmd)
+	if err != nil {
+		return nil, mapSandboxErr(err)
+	}
+	stdin, stdout := proc.StdinWriter(), proc.StdoutReader()
+	if stdin == nil || stdout == nil {
+		_ = proc.Kill()
+		return nil, fmt.Errorf("%w: StartPiped missing stdio pipes", ErrProcessFailed)
+	}
+	p := &codexProcess{proc: proc, serveDone: make(chan struct{})}
+	p.conn = codexapp.NewConn(&stdioPipe{r: stdout, w: stdin}, h)
+	var serveCtx context.Context
+	serveCtx, p.cancelServe = context.WithCancel(context.Background())
+	go func() {
+		defer close(p.serveDone)
+		p.serveErr = p.conn.Serve(serveCtx)
+	}()
+	return p, nil
+}
+
+func (p *codexProcess) stop() {
+	p.cancelServe()
+	_ = p.conn.Close()
+	_ = p.proc.Kill()
+	<-p.serveDone
+}
+
+// bounded is ctx with a timeout that also ends the moment the process does:
+// a dead app-server never answers, and waiting out the timeout for it says
+// nothing the exit has not already said.
+func (p *codexProcess) bounded(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
+	ctx, cancel := context.WithTimeout(ctx, timeout)
+	go func() {
+		select {
+		case <-p.serveDone:
+			cancel()
+		case <-ctx.Done():
+		}
+	}()
+	return ctx, cancel
+}
+
+// failure explains a failed call. A process that exited is reported as such,
+// with the line of stderr that says why; the grace closes the race between
+// the failed call and the serve loop noticing EOF, as Claude's handshake does.
+func (p *codexProcess) failure(stage string, err error) error {
+	select {
+	case <-p.serveDone:
+		reason := stderrReason(p.proc)
+		logStderrTail("codex "+stage, p.proc)
+		return fmt.Errorf("%w: %s: codex exited%s", ErrProcessFailed, stage, reason)
+	case <-time.After(serveExitGrace):
+		return fmt.Errorf("%w: %s: %v", ErrProcessFailed, stage, err)
+	}
+}
+
+// initialize is the handshake every connection starts with.
+func (a *CodexAdapter) initialize(ctx context.Context, p *codexProcess) error {
+	version := a.opts.Version
+	if version == "" {
+		version = "0.0.0"
+	}
+	var res codexapp.InitializeResponse
+	if err := p.conn.Call(ctx, codexapp.MethodInitialize, codexapp.InitializeParams{
+		ClientInfo:   codexapp.ClientInfo{Name: "devkit", Title: "DevKit", Version: version},
+		Capabilities: &codexapp.InitializeCapabilities{},
+	}, &res); err != nil {
+		return p.failure("initialize", err)
+	}
+	if err := p.conn.Notify(codexapp.MethodInitialized, nil); err != nil {
+		return p.failure("initialized", err)
+	}
+	return nil
+}
+
+// apiKey is the key this session signs in with, or "".
+func (a *CodexAdapter) apiKey(ctx context.Context) (string, error) {
+	if a.opts.APIKey == nil {
+		return "", nil
+	}
+	key, err := a.opts.APIKey(ctx)
+	if err != nil {
+		return "", fmt.Errorf("%w: the Codex API key could not be read: %v", ErrAgentAuthRequired, err)
+	}
+	return strings.TrimSpace(key), nil
+}
+
+func codexWorkDir(cwd string) (string, error) {
+	workDir, err := filepath.Abs(cwd)
+	if err != nil {
+		return "", fmt.Errorf("%w: resolve project: %v", ErrProcessFailed, err)
+	}
+	if st, err := os.Stat(workDir); err != nil || !st.IsDir() {
+		return "", fmt.Errorf("%w: project path", ErrProcessFailed)
+	}
+	return workDir, nil
+}
+
+// Open starts a sandboxed app-server and a new thread.
+func (a *CodexAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
+	return a.open(ctx, spec, "")
+}
+
+// Load resumes a thread in a new process, satisfying SessionLoader. Under the
+// same jail as Open, and in Ask: the mode is DevKit's, not Codex's, and a
+// resumed session starts from the safe one rather than from whatever the
+// last process was in when it died.
+func (a *CodexAdapter) Load(ctx context.Context, spec OpenSpec, threadID string) (SessionRuntime, error) {
+	if threadID == "" {
+		return nil, fmt.Errorf("%w: load needs the Codex thread id", ErrSessionNotFound)
+	}
+	return a.open(ctx, spec, threadID)
+}
+
+func (a *CodexAdapter) open(ctx context.Context, spec OpenSpec, resume string) (SessionRuntime, error) {
+	if err := ctx.Err(); err != nil {
+		return nil, err
+	}
+	workDir, err := codexWorkDir(spec.Cwd)
+	if err != nil {
+		return nil, err
+	}
+	l, err := a.resolveCommand()
+	if err != nil {
+		return nil, err
+	}
+	key, err := a.apiKey(ctx)
+	if err != nil {
+		return nil, err
+	}
+	store := codexStoreFile
+	if key != "" {
+		store = codexStoreEphemeral
+	}
+
+	rt := &codexRuntime{tr: newCodexTranslator(), mode: codexModeAsk, network: true}
+	p, err := a.start(workDir, l, spec, store, &codexConnHandler{rt: rt})
+	if err != nil {
+		return nil, err
+	}
+	rt.proc, rt.conn, rt.cancelServe, rt.serveDone = p.proc, p.conn, p.cancelServe, p.serveDone
+	go func() {
+		<-p.serveDone
+		rt.noteServeExit(p.serveErr)
+	}()
+	log.Printf("agenthub: codex: sandboxed process started, pid=%d (resume %q)", p.proc.PID(), resume)
+
+	hsCtx, cancel := p.bounded(ctx, handshakeTimeout)
+	defer cancel()
+	fail := func(err error) (SessionRuntime, error) {
+		p.stop()
+		return nil, err
+	}
+	if err := a.initialize(hsCtx, p); err != nil {
+		return fail(err)
+	}
+	if key != "" {
+		var login codexapp.LoginStartResponse
+		if err := p.conn.Call(hsCtx, codexapp.MethodAccountLoginStart, codexapp.LoginStartParams{
+			Type: codexapp.LoginAPIKey, APIKey: key,
+		}, &login); err != nil {
+			// The server's message, never the key: it is not part of err.
+			return fail(fmt.Errorf("%w: Codex refused the API key: %v", ErrAgentAuthRequired, err))
+		}
+	}
+	var account codexapp.GetAccountResponse
+	if err := p.conn.Call(hsCtx, codexapp.MethodAccountRead, map[string]any{"refreshToken": false}, &account); err != nil {
+		return fail(p.failure("account/read", err))
+	}
+	if account.RequiresOpenAIAuth && account.Account == nil {
+		return fail(fmt.Errorf("%w: Codex is not signed in", ErrAgentAuthRequired))
+	}
+
+	var thread codexapp.ThreadStartResponse
+	if resume != "" {
+		if err := p.conn.Call(hsCtx, codexapp.MethodThreadResume, codexapp.ThreadResumeParams{
+			ThreadID:          resume,
+			Cwd:               workDir,
+			ApprovalPolicy:    codexApprovalPolicy(codexModeAsk),
+			ApprovalsReviewer: codexapp.ReviewerUser,
+		}, &thread); err != nil {
+			return fail(p.failure("thread/resume", err))
+		}
+		// The id is ours to begin with; a different one is a different
+		// conversation, and attaching to it would be worse than failing.
+		if thread.Thread.ID != "" && thread.Thread.ID != resume {
+			return fail(fmt.Errorf("%w: thread/resume returned %q, asked for %q", ErrProcessFailed, thread.Thread.ID, resume))
+		}
+		thread.Thread.ID = resume
+	} else {
+		// No sandbox field: externalSandbox exists only per turn (spec §4.3).
+		if err := p.conn.Call(hsCtx, codexapp.MethodThreadStart, codexapp.ThreadStartParams{
+			Cwd:               workDir,
+			ApprovalPolicy:    codexApprovalPolicy(codexModeAsk),
+			ApprovalsReviewer: codexapp.ReviewerUser,
+		}, &thread); err != nil {
+			return fail(p.failure("thread/start", err))
+		}
+		if thread.Thread.ID == "" {
+			return fail(fmt.Errorf("%w: thread/start returned no thread id", ErrProcessFailed))
+		}
+	}
+	models := codexListModels(hsCtx, p.conn)
+	// Under mu: the read loop is already delivering notifications, and it
+	// reads the thread id to tell this thread's from another's.
+	rt.mu.Lock()
+	rt.threadID, rt.model, rt.models = thread.Thread.ID, thread.Model, models
+	rt.mu.Unlock()
+	rt.tr.setThread(thread.Thread.ID)
+	log.Printf("agenthub: codex: session ready (thread %s, model %q)", thread.Thread.ID, thread.Model)
+	return rt, nil
+}
+
+// codexListModels is the model picker's list. Best effort: without it the
+// session still runs on the thread's default model, it just offers no choice.
+func codexListModels(ctx context.Context, conn *codexapp.Conn) []codexapp.Model {
+	var out []codexapp.Model
+	var cursor *string
+	for page := 0; page < codexModelPages; page++ {
+		var res codexapp.ModelListResponse
+		if err := conn.Call(ctx, codexapp.MethodModelList, codexapp.ModelListParams{Cursor: cursor}, &res); err != nil {
+			log.Printf("agenthub: codex: model/list: %v", err)
+			return out
+		}
+		for _, m := range res.Data {
+			if !m.Hidden && codexModelSlug(m) != "" {
+				out = append(out, m)
+			}
+		}
+		if res.NextCursor == nil || *res.NextCursor == "" {
+			return out
+		}
+		cursor = res.NextCursor
+	}
+	return out
+}
+
+func codexModelSlug(m codexapp.Model) string {
+	if m.Model != "" {
+		return m.Model
+	}
+	return m.ID
+}
+
+// codexApprovalPolicy is a mode's approvalPolicy. Anything unknown is Ask:
+// the fail-safe end.
+func codexApprovalPolicy(mode string) string {
+	switch mode {
+	case codexModeAuto:
+		return codexapp.ApprovalOnRequest
+	case codexModeFull:
+		return codexapp.ApprovalNever
+	}
+	return codexapp.ApprovalUntrusted
+}
+
+func (a *CodexAdapter) NormalizeUsage(meta json.RawMessage) (NormalizedUsage, error) {
+	out := NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}
+	if len(meta) == 0 {
+		return out, nil
+	}
+	var root struct {
+		Codex *struct {
+			Turn *codexapp.TokenUsageBreakdown `json:"turn"`
+		} `json:"codex"`
+	}
+	if err := json.Unmarshal(meta, &root); err != nil {
+		return out, err
+	}
+	if root.Codex != nil && root.Codex.Turn != nil {
+		out.InputTokens = int(root.Codex.Turn.InputTokens)
+		out.OutputTokens = int(root.Codex.Turn.OutputTokens)
+	}
+	return out, nil
+}
+
+func (a *CodexAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
+	return nil, "", nil
+}
+
+// Probe starts an installed codex under a session's jail, sends initialize
+// and stops it: agentruntime.Manager.Verifiers[CodexCLI], the proof that the
+// binary runs from where it was installed before a version is activated. In
+// a throwaway home, so verifying an install never touches the sign-in.
+func (a *CodexAdapter) Probe(ctx context.Context, rt agentruntime.Resolved) error {
+	root, err := os.MkdirTemp("", "devkit-codex-probe-")
+	if err != nil {
+		return fmt.Errorf("%w: probe directory: %v", ErrProcessFailed, err)
+	}
+	defer os.RemoveAll(root)
+	if resolved, err := filepath.EvalSymlinks(root); err == nil {
+		root = resolved
+	}
+	work := filepath.Join(root, "work")
+	if err := os.Mkdir(work, 0o700); err != nil {
+		return fmt.Errorf("%w: probe directory: %v", ErrProcessFailed, err)
+	}
+	probe := &CodexAdapter{opts: a.opts}
+	probe.opts.Home = filepath.Join(root, "home")
+	p, err := probe.start(work, runtimeLaunch(rt), OpenSpec{}, codexStoreEphemeral, codexNoHandler{})
+	if err != nil {
+		return err
+	}
+	defer p.stop()
+	callCtx, cancel := p.bounded(ctx, probeTimeout)
+	defer cancel()
+	return probe.initialize(callCtx, p)
+}
+
+// codexNoHandler serves a connection nobody converses on: notifications are
+// dropped and every server request refused.
+type codexNoHandler struct{}
+
+func (codexNoHandler) Notification(context.Context, string, json.RawMessage) {}
+
+func (codexNoHandler) Request(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *codexapp.RPCError) {
+	return nil, codexUnsupportedRequest(method)
+}
+
+// codexRuntime is one Codex thread in one app-server process.
+type codexRuntime struct {
+	conn        *codexapp.Conn
+	proc        *sandbox.Proc
+	tr          *codexTranslator
+	network     bool
+	cancelServe context.CancelFunc
+	serveDone   chan struct{}
+	// approvals numbers MCP approval cards that have no call of their own.
+	approvals atomic.Int64
+
+	mu sync.Mutex
+	// threadID and models are set once, at the end of the handshake, under
+	// mu because the read loop is running by then.
+	threadID  string
+	models    []codexapp.Model
+	handlers  RuntimeHandlers
+	announced bool
+	mode      string
+	model     string
+	turn      *codexTurn
+	usage     codexapp.TokenUsageBreakdown // the thread's running total
+	closed    bool
+	closing   bool
+	exitErr   error
+}
+
+// codexTurn is the turn a Prompt is waiting on. id is empty until turn/start
+// answers; a turn/completed that arrives before that waits in early.
+type codexTurn struct {
+	id         string
+	done       chan codexTurnEnd // one end, for Prompt
+	over       chan struct{}     // closed at the end, for anyone else
+	ended      bool
+	early      map[string]codexTurnEnd
+	failure    *codexapp.TurnError // an error Codex will not retry
+	interrupt  bool                // Cancel came before the id did
+	startUsage codexapp.TokenUsageBreakdown
+}
+
+type codexTurnEnd struct {
+	status string
+	err    *codexapp.TurnError
+}
+
+// finish ends t once. Called with the runtime's mu held.
+func (t *codexTurn) finish(end codexTurnEnd) {
+	if t.ended {
+		return
+	}
+	t.ended = true
+	t.done <- end
+	close(t.over)
+}
+
+func (r *codexRuntime) noteServeExit(serveErr error) {
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	r.closed = true
+	if serveErr != nil && !errors.Is(serveErr, context.Canceled) && r.exitErr == nil {
+		r.exitErr = serveErr
+	}
+}
+
+// Dead reports a process that went away without Close asking it to, so
+// Service resumes the session on a fresh one (see claudeRuntime.Dead).
+func (r *codexRuntime) Dead() (bool, error) {
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	return r.closed && !r.closing, r.exitErr
+}
+
+// ACPSessionID is the Codex thread id: what Load resumes.
+func (r *codexRuntime) ACPSessionID() string { return r.thread() }
+
+func (r *codexRuntime) thread() string {
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	return r.threadID
+}
+
+func (r *codexRuntime) Modes() *acp.SessionModeState {
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	return &acp.SessionModeState{CurrentModeID: r.mode, AvailableModes: append([]acp.SessionMode(nil), codexModes...)}
+}
+
+// SetMode takes effect from the next turn: approvalPolicy travels on every
+// turn/start, and a turn already running keeps the one it started with.
+func (r *codexRuntime) SetMode(_ context.Context, modeID string) error {
+	known := false
+	for _, m := range codexModes {
+		known = known || m.ID == modeID
+	}
+	if !known {
+		return fmt.Errorf("%w: unknown mode %q", ErrInvalidConfigOption, modeID)
+	}
+	r.mu.Lock()
+	if r.closed {
+		r.mu.Unlock()
+		return ErrSessionNotFound
+	}
+	r.mode = modeID
+	r.mu.Unlock()
+	r.emit([]codexUpdate{{Update: marshalUpdate(map[string]any{
+		"sessionUpdate": string(UpdateCurrentMode),
+		"currentModeId": modeID,
+	})}})
+	return nil
+}
+
+// SetConfigOption changes the model, from the next turn on.
+func (r *codexRuntime) SetConfigOption(_ context.Context, optionID, value string) error {
+	if optionID != codexModelOption {
+		return fmt.Errorf("%w: unknown option %q", ErrInvalidConfigOption, optionID)
+	}
+	r.mu.Lock()
+	known := false
+	for _, m := range r.models {
+		known = known || codexModelSlug(m) == value
+	}
+	if !known {
+		r.mu.Unlock()
+		return fmt.Errorf("%w: unknown model %q", ErrInvalidConfigOption, value)
+	}
+	if r.closed {
+		r.mu.Unlock()
+		return ErrSessionNotFound
+	}
+	r.model = value
+	update := r.configOptionsLocked()
+	r.mu.Unlock()
+	r.emit([]codexUpdate{{Update: update}})
+	return nil
+}
+
+// configOptionsLocked is the config_option_update for the model picker.
+func (r *codexRuntime) configOptionsLocked() json.RawMessage {
+	options := make([]map[string]string, 0, len(r.models))
+	for _, m := range r.models {
+		name := m.DisplayName
+		if name == "" {
+			name = codexModelSlug(m)
+		}
+		options = append(options, map[string]string{"value": codexModelSlug(m), "name": name, "description": m.Description})
+	}
+	return marshalUpdate(map[string]any{
+		"sessionUpdate": string(UpdateConfigOptions),
+		"configOptions": []map[string]any{{
+			"id":           codexModelOption,
+			"name":         "Model",
+			"category":     "model",
+			"type":         "select",
+			"currentValue": r.model,
+			"options":      options,
+		}},
+	})
+}
+
+// SetHandlers installs the callbacks. The first time an update handler
+// arrives, the model picker is announced through it: Codex has no
+// config_option_update of its own to send, and RuntimeModes carries modes only.
+func (r *codexRuntime) SetHandlers(h RuntimeHandlers) {
+	r.mu.Lock()
+	r.handlers = h
+	var announce json.RawMessage
+	if h.OnUpdate != nil && !r.announced && len(r.models) > 0 {
+		r.announced = true
+		announce = r.configOptionsLocked()
+	}
+	r.mu.Unlock()
+	if announce != nil {
+		h.OnUpdate(announce, nil)
+	}
+}
+
+func (r *codexRuntime) handlersSnapshot() RuntimeHandlers {
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	return r.handlers
+}
+
+// emit hands translated updates to the session. Before handlers are
+// installed — a resume — they are dropped: the transcript is already stored.
+func (r *codexRuntime) emit(updates []codexUpdate) {
+	if len(updates) == 0 {
+		return
+	}
+	onUpdate := r.handlersSnapshot().OnUpdate
+	if onUpdate == nil {
+		return
+	}
+	for _, u := range updates {
+		onUpdate(u.Update, u.Meta)
+	}
+}
+
+// codexInput turns a prompt into turn/start input. Text blocks are joined
+// into one text input; a pasted image goes as a data URL; an attached file's
+// contents go as text under its path. A link is named, not opened: Codex
+// reads files itself, inside the jail.
+func codexInput(prompt []acp.ContentBlock) []codexapp.UserInput {
+	var text strings.Builder
+	var images []codexapp.UserInput
+	add := func(s string) {
+		if text.Len() > 0 {
+			text.WriteString("\n\n")
+		}
+		text.WriteString(s)
+	}
+	for _, block := range prompt {
+		switch block.Type {
+		case "text", "":
+			if block.Text != "" {
+				add(block.Text)
+			}
+		case "image":
+			if block.Data != "" && block.MimeType != "" {
+				b, _ := json.Marshal(map[string]string{"type": "image", "url": "data:" + block.MimeType + ";base64," + block.Data})
+				images = append(images, b)
+			}
+		case "resource":
+			if block.Resource != nil && block.Resource.Text != "" {
+				add(block.Resource.URI + ":\n" + block.Resource.Text)
+			}
+		case "resource_link":
+			if block.URI != "" {
+				add(block.URI)
+			}
+		}
+	}
+	var out []codexapp.UserInput
+	if text.Len() > 0 {
+		out = append(out, codexapp.TextInput(text.String()))
+	}
+	return append(out, images...)
+}
+
+// Prompt runs one turn: turn/start answers at once with the turn in
+// progress, and the turn ends on its turn/completed (spec §5.1).
+func (r *codexRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, _ json.RawMessage) (PromptOutcome, error) {
+	if err := ctx.Err(); err != nil {
+		return PromptOutcome{}, err
+	}
+	input := codexInput(prompt)
+	if len(input) == 0 {
+		return PromptOutcome{}, fmt.Errorf("%w: nothing Codex can read", ErrInvalidPrompt)
+	}
+	r.mu.Lock()
+	if r.closed {
+		r.mu.Unlock()
+		return PromptOutcome{}, ErrSessionNotFound
+	}
+	if r.turn != nil {
+		r.mu.Unlock()
+		return PromptOutcome{}, ErrSessionBusy
+	}
+	t := &codexTurn{done: make(chan codexTurnEnd, 1), over: make(chan struct{}), early: map[string]codexTurnEnd{}, startUsage: r.usage}
+	r.turn = t
+	sandboxPolicy := codexapp.ExternalSandbox(r.network)
+	params := codexapp.TurnStartParams{
+		ThreadID:          r.threadID,
+		Input:             input,
+		ApprovalPolicy:    codexApprovalPolicy(r.mode),
+		ApprovalsReviewer: codexapp.ReviewerUser,
+		SandboxPolicy:     &sandboxPolicy,
+		Model:             r.model,
+	}
+	r.mu.Unlock()
+	defer func() {
+		r.mu.Lock()
+		if r.turn == t {
+			r.turn = nil
+		}
+		r.mu.Unlock()
+	}()
+
+	log.Printf("agenthub: codex turn/start sent (thread %s, policy %s)", params.ThreadID, params.ApprovalPolicy)
+	var res codexapp.TurnStartResponse
+	if err := r.conn.Call(ctx, codexapp.MethodTurnStart, params, &res); err != nil {
+		if ctx.Err() != nil {
+			return PromptOutcome{}, ErrCancelled
+		}
+		return PromptOutcome{}, r.callFailure("turn/start", err)
+	}
+	r.mu.Lock()
+	t.id = res.Turn.ID
+	if end, ok := t.early[t.id]; ok {
+		t.finish(end)
+	}
+	interrupt := t.interrupt && !t.ended
+	r.mu.Unlock()
+	if interrupt {
+		r.interrupt(t.id)
+	}
+
+	select {
+	case end := <-t.done:
+		log.Printf("agenthub: codex turn %s ended %s (thread %s)", t.id, end.status, params.ThreadID)
+		return r.outcome(t, end)
+	case <-r.serveDone:
+		r.emit(r.tr.closeOpen("Codex stopped before the turn finished."))
+		return PromptOutcome{}, r.callFailure("turn", codexapp.ErrClosed)
+	case <-ctx.Done():
+		r.interrupt(t.id)
+		select {
+		case <-t.over:
+		case <-r.serveDone:
+		case <-time.After(codexInterruptWait):
+			r.emit(r.tr.closeOpen("Cancelled."))
+		}
+		return PromptOutcome{}, ErrCancelled
+	}
+}
+
+// callFailure explains a failed call on a live session's connection.
+func (r *codexRuntime) callFailure(stage string, err error) error {
+	select {
+	case <-r.serveDone:
+		reason := ""
+		if r.proc != nil {
+			reason = stderrReason(r.proc)
+			logStderrTail("codex "+stage, r.proc)
+		}
+		return fmt.Errorf("%w: %s: codex exited%s", ErrProcessFailed, stage, reason)
+	default:
+		return fmt.Errorf("%w: %s: %v", ErrProcessFailed, stage, err)
+	}
+}
+
+// outcome is what Prompt returns for a turn that ended.
+func (r *codexRuntime) outcome(t *codexTurn, end codexTurnEnd) (PromptOutcome, error) {
+	r.mu.Lock()
+	used := codexapp.TokenUsageBreakdown{
+		TotalTokens:           r.usage.TotalTokens - t.startUsage.TotalTokens,
+		InputTokens:           r.usage.InputTokens - t.startUsage.InputTokens,
+		CachedInputTokens:     r.usage.CachedInputTokens - t.startUsage.CachedInputTokens,
+		OutputTokens:          r.usage.OutputTokens - t.startUsage.OutputTokens,
+		ReasoningOutputTokens: r.usage.ReasoningOutputTokens - t.startUsage.ReasoningOutputTokens,
+	}
+	r.mu.Unlock()
+	meta := marshalUpdate(map[string]any{"codex": map[string]any{"turnId": t.id, "turn": used}})
+	switch end.status {
+	case codexapp.TurnCompleted:
+		return PromptOutcome{StopReason: "end_turn", Meta: meta}, nil
+	case codexapp.TurnInterrupted:
+		return PromptOutcome{StopReason: "cancelled", Meta: meta}, nil
+	}
+	return PromptOutcome{}, codexTurnError(end.err)
+}
+
+// codexTurnError maps a failed turn's error. unauthorized is a sign-in to
+// offer; the budget errors say what ran out; anything else carries Codex's
+// own message.
+func codexTurnError(e *codexapp.TurnError) error {
+	if e == nil {
+		return fmt.Errorf("%w: the Codex turn failed", ErrProcessFailed)
+	}
+	switch codexErrorKind(e.CodexErrorInfo) {
+	case "unauthorized":
+		return fmt.Errorf("%w: %s", ErrAgentAuthRequired, e.Message)
+	case "usageLimitExceeded":
+		return fmt.Errorf("%w: Codex usage limit reached: %s", ErrProcessFailed, e.Message)
+	case "contextWindowExceeded":
+		return fmt.Errorf("%w: the conversation no longer fits the model's context window: %s", ErrProcessFailed, e.Message)
+	case "sessionBudgetExceeded":
+		return fmt.Errorf("%w: the session's budget is spent: %s", ErrProcessFailed, e.Message)
+	}
+	return fmt.Errorf("%w: %s", ErrProcessFailed, e.Message)
+}
+
+// codexErrorKind reads codexErrorInfo, which is a bare string for most kinds
+// and an object keyed by the kind for the ones that carry details.
+func codexErrorKind(raw json.RawMessage) string {
+	var kind string
+	if json.Unmarshal(raw, &kind) == nil {
+		return kind
+	}
+	var obj map[string]json.RawMessage
+	if json.Unmarshal(raw, &obj) == nil {
+		for k := range obj {
+			return k
+		}
+	}
+	return ""
+}
+
+// interrupt asks Codex to stop the turn. Best effort: a process that does not
+// answer is dealt with by Cancel's watchdog.
+func (r *codexRuntime) interrupt(turnID string) {
+	if turnID == "" {
+		return
+	}
+	thread := r.thread()
+	ctx, cancel := context.WithTimeout(context.Background(), codexInterruptWait)
+	defer cancel()
+	if err := r.conn.Call(ctx, codexapp.MethodTurnInterrupt, codexapp.TurnInterruptParams{ThreadID: thread, TurnID: turnID}, nil); err != nil {
+		log.Printf("agenthub: codex turn/interrupt (thread %s): %v", thread, err)
+	}
+}
+
+// Cancel interrupts the running turn (spec §5.1). Codex ends it with
+// turn/completed interrupted, and Prompt returns a cancelled stop. A turn that
+// does not end in codexInterruptWait is a process that stopped answering:
+// it is killed, and the session resumes on a fresh one.
+func (r *codexRuntime) Cancel(context.Context) error {
+	r.mu.Lock()
+	t, closed := r.turn, r.closed
+	id := ""
+	if t != nil {
+		id = t.id
+		if id == "" {
+			t.interrupt = true
+		}
+	}
+	r.mu.Unlock()
+	if closed || t == nil {
+		return nil
+	}
+	go r.interrupt(id)
+	go func() {
+		select {
+		case <-t.over:
+		case <-r.serveDone:
+		case <-time.After(codexInterruptWait):
+			log.Printf("agenthub: codex: turn did not stop after interrupt; killing the process (thread %s)", r.thread())
+			_ = r.proc.Kill()
+		}
+	}()
+	return nil
+}
+
+func (r *codexRuntime) Close(context.Context) error {
+	r.mu.Lock()
+	if r.closed {
+		r.mu.Unlock()
+		return nil
+	}
+	r.closed, r.closing = true, true
+	r.mu.Unlock()
+	if r.cancelServe != nil {
+		r.cancelServe()
+	}
+	if r.conn != nil {
+		_ = r.conn.Close()
+	}
+	if r.proc != nil {
+		_ = r.proc.Kill()
+	}
+	if r.serveDone != nil {
+		select {
+		case <-r.serveDone:
+		case <-time.After(2 * time.Second):
+		}
+	}
+	return nil
+}
+
+// codexConnHandler is the runtime's side of the app-server connection.
+type codexConnHandler struct{ rt *codexRuntime }
+
+// Notification runs on the read loop, in wire order, so a turn's last updates
+// — including the tool calls turnCompleted closes — reach the session before
+// Prompt learns the turn is over.
+func (h *codexConnHandler) Notification(_ context.Context, method string, params json.RawMessage) {
+	r := h.rt
+	r.emit(r.tr.Translate(method, params))
+	switch method {
+	case codexapp.MethodTurnCompleted:
+		r.onTurnCompleted(params)
+	case codexapp.MethodError:
+		r.onError(params)
+	case codexapp.MethodTokenUsageUpdated:
+		r.onUsage(params)
+	default:
+		if !codexTranslates(method) {
+			codexNoteIgnored(method)
+		}
+	}
+}
+
+// codexIgnoredSeen dedupes the "not translated" log, per method, process-wide.
+// The method name only: remoteControl/status/changed carries the host name
+// and an installation id, and params are never logged (spec §5.3).
+var codexIgnoredSeen sync.Map
+
+func codexNoteIgnored(method string) {
+	if _, seen := codexIgnoredSeen.LoadOrStore(method, struct{}{}); !seen {
+		log.Printf("agenthub: codex: not translating %s", method)
+	}
+}
+
+func (r *codexRuntime) ownThread(threadID string) bool {
+	return threadID == "" || threadID == r.thread()
+}
+
+func (r *codexRuntime) onTurnCompleted(params json.RawMessage) {
+	var n codexapp.TurnCompletedNotification
+	if json.Unmarshal(params, &n) != nil || !r.ownThread(n.ThreadID) {
+		return
+	}
+	r.mu.Lock()
+	defer r.mu.Unlock()
+	t := r.turn
+	if t == nil {
+		return
+	}
+	end := codexTurnEnd{status: n.Turn.Status, err: n.Turn.Error}
+	if end.err == nil && end.status == codexapp.TurnFailed {
+		end.err = t.failure
+	}
+	switch t.id {
+	case "":
+		t.early[n.Turn.ID] = end
+	case n.Turn.ID:
+		t.finish(end)
+	}
+}
+
+// onError handles error. willRetry is Codex retrying on its own: logged, the
+// turn goes on. Otherwise the turn is over, and turn/completed normally says
+// so right after; if it does not come, the error ends the turn itself.
+func (r *codexRuntime) onError(params json.RawMessage) {
+	var n codexapp.ErrorNotification
+	if json.Unmarshal(params, &n) != nil || !r.ownThread(n.ThreadID) {
+		return
+	}
+	if n.WillRetry {
+		log.Printf("agenthub: codex: retrying after %q (thread %s)", n.Error.Message, n.ThreadID)
+		return
+	}
+	r.mu.Lock()
+	t := r.turn
+	if t == nil || t.ended || (t.id != "" && n.TurnID != "" && n.TurnID != t.id) {
+		r.mu.Unlock()
+		return
+	}
+	failure := n.Error
+	t.failure = &failure
+	r.mu.Unlock()
+	time.AfterFunc(codexErrorGrace, func() {
+		r.mu.Lock()
+		pending := r.turn == t && !t.ended
+		r.mu.Unlock()
+		if !pending {
+			return
+		}
+		r.emit(r.tr.closeOpen("The turn failed."))
+		r.mu.Lock()
+		t.finish(codexTurnEnd{status: codexapp.TurnFailed, err: &failure})
+		r.mu.Unlock()
+	})
+}
+
+func (r *codexRuntime) onUsage(params json.RawMessage) {
+	var n codexapp.TokenUsageUpdatedNotification
+	if json.Unmarshal(params, &n) != nil || !r.ownThread(n.ThreadID) {
+		return
+	}
+	r.mu.Lock()
+	r.usage = n.TokenUsage.Total
+	r.mu.Unlock()
+}
+
+// Request answers the app-server's requests on their own goroutine. Every
+// wait selects on ctx, which ends when Codex withdraws the request
+// (serverRequest/resolved) or the connection closes — codexapp.Handler's
+// contract.
+func (h *codexConnHandler) Request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, *codexapp.RPCError) {
+	r := h.rt
+	switch method {
+	case codexapp.MethodPermissionsApproval:
+		response, notice := codexPermissionsRequest(params)
+		r.emit(notice)
+		return response, nil
+	case codexapp.MethodMcpElicitation:
+		ref := strconv.FormatInt(r.approvals.Add(1), 10)
+		a, ok := r.tr.mcpToolApprovalFor(r.thread(), ref, params)
+		if !ok {
+			return nil, codexUnsupportedRequest(method)
+		}
+		return r.ask(ctx, a), nil
+	}
+	a, ok := r.tr.approvalFor(r.thread(), method, params)
+	switch {
+	case !ok:
+		return nil, codexUnsupportedRequest(method)
+	case a.Malformed:
+		return a.Response(nil), nil
+	}
+	return r.ask(ctx, a), nil
+}
+
+// ask puts an approval to the person. With nobody to ask — no turn handler
+// installed — the answer is the fail-closed one.
+func (r *codexRuntime) ask(ctx context.Context, a codexApproval) json.RawMessage {
+	onPermission := r.handlersSnapshot().OnPermission
+	if onPermission == nil {
+		return a.Response(nil)
+	}
+	// A denial comes back as an outcome and an error together; the outcome
+	// is still the answer, and one Response cannot read maps to cancel.
+	outcome, _ := onPermission(ctx, a.Params)
+	return a.Response(outcome)
+}
diff --git a/internal/agenthub/types.go b/internal/agenthub/types.go
index fd5d2713cc9eff18652fc0db9516f0b5cccf2472..94d9bd50682a68376eef046518c97b68dde20c25 100644
--- a/internal/agenthub/types.go
+++ b/internal/agenthub/types.go
@@ -11,6 +11,7 @@ type AgentType string
 const (
 	AgentTypeMock   AgentType = "mock"
 	AgentTypeClaude AgentType = "claude"
+	AgentTypeCodex  AgentType = "codex"
 )
 
 // SessionStatus is the hub-visible lifecycle state of an AgentSession.
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ -run Codex -count=1 -race -v 2>&1 | grep -E '^(--- |ok|FAIL)'` rồi `go test ./internal/agenthub/ ./internal/arch/ -count=1`
Expected: PASS; `TestCodexFakeAppServer` là SKIP (nó là tiến trình giả, không phải test). `go vet ./internal/agenthub/`, `gofmt -l internal` sạch.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/adapter_codex.go internal/agenthub/adapter_codex_test.go internal/agenthub/types.go
git commit -m "feat(agenthub): CodexAdapter runs codex app-server in the jail

One process per session under the Claude-shaped jail, with DevKit's own
CODEX_HOME. Every turn carries the mode's approval policy, the user as
reviewer and externalSandbox; a turn ends on its own turn/completed,
Stop is turn/interrupt. Approvals and MCP tool approvals go through the
permission gate and fail closed without one.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 6: Đăng nhập trong app (`InAppSignIn`)

**Model:** sonnet

**Vì sao:** Spec §6.1, §6.4. Một app-server ngắn hạn trong jail (store `file`, cùng home) chạy `account/login/start` `chatgpt` (trình duyệt — mặc định; callback `localhost:1455` tới jail vì dùng chung loopback) hoặc `chatgptDeviceCode`; sống tới `account/login/completed`, huỷ, hoặc 15 phút. Không đọc `account/read` trên chính tiến trình đó (Phase 0: trả `null` ngay sau khi xong). Trạng thái và đăng xuất dùng tiến trình mới. `Service` định tuyến theo vendor; server mode từ chối.

**Files:** Create `internal/agenthub/codex_auth.go`; Modify `internal/agenthub/adapter_codex.go` (`OnSignInCompleted`, trường sign-in), `internal/agenthub/adapter_servermode.go`; Test `internal/agenthub/adapter_codex_test.go` (app-server giả thêm login/cancel/logout + test)

- [ ] **Step 1: Áp patch test** (`extract t6-test`)

<!-- patch t6-test -->
```diff
diff --git a/internal/agenthub/adapter_codex_test.go b/internal/agenthub/adapter_codex_test.go
index 50a7787f2a19c66593ceb22f12b69ff626f7f9f3..e2af3629e4b7c0aefff7aaa1966092d9d68e331d 100644
--- a/internal/agenthub/adapter_codex_test.go
+++ b/internal/agenthub/adapter_codex_test.go
@@ -152,10 +152,28 @@ func runFakeCodex(scenario, logPath string) {
 			if scenario == "signedout" {
 				reply(map[string]any{"account": nil, "requiresOpenaiAuth": true})
 			} else {
-				reply(map[string]any{"account": map[string]any{"type": "chatgpt"}, "requiresOpenaiAuth": true})
+				reply(map[string]any{"account": map[string]any{"type": "chatgpt", "email": "me@example.com", "planType": "plus"}, "requiresOpenaiAuth": true})
 			}
 		case "account/login/start":
-			reply(map[string]any{"type": "apiKey"})
+			var login codexapp.LoginStartParams
+			_ = json.Unmarshal(m.Params, &login)
+			switch login.Type {
+			case "chatgpt":
+				reply(map[string]any{"type": "chatgpt", "loginId": "login-1", "authUrl": "https://auth.example/authorize"})
+			case "chatgptDeviceCode":
+				reply(map[string]any{"type": "chatgptDeviceCode", "loginId": "login-1", "verificationUrl": "https://auth.example/device", "userCode": "ABCD-1234"})
+			default:
+				reply(map[string]any{"type": "apiKey"})
+				continue
+			}
+			if scenario == "login" {
+				go func() {
+					time.Sleep(50 * time.Millisecond)
+					notify("account/login/completed", map[string]any{"loginId": "login-1", "success": true, "error": nil})
+				}()
+			}
+		case "account/login/cancel", "account/logout":
+			reply(map[string]any{})
 		case "thread/start":
 			thread = "th-new"
 			reply(map[string]any{"thread": map[string]any{"id": thread}, "model": "gpt-5.5"})
@@ -838,3 +856,145 @@ func TestCodexInputCarriesTextImagesAndFiles(t *testing.T) {
 func agentruntimeResolved(bin string) agentruntime.Resolved {
 	return agentruntime.Resolved{Command: bin, ReadOnlyPaths: []string{filepath.Dir(bin)}}
 }
+
+func waitSignIn(t *testing.T, results <-chan SignInResult) SignInResult {
+	t.Helper()
+	select {
+	case r := <-results:
+		return r
+	case <-time.After(5 * time.Second):
+		t.Fatal("the sign-in never finished")
+		return SignInResult{}
+	}
+}
+
+// Browser sign-in (spec §6.1): Codex's own login, started in a short-lived
+// app-server under the session jail with the ChatGPT store, which ends the
+// moment the login completes.
+func TestCodexBrowserSignIn(t *testing.T) {
+	f := newCodexFixture(t, "login")
+	results := make(chan SignInResult, 1)
+	f.adapter.opts.OnSignInCompleted = func(r SignInResult) { results <- r }
+	flow, err := f.adapter.StartBrowserSignIn(context.Background())
+	if err != nil {
+		t.Fatal(err)
+	}
+	if flow.Method != SignInBrowser || flow.AuthURL != "https://auth.example/authorize" || flow.LoginID != "login-1" {
+		t.Fatalf("flow = %+v", flow)
+	}
+	if r := waitSignIn(t, results); !r.Success || r.LoginID != "login-1" || r.Agent != AgentTypeCodex {
+		t.Fatalf("result = %+v", r)
+	}
+	pol, cmd := f.runner.started()
+	if !strings.Contains(strings.Join(cmd.Args, " "), `cli_auth_credentials_store="file"`) || pol.Env["CODEX_HOME"] != f.home {
+		t.Fatalf("sign-in must write the ChatGPT store in DevKit's home: %v %v", cmd.Args, pol.Env["CODEX_HOME"])
+	}
+	if pol.Network != sandbox.NetworkHost {
+		t.Fatal("the browser callback reaches the jail over the host loopback")
+	}
+	// account/read on the process that just signed in says "no account"
+	// (Phase 0); status is read by a fresh one.
+	if n := len(f.runner.sent(t, "account/read")); n != 0 {
+		t.Fatalf("account/read sent %d times on the sign-in process", n)
+	}
+}
+
+func TestCodexDeviceSignInShowsTheCode(t *testing.T) {
+	f := newCodexFixture(t, "login")
+	f.adapter.opts.OnSignInCompleted = func(SignInResult) {}
+	flow, err := f.adapter.StartDeviceSignIn(context.Background())
+	if err != nil {
+		t.Fatal(err)
+	}
+	if flow.Method != SignInDeviceCode || flow.UserCode != "ABCD-1234" || flow.VerificationURL == "" {
+		t.Fatalf("flow = %+v", flow)
+	}
+}
+
+// Cancel withdraws the login and stops the process; a timeout does the same
+// on its own.
+func TestCodexSignInCancelAndTimeout(t *testing.T) {
+	defer func(old time.Duration) { codexSignInTimeout = old }(codexSignInTimeout)
+	for _, how := range []string{"cancel", "timeout"} {
+		t.Run(how, func(t *testing.T) {
+			if how == "timeout" {
+				codexSignInTimeout = 100 * time.Millisecond
+			} else {
+				codexSignInTimeout = time.Minute
+			}
+			f := newCodexFixture(t, "loginWait")
+			results := make(chan SignInResult, 1)
+			f.adapter.opts.OnSignInCompleted = func(r SignInResult) { results <- r }
+			flow, err := f.adapter.StartBrowserSignIn(context.Background())
+			if err != nil {
+				t.Fatal(err)
+			}
+			if how == "cancel" {
+				if err := f.adapter.CancelSignIn(context.Background(), flow.LoginID); err != nil {
+					t.Fatal(err)
+				}
+			}
+			if r := waitSignIn(t, results); r.Success || r.Error == "" {
+				t.Fatalf("result = %+v", r)
+			}
+			if cancels := f.runner.sent(t, "account/login/cancel"); len(cancels) != 1 || !strings.Contains(string(cancels[0]), "login-1") {
+				t.Fatalf("account/login/cancel = %s", cancels)
+			}
+		})
+	}
+}
+
+func TestCodexSignInStatusAndSignOut(t *testing.T) {
+	f := newCodexFixture(t, "turn")
+	status, err := f.adapter.SignInStatus(context.Background())
+	if err != nil {
+		t.Fatal(err)
+	}
+	if status != (SignInStatus{SignedIn: true, Method: "chatgpt", Email: "me@example.com", Plan: "plus"}) {
+		t.Fatalf("status = %+v", status)
+	}
+	if err := f.adapter.SignOut(context.Background()); err != nil {
+		t.Fatal(err)
+	}
+	if len(f.runner.sent(t, "account/logout")) != 1 {
+		t.Fatal("SignOut must send account/logout")
+	}
+
+	out := newCodexFixture(t, "signedout")
+	if status, err := out.adapter.SignInStatus(context.Background()); err != nil || status.SignedIn {
+		t.Fatalf("signed out: %+v, %v", status, err)
+	}
+
+	// An API key is what sessions sign in with, so it is the status; no
+	// process is needed to say so.
+	key := newCodexFixture(t, "signedout")
+	key.adapter.opts.APIKey = func(context.Context) (string, error) { return "sk-1", nil }
+	if status, err := key.adapter.SignInStatus(context.Background()); err != nil || status != (SignInStatus{SignedIn: true, Method: "apiKey"}) {
+		t.Fatalf("api key: %+v, %v", status, err)
+	}
+}
+
+// Service routes sign-in to the vendor that supports it and says plainly
+// when one does not.
+func TestServiceSignInRoutesByVendor(t *testing.T) {
+	f := newCodexFixture(t, "turn")
+	reg := NewRegistry()
+	reg.Register(f.adapter)
+	reg.Register(NewMockAdapter())
+	svc := &Service{reg: reg}
+	if status, err := svc.SignInStatus(context.Background(), AgentTypeCodex); err != nil || !status.SignedIn {
+		t.Fatalf("codex: %+v, %v", status, err)
+	}
+	if _, err := svc.SignInStatus(context.Background(), AgentTypeMock); !errors.Is(err, ErrUnsupported) {
+		t.Fatalf("mock: %v, want ErrUnsupported", err)
+	}
+	if _, err := svc.StartSignIn(context.Background(), AgentTypeCodex, "telepathy"); !errors.Is(err, ErrAuthMethodUnknown) {
+		t.Fatalf("unknown method: %v", err)
+	}
+	for _, a := range ForServerMode(f.adapter) {
+		reg.Register(a)
+	}
+	if _, err := svc.StartSignIn(context.Background(), AgentTypeCodex, SignInBrowser); !errors.Is(err, ErrUnavailableInServerMode) {
+		t.Fatalf("server mode: %v", err)
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận hỏng**

Run: `go test ./internal/agenthub/ -run 'SignIn' -count=1`
Expected: FAIL build — `undefined: SignInResult` / `StartBrowserSignIn`.

- [ ] **Step 3: Áp patch hiện thực** (`extract t6-impl`)

<!-- patch t6-impl -->
```diff
diff --git a/internal/agenthub/adapter_codex.go b/internal/agenthub/adapter_codex.go
index cb6a663e8807374fda82480336cbcd66725adab9..e482017e6c320091399c7915a07f46eafe7fa678 100644
--- a/internal/agenthub/adapter_codex.go
+++ b/internal/agenthub/adapter_codex.go
@@ -88,6 +88,8 @@ type CodexOptions struct {
 	Runner SandboxRunner
 	// Version is DevKit's, sent as clientInfo.version.
 	Version string
+	// OnSignInCompleted reports how an in-app sign-in ended (InAppSignIn).
+	OnSignInCompleted func(SignInResult)
 }
 
 // CodexAdapter runs `codex app-server` inside DevKit's jail and speaks ACP to
@@ -95,6 +97,9 @@ type CodexOptions struct {
 // through PermissionWaiter.
 type CodexAdapter struct {
 	opts CodexOptions
+
+	signInMu sync.Mutex
+	signIn   *codexSignIn
 }
 
 // NewCodexAdapter returns the Codex adapter. Every process it starts goes
diff --git a/internal/agenthub/adapter_servermode.go b/internal/agenthub/adapter_servermode.go
index 5e07cf215edf7e79d0f789753228c5fb136d2c58..e570e48362f9e741ff85122c5ae296d8f3251601 100644
--- a/internal/agenthub/adapter_servermode.go
+++ b/internal/agenthub/adapter_servermode.go
@@ -22,6 +22,7 @@ var (
 	_ SessionLoader = (*ServerModeAdapter)(nil)
 	_ AuthProvider  = (*ServerModeAdapter)(nil)
 	_ RuntimeForker = (*ServerModeAdapter)(nil)
+	_ InAppSignIn   = (*ServerModeAdapter)(nil)
 )
 
 // NewServerModeAdapter returns a stub for t whose sessions never start.
@@ -70,3 +71,23 @@ func (a *ServerModeAdapter) SignInCommand(context.Context, string) (SignInComman
 func (a *ServerModeAdapter) Fork(context.Context, OpenSpec, string, string) (SessionRuntime, error) {
 	return nil, ErrUnavailableInServerMode
 }
+
+// The in-app sign-in refuses too: a login started from a browser tab on the
+// unauthenticated bridge would leave a credential for any local caller.
+func (a *ServerModeAdapter) SignInStatus(context.Context) (SignInStatus, error) {
+	return SignInStatus{}, ErrUnavailableInServerMode
+}
+
+func (a *ServerModeAdapter) StartBrowserSignIn(context.Context) (SignInFlow, error) {
+	return SignInFlow{}, ErrUnavailableInServerMode
+}
+
+func (a *ServerModeAdapter) StartDeviceSignIn(context.Context) (SignInFlow, error) {
+	return SignInFlow{}, ErrUnavailableInServerMode
+}
+
+func (a *ServerModeAdapter) CancelSignIn(context.Context, string) error {
+	return ErrUnavailableInServerMode
+}
+
+func (a *ServerModeAdapter) SignOut(context.Context) error { return ErrUnavailableInServerMode }
diff --git a/internal/agenthub/codex_auth.go b/internal/agenthub/codex_auth.go
new file mode 100644
index 0000000000000000000000000000000000000000..5896253ee01f378ad3c3f482fbaa8d0e57d146f4
--- /dev/null
+++ b/internal/agenthub/codex_auth.go
@@ -0,0 +1,373 @@
+package agenthub
+
+import (
+	"context"
+	"encoding/json"
+	"fmt"
+	"log"
+	"os"
+	"path/filepath"
+	"sync"
+	"time"
+
+	"devkit/internal/codexapp"
+)
+
+// SignInStatus is a vendor's sign-in as the in-app panel shows it.
+type SignInStatus struct {
+	SignedIn bool `json:"signedIn"`
+	// Method is "chatgpt" or "apiKey".
+	Method string `json:"method,omitempty"`
+	Email  string `json:"email,omitempty"`
+	Plan   string `json:"plan,omitempty"`
+}
+
+// SignInFlow is a sign-in in progress: a page to open (browser) or a code to
+// type on one (device code). LoginID names it to CancelSignIn.
+type SignInFlow struct {
+	LoginID         string `json:"loginId"`
+	Method          string `json:"method"`
+	AuthURL         string `json:"authUrl,omitempty"`
+	VerificationURL string `json:"verificationUrl,omitempty"`
+	UserCode        string `json:"userCode,omitempty"`
+}
+
+// SignInResult is how a flow ended.
+type SignInResult struct {
+	Agent   AgentType `json:"agent"`
+	LoginID string    `json:"loginId"`
+	Success bool      `json:"success"`
+	Error   string    `json:"error,omitempty"`
+}
+
+// Sign-in methods a flow can use.
+const (
+	SignInBrowser    = "browser"
+	SignInDeviceCode = "deviceCode"
+)
+
+// InAppSignIn is a vendor that signs in inside DevKit rather than in a
+// terminal (AuthProvider): DevKit starts the vendor's own login and shows
+// its page or code; the credential lands in the vendor's home, never in
+// DevKit (spec §6). UI only — there is no MCP tool for any of it.
+type InAppSignIn interface {
+	SignInStatus(ctx context.Context) (SignInStatus, error)
+	StartBrowserSignIn(ctx context.Context) (SignInFlow, error)
+	StartDeviceSignIn(ctx context.Context) (SignInFlow, error)
+	CancelSignIn(ctx context.Context, loginID string) error
+	SignOut(ctx context.Context) error
+}
+
+var _ InAppSignIn = (*CodexAdapter)(nil)
+
+func (s *Service) inAppSignIn(t AgentType) (InAppSignIn, error) {
+	adapter, ok := s.reg.Get(t)
+	if !ok {
+		return nil, ErrVendorUnknown
+	}
+	signIn, ok := adapter.(InAppSignIn)
+	if !ok {
+		return nil, fmt.Errorf("%w: agent %q does not sign in inside DevKit", ErrUnsupported, t)
+	}
+	return signIn, nil
+}
+
+// SignInStatus reads an agent type's sign-in.
+func (s *Service) SignInStatus(ctx context.Context, t AgentType) (SignInStatus, error) {
+	signIn, err := s.inAppSignIn(t)
+	if err != nil {
+		return SignInStatus{}, err
+	}
+	return signIn.SignInStatus(ctx)
+}
+
+// StartSignIn begins a sign-in by browser or device code.
+func (s *Service) StartSignIn(ctx context.Context, t AgentType, method string) (SignInFlow, error) {
+	signIn, err := s.inAppSignIn(t)
+	if err != nil {
+		return SignInFlow{}, err
+	}
+	switch method {
+	case SignInBrowser:
+		return signIn.StartBrowserSignIn(ctx)
+	case SignInDeviceCode:
+		return signIn.StartDeviceSignIn(ctx)
+	}
+	return SignInFlow{}, ErrAuthMethodUnknown
+}
+
+// CancelSignIn abandons a sign-in in progress.
+func (s *Service) CancelSignIn(ctx context.Context, t AgentType, loginID string) error {
+	signIn, err := s.inAppSignIn(t)
+	if err != nil {
+		return err
+	}
+	return signIn.CancelSignIn(ctx, loginID)
+}
+
+// SignOut removes an agent type's stored sign-in.
+func (s *Service) SignOut(ctx context.Context, t AgentType) error {
+	signIn, err := s.inAppSignIn(t)
+	if err != nil {
+		return err
+	}
+	return signIn.SignOut(ctx)
+}
+
+// codexSignInTimeout is how long a sign-in waits for the person (spec §6.1).
+// A var so a test need not wait it out.
+var codexSignInTimeout = 15 * time.Minute
+
+// codexSignIn is the one sign-in in progress: a short-lived app-server under
+// the session jail, with the ChatGPT store, alive until the login completes,
+// is cancelled or times out.
+type codexSignIn struct {
+	loginID string
+	proc    *codexProcess
+	work    string
+	timer   *time.Timer
+	once    sync.Once
+	// early is a completion that arrived before login/start answered.
+	early *SignInResult
+}
+
+// StartBrowserSignIn is the default (spec §6.1): Codex serves the callback on
+// localhost:1455 from inside the jail, which shares the host's loopback.
+func (a *CodexAdapter) StartBrowserSignIn(ctx context.Context) (SignInFlow, error) {
+	return a.startSignIn(ctx, codexapp.LoginChatGPT, SignInBrowser)
+}
+
+// StartDeviceSignIn is for a machine that cannot open a browser. It is off by
+// default per ChatGPT account (measured): the person must enable it under
+// ChatGPT → Settings → Security first, and Codex does not say so — the UI has
+// to.
+func (a *CodexAdapter) StartDeviceSignIn(ctx context.Context) (SignInFlow, error) {
+	return a.startSignIn(ctx, codexapp.LoginDeviceCode, SignInDeviceCode)
+}
+
+func (a *CodexAdapter) startSignIn(ctx context.Context, loginType, method string) (SignInFlow, error) {
+	a.signInMu.Lock()
+	previous := a.signIn
+	a.signInMu.Unlock()
+	if previous != nil {
+		// One at a time: a second Sign in replaces the first.
+		a.finishSignIn(previous, SignInResult{LoginID: previous.loginID, Error: "Replaced by a new sign-in."}, true)
+	}
+
+	l, err := a.resolveCommand()
+	if err != nil {
+		return SignInFlow{}, err
+	}
+	work, err := codexScratchDir("devkit-codex-signin-")
+	if err != nil {
+		return SignInFlow{}, err
+	}
+	s := &codexSignIn{work: work}
+	p, err := a.start(work, l, OpenSpec{}, codexStoreFile, &codexSignInHandler{a: a, s: s})
+	if err != nil {
+		_ = os.RemoveAll(work)
+		return SignInFlow{}, err
+	}
+	s.proc = p
+	fail := func(err error) (SignInFlow, error) {
+		p.stop()
+		_ = os.RemoveAll(work)
+		return SignInFlow{}, err
+	}
+	callCtx, cancel := p.bounded(ctx, handshakeTimeout)
+	defer cancel()
+	if err := a.initialize(callCtx, p); err != nil {
+		return fail(err)
+	}
+	var res codexapp.LoginStartResponse
+	if err := p.conn.Call(callCtx, codexapp.MethodAccountLoginStart, codexapp.LoginStartParams{Type: loginType}, &res); err != nil {
+		return fail(p.failure("account/login/start", err))
+	}
+	if res.LoginID == "" {
+		return fail(fmt.Errorf("%w: account/login/start returned no login id", ErrProcessFailed))
+	}
+
+	a.signInMu.Lock()
+	s.loginID = res.LoginID
+	a.signIn = s
+	s.timer = time.AfterFunc(codexSignInTimeout, func() {
+		a.finishSignIn(s, SignInResult{LoginID: res.LoginID, Error: "Sign-in timed out."}, true)
+	})
+	early := s.early
+	a.signInMu.Unlock()
+	if early != nil {
+		early.LoginID = res.LoginID
+		go a.finishSignIn(s, *early, false)
+	}
+	log.Printf("agenthub: codex: %s sign-in started", method)
+	return SignInFlow{
+		LoginID:         res.LoginID,
+		Method:          method,
+		AuthURL:         res.AuthURL,
+		VerificationURL: res.VerificationURL,
+		UserCode:        res.UserCode,
+	}, nil
+}
+
+// finishSignIn ends s once: withdraws the login when asked, stops the
+// process and reports the result. Never on the read loop of s's own
+// connection — stopping waits for that loop to end.
+func (a *CodexAdapter) finishSignIn(s *codexSignIn, result SignInResult, withdraw bool) {
+	s.once.Do(func() {
+		a.signInMu.Lock()
+		if a.signIn == s {
+			a.signIn = nil
+		}
+		if s.timer != nil {
+			s.timer.Stop()
+		}
+		a.signInMu.Unlock()
+		if withdraw && s.loginID != "" {
+			ctx, cancel := s.proc.bounded(context.Background(), 5*time.Second)
+			if err := s.proc.conn.Call(ctx, codexapp.MethodAccountLoginCancel, codexapp.CancelLoginParams{LoginID: s.loginID}, nil); err != nil {
+				log.Printf("agenthub: codex: account/login/cancel: %v", err)
+			}
+			cancel()
+		}
+		s.proc.stop()
+		_ = os.RemoveAll(s.work)
+		// The code and the page are never logged; whether it worked is.
+		log.Printf("agenthub: codex: sign-in ended (success %v)", result.Success)
+		result.Agent = AgentTypeCodex
+		if a.opts.OnSignInCompleted != nil {
+			a.opts.OnSignInCompleted(result)
+		}
+	})
+}
+
+// CancelSignIn withdraws the sign-in in progress. A login id that is not the
+// current one is already over: nothing to do.
+func (a *CodexAdapter) CancelSignIn(_ context.Context, loginID string) error {
+	a.signInMu.Lock()
+	s := a.signIn
+	a.signInMu.Unlock()
+	if s == nil || s.loginID != loginID {
+		return nil
+	}
+	a.finishSignIn(s, SignInResult{LoginID: loginID, Error: "Sign-in cancelled."}, true)
+	return nil
+}
+
+// codexSignInHandler waits for the login to complete. account/read is not
+// asked on this process: right after account/login/completed it still
+// answers "no account" (Phase 0), so status is read by a fresh process.
+type codexSignInHandler struct {
+	a *CodexAdapter
+	s *codexSignIn
+}
+
+func (h *codexSignInHandler) Notification(_ context.Context, method string, params json.RawMessage) {
+	if method != codexapp.MethodAccountLoginCompleted {
+		return
+	}
+	var n codexapp.AccountLoginCompletedNotification
+	if json.Unmarshal(params, &n) != nil {
+		return
+	}
+	result := SignInResult{Success: n.Success}
+	if n.Error != nil {
+		result.Error = *n.Error
+	}
+	h.a.signInMu.Lock()
+	loginID := h.s.loginID
+	if loginID == "" {
+		// login/start has not answered yet; it picks this up when it does.
+		h.s.early = &result
+		h.a.signInMu.Unlock()
+		return
+	}
+	h.a.signInMu.Unlock()
+	if n.LoginID != nil && *n.LoginID != loginID {
+		return
+	}
+	result.LoginID = loginID
+	// Off the read loop: finishing stops this connection.
+	go h.a.finishSignIn(h.s, result, false)
+}
+
+func (h *codexSignInHandler) Request(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, *codexapp.RPCError) {
+	return nil, codexUnsupportedRequest(method)
+}
+
+// SignInStatus reads the sign-in a session would use. An API key, when one is
+// set, is what sessions sign in with, so it is the answer; otherwise a fresh
+// process reads the ChatGPT sign-in in the home.
+func (a *CodexAdapter) SignInStatus(ctx context.Context) (SignInStatus, error) {
+	if key, err := a.apiKey(ctx); err == nil && key != "" {
+		return SignInStatus{SignedIn: true, Method: codexapp.AccountAPIKey}, nil
+	}
+	var res codexapp.GetAccountResponse
+	err := a.oneShot(ctx, func(ctx context.Context, p *codexProcess) error {
+		if err := p.conn.Call(ctx, codexapp.MethodAccountRead, map[string]any{"refreshToken": false}, &res); err != nil {
+			return p.failure("account/read", err)
+		}
+		return nil
+	})
+	if err != nil {
+		return SignInStatus{}, err
+	}
+	if res.Account == nil {
+		return SignInStatus{SignedIn: !res.RequiresOpenAIAuth}, nil
+	}
+	status := SignInStatus{SignedIn: true, Method: res.Account.Type}
+	if res.Account.Email != nil {
+		status.Email = *res.Account.Email
+	}
+	if res.Account.PlanType != nil {
+		status.Plan = *res.Account.PlanType
+	}
+	return status, nil
+}
+
+// SignOut removes the ChatGPT sign-in from the home. An API key is not
+// Codex's to forget: it is unlinked where it was chosen, in Settings.
+func (a *CodexAdapter) SignOut(ctx context.Context) error {
+	return a.oneShot(ctx, func(ctx context.Context, p *codexProcess) error {
+		if err := p.conn.Call(ctx, codexapp.MethodAccountLogout, nil, nil); err != nil {
+			return p.failure("account/logout", err)
+		}
+		return nil
+	})
+}
+
+// oneShot runs call against a fresh app-server under the jail, then stops it.
+func (a *CodexAdapter) oneShot(ctx context.Context, call func(context.Context, *codexProcess) error) error {
+	l, err := a.resolveCommand()
+	if err != nil {
+		return err
+	}
+	work, err := codexScratchDir("devkit-codex-account-")
+	if err != nil {
+		return err
+	}
+	defer os.RemoveAll(work)
+	p, err := a.start(work, l, OpenSpec{}, codexStoreFile, codexNoHandler{})
+	if err != nil {
+		return err
+	}
+	defer p.stop()
+	callCtx, cancel := p.bounded(ctx, handshakeTimeout)
+	defer cancel()
+	if err := a.initialize(callCtx, p); err != nil {
+		return err
+	}
+	return call(callCtx, p)
+}
+
+// codexScratchDir is an empty working directory for a process that has no
+// project, symlinks resolved so the jail binds what Seatbelt will match.
+func codexScratchDir(prefix string) (string, error) {
+	dir, err := os.MkdirTemp("", prefix)
+	if err != nil {
+		return "", fmt.Errorf("%w: working directory: %v", ErrProcessFailed, err)
+	}
+	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
+		dir = resolved
+	}
+	return dir, nil
+}
```

- [ ] **Step 4: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ -run 'Codex|SignIn|ServerMode' -count=1 -race`
Expected: PASS. `gofmt -l internal` không in gì.

- [ ] **Step 5: Commit**

```bash
git add internal/agenthub/codex_auth.go internal/agenthub/adapter_codex.go internal/agenthub/adapter_servermode.go internal/agenthub/adapter_codex_test.go
git commit -m "feat(agenthub): sign in to Codex inside DevKit

Browser by default, device code on request, both Codex's own login in
a short-lived app-server under the jail; the sign-in lands in DevKit's
Codex home. Status and sign-out ask a fresh process. Server mode
refuses all of it.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 7: Nối `main.go`

**Model:** haiku

**Vì sao:** Đăng ký `CodexAdapter` cạnh Claude (server mode vẫn thay bằng stub qua `ForServerMode`), `Verifiers[CodexCLI] = Probe`, `DEVKIT_CODEX_COMMAND` như `DEVKIT_CLAUDE_ACP_COMMAND`, event `agenthub:signInCompleted`. Home: `<data>/agent-homes/codex`.

**Files:** Modify `main.go`

- [ ] **Step 1: Áp patch hiện thực** (`extract t7-impl`)

<!-- patch t7-impl -->
```diff
diff --git a/main.go b/main.go
index b9085b9fac3c24b76bdf7816f29cfbc508c81339..53e47fffbdeb0bdc397c28cedcff24bc6912220f 100644
--- a/main.go
+++ b/main.go
@@ -322,10 +322,36 @@ func main() {
 		}
 	}
 	claudeAdapter := agenthub.NewClaudeAdapter(claudeOpts)
+	codexOpts := agenthub.CodexOptions{
+		// DevKit's own Codex home: the user's ~/.codex, its sign-in and its
+		// rules, is never the session's (Codex spec §6).
+		Home: filepath.Join(dataDir, "agent-homes", "codex"),
+		Runtime: func() (agentruntime.Resolved, error) {
+			return runtimes.Resolve(agentruntime.CodexCLI)
+		},
+		OnSignInCompleted: func(r agenthub.SignInResult) {
+			emitAppEvent("agenthub:signInCompleted", map[string]any{
+				"agent":   string(r.Agent),
+				"loginId": r.LoginID,
+				"success": r.Success,
+				"error":   r.Error,
+			})
+		},
+	}
+	// Development and CI only, like DEVKIT_CLAUDE_ACP_COMMAND.
+	if override := os.Getenv("DEVKIT_CODEX_COMMAND"); override != "" {
+		if filepath.IsAbs(override) {
+			codexOpts.Command = override
+		} else {
+			log.Printf("agenthub: DEVKIT_CODEX_COMMAND must be absolute, ignoring %q", override)
+		}
+	}
+	codexAdapter := agenthub.NewCodexAdapter(codexOpts)
 	runtimes.Verifiers = map[string]func(context.Context, agentruntime.Resolved) error{
 		agentruntime.ClaudeACP: claudeAdapter.Probe,
+		agentruntime.CodexCLI:  codexAdapter.Probe,
 	}
-	vendors := []agenthub.VendorAdapter{claudeAdapter}
+	vendors := []agenthub.VendorAdapter{claudeAdapter, codexAdapter}
 	if application.System.IsServer() {
 		// Server mode serves the bridge over unauthenticated loopback HTTP and
 		// the jail shares the host loopback: a session could approve its own
```

- [ ] **Step 2: Chạy, xác nhận qua**

Run: `go build -tags server -o /dev/null . && go vet -tags server . && go test ./internal/bridge/ ./internal/agenthub/ ./internal/arch/ -count=1`
Expected: sạch / PASS. `gofmt -l main.go` không in gì.

- [ ] **Step 3: Commit**

```bash
git add main.go
git commit -m "feat: register the Codex adapter and its install verifier

Next to Claude, stubbed in server mode like every vendor. Installs of
@openai/codex are now verified by an initialize in the jail.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 8: Test với codex thật: trong jail (không tốn quota) và live (bật bằng env)

**Model:** haiku

**Vì sao:** `TestCodexRealBinaryInTheJail` chạy binary thật trong jail thật với home mới: Probe qua, `SignInStatus` = chưa đăng nhập, `Open` → `ErrAgentAuthRequired` (không phải process chết — kể cả với một server MCP trên dòng lệnh, vì codex thoát trước initialize nếu `-c` sai), Codex ghi vào home của DevKit và không đụng file chính sách. `TestCodexLiveTurnWithAnApproval` — một lượt thật + một approval thật; tốn quota, cần home nháp đã đăng nhập, chỉ chạy khi người dùng đồng ý.

**Files:** Create `internal/agenthub/adapter_codex_live_test.go`

- [ ] **Step 1: Áp patch test** (`extract t8-test`)

<!-- patch t8-test -->
```diff
diff --git a/internal/agenthub/adapter_codex_live_test.go b/internal/agenthub/adapter_codex_live_test.go
new file mode 100644
index 0000000000000000000000000000000000000000..6881cdba30daf265c429045c39d182a37e5a67f8
--- /dev/null
+++ b/internal/agenthub/adapter_codex_live_test.go
@@ -0,0 +1,118 @@
+package agenthub
+
+import (
+	"context"
+	"encoding/json"
+	"errors"
+	"os"
+	"path/filepath"
+	"strings"
+	"testing"
+	"time"
+
+	"devkit/internal/acp"
+	"devkit/internal/agentruntime"
+	"devkit/internal/sandbox"
+)
+
+// codexForReal is the real codex binary under the real jail, or a skip.
+// DEVKIT_CODEX_COMMAND names the binary (the native one, not the npm
+// launcher): the managed install's vendor/<triple>/bin/codex.
+func codexForReal(t *testing.T, home string) *CodexAdapter {
+	t.Helper()
+	bin := os.Getenv("DEVKIT_CODEX_COMMAND")
+	if bin == "" {
+		t.Skip("set DEVKIT_CODEX_COMMAND to a codex 0.147.0 binary to run codex in the real jail")
+	}
+	if err := sandbox.DefaultRunner().Available(context.Background()); err != nil {
+		t.Skipf("sandbox unavailable: %v", err)
+	}
+	return NewCodexAdapter(CodexOptions{Command: bin, Home: home, Version: "test"})
+}
+
+// The real binary in the real jail, signed out: the install verifies, the
+// status reads, and a session is refused as a sign-in to offer — not as a
+// crash. Uses no quota and no network beyond what codex does on start.
+func TestCodexRealBinaryInTheJail(t *testing.T) {
+	home := filepath.Join(t.TempDir(), "agent-homes", "codex")
+	a := codexForReal(t, home)
+	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
+	defer cancel()
+
+	l, err := a.resolveCommand()
+	if err != nil {
+		t.Fatal(err)
+	}
+	if err := a.Probe(ctx, agentruntime.Resolved{Command: l.path, ReadOnlyPaths: l.ro}); err != nil {
+		t.Fatalf("Probe: %v", err)
+	}
+	status, err := a.SignInStatus(ctx)
+	if err != nil || status.SignedIn {
+		t.Fatalf("status of a fresh home = %+v, %v", status, err)
+	}
+	// With an MCP server on the command line: codex parses every -c before
+	// it answers initialize, so a value it could not read would fail here as
+	// a dead process, not as a sign-in.
+	spec := OpenSpec{Cwd: t.TempDir(), McpServers: []acp.McpServerStdio{{
+		Name: "devkit", Command: "/bin/cat", Args: []string{"--socket", "/tmp/a b.sock"}, Env: []acp.EnvVariable{{Name: "X", Value: `q"uote`}},
+	}}}
+	if _, err := a.Open(ctx, spec); !errors.Is(err, ErrAgentAuthRequired) {
+		t.Fatalf("Open signed out = %v, want ErrAgentAuthRequired", err)
+	}
+	// Codex wrote its state into the home, and not into the policy files.
+	if _, err := os.Stat(filepath.Join(home, "installation_id")); err != nil {
+		t.Errorf("codex did not use DevKit's home: %v", err)
+	}
+	if got, _ := os.ReadFile(filepath.Join(home, "config.toml")); string(got) != codexConfigHeader {
+		t.Errorf("config.toml = %q", got)
+	}
+	if entries, _ := os.ReadDir(filepath.Join(home, "skills")); len(entries) != 0 {
+		t.Errorf("skills/ was written: %v", entries)
+	}
+}
+
+// One real turn with one real approval (spec §9, Giai đoạn 4). Needs a
+// ChatGPT sign-in in a scratch CODEX_HOME — never ~/.codex, never a copy of
+// its auth.json — and spends quota, so it runs only when asked:
+//
+//	DEVKIT_CODEX_LIVE=1 DEVKIT_CODEX_LIVE_HOME=<signed-in scratch home> \
+//	DEVKIT_CODEX_COMMAND=<codex binary> go test ./internal/agenthub/ -run CodexLive -v
+func TestCodexLiveTurnWithAnApproval(t *testing.T) {
+	if os.Getenv("DEVKIT_CODEX_LIVE") != "1" {
+		t.Skip("set DEVKIT_CODEX_LIVE=1 (spends Codex quota)")
+	}
+	home := os.Getenv("DEVKIT_CODEX_LIVE_HOME")
+	if home == "" {
+		t.Fatal("DEVKIT_CODEX_LIVE_HOME must name a signed-in scratch CODEX_HOME")
+	}
+	a := codexForReal(t, home)
+	project := t.TempDir()
+	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
+	defer cancel()
+
+	rt, err := a.Open(ctx, OpenSpec{Cwd: project})
+	if err != nil {
+		t.Fatalf("Open: %v", err)
+	}
+	defer rt.Close(context.Background())
+	var cards []json.RawMessage
+	var transcript updateLog
+	rt.SetHandlers(RuntimeHandlers{
+		OnUpdate: transcript.add,
+		OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
+			cards = append(cards, params)
+			return selected(codexOptionAllow), nil
+		},
+	})
+	out, err := rt.Prompt(ctx, TextPrompt(`Create a file named done.txt in the current directory containing exactly "ok". Do nothing else.`), nil)
+	if err != nil {
+		t.Fatalf("Prompt: %v\n%s", err, transcript.joined())
+	}
+	t.Logf("stop %s, %d approval(s)\n%s", out.StopReason, len(cards), transcript.joined())
+	if len(cards) == 0 {
+		t.Fatal("Ask must ask before writing a file")
+	}
+	if got, _ := os.ReadFile(filepath.Join(project, "done.txt")); strings.TrimSpace(string(got)) != "ok" {
+		t.Fatalf("done.txt = %q", got)
+	}
+}
```

- [ ] **Step 2: Chạy, xác nhận qua**

Run: `go test ./internal/agenthub/ -run 'CodexRealBinary|CodexLive' -count=1 -v`
Expected: cả hai SKIP khi thiếu env. Controller (không phải subagent) chạy thêm với `DEVKIT_CODEX_COMMAND=<binary codex 0.147.0>` → `TestCodexRealBinaryInTheJail` PASS. **Không** đặt `DEVKIT_CODEX_LIVE`.

- [ ] **Step 3: Commit**

```bash
git add internal/agenthub/adapter_codex_live_test.go
git commit -m "test(agenthub): codex in the real jail, and a live turn behind an env

Signed out, the real binary verifies, reads its status and is refused
as a sign-in, writing only to DevKit's home. The live turn spends quota
and runs only with DEVKIT_CODEX_LIVE=1 and a scratch signed-in home.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

### Task 9: Tài liệu: spec, research, AGENTS.md

**Model:** haiku

**Vì sao:** Ghi các số đo và quyết định của kế hoạch này vào spec (§4.2, §4.3, §6.4, §7.2), research (mục 5 của Phase 0 results) và dòng Agent Hub trong AGENTS.md (vốn nói Codex chưa có).

**Files:** Modify `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md`, `docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md`, `AGENTS.md`

- [ ] **Step 1: Áp patch hiện thực** (`extract t9-impl`)

<!-- patch t9-impl -->
```diff
diff --git a/AGENTS.md b/AGENTS.md
index 2210d46a5aa5a592e57862bdfdf4d4a95095b57b..e52d52fdb13ba3a7562b29c076c427da6bec981d 100644
--- a/AGENTS.md
+++ b/AGENTS.md
@@ -92,7 +92,11 @@ as production-verified against a real Claude Code session in cloud CI.
 **Partial** v1 surface on this branch: ACP codec/conn, mock + Claude adapters,
 session service (SQLite migration 13), bridge/UI multi-tab, MCP `agenthub.*`.
 Claude vendor spawn uses `sandbox.DefaultRunner().StartPiped` (fail-closed).
-Codex/Gemini adapters are **not** shipped. Product design + status:
+A Codex adapter (`adapter_codex*.go`, `codex_*.go`) drives `codex app-server` 0.147.0
+under the same jail with its own `CODEX_HOME` (`<data>/agent-homes/codex`); it is
+registered in `main.go`, but the UI for it (sign-in panel, vendor picker) is not built
+yet — design in `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md`, state in
+`docs/superpowers/HANDOFF-codex.md`. Gemini is **not** shipped. Product design + status:
 `syurodev/base` → `/dev-kit/agent-hub`, `/dev-kit/implementation-status`.
 
 **Not in server mode**: `main.go` registers `agenthub.ServerModeAdapter` stubs there, and
@@ -101,6 +105,9 @@ and server mode's bridge has no authentication, so a session could approve itsel
 
 ```bash
 go test ./internal/acp/ ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ -count=1
+# Codex in the real jail, signed out (no quota): DEVKIT_CODEX_COMMAND is the native
+# binary, e.g. <data>/runtimes/codex/0.147.0/node_modules/@openai/codex-<platform>/vendor/<triple>/bin/codex
+DEVKIT_CODEX_COMMAND=/abs/path/to/codex go test ./internal/agenthub/ -run CodexRealBinary -v
 ```
 
 ### Agent sandbox (`internal/sandbox`)
diff --git a/docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md b/docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md
index ab05a4d06cec3493071cfb94636044c4235ff7f3..a84f1c92f872682fcabb80f63a5a0191cb781a43 100644
--- a/docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md
+++ b/docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md
@@ -186,6 +186,23 @@ Prompt: `Append the line "line three" to notes.txt, then run: cat notes.txt`
 tool call still open when a turn ends `interrupted`; use `serverRequest/resolved` to retire approval cards.
 §5.3 — never log `remoteControl/status/changed` verbatim (hostname, installation id).
 
+### 5. Đo thêm cho Kế hoạch 2c (0.147.0, không đăng nhập, không tốn quota)
+Bản cài qua chính `internal/agentruntime` (npm hoist `@openai/codex-darwin-arm64`, glob `NativeEntry` thứ nhất
+khớp); `codex app-server` chạy thẳng binary, `CODEX_HOME` nháp.
+- **`PATH`/`rg`:** `command/exec` in ra `PATH` bắt đầu bằng `<home>/tmp/arg0/…` rồi `vendor/<triple>/codex-path`,
+  `command -v rg` ra `rg` đi kèm — giống hệt khi có hay không `CODEX_MANAGED_BY_NPM`/`CODEX_MANAGED_PACKAGE_ROOT`.
+  Binary tự tìm `codex-package.json` cạnh nó.
+- **Ghi gì vào home lúc khởi động:** `installation_id`, `*.sqlite`, `shell_snapshots/`, `tmp/arg0/`,
+  `sessions/…` (khi có thread), và `skills/.system/…` (skill hệ thống). **Không** ghi `config.toml`.
+- **Home có năm file chính sách chỉ-đọc (Seatbelt):** initialize, account/read, command/exec, thread/start đều
+  chạy; stderr có `failed to install system skills: … Operation not permitted` mỗi lần khởi động, không gì khác.
+- **TLS trong Seatbelt:** kết nối websocket tới `api.openai.com` báo `invalid peer certificate: UnknownIssuer`
+  trong jail, `401 Unauthorized` (tức TLS qua) ngoài jail. Thêm đúng `mach-lookup com.apple.SecurityServer`
+  → `401`. `trustd` một mình không đủ.
+- **Cấu hình MCP qua `-c`:** giá trị TOML đúng được nhận; sai kiểu (`args="notalist"`) → tiến trình thoát
+  `exit 1` trước khi trả lời initialize.
+- **Duyệt tool MCP** (đọc mã nguồn tag `rust-v0.147.0`): xem spec §7.2.
+
 ---
 
 ## Found while checking "Full auto must stop at prod" (2026-09-24)
diff --git a/docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md b/docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md
index d4f8cc0edac9d5fc7b87869c607d377bab657932..931621defc9a05bb71cfa5667ad6b33854e9d813 100644
--- a/docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md
+++ b/docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md
@@ -88,12 +88,13 @@ request (server→client, có `id`, client phải trả lời đúng một lần
 - Agent mới `codex`: `Package "@openai/codex"`, `TestedVersion "0.147.0"`, glob entry theo `<triple>`.
 - Claude không đổi hành vi. Dùng chung màn hình cài / cập nhật / rollback.
 - `DEVKIT_CODEX_COMMAND` (tuyệt đối) ghi đè runtime, giống `DEVKIT_CLAUDE_ACP_COMMAND`.
-- **Cần quyết ở Kế hoạch 2c (runtime; review Kế hoạch 2a):** launcher `bin/codex.js` của 0.147.0 đặt
-  `CODEX_MANAGED_BY_NPM=1` và `CODEX_MANAGED_PACKAGE_ROOT=<realpath node_modules/@openai/codex>` trước khi
-  chạy binary; binary đọc hai biến này (và `codex-package.json`, thư mục `codex-path` chứa `rg` đi kèm).
-  Chạy thẳng binary thì mất cả hai → có thể đổi hành vi tự cập nhật / nhận diện cách cài, và `rg` đi kèm
-  có thể không được tìm thấy. 2c phải đo rồi chọn: đặt hai biến + thêm `codex-path` vào `PATH` trong
-  `resolveAt`, hay để trống có chủ đích (DevKit tự quản lý phiên bản, không muốn Codex tự cập nhật).
+- **Đã quyết ở Kế hoạch 2c (đo trên 0.147.0): để trống có chủ đích.** Launcher `bin/codex.js` đặt
+  `CODEX_MANAGED_BY_NPM=1` và `CODEX_MANAGED_PACKAGE_ROOT`, nhưng chạy thẳng binary thì binary **tự** đọc
+  `codex-package.json` cạnh nó và đưa `codex-path` (chứa `rg`) lên đầu `PATH` của tiến trình con — đo bằng
+  `command/exec`: `PATH` và `command -v rg` giống hệt có hay không có hai biến. Hai biến chỉ báo Codex cách
+  tự cập nhật; DevKit pin phiên bản và tắt `check_for_update_on_startup`. Jail phải đọc được cả thư mục
+  `vendor/<triple>` (không chỉ file binary) — `runtimeLaunch` đã cấp thư mục version, `DEVKIT_CODEX_COMMAND`
+  cấp thêm thư mục đó (`codexPackageDir`).
 - Bản cài Codex hiện được kích hoạt với `verified.handshake=false` (chưa có verifier) và đã gọi được qua
   bridge/MCP `AgentRuntimeInstall("codex")`. 2c thêm verifier (`initialize` app-server trong jail).
 
@@ -124,9 +125,25 @@ Hiện thực: `VendorAdapter`, `SessionLoader` (`thread/resume`), `RuntimeConfi
   - `AGENTS.md`, `AGENTS.override.md` — chỉ dẫn toàn cục: ghi được là tiêm prompt bền vững vào mọi session sau.
   - `skills/` — skill toàn cục, cùng lý do.
   Phần còn lại của home (auth, sessions, sqlite, log) vẫn ghi được.
+- DevKit **reset** các file đó trước mỗi spawn (`prepareCodexHome`): `config.toml` chỉ còn một dòng chú thích,
+  `rules/` và `skills/` rỗng, `AGENTS*.md` rỗng; symlink hay sai loại bị xoá chứ không đi theo. Chỉ-đọc chưa đủ:
+  thứ một session cũ (hoặc một symlink cài sẵn) để lại vẫn được lần spawn sau đọc.
+- Đo (0.147.0): Codex chạy bình thường khi cả năm chỉ-đọc. Mỗi lần khởi động nó cố chép skill hệ thống vào
+  `skills/.system` và ghi log `failed to install system skills` — vô hại, chỉ là không có các skill đi kèm
+  (imagegen, openai-docs, skill-creator…). Codex không ghi `config.toml` lúc khởi động.
 - Policy jail: như Claude (`NetworkHost`, `ShareHome`/Docker theo Settings), thêm
-  `ReadWritePaths += CODEX_HOME`, `DenyReadPaths += ~/.codex`, keep-list `ProtectDataDir` thêm
-  `agent-homes/codex`.
+  `ReadWritePaths += CODEX_HOME`, `DenyReadPaths += ~/.codex`. **Không** thêm `agent-homes/codex` vào keep-list
+  chung của `ProtectDataDir` (Kế hoạch 2c): keep-list dùng cho mọi vendor, nên session Claude dưới shared home sẽ
+  đọc được `auth.json` của Codex. Thay vào đó adapter Codex tự "khoét" home của mình ra khỏi danh sách ẩn của
+  **chính session Codex** (`carveOut`): mọi thứ cạnh đường tới home vẫn ẩn.
+- **macOS: Seatbelt phải cho `mach-lookup com.apple.SecurityServer` khi `NetworkHost`** (Kế hoạch 2c, đo):
+  Codex là Rust và kiểm chứng chỉ TLS bằng root của hệ thống; thiếu nó mọi kết nối HTTPS từ jail báo
+  `invalid peer certificate: UnknownIssuer`. Chỉ lookup đó không mở được login keychain (cần thêm quyền đọc
+  file mà `AllowKeychain` cấp). Claude không gặp vì Node mang bộ CA riêng.
+- Deny-write chịu được việc đổi tên thư mục cha (review Kế hoạch 2b): bwrap biến mọi thư mục phía trên thành
+  mount point (bind lên chính nó, hoặc tmpfs rỗng nếu bwrap tự tạo), Seatbelt chặn rename tổ tiên theo cả hai
+  cách viết path. `Policy.Validate` từ chối path deny-write không tồn tại, là symlink, chưa `Clean`, hoặc có
+  grant ghi nằm bên trong.
 - Handshake: `initialize {clientInfo:{name:"devkit", title:"DevKit", version}, capabilities:{experimentalApi:false}}`
   → `initialized` → `account/read` (§6.3) → `thread/start {cwd, approvalPolicy, approvalsReviewer:"user"}`.
 - `thread/start` **không** gửi `sandbox`: field đó là `SandboxMode` (`read-only`/`workspace-write`/
@@ -227,7 +244,12 @@ OpenAI. Đăng nhập trình duyệt thì chạy được ngay từ trong bwrap.
 ### 6.4 Interface và bridge
 
 - `agenthub.InAppSignIn` (tuỳ chọn): `SignInStatus`, `StartDeviceSignIn`, `StartBrowserSignIn`,
-  `CancelSignIn`, `SignOut`. `AuthProvider` (terminal) giữ nguyên cho Claude.
+  `CancelSignIn`, `SignOut`. `AuthProvider` (terminal) giữ nguyên cho Claude. Đã có ở Kế hoạch 2c, cùng
+  `Service.SignInStatus` / `StartSignIn(agent, "browser"|"deviceCode")` / `CancelSignIn` / `SignOut`
+  (`ErrUnsupported` cho vendor không có) và stub server mode. `main.go` phát event `agenthub:signInCompleted`
+  `{agent, loginId, success, error}` qua `CodexOptions.OnSignInCompleted`.
+- API key: `CodexOptions.APIKey` (hàm trả key hoặc ""); có key thì `SignInStatus` báo `apiKey` và session
+  dùng store `ephemeral`. Nối nó với accounts + preferences là việc của Kế hoạch 3.
 - Bridge: `AgentHubSignInStatus`, `AgentHubStartDeviceSignIn`, `AgentHubStartBrowserSignIn`,
   `AgentHubCancelSignIn`, `AgentHubSignOut`; event `agenthub:signInCompleted`.
 - Capability `agenthub:execute` trên `auth:codex`, **chỉ UI**. Không có tool MCP đăng nhập.
@@ -273,7 +295,16 @@ Qua `OnPermission` → `PermissionWaiter`, cùng timeout với Claude.
 - Xin thêm quyền (network, thư mục ghi): dưới `externalSandbox` vô nghĩa — jail mới quyết. Adapter tự
   trả **grant rỗng** và ghi một dòng transcript: *"Codex xin quyền X. Quyền truy cập do sandbox của
   DevKit quyết định — Settings → Agents → Sandbox."*
-- Server request khác (`item/tool/requestUserInput`, `mcpServer/elicitation/request`,
+- **Duyệt tool MCP** (Kế hoạch 2c, đọc mã nguồn 0.147.0 `core/src/mcp_tool_call.rs`; người dùng chọn hiện thẻ):
+  ở Ask/Auto, Codex hỏi trước mỗi lần gọi một tool MCP không tự khai chỉ-đọc/không phá huỷ — tức phần lớn tool
+  của DevKit — qua `mcpServer/elicitation/request` có `_meta.codex_approval_kind = "mcp_tool_call"`. Thẻ:
+  Allow (`{action:"accept"}`), Allow cho session (chỉ khi `_meta.persist` có `"session"`,
+  `{action:"accept", _meta:{persist:"session"}}`), Deny (`{action:"decline"}`); huỷ/hết giờ → `cancel`.
+  **Không bao giờ** đưa "Allow and don't ask me again" (`persist:"always"` ghi luật vĩnh viễn vào `config.toml`).
+  Request không mang item id: thẻ gắn vào lệnh gọi MCP đang mở duy nhất của server đó (Codex bắt đầu item trước
+  khi hỏi), không thì đứng riêng. Full auto không hỏi: `never` + externalSandbox → Codex tự duyệt, cổng prod của
+  DevKit vẫn hỏi bên trong tool (§7.4).
+- Server request khác (`item/tool/requestUserInput`, elicitation của chính một server MCP,
   `account/chatgptAuthTokens/refresh`, `attestation/generate`, v1 `applyPatchApproval`/`execCommandApproval`,
   và mọi method lạ) → **từ chối ngay** để lượt không treo. Mỗi loại một dòng dispatch (§5.4).
 
```

- [ ] **Step 2: Chạy, xác nhận qua**

Run: `git diff --stat`
Expected: đúng ba file trên.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md AGENTS.md
git commit -m "docs: codex plan 2c measurements and decisions

PATH and rg without the npm launcher's variables, read-only skills,
TLS under Seatbelt, the per-vendor carve-out, MCP tool approvals, and
the in-app sign-in API.

Co-Authored-By: <model> <noreply@anthropic.com>"
```

## Kết thúc

- [ ] `go test ./internal/sandbox/ ./internal/codexapp/ ./internal/agentruntime/ ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ ./internal/arch/ -count=1 -race`, `go vet -tags server ./...`, `gofmt -l internal main.go`, `go build -tags server -o /dev/null .` — sạch (bỏ qua `ld: warning` trên macOS).
- [ ] Linux: cùng bộ test `sandbox`/`codexapp`/`agenthub`/`arch` trong container có bwrap — xanh.
- [ ] `DEVKIT_CODEX_COMMAND=<codex 0.147.0> go test ./internal/agenthub/ -run CodexRealBinary -v` — PASS.
- [ ] Review tổng (opus) cho khoảng commit của kế hoạch này; sửa những gì review tìm ra.
- [ ] Cập nhật `docs/superpowers/HANDOFF-codex.md`.
- [ ] Hỏi người dùng trước live test (`DEVKIT_CODEX_LIVE=1`, tốn quota, cần đăng nhập vào home nháp).
