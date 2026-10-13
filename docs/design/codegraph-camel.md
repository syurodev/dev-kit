# Camel Java routes trong CodeGraph

`CamelExtractor` đọc AST của `RouteBuilder` và các lớp kế thừa trong project. Không chạy
`configure()`, không nạp giá trị cấu hình runtime, không kết nối Kafka. Dependency jars có thể
giúp JavaParser giải symbol; khi thiếu jar, phải có import/base type Camel xác định được từ source.
Method chỉ trùng tên `from`, `process` hoặc `to` không đủ để nhận diện Camel.

Mỗi `from(...)` là một route template, kể cả nằm trong vòng lặp. Project helpers được truy vết
với binding đối số, theo từng nơi gọi; hai lời gọi `register(...)` tạo hai node khác nhau.
Identity gồm owner có module qualifier, method signature và lexical call path/ordinal, không
dùng số dòng hoặc route ID runtime. Di chuyển dòng không đổi ID; thêm/xóa/reorder route hoặc
helper call có thể đổi ordinal. Trùng route ID giữ cả hai entries và có cảnh báo, không hợp nhất.
Source location trỏ tới `from(...)` trong method/helper khai báo route.

Kafka consumer có `kind=message`, `verb=MESSAGE`; `routeId`, `topic`, `consumerGroup` là
literal, constant hoặc biểu thức source. Helper trả URI hoặc `KafkaEndpoint` được truy vết;
`setTopic` và `setGroupId` trên configuration của endpoint được trả về ghi đè URI. Vì vậy mẫu
webhook giữ topic `{domain.source().topic()}`, không lấy `webhook-dispatch-*` từ URI làm topic.
Đối số callback qua helper cũng được bind: `process(handler)` ở mẫu fanout nối đến
`OrganizationFanoutProcessor.process`, không nối mọi implementation của interface `Processor`.

Route node nối đến lời gọi nghiệp vụ trong lambda, method reference hoặc project Processor
implementation/factory. Cạnh callback không xuất phát từ `configure()`/method đăng ký route.
`direct:`/`seda:` tạo node nội bộ; destination literal có đúng một source được nối khi dựng flow.
`to("kafka:...")` là produce access, không phải entry consumer. Kafka accesses dùng client
`camel-kafka`, nối với snapshot/query hiện có; source expressions chưa giải được nằm trong
`Unresolved` của báo cáo xuyên service, không được coi là topic thật hoặc wildcard join.

Schema bổ sung ba cột route metadata bằng migration additive, giữ HTTP entries cũ. Phạm vi
hỗ trợ và cảnh báo được lưu cùng transaction index trong `index_meta.entry_support`, phục vụ
cả resident lẫn disk views. HTTP `endpoints` JSON không đổi; `entryPoints` là phần bổ sung.
Analyzer identity hiện có (jar path/size/mtime và Go build identity) làm mất hiệu lực việc tái
sử dụng index của bản analyzer cũ. Rebuild jar rồi reindex để nhận Camel entries.

UI hiển thị route ID/topic/group, tìm kiếm cả metadata và phân biệt “Indexed” với giới hạn
entry discovery. Index không có entries hoặc còn cảnh báo có trạng thái cảnh báo; không khẳng
định service không có luồng xử lý. Index cũ chưa báo phạm vi hỗ trợ được giải thích là chưa rõ.

Giới hạn: Java DSL fluent chains xuất phát từ `from` đã nhận diện; registration helpers phải
là project method calls trực tiếp. Không diễn giải arbitrary DSL extensions, bean/reflection/XML
routes hoặc số route runtime. Factory nhiều return, overload chưa phân biệt được, binding gán
lại, setter có điều kiện/nhiều lần, handler ngoài source và internal destination không duy nhất
giữ trạng thái chưa giải được. Helper traversal bị giới hạn depth 16 và 10.000 visits. Đây là
phân tích source, không phải chứng minh toàn bộ control flow hay thứ tự runtime.

Kiểm thử headless, sau khi cài JDK 21+, Maven, Go theo `go.mod` và Bun:

```sh
cd analyzer && mvn -B package
cd ..
go test ./internal/codegraph/ ./internal/crossservice/ ./internal/arch/ -count=1
cd frontend && bun test src/modules/codegraph/ && bun run build
```

Fixture `camel-project` có API stubs tối thiểu đúng namespace Camel/Spring để chạy analyzer
không cần dependency cache của project thực tế. Fixture compile được bằng `javac`; Java tests
và Go integration test gọi analyzer thật, kiểm metadata, migration, Kafka snapshot, callback
isolation, helper bindings, inherited configure và source identity qua thay đổi số dòng. Các
trường hợp cố ý không giải được kiểm rằng cảnh báo còn nguyên và không tạo flow/giá trị giả.
Chưa kiểm chứng trên project thực tế hoặc GUI desktop.
