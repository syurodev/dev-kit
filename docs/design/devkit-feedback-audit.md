# DevKit — Rà soát feedback và phạm vi triển khai

Cập nhật: 2026-10-05. Tham chiếu: [Quy tắc UI/UX](./devkit-ui-ux-rules.md), [Tiến độ](./devkit-ui-rebuild-progress.md).

## Kết quả rà soát

| Khu vực | Hiện trạng trước thay đổi | Xử lý |
|---|---|---|
| Shell toast | Có Sonner chung, lifetime 4 giây; chưa khai báo giới hạn ba toast và nút đóng chung | Khai báo `visibleToasts=3`, bật close; helper `notify` dùng ID ổn định, action/error 6 giây |
| Alert/Loading | Đã có primitive; Alert có chiều cao cố định, LoadingStatus thiên về vault | Bổ sung wrapper `FeedbackBanner` tự giãn theo nội dung và `PendingStatus` gọn cho vùng thao tác; giữ primitive hiện có |
| SSH form | Lỗi backend inline; validation field phụ thuộc bubble native; port rỗng bị đổi thành 0 | Validation inline, liên kết aria với field, focus field đầu tiên; giữ số dạng draft tới lúc submit |
| SSH actions | Một vùng error/status chung cho mọi tab; host-key error bị lặp ngoài dialog | Feedback có scope connection/commands/tunnels/details/approval, host-key chỉ báo lỗi trong dialog |
| SSH delete | Dialog thông thường; tự autofocus có thể rơi vào hành động xóa | ConfirmDialog dựa trên AlertDialog, Cancel nhận focus; pending ngăn đóng/submit lại; lỗi giữ dialog mở |
| SSH navigation | Draft/output là state cục bộ, bị mất khi đổi connection hoặc module bị unmount | State phiên theo profile trong bộ nhớ; lock thao tác đồng bộ chống submit trùng; output/error còn khi quay lại |
| SSH background | Không có thông báo kết quả khi người dùng đã chuyển đi | Một toast ID theo profile và phạm vi, title chung không chứa command/output, action mở connection và đúng tab |
| SSH terminal | Pane terminal đã hiển thị lỗi start và exit output | Thêm trạng thái session kết thúc ở connection và hành động mở terminal mới; đóng tab chủ động không báo mất kết nối |
| Kafka | Sidebar view-owned, control tự dựng; target selection khác connection, async result cũ và publish lặp | Manifest sidebar, shared fields/feedback/confirmation; explicit target, draft theo cluster, async guards và background toast |
| Git | Wrapper bỏ params commit; History không mở diff, toolbar dày và draft/write state cục bộ | Forward params, first-parent/root/index diff; responsive Resizable/Popover, History search, shared draft/operation lock và scoped feedback |
| SSH query errors | Retry có ở profiles; environments/tunnels chỉ báo text | Banner trong vùng sở hữu, có Retry; no-results có Clear search |

## Contract dùng chung

- `InlineFeedback`: lỗi dùng `role=alert`, success/info dùng `role=status`; không tự tắt.
- `FeedbackBanner`: điều kiện bền vững trong vùng; tự giãn khi wrap nội dung/action, không dùng timer.
- `PendingStatus`: thông báo đang xử lý; spinner không quay khi reduced motion; không giả lập phần trăm tiến độ.
- `ConfirmDialog`: Cancel mặc định, nút hành động cụ thể; không đóng khi đang xử lý; failure vẫn còn để retry.
- `notify`: transient feedback với ID bắt buộc. Success/info 4 giây, action hoặc error 6 giây, có close. Không thêm toast cho thành công đã rõ trong form/nội dung đang xem.
- SSH command, draft tunnel, output và lỗi chỉ ở bộ nhớ trong phiên; không ghi vào UI preferences. Sửa/xóa connection từ SSH UI xóa state cũ của profile đó.

## Backlog cho các module còn lại

- Notes: validation giới hạn tag hiện dùng toast (`NoteEditor.tsx`); cần chuyển về field. Rà soát riêng các lỗi lưu để bảo đảm tìm lại được sau khi toast hết hạn.
- Database: `DatabaseView.tsx` phát toast từ `schemaError`; cần kiểm tra notification lặp khi view remount và khả năng mở lại lỗi indexing.
- Settings: lỗi lưu theme/palette hiện là toast (`SettingsView.tsx`); cần cân nhắc trạng thái inline tại control và semantics revert/retry.
- Các module Tasks, API Client, Agent Hub: chưa migrate toàn bộ; áp dụng cùng bảng quyết định sau khi rà soát luồng cụ thể.
- Git: draft merge editor và lỗi tải accounts/env tags ở Remotes cần lifecycle/feedback đầy đủ; smoke native/remote chưa chạy.
- Permission/question của agent và host-key approval giữ policy hiện tại; không được đổi thành toast tự tắt.
- OS notifications, tự bố trí toast tránh agent decision cards và migration mọi call site chưa thuộc triển khai lần này.

## Xác minh

- Test async: chống submit trùng, kết quả còn sau thay đổi state connection khác, lỗi giữ draft và scope, retry xóa lỗi cũ, forget chỉ tác động đúng profile.
- Test validation: port rỗng/fractional/ngoài range, secret bắt buộc khi tạo nhưng không bắt nhập lại khi edit; listen port 0 hợp lệ.
- Test semantics: alert/status, banner tự giãn.
- Browser harness import component SSH thật, bridge và dữ liệu mẫu: field errors/focus, lỗi xóa giữ dialog, command chạy khi module unmount, toast Open connection dẫn về output, lỗi tunnel light mode.
- Chưa thay thế E2E SSH/network và GUI native bằng harness này; disconnect notice cần kiểm tra thêm trên session SSH thật.

- Kafka phase 4: validation/lifecycle tests, full frontend suite và Go Kafka/architecture đạt; browser fixture dark/light/pane hẹp/keyboard/confirmation/publish nền đạt. E2E broker/native vẫn pending; chi tiết trong file tiến độ.

- Git phase 5: frontend 2551 test/build và Go Git/gitparse/bridge/app/architecture đạt; browser dark/light/pane hẹp, shared strip/rail, staged review, draft/duplicate submit và confirmation đạt. Chi tiết và giới hạn trong file tiến độ.
