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

### 3.1 Folder chồng nhau (nested roots)

Một workspace có thể chứa cả `orders-service` lẫn `orders-service/libs/common`. Chính sách:

- **Mỗi file vật lý có một chủ trong *một* kết quả**: folder sâu nhất, trong scope đang xét, chứa nó.
  Search (`wssearch.nestedSubtrees`) và cây Files (`withoutNested`, kể cả thư mục chỉ chứa folder lồng) đều
  bỏ phần cây của folder lồng khỏi folder ngoài. Nếu folder lồng **không** nằm trong scope thì folder ngoài
  lại sở hữu các file đó. Không dedupe theo relative path, không thay folder ID bằng canonical path.
- Include/exclude đọc theo **đường dẫn trong folder chủ** (`src/**` là `src` của từng folder, ở đâu cũng vậy).
  `libs/**` không khớp gì qua folder ngoài vì các file đó thuộc folder lồng.
- Cùng một file vật lý **vẫn mở được** qua hai identity (vd. đường dẫn `libs/common/x.ts` của folder ngoài, hay
  từ breadcrumb). Đó là hai buffer: lưu qua cái này thì cái kia, nếu sạch, theo đĩa; nếu có sửa dở thì thành
  *conflict* chứ không bị ghi đè (so revision, test `one physical file under two folder identities`). Các lần
  lưu cùng đường dẫn tuyệt đối được tuần tự hoá bằng một khoá (`writeLockFor`), nên hai identity không đua nhau.
  Watcher báo **cả hai key** khi file đổi.
- Hai tên folder trùng đường dẫn thật (symlink tới cùng thư mục) → folder sau là `duplicate`, không quét lại.
- Symlink trong root trỏ ra ngoài mọi root không bao giờ được theo (cả list, đọc lẫn ghi: `ErrPathOutsideProject`).
- Giới hạn: index của folder ngoài (vd. analyzer Java quét mọi `src/main/java` bên dưới) có thể **cũng** đọc phần
  folder lồng, nên một entry point có thể xuất hiện ở cả hai folder. Chưa lọc ở tầng index.

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
- **Đổi primary**: `WorkspaceSetPrimary` → `servicesChanged()`; chỉ badge primary đổi. Thứ tự folder trong
  sidebar **không** phụ thuộc primary (`displayOrder`: theo tên, rồi đường dẫn) — dịch vụ liệt kê primary
  trước cho agent, và trước đây điều đó làm cả Explorer xáo lại mỗi lần đổi primary.
- **Back/Forward** đi qua cả *file* lẫn *flow* của cùng một editor group: mở một flow từ một file là một bước
  (`NavPlace.flow`), Back về đúng vị trí con trỏ/scroll của file, Forward về flow (mở lại tab cũ nếu còn).
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
| `WorkspaceSearchSymbols(workspaceId, folderIds, query)` | tìm symbol trong index trên đĩa của từng folder (palette, `@`) |
| `WorkspacePlan*(workspaceId, folderId, …)` | Plan theo folder; không gộp giữa folder |
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

## 8. Đo thực tế

Môi trường mọi số dưới đây: Linux 6.18 (VM), 4 vCPU Xeon 2.8 GHz, 16 GiB, Go 1.27.1, bun 1.4.2, Chromium
headless (Playwright 1.56) nói chuyện với server mode (`-tags server`) trên loopback. **Không phải GUI desktop
(WebKitGTK/WKWebView)**: số của trình duyệt cho biết app làm gì, không phải app tự cảm thấy nhanh cỡ nào trên máy
người dùng.

### 8.1 Backend: tìm kiếm (`go test ./internal/wssearch/ -bench WorkspaceSearch -benchtime 6x`)

Fixture: `internal/wssearch/bench_test.go` — 6 root, **10.002 file** (orders và billing **trùng hệt relative path**,
một folder **lồng** 500 file trong `platform`, một dòng 2 MB, một file 20k dòng đều khớp), mỗi file 40 dòng, không
phải repo git (đi đường walk). Tên metric đúng như `go test` in ra:

| Ca | ns/op | B/op (cấp phát cả đời) | allocs/op | peakHeapMiB (HeapAlloc lấy mẫu mỗi 0,5 ms) | liveHeapMiB-after (sau GC) | gcs/op | gc-pause-ms/op |
|---|---|---|---|---|---|---|---|
| literal hiếm, quét hết | 930,6 ms | 94,3 MB | 195.724 | 7,4 | 0,34 | 51 | 22,4 |
| literal phổ biến, chạm trần 2.000 match | 155,2 ms | 10,3 MB | 45.598 | 7,1 | 0,74 | 3,7 | 1,3 |
| regex | 170,6 ms | 11,4 MB | 47.797 | 9,1 | 0,75 | 3,8 | 1,1 |
| whole-word | 143,2 ms | 9,8 MB | 44.564 | 10,4 | 0,65 | 2,8 | 1,1 |
| file dày match + dòng 2 MB (trần 100k) | 814,4 ms | 95,0 MB | 198.562 | 10,6 | 0,57 | 53,7 | 22,7 |
| folder lồng, quét hết | 936,7 ms | 94,5 MB | 195.721 | 8,8 | 0,42 | 52 | 23,5 |
| chỉ một folder (2.001 file) | 306,0 ms | 22,3 MB | 38.936 | 7,5 | 0,38 | 8,7 | 4,9 |

- `files-searched` = 10.002 ở ca folder lồng: file của folder lồng được quét **một lần**, không hai.
- Cấp phát cả đời là ~94 MB/lượt nhưng **gần hết là rác ngắn hạn** (mỗi file đọc rồi bỏ): heap sống lúc cao nhất
  khoảng 7–11 MiB. Hai con số này khác nhau về bản chất; con số "15 MiB heap" ở báo cáo trước là `HeapSys` cộng dồn
  qua các ca (bộ nhớ lấy từ OS), **không phải** heap sống của một lần tìm — đã bỏ.
- RSS đỉnh của tiến trình test (đọc `VmHWM` mỗi 50 ms, nên là cận dưới): 18 MiB khi chạy ca quét hết, so với 13 MiB
  chỉ dựng fixture → một lần tìm thêm chừng 5 MiB RSS.

**Huỷ (backend)** — `TestCancellationStopsAMidFlightSearch`: chỉ tính lần huỷ rơi vào lúc `Run` còn đang chạy (trả
`context.Canceled`). Từ lúc `cancel()` tới lúc `Run` trả về: nhỏ nhất ~20 µs; lớn nhất 43 / 60 / 101 ms trong ba
lần chạy. Con số cũ "~8 µs" là một lần đo hỏng (huỷ đúng lúc worker đang rảnh) và không được dùng. Nguyên nhân
phần đuôi, lấy từ goroutine dump: worker đang ở `utf8.Valid` của file 2 MB; **công việc trên một file không ngắt
được**, nên độ trễ huỷ ≤ thời gian quét file lớn nhất đang chạy (trần `MaxFileBytes`) cộng tranh chấp CPU.
`TestQueryChurn`: 12 lần tìm cách nhau 8 ms, chỉ lần cuối chạy xong, goroutine 2 → 2 (không rò).

### 8.2 Trình duyệt thật (workspace "Bench": cùng hình dạng, 6 folder qua UI)

| Việc | Kết quả | Ghi chú |
|---|---|---|
| Đổi workspace → Explorer đủ 6 root, có số file | 538 / 729 ms | từ lúc bấm menu; gồm list file + git state của 6 folder |
| Gõ query hiếm → thấy "3 matches in 3 files" | 861–962 ms | **gồm 250 ms debounce**; quét ~650 ms |
| Query phổ biến → "2000 matches" | 413–507 ms | gồm debounce |
| `needle` (file dày + dòng 2 MB) | 810–862 ms | gồm debounce |
| Gõ query B khi A đang chạy → B hiện trên màn hình | 0,95–1,1 s sau lúc gõ | = debounce + quét B; A **bị dừng ngay lúc gõ** (xem dưới) |
| Gõ query mới → bắt đầu yêu cầu huỷ job cũ | ~25 ms | trước khi sửa là sau debounce (~275 ms); `setSearchOptions` giờ dừng job đang chạy ngay |
| `WorkspaceSearchCancel` → cuộc gọi cũ được trả lời | ~8 ms | đo trực tiếp qua RPC loopback (150 → 160 ms), trả `operation_cancelled` |
| Gõ trong editor (khung hình thứ 2 sau keydown) | p50 28 ms, p95 36 ms (Files, 10k file); p50 32 ms, p95 51 ms (kết quả tìm 500+ dòng đang mở); p50 28 ms, p95 44 ms (tab Entries) | 40 phím mỗi ca; bị chặn bởi nhịp khung hình (~16,7 ms × 2) |
| DOM sidebar thay đổi qua 40 phím | 3 (Files) / 0 (Search) / 14 (Entries) | phần lớn là chấm "chưa lưu" lúc phím đầu; sửa một file **không dựng lại** cây hay danh sách kết quả |
| Đổi tab giữa các file của nhiều folder | click → 2 khung hình ≈ 16 ms (p50, đo trong trang) | chỉ là lần vẽ đầu, không phải "CodeMirror đã dựng xong mọi thứ" |

Giữ nguyên theo vòng đời hiện có (đã kiểm): scroll, dòng hiện tại và lịch sử undo của một buffer còn sau khi
chuyển tab đi rồi về.

Số **không** phải hiệu năng tương tác: `FolderView` 20 ms / FilesTab SSR 93 ms ở bản trước là thời gian dựng
model/chuỗi HTML trong bun, không phải thời gian vẽ trong trình duyệt; giữ lại như đo model, không dùng làm số UI.

### 8.3 Reindex với analyzer thật (Java qua JDK 21 + jar; TypeScript qua `tsgo`)

Fixture `scripts/codegraph-index-fixture.mjs`: `java-a` và `java-b` (mỗi cái 400 file Java, controller → service →
repository), `ts-svc` (601 file TS). Tạo workspace tự xếp hàng cả ba; đo bằng `WorkspaceReindex` + lấy mẫu `ps` mỗi 50 ms.

| | Kết quả |
|---|---|
| Lô 3 folder, một lời gọi | 10,1 s: java-a 4,1 s → java-b 4,1 s → ts-svc 1,9 s, **tuần tự** |
| Đồng thời | tối đa **1** tiến trình analyzer Java và 1 `tsgo` tại một thời điểm |
| RSS đỉnh | server 100 MiB (nghỉ 95); server + tiến trình con **423 MiB** |
| Huỷ lúc 3 s (Java đang chạy) | tiến trình analyzer biến mất sau ~20 ms kể từ khi lời gọi huỷ trả về; index cũ giữ nguyên (`indexed`) |
| Mất jar (không có toolchain) | java-a, java-b `failed` sau ~110 ms, **ts-svc vẫn index xong** (2,1 s cả lô): lỗi một folder không huỷ lô |

Chưa đo: project thật lớn (≥ 1.500 file Java đã có số trong `AGENTS.md`), Lua, và nhiều workspace cùng lúc.

## 9. Nghiệm thu bằng UI thật

Chạy bằng server mode + Chromium (Playwright), fixture `scripts/codegraph-fixture.mjs` (gateway TS + service Java
+ folder lồng + shared-lib + platform-config không analyzer + folder tên dài, `src/config.ts` có ở **bốn** folder,
CRLF, Unicode ngoài BMP, dòng 200 KB, symlink thoát khỏi root, thay đổi git ở nhiều repo). Kịch bản tìm toàn
workspace → mở hai file trùng tên → sửa một file → đổi primary → mở flow folder khác → Back → đổi file bên ngoài →
Compare → Save: **đạt 19/19 bước** (script chạy lại được; mỗi bước kiểm DOM/đĩa thật).

Lỗi tìm ra khi nghiệm thu (đều đã sửa, có test):

1. **Module Code Graph bị vô hiệu hoá** ("Module metadata is unavailable or out of sync"): danh sách lệnh phía Go
   (`internal/app/app.go`) còn tên cũ sau khi manifest đổi lệnh; test parity chỉ so *views*. Giờ so cả *commands*.
2. **Reindex bị `permission_denied`** ở UI: scope `service:reindex` chưa có grant. Test mới gọi mọi method
   `Workspace*` bằng grant thật (`TestEveryWorkspaceBridgeMethodIsGrantedToTheUI`).
3. **Watcher mù sau khi thư mục bị xoá rồi tạo lại** (đổi branch, build sạch): `docwatch` giữ watch cũ. Giờ gắn lại
   (thử lại theo chu kỳ và ở lần `Set` kế tiếp); hai test mới.
4. **Explorer xáo thứ tự khi đổi primary** (primary đứng đầu): giờ theo tên.
5. **Back không có ở flow** và mở flow không là một bước lịch sử: thêm `NavPlace.flow` + nút Back/Forward.
6. Hit tìm kiếm muộn trong dòng dài bị cắt mất (hàng hẹp cắt bên phải): cắt phần đầu thay vì phần cuối.
7. Hai `Unhandled rejection in cancelled promise` mỗi lần đổi query: không huỷ phía Wails nữa, chỉ huỷ theo request ID.
8. Tóm tắt kết quả không nói file bị cắt ở 500 match; header cắt "6 fo…"; Outline đổi chiều cao làm nhảy cây;
   tab của folder đã gỡ / của workspace khác không có nhãn để phân biệt.

Đã kiểm trực tiếp (xem 8): đổi primary không đổi Save target/tab/draft; xung đột khi file đổi dưới draft (Compare,
Keep mine không mở khoá Save, Overwrite chỉ khi chủ ý; banner hiện sau ~0,2 s); gỡ folder có draft → draft còn
trong editor, Copy / Save a copy, Save bị từ chối; thêm lại cùng đường dẫn → folder ID khác, draft cũ **không** gắn
vào; đổi workspace giữa chừng không rò kết quả; Peek Definition (Alt-F12), References (Shift-Alt-F12, panel còn
khi chọn kết quả), Esc trên Peek trả focus về editor; filter che hết một root có giải thích + Reset; giao diện sáng
(đặt `data-theme=light`, không đi qua màn Settings) và viewport 900 px. **Chưa kiểm**: điều hướng bàn phím trong cây,
Esc/focus của picker và dialog, kéo đổi cỡ panel, split editor bằng chuột, sidebar rất hẹp.

## 10. Phục hồi draft: phạm vi thật

Buffer chỉ nằm **trong bộ nhớ của tiến trình**; không có draft nào được ghi xuống đĩa hay `ui_state`.

| Tình huống | Draft còn không |
|---|---|
| Đóng tab (buffer có sửa) | Còn (buffer giữ, mở lại file ra đúng bản sửa; dấu ● ở cây). Không có danh sách "các draft đang treo" — phải biết mở file nào |
| Đổi workspace trong phiên | Còn; tab của workspace cũ giữ owner, vẫn sửa/lưu được (Go tra membership theo `workspaceId` trong key) |
| Gỡ folder khỏi workspace trong phiên | Còn, ở trạng thái *orphaned*: xem, Copy, Save a copy (hộp thoại lưu của OS), Discard; Save bị từ chối |
| Khởi động lại app / app crash / tắt máy | **Mất.** Tab được khôi phục (params lưu trong `workbench.tabs`) nhưng buffer đọc lại từ đĩa |

"Recoverable" ở các tài liệu trước nghĩa là *trong phiên*, không phải phục hồi sau crash. Chưa có cảnh báo khi đóng app
với draft chưa lưu. Persist draft cần một thiết kế riêng (mã hoá bằng vault, vòng đời, dọn rác); không làm trong đợt này.

## 11. Giới hạn còn lại

- **Chưa kiểm ở GUI desktop** (WebKitGTK/WKWebView): mọi kiểm UI ở trên là Chromium + server mode. Agent Hub không
  chạy ở server mode: "Open agent here" kiểm tới bước draft (cwd, workspace, không đổi primary, không gửi prompt),
  chưa kiểm một session thật chạy và không bị đổi cwd/quyền — mã chỉ đặt draft, không gọi API session.
- "Open agent here" **có** (menu chuột phải của folder/file trong Files) → `openNewAgentSession({cwd, workspaceId,
  defaultFolder:false})`. Chưa đính kèm file/selection vào draft (draft request chưa có chỗ cho nó; ngoài phạm vi).
  Agent Hub vẫn dùng `codeGraphProjectRoot()` làm folder dự phòng khi không có `workFolder`/recent — dấu vết cuối
  cùng của "project đang mở" ngoài CodeGraph, chưa gỡ.
- Terminal vẫn chạy ở thư mục của primary/folder hiện mở; chưa theo folder của document đang focus.
- Search không bao gồm buffer chưa lưu (UI ghi rõ). Outline theo index nên không phản ánh edit chưa lưu.
- Không có definition/references giữa hai folder; không có đồ thị call xuyên service ngoài `crossservice`.
- Index của folder ngoài có thể đọc cả folder lồng (xem 3.1).
- Hủy search: độ trễ đuôi bằng thời gian quét file lớn nhất đang chạy (xem 8.1).
- AppImage/MSIX vẫn không đóng gói analyzer (xem `AGENTS.md`); watcher dựa vào focus-return trên mount mạng.
- Test timing `debouncedWriter` và một test Agent Hub timeout có sẵn từ trước, không liên quan; thỉnh thoảng đỏ khi
  chạy cả bộ song song.
