# Agent runtime do DevKit quản lý (ACP adapter tải theo từng agent)

Ngày: 2026-09-13 · Trạng thái: Design · Phạm vi: Agent Hub, bắt đầu với Claude

## 1. Vấn đề

Hôm nay `main.go:241-265` chọn adapter **một lần lúc khởi động**: `exec.LookPath("claude-agent-acp")`,
không thấy thì rơi về `npx -y @agentclientprotocol/claude-agent-acp`. Hệ quả:

- App mở từ Finder/Dock kế thừa PATH tối thiểu của launchd, nên bản cài qua nvm/Homebrew thường
  không được thấy; không có login-shell env loader (§3 khảo sát).
- Fallback `npx` gần như luôn hỏng trong sandbox: npx cần `~/.npm` ghi được (`main.go:245-247`).
- Phiên bản trôi theo máy: đang đo trên 0.74.0 trong khi registry ACP đã là 0.76.0. DevKit dựa vào
  kênh private `_meta.claudeCode.options` (agent-hub.mdx:188-190), nên một bản mới có thể âm thầm
  bỏ cổng permission hoặc hook.
- Node không được cấp quyền rõ ràng: adapter chạy qua shebang `#!/usr/bin/env node` và PATH copy
  vào jail; không chỗ nào thêm thư mục node vào `ReadOnlyPaths`.
- Không có trạng thái "chưa cài": thiếu adapter chỉ lộ ra thành lỗi tạo phiên.

## 2. Mục tiêu và không phải mục tiêu

Mục tiêu

1. DevKit tự cài adapter ACP của từng agent vào thư mục do nó quản lý, đúng phiên bản đã khoá, giống
   JetBrains/Zed tải theo ACP registry.
2. Chạy adapter bằng **node tuyệt đối + entry tuyệt đối**, cấp quyền đọc tường minh; bỏ fallback npx.
3. Cài/cập nhật/gỡ/rollback không cần khởi động lại app; phiên đang chạy không bị ảnh hưởng.
4. Cho chọn file chạy Claude Code: bản đi kèm SDK (mặc định) hoặc bản cài trên máy.
5. Cùng một cơ chế cho agent sau (Codex, Gemini) — distribution `npx`, `binary`, `uvx` của registry.

Không phải mục tiêu (v1)

- Tải Node runtime. Thiếu Node ≥ 22 thì báo rõ kèm lệnh cài.
- Đăng nhập Claude trong DevKit. Theo Legal and compliance của Anthropic, bên thứ ba không được
  "offer Claude.ai login into their own applications"; người dùng tự `claude /login` hoặc đặt
  `ANTHROPIC_API_KEY`. DevKit tiếp tục chỉ cấp `~/.claude` như hôm nay.
- Sandbox trên Windows (vẫn `ErrUnsupportedPlatform`). Bộ cài vẫn viết trung lập OS (`node.exe`,
  `npm.cmd`) để không phải viết lại sau.
- Tự kiểm tra cập nhật nền theo lịch.

## 3. Khảo sát nhanh (điểm tựa trong code)

| Có sẵn | Ở đâu | Dùng thế nào |
|---|---|---|
| `ClaudeOptions{Command, Args}` + cấp `nodeModulesRoot` | `internal/agenthub/adapter_claude.go:33-176` | Đổi cách điền, không đổi hình dạng |
| Sandbox `Policy.ReadOnlyPaths`, bwrap tmpfs `$HOME` phải bind lại | `internal/sandbox` | Thêm thư mục runtime + thư mục node |
| Tìm file chạy + dò phiên bản (`search`, `probe`, `versionAtLeast`, `InstallCommand`) | `internal/codegraph/locate.go`, `toolchain.go` | Tách ra package dùng chung, thêm locator node/npm |
| Job dài single-flight, stream dòng output ra event | `codegraph.BuildAnalyzer` + `codegraph:analyzerBuildOutput` + `AnalyzerSetup.tsx` | Làm theo đúng mẫu |
| Setting 1 chuỗi qua `ui_state` | `bridge.ConsoleDirKey`, `DBConsoleDirectory` | Lưu lựa chọn file chạy Claude |
| Thư mục dữ liệu | `os.UserConfigDir()/DevKit` (`main.go:84-92`) | `DevKit/runtimes/` (tránh `DevKit/agents/` đang dùng cho socket) |
| Chưa có | downloader, checksum, giải nén, trạng thái "chưa cài", tab Settings Agents | Làm mới, ở mức tối thiểu |

## 4. Nguồn phiên bản

- **Phiên bản đã kiểm thử** (`TestedVersion`) được khoá trong code DevKit cho từng agent, ví dụ
  `claude-acp: 0.74.0`. Đây là mặc định khi cài. Nâng nó là một thay đổi code đi kèm chạy lại
  `TestClaudeACPMetaChannelStillWorks` và các test live của Agent Hub.
- **Phiên bản mới nhất** lấy từ `https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json`
  (mục `id: "claude-acp"`, `distribution.npx.package`), cache ở `DevKit/runtimes/registry.json` kèm
  `ETag`, chỉ tải khi mở tab Agents hoặc bấm kiểm tra. Mất mạng thì dùng cache, không có cache thì
  bỏ trống "mới nhất".
- UI cho cài bản mới nhất khi khác bản đã kiểm thử, kèm cảnh báo "chưa được DevKit kiểm thử".
- Registry ghi `license: "proprietary"` cho claude-acp (package.json của adapter ghi Apache-2.0; SDK
  bên trong là proprietary). UI hiện link `license_url` của mục registry trước khi cài lần đầu.

## 5. Bố cục trên đĩa

```
DevKit/runtimes/
  registry.json                 # cache registry ACP + ETag
  claude-acp/
    current.json                # phiên bản đang dùng
    0.74.0/
      package.json              # do npm tạo khi --prefix
      package-lock.json         # integrity sha512 của cả cây phụ thuộc
      node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js
      node_modules/@anthropic-ai/claude-agent-sdk-darwin-arm64/claude   # ~190 MB
      install.json              # manifest cài đặt (dưới đây)
    0.76.0/ ...                 # tối đa current + previous
    .staging-0.76.0-<rand>/     # chỉ tồn tại trong lúc cài
```

`install.json`:

```json
{
  "agent": "claude-acp",
  "version": "0.74.0",
  "package": "@agentclientprotocol/claude-agent-acp@0.74.0",
  "entry": "node_modules/@agentclientprotocol/claude-agent-acp/dist/index.js",
  "sdkVersion": "0.3.257",
  "bundledClaude": "node_modules/@anthropic-ai/claude-agent-sdk-darwin-arm64/claude",
  "node": { "path": "/Users/x/.nvm/versions/node/v24.14.1/bin/node", "version": "24.14.1" },
  "npmVersion": "11.6.0",
  "installedAt": "2026-09-13T10:00:00Z",
  "verified": { "handshake": true }
}
```

`current.json`: `{ "version": "0.74.0", "previous": "0.73.2" }`.

## 6. Luồng cài đặt

Chạy trong backend, **ngoài sandbox** (cần mạng và quyền ghi), single-flight theo agent
(`TryLock`, trả `ErrInstallAlreadyRunning`), stream từng dòng qua event.

1. **Kiểm tra điều kiện**: tìm `node` ≥ 22 và `npm` đi kèm cùng thư mục. Thứ tự dò: setting người
   dùng chọn → `$NVM_DIR`/`~/.nvm/versions/node/*/bin` (mới nhất trước) → `~/.volta/bin` →
   `~/.local/share/fnm` → `/opt/homebrew/bin`, `/usr/local/bin` → PATH của tiến trình. Thiếu thì
   dừng với `Requirement{Problem, InstallHint, Install}` (brew/nvm/winget theo OS).
2. **Staging**: tạo `.staging-<ver>-<rand>` rồi chạy
   `npm install --prefix <staging> @agentclientprotocol/claude-agent-acp@<ver>`
   `--ignore-scripts --no-audit --no-fund --omit=dev --no-update-notifier`.
   - Env tối giản: PATH chỉ gồm thư mục node, `npm_config_cache=DevKit/runtimes/.npm-cache`
     (không đụng `~/.npm` của người dùng), `HOME` thật để npm đọc `.npmrc` (proxy/registry nội bộ).
   - `--ignore-scripts` bắt buộc: bộ cài chạy ngoài sandbox, và cả adapter lẫn SDK không có
     `install`/`postinstall` (đã kiểm tra 0.74.0 và SDK 0.3.257). Binary Claude đến qua
     `optionalDependencies` theo `os`/`cpu`, không cần script.
3. **Xác minh tĩnh**: entry tồn tại; `node_modules/@agentclientprotocol/claude-agent-acp/package.json`
   đúng phiên bản yêu cầu; `package-lock.json` có mặt; file Claude đi kèm của đúng nền tảng tồn tại
   và thực thi được (trừ khi chọn bản trên máy).
4. **Xác minh chạy**: khởi động adapter **trong sandbox** với đúng policy như phiên thật, gửi ACP
   `initialize` với timeout 20s, rồi dừng. Chứng minh node + entry + quyền đọc đủ trước khi kích hoạt.
5. **Kích hoạt nguyên tử**: rename staging → `<ver>/`, ghi `install.json`, rồi ghi `current.json`
   qua tệp tạm + rename. Lỗi ở bất kỳ bước nào: xoá staging, `current.json` không đổi.
6. **Dọn dẹp**: giữ `current` và `previous`. Bản cũ hơn chỉ bị xoá lúc app khởi động (khi chắc chắn
   không phiên nào đang chạy trên nó). Staging mồ côi cũng xoá lúc khởi động.

Hủy: bridge call hủy được (context) → kill `npm`, xoá staging.

## 7. Khởi chạy phiên

Việc chọn lệnh chuyển từ lúc khởi động sang **lúc tạo/resume phiên**, qua một `RuntimeResolver`:

```go
type ResolvedRuntime struct {
    Command       string   // node tuyệt đối
    Args          []string // [entry tuyệt đối]
    ReadOnlyPaths []string // thư mục phiên bản runtime, prefix cài node (đã EvalSymlinks)
    Env           map[string]string // PATH=<dir node>, CLAUDE_CODE_EXECUTABLE nếu chọn bản trên máy
    Version       string
}
```

Thứ tự:

1. `DEVKIT_CLAUDE_ACP_COMMAND` (tuyệt đối, cho dev và CI) — giữ hành vi `nodeModulesRoot` cũ.
2. Bản đang dùng trong `current.json`.
3. Không có → `ErrAgentNotInstalled` (`agenthub.notInstalled`), **không** rơi về PATH hay npx.

Thay đổi trong adapter:

- `Command` là node, `Args` là entry; bỏ `AllowNpx` và nhánh `npx` trong `resolveCommand`.
- `buildSandbox` nhận `ReadOnlyPaths` và `Env` bổ sung từ resolver. Trên Linux thư mục runtime nằm
  dưới `$HOME` nên bắt buộc có trong `ReadOnlyPaths` (bwrap tmpfs `$HOME`).
- Chọn "Claude Code trên máy này": resolver đặt `CLAUDE_CODE_EXECUTABLE=<đường dẫn đã EvalSymlinks>`
  (adapter đọc biến này ở `acp-agent.js:319`) và thêm thư mục chứa file đó vào `ReadOnlyPaths`
  (`~/.local/bin/claude` là symlink tới `~/.local/share/claude/versions/<v>`).
- Phiên đang chạy giữ tiến trình của nó; cài bản mới chỉ ảnh hưởng phiên tạo sau. Resume phiên cũ
  dùng bản hiện hành (session file nằm ở `~/.claude`, không gắn với phiên bản adapter).

## 8. Bridge và sự kiện

| Method | Trả về | Ghi chú |
|---|---|---|
| `AgentRuntimeStatus(agent)` | `RuntimeStatus` | Không gọi mạng; `latest` từ cache |
| `AgentRuntimeCheckLatest(agent)` | `RuntimeStatus` | Tải lại registry (ETag) |
| `AgentRuntimeInstall(agent, version)` | `RuntimeStatus` | Hủy được; stream output |
| `AgentRuntimeActivate(agent, version)` | `RuntimeStatus` | Rollback về `previous` |
| `AgentRuntimeRemove(agent, version)` | `RuntimeStatus` | Từ chối gỡ bản đang dùng |
| `AgentRuntimeSetClaudeBinary(source)` | `RuntimeStatus` | `"bundled"` \| `"machine"`, lưu `ui_state` key `agenthub.claude.binary` |

```ts
type RuntimeStatus = {
  agent: "claude-acp";
  testedVersion: string;            // khoá trong DevKit
  latestVersion: string | null;     // từ registry cache
  current: InstalledRuntime | null;
  previous: InstalledRuntime | null;
  installing: boolean;
  requirements: Requirement[];      // node >= 22, npm
  claudeBinary: {
    source: "bundled" | "machine";
    machine: { path: string; version: string } | null; // dò `claude --version`
  };
  licenseUrl: string | null;
  diskBytes: number;                // tổng các phiên bản giữ lại
};
```

Sự kiện: `agenthub:runtimeInstallOutput` `{ agent, line }`, cùng quy ước với
`codegraph:analyzerBuildOutput`. Không có sự kiện phần trăm; promise của `AgentRuntimeInstall`
resolve khi xong.

Lỗi mới: `agenthub.notInstalled`, `agenthub.runtimeRequirement`, `agenthub.installFailed`
(kèm dòng cuối của npm, đã lọc token trong `.npmrc` nếu có).

## 9. Giao diện

**Settings → tab "Agents"** (thêm vào `SettingsTab` trong `SettingsView.tsx`), mỗi agent một nhóm:

- Dòng trạng thái: `Claude Agent 0.74.0 · đã kiểm thử` / `Chưa cài` / `Có 0.76.0 (chưa kiểm thử)`.
- Yêu cầu: `Node 24.14.1 ✓ (~/.nvm/…)` hoặc lỗi kèm lệnh cài có nút Copy.
- Nút: Cài đặt / Cập nhật / Cài lại / Quay về bản trước; menu phụ Gỡ bản cũ, Mở thư mục.
- Log cài đặt thu gọn được, theo mẫu `AnalyzerSetup.tsx`.
- File chạy Claude Code: radio "Đi kèm adapter (khuyên dùng)" / "Claude Code trên máy này
  (`~/.local/bin/claude` · 2.1.269)"; lựa chọn thứ hai hiện ghi chú có thể lệch phiên bản SDK.
- Đăng nhập: một dòng chỉ dẫn, không có form — "Đăng nhập bằng `claude` trong terminal hoặc đặt
  `ANTHROPIC_API_KEY`. DevKit không bao giờ hỏi mật khẩu Claude của bạn."
- Dung lượng đang dùng.

**Agent Hub**

- `AgentNewSession`: khi `current == null` thay form tạo phiên bằng empty state "Claude chưa được
  cài" với nút "Cài Claude Agent" chạy cài ngay tại chỗ (cùng component log), và link sang Settings.
- `AgentSessionError`: nhận thêm `cause: "notInstalled" | "requirement"` để hiện đúng hành động.

## 10. Bảo mật

- Bộ cài chạy ngoài sandbox nên luôn `--ignore-scripts`, không bao giờ `npx`, không chạy code của
  gói trước bước xác minh trong sandbox.
- Toàn vẹn: npm kiểm sha512 từ registry cho từng tarball và ghi vào `package-lock.json`; DevKit lưu
  lockfile để truy vết. Phiên bản top-level luôn khoá chính xác; phụ thuộc bắc cầu của adapter hầu
  hết đã khoá (`@anthropic-ai/claude-agent-sdk` 0.3.257 cố định).
- Agent chỉ đọc được thư mục runtime (`ReadOnlyPaths`), không ghi được, nên không tự sửa adapter.
- Không mở thêm quyền ghi nào ngoài `~/.claude` và `~/.claude.json` như hiện nay.
- Thư mục cache npm riêng để lỗi cài không làm bẩn `~/.npm` của người dùng.

## 11. Đa nền tảng

- macOS: Seatbelt cần cấp đọc cả dạng gốc lẫn dạng đã resolve symlink (đã có `writeSBPLPath`).
- Linux: thư mục `~/.config/DevKit/runtimes/...` và prefix node dưới `$HOME` phải nằm trong
  `ReadOnlyPaths`, nếu không bwrap che mất bằng tmpfs.
- Windows: bộ cài dùng `node.exe`/`npm.cmd` qua `programName`; khởi chạy phiên vẫn báo sandbox không
  hỗ trợ như hiện nay.
- Nền tảng binary Claude đi theo `os`/`cpu` của npm; Linux musl có gói `-musl` riêng, npm tự chọn.

## 12. Kiểm thử

Unit

- Chọn phiên bản: tested vs latest vs cache hỏng/mất mạng.
- Ghi `current.json` nguyên tử; staging bị xoá khi lỗi; GC giữ current + previous.
- Locator node: ưu tiên setting, nvm nhiều bản chọn bản mới nhất ≥ 22, bỏ bản < 22.
- Resolver: override env → current → `ErrAgentNotInstalled`; không bao giờ trả npx.
- `buildSandbox` có đủ thư mục runtime, node, và file Claude trên máy khi được chọn.

Live (gated `DEVKIT_AGENT_INSTALL_TEST=1`, cần mạng + Node)

- Cài thật `claude-agent-acp@<TestedVersion>` vào thư mục tạm, xác minh handshake trong sandbox.
- `TestClaudeACPMetaChannelStillWorks` chạy trên bản đã cài qua resolver, không qua PATH.
- Cài bản thứ hai rồi Activate/rollback; phiên đang chạy không bị ngắt.

## 13. Kế hoạch triển khai

1. Tách locator/requirement từ `codegraph` sang `internal/toolchain`, thêm node/npm/claude.
2. `internal/agenthub/runtime`: registry cache, `Installer` (staging, npm, xác minh, kích hoạt, GC),
   `Resolver`. Unit test.
3. Adapter dùng `Resolver` lúc tạo/resume phiên; bỏ `AllowNpx`; lỗi `notInstalled`. Cập nhật
   `main.go` và test adapter.
4. Bridge + event + bindings.
5. Settings tab Agents + empty state trong Agent Hub.
6. Cập nhật `docs/dev-kit/agent-hub.mdx` và `sandbox.mdx` (bỏ mô tả npx, thêm runtime), ADR về quy
   trình nâng `TestedVersion`.

## 14. Quyết định (2026-09-13)

1. **Phiên bản mặc định**: bản DevKit đã kiểm thử (`TestedVersion`). Bản mới nhất của registry chỉ
   hiện như bản cập nhật, kèm cảnh báo chưa kiểm thử.
2. **File chạy Claude mặc định**: bản đi kèm adapter (khớp SDK). "Claude Code trên máy này" là tuỳ
   chọn trong Settings.
3. **Thiếu Node ≥ 22**: chỉ hướng dẫn cài, có lệnh để copy. v1 không tải Node.
4. **Adapter cài global trên PATH**: bỏ qua. Dev/CI dùng `DEVKIT_CLAUDE_ACP_COMMAND` để trỏ tới bản
   khác.
