# Sử dụng Telegram để điều khiển agent DevKit

Tính năng ở bản mã hiện tại; cần kiểm chứng với bot và agent thật trước phát hành.

## Thiết lập

1. Mở DevKit desktop, mở khóa vault, vào **Settings → Remote control → Telegram**.
2. Ở **Connect bot**, mở BotFather, gửi `/newbot`, làm theo hướng dẫn đặt tên rồi dán token vào **Bot token**. Bấm **Connect bot** để kiểm tra và lưu token mã hóa cục bộ.
3. Ở **Choose access**, chọn workspace và bật các quyền cần dùng: **Review requests and answer questions**, **Read conversations**, hoặc **Review desktop and terminal confirmations**. Quyền xác nhận desktop/terminal áp dụng trên toàn máy và cần bật gửi nội dung yêu cầu trước. Bấm **Continue to pairing**.
4. Ở **Pair account**, bấm **Create pairing link**, sau đó **Open Telegram** (hoặc **Copy link** để mở trên thiết bị khác).
5. Bấm **Start** trong cuộc trò chuyện với bot. Liên kết ghép hết hạn sau năm phút; có thể tạo lại bằng **Create new link**.
6. Quay lại DevKit, kiểm tra tài khoản và user ID rồi bấm **Confirm my account**.

Sau khi ghép, trang hiển thị trạng thái **Connected** hoặc **Paused**, các quyền có thể chỉnh sửa và **Save changes**. Dùng công tắc **Remote control** để tạm dừng, **Unpair account** để hủy ghép, hoặc **Change bot** để thay token. Phần **Telegram commands** có thể mở khi cần tra lệnh.

Chỉ chat riêng của tài khoản đã ghép được điều khiển. Không dùng group hoặc tin nhắn chuyển tiếp. Người dùng có thể hủy ghép hoặc tắt tích hợp trong Settings.

## Bật theo từng session

Workspace được cấp quyền không tự bật mọi session. Trong Agent Hub, mở menu `…` hoặc chuột phải session → **Enable remote control**. Chỉ session đã bật mới xuất hiện trong `/sessions`, được điều khiển/đọc qua Telegram và gửi thông báo kết thúc, yêu cầu quyền hay câu hỏi. Session cũ mặc định tắt; session con không tự kế thừa từ session cha.

**Disable remote control** dừng gửi dữ liệu của session đó, bỏ lựa chọn/buffer đọc và vô hiệu hóa nút duyệt/trả lời cũ. Lựa chọn được lưu cục bộ và vẫn cần workspace nằm trong phạm vi được cấp quyền. Session tạo bằng `/new` được bật tự động vì thao tác tạo đến từ người dùng Telegram đã ghép.

Xác nhận phát sinh từ agent cũng theo lựa chọn của session. Xác nhận UI/terminal tiếp tục dùng tùy chọn riêng **Review desktop and terminal confirmations**.

## Điều khiển và đọc session

| Lệnh | Chức năng |
| --- | --- |
| `/help` | Xem các lệnh |
| `/sessions` | Liệt kê session đã bật điều khiển từ xa trong workspace được phép và nút chọn |
| `/sessions 10` | Xem trang tiếp theo của danh sách |
| `/use <session ID>` | Chọn session bằng ID |
| `/models` hoặc `/model` | Xem model hiện tại và các nút chọn model của session |
| `/models 10` | Xem trang tiếp theo của danh sách model |
| `/model <model ID>` | Chuyển sang model được session cung cấp |
| `/status` | Xem trạng thái session đã chọn |
| `/read` | Đọc trang transcript mới nhất |
| `/more` | Đọc tiếp nội dung dài trong cùng trang |
| `/older` | Đọc trang lịch sử trước đó; đọc hết trang hiện tại bằng `/more` trước |
| `/effort` hoặc `/efforts` | Xem effort hiện tại và chọn bằng nút |
| `/effort <ID>` | Đổi effort được model hiện tại hỗ trợ |
| `/new` hoặc `/session` | Chọn workspace và agent bằng nút, rồi cấu hình model/effort cho session mới |
| `/new <agent> <workspace ID> [model ID] [effort]` | Tạo và cấu hình session bằng lệnh trực tiếp |
| `/stop` | Dừng lượt đang chạy của session đã chọn |
| `/pending` | Đối soát yêu cầu quyền và câu hỏi còn chờ |

Sau khi chọn session, gửi tin nhắn văn bản thông thường để gửi prompt. Session đang bận không tự nhận thêm prompt; chờ lượt kết thúc hoặc dùng `/stop`. Không tự chuyển prompt sang agent khác.

Transcript có phân trang; phần dài được chia qua `/more` thay vì cắt bỏ. Một số payload tool đã được Agent Hub giới hạn ở tầng đọc transcript, nên đây không phải bản xuất toàn bộ dữ liệu raw của nhà cung cấp.

Bot thông báo khi phát hiện agent kết thúc lượt chạy. Mất mạng có thể khiến bot không thấy một lượt ngắn đã chạy và kết thúc hoàn toàn trong thời gian ngắt kết nối; dùng `/status` và `/read` để lấy trạng thái hiện tại.

## Chuyển model

Chọn session bằng `/sessions` hoặc `/use`, rồi gửi `/models` để xem model hiện tại và danh sách của chính session đó. Bấm nút hoặc gửi `/model <model ID>` để chuyển. Tính năng dùng API cấu hình chung của Agent Hub cho cả Claude và Codex, không gửi một prompt yêu cầu agent tự đổi model.

Chỉ chuyển khi session đang rảnh; chờ lượt chạy kết thúc hoặc dùng `/stop` trước. Nút cũ bị từ chối nếu session đã chọn hoặc cấu hình model đã thay đổi. Model chưa được session cung cấp không được chấp nhận. Đây là đổi model trong cùng nhà cung cấp, không chuyển session Claude sang Codex hay ngược lại.

## Chọn effort và tạo session

Dùng `/effort` để xem và chọn mức effort từ cấu hình của model hiện tại, hoặc `/effort high` để chọn trực tiếp nếu model hỗ trợ. Không cố định một danh sách chung cho mọi model. Claude dùng option `effort`, Codex dùng `reasoning_effort`; kênh Telegram gửi đúng option session cung cấp. Chỉ đổi khi session rảnh. Nút effort cũ không dùng được sau khi model hoặc phạm vi workspace thay đổi.

Gửi `/new` để tạo session mà không cần nhớ ID:

1. Chọn workspace trong danh sách được cấp quyền.
2. Chọn agent đã đăng ký trong DevKit.
3. DevKit tạo session ở thư mục primary của workspace, chưa chạy prompt; chọn model hoặc giữ model hiện tại.
4. Chọn effort được model hỗ trợ hoặc giữ mức hiện tại. Nếu agent không cung cấp lựa chọn, session dùng mặc định.
5. Gửi tin nhắn để bắt đầu lượt chạy đầu tiên.

Có thể dùng lệnh trực tiếp, ví dụ `/new codex <workspace ID> <model ID> high`. Nếu model/effort không hợp lệ, session đã tạo vẫn còn và bot báo rõ để chọn lại bằng `/models` hoặc `/effort`. Lệnh không nhận đường dẫn tùy ý và không mở rộng quyền thư mục. Bấm lại nút tạo đã xử lý không tạo thêm session.

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
