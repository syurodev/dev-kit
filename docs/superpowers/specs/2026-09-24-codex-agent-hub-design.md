# Codex trong Agent Hub (Lớp 2) qua `codex app-server`

Ngày: 2026-09-24 · Trạng thái: Design (đã duyệt; cập nhật theo Giai đoạn 0) · Phạm vi: Agent Hub, vendor thứ hai sau Claude

Liên quan: `docs/dev-kit/agent-hub.mdx` (Lớp 1 + Phase 0 ngày 2026-09-20),
`docs/superpowers/research/2026-09-24-agent-auth-in-sandbox.md` (cách lấy credential),
`docs/superpowers/specs/2026-09-13-agent-runtime-install-design.md` (runtime do DevKit quản lý).

## 1. Vấn đề

Codex hôm nay chỉ được **quan sát** (Lớp 1: hook → `codex.hookEvent` → `internal/codexsessions`,
xem ở Settings → Agents → "Codex (observed)", chỉ đọc). Người dùng không mở được một session Codex
trong Agent Hub như Claude: không gửi prompt, không duyệt quyền, không sandbox, không lịch sử.

Phase 0 (2026-09-20) đã chọn hướng `codex app-server` thay vì `@zed-industries/codex-acp` (im lặng từ
2026-06-08), nhưng dừng trước khi chạy được một lượt thật. Máy phát triển hiện có `codex-cli 0.147.0`
đã đăng nhập, nên phần đo còn thiếu làm được (§9, Giai đoạn 0).

## 2. Mục tiêu và không phải mục tiêu

Mục tiêu (phạm vi "A — cốt lõi"):

1. Mở session Codex trong tab Agent Hub; prompt; stream câu trả lời và tool call (shell, sửa file).
2. Duyệt quyền qua `PermissionWaiter`/thẻ duyệt sẵn có; Stop (interrupt).
3. Chạy trong jail của DevKit (`sandbox.StartPiped`), giống Claude.
4. Resume session sau khi khởi động lại app; chọn model; ba chế độ Ask / Auto / Full auto.
5. Đăng nhập theo cách chuẩn (research 2026-09-24): home Codex riêng của DevKit, đăng nhập trình duyệt (hoặc device code) hoặc API key.
6. Sửa trước các lỗ bảo mật tìm thấy khi rà "Full auto có bị chặn ở prod không" (§8).

Không phải mục tiêu (phạm vi "B", làm sau — thiết kế chừa đường ở §5.4):

- Checkpoint/rewind (`thread/fork`, `turn/diff/updated`), steer, goal (`thread/goal/*`),
  plan streaming (`item/plan/delta`), elicitation (`item/tool/requestUserInput`), slash commands.
- Chuyển Claude sang cách đăng nhập mới — follow-up riêng, các bước ghi ở cuối research doc.
- Thu hẹp `ShareHome`/Docker socket khi Full auto — **cố ý không làm**: không có Docker, DevKit vô dụng
  với nhiều workflow. Thay bằng cảnh báo rõ ràng (§7.1).
- Gemini.

## 3. Quyết định đã chốt

| # | Câu hỏi | Chốt |
|---|---|---|
| Q1 | "Hỗ trợ Codex" là gì | Lớp 2: chạy Codex trong Agent Hub như Claude |
| Q2 | Phạm vi v1 | Cốt lõi (§2); nâng cao làm sau |
| Q3 | Lấy binary từ đâu | DevKit tự cài `@openai/codex` pin phiên bản qua `internal/agentruntime`; `DEVKIT_CODEX_COMMAND` để dev |
| Q4 | Xác thực | Home Codex riêng; đăng nhập trình duyệt mặc định, device code tuỳ chọn (sau Giai đoạn 0); API key từ module accounts, store `ephemeral` |
| Q5 | Chế độ | Ask (`untrusted`, mặc định) / Auto (`on-request`) / Full auto (`never`, xác nhận rõ ràng) |
| Q6 | Lỗ bảo mật | Sửa server mode + 5 tool MCP trước adapter; **không** thu hẹp Docker/`$HOME` |
| Q7 | Kiến trúc | Adapter Go nói app-server, dịch sang ACP trong adapter; core không biết Codex |

## 4. Kiến trúc

```
UI (AgentHubView, Composer, Settings→Agents)        ← bảng mô tả vendor thay hard-code "claude"
        │ bridge (+ vài method đăng nhập)
AgentHub Service / Store / Transcript / PermissionWaiter   ← KHÔNG sửa
        │ VendorAdapter (ACP-shaped: OnUpdate / OnPermission)
 ┌──────┴────────┐
ClaudeAdapter   CodexAdapter (mới)
 (ACP)            ├─ codexTranslate: notification → ACP session/update     (§5)
                  ├─ codexApprovals: server request → ACP request_permission (§7)
                  └─ codexAuth: account/*                                    (§6)
                        │ internal/codexapp (mới, lá): wire app-server
                  sandbox.StartPiped → codex app-server (binary native, pin 0.147.0)
                        CODEX_HOME = <data>/agent-homes/codex
```

### 4.1 `internal/codexapp` (package lá mới)

Kết nối stdio với `codex app-server`. Newline-delimited JSON, **không có field `"jsonrpc"`** (Phase 0).
Bốn loại message: request (client→server, có `id`), response, notification (không `id`), server
request (server→client, có `id`, client phải trả lời đúng một lần).

- Type Go viết tay **chỉ cho phần giao thức dùng tới**, đối chiếu với schema sinh từ
  `codex app-server generate-ts` của đúng phiên bản pin. Không sinh cả 90+ method.
- Notification giao theo thứ tự nhận; server request chạy handler riêng (như `acp.Conn` làm với
  `elicitation/create`) để một approval đang chờ không chặn stream.
- Tiến trình chết → mọi call đang chờ nhận lỗi; không treo.
- Không import `agenthub`; đăng ký trong `internal/arch` như `internal/acp`.

### 4.2 `internal/agentruntime`: entry native

`@openai/codex` là **binary native** (~300 MB/nền tảng) trong
`node_modules/@openai/codex-<platform>/vendor/<triple>/bin/codex`; `bin/codex.js` chỉ là launcher.

- `Agent` thêm khả năng khai entry native: `Resolved.Command` = binary `codex`, `Args` rỗng, `ReadOnlyPaths`
  = thư mục version. Không cần node lúc chạy; npm chỉ dùng lúc cài.
- Agent mới `codex`: `Package "@openai/codex"`, `TestedVersion "0.147.0"`, glob entry theo `<triple>`.
- Claude không đổi hành vi. Dùng chung màn hình cài / cập nhật / rollback.
- `DEVKIT_CODEX_COMMAND` (tuyệt đối) ghi đè runtime, giống `DEVKIT_CLAUDE_ACP_COMMAND`.
- **Cần quyết ở Kế hoạch 2c (runtime; review Kế hoạch 2a):** launcher `bin/codex.js` của 0.147.0 đặt
  `CODEX_MANAGED_BY_NPM=1` và `CODEX_MANAGED_PACKAGE_ROOT=<realpath node_modules/@openai/codex>` trước khi
  chạy binary; binary đọc hai biến này (và `codex-package.json`, thư mục `codex-path` chứa `rg` đi kèm).
  Chạy thẳng binary thì mất cả hai → có thể đổi hành vi tự cập nhật / nhận diện cách cài, và `rg` đi kèm
  có thể không được tìm thấy. 2c phải đo rồi chọn: đặt hai biến + thêm `codex-path` vào `PATH` trong
  `resolveAt`, hay để trống có chủ đích (DevKit tự quản lý phiên bản, không muốn Codex tự cập nhật).
- Bản cài Codex hiện được kích hoạt với `verified.handshake=false` (chưa có verifier) và đã gọi được qua
  bridge/MCP `AgentRuntimeInstall("codex")`. 2c thêm verifier (`initialize` app-server trong jail).

### 4.3 `CodexAdapter` (`internal/agenthub/adapter_codex*.go`)

File theo trách nhiệm: `adapter_codex.go` (spawn, handshake, jail), `codex_translate.go`,
`codex_approvals.go`, `codex_auth.go`.

Hiện thực: `VendorAdapter`, `SessionLoader` (`thread/resume`), `RuntimeConfigurator`
(model qua config option, chế độ qua `SetMode`), `RuntimeModes`, `InAppSignIn` (§6). Không hiện thực
`SessionSteerer`, `RuntimeForker`, `RuntimeDeleter` ở v1 — core đã xử lý "vendor không hỗ trợ".

**Spawn** (mỗi session một tiến trình, như Claude):

- `codex app-server` với:
  - `-c cli_auth_credentials_store="file"` (ChatGPT) hoặc `"ephemeral"` (API key, §6.2)
  - `-c check_for_update_on_startup=false`
  - `-c mcp_servers.devkit.*` → `devkit-mcp` proxy với socket định danh của session (như Claude nhận qua `OpenSpec.McpServers`)
  - `--disable hooks --disable plugins --disable apps --disable computer_use` (§7.3)
- Env: `CODEX_HOME=<data>/agent-homes/codex` (tạo 0700).
- **Các file mang chính sách trong `$CODEX_HOME` không được để jail ghi** — qua `sandbox.Policy.DenyWritePaths`
  (Kế hoạch 2b): bwrap `--ro-bind` đè lên home đọc-ghi, Seatbelt `deny file-write*` sau mọi allow (trước đây
  `ReadOnlyPaths` trên macOS **không** chặn ghi bên trong một vùng đọc-ghi). Danh sách, DevKit tạo sẵn (rỗng)
  trước mỗi spawn vì bwrap chỉ bind được thứ đã tồn tại:
  - `config.toml` — Giai đoạn 0: ghi được thì agent tự "trust" repo cho các session sau.
  - `rules/` — exec policy toàn cục (`~/.codex/rules/default.rules` có thật trên máy dev): một luật `allow`
    tự duyệt lệnh, **có thể ngay trong session đang chạy** — ghi lại lúc spawn không chặn được, nên phải chỉ-đọc.
  - `AGENTS.md`, `AGENTS.override.md` — chỉ dẫn toàn cục: ghi được là tiêm prompt bền vững vào mọi session sau.
  - `skills/` — skill toàn cục, cùng lý do.
  Phần còn lại của home (auth, sessions, sqlite, log) vẫn ghi được.
- Policy jail: như Claude (`NetworkHost`, `ShareHome`/Docker theo Settings), thêm
  `ReadWritePaths += CODEX_HOME`, `DenyReadPaths += ~/.codex`, keep-list `ProtectDataDir` thêm
  `agent-homes/codex`.
- Handshake: `initialize {clientInfo:{name:"devkit", title:"DevKit", version}, capabilities:{experimentalApi:false}}`
  → `initialized` → `account/read` (§6.3) → `thread/start {cwd, approvalPolicy, approvalsReviewer:"user"}`.
- `thread/start` **không** gửi `sandbox`: field đó là `SandboxMode` (`read-only`/`workspace-write`/
  `danger-full-access`), không có biến thể external. `externalSandbox` chỉ có ở `sandboxPolicy` của
  `turn/start`, nên **mọi** `turn/start` đều gửi nó (§5.1). `thread/resume` theo cùng quy tắc.
- `ACPSessionID()` = `threadId`.

## 5. Ánh xạ sự kiện Codex → ACP

`codexTranslate` là **hàm thuần** (notification vào, update ra) → test bằng golden replay (§9).

### 5.1 Vòng đời lượt

| Codex | DevKit |
|---|---|
| `Prompt()` → `turn/start {threadId, input, approvalPolicy, approvalsReviewer:"user", sandboxPolicy:{type:"externalSandbox", networkAccess}, model?}` | Codex trả `inProgress` ngay; adapter chờ `turn/completed` đúng `turnId` rồi mới trả `PromptOutcome` |
| `turn/completed` `completed` / `interrupted` | `StopReason` `end_turn` / `cancelled` |
| `turn/completed` `failed` | Lỗi có kiểu: `codexErrorInfo:"unauthorized"` → lỗi auth-required sẵn có; `usageLimitExceeded`, `contextWindowExceeded`, `sessionBudgetExceeded` → thông điệp rõ ràng; khác → lỗi chung kèm `message` |
| `error {willRetry:true}` | Chỉ log; không kết thúc lượt |
| `error {willRetry:false}` | Kết thúc lượt bằng lỗi đó nếu `turn/completed` không tới |
| `Cancel()` | `turn/interrupt {threadId, turnId}` |
| `turn/completed` `interrupted` với item còn mở | Giai đoạn 0: item đang chạy **không** nhận `item/completed`. Adapter giữ tập item đã `started` chưa `completed` và đóng từng cái bằng `tool_call_update {status:"failed"}` + lý do "cancelled" trước khi trả `PromptOutcome` |
| `serverRequest/resolved` | Codex tự rút một approval (ví dụ khi interrupt) → huỷ `PermissionWaiter` tương ứng để thẻ không treo |

### 5.2 Nội dung

| Codex | ACP `session/update` |
|---|---|
| `item/agentMessage/delta` | `agent_message_chunk`; `item/completed` của `agentMessage` chưa có delta nào → gửi nguyên `text` |
| `item/reasoning/summaryTextDelta` | `agent_thought_chunk` (chỉ tóm tắt; `textDelta` thô bỏ qua) |
| `commandExecution` started | `tool_call {toolCallId:itemId, kind:"execute", title, status:"in_progress", rawInput:{command,cwd}}` — `title` là `commandActions[0].command` (lệnh như model viết); `command` bị bọc `/usr/bin/zsh -lc "…"` |
| `item/commandExecution/outputDelta` | Không dịch ở v1: Giai đoạn 0 **không thấy** delta trên dây — output tới một lần trong `aggregatedOutput` khi item xong (giữ **64 KB cuối**). Nối streaming sau là một dòng dispatch |
| `commandExecution` completed | `tool_call_update` `completed`; `exitCode≠0` hoặc `failed` → `failed`; `declined` → `failed` + lý do; `rawOutput:{exitCode,durationMs}` |
| `fileChange` | `tool_call {kind:"edit"}` (có `move_path` → `"move"`, toàn xoá → `"delete"`), `locations`, content `diff`: `update` **dựng từ hunk** (`oldText` = ngữ cảnh + dòng xoá, `newText` = ngữ cảnh + dòng thêm, `…` giữa hai hunk); `add` → `diff` của Codex **là cả file mới** (đo được: không có `@@`), không `oldText`; `delete` → `oldText` = nội dung. Không đặt `truncated` (cờ đó của UI nghĩa là "bị cắt để xem trước"). **Không đọc file trên đĩa** (adapter ở ngoài jail; đường dẫn do agent đưa) |
| `mcpToolCall` | `tool_call {kind:"other", title:"server.tool", rawInput:arguments}`; completed → `rawOutput` |
| `webSearch` | `tool_call {kind:"fetch"}` |
| `turn/plan/updated` | `plan {entries}`; `inProgress` → `in_progress` |
| `thread/tokenUsage/updated` | `usage_update` qua `NormalizeUsage` (input, cached, output, reasoning, context window) |
| `thread/name/updated` | `session_info_update {title}` |

### 5.3 Bỏ qua và không nuốt mất

- Bỏ qua (log): `hook/*` (home DevKit không có hook), `userMessage` (DevKit tự ghi), `warning`,
  `configWarning`, `deprecationNotice`, `realtime/*`, `fuzzyFileSearch/*`, `skills/changed`,
  `thread/status/changed`, `thread/started`, `turn/started`, `account/rateLimits/updated` (Giai đoạn 0 thấy
  cả bốn cái sau trên dây).
- `remoteControl/status/changed`: bỏ qua **không log nội dung** — chứa `serverName` (hostname máy) và
  `installationId` (Giai đoạn 0).
- `turn/diff/updated`: bỏ qua ở v1, là đầu vào của checkpoint ở phạm vi B (§5.4).
- Item có loại chưa biết (`collabAgentToolCall`, `imageGeneration`, `contextCompaction`, …) →
  `tool_call {kind:"other", title:<type>, status:"completed", rawOutput:<item>}`.
- FR-8: mỗi `item/completed` chuyển **item Codex gốc** vào đối số `meta` của `OnUpdate`. Delta thì không
  (tránh phình dữ liệu).

### 5.4 Chừa đường cho phạm vi B

- `codexTranslate` và `codexApprovals` là **bảng dispatch** `map[method]handler` / `map[itemType]handler`.
  Thêm một loại = thêm một dòng + một hàm, không sửa luồng chính.
- Mọi method/item chưa có handler đi qua nhánh mặc định (§5.3), nên nối thêm sau không cần migrate transcript.
- Checkpoint (`turn/diff/updated`, `thread/fork`), steer, goal, elicitation, plan streaming: mỗi thứ là
  một handler + hiện thực một interface tuỳ chọn có sẵn (`RuntimeForker`, `SessionSteerer`, `OnElicit`).

## 6. Đăng nhập

Nguyên tắc (research 2026-09-24): Codex của DevKit có **home riêng**; `~/.codex` thật bị ẩn khỏi jail;
đăng nhập qua **API `account/*` của chính app-server**; DevKit không giữ token ChatGPT.

### 6.1 ChatGPT — trình duyệt (mặc định) hoặc device code

Giai đoạn 0 đã đổi thứ tự: device code **tắt mặc định theo tài khoản** (người dùng phải bật trong ChatGPT →
Settings → Security) và app-server **không báo trước** — nó trả mã bình thường, lỗi chỉ hiện trên trang của
OpenAI. Đăng nhập trình duyệt thì chạy được ngay từ trong bwrap.

1. UI bấm *Sign in* → backend chạy một `codex app-server` **ngắn hạn trong jail**, cùng home, store `file`.
2. **Mặc định — "Sign in with browser":** `account/login/start {type:"chatgpt"}` → mở `authUrl`; callback
   `localhost:1455` tới tiến trình trong jail vì dùng chung loopback (`NetworkHost`) — đã đo.
3. **Tuỳ chọn — "Use a code instead"** (máy không mở trình duyệt được, SSH…): `{type:"chatgptDeviceCode"}` →
   UI hiện link + mã, **kèm dòng**: *"Cần bật 'device code authorization for Codex' trong ChatGPT → Settings →
   Security trước."*
4. Chờ `account/login/completed`. Cancel → `account/login/cancel`; hết 15 phút → huỷ. Xong → tiến trình thoát.
5. **Không** đọc `account/read` trên chính tiến trình vừa đăng nhập: Giai đoạn 0 thấy nó trả `account: null`
   ngay sau `login/completed`. Trạng thái đọc ở tiến trình kế tiếp (Settings mở lại card, hoặc session mới).

### 6.2 API key

- Settings chọn một account `KindToken` trong `internal/accounts` (hoặc tạo mới). Preferences lưu **id account**, không lưu key.
- Mỗi spawn: store `ephemeral`; sau `initialize`, adapter `accounts.Resolve` trong Go rồi
  `account/login/start {type:"apiKey", apiKey}`. Key không nằm trên đĩa trong jail, không nằm trong env.

### 6.3 Trạng thái và lỗi

- Card Codex trong Settings hiện `account/read`: "ChatGPT · email · plan", "API key · <tên account>", hoặc "Chưa đăng nhập".
  Sign out → `account/logout` (ChatGPT) hoặc bỏ liên kết account (API key).
- Trước `thread/start`: `requiresOpenaiAuth && account == null` → **lỗi auth-required sẵn có**; UI dùng lại
  `authRetry` (mở panel đăng nhập, xong thì thử lại) nhưng panel Codex hiện mã thay vì terminal.
- Accounts có một dòng `KindExternal` "Codex (ChatGPT)" — đúng như package doc của `accounts` mô tả.

### 6.4 Interface và bridge

- `agenthub.InAppSignIn` (tuỳ chọn): `SignInStatus`, `StartDeviceSignIn`, `StartBrowserSignIn`,
  `CancelSignIn`, `SignOut`. `AuthProvider` (terminal) giữ nguyên cho Claude.
- Bridge: `AgentHubSignInStatus`, `AgentHubStartDeviceSignIn`, `AgentHubStartBrowserSignIn`,
  `AgentHubCancelSignIn`, `AgentHubSignOut`; event `agenthub:signInCompleted`.
- Capability `agenthub:execute` trên `auth:codex`, **chỉ UI**. Không có tool MCP đăng nhập.
- Audit ghi id account + phương thức; **không bao giờ** ghi mã một lần hay key.

## 7. Duyệt quyền và chế độ

### 7.1 Chế độ

Mỗi `turn/start` gửi tường minh `approvalPolicy`, `sandboxPolicy:{type:"externalSandbox"}`,
`approvalsReviewer:"user"`.

| UI | id ACP | `approvalPolicy` |
|---|---|---|
| Ask (mặc định mỗi session mới) | `ask` | `untrusted` |
| Auto | `auto` | `on-request` |
| Full auto | `bypassPermissions` | `never` |

- Công bố qua `RuntimeModes` / `current_mode_update`; composer dùng lại picker của Claude.
- id `bypassPermissions` **cố ý** khớp `isPermissionBypassChoice` để hộp xác nhận bypass tự bật; có test chốt.
- `SetMode` giữa lượt áp dụng từ lượt sau.
- Hộp xác nhận Full auto (dùng chung Claude) thêm dòng khi Settings đang bật `ShareHome`/Docker:
  *"Docker socket và `$HOME` đang mở cho session này — jail không chặn agent dùng chúng."*
- Đã đo (Giai đoạn 0): `untrusted` hỏi trước khi sửa file **và** trước lệnh shell có ghi (`touch …`); lệnh
  chỉ đọc (`cat`) chạy không hỏi. `on-request` và `never` với `externalSandbox` không hỏi gì.

### 7.2 Approval → thẻ duyệt quyền

Qua `OnPermission` → `PermissionWaiter`, cùng timeout với Claude.

| Codex | Thẻ | Allow | Allow cho session | Deny | Hết giờ / huỷ |
|---|---|---|---|---|---|
| `item/commandExecution/requestApproval` | `execute`: lệnh, cwd, `reason` | `accept` | `acceptForSession` | `decline` | `cancel` |
| `item/fileChange/requestApproval` | `edit`: diff từ item (§5.2) | `accept` | `acceptForSession` | `decline` | `cancel` |
| `item/permissions/requestApproval` | không hiện thẻ | — | — | — | — |

- **Nút dựng từ `availableDecisions`** của chính request (Kế hoạch 2b): Giai đoạn 0 đo `untrusted` với lệnh shell chỉ
  có `accept`, một execpolicy amendment và `cancel` — không `decline`, không `acceptForSession`. Allow = `accept`;
  "Allow cho session" chỉ khi có `acceptForSession`; Deny = `decline` nếu có, không thì `cancel` (dừng lượt).
  Trả lời hỏng/huỷ/hết giờ → `cancel` (nếu có, không thì `decline`). File change luôn đủ bốn quyết định.
- `acceptWithExecpolicyAmendment` / `applyNetworkPolicyAmendment` **không bao giờ** được đưa ra: một
  approval không ghi luật vĩnh viễn.
- Xin thêm quyền (network, thư mục ghi): dưới `externalSandbox` vô nghĩa — jail mới quyết. Adapter tự
  trả **grant rỗng** và ghi một dòng transcript: *"Codex xin quyền X. Quyền truy cập do sandbox của
  DevKit quyết định — Settings → Agents → Sandbox."*
- Server request khác (`item/tool/requestUserInput`, `mcpServer/elicitation/request`,
  `account/chatgptAuthTokens/refresh`, `attestation/generate`, v1 `applyPatchApproval`/`execCommandApproval`,
  và mọi method lạ) → **từ chối ngay** để lượt không treo. Mỗi loại một dòng dispatch (§5.4).

### 7.3 Cấu hình không được làm hỏng cổng

Tương tự bản sửa `settingSources` cho Claude (`adapter_claude.go`, `sessionNewMeta`):

- `--disable hooks`: hook `PermissionRequest` trong `.codex/config.toml` của repo không tự duyệt được.
- Tham số theo lượt (`approvalPolicy`, `approvalsReviewer`) thắng `approval_policy`/`approvals_reviewer`
  trong config của project.
- `--disable plugins --disable apps --disable computer_use`: không kéo hook/tool/MCP từ ngoài vào.
- Đã đo (Giai đoạn 0): repo chưa trust → Codex không nạp config của repo. Repo **đã trust** (ghi vào
  `$CODEX_HOME/config.toml`) → config được nạp nhưng tham số theo lượt vẫn thắng; approval vẫn tới client; hook
  không chạy. Chưa đo đối chứng "hook có tự duyệt khi bật" — `--disable hooks` giữ như lớp phòng thủ. Việc
  chặn jail ghi `config.toml` (§4.3) đóng luôn đường tự trust.

### 7.4 Cổng env prod

Tool MCP của DevKit chạm prod hỏi ở **mọi** chế độ kể cả Full auto (cổng nằm trong tool, phía DevKit;
lời nhắc do UI trả lời, không qua agent). Ở Ask có thể hỏi hai lần (Codex duyệt gọi tool, DevKit duyệt
prod) — giữ nguyên, vì là hai câu hỏi khác nhau.

## 8. Sửa lỗi bảo mật (làm trước adapter)

Tìm thấy khi rà yêu cầu "Full auto phải bị chặn ở prod" (2026-09-24, đọc code; chưa tái hiện).

### 8.1 Server mode: bridge mở cho agent trong jail

Server mode phục vụ **toàn bộ** bridge qua HTTP loopback không xác thực (`POST /wails/runtime`); jail dùng
`NetworkHost` nên agent tới được. Hệ quả: agent gọi được `AgentHubResolvePermission` (tự duyệt tool call)
và `InteractionPending`/`InteractionComplete` (tự duyệt lệnh prod). AGENTS.md nói server mode fail-fast
nhưng `main.go` tạo `interaction.NewHub(interaction.NoopPrompter{})` không headless. Ảnh hưởng Claude
hiện tại.

Sửa hai lớp:

- **(a)** Dùng `application.System.IsServer()` sẵn có trong `main.go` (đã dùng cho notification). Server
  mode đăng ký `agenthub.ServerModeAdapter` (stub cùng `AgentType`) **thay cho** `ClaudeAdapter`/`CodexAdapter`;
  mọi `Open`/`Load` trả `agenthub.ErrUnavailableInServerMode`; UI hiện "Agent Hub cần app desktop".
- **(b)** Server mode: hub tương tác `Headless = true` → selection/confirmation fail-fast. Chặn agent
  terminal (không jail) nối MCP vào server mode rồi tự bấm Allow qua HTTP.
- **Cái giá — quyết định 2026-09-24: chấp nhận, tạm thời.** Hub dùng chung cho MCP *và* UI, nên ở server
  mode **mọi** lời nhắc đều fail-fast, không chỉ tool MCP chạm prod:
  - tool MCP theo target đang active ở **mọi** env (`db.query`, `ssh.runCommand`, `kafka.*`) — trước đây env
    dev hỏi một lần rồi cho "allow always";
  - thao tác người dùng làm từ UI trình duyệt mà cần xác nhận: `git push` lên remote prod, commit vào nhánh có
    `git-guard`, `api.send` tới env prod, gate của `tasks`.

  Lý do: server mode không phân biệt được cú bấm Allow của người với của một tiến trình cục bộ khác — bridge
  không có xác thực.
- **Hướng về sau — hỏi quyền qua notification của hệ điều hành.** Lời nhắc đi ra ngoài băng (out-of-band):
  DevKit gửi notification OS có nút Allow/Deny, câu trả lời về qua kênh notification của OS (D-Bus
  `org.freedesktop.Notifications` `ActionInvoked` trên Linux, `UNUserNotificationCenter` trên macOS, toast
  activation trên Windows) — **không** qua HTTP bridge, nên tiến trình gọi bridge không tự trả lời được.
  Điều kiện khi làm: (1) server mode cần một backend notification (hiện `main.go` để notifier `nil` ở server
  mode); (2) chỉ nhận action từ **đúng** notification server (kiểm tra sender của signal D-Bus), vì tiến trình
  cùng user có thể gửi signal giả; (3) mỗi lời nhắc một id ngẫu nhiên, hết hạn theo timeout của hub, env
  `requiresConfirmation` vẫn không có "allow always"; (4) không có session D-Bus (WSL, container) → giữ
  fail-fast như hiện tại. Làm như một task riêng, tách khỏi Codex.
- Kiểm chứng desktop (2026-09-24): **không mở cổng mạng nào.**
  - Tĩnh: trong Wails v3.0.0-beta.25 `pkg/application`, listener TCP chỉ có ở `application_server.go`
    (`//go:build server`) và `mcp_enabled.go` (`//go:build mcp`); bản desktop build với `-tags production`.
  - Động: `bin/dev-kit` đang chạy — `ss -ltunp` trên **toàn bộ cây tiến trình** (hơn 100 pid, kể cả tiến trình
    WebKit) không có listener TCP/UDP nào; chỉ có Unix socket `~/.config/DevKit/mcp.sock` của MCP host.
  - Rủi ro còn lại: ai đó build với `EXTRA_TAGS=mcp` sẽ bật listener của Wails.

### 8.2 Năm tool MCP chạm prod không hỏi

`db.explain`, `db.listForeignKeys`, `db.testConnection`, `ssh.testConnection` nhận `profileId` và kết nối
server thật mà không qua cổng env. **`db.schema.get`** cũng vậy (review Task 9 tìm ra): mô tả là "đọc cache"
nhưng khi cache trống `GetSchemaCache` gọi `IndexSchema` → mở kết nối thật. Rà soát toàn bộ tool MCP sau
đó (review Task 10) không thấy lỗ nào khác; `api.send` và `git.push` có cổng env bên trong service.

- Thêm `GateProfileMCP(ctx, hub, envs, profiles, profileID, tool, summary, kind)` cạnh `GateActiveMCP`.
- Chỉ hỏi khi env của profile `requiresConfirmation` — khi đó hỏi **mọi lần**, không allow_always.
  Env khác giữ nguyên (không hỏi) vì các tool này chỉ đọc.
- Tool chỉ đọc dữ liệu local (`db.history.list`, `db.savedQuery.list`, `kafka.connectionStatus`) giữ nguyên.
- Đã xác nhận `db.explain` không bị chèn câu lệnh thứ hai: pgx extended protocol; MySQL chặn `multiStatements` qua allowlist.

### 8.3 Không làm

`ShareHome` và Docker socket vẫn mở theo Settings khi Full auto (Q6). Rủi ro còn lại được ghi rõ trong
hộp xác nhận (§7.1) và ở đây: ở Full auto, agent đọc được `~/.kube`, `~/.pgpass`, `~/.netrc`, `.env`
của project, và Docker socket cho phép thoát jail hoàn toàn.

## 9. Test và thứ tự triển khai

Mỗi giai đoạn: commit nhỏ, test xanh mới sang tiếp; hết giai đoạn chạy `.sync/export.sh`.
Mọi giai đoạn chạy `go test` theo phạm vi, `go test ./internal/arch/`, `gofmt -l internal main.go`,
`go vet -tags server ./...`; frontend `bun run build` + `bun test`.

**Giai đoạn 0 — Đo bằng Codex thật** (cần người dùng đồng ý: ~5–6 lượt ngắn, repo nháp trong scratchpad,
chạy trong bwrap). `CODEX_HOME` là **thư mục mới trong scratchpad**, người dùng đăng nhập (dự định **device code**; thực tế phải dùng **trình duyệt**, xem research)
(tài khoản cá nhân). **Không copy `~/.codex/auth.json`**: hai bản cùng một refresh token có thể đăng xuất
codex ở terminal (research 2026-09-24). Cách này kiểm luôn §6.1 và điểm "device code có cho mọi workspace" (kết quả: không — tắt mặc định).

0. Đăng nhập vào home nháp (device code, rồi trình duyệt khi device code bị chặn); ghi lại kết quả.
1. `untrusted` / `on-request` / `never` + `externalSandbox`: lệnh shell và sửa file có sinh approval không.
2. Repo với `.codex/config.toml` độc hại (hook tự duyệt, `approval_policy="never"`,
   `approvals_reviewer="auto_review"`) → DevKit **vẫn** nhận approval.
3. Ghi một phiên thật (lệnh, sửa file, approval, interrupt, usage) vào
   `internal/agenthub/testdata/codex_session_capture.jsonl`, đã lọc token/email/đường dẫn cá nhân.
4. `ss -ltnp` trên app desktop: không có cổng TCP.

→ Kết quả trái thiết kế: **dừng và báo** trước khi code.

**Giai đoạn 1 — Sửa lỗi bảo mật (§8)**, mỗi mục một commit. Test: `tools_gated_test.go` (5 tool ×
prod/dev/headless), test `ServerModeAdapter` (mọi `Open`/`Load` → `ErrUnavailableInServerMode`) và
ánh xạ lỗi ở bridge; `go build -tags server` + `go vet -tags server` cho phần nối trong `main.go`.

**Giai đoạn 2 — `internal/codexapp`.** Test: framing không `jsonrpc`; ghép request/response; server request
trả lời đúng một lần; thứ tự notification; tiến trình chết → call đang chờ nhận lỗi. Đăng ký `internal/arch`.

**Giai đoạn 3 — `agentruntime` entry native + agent `codex`.** Test cây npm giả (đúng
`vendor/<triple>/bin/codex`; Claude không đổi). Cài thật: `DEVKIT_AGENT_INSTALL_TEST=1`.

**Giai đoạn 4 — `CodexAdapter`.**

| Phần | Test |
|---|---|
| Spawn/handshake/jail | Runner giả (`SandboxRunner`): `CODEX_HOME`, deny `~/.codex`, cờ `--disable`, keep-list `ProtectDataDir`, `clientInfo.name` |
| Dịch | Golden replay từ capture + ca biên: item lạ, thiếu delta, output > 64 KB, `willRetry` |
| Duyệt quyền | Bảng §7.2, timeout, cancel, không `ExecpolicyAmendment`, server request lạ bị từ chối, permissions → grant rỗng + dòng transcript |
| Chế độ | Tham số theo lượt đúng từng chế độ; `bypassPermissions` bật xác nhận |
| Đăng nhập | Trình duyệt (mặc định), device code (tuỳ chọn, kèm hướng dẫn bật), API key + `ephemeral`, `unauthorized` → auth-required, không đọc `account/read` trên tiến trình vừa đăng nhập |
| Resume | `thread/resume` bằng `threadId` đã lưu |
| Live (`DEVKIT_CODEX_LIVE=1`) | Một lượt thật trong jail thật + một approval thật |

**Giai đoạn 5 — Bridge + `main.go`.** `wails3 generate bindings -clean=true -ts -i`; test capability `auth:codex` chỉ UI.

**Giai đoạn 6 — Frontend.** Bảng mô tả vendor (thay ~10 chỗ hard-code `"claude"`: `bridge/agenthub.ts`,
`bridge/agentRuntime.ts`, `AgentHubView.tsx`, `AgentHubContent.tsx`, `AgentComposer.tsx`, `AgentRail.tsx`,
`dev/AgentHubTranscriptSection.tsx`); panel đăng nhập device code / API key; card Codex trong Settings;
dòng cảnh báo trong hộp xác nhận Full auto. Test `bun test`: bảng vendor, `isPermissionBypassChoice` với
id Codex, `authRetry` cho Codex.

**Giai đoạn 7 — Tài liệu + smoke.** Cập nhật `agent-hub.mdx`, `implementation-status.mdx`, AGENTS.md,
README. Checklist thủ công trên app desktop: đăng nhập; lượt Ask; Full auto + một lệnh prod qua MCP
(phải bị hỏi); resume sau khởi động lại.

## 10. Rủi ro và điểm chưa xác minh

| Rủi ro | Xử lý |
|---|---|
| Giao thức app-server đổi (Codex ship gần như hằng ngày, `[experimental]`) | Pin `0.147.0`; nâng version là thay đổi code, chạy lại golden + live |
| `untrusted` + `externalSandbox` không hỏi khi sửa file | Giai đoạn 0 đo; lệch thì báo |
| Device code tắt mặc định theo tài khoản (đã đo) | Trình duyệt là mặc định; device code kèm hướng dẫn bật (§6.1) |
| Lỗi token hết hạn giữa lượt có hình dạng khác | Live test; mặc định map mọi `unauthorized` sang auth-required |
| Hai tiến trình cùng dùng một refresh token | Không copy `auth.json`; home riêng = một token family riêng |
| Agent đọc được `auth.json` trong home riêng (ChatGPT mode) | Chấp nhận (research: không tránh được với managed login); API key mode dùng `ephemeral` |
| Hỏi hai lần ở chế độ Ask với tool MCP prod | Chấp nhận (§7.4) |
