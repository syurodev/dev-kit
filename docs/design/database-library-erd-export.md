# Database phase 8 — Query library, ERD và Export

Triển khai: 2026-10-06. Giữ shared sidebar/workbench tabs và theme component hiện có. Không thêm dependency hoặc tăng performance ceiling.

## History và Saved queries

- Dùng một query-library surface chung. Row cố định 88px, preview plain text tối đa 300 ký tự; danh sách trên 100 rows dùng shared virtual rows. Chỉ SQL preview đang mở mới highlight, tối đa 12.000 ký tự. Copy/Open dùng toàn bộ SQL.
- Search có state local, phần filter defer để input phản hồi ngay. Tìm theo name hoặc nội dung SQL; no-match khác danh sách trống và có Clear. Badge giữ chỗ và không báo zero khi chưa tải.
- Cache theo connection, không dùng placeholder của connection trước. Same-target refresh giữ danh sách khi pending/error, có Retry. Không có connection thì hướng dẫn chọn connection, không loading vô hạn.
- Saved rail chỉ render 50 entries đầu và dẫn tới full list. Query observer của rail/history/saved/ERD được gate theo visibility/activity phù hợp; không polling nền.
- SQL preview chỉ đọc. Open in console không chạy SQL, dùng document helper/ID ổn định; mở lại console đã sửa không ghi đè draft. Saved view có New SQL console thay CTA Save Current Query không rõ console đích.
- Delete dùng ConfirmDialog, Cancel nhận focus; lock đồng bộ chống gửi lặp/đóng/đổi intent khi pending. Intent giữ ngoài view mounting, error giữ đúng connection/query để Retry; invalidate captured connection khi thành công.

## Save và Export

Snapshot/draft nằm trong store ephemeral riêng, không persist SQL/results vào preferences. Save ghi lại connection ID/name và SQL lúc mở; Export giữ result/filename lúc mở. Gõ chỉ thay draft, không đổi console/result. Draft sống qua hidden/unmount; mở request mới nhận ID riêng để completion cũ không ghi đè. Pending ngăn submit lặp, Close/Escape và thay request; cancel rõ ràng giải phóng draft. Đóng Export cũng bỏ reference result trong database store.

Save/Export dùng shadcn Dialog/Field/Input/NativeSelect và shared inline/pending feedback. Validation focus field lỗi. Save có SQL preview bounded và ghi rõ connection đích; lỗi giữ tên/SQL để Retry.

Export chỉ ghi rows đã tải. Chuẩn bị CSV/JSON chạy trong worker nếu ít nhất 1.000 rows hoặc hơn 128 Ki ký tự cell; worker kết thúc sau thành công/error. Structured clone và chuyển string qua bridge vẫn có chi phí, chưa được profile native trong phase này. Không tăng backend response/export limit. File picker native chọn destination và có Cancel; bridge hiện chưa có cancel write, nên UI không giả lập hủy thao tác đang chờ picker/write.

CSV giữ escaping hiện có, SQL NULL/empty text đều thành empty field. JSON giữ string precision/SQL NULL, bảo toàn tên cột `__proto__`/`constructor`; duplicate column names bị từ chối trước file picker để tránh mất giá trị — dùng SQL alias hoặc CSV. Suggested filename là basename, kiểm tra trống/path separators/control/reserved characters và chuẩn hóa extension; không đoán type từ string.

## ERD

- NativeSelect/Input/ActionTooltip chung; React Flow controls nhận resolved dark/light theme.
- Typed columns lấy từ schema snapshot đã index, bỏ fan-out bridge request mỗi table. Foreign key/index error không bị đổi thành empty array; Retry riêng, cache metadata cùng target còn hiển thị khi refresh lỗi.
- Không suy quan hệ trước khi FK/index load lần đầu. Inferred links cần column type family phù hợp và một primary key `id` đơn được index metadata chứng minh; tên `id` đơn thuần không đủ. Inferred vẫn là giả thuyết, không là constraint.
- Bounded subgraph: tối đa 100 tables, 400 edges. Focus BFS theo hop, thứ tự ổn định và giữ root. Tìm table ở control local; tối đa 200 matching options cộng selection hiện có, không relayout graph theo phím tìm kiếm. Sidebar vẫn mở/focus table ngoài vùng đang xem.
- Column DOM mỗi node tối đa 80, ưu tiên relation keys; edge chỉ dùng handle thực sự render. Có notice khi preview columns bị giới hạn. React Flow chỉ render elements trong viewport; positions/edges được đồng bộ trước paint khi target/layout thay đổi. Animation edges tiếp tục ngừng ở graph lớn và tuân CSS reduced motion hiện có.

Giới hạn này không chuyển toàn bộ metadata/layout sang worker hoặc bổ sung backend graph paging. Relation discovery vẫn đọc schema snapshot; schema rất lớn cần native profiling và tối ưu riêng. Index cache thiếu typed metadata cần re-index qua explorer.

## Bằng chứng

Browser production fixture dùng components thật, mock bridge, QueryClient/visibility/tooltip providers và profiling build. Synthetic library 10.000 entries chỉ có 24 row elements ở viewport 1280×720. Đã thử refresh failure giữ rows, scoped connection switch, delete safe focus/pending/failure, save validation/pending/error/hidden draft, export 10.000 rows qua worker và picker cancellation, ERD focus/filter/light theme. Screenshot nằm ngoài repository; temporary harness đã dọn.

Unit/regression gồm scoped drafts, ABA response, synchronous operation locking, failed delete retry, giữ console draft, graph/column limits/key proof, worker serialization/precision/NULL, filename và duplicate JSON names. Xem số test/build cuối trong [file tiến độ](devkit-ui-rebuild-progress.md).

Native smoke chưa chạy vì Mac khóa tại lần kiểm tra. Browser không thay thế Wails/WKWebView: native worker support, save picker/write, bridge serialize/transfer/parse, scripting/style/layout/paint/memory, p95 và frame gates còn pending theo [Frontend performance](../dev-kit/frontend-performance.mdx) và [Smoothness](../dev-kit/smoothness.mdx).
