# DevKit — Tiến độ rebuild UI/UX

Cập nhật: 2026-10-05.

File này theo dõi đợt rebuild UI/UX và được cập nhật sau mỗi bước triển khai. Quy tắc thiết kế nằm tại [devkit-ui-ux-rules.md](./devkit-ui-ux-rules.md).

## Mục tiêu

- UI mềm, bo góc, màu sắc liền mạch; kết hợp sự tiết chế của mẫu đọc sách và cấu trúc workspace của mẫu node studio.
- Ưu tiên thao tác nhanh, chuyển module mượt và giữ trạng thái công việc.
- Giữ màu component của các theme hiện có trong cả dark/light; nền app có lớp riêng.

## Tiến độ tổng thể

| Bước | Phạm vi | Trạng thái |
|---|---|---|
| Chuẩn bị | Bộ quy tắc hiển thị và tương tác UI/UX | Đã viết v1; là tài liệu đề xuất, chưa có nghĩa toàn bộ app tuân thủ |
| 1 | Nền app và hệ bề mặt shell | Đã triển khai; còn kiểm tra trực tiếp cửa sổ native |
| 2 | Chuẩn hóa feedback: toast, inline error, banner, confirmation, loading | Đã triển khai bộ dùng chung và áp dụng SSH; xem phạm vi bên dưới |
| 3 | Áp dụng quy tắc vào SSH làm module mẫu | Đã polish bố cục, shadcn và keyboard; browser/SSH protocol test đạt; còn native và server SSH bên ngoài |
| 4 | Kafka theo chuẩn shell và feedback | Đã triển khai và kiểm tra browser; còn E2E broker/native |
| 5 | Git: review commit/file và tối ưu tương tác | Đã triển khai; browser và Git trong repository thử nghiệm đạt; còn native/remote thật |
| 6 | Database: console/schema | Đã triển khai; frontend/browser đạt; còn database thật |
| 7 | Database: connection forms/table editing | Đã triển khai; regression và fixture WKWebView đạt; trace đầy đủ/database thật còn pending |

## Bước 1 — Nền app và shell

### Đã triển khai

- Tách `--shell-backdrop` và `--shell-backdrop-top` khỏi background token của theme; dùng gradient nhẹ theo dark/light.
- Giữ fill `surface-base`, màu control, editor, terminal và status của theme hiện có.
- Pane bo góc tối thiểu 12px, giữ tính toán cho radius cửa sổ lớn hơn và xử lý squircle hiện có trên macOS.
- Bổ sung bóng nhẹ theo mode cho pane; sidebar hòa vào app shell, không nền/border/shadow riêng.
- Sidebar dùng nền opaque có sẵn của shell, kể cả khi giảm transparency.
- Giữ gutter 4px, kích thước panel và hành vi resize hiện tại.
- Nâng độ sáng nền light sau khi kiểm tra phát hiện một số màu theme giảm nhẹ độ tương phản.

### File liên quan

- `frontend/src/index.css`: token nền shell, material, radius, shadow và fallback transparency.
- `frontend/src/workbench/AppShell.tsx`: áp dụng nền riêng cho shell.
- `frontend/src/components/ui/sidebar-shell.tsx`: sidebar chung hòa vào shell, giữ geometry/resize.
- `frontend/src/workbench/panelLayout.ts`: cập nhật mô tả geometry.
- `frontend/src/theme/themeContrast.test.ts`: kiểm tra thêm hai đầu gradient và nền sidebar cho mọi theme family.
- `frontend/src/workbench/step4bComponents.test.tsx`: cập nhật contract shell theo thiết kế mới.

### Kiểm tra đã hoàn thành

- Frontend production build, TypeScript và performance budget: **pass**.
- Theme, panel layout, shell component contract và squircle: **529 pass, 0 fail** trên 6 file test.
- Kiểm tra hình ảnh shell preview trong trình duyệt với DevKit và Catppuccin, ở cả dark/light: **đã thực hiện**.
- `git diff --check`: **pass**.

### Giới hạn và việc còn lại

- Preview dùng dữ liệu mẫu và layout kiểm tra material; không thay thế kiểm tra toàn bộ app đang chạy.
- Chưa kiểm tra trực tiếp bo góc, shadow và native window trên macOS.
- Bước này không thay vòng đời module hoặc animation chuyển module; UX liên module cần kiểm tra trong các bước tiếp theo.
- Chưa chuẩn hóa feedback hoặc redesign nội dung từng module.

## Bước 2 — Feedback và trạng thái SSH

Chi tiết rà soát, contract và backlog: [devkit-feedback-audit.md](./devkit-feedback-audit.md).

### Đã triển khai

- Bộ dùng chung: `InlineFeedback`, `FeedbackBanner`, `PendingStatus`, `ConfirmDialog` và helper `notify`.
- Shell Sonner giới hạn ba toast thường và bật nút đóng; helper dùng 4 giây mặc định, 6 giây cho action/error và ID để gộp thông báo.
- SSH tạo/sửa và tunnel validation nằm dưới field, có aria-invalid/description và focus field đầu tiên; không đổi input số rỗng thành 0 khi đang nhập.
- Lỗi thao tác phân theo vùng/tab; host-key failure chỉ nằm trong dialog approval. Lỗi tải dữ liệu có banner và Retry.
- Delete confirmation dùng AlertDialog, Cancel nhận focus; failure giữ dialog mở, pending ngăn đóng/submit lại.
- Giữ command, output, tab và draft tunnel theo profile trong bộ nhớ kể cả khi view bị unmount. Không persist command/output vào UI preferences.
- Khóa thao tác đồng bộ chống double submit; background completion/failure có một toast dẫn về connection/tab, không chứa command/output.
- Terminal exit có notice tại connection và Reconnect; đóng tab chủ động không sinh notice mất kết nối.

### Kiểm tra

- Bộ kiểm tra liên quan cuối cùng: **534 pass, 0 fail** trên 8 file, gồm 5 test mới về async state/validation và accessibility semantics của feedback.
- Browser harness dùng component SSH thật với bridge mô phỏng: field error/focus, safe Cancel focus, delete failure, module unmount trong lúc chạy command, giữ output và toast action Open connection, tunnel failure ở light mode.
- Frontend production build/TypeScript/performance budget: pass.
- Architecture Go: pass.
- Kiểm tra trước PR: toàn bộ frontend **2514 pass, 0 fail**; build/performance budget pass. Đã ổn định query defaults và cập nhật test passphrase theo vị trí form mới; regression Go SSH/app/bridge/MCP/architecture pass.

### Việc còn lại

- Chưa migrate toàn bộ module sang wrapper mới; backlog cụ thể nằm trong audit.
- Chưa E2E các luồng feedback mới trên server SSH thật hoặc native GUI; terminal disconnect/reconnect cần kiểm tra thực tế.
- Chưa thêm OS notification hoặc bố trí ưu tiên toast so với thẻ quyết định của agent.

## Bước 3 — SSH làm module mẫu

### Đã triển khai

- Connection rail thuộc manifest `sidebar` của SSH, độc lập với document đang mở; vẫn có danh sách khi đóng mọi tab. Dùng SidebarHeader/Search/Section chung và sidebar của shell.
- Mỗi connection là một document workbench với `params.profileId`; dùng tab strip chung qua `tabs: true`. Pin/reorder/split/close và persistence thuộc shell; bỏ openIds/selectedId và strip connection riêng. Tabs Terminal/Commands/Tunnels/Details bên trong connection tiếp tục dùng shadcn Tabs.
- Nút + của strip mở form tạo connection; sidebar mở/focus document tương ứng. Đổi tên cập nhật title, xóa profile đóng document của profile đó. Đóng document giữ draft và terminal; đóng terminal riêng mới kết thúc session.
- Dùng Field/FieldLabel/FieldError/FieldSet/FieldLegend cho form; accessible name lấy từ label hiển thị, click label focus đúng control.
- Dùng Badge cho môi trường/agent target, Card cho thông tin kết nối, Item cho session, Empty/EmptyState cho vùng trống và ActionTooltip cho icon action. Giữ palette component của theme.
- Command/tunnel form đổi bố cục theo chiều rộng pane; phần tab có thể cuộn ngang khi hẹp. Loading/error của danh sách không hiển thị nhầm CTA first-run.
- Giữ tab, command/output và tunnel draft qua chuyển connection/module bằng store hiện có.
- Tách lý do terminal exit: mất transport với code -1 hiển thị reconnect, đóng tab chủ động không báo mất kết nối. Callback cũ chỉ nhận code vẫn tương thích.

### Kiểm tra

- Toàn bộ frontend: **2519 pass, 0 fail**; production build/TypeScript/performance budget: **pass**.
- `go test -race ./internal/ssh/ ./internal/arch/ -count=1`: **pass**. Bổ sung test transport bị ngắt và mở PTY mới với cùng profile/pinned key, dùng SSH peer loopback trong test.
- Browser harness với SSH component thật và bridge mô phỏng: dark/light tại 1080×800, pane 560px với sidebar 256px; tunnel form xếp một cột, Arrow/Home/End/Delete và focus sau đóng tab, giữ command draft qua unmount module, label focus/accessibility.
- Bỏ fill tint và inset highlight của sidebar chung theo yêu cầu; cập nhật quy tắc và contrast checks để rail dùng nền shell ở cả dark/light. Browser preview hai mode và 488 kiểm tra theme/shell đạt; production build/performance budget đạt.
- Kiểm tra browser bổ sung với `SidebarSurface` thật và bridge mô phỏng: toggle sidebar, kéo resize, đổi module và giữ tìm kiếm khi quay lại SSH đều đạt.
- Tab strip migration: 4 regression test về dedupe/active tab, focused split group, giữ draft/terminal khi đóng document và rename giữ pin. Browser harness dùng TabStrip/SidebarSurface thật, bridge mô phỏng: hai connection, arrow navigation, context menu pin/close, mở lại giữ draft, đổi module và nút + mở form; dark/light đều đạt. Dialog áp dụng visibility của document để không xuất hiện từ tab nền.
- Screenshot kiểm tra được lưu ngoài repository; harness chỉ tạm thời và đã dọn sau kiểm tra.

### Giới hạn

- Browser harness không chạy terminal PTY/native thật; protocol test dùng SSH peer trong test, chưa xác minh với server SSH bên ngoài.
- Cần smoke test native macOS: terminal focus/resize, session/tunnel khi chuyển module, host-key thay đổi và bo góc cửa sổ.

## Tài liệu tích hợp cho contributor

- Đã viết [module-shell-integration.md](./module-shell-integration.md): ownership, manifest sidebar/useSidebarContent, document helper, lifecycle/dialog, lỗi cần tránh và checklist review.
- `AGENTS.md`, README và UI/UX rules dẫn về hướng dẫn; trang Sidebar/Module tabs đánh dấu mô tả lịch sử và chỉ rõ contract hiện tại.
- Thay đổi này chỉ là tài liệu, không đổi runtime. Đã kiểm tra 24 đường dẫn local/code fences trong 7 file tài liệu và `git diff --check`: đạt.

## Bước 4 — Kafka theo chuẩn shell và feedback

### Đã triển khai

- Sidebar thuộc manifest, dùng rail trong suốt của shell và vẫn tồn tại khi explorer đóng; form tạo cluster thuộc rail, giữ draft và chỉ hiện khi Kafka đang active.
- Kafka giữ một explorer vì backend chỉ có một connection. Topics/Publish/Consumer groups là shadcn Tabs bên trong document; không dựng strip hoặc giả lập nhiều connection.
- Thay input/select và option tabs tự dựng bằng Input, NativeSelect và Field/FieldLabel/FieldError/FieldSet. Environment phải chọn rõ ràng; bootstrap host:port, limit/offset và partition được validate trước khi gọi bridge.
- Loading/error/no-results có scope và Retry/Clear search. Xóa profile dùng ConfirmDialog, Cancel nhận focus, pending chặn gửi lại/đóng, lỗi giữ dialog mở.
- Header ghi cluster đang kết nối; chọn cluster khác chỉ đổi selection và đưa ra action Connect rõ ràng. Connect thất bại giữ nguyên connection đang chạy. Không đổi cluster trong lúc publish.
- Draft publish giữ riêng theo cluster trong bộ nhớ; chọn topic khác không tự retarget payload. Chống publish lặp từ nút hoặc shortcut; khóa field trong lúc gửi; receipt/error giữ khi đổi module, completion nền dùng toast ID ổn định không chứa payload.
- Hủy peek/đổi topic/cluster vô hiệu kết quả cũ; lỗi describe cũ cũng bị bỏ qua. Topic và consumer groups tải độc lập. Refresh thất bại giữ bảng trước đó cùng banner; inspector giữ partition của kết quả đã đọc.
- Header/actions/form wrap theo pane; bảng message có scroll ngang và giữ virtualization. Message row nhận Enter/Space; Tabs giữ keyboard của primitive.
- Sửa hai empty state SSH đã merge trước đó về EmptyState để tuân thủ conformance của repository.

### Kiểm tra

- Toàn bộ frontend: **2546 pass, 0 fail**; production build/TypeScript/performance budget: **pass**.
- `go test ./internal/arch/ ./internal/kafka/ -count=1`: **pass**.
- Bổ sung 12 test validation/lifecycle: duplicate publish, canceled/stale peek, stale describe/cluster metadata, connect thất bại, draft theo cluster, partial delete failure, groups failure độc lập và giữ rows khi peek lỗi.
- Browser harness dùng KafkaView, manifest sidebar, SidebarSurface, CodeEditor, ThemeProvider và Toaster thật với bridge mô phỏng: dark/light; 860×800 với pane ~542px; tab Arrow/End, message Enter, explicit target; form validation/focus/pending/failure; safe Cancel focus và delete failure; publish qua module unmount + toast nền; search topic chỉ match cluster sở hữu, no-results có Clear search.
- Screenshot lưu ngoài repository; tab/viewport override/harness đã được dọn sau kiểm tra.

### Giới hạn

- Browser harness và Go test không thay thế E2E UI với Kafka broker thật/native desktop. Chưa kiểm tra protocol credentials hoặc cluster production.
- Smoke test native SSH từ bước 3 vẫn pending: cần vault mở và endpoint thử nghiệm để xác minh PTY focus/resize, tunnel, host-key đổi. Không dùng connection cá nhân làm fixture tự động.

## Bước 5 — Git: review commit và tối ưu UX

### Đã triển khai

- Sửa `withGitFolder` để chuyển tiếp document params; tab commit đọc đúng SHA thay vì rơi về working tree. Tiếp tục dùng manifest sidebar và workbench tab strip chung.
- History mở được commit, tìm theo message/author/SHA/body, tải thêm từng 100 commit tới giới hạn 500. Giữ search và giới hạn tải theo repository trong phiên.
- Diff header gọn, message body thu gọn, hiển thị author/date/SHA/task và thống kê. Copy SHA/path có feedback; file list có filter, previous/next và mở thêm khi vượt 200 file.
- Pane rộng có file panel resize bằng shadcn Resizable; pane dưới 760px dùng Popover file picker, mặc định unified. Split/unified là ToggleGroup, lựa chọn của người dùng được giữ trong phiên. Không dựng sidebar hoặc strip riêng.
- File diff dùng shared DiffPane/DiffSplitPanes và syntax highlighting; loading/error/Retry/binary/empty có scope. Không hiển thị diff file cũ dưới tên file mới. Render đầu 1500 dòng, có Show more.
- Backend so sánh commit với first parent; root commit so với empty tree. File statistics và patch cùng một cách so sánh, không dùng combined merge diff. Literal pathspec bảo đảm tên chứa dấu `[]` chỉ lấy đúng file.
- Review staged dùng endpoint đọc index riêng, không lấy nội dung unstaged. Hoạt động cả trước commit đầu tiên; Review unstaged dùng cùng viewer.
- Gom stash/restore/protection vào menu phụ, giữ Fetch/Pull/Push trên toolbar. Stash/restore/abort merge dùng ConfirmDialog, Cancel nhận focus, pending khóa gửi lại/đóng, failure giữ dialog và chỉ hiện lỗi tại vùng quyết định.
- Draft commit giữ theo repository trong bộ nhớ, không persist vào UI preferences; chỉ xóa sau commit thành công. Shared operation lock nhận closure để chặn gọi bridge trùng, dùng cho stage/hunk/commit/branch/stash/remote/conflict. Khóa picker của Git trong lúc write đang chạy.
- Kết quả/error giữ khi rời document; một toast nền có ID theo repository dẫn về Git. Feedback của thao tác không lặp giữa sidebar và document đang xử lý.
- Dùng Button, Field, Input, Textarea, Badge, DropdownMenu, ScrollArea, Popover, Collapsible, ToggleGroup và Resizable chung. Git không còn nợ typography scale; bỏ các `text-fg-muted` không có token.

### Kiểm tra

- Toàn bộ frontend: **2551 pass, 0 fail**; TypeScript/production build/performance budget: **pass**.
- `go test ./internal/git/ ./internal/gitparse/ ./internal/bridge/ ./internal/app/ ./internal/arch/ -count=1`: **pass**. Regression cuối cùng Git/gitparse/architecture cũng đạt.
- Test mới: params qua wrapper; serialize writes cả giữa repositories, giữ draft/error scope/retry, filter history và clear feedback không nhả lock pending. Go dùng Git thật trong temp repositories: root/merge first-parent, literal paths, staged khác unstaged, staged trước commit đầu tiên và từ chối option/range trong revision.
- Browser harness dùng views qua manifest/wrapper, SidebarSurface, TabStrip, ThemeProvider và Toaster thật với bridge mô phỏng: History → commit → file; filter/Clear, next file, split/unified, binary/root; pane rộng và 860×800 với pane ~558px; file picker, staged review, keyboard resize; draft qua module unmount, double-click chỉ gửi một commit, toast lỗi nền; confirmation safe focus/pending/failure và một lỗi trong dialog.
- Screenshot dark rộng/light hẹp lưu ngoài repository; harness/tab/viewport tạm đã được dọn sau kiểm tra.

### Giới hạn

- Browser harness mô phỏng bridge. Go tests chạy Git thật trong repository thử nghiệm, chưa thay thế smoke native Wails, credentials hoặc Fetch/Pull/Push tới remote thật.
- Git vẫn có một repository đang active; các document theo repository của module. Không giả lập nhiều Git sessions độc lập.
- Search History chỉ tìm trong commits đã tải (tối đa 500). Binary không có text preview; không thêm stash browser, graph branch, blame hoặc rebase/cherry-pick trong phase này.
- Form conflict và cấu hình remotes dùng shared lock; chưa chuyển draft merge editor hoặc xử lý lỗi tải accounts/env tags sang lifecycle đầy đủ như composer.

## Bước 6 — Database: console, schema feedback và lifecycle

### Đã triển khai

- Giữ manifest sidebar/workbench tab strip, SQL draft và kết quả theo console trong store hiện có. Document overlays/polling chỉ thuộc focused group và document đang visible.
- Connection/schema hiện trên một hàng riêng, toolbar và result header wrap theo pane. Run/error giữ sát cạnh phải; lỗi mở ra được giới hạn theo chiều rộng pane.
- Split editor/result dùng shadcn Resizable với hit area thật 4px; sửa handle chiều cao 0 khiến thao tác keyboard qua browser không focus được.
- Script giữ kết quả cũ trong lúc statement đầu pending; chỉ thay sau thành công rồi append kết quả tiếp theo. Chặn repeated Run trước khi sửa results/statement marks; Cancel statement đầu giữ kết quả trước đó và dừng cả script.
- Lỗi index schema thuộc sidebar, tồn tại cả khi không có document, có Retry và pending status; bỏ effect toast chạy lại ở mỗi console. Chặn index trùng và loại snapshot/error cũ kể cả đổi A → B → A hoặc cache trả về sau refresh.
- Explain bỏ qua success/error đã superseded bởi đổi connection/run/unmount; plan portal theo visibility và đóng khi target đổi. Run không gửi SQL trong lúc Explain pending.
- Dùng shared FeedbackBanner/PendingStatus/Button cho schema và environment/tunnel banners; giữ palette component dark/light hiện tại.

### Kiểm tra

- Frontend regression: 2580 test đạt, 0 fail; TypeScript/production build/performance budget đạt.
- `go test ./internal/arch/ -count=1`: đạt. Không đổi Go backend/bindings.
- Test lifecycle thêm: giữ rows khi script pending/Cancel, repeated Run, index trùng/Retry, stale schema failure A → B → A, cache cũ không ghi đè schema vừa index, background module không sở hữu overlay.
- Browser harness dùng QueryConsole, CodeEditor, ResultGrid, ThemeProvider và SchemaFeedback thật với bridge mô phỏng: dark/light, 1280×720 và 860×800 (console ~560px), schema loading/error/Retry, lỗi query có full detail, SQL draft/result qua unmount, keyboard resize 50 → 45.

### Giới hạn

- Chưa chạy query/Cancel/Explain với database thật hoặc native Wails. Các fixture browser không thay thế smoke với connection thử nghiệm.
- Giữ kiến trúc một connection active chung cho Database; đổi connection giữ SQL nhưng xóa result/error cũ đúng ownership. Không thêm connection độc lập cho mỗi console.
- Phase này tập trung console/schema feedback, chưa làm lại toàn bộ form connection, ERD, history/export và table editing.

## Bước 7 — Database: connection forms và table editing

### Đã triển khai

- New Connection/Options dùng shadcn Dialog, Field, Input, NativeSelect, Switch và Textarea. Validation gắn field/focus, chọn environment rõ ràng; Options khởi tạo đúng target trước paint, không ghi đè draft khi refetch.
- Credential draft chỉ nằm cục bộ ở form, giữ khi module ẩn và xóa khi đóng rõ ràng/lưu thành công. Module ẩn không sở hữu portal.
- Test connection dùng cancellable bridge thật, có elapsed clock riêng; Cancel mở khóa ngay và bỏ completion cũ. Save/Delete/Apply khóa đồng bộ chống gửi lặp/đóng khi pending, lỗi ở vùng thao tác, giữ draft/staged để Retry.
- Delete dùng ConfirmDialog; Apply dùng AlertDialog/CodeEditor rộng, Cancel nhận focus. Bỏ fetch toàn bộ history/saved queries chỉ để đếm trước Delete.
- Tách WHERE draft khỏi browse state: gõ không render lại grid hoặc phát COUNT; Enter mới áp dụng. Draft ephemeral theo table, xóa khi đóng document. Context-menu filter cập nhật cả draft và applied filter.
- Overlay staged chỉ copy row array và những row sửa, giữ identity các row còn lại. Virtual grid tiếp tục bounded; toolbar wrap theo pane.
- Store giữ write pending/error/status theo table, chặn reload/filter/sort/page/transaction giữa lúc ghi; success cũ không reload/xóa staged của target mới kể cả A → B → A. Chỉ xóa staged sau write được chấp nhận, không cho áp lại vì reload lỗi.
- Rows cũ giữ khi reload pending/error; lần đầu có Loading, failure có Retry. Catalog/page/count effects không khởi phát khi document ẩn. Giữ shell/sidebar/tab strip và palette component hiện có.

### Kiểm tra

- Frontend: **2597 pass, 0 fail**; gồm 17 test mới về validation, row identity/10.000×30, write locking/failure/reload, stale target và filter draft cleanup.
- `go test ./internal/arch/ ./internal/database/ -count=1`: đạt. Không đổi Go backend/bindings.
- Production TypeScript/build/performance budget: đạt, initial JS 1020,7 KiB và CSS 259,7 KiB; không tăng ceiling. Smoothness ratchet blur giảm 35 → 33 sau bỏ hai overlay tự dựng.
- Native Wails/WKWebView production profiling fixture dùng component thật/bridge giả lập: gõ 6 ký tự WHERE, COUNT 6 → 0; max React update 9 → 2 ms; khoảng 40 row DOM trên nguồn 10.000×30. Overlay staged median 41 ms → dưới độ phân giải timer. Chi tiết hardware/runtime/scenario và giới hạn: [database-ui-performance.md](./database-ui-performance.md).
- Native dark/light: validation/focus, probe pending/Cancel/elapsed, Save failure giữ draft; Options đúng target; Apply safe focus/pending/Escape/double-click/failure/Retry; Delete safe focus/pending/failure. Screenshot và harness tạm nằm ngoài repository; source harness tạm đã dọn.

### Giới hạn

- Fixture chạy native frontend, bridge mô phỏng. Chưa xác minh database/network/driver thật, transaction/prod confirmation hoặc Cancel Go context end-to-end.
- Đã lấy React duration/DOM/bridge-call counts trên WKWebView; chưa thu được trace scripting/style/layout/paint/memory đầy đủ từ WebKit Inspector của fixture. Phần này còn pending cùng đo bridge serialize/transfer/parse thật và p95/frame/memory gates; không coi performance audit toàn app hoàn tất.
- Chưa chuyển ERD/history/saved queries/export sang scope phase này; cấu trúc một connection active chung cho Database vẫn giữ.

## Bước tiếp theo

1. Smoke test native SSH và Kafka với endpoint thử nghiệm.
2. Smoke Git native với repository/remote thử nghiệm: stage/commit/hook failure, guarded commit/push, stash conflict và đổi folder trong lúc thao tác.
3. Smoke Database với connection thử nghiệm: Run/Cancel/Test/Explain, đổi connection, direct/staged edits và transaction; thu trace WebKit/bridge thật.
4. Phase UI tiếp theo: ERD, query history/saved queries và export; tiếp tục dùng shadcn/shared feedback, draft lifecycle và performance gates.

## Nhật ký

| Ngày | Cập nhật |
|---|---|
| 2026-10-05 | Phase 7 Database forms/table: shadcn, cancellable probe, scoped writes; WHERE không COUNT theo phím gõ, staged row identity; frontend 2597 test đạt |
| 2026-10-05 | Phase 6 Database: responsive console/schema feedback, giữ result pending/Cancel, chống schema/Explain cũ và focused overlay |
| 2026-10-05 | Phase 5 Git: sửa commit params/root/merge diff, staged review; tối ưu pane, History và async feedback; frontend 2551 test, build và Go regression đạt |
| 2026-10-04 | Viết bộ quy tắc UI/UX v1 và triển khai nền/surface shell của bước 1 |
| 2026-10-04 | Triển khai feedback dùng chung và áp dụng SSH; tạo audit/backlog và kiểm tra browser với bridge mô phỏng |
| 2026-10-04 | Tạo file tiến độ; ghi kết quả kiểm tra và phần native chưa xác minh |
| 2026-10-04 | Phase 4: Kafka dùng manifest sidebar/shadcn/feedback chung, xử lý target và async lifecycle; frontend 2546 test, build và Go Kafka/architecture đạt |
| 2026-10-04 | Phase 3: polish SSH bằng shadcn, keyboard/responsive và phân biệt disconnect với đóng terminal; frontend 2515 test và Go race đạt |

Khi cập nhật: ghi phần thay đổi thực tế, kết quả kiểm tra, giới hạn còn lại và bước tiếp theo; chỉ đánh dấu hoàn thành phạm vi đã triển khai và xác minh.
