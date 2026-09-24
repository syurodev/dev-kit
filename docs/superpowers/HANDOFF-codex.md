# Bàn giao: Codex trong Agent Hub

Ngày: 2026-09-24 · Viết cho: session tiếp theo (máy cá nhân, có git) — session đó **không** có hội thoại
hay bộ nhớ của máy trước, nên mọi thứ cần biết nằm ở đây và trong các tài liệu được trỏ tới.

## Đọc theo thứ tự

1. File này.
2. Spec: `docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md` — thiết kế đã được người dùng duyệt
   từng phần, cập nhật theo số đo thật (Giai đoạn 0) và theo review của từng kế hoạch.
3. Research: `docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md` — vì sao Codex có `CODEX_HOME`
   riêng, cách đăng nhập, và **kết quả đo Giai đoạn 0** (mục "Phase 0 results").
4. Các kế hoạch đã chạy (để biết cách làm, không cần chạy lại):
   `docs/superpowers/plans/2026-09-24-codex-plan-1-measure-and-security.md`,
   `…-plan-2a-wire-and-runtime.md`, `…-plan-2b-translate-and-approvals.md`.

## Trạng thái

| Phần | Trạng thái | Nằm ở đâu |
|---|---|---|
| Kế hoạch 1 — đo Codex thật + vá bảo mật | Xong, đã review | `internal/mcp/gate.go` (`GateProfileMCP`, 5 tool), `internal/agenthub/adapter_servermode.go`, `main.go` (server mode), capture `internal/agenthub/testdata/codex_session_capture.jsonl` |
| Kế hoạch 2a — kết nối app-server + runtime native | Xong, đã review + sửa | `internal/codexapp/{wire,conn,protocol}.go`, `internal/agentruntime` (`NativeEntry`, `Verifiers`, agent `codex` 0.147.0) |
| Kế hoạch 2b — sandbox DenyWritePaths, bộ dịch, approval | Xong 4/4 task, **chưa review tổng** | `internal/sandbox` (`DenyWritePaths`), `internal/codexapp/items.go`, `internal/agenthub/codex_translate.go`, `codex_approvals.go` |
| Kế hoạch 2c — runtime `CodexAdapter` | **Chưa viết** | xem "Việc tiếp theo" |
| Kế hoạch 3 — bridge, frontend, tài liệu | **Chưa viết** | spec §6.4, §7.1, §9 Giai đoạn 5–7 |

Chưa có gì của Codex được nối vào app: `main.go` chưa đăng ký `CodexAdapter` (chưa tồn tại). Mọi thứ đã
làm đều có test và đều xanh.

## Quyết định của người dùng (giữ nguyên)

- **Phạm vi A** (cốt lõi): session, prompt, stream, tool call, duyệt quyền, Stop, sandbox, resume, chọn model,
  ba chế độ. Checkpoint/rewind, steer, goal, elicitation, plan streaming để sau — nhưng **chừa đường**:
  bộ dịch và bộ ánh xạ là bảng dispatch (spec §5.4).
- **Binary**: DevKit tự cài `@openai/codex` **pin 0.147.0** qua `internal/agentruntime`; chạy thẳng binary native.
- **Xác thực**: `CODEX_HOME` riêng của DevKit (`<data>/agent-homes/codex`), `~/.codex` thật bị ẩn khỏi jail.
  Đăng nhập **trình duyệt mặc định** (device code tắt sẵn theo tài khoản — đo được), device code tuỳ chọn,
  API key từ module accounts với store `ephemeral`. Không copy `auth.json`.
- **Chế độ**: Ask = `untrusted` (mặc định), Auto = `on-request`, Full auto = `never` (id ACP `bypassPermissions`
  để hộp xác nhận bypass sẵn có tự bật). Full auto **vẫn phải bị cổng env prod của DevKit chặn** — cổng nằm
  trong tool MCP phía DevKit, đã kiểm.
- **Không thu hẹp** `ShareHome`/Docker khi Full auto (Docker là cần thiết); thay bằng cảnh báo trong hộp xác nhận.
- **Server mode**: hiện fail-fast mọi lời nhắc; **về sau hỏi quyền qua notification của hệ điều hành** (spec §8.1).
- Claude sẽ chuyển sang cùng cách đăng nhập (`CLAUDE_CONFIG_DIR`) — follow-up, các bước ở cuối research doc.

## Cách làm đã chạy tốt

- Người dùng muốn **subagent-driven**, việc nhẹ giao model nhỏ: haiku cho task cơ học, sonnet cho task nhiều
  file/review, opus cho review tổng cuối mỗi kế hoạch.
- **Viết plan bằng code đã chạy**: viết và test toàn bộ code trong một `git worktree` tạm (nhớ copy
  `frontend/dist` vào — `main.go` embed nó, `internal/arch` sẽ fail nếu thiếu), rồi nhúng vào plan dưới dạng
  **patch test + patch hiện thực** mỗi task; trích patch từ chính file plan, áp lên worktree sạch, test lại.
- Kiểm task của subagent bằng **so cây**: áp patch của plan lên index tạm từ commit trước
  (`GIT_INDEX_FILE=… git read-tree <prev> && git apply --cached …`), `git write-tree`, so với cây của commit.
- Sau mỗi kế hoạch: review tổng (opus) → sửa → xuất.

## Việc tiếp theo

1. **Review tổng Kế hoạch 2b (opus)** trên các file của 2b (bảng trạng thái). Trọng tâm: Seatbelt deny-write +
   chặn rename tổ tiên; bộ dịch đóng tool call khi lượt bị ngắt; ánh xạ approval fail closed.
2. **Viết Kế hoạch 2c — runtime `CodexAdapter`** (`internal/agenthub/adapter_codex*.go`), theo spec §4.3, §5.1, §6, §7:
   - Spawn `codex app-server` qua `sandbox.StartPiped` (xem `adapter_claude.go`: `sandboxFor`, `resolveCommand`,
     `runtimeLaunch`, `stdioPipe`); cờ `-c cli_auth_credentials_store=…`, `-c check_for_update_on_startup=false`,
     `-c mcp_servers.devkit.*` từ `OpenSpec.McpServers`, `--disable hooks --disable plugins --disable apps --disable computer_use`.
   - Tạo sẵn trong `CODEX_HOME`: `config.toml`, `rules/`, `AGENTS.md`, `AGENTS.override.md`, `skills/` rồi đưa
     vào `Policy.DenyWritePaths` (spec §4.3). `ReadWritePaths += CODEX_HOME`, `DenyReadPaths += ~/.codex`,
     keep-list `agenthub.ProtectDataDir` thêm `agent-homes/codex`.
   - Handshake `initialize` (`clientInfo.name = "devkit"`) → `initialized` → `account/read` → `thread/start`
     (không gửi `sandbox`). Mọi `turn/start` gửi `approvalPolicy`, `approvalsReviewer: "user"`,
     `sandboxPolicy: externalSandbox`.
   - `Prompt` chờ `turn/completed` đúng `turnId`; `Cancel` = `turn/interrupt`; lỗi `unauthorized` → `ErrAgentAuthRequired`;
     `error{willRetry:true}` chỉ log. Handler `codexapp.Handler`: `Notification` → `codexTranslator.Translate` →
     `OnUpdate`; `Request` → `approvalFor` → `OnPermission` → `Response`, `codexPermissionsRequest`,
     `codexUnsupportedRequest`. **Handler `Request` phải tôn trọng ctx** (xem doc `codexapp.Handler`).
   - `RuntimeModes` (ask/auto/bypassPermissions), `RuntimeConfigurator` (model qua `model/list`), `SessionLoader`
     (`thread/resume`), `NormalizeUsage`.
   - Đăng nhập: interface tuỳ chọn `InAppSignIn` (spec §6.4) — tiến trình app-server ngắn hạn trong jail.
   - Verifier: `runtimes.Verifiers[agentruntime.CodexCLI]` = `initialize` trong jail.
   - **Quyết định còn mở (spec §4.2):** đặt `CODEX_MANAGED_BY_NPM` / `CODEX_MANAGED_PACKAGE_ROOT` và thêm
     `codex-path` (chứa `rg` đi kèm) vào `PATH` trong `resolveAt`, hay để trống có chủ đích — phải đo.
   - Nối `main.go`: đăng ký cạnh Claude, trong danh sách `vendors` đi qua `agenthub.ForServerMode` ở server mode.
   - Live test bật bằng `DEVKIT_CODEX_LIVE=1`.
3. **Kế hoạch 3 — bridge + frontend + tài liệu**: bảng mô tả vendor thay ~10 chỗ hard-code `"claude"`; panel đăng
   nhập (trình duyệt/mã/API key); card Codex trong Settings; dòng cảnh báo Docker/`$HOME` trong hộp xác nhận
   Full auto; thông báo `agent_auth_required` ở `internal/bridge/errors.go` đang viết riêng cho Claude → theo vendor;
   cập nhật `agent-hub.mdx`, `implementation-status.mdx`, AGENTS.md, README; smoke test thủ công trên app desktop.

## Chưa đo / còn treo

- Test cài npm thật: `DEVKIT_AGENT_INSTALL_TEST=1 go test ./internal/agentruntime/ -run ForReal -v` (~300 MB,
  cần node/npm). Chưa chạy — đường dẫn đã đối chiếu với một bản cài 0.147.0 có sẵn, khớp glob "nested".
- Hình dạng lỗi khi token hết hạn giữa lượt.
- Đối chứng: hook `PermissionRequest` *có* tự duyệt khi hooks bật (hiện chỉ biết `--disable hooks` chặn được).
- Bằng chứng sau khi sửa rằng server mode từ chối `AgentHubCreateSession`.

## Việc nhỏ ghi lại để làm sau

- Test helper `waitForPending` (`internal/mcp/gate_test.go`) gọi `t.Fatal` trong goroutine.
- Lời nhắc xác nhận env chưa ghi **tên profile** (khó phân biệt hai profile prod).
- Hỏi quyền qua notification OS cho server mode (spec §8.1, 4 điều kiện).
- Chuyển Claude sang `CLAUDE_CONFIG_DIR` + deny-write file chính sách của Claude (research doc, mục cuối).

## Lưu ý môi trường

- Test sandbox runtime cần `bwrap` (Linux). Trên macOS, phần Seatbelt chỉ có test chuỗi profile.
- Live test Codex cần đăng nhập Codex vào một `CODEX_HOME` nháp (trình duyệt; device code phải bật trong
  ChatGPT → Settings → Security). Không dùng/copy `~/.codex` thật.
- `go test ./...` fail ở gói gốc nếu thiếu `frontend/dist` và thư viện GTK — đúng như AGENTS.md; chạy theo
  phạm vi `./internal/...`.
