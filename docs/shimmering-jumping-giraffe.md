# Agent Hub — nâng adapter 0.78, checkpoint file và rewind hội thoại

## Tiến độ

| Phase | Trạng thái |
|---|---|
| A — Nâng adapter 0.78 + spike | **Xong** (2026-09-17) |
| B — Tiền đề trong code hiện có | **Xong** (2026-09-17) |
| C — Engine `internal/checkpoint` | Chưa làm |
| D — Chụp mỗi lượt + report + UI "file đã đổi" | Chưa làm |
| E — Khôi phục code | Chưa làm |
| F — Rewind hội thoại + code | Chưa làm |
| G — Settings + tài liệu | Chưa làm |

**Đã có sau A:** pin `0.78.0`; test live đều qua (install, `_meta` channel, integration, resume,
auth); builder AskUserQuestion gửi cả lựa chọn lẫn `question_N_custom`; `ToolCall.Name` (top-level
`name` hoặc `_meta.claudeCode.toolName`) tách compaction khỏi subagent; `_auth/status_update` nhận
im lặng; test `agentruntime` đọc `TestedVersion` thay vì chuỗi cứng; `DEVKIT_RUNTIMES_ROOT` cho
`TestClaudeACPAuthMethodsLive`. Spike S1–S7 ghi trong `docs/dev-kit/agent-hub.mdx` (mục "Nâng
0.74.0 → 0.78.0"), test `internal/agenthub/adapter_claude_fork_spike_test.go`:
S1 fork tại `msg_…` từ process mới rồi `session/load` — chạy, chỉ nhớ tới message đó, session gốc
còn dùng được; S2 mode **mất** sau fork → phải áp lại; S3 fork lượt bị kill giữa tool — chạy;
S4 chunk subagent tới dưới `params.sessionId` của subagent với `msg_…` riêng; S5 fork qua
`/compact` — chạy (không cần chặn); S6 `session/delete` session chưa load — chạy; S7 report ở
`update._meta.jetbrains.air.agentFileChangeReport` của `session_info_update`, cùng lúc prompt
response, **không** có file do Bash tạo, lượt huỷ → `unavailable/cancelled`.

**Đã có sau B:** `Invoke` sinh một `promptMessageID` sau claim — echo mang nó, `TurnRecord.PromptMessageID`
lưu nó; **luôn** echo kể cả prompt chỉ ảnh/file (`SessionUpdate.Attachments`, wire `_devkit.attachments`,
chip trong bong bóng prompt); adapter gắn `_devkit.origin` khi `params.sessionId` khác session của
runtime (`SessionUpdate.Origin`, `TranscriptItem.origin`); migration m22 (`agent_turns.prompt_message_id`,
bảng `agent_checkpoints` có cột `detail` JSON cho dữ liệu riêng từng kind); store
`FindPromptEntry`, `UpdateSessionACPID`, `DeleteSessions` xoá cả checkpoints.

## Context

DevKit chưa có cách hoàn tác việc agent đã làm. Research (phiên 2026-09-16) chốt:

- **Không vendor nào cho client rewind file qua ACP** — kể cả `claude-agent-acp` 0.78.0 (mới nhất). Issue
  upstream #1048 chỉ xin lùi *hội thoại* và để file cho client (như Zed). Checkpoint gốc của Claude
  mù với Bash/subagent (đã thấy thật: file sửa bằng script Bash không có trong snapshot nào).
- Jail chỉ cho agent ghi vào `project_path` (+ `~/.claude`) → snapshot cả cây ở ranh giới lượt bắt
  được mọi thay đổi. Đo trên repo này: snapshot đầu 0.86s, sau đó 0.04s, diff 27ms, shadow repo 9.2MB.
- 0.78 có `session/fork` tại một messageId (`_meta.jetbrains.air.fork`) và `agentFileChangeReport`
  (danh sách file đổi mỗi lượt, lấy từ checkpoint native bằng `rewindFiles(dryRun)`).

**Quyết định của người dùng:** (1) nâng lên 0.78; (2) lùi hội thoại qua `session/fork` nhưng **đi
cùng** rewind của DevKit (code + transcript) thành một tính năng; (3) checkpoint file do DevKit tự làm
bằng shadow git, **luôn bỏ qua** thư mục phụ thuộc như `node_modules` kể cả khi `.gitignore` thiếu;
(4) dùng `agentFileChangeReport` của vendor.

---

## Phase A — Nâng adapter 0.78 + spike

- `internal/agentruntime/agentruntime.go:54` `TestedVersion` → `0.78.0`; kiểm lại "không có install
  script" (comment `manager.go:177`, SDK nay 0.3.270, Claude Code 2.1.270).
- Chạy bộ live theo công thức `docs/dev-kit/agent-hub.mdx` (~438): `TestAgentRuntimeLiveInstall`,
  `TestClaudeACPMetaChannelStillWorks` (hỏng → không nâng), `TestClaudeACPIntegration`,
  `TestClaudeACPResumeAcrossProcesses`, `TestClaudeACPAuthMethodsLive`, `TestClaudeACPCapture`
  (chụp lại `testdata/claude_session_capture.jsonl`, đếm lại số kind "sixteen").
- **Thay đổi hành vi phải xử lý:**
  - AskUserQuestion: 0.78 giữ lựa chọn và coi text "Other" là ghi chú (multi-select: nối thêm).
    `buildElicitationContent` (`frontend/src/modules/agenthub/elicitation.ts`) gửi **cả** `question_N`
    lẫn `question_N_custom` khi có cả hai; chỉ custom khi không chọn gì. Cập nhật test + FR-13.
  - Compaction thành `tool_call` "Compact conversation" (kind think, không messageId) — kiểm render
    trong turn và `/compact` của composer.
  - `_auth/status_update`: nhận im lặng trong `claudeConnHandler`, không log là unsupported.
- Cập nhật mọi comment/tài liệu "đo trên 0.74" sau khi đo lại (danh sách đầy đủ từ khảo sát:
  `adapter_claude.go`, `service.go:118`, `update.go:25`, `types_v1.go:84/109`, `transcript.go:526`,
  `auth_test.go:20`, `update_test.go`, `agent-hub.mdx`…); sửa luôn mâu thuẫn "fifteen/sixteen",
  "2→13 / 7→20".
- **Spike (test env-gated `DEVKIT_AGENT_FORK_SPIKE=1`, cùng kiểu `adapter_claude_runtime_live_test.go`)**:
  - **S1** fork khi session nguồn chưa load trong process, rồi `session/load` id mới cùng process.
  - **S2** mode/model/effort sau fork+load và sau `session/new`.
  - **S3** fork tại assistant message cuối của một lượt bị Cancel (kill) giữa tool, rồi prompt tiếp.
  - **S4** subagent chunk có marker gì; fork bằng id của subagent ra sao.
  - **S5** fork tới message trước một lần compaction.
  - **S6** `session/delete` session không load trong process.
  - **S7** report nằm ở `update._meta` hay `params._meta`, thứ tự so với prompt response, khi cancel,
    khi chỉ sửa bằng Bash.
  - S1 và S3 phải qua trước Phase F.

## Phase B — Tiền đề trong code hiện có

- `service.go` `Invoke`: sinh **một** `promptMessageID` đầu hàm; **luôn** echo prompt, kể cả chỉ có
  ảnh/attachment (text rỗng + `_meta.devkit {attachments, images}`; frontend hiện chip). Mọi lượt có
  khoá ổn định live lẫn reload.
- Lưu marker nguồn subagent vào payload chunk (field theo S4) để chọn fork point không trúng subagent.
- Migration **m22** (`internal/storage/sqlite.go`, dùng `addColumnIfNotExists`):
  - `agent_turns.prompt_message_id`;
  - `agent_checkpoints(id, session_id FK, project_key, prompt_message_id, kind turn|rewind_safety|restore_safety,
    before_tree, after_tree, status, outcome, changed JSON, skipped JSON, agent_report JSON,
    overlap JSON, created_at, finished_at)` + index `(session_id, created_at)`, `(session_id, prompt_message_id)`.
- `store.go`: `DeleteSessions` xoá cả checkpoints (PruneClosedSessions hưởng theo); query tìm dòng
  prompt theo `json_extract(payload,'$.messageId')`; writer riêng cho `acp_session_id` (theo
  `updateSessionColumn`, không whole-row write).

## Phase C — Engine `internal/checkpoint`

- **Locate git** (`locate.go`, theo `agentruntime/locate.go`): Homebrew, `/usr/local`, `LookPath`;
  `/usr/bin/git` chỉ khi Command Line Tools có thật (không chạy shim — nó bật hộp thoại cài). Git
  ≥ 2.26. Trả `Availability{Path, Version, Reason}`.
- **Repo** (`repo.go`): shadow repo mỗi project tại `<dataDir>/checkpoints/<hash(EvalSymlinks(project))>`
  (0700, file owner ghi đường dẫn gốc). Không bao giờ đụng `.git`/index/HEAD của người dùng.
  - Env sạch: bỏ mọi `GIT_*`, `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`,
    `GIT_TERMINAL_PROMPT=0`, `LC_ALL=C`; `init --template=`.
  - Config: `core.autocrlf=false`, `core.symlinks/filemode=true`, `core.precomposeunicode=true`
    (darwin), `core.fsmonitor=false`, `core.untrackedCache=false`, `gc.auto=0`.
  - `info/attributes` = `* -text -eol -filter -ident -diff -merge -working-tree-encoding` (khôi phục
    đúng từng byte, bất kể `.gitattributes`/LFS của project).
  - Mutex theo project; mọi lệnh git có context + kill khi quá hạn.
- **Bỏ qua bắt buộc** (yêu cầu 3): truyền dạng `-x` cho `ls-files` (ưu tiên cao nhất, thắng cả `!`
  trong `.gitignore`) **và** lọc lại trong Go theo thành phần đường dẫn:
  `node_modules`, `bower_components`, `.pnpm-store`, `.venv`, `venv`, `__pycache__`, `.gradle`,
  `.next`, `.nuxt`, `.turbo`, `.cache`, `.DS_Store`, repo con (`.git` thư mục hoặc file).
  Không chặn cứng `dist/build/target/vendor` (Go vendor là mã nguồn) — để `.gitignore` + giới hạn lo.
  Thêm `extraExcludes` từ Settings.
- **Snapshot** (`snapshot.go`): `ls-files -z -m -o -d --exclude-standard -x …` → lọc Go (deny, repo
  con `dir/`, file > 10 MiB qua Lstat, trần số file ~50k → "project quá lớn") → `update-index -z --add
  --remove --stdin` → `write-tree` → ref `refs/devkit/cp/<id>/{before,after}`. Lưu tree id, báo `skipped`.
- **Diff** (`diff.go`): `diff-tree -r -z --no-renames --numstat` + `--raw` cho mode.
- **Restore** (`restore.go`): `Plan(target, expected, current)` → hành động + xung đột; `Apply` bằng
  index tạm: `read-tree <target>` + `checkout-index --stdin -z -f` cho path đã chọn (git tự từ chối ghi
  xuyên symlink, giữ mode 100755/120000). Xoá bằng Go với `Lstat` + kiểm mọi thư mục cha là thư mục
  thật trong root; chỉ xoá path có trong cây "hiện tại mong đợi" mà không có trong đích; không bao giờ
  đụng path bị skip/ignore ở bất kỳ snapshot nào; chỉ xoá thư mục do chính lần restore làm rỗng.
- **Hoạt động** (`activity.go`): registry theo project `Begin(project, session, kind)`/`End` cho lượt
  và restore → biết session nào chạy chồng thời gian.
- **Bảo trì** (`maintenance.go`, lúc khởi động + hằng ngày, giữ mutex): giữ 30 ngày / 200 checkpoint mỗi
  session / trần mềm 1 GiB mỗi project; safety row hết hạn 7 ngày; xoá ref mồ côi, `pack-refs`,
  `gc --prune=now` khi nhiều loose object; xoá thư mục shadow không còn dòng nào.

## Phase D — Chụp mỗi lượt + report của vendor + UI "file đã đổi"

- `agenthub.Service` nhận interface `Checkpoints` (nil = tắt), nối ở `main.go`/`container.go`, theo
  preference + availability.
- `Invoke`:
  - baseline chạy nền khi tạo/resume session (để snapshot đầu lượt luôn là loại 0.04s);
  - snapshot **before** chạy song song với `runtimeFor` ngay sau khi claim, join trước `rt.Prompt`
    (hạn ~5s). Quá hạn → **kill** và đánh dấu `before_missing`; không bao giờ giữ cây chụp muộn;
  - `activity.Begin`; prompt kèm `_meta.jetbrains.air.agentFileChangeReportRequest {version:1,
    requestId: checkpointID}` — `SessionRuntime.Prompt` nhận thêm meta (`PromptRequest.Meta` đã có
    sẵn ở `types_v1.go:124`), mock cập nhật theo;
  - snapshot **after** ngay khi `rt.Prompt` trả về, trên **mọi** nhánh, **trước** khi đổi status
    (không để trong defer: UI thấy idle trong khi claim còn giữ → prompt kế bị busy). Hạn ~10s;
  - tính `changed` trong goroutine, phân loại mỗi path: `agent` (có trong report hoặc
    locations/diff của tool_call), `child:<session>` (nằm trong checkpoint của session con lồng
    trong lượt), `unattributed` (Bash/MCP/async — "có thể do session khác" nếu `overlap` có session
    không phải con). `diff(prev.after, this.before)` = thay đổi ngoài lượt.
- `adapter_claude.go`: thêm `agentFileChangeReport` vào `devkitClientCapabilities` **chỉ khi bật**
  (nó bật checkpoint native của Claude cho mọi session; áp dụng cho process mới). Nhận report ở
  update/params `_meta` (theo S7) → `Checkpoints.AttachReport(requestId, report)` kể cả khi tới sau
  lượt; không ghi vào transcript.
- Bridge (`internal/bridge/agenthub.go`, grant `session:*` đã có): `AgentHubListCheckpoints(sessionId)`,
  `AgentHubCheckpointFileDiff(checkpointId, path)`; event `agenthub:checkpoint` khi after xong.
- Frontend: header "Worked for…" của lượt hiện "N file đã đổi" (khoá theo prompt messageId) → danh
  sách +/- với nhãn nguồn và drawer diff từng file. Lượt không có checkpoint thì không hiện số.

## Phase E — Khôi phục code

- Service `PlanRestore(session, promptMessageID, mode undo|toBefore)` (chỉ đọc, trả token) và
  `ApplyRestore(token, overwrite[])`:
  - **chỉ người dùng** (từ chối khi `identity` là agent; không mở thành MCP tool);
  - từ chối khi session đang chạy/chờ permission/giữ claim; cảnh báo session khác cùng project đang
    chạy và async task còn sống;
  - **Undo lượt này**: mọi path trong diff của lượt; **Về trước lượt N**: hợp các diff lượt ≥ N của
    session, đích `before_N`, "hiện tại mong đợi" = after của lượt cuối chạm path đó. Lệch → xung đột,
    mặc định bỏ qua, có tuỳ chọn ghi đè;
  - checkpoint `before_missing`/xấp xỉ → cảnh báo;
  - luôn ghi safety row (snapshot hiện tại) trước; `activity.Begin/End` quanh apply.
- UI: dialog xác nhận (file sẽ đổi, xung đột, không khôi phục được) + "Hoàn tác khôi phục" từ safety tree.

## Phase F — Rewind: hội thoại + code

- Adapter: interface tuỳ chọn `RuntimeForker`; `ClaudeAdapter.Fork(ctx, OpenSpec, srcACPID, forkMeta)`
  dùng lại `open()`: initialize → `session/fork` → `session/load(newId)` trong **một** process, handler
  để nil (replay tự bị bỏ). Types `acp.SessionForkRequest/Result`. Mock adapter có Fork + tiêm lỗi.
- **Fork point** = messageId `msg_…` cuối cùng của luồng chính (không phải subagent) trước lượt N;
  vendor từ chối → lùi ứng viên trước (tối đa ~5). Lượt đầu → `Open` (session mới). DevKit cắt
  transcript tại `user_message_chunk` đầu tiên **sau** dòng cuối của message fork point — nên lượt
  lỗi/huỷ không có trả lời cũng bị cắt cùng; dialog nói rõ.
- `PlanRewind(session, promptMessageID, mode both|conversation|code)` (chỉ đọc, token) →
  `ApplyRewind(token, choices)` theo thứ tự:
  1. claim + kiểm lại token (đổi → "kế hoạch đã cũ");
  2. đóng runtime + MCP endpoint cũ (giết luôn async task, không còn ai ghi);
  3. `recorder.Flush` rồi `Drop`;
  4. ghi safety row `rewind_safety` trạng thái `pending` (nhật ký phục hồi khi crash);
  5. endpoint mới + `Fork` (hoặc `Open`); ghi id mới vào safety row;
  6. khôi phục code (nếu mode có) — lỗi → trả path từ safety tree, đóng runtime mới, xoá fork, huỷ;
  7. **một transaction**: `acp_session_id` mới; xoá transcript `seq >= cut` trừ dòng state
     mode/config/commands (đánh số lại từ `cut`), xoá dòng usage; xoá `agent_turns`/`agent_checkpoints`
     của các prompt bị cắt; safety row → `done`. Lỗi → trả code từ safety tree, xoá fork;
  8. publish runtime/endpoint, `attachUpdates`, áp lại mode/model/effort từ dòng state (theo S2);
  9. emit `agenthub:sessionRewound {sessionId, promptText}`;
  10. best-effort `session/delete(oldId)` (theo S6); nhả claim.
  - Mode "chỉ hội thoại" bỏ bước 4 và 6; "chỉ code" = Phase E.
- `RecoverStaleSessions`: safety row còn `pending` → banner trên session "Rewind bị gián đoạn — khôi
  phục file về trước rewind?" (không tự động revert); fork dở dang xoá best-effort.
- S5 thất bại → không cho rewind vượt qua lần compaction gần nhất, giải thích trong UI.
- Frontend: `sessionRewound` → `reloadTranscript` + reset paging, buffer stream, thẻ permission/câu
  hỏi; từ chối rewind khi hàng đợi prompt còn (hoặc hỏi xoá). Hành động trên bong bóng prompt:
  - **Rewind** (menu): cả hội thoại và code (mặc định khi có checkpoint) / chỉ hội thoại / chỉ code;
  - **Bút chì = "Sửa và chạy lại từ đây"**: rewind theo mode đã chọn rồi đặt text vào composer (không
    tự gửi; ảnh/attachment cũ không khôi phục được — nói rõ);
  - `AgentRail` truyền `onEditPrompt`/`onCopy` vào `TranscriptStream` (hiện đang thiếu).

## Phase G — Settings + tài liệu

- Preference `agenthub.checkpoints {enabled=true, maxFileSizeMiB=10, retentionDays=30, extraExcludes}`
  theo mẫu `agent_notifications` (`internal/preferences`, bridge `preferences.go`, grant
  `container.go:275`). Settings → Agents: dòng trạng thái "Checkpoint: bật (git 2.x tại …)" hoặc lý do
  không dùng được (không có git / thiếu Command Line Tools `xcode-select --install` / git quá cũ), và
  "project quá lớn".
- `docs/dev-kit/agent-hub.mdx`: FR mới cho checkpoint/rewind (lưu ở đâu, bỏ qua gì, retention, giới
  hạn), cập nhật FR-13 theo ngữ nghĩa 0.78; `implementation-status.mdx`.

---

## Verification

**Go — git thật trong `t.TempDir()` (skip nếu không có git):** deny thắng `!node_modules/x` trong
`.gitignore`; giới hạn kích thước (cả file đang theo dõi lớn dần); repo con/submodule bị bỏ và báo;
symlink khôi phục là symlink; giữ exec bit; `.gitattributes` `* text=auto eol=crlf` + `filter=lfs`
khôi phục đúng byte; tên NFD trên darwin; ghi xuyên thư mục symlink ra ngoài root bị từ chối; không
xoá path bị skip; chỉ xoá thư mục do restore làm rỗng; phát hiện xung đột; `Snapshot` song song tuần
tự hoá; gc giữ cây có ref; trần số file; hết hạn giết git; `GIT_DIR`/`GIT_WORK_TREE` từ env cha không
lọt vào. Locate: shim `/usr/bin/git` không bị chạy khi thiếu CLT.

**Store:** m22 trên DB cũ; transaction rewind giữ + đánh số lại dòng state, xoá usage/turns/checkpoints,
rollback trọn vẹn khi tiêm lỗi; `DeleteSessions` xoá checkpoints.

**Service (MockAdapter):** đúng thứ tự rewind; rollback khi fork / restore / transaction lỗi; không
dòng nào được ghi giữa lúc đóng runtime và commit; từ chối khi bận, khi caller là agent, khi token cũ;
echo prompt chỉ có ảnh có messageId; snapshot after xong trước khi status idle; report tới sau lượt vẫn
gắn đúng checkpoint.

**Wire (fake runner như `adapter_claude_test.go`):** `Fork` gửi initialize → `session/fork{sessionId,
cwd, mcpServers, _meta.jetbrains.air.fork{version:1, messageId}}` → `session/load(newId)`; prompt
`_meta` đúng hai key; capability `agentFileChangeReport` chỉ có khi bật.

**Frontend:** `elicitation.test.ts` theo ngữ nghĩa 0.78; `turns.test.ts` prompt chỉ có ảnh mở lượt mới;
event rewind reset paging/hàng đợi/thẻ; surfaces cho "N file đã đổi", dialog restore/rewind. Hai
ratchet + `bun test` + `bun run build`; `wails3 generate bindings -clean=true -ts -i`.

**Mutation:** bỏ `-x` deny, bỏ kiểm symlink cha, đổi thứ tự snapshot after/status, bỏ transaction → mỗi
cái làm test tương ứng đỏ.

**Live (Claude thật, 0.78):** bộ live cũ + spike S1–S7. Kịch bản đầu-cuối: nhờ agent sửa 2 file bằng
Edit và 1 file bằng `sed` trong Bash → lượt hiện "3 file đã đổi" (2 `agent`, 1 `unattributed`) → Undo
lượt → cả 3 về như cũ, `git status` của repo người dùng không đổi index/HEAD → gửi tiếp 2 lượt, Rewind
về trước lượt 2 với mode "cả hai" → transcript cắt đúng, agent không nhớ nội dung lượt bị cắt, file về
đúng trạng thái → "Sửa và chạy lại" đặt prompt vào composer. Đối chứng: project có `.gitignore` thiếu
`node_modules` → snapshot vẫn không chứa `node_modules`, thời gian vẫn ~0.04s sau baseline.

## Rủi ro

1. **Fork khi lượt bị huỷ giữa tool / qua compaction** chưa được đo — S3/S5 chặn Phase F.
2. **Phân loại nguồn khi nhiều session cùng thư mục** chỉ là gợi ý; khôi phục dựa vào kiểm "hiện tại
   mong đợi" để không ghi đè việc sau đó.
3. **Bật `agentFileChangeReport` = bật checkpoint native của Claude cho mọi session** (ghi
   `~/.claude/file-history`) — gắn với toggle.
4. **Máy không có git / thiếu Command Line Tools** → tính năng tắt kèm lý do rõ ràng, rewind chỉ còn
   hội thoại.
5. Rewind hội thoại **không hoàn tác được** (session vendor cũ bị xoá); code thì hoàn tác được qua
   safety tree.

## Không làm ở v1

Theo dõi thư mục ngoài project; khôi phục ảnh/attachment khi sửa prompt; rewind do agent gọi (MCP);
đồng bộ checkpoint giữa máy; dùng `--rewind-files`/`rewindFiles` thật của Claude để khôi phục.
