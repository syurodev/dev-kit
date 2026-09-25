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
   `…-plan-2b-translate-and-approvals.md`, `…-plan-2c-runtime-adapter.md`,
   `docs/superpowers/plans/2026-09-25-codex-plan-3-bridge-frontend-docs.md`.

## Trạng thái

Kế hoạch 1–2c đã merge vào `main` (PR syurodev/dev-kit#148). Kế hoạch 3 nằm trên nhánh
**`claude/adoring-bell-9b9d9k`** (tách từ `main` 05ef2f1 — merge của #148), **chưa push** tại thời điểm viết (người dùng
quyết định lúc nào push; container cloud là tạm, commit chưa push mất khi container bị thu hồi).

| Phần | Trạng thái | Nằm ở đâu |
|---|---|---|
| Kế hoạch 1 — đo Codex thật + vá bảo mật | Xong, đã review, đã merge | `internal/mcp/gate.go`, `internal/agenthub/adapter_servermode.go`, `main.go` |
| Kế hoạch 2a — kết nối app-server + runtime native | Xong, đã merge | `internal/codexapp`, `internal/agentruntime` |
| Kế hoạch 2b — deny-write, bộ dịch, approval | Xong, đã merge | `internal/sandbox`, `codex_translate.go`, `codex_approvals.go` |
| Kế hoạch 2c — runtime `CodexAdapter` | Xong, live test trên macOS, đã merge | `adapter_codex.go`, `codex_auth.go`, `codex_home.go`, `protect.go` |
| Kế hoạch 3 — bridge, frontend, tài liệu | **Xong 10/10 task, đã review tổng (opus) + sửa** (d4df98e…e5168cf) | xem dưới |
| Smoke thủ công trên app desktop | **Chưa làm** — người dùng làm, checklist ở "Việc tiếp theo" | |

Kế hoạch 3, theo chỗ:

- **Lỗi theo vendor**: `agenthub.VendorError{Agent, Err}` gói `ErrAgentNotInstalled`/`ErrAgentAuthRequired` tại adapter
  (Claude 4 chỗ, Codex 6 chỗ); `ErrorVendor(err)`; `internal/bridge/errors.go` viết thông báo theo vendor. Key API chọn
  trong Settings mà hỏng (mất khỏi Accounts, không đọc được, Codex từ chối) còn mang `ErrAgentAPIKeyUnusable` → mã
  `agent_api_key_unusable`, UI trỏ về Settings chứ không mở đăng nhập ChatGPT (key luôn thắng khi spawn).
- **Bridge đăng nhập, chỉ UI**: `AgentHubSignInStatus` / `AgentHubStart{Browser,Device}SignIn` / `AgentHubCancelSignIn` /
  `AgentHubSignOut` (`internal/bridge/agenthub.go`, scope `auth:<agent>`). `Service.inAppSignIn` (`codex_auth.go`) và
  `app.CodexAPIKey.Choose` từ chối caller đã đóng dấu agent/user-terminal; `internal/mcp/tool_names_test.go`
  `TestNoToolSignsAgentsIn` chặn tool/lời gọi sign-in hay chọn key. Audit ghi phương thức + id account, không bao giờ
  trang/mã/login id/key.
- **API key**: `preferences` key `agent_codex_api_key_account` (chỉ id); `internal/app/agentcodex.go` `CodexAPIKey`
  (`Key` = `CodexOptions.APIKey` trong `main.go`, `Choose` kiểm account dùng được, `UsedBy` chặn xoá); bridge
  `PreferencesGet/SetCodexAPIKeyAccount`. `SignInStatus` với key hỏng báo `{signedIn:false, method:"apiKey"}`.
- **Accounts**: `RecordExternal`/`ForgetExternal` (xoá mọi bản trùng); dòng `external` "Codex (ChatGPT)" provider `openai`
  ghi khi `account/login/completed` thành công (`main.go`), xoá khi `AgentHubSignOut`.
- **Frontend**: bảng vendor `frontend/src/modules/agenthub/vendors.ts`; bridge `frontend/src/bridge/agentSignIn.ts`;
  `CodexSignIn.tsx` (`createSignInTracker` quyết event nào tính: flow của mình, **mọi** Codex sign-in thành công, event
  tới trước khi start trả lời); `AgentRuntimeSetup.tsx` (thay `ClaudeAgentSetup`); `AgentHubView` (`createFailure`,
  `signInFor`, `retryAfterSignIn`); `CodexAccountSection.tsx` + `AgentRuntimeSetting({vendor})` + nhóm CODEX trong
  Settings; `bypassSandboxWarning` trong `AgentComposer.tsx`; `permissionDetail` + `allowAlwaysLabel` cho thẻ duyệt tool
  MCP; `AgentSessionError` cause `apiKey`.
- **Tài liệu**: `docs/dev-kit/agent-hub.mdx`, `implementation-status.mdx`, `AGENTS.md`, `README.md`, spec §6.4.

Kiểm cuối (2026-09-25, **Linux**, container cloud, bwrap 0.9.0 cài bằng apt, chạy root):
`go test ./internal/{sandbox,codexapp,agentruntime,agenthub,bridge,mcp,arch,app,accounts,preferences}/ -race` xanh,
`go test ./internal/... ./cmd/...` xanh, `go vet -tags server ./...`, `gofmt`, `go build -tags server` sạch;
`cd frontend && bun run build && bun test` xanh (trừ flake có sẵn, xem "Lưu ý môi trường"). Chưa chạy lại trên macOS.

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

1. **Smoke thủ công trên app desktop** (người dùng làm; tốn quota Codex — một lượt Ask và một lượt Full auto ngắn;
   home Codex của DevKit là `<data>/agent-homes/codex`, không đụng `~/.codex`). Chuẩn bị: `task build`; một profile DB
   (hoặc SSH) gắn env prod có "Requires confirmation"; Settings → Agents → Sandbox để mặc định.
   - [ ] **Cài + đăng nhập**: Settings → Agents có nhóm CODEX; Install 0.147.0 → headline "Codex 0.147.0 · tested", dòng
     phụ chỉ "… on disk". SIGN IN: "Not signed in", nút **Sign in with ChatGPT** (đầu) và **Use a code instead**. Bấm
     trình duyệt → trang OpenAI mở, panel "Finish signing in in your browser." → xong thì "Signed in to Codex.", dòng
     `ChatGPT · email · plan`, có Sign out; Accounts có dòng external "Codex (ChatGPT)". (Tuỳ chọn) "Use a code instead"
     hiện mã + dòng nhắc bật device code trong ChatGPT → Settings → Security; Cancel.
   - [ ] **Một lượt Ask**: New session, bộ chọn có Claude và Codex; chọn Codex, project nháp, "Tạo file hello.txt chứa
     chữ hi" → thẻ duyệt sửa file (diff), Allow once → file tạo; placeholder "Ask Codex…", viền streaming màu ocean.
   - [ ] **Thẻ tool MCP (Ask)**: "Dùng tool DevKit env.listTags rồi cho tôi biết kết quả" → thẻ hiện tên tool, câu hỏi
     của Codex và đối số; nút thứ ba **Allow for session**, không phải "Allow always".
   - [ ] **Full auto + prod**: chọn Full auto → hộp "Skip permission prompts?" có dòng vàng về Docker socket và `$HOME` →
     Skip permissions. "Chạy db.query `select 1` trên profile <prod>" → **phải hiện lời nhắc xác nhận env prod của
     DevKit**; Deny → Codex báo lỗi tool.
   - [ ] **Resume**: thoát hẳn app, mở lại session Codex, gửi tiếp → đúng hội thoại cũ, chế độ về Ask.
   - [ ] **Lỗi + dọn**: Sign out → "Not signed in", dòng "Codex (ChatGPT)" biến mất; gửi một câu trong session Codex →
     "Codex isn't signed in…" (không nhắc Claude/ANTHROPIC_API_KEY) và panel đăng nhập Codex ngay trong thẻ lỗi.
     (Tuỳ chọn) tạo token OpenAI trong Accounts, chọn ở "API key instead of ChatGPT" → `API key · <tên>`; xoá account
     đó → bị từ chối, nêu "Agent Hub: Codex API key"; chọn lại "None".
2. Sau smoke: sửa những gì lệch, rồi PR (tách từ `main`; ghi rõ plan 3 và các commit review).

## Chưa đo / còn treo

- **Kế hoạch 3 (review opus, không sửa)**: dòng `external` "Codex (ChatGPT)" là vault item nên sync — đăng xuất ở máy A
  xoá ghi chú của máy B dù B vẫn đăng nhập (nó chỉ là ghi chú; credential ở home Codex của từng máy). Nếu muốn chính
  xác theo máy thì thêm tên máy vào label hoặc giữ ngoài sync — hỏi người dùng trước (đổi quyết định "Accounts thêm dòng
  KindExternal").
- `unauthorized` **giữa lượt** khi đang dùng API key vẫn ra `agent_auth_required` (panel ChatGPT) chứ không phải
  `agent_api_key_unusable`: runtime chưa nhớ lượt đó dùng key. Chỉ lúc spawn mới phân biệt.
- Việc bỏ `authRetry` khi đổi agent trong bản nháp (`AgentHubView`, `onAgentChange`) không có unit test — nằm trong view
  không render được bằng `renderToString`; thuộc smoke.

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

- `AgentSessionError` còn câu mặc định nói "Claude" cho `notInstalled`/`authRequired` — chỉ dùng khi view không truyền
  `reason` (view luôn truyền), nên không hiện ra; đổi sang trung tính khi tiện.

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

- **Container cloud Linux** (session Kế hoạch 3): không có sẵn bwrap, GTK/WebKit, wails3. `apt-get update && apt-get
  install bubblewrap` (nhỏ); `libgtk-4-dev libwebkitgtk-6.0-dev` (~92 MB, người dùng đồng ý) cần cho `internal/bridge` và
  wails3; wails3 cài vào module tạm (`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.25` từ một thư mục
  ngoài repo). bwrap chạy được dưới root trong container này. Không có Docker-in-Docker cần dùng.
- Flake frontend có sẵn trên `main`: `src/lib/debouncedWriter.test.ts` thỉnh thoảng hỏng khi chạy cả bộ (`window is not
  defined`), chạy riêng thì qua.

- Test sandbox runtime cần `bwrap` (Linux) hoặc `sandbox-exec` (macOS); `TestDenyWriteHoldsAtRuntime` chạy trên cả hai.
- `TestCodexRealBinaryInTheJail` cần `DEVKIT_CODEX_COMMAND` = binary native
  (`…/node_modules/@openai/codex-<platform>/vendor/<triple>/bin/codex`), không phải launcher npm.
- `go test ./...` fail ở gói gốc nếu thiếu `frontend/dist` và thư viện GTK — đúng như AGENTS.md; chạy theo
  phạm vi `./internal/...`.
