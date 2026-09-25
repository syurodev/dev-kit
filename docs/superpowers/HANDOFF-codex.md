# Bàn giao: Codex trong Agent Hub

Cập nhật: 2026-09-25 · Viết cho: session tiếp theo — session đó **không** có hội thoại hay bộ nhớ của các
session trước, nên mọi thứ cần biết nằm ở đây và trong các tài liệu được trỏ tới.

## Đọc theo thứ tự

1. File này.
2. Spec: `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md` — thiết kế đã được người dùng duyệt,
   cập nhật theo số đo thật (Giai đoạn 0, Kế hoạch 2c) và theo review của từng kế hoạch.
3. Research: `docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md` — vì sao Codex có `CODEX_HOME`
   riêng, cách đăng nhập, **kết quả đo** (mục "Phase 0 results", gồm mục 5 đo cho 2c).
4. Các kế hoạch đã chạy (để biết cách làm, không cần chạy lại):
   `docs/superpowers/plans/2026-09-24-codex-plan-1-measure-and-security.md`, `…-plan-2a-wire-and-runtime.md`,
   `…-plan-2b-translate-and-approvals.md`, `…-plan-2c-runtime-adapter.md`.

## Trạng thái

Nhánh **`codex/plan-2c-adapter`** (tách từ `main` a6a2d5ed), **chưa push**. Mọi thứ dưới đây nằm trên nhánh đó.

| Phần | Trạng thái | Nằm ở đâu |
|---|---|---|
| Kế hoạch 1 — đo Codex thật + vá bảo mật | Xong, đã review | `internal/mcp/gate.go`, `internal/agenthub/adapter_servermode.go`, `main.go`, capture `internal/agenthub/testdata/codex_session_capture.jsonl` |
| Kế hoạch 2a — kết nối app-server + runtime native | Xong, đã review | `internal/codexapp/{wire,conn,protocol}.go`, `internal/agentruntime` (`NativeEntry`, `Verifiers`, agent `codex` 0.147.0). Commit gốc a6a2d5ed đặt nhầm 5 file `agentruntime` vào `internal/agenthub/` và thiếu phần nối `main.go`/`arch` — đã sửa ở e3425c38 |
| Kế hoạch 2b — deny-write, bộ dịch, approval | Xong, **đã review tổng + sửa** (f4c625ce, 50455496, 1aecfbc6) | `internal/sandbox`, `internal/codexapp/items.go`, `internal/agenthub/codex_translate.go`, `codex_approvals.go` |
| Kế hoạch 2c — runtime `CodexAdapter` | Xong 9/9 task, **đã review tổng + sửa** (497f9099, dd744616), **đã live test + sửa** (7a44da1d, 5051ba3a) | `internal/agenthub/adapter_codex.go`, `codex_auth.go`, `codex_home.go`, `protect.go` (`carveOut`), `codex_approvals.go` (`mcpToolApprovalFor`), `internal/sandbox/seatbelt_profile.go`, `internal/sandbox/proc.go`, `main.go` |
| Kế hoạch 3 — bridge, frontend, tài liệu | **Chưa viết** | xem "Việc tiếp theo" |

Codex **đã đăng ký** trong `main.go` (adapter, verifier cài đặt, `DEVKIT_CODEX_COMMAND`, event
`agenthub:signInCompleted`), nhưng **chưa có UI**: không có panel đăng nhập, bridge chưa có method sign-in,
nhiều chỗ frontend còn hard-code `"claude"`. `CodexOptions.APIKey` chưa nối với accounts.

Kiểm cuối (2026-09-25): trên macOS `go test ./internal/{sandbox,codexapp,agentruntime,agenthub,bridge,mcp,arch}/
-race` xanh, `go vet -tags server ./...`, `gofmt`, `go build -tags server` sạch; `TestCodexRealBinaryInTheJail` qua
với codex 0.147.0 thật, không để lại zombie. Linux (bwrap trong container) xanh trên 5051ba3a.

**Live (2026-09-25, người dùng đồng ý, tài khoản ChatGPT cá nhân, home nháp — đã đăng xuất và xoá):**
đăng nhập trình duyệt qua chính `StartBrowserSignIn` từ jail Seatbelt; `TestCodexLiveTurnWithAnApproval` và
`TestCodexLiveMcpApprovalAndResume` đều qua (một lượt Ask + approval sửa file; thẻ duyệt tool MCP khớp dây thật;
usage sau resume tính riêng lượt). Live tìm ra hai lỗi đăng nhập trên macOS, đã sửa ở 7a44da1d: Seatbelt chặn
bind cổng callback 1455 (`Policy.ListenLoopbackPorts`), và bước đổi token cần `com.apple.trustd.agent`.
Chi tiết: research doc mục 6.

## Quyết định của người dùng (giữ nguyên)

- **Phạm vi A** (cốt lõi): session, prompt, stream, tool call, duyệt quyền, Stop, sandbox, resume, chọn model,
  ba chế độ. Checkpoint/rewind, steer, goal, elicitation, plan streaming để sau — nhưng **chừa đường**:
  bộ dịch và bộ ánh xạ là bảng dispatch (spec §5.4).
- **Binary**: DevKit tự cài `@openai/codex` **pin 0.147.0** qua `internal/agentruntime`; chạy thẳng binary native.
  Không đặt `CODEX_MANAGED_*` (đo: binary tự tìm `rg` đi kèm; spec §4.2).
- **Xác thực**: `CODEX_HOME` riêng của DevKit (`<data>/agent-homes/codex`), `~/.codex` thật bị ẩn khỏi jail.
  Đăng nhập **trình duyệt mặc định** (device code tắt sẵn theo tài khoản — đo được), device code tuỳ chọn,
  API key từ module accounts với store `ephemeral`. Không copy `auth.json`.
- **Chế độ**: Ask = `untrusted` (mặc định), Auto = `on-request`, Full auto = `never` (id ACP `bypassPermissions`
  để hộp xác nhận bypass sẵn có tự bật). Full auto **vẫn phải bị cổng env prod của DevKit chặn** — cổng nằm
  trong tool MCP phía DevKit, đã kiểm. Resume luôn bắt đầu ở Ask.
- **Duyệt tool MCP** (2026-09-24, Kế hoạch 2c): ở Ask/Auto Codex hỏi trước mỗi lần gọi tool DevKit qua
  `mcpServer/elicitation/request` → **hiện thẻ** Allow / Allow cho session / Deny; không bao giờ "always".
- **Không thu hẹp** `ShareHome`/Docker khi Full auto (Docker là cần thiết); thay bằng cảnh báo trong hộp xác nhận.
- **Server mode**: hiện fail-fast mọi lời nhắc (và mọi sign-in); **về sau hỏi quyền qua notification của hệ điều
  hành** (spec §8.1).
- Claude sẽ chuyển sang cùng cách đăng nhập (`CLAUDE_CONFIG_DIR`) — follow-up, các bước ở cuối research doc.

## Cách làm đã chạy tốt

- Người dùng muốn **subagent-driven**, việc nhẹ giao model nhỏ: haiku cho task cơ học, sonnet cho task nhiều
  file/review, opus cho review tổng cuối mỗi kế hoạch.
- **Viết plan bằng code đã chạy**: viết và test toàn bộ code trong một `git worktree` tạm (nhớ copy
  `frontend/dist` vào — `main.go` embed nó, `internal/arch` sẽ fail nếu thiếu), commit mỗi task một commit nháp,
  rồi sinh plan bằng script: mỗi task nhúng `git diff --full-index` tách **patch test** (`*_test.go`) và **patch
  hiện thực**, trước mỗi khối diff có marker `<!-- patch tN-test|impl -->`. Subagent trích bằng `awk` (hàm
  `extract` trong plan 2c) — không gõ lại, nên haiku làm được cả patch 2000 dòng.
- Kiểm plan: trích mọi patch **từ chính file plan**, áp lên worktree sạch tách từ commit gốc, so `git write-tree`
  với cây nháp (phải trùng hash).
- Kiểm task của subagent bằng **so cây**: `git diff --stat <commit nháp N> <commit task N>` chỉ được khác đúng
  file plan.
- Sau mỗi kế hoạch: review tổng (opus) → sửa (kèm test chứng minh; thử phá từng sửa xem test có bắt không) → cập
  nhật file này.
- **Test Linux trên máy macOS**: container `golang:1.25-bookworm` (đã kéo) + `apt install bubblewrap`, chạy
  `--privileged`, user thường, `GOTOOLCHAIN=auto` (go.mod cần 1.27), mount repo `:ro`, volume `devkit-gomod` cho
  module cache (phải `chmod a+rwX /go/pkg` và tạo `/go/pkg/sumdb`). Container tên `devkit-bwrap`.
- **Codex thật không tốn quota**: cài bằng chính `agentruntime.Manager.Install` vào scratchpad (file test tạm,
  không commit); `codex app-server generate-ts` cho schema; mã nguồn đúng bản ở tag `rust-v0.147.0` của
  `openai/codex` (đọc để biết hành vi, ví dụ duyệt tool MCP).
- Test adapter bằng **app-server giả chạy trong tiến trình con** (`TestCodexFakeAppServer`, chính binary test,
  kịch bản qua `DEVKIT_FAKE_CODEX`). `GORACE=atexit_sleep_ms=0` cho helper, nếu không mỗi Close dưới `-race` chờ 1 s.

## Việc tiếp theo

1. **Kế hoạch 3 — bridge + frontend + tài liệu** (viết theo đúng cách trên):
   - Bridge: `AgentHubSignInStatus`, `AgentHubStartBrowserSignIn`/`AgentHubStartDeviceSignIn` (→ `Service.StartSignIn`),
     `AgentHubCancelSignIn`, `AgentHubSignOut`; capability `agenthub:execute` trên `auth:codex`, **chỉ UI**, không
     tool MCP; `wails3 generate bindings -clean=true -ts -i`.
   - `CodexOptions.APIKey`: preferences lưu **id account** `KindToken` của `internal/accounts`, resolve trong Go
     lúc spawn; `SignInStatus` báo `apiKey` khi có. Accounts thêm dòng `KindExternal` "Codex (ChatGPT)".
   - Frontend: bảng mô tả vendor thay ~10 chỗ hard-code `"claude"` (spec §9 Giai đoạn 6); panel đăng nhập (trình
     duyệt mặc định, "Use a code instead" kèm dòng hướng dẫn bật device code trong ChatGPT → Settings → Security,
     nghe event `agenthub:signInCompleted`); card Codex trong Settings → Agents; dòng cảnh báo Docker/`$HOME` trong
     hộp xác nhận Full auto; thẻ duyệt tool MCP hiện đúng (toolCallId có thể là `mcp-approval-<n>` khi không gắn
     được vào lệnh gọi); `isPermissionBypassChoice` với id Codex.
   - `internal/bridge/errors.go`: thông báo `agent_auth_required` đang viết riêng cho Claude → theo vendor.
   - Tài liệu: `agent-hub.mdx`, `implementation-status.mdx`, AGENTS.md, README; smoke test thủ công trên app desktop
     (đăng nhập; lượt Ask; Full auto + một lệnh prod qua MCP phải bị hỏi; resume sau khởi động lại).

## Chưa đo / còn treo

- Hình dạng lỗi khi token hết hạn giữa lượt (hiện map mọi `unauthorized` sang auth-required).
- Đăng nhập device code từ jail (chỉ trình duyệt đã chạy thật; device code không cần cổng callback nhưng vẫn cần
  `trustd` cho bước đổi token — đã có).
- Live trên Linux (bwrap): chỉ Phase 0 đã chạy thật ở đó; đường đăng nhập và lượt thật của adapter mới chạy thật
  trên macOS.
- Đối chứng: hook `PermissionRequest` *có* tự duyệt khi hooks bật (hiện chỉ biết `--disable hooks` chặn được).
- Bằng chứng sau khi sửa rằng server mode từ chối `AgentHubCreateSession` (và sign-in) qua bridge thật.
- Nếu Codex không bao giờ gửi `account/login/completed` sau khi đăng nhập thành công, flow bị báo "timed out"
  sau 15 phút dù `auth.json` đã ghi — chưa gặp, chỉ đọc code thấy.
- Test cài npm thật trên Linux (trên macOS đã chạy qua `agentruntime.Install`: glob `NativeEntry` thứ nhất khớp).

Chạy lại live (hỏi người dùng trước — tốn quota): đăng nhập một home nháp bằng `StartBrowserSignIn` (file test tạm
gọi adapter với `Home` = thư mục nháp, mở `authUrl` bằng `open`), rồi
`DEVKIT_CODEX_LIVE=1 DEVKIT_CODEX_LIVE_HOME=<home nháp> DEVKIT_CODEX_COMMAND=<binary> go test ./internal/agenthub/ -run CodexLive -v`;
xong thì `CODEX_HOME=<home nháp> <binary> logout` và xoá thư mục.

## Việc nhỏ ghi lại để làm sau

- Test helper `waitForPending` (`internal/mcp/gate_test.go`) gọi `t.Fatal` trong goroutine.
- Lời nhắc xác nhận env chưa ghi **tên profile** (khó phân biệt hai profile prod).
- Hỏi quyền qua notification OS cho server mode (spec §8.1, 4 điều kiện).
- Chuyển Claude sang `CLAUDE_CONFIG_DIR` + deny-write file chính sách của Claude (research doc, mục cuối).
  **Lưu ý khi làm**: dưới shared home, session Codex ghi được `agent-homes/` (vì `carveOut` mở đường tới home
  Codex), nên có thể tạo sẵn `agent-homes/claude/…`. DevKit phải tạo và reset home Claude trước session đầu,
  giống `prepareCodexHome`.
- Kill theo process group khi đóng session trên macOS (Seatbelt không có pid namespace): codex tự dọn lệnh con
  khi stdin đóng (đã đo), nhưng Claude đang bị SIGKILL thẳng.
- Skill hệ thống của Codex (`skills/.system`) không cài được vì `skills/` chỉ-đọc — codex log một dòng ERROR mỗi
  lần khởi động, vô hại. Nếu muốn có chúng, DevKit tự chép vào lúc reset.

## Lưu ý môi trường

- Test sandbox runtime cần `bwrap` (Linux) hoặc `sandbox-exec` (macOS); `TestDenyWriteHoldsAtRuntime` chạy trên cả hai.
- `TestCodexRealBinaryInTheJail` cần `DEVKIT_CODEX_COMMAND` = binary native
  (`…/node_modules/@openai/codex-<platform>/vendor/<triple>/bin/codex`), không phải launcher npm.
- `go test ./...` fail ở gói gốc nếu thiếu `frontend/dist` và thư viện GTK — đúng như AGENTS.md; chạy theo
  phạm vi `./internal/...`.
