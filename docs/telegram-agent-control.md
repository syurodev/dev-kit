# Sử dụng Telegram để điều khiển agent DevKit

Tính năng ở bản mã hiện tại; cần kiểm chứng với bot và agent thật trước phát hành.

## Thiết lập

1. Tạo một bot riêng trong Telegram bằng BotFather. Mỗi bot chỉ dùng cho một máy DevKit; bot không được có webhook của tích hợp khác.
2. Mở DevKit desktop, mở khóa vault, vào **Settings → Agents → Telegram**.
3. Nhập token bot, bật tích hợp và chọn workspace được phép. Token được lưu trong Accounts dưới dạng mã hóa, chỉ trên máy này.
4. Bật **Gửi nội dung yêu cầu quyền và câu hỏi** để nhận thẻ duyệt và trả lời câu hỏi. Bật **Cho phép đọc nội dung session** nếu muốn đọc transcript. Các nội dung này đi qua dịch vụ Telegram.
5. Nếu cần duyệt thao tác phát sinh từ giao diện DevKit hoặc terminal, bật **Cho phép duyệt xác nhận từ UI/terminal trên toàn máy**. Quyền này độc lập với các workspace của agent đã chọn.
6. Lưu cấu hình, bấm **Tạo liên kết ghép tài khoản**, mở liên kết trong Telegram và bấm Start.
7. Quay lại DevKit, kiểm tra user ID rồi **Xác nhận ghép tài khoản này**. Liên kết ghép hết hạn sau năm phút.

Chỉ chat riêng của tài khoản đã ghép được điều khiển. Không dùng group hoặc tin nhắn chuyển tiếp. Người dùng có thể hủy ghép hoặc tắt tích hợp trong Settings.

## Điều khiển và đọc session

| Lệnh | Chức năng |
| --- | --- |
| `/help` | Xem các lệnh |
| `/sessions` | Liệt kê session trong workspace được phép và nút chọn |
| `/sessions 10` | Xem trang tiếp theo của danh sách |
| `/use <session ID>` | Chọn session bằng ID |
| `/models` hoặc `/model` | Xem model hiện tại và các nút chọn model của session |
| `/models 10` | Xem trang tiếp theo của danh sách model |
| `/model <model ID>` | Chuyển sang model được session cung cấp |
| `/status` | Xem trạng thái session đã chọn |
| `/read` | Đọc trang transcript mới nhất |
| `/more` | Đọc tiếp nội dung dài trong cùng trang |
| `/older` | Đọc trang lịch sử trước đó; đọc hết trang hiện tại bằng `/more` trước |
| `/new <claude hoặc codex> <workspace ID>` | Tạo và chọn session ở thư mục đầu tiên của workspace |
| `/stop` | Dừng lượt đang chạy của session đã chọn |
| `/pending` | Đối soát yêu cầu quyền và câu hỏi còn chờ |

Sau khi chọn session, gửi tin nhắn văn bản thông thường để gửi prompt. Session đang bận không tự nhận thêm prompt; chờ lượt kết thúc hoặc dùng `/stop`. Không tự chuyển prompt sang agent khác.

Transcript có phân trang; phần dài được chia qua `/more` thay vì cắt bỏ. Một số payload tool đã được Agent Hub giới hạn ở tầng đọc transcript, nên đây không phải bản xuất toàn bộ dữ liệu raw của nhà cung cấp.

Bot thông báo khi phát hiện agent kết thúc lượt chạy. Mất mạng có thể khiến bot không thấy một lượt ngắn đã chạy và kết thúc hoàn toàn trong thời gian ngắt kết nối; dùng `/status` và `/read` để lấy trạng thái hiện tại.

## Chuyển model

Chọn session bằng `/sessions` hoặc `/use`, rồi gửi `/models` để xem model hiện tại và danh sách của chính session đó. Bấm nút hoặc gửi `/model <model ID>` để chuyển. Tính năng dùng API cấu hình chung của Agent Hub cho cả Claude và Codex, không gửi một prompt yêu cầu agent tự đổi model.

Chỉ chuyển khi session đang rảnh; chờ lượt chạy kết thúc hoặc dùng `/stop` trước. Nút cũ bị từ chối nếu session đã chọn hoặc cấu hình model đã thay đổi. Model chưa được session cung cấp không được chấp nhận. Đây là đổi model trong cùng nhà cung cấp, không chuyển session Claude sang Codex hay ngược lại.

## Duyệt quyền và xác nhận DevKit

Thẻ ghi rõ nguồn **Quyền agent** hoặc **Xác nhận DevKit**, session và nội dung yêu cầu. Các thao tác Git có metadata repository, branch, remote và môi trường khi gate cung cấp.

Bấm **Cho phép lần này** hoặc **Từ chối**. Bản đầu không cấp quyền lâu dài qua Telegram. Giữ nguyên chính sách prod, giới hạn nhánh của agent và quyền repository; Telegram không thể vượt chúng.

Desktop và Telegram xử lý cùng một yêu cầu. Câu trả lời đầu tiên được chấp nhận; nút cũ báo yêu cầu đã xử lý/hết hiệu lực. Thẻ được gỡ nút khi bot đối soát lại. Thẻ quá dài hoặc lựa chọn không hỗ trợ được xử lý trên desktop.

Gate DevKit mặc định có thời gian chờ 60 giây; việc gửi lại thẻ không gia hạn. Quyền vendor mặc định không có deadline riêng, nhưng bị hủy khi lượt chạy bị dừng.

Thông báo đã nhận câu trả lời không đồng nghĩa thao tác đã thành công; xem kết quả của agent hoặc DevKit.

## Trả lời câu hỏi

Bấm **Trả lời** trên thẻ câu hỏi. Bot lần lượt hỏi từng trường:

- Chuỗi: reply bằng văn bản.
- Boolean hoặc enum: dùng nút chọn hoặc reply giá trị hợp lệ.
- Số/số nguyên: reply giá trị JSON như `42`.
- Mảng chuỗi: reply JSON như `["a", "b"]`.

Reply vào đúng tin hỏi trường. Sau khi điền đủ, bot hiển thị bản xem lại; bấm **Gửi câu trả lời** để chuyển về agent. **Bỏ qua** cho phép tool tiếp tục không có câu trả lời; **Hủy** bỏ lời gọi tool đó. Schema chưa hỗ trợ phải trả lời trên desktop.

## Vòng đời và giới hạn

Máy phải thức, có mạng, DevKit desktop đang chạy và vault đã mở khóa. Server mode không có kênh điều khiển Telegram.

Khóa vault, đổi cấu hình hoặc hủy ghép thu hồi nút và bản nháp đang có. Các lượt được khởi động từ Telegram dùng context của kênh, nên dừng kênh cũng hủy chúng. Lượt khởi động từ desktop tiếp tục theo chính sách của Agent Hub.

Khởi động lại làm nút/reply cũ hết hiệu lực; bot đọc lại các yêu cầu còn sống. Lệnh thay đổi trạng thái đã chờ quá hai phút bị từ chối. Offset được lưu trước thực thi để không tự phát lại lệnh sau crash, nhưng lệnh có thể mất ở cửa sổ ghi offset → thực thi.

Thông tin lỗi kết nối hoặc token không dùng được hiện trong Settings. Nếu bot có webhook, dùng bot riêng hoặc chủ động bỏ webhook ở tích hợp cũ; DevKit không tự xóa nó.
