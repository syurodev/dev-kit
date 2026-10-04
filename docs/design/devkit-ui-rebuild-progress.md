# DevKit — Tiến độ rebuild UI/UX

Cập nhật: 2026-10-04.

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
| 4 | Mở rộng sang các module còn lại | Chưa bắt đầu |

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

- Đăng ký connection rail qua `useSidebarContent` của app shell; bỏ aside cố định trong workspace. Dùng SidebarHeader/Search/Section chung để sidebar nhận resize, ẩn/hiện và material của shell.
- Thay hai thanh tab tự viết bằng shadcn Tabs: semantics tab/tabpanel, focus roving, Arrow/Home/End và indicator/transition dùng chung.
- Connection tab hỗ trợ Delete để đóng; focus chuyển về connection còn lại hoặc ô tìm kiếm khi đóng tab cuối.
- Dùng Field/FieldLabel/FieldError/FieldSet/FieldLegend cho form; accessible name lấy từ label hiển thị, click label focus đúng control.
- Dùng Badge cho môi trường/agent target, Card cho thông tin kết nối, Item cho session, Empty/EmptyState cho vùng trống và ActionTooltip cho icon action. Giữ palette component của theme.
- Command/tunnel form đổi bố cục theo chiều rộng pane; phần tab có thể cuộn ngang khi hẹp. Loading/error của danh sách không hiển thị nhầm CTA first-run.
- Giữ tab, command/output và tunnel draft qua chuyển connection/module bằng store hiện có.
- Tách lý do terminal exit: mất transport với code -1 hiển thị reconnect, đóng tab chủ động không báo mất kết nối. Callback cũ chỉ nhận code vẫn tương thích.

### Kiểm tra

- Toàn bộ frontend: **2515 pass, 0 fail**; production build/TypeScript/performance budget: **pass**.
- `go test -race ./internal/ssh/ ./internal/arch/ -count=1`: **pass**. Bổ sung test transport bị ngắt và mở PTY mới với cùng profile/pinned key, dùng SSH peer loopback trong test.
- Browser harness với SSH component thật và bridge mô phỏng: dark/light tại 1080×800, pane 560px với sidebar 256px; tunnel form xếp một cột, Arrow/Home/End/Delete và focus sau đóng tab, giữ command draft qua unmount module, label focus/accessibility.
- Bỏ fill tint và inset highlight của sidebar chung theo yêu cầu; cập nhật quy tắc và contrast checks để rail dùng nền shell ở cả dark/light. Browser preview hai mode và 488 kiểm tra theme/shell đạt; production build/performance budget đạt.
- Kiểm tra browser bổ sung với `SidebarSurface` thật và bridge mô phỏng: toggle sidebar, kéo resize, đổi module và giữ tìm kiếm khi quay lại SSH đều đạt.
- Screenshot kiểm tra được lưu ngoài repository; harness chỉ tạm thời và đã dọn sau kiểm tra.

### Giới hạn

- Browser harness không chạy terminal PTY/native thật; protocol test dùng SSH peer trong test, chưa xác minh với server SSH bên ngoài.
- Cần smoke test native macOS: terminal focus/resize, session/tunnel khi chuyển module, host-key thay đổi và bo góc cửa sổ.

## Bước tiếp theo

1. Smoke test native và server SSH bên ngoài theo checklist trên.
2. Bắt đầu bước 4: chọn module tiếp theo từ feedback audit; ưu tiên primitive shadcn sẵn có, giữ draft và lifecycle đang chạy.

## Nhật ký

| Ngày | Cập nhật |
|---|---|
| 2026-10-04 | Viết bộ quy tắc UI/UX v1 và triển khai nền/surface shell của bước 1 |
| 2026-10-04 | Triển khai feedback dùng chung và áp dụng SSH; tạo audit/backlog và kiểm tra browser với bridge mô phỏng |
| 2026-10-04 | Tạo file tiến độ; ghi kết quả kiểm tra và phần native chưa xác minh |
| 2026-10-04 | Phase 3: polish SSH bằng shadcn, keyboard/responsive và phân biệt disconnect với đóng terminal; frontend 2515 test và Go race đạt |

Khi cập nhật: ghi phần thay đổi thực tế, kết quả kiểm tra, giới hạn còn lại và bước tiếp theo; chỉ đánh dấu hoàn thành phạm vi đã triển khai và xác minh.
