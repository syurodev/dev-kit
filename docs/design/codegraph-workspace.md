# CodeGraph theo workspace (nhiều folder)

Áp dụng cho `frontend/src/modules/codegraph/`, `internal/codegraph/`, `internal/wssearch/`,
`internal/docwatch/` và các method `Workspace*` của bridge. Đọc kèm
[Tích hợp module với sidebar và tab strip](./module-shell-integration.md) và
[Workspace](../dev-kit/workspace.mdx).

## 1. Khảo sát: hệ thống đã có gì

| Thứ cần | Đã có | Ghi chú |
|---|---|---|
| Workspace ID ổn định | `workspaces.id` (24 hex) | `useWorkspaceSwitcherStore.currentId` giữ workspace đang xem |
| Folder ID ổn định | `workspace_services.id` (24 hex) | `UNIQUE (workspace_id, path)`: đổi path = folder khác. Trong UI này một dòng đó gọi là **folder** |
| Primary | `workspaces.root` = path của folder đầu tiên | Chỉ đổi được bằng `UpdateWorkspace` (đặt lại cả danh sách); chưa có API đổi riêng |
| Nhiều project đồng thời | `codegraph.Registry` (focus + ≤8 resident) trên một `Host` | 16 method đọc nhận `servicePath` |
| Trần analyze | `Host.jobs` (2) | Bao cả open, watcher re-index và index queue |
| Hàng đợi index | `Registry.Enqueue` (1 worker) + `IndexStatus` | Không huỷ được; run "refresh" cho folder đang mở chỉ **chờ** index, không re-index |
| Index trên đĩa không cần mở Service | `ReadStoredSnapshot`, `StoredIndex` | Mẫu để đọc entry points / outline mà không chiếm slot resident |
| Watcher | `Service.restartWatcher` (mỗi thư mục, để re-index) | Không dùng được cho editor: không bounded theo tài liệu mở |
| Draft recovery | `FileBuffer` còn lại sau khi đóng tab (store module) | Giữ nguyên ý tưởng, đổi key |

### Giả định "một project" cần thay (đã kiểm tra bằng grep)

1. `codegraphStore.projectPath` — một project duy nhất cho entry points, files, git, plan, index info.
2. `buffers`/`flows` key bằng đường dẫn tương đối / node id trần → hai folder trùng `src/config.ts`
   dùng chung buffer; flow của hai service trùng node id dùng chung state.
3. `switchProject()` đóng mọi flow/file khi đổi project; `reindex(path)` = `Focus` ở Go.
4. `ProjectHeader` chọn folder = chuyển toàn bộ CodeGraph sang folder đó.
5. `saveFile` gửi `expectedRoot` rồi ghi vào **service đang focus** (`CodeGraphWriteFile` không có `servicePath`).
6. `ListFiles`/`ReadFile`/`GitStatus` đi qua `Registry.Open` ⇒ mở một Service resident (watcher + analyze)
   chỉ để liệt kê file; workspace 12 folder làm LRU 8 xoay vòng.
7. Plan, terminal, analyzer build chỉ nhận service đang focus.
8. `isFocusedService` lọc event index theo `projectPath`.
9. `paletteSearch`, `codegraphIntelProvider`, `BlastRadiusPanel`, `loadPlanTasks` đọc "project hiện tại".
10. `openFileInProject(root, path)` gọi `reindex(root)` ⇒ một agent chỉ file làm đổi cả view.

## 2. Mô hình identity và state

```
workspaceId   workspaces.id                      (ổn định, không đổi khi đổi tên/primary)
folderId      workspace_services.id              (ổn định, gắn với path)
relPath       "/"-separated, đã Clean, không "..", giữ nguyên hoa/thường
DocKey        workspaceId ":" folderId ":" relPath
```

- **Display name** (tên folder, tên workspace) ≠ **stable identity** (ID) ≠ **absolute path** (chỉ Go
  biết và chỉ dùng để I/O). Frontend không bao giờ gửi path tuyệt đối để đọc/ghi: nó gửi
  `(workspaceId, folderId, relPath)` và Go tra membership (`workspace.Service.ListServices`) rồi mới
  `resolvePath` dưới root đó (từ chối symlink thoát root, như `ReadFile` cũ).
- Folder bị gỡ khỏi workspace ⇒ `folderId` không còn trong membership ⇒ đọc/ghi trả lỗi có cấu trúc
  `folder_not_in_workspace`. Buffer cũ **không** chuyển sang folder cùng tên/path mới: folder thêm lại
  nhận ID mới nên DocKey khác.
- Root đổi path không tồn tại: path là một phần danh tính nên đó là folder khác. Folder biến mất trên đĩa
  (`accessible=false`) giữ nguyên ID; save báo `folder_unavailable`, edit còn trong buffer.

| State | Nơi ở | Key |
|---|---|---|
| Workspace hiện tại | `useWorkspaceSwitcherStore` (có sẵn) | — |
| Folder, primary | `useWorkspaceServices` (Go là nguồn; primary = `workspace.root`) | `folderId`; **không** lưu bản sao primary |
| Entry points, files, git, index info, outline | `codegraphStore.folders[folderId]` | `folderId` (+ `generation` của workspace) |
| Buffer (nội dung, base, disk revision, dirty, external state) | `codegraphStore.buffers` | `DocKey` |
| Flow (đồ thị) | `codegraphStore.flows` | `folderId + "\u0000" + rootNodeId` |
| Navigation history | `navigationHistory.ts` | `editor group id` |
| Search session | `codegraphStore.search` | `sessionId` (1 session đang hiện trong rail) |
| Discovery scope | `codegraphStore.scope` | workspaceId → `{all}` hoặc `{folderIds}` (UI state, không persist cùng nội dung) |
| Tab params | workbench tabs | `{workspaceId, folderId, path}` / `{workspaceId, folderId, root, entryPoint}` / `{workspaceId, folderId}` (plan) |

Không persist nội dung file hay kết quả tìm kiếm. Draft dirty chỉ sống trong store phiên (như trước);
nó được bảo toàn qua đóng tab, đổi workspace, gỡ folder.

Tab cũ (trước tính năng này) chỉ có `{path}` hoặc `{root}`: `legacy.ts` quy về folder đang là
`codeGraphProjectRoot()` **nếu** folder đó thuộc workspace hiện tại, đúng nghĩa tab cũ; không thì tab hiện
trạng thái "không rõ folder" thay vì đoán bằng primary.

## 3. Scope

| Tính năng | Scope | Đổi bởi |
|---|---|---|
| Sidebar Files/Entry points | **discovery scope** (`all` hoặc tập folder) | Picker ở header |
| Index status / Reindex | discovery scope; menu có "Reindex toàn workspace" | Picker |
| Find in workspace | scope **của search session** (nhìn thấy trong form) | Form; khởi tạo từ discovery scope hoặc folder trong context menu |
| Flow / Plan | **một** folder (document params) | Không đổi sau khi mở |
| File, Breadcrumb, Outline, Back/Forward | document đang focus | — |
| Save | owner của document | — |
| Spawn agent | primary tại thời điểm spawn (module Agent Hub), không phải việc của CodeGraph | — |

Primary không quyết định bất kỳ dòng nào ở trên. Đổi primary (`WorkspaceSetPrimary`) chỉ ghi
`workspaces.root`; không reindex, không đóng tab, không đổi scope. Đổi discovery scope chỉ lọc sidebar:
không đóng tab, không xoá buffer/history, không đổi primary.

## 4. Lifecycle index/analyzer

- Frontend **không bao giờ gọi `Focus`** để xem folder. Dữ liệu đọc theo hai đường:
  1. *Không cần analyzer* — file tree, đọc/ghi file, git status, branch, tìm kiếm: hàm theo **root**
     (`codegraph/rootfiles.go`, `internal/wssearch`) không mở Service. Một folder lỗi/unsupported vẫn đọc được.
  2. *Cần index* — entry points, outline: đọc **index trên đĩa** không mở Service
     (`ReadStoredEntryPoints`, `ReadStoredOutline`, cùng mẫu `ReadStoredSnapshot`), kèm `indexedAt` và
     trạng thái. Truy vấn nặng (flow, definition, references) vẫn qua `Registry.Open(servicePath)`:
     chiếm slot resident (≤8, LRU, ≤2 job analyze) đúng lúc người dùng thực sự mở flow ở folder đó.
- Cache theo `(folderId, indexedAt/version)`; sửa một file chỉ làm hỏng cache của folder đó
  (`codegraph:indexUpdated` mang `servicePath`).
- Analyzer: `Host.analyzers` giữ một analyzer mỗi ngôn ngữ (không nhân theo folder/tab/lookup).
- **Reindex scope**: `WorkspaceReindex(workspaceId, folderIds, force)` đưa folder vào hàng đợi.
  Hàng đợi một worker (tận dụng trần job), có `Cancel(paths)`, folder đang mở thì **re-index thật**
  (`Service.Refresh`) thay vì chỉ chờ. Một folder lỗi không huỷ lô (trạng thái `failed` riêng).
  Tiến độ = số folder xong / số folder trong lô (mẫu số đáng tin vì lô cố định).
- Kết quả cũ không ghi vào context mới: mọi response/event mang `servicePath`, frontend kiểm
  `generation` của workspace + folder còn thuộc workspace trước khi `set`.

## 5. Luồng chuyển

- **Đổi workspace**: tăng `generation`, huỷ request guard, reset state theo-folder của workspace cũ
  (entry points, files…). Buffer dirty và tab của workspace cũ **giữ nguyên** (owner = DocKey); Save vẫn
  hoạt động vì Go tra membership theo `workspaceId` trong DocKey chứ không theo workspace hiện tại.
- **Đổi primary**: `WorkspaceSetPrimary` → `servicesChanged()`; chỉ badge primary đổi.
- **Đổi scope**: set `scope`; selector lọc.
- **Focus document**: không đổi gì ngoài selection trong rail và Outline.
- **Thêm/gỡ folder**: ở Edit workspace (module Workspace); CodeGraph chỉ phản ứng qua
  `servicesVersion` — folder mới vào hàng đợi index (đã có), folder gỡ: tab giữ, buffer dirty thành
  *orphaned* (Copy / Save a copy / Discard), buffer sạch bị đóng cùng tab.

## 6. API Go mới (không cần migration SQL)

Tất cả resolve `(workspaceId, folderId)` → path trong Go; capability dùng grant sẵn có
(`codegraph:fs` `file:*` / `project:active`, `codegraph:read`, `ws:write`).

| Method | Việc |
|---|---|
| `WorkspaceFolderStates(workspaceId)` | `[{folderId, accessible, error, isRepo, branch, changed}]` (git chạy song song, có timeout) |
| `WorkspaceFileList(workspaceId, folderId)` | cây file (dùng `ignoredDir`, tối đa 50k) |
| `WorkspaceFileRead` / `WorkspaceFileStat` | nội dung + `revision` / chỉ metadata + revision |
| `WorkspaceFileWrite(…, content, expectedRevision)` | ghi nguyên tử; `file_conflict` có cấu trúc khi đĩa đã đổi; trả `revision` mới |
| `WorkspaceGitStatus(workspaceId, folderId)` | map path → M/A/D/R |
| `WorkspaceEntryPoints(workspaceId, folderId)` | entry points từ index trên đĩa/Service đang mở + `indexedAt`, `stale` |
| `WorkspaceFileOutline(workspaceId, folderId, path)` | cây symbol theo range từ index |
| `WorkspaceSearch(request)` / `WorkspaceSearchCancel(id)` | tìm nội dung; cancel theo request ID |
| `WorkspaceSetPrimary(workspaceId, folderId)` | đổi `workspaces.root` |
| `WorkspaceReindex(workspaceId, folderIds, cancel)` | reindex theo scope / huỷ |
| `WorkspaceWatchDocuments(docs)` | đăng ký tập tài liệu mở; sự kiện `workspace:fileChanged` |

**Revision của file** = SHA-256 nội dung (hex, 16 byte đầu). Save: kiểm revision hiện tại == expected
rồi rename temp→target. Có TOCTOU nhỏ giữa kiểm và rename (không có CAS trên filesystem); trong tiến
trình DevKit các save cùng file được tuần tự hoá bằng khoá theo đường dẫn. Một tiến trình ngoài ghi
đúng trong cửa sổ đó vẫn có thể bị đè; bảo đảm là "phát hiện thay đổi tại thời điểm save", không phải
cô lập tuyệt đối.

## 7. Giới hạn của intelligence xuyên folder

- Definition/implementation/references chỉ trả kết quả **trong index của folder chứa file** (analyzer
  giải type trong project đó; `CombinedTypeSolver` cho module cùng folder). Không có match theo tên toàn
  workspace. UI nói rõ "trong folder X"; kết quả tìm văn bản **không** được gọi là references.
- Liên kết giữa service qua HTTP/Kafka/Redis đã có ở `internal/crossservice` (join khi hỏi). Không vẽ
  cạnh call tĩnh giữa hai folder, không suy luận từ tên giống nhau.
- Plan là dữ liệu **của từng project** (`plan_nodes` trong index riêng). UI Plan chọn một folder và nói rõ
  nguồn; không gộp mảng giữa folder.
