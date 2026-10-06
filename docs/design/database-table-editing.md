# Database — typed cell và bulk update

Triển khai: 2026-10-06. Dùng cùng palette/component theme hiện có và shadcn Popover, Dialog, Field, Input, NativeSelect, Textarea. Không tạo sidebar hoặc document tab riêng; xem [module shell integration](module-shell-integration.md).

## Chọn editor theo metadata

Không đoán kiểu từ nội dung một cell. Thiếu metadata, domain/array và kiểu chưa hỗ trợ dùng text editor.

| Kiểu cột | Control | Quy tắc |
|---|---|---|
| Boolean, MySQL `tinyint(1)` | Select true/false | Hiển thị alias `t/f/1/0` tương ứng; SQL dùng TRUE/FALSE |
| Date | Date picker và chuyển sang text | Kiểm tra ngày lịch, gồm năm nhuận |
| Time, timestamp/datetime | Native picker và text | Không chuyển qua JS Date hoặc timezone của máy |
| Timestamp/time có timezone | Picker + trường UTC offset | Offset nhập riêng; giữ trạng thái gõ dở, không tự quy đổi |
| Integer, decimal/float | Text với input hint | Giữ nguyên bigint/decimal; không ép giá trị qua JS Number |
| JSON/JSONB | Textarea | Parse để kiểm tra lúc submit, không stringify lại dữ liệu |
| UUID | Text | Kiểm tra cấu trúc 8-4-4-4-12 |
| Text/kiểu chưa biết | Text | Chuỗi rỗng là giá trị riêng |

Mọi editor có lựa chọn Value/SQL NULL rõ ràng; NULL bị khóa khi metadata cho biết NOT NULL. Không dùng chuỗi `"NULL"` thay SQL NULL.

Native picker không biểu diễn đủ microsecond trên WKWebView đã thử. Giá trị có hơn ba chữ số phần lẻ dùng text ngay và không cho chuyển về picker để tránh mất precision. TIME âm/dài hơn 23 giờ và PostgreSQL ±infinity cũng dùng text. Text temporal hiện kiểm tra dạng ISO thông dụng: năm bốn chữ số, tối đa sáu chữ số phần lẻ và offset Z/±HH/±HH:mm/±HHmm; không hỗ trợ toàn bộ cách viết ngày, BC hoặc tên timezone mà database chấp nhận. MySQL/MariaDB TIME hỗ trợ duration tới 838 giờ; PostgreSQL TIME hỗ trợ 24:00 với phần còn lại bằng zero. Range/precision/constraint cụ thể của cột vẫn do database quyết định.

## Tương tác và lifecycle

- Double-click hoặc keyboard edit mở popover cho cell/range; Enter lưu, Escape hủy. JSON dùng Ctrl/Cmd+Enter để submit.
- Validation giữ editor và draft, báo lỗi tại chỗ. Lưu/đóng trả focus về grid; dialog của module ẩn không giành focus.
- Gõ editor chỉ cập nhật draft cục bộ. Không cập nhật result/store hoặc phát bridge call theo mỗi phím.
- Bulk dialog giữ draft qua việc ẩn/hiện module; đổi connection/page/run xóa selection/editor theo scope để không stage lên dữ liệu khác.
- Boolean và temporal no-op được bỏ qua mà không làm tròn số hoặc đổi timezone.

## Update hàng loạt

Chọn vùng bằng keyboard/Shift; gutter chọn nguyên hàng và Shift kéo dài vùng hàng. Toolbar cho biết số hàng/cell, chọn toàn bộ **page đã tải**, bỏ chọn và mở Edit selected rows. Không ngầm update các hàng chưa tải hoặc tất cả hàng khớp filter.

Dialog cho thêm nhiều cột, mỗi cột một giá trị/NULL riêng, không cho trùng cột. Cột ngoài khóa được ưu tiên khi thêm assignment. Validation tất cả assignments trước khi stage; lỗi đưa focus về value editor tương ứng. Một lần bulk tối đa 10.000 cells và 1 MiB giá trị UTF-8 sau khi nhân số hàng; kiểm tra trước khi mở rộng draft. Giới hạn này không thay query/backend ceilings.

Bulk luôn stage rồi review SQL trước khi write, kể cả connection không yêu cầu review direct edit. SQL gom tất cả cột chỉnh của một hàng vào một UPDATE, WHERE dùng khóa gốc. Điều này cho phép đổi nhiều phần của composite key cùng nhau và giữ no-match check riêng cho từng hàng; không gom các hàng vào một WHERE OR khiến hàng bị xóa khó phát hiện.

ApplyEdits ở chế độ Auto giữ transaction/rollback hiện có. Trong Manual transaction, các statement thành công thuộc transaction đang mở; người dùng vẫn chịu trách nhiệm Commit/Rollback nếu statement sau lỗi. Read-only, thiếu primary key, pending operation và environment confirmation tiếp tục dùng guards hiện có.

## Kiểm chứng và phần còn lại

Frontend: 2.655 test đạt, typecheck/build và performance budget đạt. Go architecture/database đạt. Live test `TestMySQLTypedEditMetadataAndValues` và `TestApplyEditsMySQL` đạt trên MariaDB 12.3 riêng, gồm boolean metadata, leap date, DATETIME(6), TIME duration, decimal precision và transaction apply. Không coi đây là test MySQL 8 hoặc PostgreSQL thật.

Native Wails/WKWebView fixture đã thử typed cell, validation, offset gõ dở, selection/bulk nhiều cột và focus; browser bổ sung light theme, limits và draft khi ẩn/hiện module. Fixture UI dùng bridge mô phỏng. Xem [bằng chứng và giới hạn performance](database-ui-performance.md); trace WebKit và native bridge/database E2E đầy đủ còn pending.
