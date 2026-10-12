# Điều khiển Agent Hub qua Telegram

Ngày nghiên cứu: 11/10/2026. Trạng thái: đã triển khai bản đầu trong repo, chưa phát hành và chưa kiểm chứng với bot/agent thật.

## Tình trạng triển khai bản đầu

Mã hiện có ở `internal/telegramcontrol`, cấu hình trong Settings → Remote control → Telegram. Hướng dẫn thao tác: [Sử dụng Telegram](../telegram-agent-control.md).

Đã có ghép tài khoản cần xác nhận desktop, token Accounts mã hóa cục bộ, đọc transcript qua `/read`/`/more`/`/older`, gửi prompt, tạo/chọn/dừng session, chuyển model qua `/models` hoặc `/model <ID>`, chọn effort qua `/effort`, tạo session qua `/new` với nút chọn workspace/agent và cấu hình model/effort trước prompt đầu tiên, thẻ quyền vendor và gate DevKit, câu hỏi nhiều trường có xem lại trước khi gửi. Xác nhận từ agent giới hạn theo workspace; xác nhận từ UI/terminal cần bật riêng cho toàn máy vì các gate ngoài Git chưa có đầy đủ metadata workspace. Không tự suy ra phạm vi cho chúng.

Các khác biệt so với đề xuất chi tiết bên dưới:

- Bản đầu dùng đối soát danh sách chờ định kỳ, chưa thêm hook đóng chung cho permission/interaction. Câu trả lời vẫn đi qua bộ chờ gốc, với kiểm tra context/deadline và loại yêu cầu đã hoàn thành khỏi danh sách chờ.
- Bộ điều khiển nằm chung trong `telegramcontrol`, chưa tách `remotecontrol` thành package riêng. Chỉ dùng các interface nghiệp vụ giới hạn; không mở endpoint từ xa hoặc tool MCP duyệt quyền.
- Git đã có metadata repo/branch/remote/đích/môi trường theo hợp đồng tùy chọn `DetailedConfirmer`. Duyệt từ xa giữ ngữ nghĩa gate desktop hiện có; không khóa repository hay cố định nội dung commit trong thời gian chờ.
- Schema câu hỏi có ràng buộc chưa hỗ trợ được chuyển về desktop. Không dùng câu trả lời một phần để bỏ qua ràng buộc.
- Offset được lưu trước thao tác để tránh chạy lại lệnh sau sự cố; có thể mất lệnh nếu crash ngay sau khi ghi offset. Chưa có hộp thư ghi nhận kết quả bền hoặc cam kết đúng một lần.
- Thẻ quyền quá dài yêu cầu duyệt trên desktop. Chưa có xem trước diff qua đọc file hoặc tải tệp lên Telegram.

Đã chạy test chức năng/race của Telegram và hồi quy các luồng liên quan, build frontend và build server. Kiểm tra toàn repo có lỗi analyzer Java ở CodeGraph/crossservice; cùng các lỗi được tái hiện trên `HEAD` chưa sửa với cùng artifact Java. Chưa chạy ghép tài khoản, duyệt prod hoặc câu hỏi trên bot thật với Claude/Codex thật; cần hoàn thành kiểm tra đó trước phát hành.

## Mục tiêu và phạm vi

Điều khiển agent trong DevKit desktop từ một cuộc trò chuyện Telegram riêng đã ghép tài khoản. Bản phát hành hoàn chỉnh đầu tiên phải bao phủ quyền do agent xin, xác nhận của DevKit (bao gồm commit trên nhánh có quy tắc bảo vệ và push tới môi trường prod), cùng câu hỏi của agent. Cả ba thuộc cùng phạm vi phát hành, không để thành phần bổ sung tùy chọn.

Các chức năng đi kèm gồm liệt kê và chọn session, gửi prompt, dừng lượt chạy, thông báo hoàn thành hoặc lỗi. Người dùng vẫn có thể thao tác đồng thời trên desktop. Một bot phục vụ một bản cài DevKit đang chạy; nhiều máy dùng chung bot, nhóm chat, máy chủ chuyển tiếp và agent trong server mode nằm ngoài bản đầu tiên.

Đề xuất chạy một worker Telegram dùng long polling ngay trong tiến trình desktop. Worker gọi các dịch vụ nghiệp vụ qua một lớp điều khiển từ xa có xác thực và quyền hạn giới hạn. Không công khai Wails bridge hay MCP socket ra Internet. Máy phải thức, có mạng, DevKit đang chạy và vault đã mở khóa.

## Hiện trạng: ba nguồn yêu cầu, hai tuyến xác nhận quyền

| Yêu cầu | Thành phần hiện giữ quyền quyết định | Cách trả lời | Thời gian tồn tại |
| --- | --- | --- | --- |
| Quyền do agent xin | `agenthub.PermissionWaiter` | `Complete(id, outcome)` | Đến khi context bị hủy; có thể đặt timeout ở Service, mặc định bằng 0 |
| Xác nhận DevKit từ agent trong Hub | `interaction.Hub.Confirm` → `Service.ConfirmTool` → cùng PermissionWaiter qua `ResolveGate` | Trả lời qua broker quyền, sau đó gate kiểm tra quyết định | Timeout của Hub, mặc định 60 giây |
| Xác nhận DevKit từ UI hoặc terminal của người dùng | Danh sách chờ của `interaction.Hub` | `Complete(id, value)` | Đến khi context bị hủy hoặc hết timeout của Hub, mặc định 60 giây |
| Câu hỏi của agent | `agenthub.ElicitationWaiter` | `Complete(id, response)` | Không có deadline riêng; kết thúc khi trả lời, bỏ qua, hủy hoặc dừng lượt chạy |

Các điểm tích hợp đã kiểm tra:

- `internal/agenthub/gate_permission.go`: `ConfirmTool` gắn `origin: devkit_gate`, môi trường, các lựa chọn do chính sách quy định và session của bên gọi. Hàm kiểm tra lại session còn hoạt động sau khi chờ. Các gate của agent có thể xếp hàng độc lập.
- `internal/interaction/hub.go`: `ConfirmAgent` chuyển yêu cầu của agent đã được định danh vào broker; bên gọi khác dùng danh sách chờ. `EnvBlocks` luôn ưu tiên hơn quyền đã ghi nhớ.
- `main.go`: nối cả hai bộ xử lý quyền, callback câu hỏi và `hub.ConfirmAgent = agentSvc.ConfirmTool`. Interaction dùng `NoopPrompter` vì desktop chủ động đọc danh sách chờ; chỉ nghe sự kiện sẽ bỏ sót các yêu cầu này.
- `internal/bridge/agenthub.go`: có API liệt kê, trả lời và đọc. Đây là hợp đồng tham khảo, không phải API HTTP để công khai từ xa.
- `internal/bridge/interaction.go`: có API danh sách chờ và trả lời/từ chối.
- `internal/agenthub/elicitation.go`: chuẩn hóa kiểu dữ liệu cơ bản và bỏ khóa không được hỏi. Chưa kiểm tra đầy đủ trường bắt buộc, enum và giới hạn giá trị theo schema.
- `internal/app/wiring.go`: `hubConfirmer` luôn đặt `EnvBlocks: true` cho xác nhận Git, HTTP và tác vụ. Hợp đồng hiện chỉ có hai chuỗi, làm mất metadata có cấu trúc về đích và môi trường.
- `internal/git/remote.go`, `guard.go`: gate thực tế phụ thuộc môi trường và quy tắc bảo vệ nhánh. Comment nói mọi push đều có gate không chứng minh rằng mọi push đều hiện xác nhận. Quyền push vào nhánh được kiểm tra trước khi duyệt: agent vẫn bị chặn ở nhánh mặc định/main/master hoặc nhánh không thuộc tác vụ đang giữ.
- `internal/agenthub/adapter_servermode.go`: agent thật vẫn không hoạt động trong server mode vì bridge là HTTP loopback không có xác thực.

Mã nguồn trên là căn cứ cho các giả định triển khai. Trong bước nghiên cứu chưa chạy thử bot Telegram hoặc agent của nhà cung cấp thật.

## Kết quả nghiên cứu giao tiếp Telegram

Telegram hỗ trợ long polling hoặc webhook, không dùng đồng thời; bản cập nhật chưa nhận được giữ tối đa 24 giờ. Offset của polling xác nhận bản cập nhật đã nhận. Đăng ký `message` và `callback_query`. Dữ liệu callback của nút giới hạn 64 byte; nội dung tin nhắn giới hạn 4096 ký tự. Phản hồi callback bằng `answerCallbackQuery`; dùng reply markup để nhập câu trả lời. Phản hồi khi vượt giới hạn lưu lượng có `retry_after`.
Nguồn: [Bot API](https://core.telegram.org/bots/api#getupdates), [nút](https://core.telegram.org/bots/api#inlinekeyboardbutton), [callback](https://core.telegram.org/bots/api#callbackquery), [tin nhắn](https://core.telegram.org/bots/api#sendmessage), [tham số phản hồi](https://core.telegram.org/bots/api#responseparameters).

Deep link có thể mang dữ liệu ghép tài khoản qua `/start`. Xem [tính năng bot](https://core.telegram.org/bots/features#deep-linking). Telegram khuyên tránh gửi liên tục quá một tin mỗi giây trong một chat. Xem [FAQ bot](https://core.telegram.org/bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this).

Cloud Chat khác Secret Chat có mã hóa đầu cuối. Phải xem tích hợp này là kênh đưa dữ liệu ra ngoài; không mô tả bot là vẫn giữ bảo đảm zero-knowledge của vault/sync trong DevKit. Xem [FAQ Telegram](https://telegram.org/faq#q-how-secure-is-telegram).

Lựa chọn triển khai: HTTP client nhỏ bằng thư viện chuẩn Go, chỉ khai báo các kiểu request/response cần dùng, đặt timeout rõ ràng và giới hạn kích thước body. Không cần thêm framework cho một tập phương thức nhỏ. Đặt client sau interface để thay thế được và kiểm thử bằng HTTP endpoint giả.

## Package và cách nối đề xuất

```text
HTTP client Telegram ← dịch vụ internal/telegramcontrol
                                ↓ interface giới hạn do host cung cấp
                        internal/remotecontrol
                          ↙          ↓          ↘
                  AgentHub      bộ chờ quyền / interaction.Hub
                                câu hỏi

UI desktop → bridge hiện có → cùng bộ chờ và gate giữ quyền quyết định
```

Tên trên là đề xuất, chưa phải API hiện có. `telegramcontrol` quản lý giao tiếp Telegram, ghép tài khoản, định tuyến chat và trình bày. `remotecontrol` quản lý hành động được phép, phạm vi, bản biểu diễn yêu cầu và kiểm tra câu trả lời. Truyền store và interface từ `main.go` / `internal/app`, tuân thủ quy tắc kiến trúc. Package nghiệp vụ không import package Telegram. Bridge giữ vai trò mỏng. Cố định chiều phụ thuộc bằng test kiến trúc.

Cung cấp bản biểu diễn yêu cầu thống nhất gồm loại nguồn và ID nguồn, session/workspace nếu có, dấu vân tay yêu cầu, nguồn gốc, hành động, thông tin đích an toàn, lựa chọn, thời điểm tạo và deadline thực tế. ID được phân loại theo nguồn: ID quyền, câu hỏi và interaction không thể thay thế cho nhau. Đây là cách nhìn vào yêu cầu còn hiệu lực, không phải nơi cấp quyền thứ hai.

Dùng hook tạo/đóng bất đồng bộ để cập nhật nhanh và đối soát snapshot để phục hồi. Bổ sung thông báo đóng cho quyền và interaction; câu hỏi đã có. Không gọi HTTP ngay trong callback của bộ chờ. Hàng đợi có giới hạn có thể mất tín hiệu đánh thức nhưng phải khôi phục bằng snapshot; không được biến yêu cầu duyệt bị mất thành ngầm cho phép. Không giữ khóa của nguồn khi thông báo cho bên đăng ký.

## Ghép tài khoản, thông tin xác thực và danh tính từ xa

1. Trong Settings trên desktop, chọn token của bot riêng, bật tích hợp và chọn workspace được phép. Kiểm tra danh tính bot mà không ghi token vào log.
2. Tạo nonce ghép tài khoản ngẫu nhiên bằng nguồn mật mã, dùng một lần, đề xuất hết hạn sau năm phút; hiển thị deep link Telegram trên desktop. Chỉ lưu giá trị băm.
3. `/start <nonce>` ghi nhận ứng viên với Telegram user ID dạng số và ID chat riêng. Desktop hiển thị ứng viên và yêu cầu người dùng xác nhận tại máy trước khi bật thao tác từ xa. Username chỉ để hiển thị, không dùng làm khóa xác thực.
4. Ràng buộc bot ID, ID bản cài, user ID, ID chat riêng và phiên ghép tài khoản. Từ chối nhóm, kênh, bot, đầu vào chuyển tiếp và người gửi khác. Hủy ghép hoặc đổi token thu hồi mọi ánh xạ và mã cho phép callback hiện có.

Settings và ghép tài khoản chỉ dành cho người dùng trên desktop, không có tool MCP. Telegram không được đăng nhập/đăng xuất nhà cung cấp, mở khóa vault, sửa chính sách hay mở rộng quyền workspace. Định nghĩa danh tính người dùng từ xa và nguồn audit riêng; không giả danh `identity.UI()` để thừa hưởng toàn bộ quyền builtin. Lớp này chỉ nhận câu trả lời từ người đã ghép tài khoản, đồng thời giữ riêng danh tính bên tạo yêu cầu. Người duyệt và đối tượng được duyệt là hai trường audit khác nhau.

Lưu token bằng thông tin xác thực Accounts cục bộ có mã hóa, preferences chỉ giữ ID (khi triển khai cần xác nhận kind/provider phù hợp và hành vi lúc khóa vault). Không lưu token dạng rõ trong SQLite, bộ nhớ lưu trữ frontend, log hay audit. Token Telegram nằm trong URL request, nên cũng phải làm sạch URL trong lỗi HTTP. Chặn xóa tài khoản token khi còn được chọn.

Thiết lập tích hợp, thông tin ghép và metadata gửi/nhận chỉ thuộc bản cài cục bộ, không đồng bộ. Không sync token bot sang các bản DevKit có thể tranh nhận tin.

Khi khóa vault: dừng polling/gửi tin, thu hồi ánh xạ trả lời từ xa và xóa thông tin xác thực trong bộ nhớ. Hành động đã được chấp nhận không bị hoàn tác; câu trả lời chưa được chấp nhận không được thực thi. Hành vi hủy trên desktop/session tiếp tục theo chính sách hiện có. Khi mở khóa, kết nối lại và chỉ đối soát yêu cầu còn hiệu lực.

## Duyệt quyền và xác nhận của DevKit

Hiển thị máy, workspace/repository, agent hoặc bên gọi ban đầu, nguồn yêu cầu, tool/hành động, môi trường, đích và deadline. Phân biệt rõ quyền do agent xin với xác nhận theo chính sách DevKit. Giao diện từ xa có Cho phép lần này và Từ chối; bản đầu không có quyền cho phép lâu dài, kể cả nhà cung cấp đưa ra lựa chọn đó. Nút chỉ ánh xạ tới ID/loại lựa chọn thực sự được đề nghị, không giả định ID cố định. Nếu không hiểu tập lựa chọn thì không hiện nút cho phép và yêu cầu xem trên desktop.

Dùng mã callback ngẫu nhiên, không chứa nội dung nghiệp vụ (ví dụ token 128 bit mã hóa base64url), ánh xạ trong bộ nhớ tới phiên ghép, phiên tiến trình, chat/user, ID nguồn, dấu vân tay và hành động cụ thể. Không nhét lệnh, JSON kết quả, profile ID hay thông tin xác thực vào callback data. Callback không được tự chọn session/hành động. ID tin nhắn của callback cũng phải khớp lần gửi đã ghi nhận; gửi lại khi chưa rõ kết quả có thể tạo thẻ trùng, mỗi thẻ được ghi nhận riêng và tất cả bị thu hồi khi yêu cầu được xử lý.

Trước khi trả lời, kiểm tra lại ghép tài khoản, trạng thái khóa, phạm vi được phép, yêu cầu còn hiệu lực, deadline, dấu vân tay và lựa chọn. Việc kiểm tra deadline/context bị hủy và chọn câu trả lời thắng phải nằm trong từng bộ chờ giữ quyền quyết định, dưới khóa của chính bộ chờ đó. Chỉ kiểm tra `PendingList` rồi gọi `Complete` hiện có chưa đủ bảo đảm tính nguyên tử. Củng cố nhất quán cả đường trả lời UI và từ xa. Hiện Permission/Interaction đánh dấu hoàn thành rồi mới xóa sau; snapshot phải loại yêu cầu đã xử lý. Trả lỗi có kiểu cụ thể cho yêu cầu cũ, đã đóng hoặc không hợp lệ.

Desktop và Telegram cùng trả lời một yêu cầu; chỉ một câu trả lời được chấp nhận. Callback đến sau báo đã xử lý/hết hạn. Cố gắng gỡ nút và cập nhật trạng thái tin nhắn; không sửa được tin nhắn không làm nút cũ có hiệu lực trở lại. Phản hồi callback chỉ báo câu trả lời được nhận hoặc đã cũ, không có nghĩa thao tác Git/API/database đã thành công. Kết quả thao tác được báo riêng qua cập nhật session/tool.

Giữ nguyên kiểm tra gate, nhánh được phép, quyền repository và chính sách môi trường. Duyệt Telegram chỉ cho phép thao tác đang chờ; không cấp quyền force push, push của agent lên nhánh mặc định hay truy cập workspace mới. Gate DevKit của agent chỉ được phản chiếu một lần từ PermissionWaiter, không tạo thêm bản sao interaction. Xác nhận từ UI/terminal được lấy qua nguồn interaction.

Yêu cầu interaction hiện thiếu metadata để lọc theo workspace, đặc biệt hợp đồng hai chuỗi của bộ xác nhận Git. Mở rộng bằng request có kiểu rõ ràng mang danh tính repo, nhánh, đích remote, môi trường và hành động; chuyển qua adapter ở tầng kết nối tới bản biểu diễn yêu cầu chờ. Tách trường hiển thị đã làm sạch khỏi tham số thực thi. Đích không rõ hoặc chưa xác định phạm vi chỉ xử lý trên desktop cho đến khi phân loại được, không đoán workspace.

Bản đầu giữ timeout thực tế: gate DevKit mặc định hết hạn sau 60 giây, kể cả khi qua broker agent; quyền của nhà cung cấp mặc định không có deadline. Bổ sung `ExpiresAt` cho bản biểu diễn interaction thông thường. Sau này có thể cho cấu hình thời gian dài hơn trên desktop như một thay đổi chính sách rõ ràng; gửi lại tin không được gia hạn gate đang chờ.

## Trả lời câu hỏi của agent

Hiển thị `requestedSchema` với trường chuỗi, boolean, số/số nguyên, enum và mảng chuỗi; hỗ trợ nhiều trường bắt buộc bằng bản nháp riêng cho từng câu hỏi. Nút dùng để chọn giá trị; văn bản chỉ được nhận khi reply vào tin hỏi trường đã ghi nhận. Tin nhắn thông thường không tự trở thành câu trả lời cho câu hỏi gần nhất. Ánh xạ reply chứa chat/user đã ghép, ID yêu cầu, trường và phiên bản bản nháp.

Kiểm tra khóa bắt buộc, enum, ràng buộc được hỗ trợ và kiểu dữ liệu trước khi hoàn thành. Giữ chuẩn hóa hiện có của Service như lớp bảo vệ bổ sung; dùng chung kiểm tra ở nơi phù hợp để desktop và Telegram không lệch nhau. Schema chưa hỗ trợ hoặc yêu cầu xác thực qua URL tiếp tục chờ xử lý trên desktop; không âm thầm trả lời một phần hay mở liên kết đăng nhập do agent đưa ra.

Hiển thị bản xem trước cùng các thao tác Gửi câu trả lời, Bỏ qua (`decline`) và Hủy (`cancel`). Bỏ qua cho phép nhà cung cấp tiếp tục mà không có câu trả lời; hủy làm bỏ lời gọi tool đó. Dừng session phải hủy câu hỏi đang chờ, không chuyển thành bỏ qua. Không tự thêm deadline cho câu hỏi. Xóa bản nháp khi yêu cầu đóng hoặc hủy ghép tài khoản.

## Điều khiển session và đưa dữ liệu ra ngoài

`/sessions`, `/use`, `/new`, `/status`, `/stop`, `/pending` và `/help` chọn session qua mã định danh không mang nội dung. Mỗi lệnh kiểm tra lại quan hệ workspace và session còn hoạt động. `/new` chỉ đưa ra nhà cung cấp/model đã cấu hình và thư mục workspace được cấp quyền; không nhận đường dẫn tùy ý từ Telegram. Dừng phải nhắm tới session cụ thể.

Chạy Invoke bất đồng bộ: lượt agent dài không được chặn việc nhận yêu cầu duyệt hoặc câu hỏi cần cho chính lượt đó. Quy định rõ hành vi gửi prompt khi session đang bận; không âm thầm chuyển prompt sang session hoặc nhà cung cấp khác.

Mặc định gửi bản tóm tắt có giới hạn và thẻ yêu cầu có thể thao tác. Toàn bộ transcript, nội dung SQL/lệnh và diff file cần thiết lập cho phép đưa dữ liệu ra ngoài rõ ràng trên desktop theo từng phạm vi. Chỉ che dữ liệu nhạy cảm không bảo đảm văn bản tùy ý của agent không chứa bí mật. Nếu thông tin an toàn không đủ để đánh giá hành động thì bỏ nút Cho phép và yêu cầu xem trên desktop.

Đường dẫn trong yêu cầu diff của nhà cung cấp là dữ liệu không đáng tin; xem trước từ xa phải kiểm tra ranh giới workspace và symlink trước khi đọc file. Không dùng lại cách nối `os.ReadFile` không giới hạn để tải file bất kỳ lên Telegram.

Escape nội dung hiển thị hoặc dùng văn bản thuần; không diễn giải văn bản agent thành markup Telegram hoặc lệnh bot. Giới hạn và phân trang chi tiết; không cắt bỏ ngữ cảnh quan trọng của hành động mà vẫn để nút Cho phép hoạt động. Gộp cập nhật tiến độ, gửi thông báo hoàn thành/lỗi thay vì từng đoạn transcript. Ưu tiên thẻ duyệt và phản hồi callback, có giới hạn tốc độ và thời gian chờ tăng dần nhưng có mức trần.

## Gửi/nhận, sự cố và vòng đời

Lưu offset polling và metadata tối thiểu để chống xử lý trùng. Tăng offset sau khi phân loại/ghi nhận bản cập nhật, không chờ lượt agent kết thúc. Trước khi chuyển lệnh thay đổi session để thực thi, lưu dấu đã nhận xử lý trong hộp thư đầu vào. Nếu tiến trình lỗi giữa ghi nhận và thực thi thì trạng thái không rõ: không tự chạy lại; báo chưa xác định kết quả và yêu cầu lệnh mới. Không cam kết thực thi đúng một lần trong mọi tình huống.

Quyết định duyệt đang chờ không phải quyền lâu dài. Khởi động lại tiến trình thu hồi mọi mã callback và ánh xạ câu trả lời. Nút/reply Telegram cũ hết hiệu lực; sau mở khóa đối soát các yêu cầu mới còn hiệu lực ở nguồn. ID tin nhắn lưu bền có thể giúp đánh dấu thẻ cũ đã đóng nhưng không được khôi phục yêu cầu hay câu trả lời.

Gửi lại thẻ khi lỗi mạng có thể tạo bản trùng; tất cả vẫn trỏ tới một yêu cầu nguồn. Khi chưa rõ kết quả nhận câu trả lời, đọc trạng thái nguồn để xác định, không chạy lại thao tác.

Kiểm tra chưa có webhook trước khi khởi động polling. Nếu có, giải thích xung đột trong Settings thay vì âm thầm xóa webhook của tích hợp khác. Mỗi bot/bản cài chỉ có một poller; nhiều bản cài tranh nhận là lỗi cấu hình. Khi kết nối lại, từ chối lệnh thay đổi trạng thái đã nằm trong hàng đợi quá lâu, đề xuất cửa sổ hai phút; quyền duyệt dùng ánh xạ còn hiệu lực và deadline thực tế của yêu cầu. Tránh chạy prompt hôm qua vào session tiếp tục hôm nay. Lệnh chỉ đọc có thể lấy trạng thái mới nhất.

Tắt mọi thao tác Telegram từ xa trong server mode. Thêm bot không giải quyết bridge cục bộ thiếu xác thực; dịch vụ agent headless cần thiết kế host có xác thực riêng. Khi tắt ứng dụng, hủy worker và chờ kết thúc. Lỗi giao tiếp vẫn để desktop xử lý được và không bao giờ tự duyệt.

Audit ghi ghép/thu hồi, thao tác từ xa bị từ chối và câu trả lời được nhận, gồm người dùng từ xa, bên gọi/session ban đầu, ID nguồn, hành động, thời gian và kết quả. Không ghi token, nonce ghép, mã callback, prompt, nội dung câu trả lời, câu truy vấn hay diff. Chính sách lưu giữ và dọn hộp thư có giới hạn thuộc phần lưu trữ cục bộ.

## Trình tự triển khai và điều kiện nghiệm thu

1. Củng cố hợp đồng bộ chờ dùng chung: lỗi hoàn thành có kiểu, kiểm tra lựa chọn/schema, kiểm tra hiệu lực/deadline nguyên tử, lọc yêu cầu đã xử lý, hook tạo/đóng và deadline interaction. Bổ sung metadata xác nhận Git có cấu trúc.
2. Xây lớp biểu diễn/hành động điều khiển từ xa theo phạm vi và ranh giới danh tính/audit người dùng. Dùng giao tiếp giả trước; bao phủ quyền nhà cung cấp, gate DevKit chuyển qua broker, xác nhận interaction thông thường và câu hỏi trước khi làm giao diện Telegram.
3. Thêm worker HTTP Telegram, tham chiếu token mã hóa, migration cục bộ, ghép tài khoản, Settings và vòng đời khóa/mở khóa/thu hồi. Không thêm tool MCP duyệt quyền/cấu hình.
4. Thêm thẻ quyền và bản nháp câu hỏi có kiểu; xử lý cả ba nguồn, cạnh tranh trả lời với desktop, dọn yêu cầu cũ và kiểm soát đưa dữ liệu ra ngoài.
5. Thêm điều khiển session theo phạm vi, Invoke bất đồng bộ, gộp thông báo, ghi nhận xử lý hộp thư, kết nối lại và trạng thái giao tiếp có hướng xử lý trong Settings.
6. Kiểm tra đầu cuối trên desktop thật với cả Claude và Codex, gồm Git prod từ nhánh tác vụ được phép và interaction từ UI/terminal. Token và ID tài khoản chỉ nằm trong dữ liệu kiểm thử/biến môi trường, không commit.

Các test bắt buộc:

- Sai user/chat, nhóm, người gửi là bot, ghép sai/hết hạn hoặc phiên ghép bị thu hồi không đọc được yêu cầu hay thay đổi session. Agent không cấu hình được kênh.
- Callback trùng, bản cập nhật trùng và trả lời đồng thời trên desktop chỉ hoàn thành một lần. Cạnh tranh với hết hạn/hủy/dừng/đóng không cho phép lời gọi đã hủy.
- DevKit prod không nhận quyền lâu dài; không chọn được lựa chọn nhà cung cấp chưa đề nghị; gate từ chối/hết hạn không thực thi; giới hạn nhánh/repo được giữ nguyên.
- Gate Git của UI/terminal chỉ hiện cho đích được phép rõ ràng; gate agent trong Hub chỉ có một thẻ. Thay đổi repo/remote phải phát hiện yêu cầu cũ hoặc kiểm tra lại ngay trước thực thi; checksum của phần hiển thị không đủ.
- Câu hỏi đồng thời, nhiều trường, enum/trường bắt buộc và reply sai ID; khác biệt bỏ qua/hủy; trả lời desktop xóa bản nháp và nút từ xa.
- Khóa, khởi động lại, đổi token, hủy ghép, xóa workspace, server mode và mất kết nối thu hồi quyền đúng thiết kế. Kết nối lại không phát lại lệnh ghi cũ.
- Giới hạn tốc độ, xung đột webhook, timeout sau gửi tin Telegram, phản hồi sai/quá lớn, retry/offset và agent đang chờ trong khi vòng nhận bản cập nhật vẫn chạy.
- Log/audit không chứa token (kể cả URL trong lỗi), nội dung câu hỏi hay diff. Xem trước diff không thoát khỏi thư mục được phép qua đường dẫn tuyệt đối hoặc symlink.

Chạy test tập trung cho package mới và hồi quy interaction/agenthub/git/MCP/bridge, `go test ./internal/arch/`, rồi frontend `bun run build`. Trước phát hành chạy toàn bộ phạm vi Go: `go test ./internal/... ./cmd/...`. Bắt buộc kiểm tra nhanh trên desktop thật; test trình duyệt ở server mode không chứng nhận được duyệt agent từ xa.

## Quyết định còn lại khi triển khai

- Provider/kind Accounts và số migration cụ thể theo quy ước hiện tại của account và storage; tài liệu chưa giữ trước số migration.
- Xác nhận metadata có cấu trúc cho gate profile ngoài Git và cách xác định workspace cho UI/terminal. Thao tác chưa phân loại được chỉ xử lý trên desktop, có lý do rõ ràng.
- Sau này cân nhắc quyền nhà cung cấp lâu dài hoặc deadline DevKit dài hơn. Không cần hai tính năng này để phát hành Cho phép lần này, Từ chối và trả lời câu hỏi từ xa.
- Tài khoản Telegram hoặc dịch vụ bị chiếm quyền nằm trong ranh giới tin cậy của người duyệt từ xa. Nếu prod cần xác thực độc lập thì thiết kế thêm yếu tố riêng; nút chỉ qua Telegram không cung cấp điều đó.


### Session opt-in

Workspace grants are necessary but insufficient for session remote control. Local Telegram preferences hold an explicit `sessionIds` allowlist; legacy/absent lists enable no sessions. The desktop-only `TelegramSetSessionEnabled` bridge mutates this list independently of settings drafts. All session reads, prompts, configuration changes, pending decisions and completion notifications check both the workspace grant and this allowlist. Agent-origin desktop confirmations also require their caller session to be opted in. UI/terminal confirmations retain their separate machine-wide opt-in.

Opt-out revokes local button/form capabilities immediately, clears the selected session's transcript buffer, and strips old message buttons on the next reconciliation. A later opt-in issues new decision buttons. Telegram-created sessions are opted in only after the session is created and the local preference write succeeds.
