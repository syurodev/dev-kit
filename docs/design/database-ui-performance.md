# Database forms/table — kiểm chứng UI phase 7

Ngày đo: 2026-10-05. Baseline: `main` sau PR #190 (`4d5fa756`).

## Phạm vi và cách đo

- Apple M3 Pro, RAM 18 GiB, macOS 27.0.1 arm64; Wails v3.0.0-beta.25, WKWebView UA `AppleWebKit/605.1.15 … wails.io`.
- Production Vite build, cùng React Compiler/Tailwind như app, alias React DOM sang production profiling build để lấy `Profiler.actualDuration`. Không lấy số từ Vite dev server.
- Wails window riêng, 1280×900 yêu cầu (client area tùy titlebar), cùng ThemeProvider/CodeEditor/ResultGrid/form thật. Bridge mô phỏng, mỗi query/probe/write chờ 1,8 giây. Không mở vault hoặc dùng connection cá nhân.
- Fixture được giữ tại `frontend/src/modules/database/grid/databasePerformance.fixture.ts`: 10.000 dòng × 30 cột, JSON/text dài. Đây là stress fixture render; bridge query thật vẫn bị giới hạn theo page hiện có.
- So sánh TableDataView/TableToolbar trước và sau cùng dữ liệu. Nạp fixture, đợi metadata, reset metrics, gõ từng ký tự `id > 2` bằng native input, chưa Enter. Đọc metrics sau thao tác. Profiler ghi update commits, không ghi mount; snapshot lấy trước khi render chính vùng metrics.

## Kết quả quan sát

| Chỉ số, một lượt cùng scenario | Trước | Sau |
|---|---:|---:|
| COUNT query phát sinh khi gõ 6 ký tự | 6 | 0 |
| React update commit lớn nhất | 9 ms | 2 ms |
| Số update commit trong cửa sổ đo | 8 | 8 |
| Dòng hiện trong DOM, 10.000 dòng nguồn | 40 | 39 |
| Overlay 2 staged cells, median 5 lần trong WKWebView | 41 ms | dưới độ phân giải timer (0 ms) |

Lượt overlay tiếp theo: nested row/cell mapper 43 ms, `stagedResult` dưới độ phân giải timer. Không diễn giải 0 ms là không có chi phí. Helper mới copy array của rows và chỉ copy những row bị chỉnh; không đi qua toàn bộ 300.000 cells.

Nguyên nhân: baseline gõ WHERE cập nhật `browse.where`, làm TableDataView và COUNT effect chạy. Bản mới đặt filter draft ở một map ephemeral riêng, chỉ TableFilter subscribe; Enter mới áp filter, invalidate count và load rows. Commit count bằng nhau không phải bằng chứng ngang hiệu năng: duration, bridge calls và DOM cho thấy phần việc trong mỗi update khác nhau. DOM chênh một row do vị trí viewport/overscan, vẫn bounded.

## Kiểm tra tương tác native

- New Connection: focus tên khi mở; thiếu tên/env đưa focus về trường lỗi; pending khóa form/Save/close; Test có elapsed time, Cancel thật qua cancellable chain; cancellation mở khóa ngay, bỏ late result.
- Save lỗi giữ tên/env/draft và lỗi tại form; credential draft chỉ local, không persist vào preferences/store.
- Options: giá trị đúng ngay khi mở, chọn environment có policy feedback; light theme giữ màu controls hiện tại.
- Apply: đọc được SQL trong pane rộng, Cancel nhận focus; double-click chỉ một write; Escape không đóng pending; failure giữ review/staged; Retry thành công xóa staged và reload. Hai write trong fixture tương ứng một failure + một Retry.
- Delete: Cancel nhận focus; pending không đóng bằng Escape; failure chỉ hiện tại dialog, vẫn có Retry/Cancel.
- Screenshot native dark form, dark review và light options lưu ngoài repository.

## Giới hạn

Đây là native frontend fixture với bridge giả lập, không phải E2E database. Số đo một lượt phục vụ khoanh vùng bottleneck, chưa phải thống kê p95 hoặc gate throughput. Chưa thu được trace scripting/style/layout/paint/memory đầy đủ từ WebKit Inspector của fixture. Chưa đo serialize/transfer/JS parse của bridge thật, mạng/driver/database, memory plateau, hay warm module activation toàn app. Không suy ra FPS từ React commit duration. Các phần đó cần fixture database thử nghiệm và trace ứng dụng đầy đủ theo `docs/dev-kit/frontend-performance.mdx`; không tăng budget hoặc đánh dấu native DB smoke hoàn thành.
