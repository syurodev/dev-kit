# Tasks × Workspace — Kế hoạch implement

> **For agentic workers:** làm theo thứ tự task, mỗi task một commit. Steps dùng checkbox (`- [ ]`).

**Goal:** Board gắn workspace qua binding local; agent chỉ thấy board của workspace nó đang làm; `verify` chạy theo **tên service** thay vì path tuyệt đối đã sync; hàng đợi `verify` rút cả lúc khởi động và lúc đổi work folder; UI có sidebar **tree** (cùng `TreeView` với Database) và tự cập nhật khi agent đổi task.

**Spec:** `docs/dev-kit/tasks.mdx` § "Tích hợp Workspace — liên kết nằm ở máy, tên đi theo sync" (commit `8d1a682a`, nhánh `claude/tasks-workspace`).

**Mặc định cho ba open question của spec** (đổi được trước Task 3, sau đó đắt hơn):

1. Board chưa gắn ⇒ agent **không** thấy (fail closed). Migration (Task 5) gắn được mọi board có `project_path` trùng một service.
2. Workspace ↔ board là **N:1, tạo tay**. "New board" trong sidebar tạo và gắn luôn vào workspace đang đứng.
3. `verify` **một lệnh mỗi board**, chạy trong một service (`verify.service`, rỗng = primary).

**Quy ước chung**

- Nhánh `claude/tasks-workspace`. Không push khi chưa được bảo.
- `frontend/dist` phải tồn tại trước mọi lệnh Go chạm `internal/arch`: `cd frontend && bun run build`.
- Regression mỗi task Go:
  `go test ./internal/tasks/ ./internal/workspace/ ./internal/mcp/ ./internal/bridge/ ./internal/app/ ./internal/storage/ ./internal/arch/ -count=1`
- Frontend: `cd frontend && bun test && bun run build`.
- Đổi signature bridge ⇒ `wails3 generate bindings -clean=true -ts -i` (xem AGENTS.md § Regenerating bindings) rồi `bun run build`.

---

## Phần A — Backend

### Task 1: Bảng `task_board_bindings` (migration 32)

**Files:** `internal/storage/sqlite.go`, `internal/storage/*_test.go`, `internal/app/wiring.go` (field `TaskBoardBindings` trong struct tables)

- [ ] Hằng `TableTaskBoardBindings = "task_board_bindings"`, comment: machine-local, không sync, cùng loại với `task_verify_runs`.
- [ ] Migration 32:
  ```sql
  CREATE TABLE task_board_bindings (
    board_id     TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    bound_at     TEXT NOT NULL
  );
  CREATE INDEX idx_task_board_bindings_workspace ON task_board_bindings (workspace_id);
  ```
  `PRAGMA foreign_keys=ON` đã bật ở `sqlite.go:116`. Không bọc transaction ngoài (AGENTS.md: `MaxOpenConns(1)`).
- [ ] Test: xoá workspace ⇒ binding biến mất; board không bị đụng (board ở `vault_items`).

### Task 2: Tách luật "phiên thuộc workspace nào" khỏi MCP

**Vì sao:** `workspace.list` (`internal/mcp/tools_workspace.go:78`) đã có luật `coveredBy(svc.Path, granted) || within(cwd, svc.Path)`. Tasks cần đúng luật đó; hai bản copy sẽ lệch nhau.

**Files:** Create `internal/workspace/scope.go`, `internal/workspace/scope_test.go`; Modify `internal/mcp/tools_workspace.go`

- [ ] `func SessionOwns(services []WorkspaceService, cwd string, granted []string) bool` + hai helper path chuyển nguyên từ MCP.
- [ ] `tools_workspace.go` gọi helper mới; `tools_workspace_test.go` phải xanh **không sửa** — đó là bằng chứng hành vi không đổi.

### Task 3: `tasks` biết phạm vi — binding, `WorkspaceScope`, `boardFor`

**Files:** Create `internal/tasks/binding.go`, `internal/tasks/scope_test.go`; Modify `internal/tasks/tasks.go`, `service.go`, `transition.go`, `plan.go`, `tasks_test.go`, `plan_test.go`

- [ ] `binding.go`: repo mỏng trên `*sql.DB` — `Bind(boardID, workspaceID)`, `Unbind(boardID)`, `WorkspaceOf(boardID) (string, bool)`, `Bindings() (map[string]string, error)`. Binding trỏ tới workspace không còn tồn tại ⇒ coi như chưa gắn (phòng khi FK bị tắt ở test).
- [ ] Interface tiêm từ app:
  ```go
  type WorkspaceScope interface {
      ServicePath(workspaceID, service string) (string, bool)
      SessionWorkspaces(sessionID string) []string
      Exists(workspaceID string) bool
  }
  ```
  `SetScope(scope, db, table)`. **Scope nil ⇒ hành vi như hôm nay** (mọi board thấy được) để test cũ và server mode thiếu wiring không vỡ.
- [ ] `Board` thêm `WorkspaceID string \`json:"workspaceId,omitempty"\`` — **tính lúc đọc** từ binding, **không** ghi vào `boardMetadata`. `Verify` thêm `Service string \`json:"service,omitempty"\``.
- [ ] Helper duy nhất `boardFor(caller, boardID) (Board, error)`:
  - caller là user (`SessionID == ""`) ⇒ mọi board.
  - caller là agent ⇒ board phải gắn vào một workspace trong `SessionWorkspaces(sessionID)`; không thì `ErrNotFound` (**không** `ErrPermissionDenied` — không lộ việc task tồn tại).
  - Caller là CLI terminal trên socket chung: `identity` không đóng dấu `SessionID` ⇒ đi nhánh user, khớp `workspace.list`.
- [ ] Mọi method công khai nhận `Caller` và đi qua `boardFor` (qua board của task với method nhận task id): `Boards`, `Board`, `List`, `Get`, `Update`, `SetFields`, `Archive`, `Unarchive`, `Claim`, `Release`, `Transition`, `AddComment`, `Comments`, `Progress`, `Search`, `LinkPlanNode`, `ByPlanNode`, `CreateFromPlan`, `Create`. Đường nội bộ (`applyTransition` của gate, `systemComment`, `ReconcilePlan`) giữ `get`/`Board` không lọc.
- [ ] `SetFields` với `BoardID` mới: board đích cũng phải qua `boardFor` — không thì agent chuyển task sang board ngoài phạm vi.
- [ ] Test (bảng): agent ở WS-A thấy/claim/transition task của board gắn WS-A; với board WS-B và board chưa gắn thì mọi method trả `ErrNotFound`; user thấy tất cả; scope nil = hành vi cũ.

### Task 4: `verify` chạy theo tên service; drain lúc khởi động và lúc đổi work folder

**Files:** `internal/tasks/gate.go`, `internal/tasks/transition.go`, `internal/tasks/tasks.go`, tests

- [ ] `resolveVerifyDir(board) (string, error)`:
  1. có binding + `verify.service` ⇒ `ServicePath(ws, service)`;
  2. có binding + service rỗng ⇒ primary (`ServicePath(ws, "")`);
  3. không binding, scope nil ⇒ `board.ProjectPath` (legacy);
  4. còn lại ⇒ `ErrBoardUnbound` (`tasks.boardUnbound`) — **không** rơi về hỏi người.
- [ ] `closeThroughGate` / `enqueue` dùng path đã resolve làm `project_path` của run — khoá hàng đợi vẫn là work tree (NFR-13).
- [ ] `DrainAll()`: rút mọi `project_path` distinct đang `queued`. Path không còn (`os.Stat`) ⇒ `stale` + comment system (FR-8f).
- [ ] `Drain(path)` công khai (bọc `drain`).
- [ ] Test: board gắn WS có service `api` ⇒ run có `project_path` = path của `api`; board chưa gắn ⇒ `ErrBoardUnbound`, không có dòng nào trong bảng; `Recover` + `DrainAll` chạy run bị bỏ dở; path biến mất ⇒ `stale`.

### Task 5: Wiring ở `internal/app` + migration board cũ + reconcile + quyền push

**Files:** `internal/app/wiring.go`, `internal/app/plantasks.go`, `internal/app/committurn.go`, Create `internal/app/taskscope.go` + test, `main.go`

- [ ] `taskScope` adapter: `workspace.Service` + `agenthub.Service.GetStatus` (+ `workspace.SessionOwns` của Task 2). Wire trong `WireAgentLiveness` (hub có muộn — cùng chỗ `SetLiveness`).
- [ ] `MigrateBoardBindings(c)`: idempotent, chạy lúc start sau khi workspace sẵn sàng. Board có `project_path`, chưa có binding ⇒ tìm workspace có service trùng path ⇒ `Bind`. **Không ghi** metadata board (tránh conflict sync khi hai máy cùng migrate). `verify.service` rỗng + `project_path` trùng service của workspace đã gắn ⇒ Task 4 bước 2' dùng service đó lúc resolve (thêm vào `resolveVerifyDir` trước bước 2).
- [ ] Sau `gate.Recover()`: `gate.DrainAll()`. `main.go`: `WorkFolder.Subscribe` ⇒ `c.Tasks.Gate().Drain(state.Current)` (không block — `drain` đã chạy goroutine riêng).
- [ ] `reconcileBoardsForProject`: `board.ProjectPath != root` ⇒ "board gắn workspace có service path == root" (fallback `ProjectPath` khi scope nil).
- [ ] `TaskBranches.BranchesForSession` và `taskKeyForSession`: dùng `Boards(Caller{SessionID})` — chỉ quét board trong phạm vi.
- [ ] Test: migration gắn đúng, chạy hai lần không đổi gì; reconcile bỏ qua board của workspace khác.

### Task 6: Bridge

**Files:** `internal/bridge/tasks.go`, tests, `frontend/bindings/**` (regenerate)

- [ ] Mọi `Tasks*` hiện có truyền `tasks.Caller{}` theo signature mới.
- [ ] Mới, `tasks:admin`, UI-only: `TasksBoardBind(boardID, workspaceID)`, `TasksBoardUnbind(boardID)`.
- [ ] `TasksBoardCreate(name, keyPrefix, color, workspaceID)` — gắn luôn khi `workspaceID != ""`.
- [ ] `TasksBoardSetVerify(boardID, command, timeoutSec, service)` thay `projectPath`. Bỏ `TasksBoardChooseProject` (service chọn từ danh sách service của workspace, không còn folder picker) — xoá luôn test của nó.
- [ ] Lỗi `tasks.boardUnbound` đi ra UI kèm câu "Gắn board vào một workspace để chạy verify".
- [ ] Regenerate bindings; `bun run build` xanh.

### Task 7: MCP

**Files:** `internal/mcp/tools_tasks.go`, `internal/mcp/tools_tasks_test.go` (hoặc file test hiện có), `internal/mcp/tool_names_test.go`

- [ ] Interface `TasksAPI` theo signature mới; mọi tool truyền `taskCaller(ctx)`.
- [ ] **Không** thêm tool bind/unbind; thêm vào test loại trừ cứng (cùng chỗ đang khẳng định không có delete/board write).
- [ ] Mô tả `tasks.get` đang nói "comments" nhưng không trả — sửa mô tả (agent có `tasks.comment.list`).
- [ ] Test qua server thật: session ở WS-A gọi `tasks.get` task WS-B ⇒ `tasks.notFound`.

### Task 8: Sự kiện `tasks:changed`

**Vì sao:** sidebar tree hiện task; agent đổi task qua MCP mà tree không đổi là tree nói sai.

**Files:** `internal/tasks/service.go`, `gate.go`, `main.go`, `frontend/src/bridge/tasks.ts`

- [ ] `Service.OnChanged func(boardID, taskID string)` gọi sau mọi write thành công (`Create`, `mutate`, `Update`, `Delete`, `writeComment`, `CreateBoard`/`UpdateBoard`/`DeleteBoard`, bind/unbind, gate finish). Non-blocking: chỉ gọi hàm, `main.go` gắn `emitAppEvent("tasks:changed", {boardId, taskId})`.
- [ ] Payload chỉ có id — không title, không description (NFR-4).
- [ ] `onTasksChanged(cb)` trong bridge TS.

---

## Phần B — Frontend

### Task 9: Store + builder tree thuần

**Files:** Create `frontend/src/modules/tasks/tasksStore.ts`, `tasksTree.ts`, `tasksTree.test.ts`

- [ ] `tasksStore` (zustand, `persist` qua `uiStateStorage` như `databaseStore` — chỉ persist trạng thái mở/đóng): `boards`, `tasksByBoard` (tải **lười** khi mở board), `expanded{workspaces,boards,statuses,tasks}`, `selectedBoardId`, `refreshBoard(id)`, `refreshBoards()`. Subscribe `onTasksChanged` ⇒ `refreshBoard(boardId)` debounce ~150ms.
- [ ] `tasksTree.ts` — hàm thuần dựng `TreeNodeData[]`, test không cần DOM (giống `databaseTree.ts`):

  ```
  Workspace (FolderIcon, count = số board)        ← workspace hiện tại lên đầu, mở sẵn
  └─ Board (KanbanSquare, count = task mở)        ← click: mở tab board
     └─ Status (theo thứ tự workflow, count)      ← category terminal đóng sẵn
        └─ Task  KEY  title   [agent•] [2/5]      ← click: preview tab; double-click: pin
           └─ Subtask                             ← một cấp, đúng parent_id
  Chưa gắn workspace (chỉ hiện khi có)
  └─ Board …
  ```
  - Task con không lặp lại ở cấp status — chỉ nằm dưới cha.
  - Archived không hiện.
  - Search: lọc theo key / title / label (đều là metadata plaintext); đang search thì mở ép mọi tầng phía trên match — cùng luật `DatabaseSidebar` ("a hit four levels down that stays folded is the same as no hit at all").
- [ ] Test: thứ tự status theo workflow; subtask chỉ dưới cha; workspace hiện tại đứng đầu; board chưa gắn vào nhóm riêng; search mở ép đúng nhánh.

### Task 10: `TasksSidebar` + manifest

**Files:** Create `frontend/src/modules/tasks/TasksSidebar.tsx`; Modify `manifest.ts`

- [ ] Khung giống `DatabaseSidebar`: `SidebarHeader` + `SidebarSearch` + nút **New board** (gắn vào workspace hiện tại) + **New task** (board đang chọn); `TreeView` + `Empty` states.
- [ ] Menu ngữ cảnh / `TreeRowAction`:
  - Workspace: New board.
  - Board: Open, Verify settings…, Create tasks from plan (khi Code Graph đang mở service của workspace), Move to workspace…, Unbind.
  - Board chưa gắn: **Bind to current workspace**, Bind to…
  - Task: Open, Open beside, Archive, Copy key. (Delete chỉ ở detail — hành động phá huỷ không nằm cạnh một click nhầm.)
- [ ] Trailing: chấm "held by an agent" khi `sessionId`, `TreeRowTag` cho priority, `2/5` cho task có con (dùng lại `progressOf`).
- [ ] `manifest.ts`: `sidebar: lazy(() => import("./TasksSidebar")…)`.

### Task 11: Board view và dialog theo mô hình mới

**Files:** `TasksBoardView.tsx`, `BoardVerifyDialog.tsx`, `documents.ts`, `frontend/src/bridge/tasks.ts`

- [ ] `tasks.board` nhận `params.boardId`; bỏ `<select>` board (sidebar là chỗ chọn). `opensHome`: board đầu tiên của workspace hiện tại, không có thì empty state "New board".
- [ ] `openBoard(boardId, name)` trong `documents.ts` (per-board tab, như `openTaskDetail`).
- [ ] `BoardVerifyDialog`: bỏ folder picker; **select service** lấy từ `useCurrentWorkspaceServices()` (workspace của board), mặc định primary. Board chưa gắn ⇒ dialog nói phải gắn trước.
- [ ] Board view và `TaskDetailView` subscribe `onTasksChanged` để tự tải lại.
- [ ] Verify tay trong preview (server mode — memory `e2e-verify-server-mode`): tạo 2 workspace, board ở mỗi cái, đổi workspace switcher ⇒ tree đúng; agent/MCP đổi task ⇒ tree đổi không cần refresh.

---

## Phần C — Tài liệu

### Task 12: Đồng bộ docs với code

**Files:** `docs/dev-kit/tasks.mdx`, `docs/dev-kit/implementation-status.mdx`, `AGENTS.md`

- [ ] `tasks.mdx`: callout "Design — chưa có runtime" của mục Workspace ⇒ trạng thái thật; callout Partial ở đầu trang bỏ "drain lúc khởi động" và "tự cập nhật" khỏi danh sách "Chưa có"; mục UI ghi sidebar tree.
- [ ] `implementation-status.mdx` dòng Tasks: binding, phạm vi agent, `verify.service`, tree sidebar, `tasks:changed`.
- [ ] `AGENTS.md` § Credentials, work…: một bullet — board gắn workspace qua `task_board_bindings` (local), agent chỉ thấy board trong phạm vi, `project_path` chỉ còn là fallback đọc.

---

## Thứ tự và phụ thuộc

```
T1 ─┐
T2 ─┼─► T3 ─► T4 ─► T5 ─► T6 ─► T7
    │                      └──► T8 ─► T9 ─► T10 ─► T11 ─► T12
```

T3 là task lớn nhất (đổi signature mọi method) — tách hai commit nếu cần: (a) binding + scope + `boardFor` với test, (b) đổi signature và cập nhật caller.

## Rủi ro đã biết

- **Board biến khỏi agent sau nâng cấp** nếu `project_path` không trùng service nào — nhóm "Chưa gắn workspace" trong tree là lối ra; ghi trong release note.
- **Binding trỏ vào workspace đã xoá**: FK `ON DELETE CASCADE` tới `workspaces(id)` dọn nó (cùng cách migration 30 đã làm cho `workspace_services`); `Exists` lúc đọc chỉ là lưới cho test/DB mở không có `foreign_keys=ON`.
- **CLI terminal thấy mọi board** — cố ý, khớp `workspace.list`; nếu cần siết thì đó là quyết định chung cho cả hai tool.
