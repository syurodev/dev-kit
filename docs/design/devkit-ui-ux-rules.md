# DevKit — Quy tắc thiết kế UI/UX v1

Ngày: 2026-10-04. Trạng thái: bộ quy tắc đề xuất cho đợt redesign; chưa phải báo cáo rằng mọi màn hình đã tuân thủ.

Phạm vi: shell và mọi module, trong cả dark/light và mọi theme family hiện có. Tài liệu này bổ sung quy tắc hành vi cho [Design system hiện tại](../dev-kit/design-system.mdx). Token, component và policy bảo mật hiện có tiếp tục là nền tảng; một thay đổi policy sản phẩm phải được thiết kế riêng.

## 1. Hướng thiết kế và nguyên tắc chung

Kết hợp sự tiết chế, khoảng trống và bề mặt mềm của mẫu UI đọc sách với cấu trúc workspace, panel nổi và chiều sâu của mẫu UI dạng node.

1. Cùng một hành vi phải có cùng cách hiển thị ở mọi module.
2. Phản hồi nằm gần nơi người dùng vừa thao tác; chỉ đưa ra shell khi cần chú ý từ nơi khác.
3. Mức độ gián đoạn tương ứng với mức độ cần quyết định. Thành công thông thường không mở dialog.
4. Một sự kiện có một nơi hiển thị chính. Tránh đồng thời toast, banner và dialog cùng báo một lỗi.
5. Đẹp phải giúp đọc và làm việc: nội dung, focus và trạng thái rõ trước; hiệu ứng đứng sau.
6. Theme hiện có giữ màu component. Redesign nền app không được vô tình đổi màu card, button, terminal, editor hay status.
7. Đổi tab/module là điều hướng. Nó không tự hủy một thao tác đang chạy hoặc đóng session.
8. Lời nhắc phải nói đúng kết quả thật. “Đã lưu”, “Đã kết nối”, “Đã hủy” chỉ xuất hiện khi backend xác nhận tương ứng.

## 2. Chọn cách hiển thị: bảng quyết định

| Tình huống | Hiển thị chính | Ví dụ DevKit | Quy tắc |
|---|---|---|---|
| Kết quả ngắn của thao tác người dùng vừa làm | Toast hoặc phản hồi tại control | Copy endpoint, export xong | Nếu control đã đổi thành “Copied”, không thêm toast |
| Thành công đã rõ trong nội dung | Nội dung cập nhật / status tại chỗ | Tạo connection và mở connection mới | Không cần “thành công” ở mọi nơi |
| Field nhập sai | Lỗi inline dưới field | Port ngoài phạm vi, tên trống | Giữ input; chỉ rõ field; không dùng toast |
| Submit form thất bại ở backend | Lỗi inline trong form | Không lưu được SSH profile | Giữ dialog mở và dữ liệu đã nhập |
| Một vùng dữ liệu không tải được | Error state trong vùng + Retry | Sidebar không tải được connections | Không thay toàn bộ app bằng lỗi |
| Điều kiện tồn tại lâu ảnh hưởng vùng làm việc | Banner trong vùng hoặc module | Connection mất kết nối, conflict chưa xử lý | Không tự biến mất theo timer |
| Điều kiện lâu dài của một đối tượng | Badge/status chip | PROD, Connected, Agent target | Ngắn; có nghĩa rõ; không nhấp nháy |
| Tác vụ dài người dùng đang xem | Progress/status tại chỗ | Query, indexing, export | Có Cancel khi backend hỗ trợ |
| Tác vụ nền ở module khác hoàn thành | Toast + Open result | Export Database xong khi đang xem SSH | Không tự chuyển module hoặc lấy focus |
| Tác vụ nền thất bại | Trạng thái lỗi bền vững ở nơi sở hữu + một toast dẫn về | Indexing thất bại | Toast là lời nhắc; lỗi phải còn tìm lại được |
| Cần một quyết định trước khi tiếp tục | Confirmation / AlertDialog | Xóa connection, bỏ nội dung chưa lưu | Nêu đối tượng và hậu quả, nút động từ cụ thể |
| Cần tạo/sửa một đối tượng có nhiều field | Form dialog | Create/Edit connection | Một tác vụ gọn, có Save/Cancel |
| Cần xem/sửa chi tiết trong lúc đối chiếu nội dung | Side panel | Chi tiết task, node, schema | Giữ nội dung chính hiện diện |
| Nội dung làm việc dài hoặc có thể mở song song | Workspace tab | SQL console, file, note | Không nhét vào modal |
| Một tùy chọn ngắn gắn với control | Popover | Filter, chọn column, màu | Có anchor; đóng không làm mất tác vụ lớn |
| Danh sách hành động cho đối tượng | Dropdown / context menu | Rename, Duplicate, Delete | Menu thao tác theo đối tượng; không dùng cho form lớn |
| Giải thích ngắn cho icon/control | Tooltip | “Stop tunnel”, kèm shortcut | Không chứa hướng dẫn bắt buộc hay lỗi cần đọc |
| Chưa có dữ liệu | Empty state | Chưa có SSH connection | Một mô tả và một hành động chính |
| Tìm kiếm không có kết quả | No-results state | Không có host khớp | Giữ search; cho Clear filters; không nói “chưa có dữ liệu” |

Thứ tự lựa chọn: **field → vùng nội dung → module → shell**. Chỉ nâng phạm vi khi vấn đề thực sự vượt phạm vi nhỏ hơn.

## 3. Toast

### Khi dùng

- Một kết quả ngắn cần xác nhận nhưng chưa rõ trên màn hình.
- Kết quả của tác vụ nền xảy ra ngoài vùng người dùng đang xem.
- Một thất bại không có control/vùng nội dung phù hợp để giải thích; kèm đường tới thông tin bền vững nếu lỗi cần xử lý.

### Khi không dùng

- Validation, lỗi submit trong dialog, trạng thái kết nối kéo dài.
- Mỗi lần autosave, mỗi lần refresh, mỗi chunk output, mỗi bước nhỏ của task.
- Thay cho host-key approval, yêu cầu password, quyết định xóa hoặc cảnh báo mất nội dung.
- Là nơi duy nhất lưu một lỗi khiến người dùng chưa thể tiếp tục.

### Thiết kế và thời gian

- Dùng Toaster chung của shell, bottom-right; offset theo spacing token.
- Giữ material và palette của toast hiện tại: glass nhẹ, viền mảnh, radius `float`; màu status ưu tiên ở icon, text giữ màu theme.
- Chiều rộng chuẩn 380px, co theo vùng khả dụng. Title 13 medium, mô tả 12; mô tả tối đa hai dòng trước khi chuyển thông tin dài sang Details.
- Title là kết quả cụ thể; mô tả chỉ thêm ngữ cảnh cần thiết. Ví dụ: “Tunnel đã mở” / “API staging · 127.0.0.1:15432”.
- Success/info: mặc định 4 giây, khớp Toaster hiện tại.
- Toast có action hoặc nội dung warning/error dài hơn: đề xuất 6 giây; dừng đếm khi hover hoặc focus. Lỗi cần xử lý vẫn phải có nơi hiển thị bền vững.
- Toast thường tối đa một action: Open result, Retry, Undo hoặc View details; có nút đóng khi cần.
- Undo chỉ được hiển thị nếu thật sự khôi phục được đối tượng và trạng thái liên quan. Thời gian toast không được dài hơn cửa sổ Undo thật.
- Tối đa ba toast thường cùng lúc. Gộp sự kiện giống nhau theo đối tượng/tác vụ; một batch dùng một thông báo tổng hợp.
- Một task có một toast ID nếu cần cập nhật loading → success/error. Tác vụ dài vẫn có progress tại module, không dựa vào toast loading vô hạn.
- Không cướp focus, không tự mở trang đích, không phát âm thanh mặc định.
- Dữ liệu nhạy cảm không xuất hiện trong toast hoặc message history: password, private key, token, sign-in code; chi tiết lỗi phải được redaction.

### Ngoại lệ: yêu cầu của agent

DevKit hiện có thẻ nổi riêng cho permission/question của agent. Đây là **thẻ yêu cầu quyết định**, không phải toast thông báo tự tắt:

- Nếu conversation đang hiển thị, yêu cầu nằm inline tại conversation đó.
- Nếu conversation đang ẩn, shell có một thẻ nổi dẫn về đúng session; không đồng thời nhân đôi cùng yêu cầu ở hai nơi.
- Grant và deadline do backend/policy quyết định. Hết thời gian hoặc đóng lời nhắc không được suy diễn thành Allow.
- “Allow once” và hành động xem đầy đủ ngữ cảnh tiếp tục theo contract hiện có; đổi theme không thay đổi quyền.
- Không chồng toast thường che thẻ yêu cầu; lời nhắc thường có thể chờ hoặc gộp.

### Thông báo hệ điều hành

- Dành cho việc cần chú ý khi DevKit không ở foreground: task dài hoàn thành/thất bại hoặc một yêu cầu người dùng cần trả lời.
- Theo tùy chọn notification của người dùng; không phát OS notification cho từng lần copy/save/refresh.
- Không đồng thời phát toast foreground và OS notification cho cùng sự kiện. Trạng thái bền vững vẫn nằm ở module sở hữu.
- Click notification dẫn tới đúng module/session/object; không chấp thuận quyền hoặc thực thi command chỉ bằng việc mở notification.
- Nội dung tránh command, query, host hoặc nội dung riêng tư không cần xuất hiện trên màn khóa. Quyền và hành vi OS phải được kiểm tra trên nền tảng hỗ trợ.

## 4. Inline feedback, banner và badge

### Inline

- Đặt ngay dưới field hoặc gần thao tác gây lỗi. Với lỗi chung của form, đặt trước footer hành động và liên kết với field khi có thể.
- Lỗi field: viền/focus theo token lỗi, message có icon/text; `aria-invalid` và `aria-describedby`.
- Không xóa nội dung người dùng nhập sau submit thất bại.
- Validation client xuất hiện sau blur hoặc submit; sau đó cập nhật khi sửa. Không báo lỗi ngay trên một field trống chưa được chạm tới.
- Thông tin kỹ thuật dài nằm trong phần Details có thể mở, copy được sau khi redaction.

### Banner

- Nằm đầu vùng bị ảnh hưởng, trong layout; không đè editor hoặc đẩy toàn bộ cửa sổ mỗi lần polling.
- Info cho ngữ cảnh, warning cho vấn đề cần chú ý, danger cho trạng thái đã chặn/hỏng. PROD thông thường là badge; chỉ nâng thành banner ở vùng thực thi khi cần nhận biết target.
- Một nguyên nhân có một banner; cho hành động xử lý cụ thể như Reconnect, Unlock, Resolve conflict.
- Đóng banner chỉ ẩn lời nhắc theo phạm vi được thiết kế, không có nghĩa vấn đề đã được xử lý. Điều kiện quan trọng vẫn được thể hiện bằng status.
- Chỉ tự gỡ khi dữ liệu thực xác nhận điều kiện đã hết.

### Badge / status

- Dùng cho môi trường, connection state, target agent và số lượng. Luôn có text hoặc nhãn truy cập; màu không phải thông tin duy nhất.
- Badge status không tự là button. Nếu click được, phải thể hiện hover/focus và nhãn hành động.
- Selected, running và success là ba ý nghĩa riêng. Connection được chọn không có nghĩa đã kết nối.
- Không hiển thị “Connected” chỉ vì một Test connection trước đây thành công. Với test, hiển thị “Last test succeeded” và thời gian khi có dữ liệu.

## 5. Dialog, xác nhận và panel

### Form dialog

- Dùng cho tác vụ hữu hạn, một đối tượng; field chia thành nhóm có nghĩa. Advanced đóng mặc định nếu ít dùng.
- Header: tên tác vụ + mô tả khi cần. Body: fields. Footer: Cancel và một nút chính như Save connection.
- Footer dễ tiếp cận khi nội dung dài; vùng field cuộn trong dialog và focus không bị che.
- Enter submit khi phù hợp; textarea giữ newline. Escape/outside click chỉ đóng khi việc đóng an toàn; nội dung chưa lưu đi qua quy tắc unsaved.
- Khi submit đang chạy: chặn submit lặp, giữ label/chiều rộng nút ổn định. Nếu đóng không thể hủy request, không giả vờ đóng đã hủy tác vụ.
- Không chồng nhiều modal. Bước xác nhận bảo mật có thể thay nội dung dialog hiện tại hoặc dùng flow tuần tự.

### Confirmation

Dùng khi hành động gây mất dữ liệu, đóng tài nguyên đang chạy hoặc cần quyết định bảo mật. Tạo mới, mở tab, copy và thao tác phục hồi được rõ ràng không mặc định hỏi lại.

- Title cụ thể: “Xóa API staging?”; body nói dữ liệu/tài nguyên chịu tác động và khả năng phục hồi thực tế.
- Primary dùng động từ: Delete connection, Discard changes, Trust host key. Không dùng “OK”, “Yes” hay “Confirm” chung chung.
- Destructive action dùng variant semantic danger. Focus mặc định ở lựa chọn an toàn cho việc không phục hồi được; Enter không vô tình chấp thuận hành động nguy hiểm.
- Nội dung approval của host key phải có endpoint, fingerprint đã tin nếu có và fingerprint mới. Approval luôn theo đúng profile/target đang được hỏi.
- Không bắt gõ tên cho mọi Delete. Chỉ cân nhắc cho hành động phạm vi lớn/không phục hồi được, sau một thiết kế tác vụ riêng.
- Không hiển thị Undo hoặc “có thể khôi phục” khi backend chưa hỗ trợ.

### Side panel và tab

- Side panel: xem/sửa ngữ cảnh cùng tài liệu chính; có tiêu đề đối tượng và cách đóng rõ ràng.
- Tab: nội dung dài, độc lập, có draft/history/session hoặc cần so sánh song song.
- Đóng panel hoặc connection details tab chỉ đóng vùng xem. Đóng terminal/session có lifecycle riêng và phải thể hiện đúng hậu quả.
- Panel đang được pin không tự đổi đối tượng khi selection khác thay đổi. Panel theo selection phải cho biết nó đang theo selection.

## 6. Loading, progress và tác vụ dài

Các mốc dưới đây là mặc định thiết kế để kiểm thử cảm nhận, chưa phải cam kết latency của backend.

| Trường hợp | Cách hiển thị |
|---|---|
| Click hoặc submit vừa xảy ra | Press/pending state ngay, chặn thao tác lặp có hại |
| Nội dung tải nhanh | Không nháy spinner/skeleton; loader có thể trì hoãn khoảng 150–200ms |
| Tải lần đầu chưa có dữ liệu | Skeleton mô phỏng layout thật; không dựng lại toàn bộ khung |
| Refresh đã có dữ liệu | Giữ dữ liệu, indicator nhỏ “Updating”; thể hiện stale khi có ý nghĩa |
| Task kéo dài vài giây | Tên thao tác và trạng thái, Cancel nếu thực sự hỗ trợ |
| Task có tổng công việc biết được | Progress định lượng từ backend |
| Task không biết tổng | Spinner/progress indeterminate + bước đang chạy; không giả lập phần trăm |
| Đang reconnect | Status ở connection, thời điểm/thử lại theo policy thật |
| User đổi module khi task chạy | Task tiếp tục nếu contract cho phép; badge ở nơi sở hữu, toast khi có kết quả cần nhắc |

- Không blank-out nội dung cũ khi mỗi lần refetch.
- Cancel phải hủy backend hoặc được ghi rõ là “Dừng theo dõi”. UI chỉ chuyển sang Cancelled khi biết trạng thái thật; kết quả hoàn thành trước cancel vẫn là completed.
- Không tự chạy lại command, query có side effect hoặc agent action sau lỗi/mất mạng. Retry chỉ khi hành động đó an toàn và được người dùng khởi tạo phù hợp.
- Reconnect SSH tạo shell mới nếu chưa có cơ chế resume; không gọi nó là “khôi phục phiên” khi không phục hồi được phiên cũ.
- Streaming giữ thứ tự và nội dung đã nhận, có điểm kết thúc/lỗi rõ; không kéo người đang đọc về cuối nếu họ đã cuộn lên.

## 7. Các trạng thái bắt buộc của màn hình

Mỗi list/view phải thiết kế riêng các trạng thái: loading lần đầu, có dữ liệu, empty, no results, refresh, error và unavailable/locked khi áp dụng.

- Empty: mô tả ngắn + một CTA. “Chưa có connection” → New connection.
- No results: mô tả theo filter đang dùng + Clear filters; giữ dữ liệu/filter để sửa.
- Error: nói phần nào không tải được + Retry; giữ phần app khác hoạt động.
- Locked/auth required: đúng hành động Unlock/Sign in/Choose credential; không đưa secret lên tooltip hoặc toast.
- Unsupported/unavailable: nói khả năng chưa có ở môi trường này; không tạo spinner chờ vô hạn.
- View có dữ liệu kỹ thuật quá dài: wrapping có chọn lọc, copy/download/details; đừng cắt mất giá trị cần chẩn đoán.

## 8. Shell, điều hướng và tương tác liên module

Hợp đồng tích hợp bắt buộc: [Sidebar và tab strip của app shell](./module-shell-integration.md). Module cung cấp rail qua manifest/hook và mở document qua workbench; không dựng `<aside>` hoặc document strip riêng. Shadcn Tabs chỉ dùng cho chế độ bên trong cùng một document.

- Activity navigation dùng để đổi module. Sidebar dùng để chọn đối tượng. Tabs dùng cho nội dung đang mở. Không tạo thêm một tầng điều hướng nếu không có nhiệm vụ khác.
- Module có thể có layout khác theo công việc, nhưng header, menu, feedback và hành vi đóng/mở dùng cùng quy tắc.
- Mỗi vùng có một header chức năng; tránh header shell, header module và tab strip cùng lặp một tiêu đề.
- Giữ filter, selection, draft, scroll và tab hợp lệ khi quay lại module. State nhạy cảm không persist chỉ để tiện giữ UI.
- Data cache, state người dùng và tài nguyên sống có owner rõ. Có thể bỏ DOM của view nặng theo chính sách bộ nhớ; phải giữ state cần thiết và không vô tình kill runtime.
- Điều hướng foreground do người dùng chủ động. Kết quả background không giành active tab/focus.
- Hành động liên module mang theo target chính xác: Open database through this tunnel, Reveal file, Open task, Open agent conversation.
- Không tự đổi sticky target của agent theo selection sidebar. Target thực thi luôn được hiển thị ở vùng hành động.
- Mở một đối tượng đã có tab thì focus tab đó; “New console/session” tạo instance mới khi người dùng thật sự yêu cầu.
- Đóng active tab chuyển sang tab gần nhất; đóng tab nền không đổi focus.
- Unsaved draft: Save / Discard / Cancel khi thật sự có nguy cơ mất nội dung. Autosave đã được xác nhận hoặc nội dung có recovery tin cậy không mặc định thêm confirmation.
- Phím tắt hiện cạnh action và trong command palette. Dùng mapping platform chung; không mỗi module tự đoán OS.

## 9. Quy tắc bề mặt và nền app

| Bề mặt | Material | Quy tắc |
|---|---|---|
| Nền cửa sổ / khoảng trống shell | Nền dark/light liền mạch, gradient tĩnh rất nhẹ nếu cần | Là phần được phép thiết kế lại màu; không giảm khả năng đọc |
| Sidebar / chrome shell | Sidebar hòa vào nền opaque của shell, không fill/border/shadow riêng | Control/selection giữ token theme; module không tự phủ nền rail |
| Editor, terminal, data table | Bề mặt ổn định theo theme hiện tại | Không dùng blur/gradient dưới nội dung cần đọc chính xác |
| Card chức năng | Surface hiện có, viền mảnh | Bóng rất ít hoặc không cần bóng; tránh card trong card liên tục |
| Floating toolbar / popover / menu / toast | Glass floating chung | Blur và bóng theo token; có fallback solid nếu nền quá phức tạp |
| Dialog | Glass modal chung + scrim | Phân biệt rõ với nội dung nền, giữ text/form tương phản |

- Slot/shell sở hữu surface. Component nội dung có thể trong suốt; input/control vẫn dùng material của component.
- Bo góc theo cấp: control nhỏ → container → floating surface. Các góc lồng nhau giảm theo inset, không áp cùng một radius cho mọi cấp.
- Không thêm gradient riêng cho từng module để tạo “cá tính”. Module có thể khác icon và nội dung, vẫn dùng cùng hệ bề mặt.
- Không animating gradient/blur trên toàn nền trong khi làm việc.

## 10. Theme và token

- Giữ palette, semantic colors, syntax colors, vendor colors và màu trạng thái theo từng theme family hiện tại.
- Nền app dùng lớp/token nền riêng. Repo đã có `--background-app`, `--background-canvas` và utility `bg-bg-app`/`bg-bg-canvas`; ưu tiên kiểm tra scope của chúng trước khi thêm token mới.
- Không sửa `surface-base`, `primary`, `text-*` hoặc `status-*` chỉ để đạt màu nền app mới.
- Glass lấy tint từ theme và material chung; tăng khả năng đọc bằng material đúng/fallback thay vì tùy tiện đổi màu chữ.
- Button, card và dialog không hardcode white/black hoặc mã hex theo mẫu tham chiếu.
- Mỗi thiết kế phải được kiểm tra cả dark và light; thay nền không làm warning, focus ring, disabled hay selection biến mất.
- Dùng token hiện có cho typography/spacing/radius/motion. Nếu cần vai trò mới, thêm token có tên và mapping tập trung; không thêm giá trị riêng ở mỗi component.

## 11. Typography, mật độ, spacing và radius

Đây là mapping mặc định dựa trên code hiện tại. Giá trị được viết ở đây để review; component dùng token/variant thay vì số tùy ý.

| Vai trò | Quy tắc |
|---|---|
| Nội dung UI thường | Body 13 |
| Tên đối tượng / nhãn hành động | 12–13 medium theo typography token |
| Mô tả | Body 12 secondary; thông tin cần hành động không giảm thành tertiary khó đọc |
| Timestamp / metadata ít quan trọng | Caption 11 |
| Header workspace | Title 14/16 theo cấp; không dùng heading lớn cho mọi panel |
| Code, lệnh, path, host, fingerprint | Monospace theo Code token; đủ contrast để đọc/copy |
| Nội dung note dài | Typography của editor, không ép về mật độ toolbar |
| Icon | 12/14 ở control compact; 16 thường; 20 ở feedback; cùng stroke family |
| Spacing | 4/8/12/16/24/32 theo scale hiện có |
| Row, button, input | Radius sm/md/control theo variant; mặc định code hiện có 6/8/10px |
| Panel/container | Radius lg/xl theo vai trò; mặc định code hiện có 12/16px |
| Toast/floating surface | Radius float, hiện có 14px; không tự đặt radius mới |

Mật độ mặc định là compact cho desktop, nhưng các nhóm hành động vẫn có khoảng nghỉ. Label phân cấp bằng weight/màu trước khi tăng font size. Điều chỉnh panel radius hoặc control height ở hệ thống chung khi redesign, không từng màn hình tự sửa.

## 12. Button, icon và menu

- Mỗi nhóm tác vụ có một hành động chính dễ thấy. Secondary cho hành động thay thế, ghost cho thao tác phụ, destructive cho hành động mất dữ liệu.
- Icon-only dùng cho action quen thuộc trong toolbar/row; phải có accessible name và tooltip khi ý nghĩa chưa hiển nhiên.
- Action ít dùng vào overflow/context menu. Action được dùng thường xuyên không bị giấu hết vào dấu ba chấm.
- Delete không trở thành action nổi nhất trên một view thường xuyên làm việc.
- Disabled có lý do tìm được qua helper text/status; không chỉ giải thích bằng tooltip trên một button không focus được.
- Hover chỉ nhấn surface/viền nhẹ. Selected phải nhìn thấy độc lập với hover.
- Busy button giữ kích thước, giữ động từ hoặc dùng “Saving…”/“Connecting…” đúng thao tác; không làm cả toolbar nhảy layout.
- Menu có nhóm và thứ tự thống nhất: Open/Connect → Create/Edit/Duplicate → Copy/Export → Delete khi áp dụng.

## 13. Motion và cảm giác nhanh

Dùng hệ thời gian đang có: `motion-fast` 120ms, `motion-base` 180ms, `motion-slow` 240ms.

- Hover/press/focus: fast. Popup/toast: base. Panel/dialog: tối đa slow; exit thường nhanh hơn entry.
- Đổi tab/module phản hồi ngay; chỉ dùng chuyển động nhỏ nếu giúp định hướng. Tránh slide toàn trang trên các màn nhiều dữ liệu.
- Resize/drag bám con trỏ; không dùng spring làm panel tiếp tục đuổi theo sau khi thả.
- Ưu tiên opacity/transform; không animate blur trên vùng lớn, không phát hiệu ứng cho mỗi dòng log/token streamed.
- Dữ liệu, con trỏ, focus và selection không nảy. Spring nếu dùng dành cho chi tiết trang trí/khung phù hợp, không cản đọc.
- Không chờ animation xong mới bắt đầu request. Không giữ màn cũ chỉ để hoàn thành transition.
- Respect reduced motion: bỏ slide/bounce/scale/height morph không cần thiết, dùng thay đổi tức thì hoặc fade nhẹ; trạng thái vẫn đầy đủ.
- Kiểm thử thao tác liên tiếp: popup đóng/mở nhanh, đổi module khi data tải, resize với nhiều terminal; không chỉ review một animation riêng lẻ.

## 14. Accessibility, focus và nội dung thông báo

- Tab order theo bố cục; focus rõ trong mọi theme. Popover/dialog mở focus hợp lý và đóng trả focus về control khởi tạo khi nó còn tồn tại.
- Dialog trap focus; không để modal tự focus một destructive action nguy hiểm.
- Toast/status thường dùng live region polite. Lỗi cần chú ý dùng alert phù hợp; không đọc lặp mỗi lần polling.
- Không dựa riêng vào màu, hover hay icon. Control quan trọng dùng được bằng keyboard.
- Tooltip xuất hiện cả hover/focus, mô tả ngắn; thông tin bắt buộc luôn nhìn thấy ở field/body.
- Không truncate message quan trọng hoặc fingerprint theo cách mất khả năng kiểm tra; path/host có thể truncate nếu full value truy cập được.
- Kiểm thử zoom, nội dung dài, dark/light, cửa sổ tối thiểu và pane bị chia nhỏ.
- Message theo công thức: **đối tượng + chuyện đã xảy ra + bước tiếp theo nếu cần**. Tránh “Something went wrong”, “Success” hoặc mã lỗi trần.
- Không hứa đã hủy, rollback, resume, undo hoặc restore nếu backend chưa có semantics đó.

## 15. Ví dụ xuyên module

| Sự kiện | Quyết định thiết kế |
|---|---|
| Copy SSH endpoint | Nút chuyển “Copied” trong thời gian ngắn hoặc một toast 4s; không cả hai |
| SSH port nhập 70000 | Inline ở Port, giữ dialog mở |
| Host key chưa trusted hoặc đổi | Dialog endpoint + fingerprint + hành động Trust rõ ràng; không tự approve |
| Terminal rớt kết nối | Dòng kết thúc + exit/disconnected trong terminal; Connect mở session mới |
| Tunnel mở thành công | Row active + endpoint; toast chỉ khi cần nhắc ngoài vùng đang xem |
| Sửa SSH connection sẽ đóng terminal/tunnel | Nêu hậu quả trong flow edit; khi có tài nguyên đang chạy, xác nhận cụ thể trước Save |
| Query đang chạy | Run thành trạng thái running, Cancel tại console; kết quả không bị xóa lúc refresh |
| Query lỗi cú pháp | Error trong console, gắn vị trí nếu backend cung cấp; không chỉ toast |
| Export xong khi đang xem Notes | Toast “Export completed” + Open result; không đổi active module |
| Autosave note thành công | Status nhỏ theo nhu cầu; không phát toast mỗi lần gõ |
| Autosave note thất bại | Banner/status lỗi bền vững với Retry và bảo toàn draft |
| Agent xin quyền ở conversation đang mở | Thẻ quyết định inline tại đúng session |
| Agent xin quyền khi conversation bị ẩn | Thẻ nổi chuyên dụng của shell; mở đúng session để xem ngữ cảnh |
| Sync conflict chưa xử lý | Badge số conflict + trạng thái tại Sync; banner ở đối tượng ảnh hưởng nếu cần |
| Settings thay theme | Preview/áp dụng theo flow settings hiện có; không cần success toast mỗi lần chọn |

## 16. Cách áp dụng trong repo

Các primitive đã có: `components/ui/sonner.tsx`, `dialog.tsx`, `alert-dialog.tsx`, `alert.tsx`, `field.tsx`, `tooltip.tsx`, `empty-state.tsx`, `skeleton.tsx`, `spinner.tsx`; typography/token ở `src/index.css`, motion adapter ở `src/lib/motion.ts`.

- Dùng primitive chung, không tự vẽ modal/toast cho từng module.
- Dùng structured error code khi bridge có; tránh mỗi module phân loại lỗi bằng một cách dò chuỗi khác nhau.
- Notification metadata phải có owner/target và dedupe ID phù hợp; async result không được gắn sang đối tượng đang được chọn mới.
- Owner tác vụ xử lý lifecycle/cancel/result; component chỉ hiển thị và gửi ý định. Toast host thuộc shell.
- Module nền có thể báo cần chú ý, nhưng không tự đổi focus hoặc sticky execution target.
- Giữ design-system conformance checks và performance budget hiện có; chỉ thêm test hành vi có ý nghĩa cho lifecycle, routing, confirmation và mất dữ liệu.
- Các quy tắc mới như duration có action, giới hạn toast, confirmation khi edit có session sống hoặc state refresh cần được triển khai theo từng đợt; tài liệu này không khẳng định chúng đã có đầy đủ.

## 17. Checklist review một màn hình

1. Đã có loading, content, empty, no-results, error và refresh riêng chưa?
2. Mỗi feedback nằm đúng phạm vi, có bị nhân đôi hoặc biến mất trước khi xử lý không?
3. Target, trạng thái selected/running/connected và tác động của Close/Cancel có chính xác không?
4. Form thất bại có giữ dữ liệu; task nền có giữ lifecycle khi đổi module không?
5. Theme component có được giữ; glass/gradient có làm giảm khả năng đọc không?
6. Typography, spacing, radius, motion dùng cùng scale/primitive không?
7. Keyboard/focus/reduced motion và cửa sổ nhỏ có dùng được không?
8. Thông báo có rõ ràng, không lộ secret, có đường xử lý lỗi và không hứa khả năng chưa có không?
9. Các thao tác liên module có mang đúng target, không giành focus hoặc quyền ngoài ý định không?
10. Chuyển module, resize và thao tác liên tiếp có phản hồi nhanh trên màn hình thực không?

## 18. Chọn cách trình bày dữ liệu

| Dạng nội dung / nhiệm vụ | Component/layout ưu tiên | Ví dụ |
|---|---|---|
| Nhiều đối tượng cần chọn nhanh, mỗi item ít thuộc tính | List row có selection và row actions | SSH connections, agent sessions |
| Cần so sánh nhiều thuộc tính hoặc sort/filter hàng loạt | Table / data grid | Database results, query history |
| Đối tượng có cấu trúc cha–con | Tree view, mở nhánh theo nhu cầu | Workspace files, schema, folders |
| Nội dung nổi bật bằng hình/preview và có ít đối tượng | Card grid | Template/gallery, onboarding choices |
| Giá trị kỹ thuật dài, cần đọc/copy đúng từng byte | Code block/editor hoặc details view | Output command, SQL, JSON, diff |
| Quan hệ giữa nhiều đối tượng là câu hỏi chính | Graph/canvas có zoom và selection | CodeGraph; không mặc định dùng graph cho list connections |
| Các thuộc tính của một đối tượng được chọn | Definition/details panel có nhãn–giá trị | Connection details, node properties |
| Sự kiện có thứ tự theo thời gian | Timeline/log/transcript | Agent turns, task runs, terminal output |
| Hai nội dung cần đối chiếu trực tiếp | Split view/diff | Sync conflict, code review |

- List/table giữ chiều cao row ổn định; selected nhìn thấy khi hover ở row khác. Row actions xuất hiện khi hover hoặc keyboard focus; action quan trọng không chỉ tồn tại khi hover.
- Chỉ dùng card khi preview hoặc nhóm nội dung có giá trị; không biến mọi row connection thành một card lớn gây ít dữ liệu trên màn hình.
- Bảng lớn dùng virtualization/paging phù hợp; sorting/loading phải rõ. Header nói đơn vị, alignment giúp so sánh; dữ liệu copy/export giữ giá trị gốc.
- Tree phân biệt click chọn, expand và mở đối tượng. Arrow keyboard và focus theo primitive chung.
- Search/filter gần dữ liệu, có clear và chỉ báo filter đang áp dụng. Không dùng placeholder làm label duy nhất của form.

## 19. Chọn control nhập liệu

| Cần nhập/chọn | Control | Quy tắc |
|---|---|---|
| Text ngắn | Input | Label cố định, helper/validation gần field |
| Text nhiều dòng | Textarea | Enter xuống dòng; giới hạn chiều cao trước khi cuộn |
| Code/query cần syntax và thao tác soạn thảo | Code editor | Không dùng textarea thiếu hành vi cần thiết |
| Một giá trị trong danh sách ngắn | Select | Có trạng thái chưa chọn/default rõ ràng |
| Danh sách dài hoặc cần tìm đối tượng | Combobox/search picker | Search, empty/no-results/loading riêng |
| 2–3 lựa chọn quan trọng cần thấy cùng lúc | Radio / segmented control | Không giấu khác biệt quan trọng trong dropdown |
| Bật/tắt setting có hiệu lực ngay | Switch | Nêu phạm vi tác động; có pending/error nếu cần persist |
| Đồng ý hoặc chọn nhiều item trong form | Checkbox | Áp dụng khi Submit; label nói chính xác điều được chọn |
| Password/token/key | Secret input thích hợp | Mask mặc định khi phù hợp; reveal rõ ràng; không prefill secret vào DOM chỉ để thuận tiện |
| Số có khoảng hợp lệ | Numeric input với validation | Không âm thầm đổi input rỗng thành 0; cho biết đơn vị/range |
| Ngày/giờ | Date/time picker | Hiển thị timezone và format thống nhất khi có lịch/timestamp |

- Không dùng switch cho command một lần như Connect, Run, Delete.
- Không dùng tooltip thay label/helper bắt buộc. Placeholder là ví dụ, không phải tên field.
- Form luôn có semantics Save/Cancel hay Apply immediately rõ ràng; không trộn hai kiểu mà không phân biệt.
- Không tự chuyển focus, submit hoặc điều hướng chỉ vì người dùng đang chọn một option, trừ control navigation được thiết kế rõ.


## 20. Shell foundation đã triển khai (bước 1)

- Nền app riêng theo dark/light: `--shell-backdrop` và `--shell-backdrop-top`, gradient nhẹ theo chiều dọc. Không ghi đè palette background của editor/terminal hay module.
- Pane dùng `surface-base` opaque của theme hiện tại, viền hiện có và bóng nhẹ theo mode. Bo góc tối thiểu 12px; radius lớn hơn của cửa sổ vẫn được tính theo gutter. Giữ xử lý squircle hiện có trên macOS.
- Sidebar hòa trực tiếp vào nền app shell: không fill, border hoặc shadow riêng. Control và trạng thái chọn vẫn dùng token theme hiện có. Nền shell đã opaque nên giảm transparency không cần fill thay thế cho sidebar.
- Gutter 4px và kích thước/hành vi resize hiện tại được giữ nguyên. Bước này không đổi vòng đời module hoặc animation chuyển module.
- Độ tương phản được kiểm tra cho tất cả theme family trên hai đầu gradient và nền sidebar, bên cạnh các surface hiện có.
