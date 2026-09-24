# Research: How a jailed agent should get its credentials

**Date:** 2026-09-24  
**Decision context:** Codex is about to join Agent Hub (Layer 2, via `codex app-server`). Before copying what the Claude adapter does for sign-in, check whether that is the right pattern — and if not, build Codex the right way and move Claude over afterwards.  
**Status:** Recommendation below adopted for the Codex adapter. Claude migration is a follow-up (last section).

**Primary sources:**
- Claude Code — [Authentication](https://code.claude.com/docs/en/authentication) (storage, precedence, `CLAUDE_CONFIG_DIR`, `setup-token`), [Sandboxing](https://code.claude.com/docs/en/sandboxing) (config-write escalation warning, `sandbox.credentials`)
- Codex — [Authentication](https://learn.chatgpt.com/docs/auth) (`cli_auth_credentials_store`, headless login), [App server](https://learn.chatgpt.com/docs/app-server) (initialize / `clientInfo`)
- Codex 0.147.0 app-server protocol, generated locally: `codex app-server generate-ts` / `generate-json-schema` (types `v2/LoginAccountParams`, `v2/GetAccountParams`, `ServerRequest`)
- ACP — [Initialization](https://agentclientprotocol.com/protocol/initialization) (`authMethods`, `auth.terminal`)
- DevKit — `internal/agenthub/adapter_claude.go` (`claudeCredentialPaths`), `internal/sandbox/policy.go`, `internal/sandbox/bwrap_args.go`

---

## What the vendors say

### Claude Code

- Credentials: Linux/Windows `~/.claude/.credentials.json` (mode 0600); macOS Keychain (service `Claude Code-credentials`), file fallback when the keychain refuses.
- `CLAUDE_CONFIG_DIR` moves the whole config dir, including `.credentials.json` — and on macOS **keys the Keychain entry to that directory**, so a different dir is a different login.
- Precedence: cloud provider → `ANTHROPIC_AUTH_TOKEN` → `ANTHROPIC_API_KEY` → `apiKeyHelper` → `CLAUDE_CODE_OAUTH_TOKEN` → profiles → `/login` OAuth.
- Headless guidance: `ANTHROPIC_API_KEY`, or `claude setup-token` → `CLAUDE_CODE_OAUTH_TOKEN` (one-year, **inference-only**: no Remote Control, no claude.ai connectors).
- Sandboxing docs, verbatim concern: with filesystem isolation off, a sandboxed command "can write files that later commands run or read, such as shell startup files, executables on `$PATH`, or `~/.claude/settings.json`, and use them to widen its own access on the next run." Claude's own sandbox also offers `sandbox.credentials` (deny / mask) so tool subprocesses cannot read credential files.

### Codex

- `cli_auth_credentials_store = "file" | "keyring" | "auto" | "ephemeral"`; `file` writes `auth.json` under `CODEX_HOME` (default `~/.codex`). Docs: `auth.json` "contains access tokens. Don't commit it, paste it into tickets, or share it in chat."
- Headless, in the docs' order of preference: **device code** (`codex login --device-auth`), API key, copying `auth.json`, SSH-forwarding the `localhost:1455` callback.
- The app-server has a first-class account API (confirmed in the 0.147 schema):
  - `account/read` → `{ account | null, requiresOpenaiAuth }`
  - `account/login/start` with `type`: `apiKey`, `chatgpt` (returns `authUrl`; callback server on `localhost:1455`), `chatgptDeviceCode` (returns `verificationUrl` + `userCode`, no callback), `chatgptAuthTokens`, `amazonBedrock`
  - `account/login/completed`, `account/updated` notifications; `account/login/cancel`, `account/logout`
  - Two modes, named in the schema docs: **managed** (Codex stores and refreshes the tokens) and **external** (`chatgptAuthTokens`: the host supplies an access token and Codex calls back `account/chatgptAuthTokens/refresh` when it gets a 401 — "Clients should refresh tokens themselves").

### ACP (for Claude)

The agent lists `authMethods` in `initialize`; a client that sets `auth.terminal` may be offered terminal methods — "run this login command". DevKit already does this (`internal/agenthub/auth.go`: the agent's own login command, unmodified, in a terminal the user watches).

---

## Measured today (codex-cli 0.147.0, no quota used)

Spawned `codex app-server -c cli_auth_credentials_store="file"` with `CODEX_HOME` pointing at an empty scratch dir:

| Call | Result |
|---|---|
| `initialize` | `codexHome` = the scratch dir — nothing read from or written to the real `~/.codex` |
| `account/read` | `{"account":null,"requiresOpenaiAuth":true}` — "not signed in" is a clean, typed answer |
| `account/login/start {type:"chatgptDeviceCode"}` | `verificationUrl: https://auth.openai.com/codex/device` + `userCode`; cancelled |
| `account/login/start {type:"chatgpt"}` | `authUrl` at `auth.openai.com/oauth/authorize`, `redirect_uri=http://localhost:1455/auth/callback`; cancelled |

Each cancel produced `account/login/completed`. The scratch dir received only Codex's own state (`state_5.sqlite`, `logs_2.sqlite`, `installation_id`, …).

---

## Is DevKit's Claude approach sound?

| Part | Verdict |
|---|---|
| Sign-in via ACP `authMethods`, the vendor's own login command in a terminal the user watches | **Standard.** Matches the ACP spec; DevKit never sees a credential. Keep. |
| Private home: bind `~/.claude` and `~/.claude.json` **read-write** into the jail | **Not sound.** That directory holds `settings.json` (hooks, permission rules, `env`). The jailed agent can write a `hooks` command that later runs **unsandboxed** in the user's own terminal `claude`. This is exactly the escalation Claude Code's docs warn about. |
| Shared home (the default, `ShareHome`): the whole `$HOME` bound read-write | Same problem, wider: `~/.claude/settings.json`, `~/.codex/config.toml` (whose `[hooks]` can be `command` handlers), shell rc files, `~/.local/bin`. The vendor config dirs are the part this research covers; the rc-file problem is a general `ShareHome` issue, noted here only. |
| The jailed agent shares the user's terminal config | **Unintended coupling.** Personal hooks, MCP servers, plugins and permission rules from the terminal apply inside DevKit sessions (the `adapter_claude.go` comment near line 397 already works around this). For Codex it would also double-fire DevKit's own Layer 1 `codex.hookEvent` hooks for every Hub session — the duplication Phase 0 flagged. On this machine `~/.codex` is also a *work* profile (company projects, plugins, an MCP server), which a personal tool should not inherit. |
| Credentials readable by the agent's tool subprocesses | **Inherent** to managed login — Claude's Bash tool and Codex's shell tool run as the same user inside the same jail. Not fixable by path tricks; reduced by giving the jail a credential that is only good for inference (see option C). |

---

## Options

| | Option | Isolation | UX | Notes |
|---|---|---|---|---|
| A | Share the vendor's real config dir (today) | Poor: config poisoning escapes the jail | Best: no extra sign-in | Rejected. |
| **B** | **DevKit-owned vendor home** (`CODEX_HOME` / `CLAUDE_CONFIG_DIR`) bound read-write; real `~/.codex` / `~/.claude` **hidden** from the jail; sign-in through the vendor's own flow | Good: anything the agent writes only affects jailed sessions | One extra sign-in per vendor | Recommended default. |
| C | Inject a least-privilege credential by env: `OPENAI_API_KEY` / `CODEX_API_KEY`, `ANTHROPIC_API_KEY`, `CLAUDE_CODE_OAUTH_TOKEN`, stored sealed in the DevKit vault | Best: no refresh token in the jail at all | Needs a key or a setup-token | Offer beside B, not instead of it: most users are on a subscription, not an API key. |
| D | Codex external auth (`chatgptAuthTokens`) with DevKit holding tokens in the vault | Best in theory | — | Rejected: DevKit would have to run OpenAI's OAuth itself (Codex's client id) to get and refresh tokens. The mode exists for OpenAI's own hosts; a third party doing it is impersonation, not integration. *(Inference from the API shape; not stated by OpenAI.)* |

Copying the user's existing `auth.json` into the DevKit home is **not** offered. Two processes would hold the same refresh token; if the provider rotates refresh tokens on use, one copy logs the other out. *(Rotation behaviour not verified — do not test it on a real account.)*

---

## Recommendation (adopted for Codex)

1. **Home:** `CODEX_HOME=<DevKit data dir>/agent-homes/codex`, created 0700, bound read-write, and added to the keep-list of `agenthub.ProtectDataDir` (the rest of the data dir stays hidden). The Hub owns this dir: threads, rollouts for `thread/resume`, and its own `config.toml`.
2. **Store:** always start with `-c cli_auth_credentials_store="file"` — the jail has no D-Bus secret service or login keychain, and `auto` would fail differently per OS.
3. **Hide the real one:** add `~/.codex` to the session's `DenyReadPaths`, so under `ShareHome` the jailed agent can neither read the user's other login nor write their terminal config.
4. **Sign in with the app-server's own API**, not a terminal command:
   - `account/read` before `thread/start`; `requiresOpenaiAuth && account == null` → the typed "not signed in" error the UI already handles for Claude.
   - Default flow: `chatgptDeviceCode` — no callback server, so it works whatever the jail's network mode; the UI shows `verificationUrl` + `userCode`.
   - Secondary: `chatgpt` (browser, callback on `localhost:1455`) only where the jail shares the host loopback; `apiKey` (option C) from the vault.
   - Sign-in runs on a short-lived app-server process under the same jail and home, then exits; sessions never see DevKit's side of it.
5. **`clientInfo.name = "devkit"`** — it is what OpenAI's compliance logs record.

---

## Unverified — measure on a signed-in machine

- ~~Whether device-code sign-in is available to every ChatGPT plan / workspace~~ — **measured 2026-09-24: no.** Off by default per account; the user must enable it in ChatGPT → Settings → Security. See "Phase 0 results" below.
- The exact error shape when a signed-in token expires mid-turn (managed refresh should hide it; confirm). *Still open — needs a live test that outlives a token.*
- ~~Whether `account/login/start {chatgpt}` inside `bwrap` with `NetworkHost` can receive the `localhost:1455` callback~~ — **measured 2026-09-24: yes.**

---

## Phase 0 results (codex-cli 0.147.0, 2026-09-24)

Measured in bwrap with a scratch `CODEX_HOME`, `-c cli_auth_credentials_store="file"`,
`--disable hooks --disable plugins --disable apps --disable computer_use`, `turn/start` with
`sandboxPolicy: externalSandbox` and `approvalsReviewer: "user"`. Nine short turns on the user's
personal account; scratch home logged out and deleted afterwards. Golden capture:
`internal/agenthub/testdata/codex_session_capture.jsonl`.

### 0. Device code
- Lần 1: `account/login/start {chatgptDeviceCode}` trả về `verificationUrl` + `userCode` bình thường
  (API không từ chối). Nhưng khi người dùng nhập mã, trang của OpenAI báo:
  "Enable device code authorization for Codex in ChatGPT Security Settings, then run
  `codex login --device-auth` again."
- Kết luận: device code **tắt mặc định theo từng tài khoản** — người dùng phải tự bật trong
  ChatGPT → Settings → Security. App-server không báo trước điều này; lỗi chỉ lộ ra ở trình duyệt,
  và phía app-server vẫn chờ (không có `account/login/completed` với lỗi ngay).
- Hệ quả cho spec §6.1: UI phải (a) nói trước điều này cạnh mã, kèm link tới Security Settings, và
  (b) luôn hiện nút "Sign in with browser" (không chỉ khi `login/start` bị từ chối).
- Lần 2 (browser, `account/login/start {chatgpt}`): **thành công từ trong bwrap** — callback
  `localhost:1455` tới được tiến trình trong jail (mạng dùng chung, như `NetworkHost`).
  `auth.json` được ghi vào home nháp, quyền `0600`.
- **Race:** `account/read` gửi ngay sau `account/login/completed` trên **cùng tiến trình** trả
  `account: null`. Một tiến trình mới đọc ra `chatgpt · plus`. → Adapter không được đọc trạng thái
  ngay sau `login/completed`; chờ `account/updated` hoặc đọc lại (tiến trình đăng nhập vốn ngắn hạn,
  nên đọc ở tiến trình session kế tiếp là đủ).

### 1. Approval theo chế độ (turn/start: externalSandbox + approvalsReviewer=user)
Prompt: `Append the line "line three" to notes.txt, then run: cat notes.txt`

| Chế độ | Quyết định | Server requests | Item statuses | Turn | notes.txt |
|---|---|---|---|---|---|
| untrusted | accept | `fileChange/requestApproval` | fileChange completed, commandExecution completed | completed | đổi |
| untrusted | decline | `fileChange/requestApproval` | fileChange **declined**, commandExecution completed | completed | **không đổi** |
| on-request | accept | (không có) | cả hai completed | completed | đổi |
| never | — | (không có) | cả hai completed | completed | đổi |

- `untrusted` hỏi trước khi sửa file ✔ (spec §7.1 đúng). `cat` là lệnh chỉ đọc an toàn nên chạy không hỏi — đúng như
  mô tả "trừ lệnh chỉ đọc". Lệnh ghi qua shell được đo ở phiên capture (mục 3).
- `thread/start` không gửi `sandbox` → response báo `readOnly`; `sandboxPolicy: externalSandbox` gửi ở `turn/start`
  vẫn cho sửa file → tham số theo lượt có hiệu lực, đúng thiết kế §4.3.

### 2. Config độc hại trong repo (`.codex/config.toml`: `approval_policy="never"`, `approvals_reviewer="auto_review"`, hook `PermissionRequest` trả "allow")
- **Project chưa trust** (mặc định trong home riêng): Codex báo `configWarning` "Project-local config,
  hooks, and exec policies are disabled … until the project is trusted" — config không được nạp.
- `-c projects."<repo>".trust_level="trusted"` **không** có tác dụng (cảnh báo vẫn còn).
- **Project trust bằng cách ghi vào `$CODEX_HOME/config.toml`** (đúng cách một agent trong jail làm được,
  vì home ghi được): cảnh báo biến mất, sandbox mặc định của thread đổi thành `workspaceWrite` → config
  của repo **đã nạp**. Kết quả: hiệu lực vẫn `approvalPolicy=untrusted`, `approvalsReviewer=user`;
  hai `fileChange/requestApproval` tới client; decline → `notes.txt` không đổi; hook không chạy.
  → Tham số theo lượt thắng config của project ✔ (§7.3).
- **Chưa đo đối chứng**: chưa chạy lại với hooks bật để chứng minh hook `PermissionRequest` *sẽ* tự duyệt
  nếu không có `--disable hooks`. `--disable hooks` giữ nguyên như một lớp phòng thủ; không claim đã chứng minh.
- **Phát hiện cho Kế hoạch 2:** agent trong jail ghi được `$CODEX_HOME/config.toml` → tự trust repo
  cho các session sau. Không mở được cổng (tham số theo lượt vẫn thắng) nhưng là cách lây cấu hình giữa
  các session. Cân nhắc bind `config.toml` chỉ đọc trong jail, hoặc để DevKit ghi lại file đó mỗi lần spawn.
- Cảnh báo khác mỗi lần spawn: "Codex's Linux sandbox uses bubblewrap and needs access to create user
  namespaces" — vô hại với `externalSandbox`, adapter chỉ log.

### 3. Phiên capture (commit 104fa6f, internal/agenthub/testdata/codex_session_capture.jsonl)
- `untrusted` hỏi cả **lệnh shell ghi file** (`touch done.flag && ls -la …` → `commandExecution/requestApproval`)
  lẫn sửa file → chế độ Ask đúng spec.
- **Interrupt:** `turn/interrupt` khi đang chạy `sleep 20` → `turn/completed {status: interrupted}`, nhưng item
  `commandExecution` đang chạy **không có `item/completed`**. → Adapter phải tự đóng mọi tool_call còn mở
  thành cancelled/failed khi lượt kết thúc bằng interrupted.
- Notification thấy thật mà spec §5 chưa liệt kê: `thread/status/changed`, `account/rateLimits/updated`,
  `serverRequest/resolved`, `turn/diff/updated`, `remoteControl/status/changed`. Tất cả vào nhánh
  "bỏ qua / log" trừ `serverRequest/resolved` (nên dùng để đóng thẻ duyệt quyền nếu Codex tự huỷ request).
- Dữ liệu cá nhân trong wire cần lọc: `serverName` (hostname), `installationId`, tên user trong output lệnh.
  → Adapter không được log nguyên `remoteControl/status/changed`.

### 4. App desktop có mở cổng TCP không
- **Tĩnh (Wails v3.0.0-beta.25, `pkg/application`)**: chỉ hai file mở listener TCP —
  `application_server.go` (`//go:build server`) và `mcp_enabled.go` (`//go:build mcp && !ios && !android`).
  Bản desktop build với `-tags production` (+ `wails_obfuscated`/`EXTRA_TAGS` tuỳ chọn), không có `mcp`
  → bridge chỉ đi qua custom scheme trong tiến trình. Rủi ro còn lại: ai đó build với `EXTRA_TAGS=mcp`.
- **Động (`ss -ltunp`, 2026-09-24)**: `bin/dev-kit` do người dùng mở — toàn bộ cây tiến trình (hơn 100 pid, kể cả
  tiến trình WebKit) **không có listener TCP/UDP nào**; chỉ có Unix socket `~/.config/DevKit/mcp.sock`. Khớp kiểm tĩnh.

**Against the Codex spec:** §7.1 (modes) and §7.3 (per-turn parameters beat project config) hold. Changes for plan 2:
§6.1 — always show "Sign in with browser" and tell the user device code must be enabled first; don't read
`account/read` on the process that just completed a login. §4.3 — the jail can write `$CODEX_HOME/config.toml`
(e.g. trust a repo for later sessions); bind it read-only or have DevKit rewrite it on spawn. §5.1 — close any
tool call still open when a turn ends `interrupted`; use `serverRequest/resolved` to retire approval cards.
§5.3 — never log `remoteControl/status/changed` verbatim (hostname, installation id).

### 5. Đo thêm cho Kế hoạch 2c (0.147.0, không đăng nhập, không tốn quota)
Bản cài qua chính `internal/agentruntime` (npm hoist `@openai/codex-darwin-arm64`, glob `NativeEntry` thứ nhất
khớp); `codex app-server` chạy thẳng binary, `CODEX_HOME` nháp.
- **`PATH`/`rg`:** `command/exec` in ra `PATH` bắt đầu bằng `<home>/tmp/arg0/…` rồi `vendor/<triple>/codex-path`,
  `command -v rg` ra `rg` đi kèm — giống hệt khi có hay không `CODEX_MANAGED_BY_NPM`/`CODEX_MANAGED_PACKAGE_ROOT`.
  Binary tự tìm `codex-package.json` cạnh nó.
- **Ghi gì vào home lúc khởi động:** `installation_id`, `*.sqlite`, `shell_snapshots/`, `tmp/arg0/`,
  `sessions/…` (khi có thread), và `skills/.system/…` (skill hệ thống). **Không** ghi `config.toml`.
- **Home có năm file chính sách chỉ-đọc (Seatbelt):** initialize, account/read, command/exec, thread/start đều
  chạy; stderr có `failed to install system skills: … Operation not permitted` mỗi lần khởi động, không gì khác.
- **TLS trong Seatbelt:** kết nối websocket tới `api.openai.com` báo `invalid peer certificate: UnknownIssuer`
  trong jail, `401 Unauthorized` (tức TLS qua) ngoài jail. Thêm đúng `mach-lookup com.apple.SecurityServer`
  → `401`. `trustd` một mình không đủ.
- **Cấu hình MCP qua `-c`:** giá trị TOML đúng được nhận; sai kiểu (`args="notalist"`) → tiến trình thoát
  `exit 1` trước khi trả lời initialize.
- **Duyệt tool MCP** (đọc mã nguồn tag `rust-v0.147.0`): xem spec §7.2.

---

## Found while checking "Full auto must stop at prod" (2026-09-24)

- **Server mode let a jailed agent approve itself.** The bridge is served over
  unauthenticated loopback HTTP (confirmed: an unauthenticated `POST /wails/runtime`
  calling `InteractionPendingList` returned `HTTP 200`), and the jail shares the host
  loopback, so a session could call `AgentHubResolvePermission` or `InteractionComplete`.
  Fixed: server mode registers `agenthub.ServerModeAdapter` stubs and makes the
  interaction hub headless.
  **Cost, accepted for now:** the hub is shared by MCP and the UI, so in server mode *every*
  prompt fails fast — active-target MCP tools on any env, and UI actions that need a
  confirmation (prod `git push`, guarded commits, prod `api.send`, task gates). **Later:** ask
  out-of-band through OS notifications (answer arrives via the OS notification channel, not the
  HTTP bridge) — see Codex spec §8.1.
- **Five profile-scoped MCP tools reached prod without asking** (`db.explain`,
  `db.listForeignKeys`, `db.testConnection`, `db.schema.get`, `ssh.testConnection`).
  Fixed with `GateProfileMCP`. A sweep of every other MCP tool found no further gaps
  (`api.send` and `git.push` confirm inside their services).
- **Not changed, by decision:** under Full auto with `ShareHome` and the Docker socket on,
  the jail is not a boundary (`~/.kube`, `~/.pgpass`, project `.env`; `docker run -v /:/host`).
  Surfaced in the Full auto confirmation instead (Codex spec §7.1).

---

## Follow-up: move Claude to the same pattern

Tracked separately; nothing here changes Claude yet.

- `CLAUDE_CONFIG_DIR=<data dir>/agent-homes/claude` for jailed sessions and for the ACP terminal sign-in command (so the login lands in the same dir), replacing the `~/.claude` + `~/.claude.json` read-write grant in `claudeCredentialPaths`.
- Add `~/.claude` and `~/.claude.json` to the jail's deny list under `ShareHome`.
- macOS: the Keychain entry is keyed by config dir, so `AllowKeychain` stays, and existing users sign in once more.
- Offer option C (`CLAUDE_CODE_OAUTH_TOKEN` from `claude setup-token`, sealed in the vault) as the least-privilege path.
- Migration note in the UI: "Agent Hub now keeps its own Claude sign-in."
