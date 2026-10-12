# Nghiên cứu memory và AI local cho DevKit

Ngày tổng hợp: **12/10/2026**.

## 1. Mục tiêu và trạng thái

Nghiên cứu các cơ chế memory, truy xuất context và model local để **tự thiết kế một hệ thống phù hợp với DevKit**. Hindsight và các dự án khác là nguồn học hỏi, không phải quyết định tích hợp hoặc sao chép nguyên engine.

Định hướng đã thống nhất:

- Ưu tiên xử lý local và cho người dùng chọn model.
- Một runtime inference có thể phục vụ memory, CodeGraph và các tác vụ nhỏ ở nhiều module.
- Tận dụng Go, SQLite, FTS và hệ thống workspace hiện tại.
- Host quản lý nguồn, quyền, scope, revision và sự kiện; model đề xuất kiến thức hoặc diễn giải.
- Tự viết code, schema, prompts và tests từ đặc tả của DevKit.

**Đã thực hiện:** đọc tài liệu/repo chính thức và kiểm tra một số luồng code hiện có của DevKit.

**Chưa thực hiện:** cài engine, tải/chạy model, benchmark chất lượng/RAM, kiểm chứng tích hợp bằng runtime thật, rà soát sáng chế hoặc thiết kế triển khai cuối cùng. Các đề xuất và mức tài nguyên dưới đây là giả thuyết cần đo.

Ngày nghiên cứu không đồng nghĩa ngày phát hành. Nguồn liên kết theo nhánh hiện tại có thể thay đổi; khi triển khai cần ghi commit/model revision và cấu hình cụ thể.

## 2. Phân biệt các tầng

| Thành phần | Vai trò |
|---|---|
| Model | Suy luận, sinh văn bản hoặc trả quyết định |
| Agent runtime/harness | Vòng lặp model → công cụ → kết quả; quản lý phiên/context |
| Memory engine | Thu thập, trích xuất, cập nhật, truy xuất và đưa kiến thức vào context |
| DevKit | Workspace, module, công cụ, quyền, sandbox, UI và quản lý agent |

### oh-my-pi và Jev

**oh-my-pi (omp)** là coding-agent harness có CLI, SDK, RPC và ACP. Nó có thể dùng các model/provider khác nhau và có các chức năng chồng lấn với Agent Hub. DevKit có thể học kỹ thuật memory và công cụ của omp mà vẫn tiếp tục quản lý Claude Code/Codex như hiện tại. [SDK omp](https://omp.sh/docs/sdk)

**Jev của TypeSafe** là model quyết định: Choice, Score và Noul. Vai trò có thể thử là phân loại hoặc chọn workflow. Viết nội dung memory cần khả năng sinh văn bản. Trong nghiên cứu có xuất hiện các website bên thứ ba dùng tên gần giống TypeSafe; nguồn kỹ thuật ưu tiên là tài liệu chính thức. [Chỉ mục TypeSafe](https://docs.typesafe.ai/llms.txt)

## 3. Memory cần nhớ những gì?

| Loại | Nội dung | Ví dụ cho DevKit |
|---|---|---|
| Kiến thức | Facts, giới hạn hệ thống | Agent Hub không khả dụng trong server mode hiện tại |
| Quyết định | Lựa chọn và lý do | Vì sao một seam nằm ở app thay vì bridge |
| Quy ước | Cách làm ổn định | Quy tắc sidebar và document tabs |
| Trải nghiệm | Đã thử gì, kết quả nào | Cách tái hiện một lỗi GUI Linux |
| Quy trình/skill | Cách thực hiện có thể dùng lại | Thêm analyzer hoặc module mới |
| Trạng thái tác vụ | Mục tiêu, việc còn dở, blocker | Handoff giữa phiên/agent |
| Lịch sử phiên | Chi tiết gốc, kết quả công cụ | Tìm lại bằng chứng sau compaction |

DevKit hiện hỗ trợ các kind `decision`, `convention`, `knowledge`, `handoff`. Các loại còn lại trong bảng là nhu cầu đề xuất, chưa phải schema đã có.

Memory cho coding cần phân biệt kế hoạch, giả thuyết, kết quả thực hiện và tính năng đã xác minh. Nhớ một câu trong transcript không chứng minh câu đó đúng.

## 4. Khảo sát các hệ thống

Nhận định về độ phù hợp là đánh giá cho DevKit, không phải xếp hạng benchmark chung.

| Hệ thống | Cơ chế đáng học | Nhận định |
|---|---|---|
| [Mnemopi](https://github.com/can1357/oh-my-pi/blob/main/docs/mnemosyne-memory-backend.md) | SQLite, FTS/embeddings tùy chọn; retain/recall/reflect; context budget; consolidation | Sát bài toán coding và desktop |
| [ReMe](https://github.com/agentscope-ai/ReMe) | Markdown có metadata/wikilinks; BM25; passages theo dòng; embeddings tùy chọn | Học cách kiểm tra, sửa và truy nguyên memory |
| [MemOS](https://github.com/MemTensor/MemOS) | Nhánh local dùng SQLite, FTS5 + vector; dedup và chuyển trải nghiệm thành skills | Học vòng đời kinh nghiệm → quy trình; phân biệt local plugin và service đầy đủ |
| [Hindsight](https://github.com/vectorize-io/hindsight) | Facts/experiences/observations; nhiều đường truy xuất; RRF; evidence; mental models | Nguồn nghiên cứu sâu về thuật toán |
| [Engram](https://github.com/Gentleman-Programming/engram) | Go binary, SQLite + FTS5, MCP/HTTP/CLI | Mốc so sánh sát nền tảng DevKit |
| [Graphiti](https://github.com/getzep/graphiti) | Temporal graph, khoảng hiệu lực, giữ lịch sử fact bị thay thế | Học thời gian và cập nhật kiến thức; graph runtime là lựa chọn riêng |
| [OpenViking](https://github.com/volcengine/OpenViking) | Filesystem context; summaries L0/L1/L2; resources/memory/skills | Học đọc theo tầng và quan sát đường truy xuất |
| [EverOS](https://github.com/EverMind-AI/EverOS) | Markdown nguồn gốc, SQLite/LanceDB index; episodes/profile và cases/skills | Học tính di động và tách loại memory |
| [Mem0](https://github.com/mem0ai/mem0) | Extraction và truy xuất semantic/BM25/entity/time | Managed và OSS có khả năng khác nhau; benchmark phải đúng phiên bản |
| [Cognee](https://github.com/topoteretes/cognee) | Liên kết documents/code/conversations thành graph; đường xử lý model local | Tham khảo kho tri thức liên kết |
| [Letta](https://docs.letta.com/configuration/memory) | Memory filesystem dựa trên Git và dreaming | Học versioning/tự bảo trì; phạm vi gồm cả runtime |
| [LangMem](https://github.com/langchain-ai/langmem) | Primitive extraction/consolidation; quản lý trong lượt và xử lý nền | Tham khảo thuật toán |
| [Claude-mem](https://docs.claude-mem.ai/architecture/hooks) | Capture qua lifecycle hooks, worker nền; search → timeline → chi tiết | Học capture và progressive disclosure |
| [Lossless Claw](https://github.com/Martian-Engineering/lossless-claw) | Raw messages trong SQLite; summary DAG; mở rộng về nguồn | Học phục hồi chi tiết sau compaction |
| [Mastra OM](https://mastra.ai/docs/memory/observational-memory) | Observer/Reflector rút hội thoại thành log có giới hạn | Học context phiên dài; cần kiểm tra mất chi tiết |

### Các thay đổi gần đây đã thấy trong nguồn

- Mem0 mô tả thuật toán mới tháng 4/2026: extraction theo hướng ADD-only, entity linking và truy xuất nhiều tín hiệu. Các điểm công bố thuộc managed platform, có tối ưu ngoài OSS. [Nguồn](https://github.com/mem0ai/mem0)
- ReMe ghi các cập nhật tháng 9–10/2026 về tags, tích hợp agent và Studio. [Nguồn](https://github.com/agentscope-ai/ReMe)
- MemOS có các cập nhật local plugin trong năm 2026. Bản service self-host đầy đủ dùng Neo4j/Qdrant, khác nhánh SQLite local. [Nguồn](https://github.com/MemTensor/MemOS)
- Mastra đưa Observational Memory vào changelog tháng 2/2026. [Nguồn](https://mastra.ai/blog/changelog-2026-02-04)
- Hindsight có thông báo 0.10.2 ngày 29/9/2026; đây là bản được thấy trong nghiên cứu, không khẳng định là bản mới nhất mãi về sau. [Nguồn](https://hindsight.vectorize.io/blog/2026/09/29/version-0-10-2)

Lưu local và xử lý local là hai thuộc tính riêng. Cần kiểm tra endpoint của extraction, embedding, reranking và reflection để biết dữ liệu có rời máy hay không.

## 5. Host ghi bằng chứng và model viết memory

### 5.1 Ba đường ghi

| Đường ghi | Ai chọn nội dung? | Đặc điểm |
|---|---|---|
| Agent chủ động gọi công cụ | Agent chính | Phụ thuộc agent nhận ra điều cần lưu |
| Trích xuất tự động | Worker gọi model với context riêng | Không cần agent chính nhớ gọi save |
| Host ghi sự kiện | Code tại nơi thực hiện thao tác | Ghi điều host trực tiếp quan sát được |

Claude-mem thu input/response của công cụ qua hook và gửi tới worker. LCM lưu lịch sử gốc rồi tạo summaries liên kết. Hindsight nhận nội dung qua retain và tạo kiến thức có provenance. Mnemopi trong omp tự retain theo lifecycle/config. Đây là các cách capture khác nhau, không đảm bảo mọi runtime cung cấp đủ mọi sự kiện.

### 5.2 Đề xuất phân tầng cho DevKit

1. **Evidence:** sự kiện gốc do host quan sát và dữ liệu nguồn có định danh.
2. **Fact:** thông tin trích xuất có liên kết về evidence.
3. **Observation/lesson:** kết luận hợp nhất từ một hoặc nhiều facts.
4. **Knowledge page:** bản tổng hợp cho một chủ đề/câu hỏi ổn định.

Ví dụ trường sự kiện đề xuất:

```json
{
  "type": "test.finished",
  "workspaceId": "ws-1",
  "sessionId": "session-12",
  "command": "go test ./internal/arch/",
  "exitCode": 0,
  "sourceRevision": "working-tree-fingerprint",
  "outputRef": "artifact-42"
}
```

Exit code `0` chứng minh kết quả của lệnh trên trạng thái mã nguồn tương ứng; kết luận “bug đã sửa hoàn toàn” cần bằng chứng bổ sung. Revision nên bao gồm thay đổi chưa commit, không chỉ HEAD.

DevKit trực tiếp thấy các thao tác đi qua service/MCP của mình. Shell bên trong runtime vendor cần hook/adapter tương ứng. Không suy ra test đã chạy chỉ từ câu agent nói. Các task verification do host chạy là nguồn bằng chứng phù hợp hơn.

Thông tin credentials phải được lọc trước capture. Memory hiện tại ở ngoài vault/sync và là plaintext; thay đổi sang encrypted/synced storage cần thiết kế riêng.

## 6. Hindsight: những thuật toán nên học

### 6.1 Retain

Hindsight chuyển đầu vào thành facts, entities và quan hệ; phân biệt `world` và `experience`, xử lý thời điểm xảy ra và thời điểm được biết. Entity resolution có thể ghép nhầm tên gần nhau; cách đó không thích hợp để định danh file/symbol của DevKit. [Retain architecture](https://hindsight.vectorize.io/developer/retain)

Đề xuất DevKit: code quyết định identity/scope; model chỉ trích xuất nội dung. Giữ bằng chứng riêng và xem facts là sản phẩm có thể sửa của quá trình extraction.

### 6.2 Recall nhiều đường và RRF

Hindsight tìm theo semantic, keyword, graph và temporal; hợp nhất thứ hạng bằng Reciprocal Rank Fusion, sau đó rerank và giới hạn context. [Retrieval](https://hindsight.vectorize.io/developer/retrieval)

```text
RRF(memory) = Σ 1 / (k + rank_i(memory))
```

Tài liệu dùng `k = 60`. Đây là điểm khởi đầu để thử, không phải tham số bắt buộc cho DevKit.

Đề xuất các danh sách candidates:

- Lexical từ FTS.
- Memory gắn với file/symbol đang xem hoặc tác vụ đang làm.
- Thời gian và mức độ còn hiệu lực.
- Semantic nếu bật embeddings.

Scope/permission được lọc trước và kiểm tra ở lúc đọc đầy đủ, không phải một điểm số có thể bị các tín hiệu khác vượt qua. Dedupe theo memory ID trước khi tính ngân sách context.

Recall thông thường của Hindsight là truy xuất, không cần gọi LLM sinh văn bản. Embeddings/reranker vẫn có thể dùng model ML riêng. [Configuration](https://hindsight.vectorize.io/developer/configuration)

### 6.3 Consolidation có evidence

Hindsight hợp nhất facts thành observations có trích dẫn hỗ trợ và proof count; observations được tinh chỉnh khi bằng chứng đổi hoặc mâu thuẫn. [Observations](https://hindsight.vectorize.io/developer/observations)

Đề xuất DevKit:

- Job chỉ xét một nhóm facts liên quan trong một scope.
- Model đề xuất giữ riêng/hợp nhất/ghi nhận mâu thuẫn cùng source IDs.
- Host xác minh IDs thuộc scope, nguồn còn tồn tại và revision đầu vào chưa đổi.
- Áp dụng qua CAS; facts/evidence gốc vẫn truy nguyên được.
- Nhiều câu lặp từ cùng một nguồn không được tính như nhiều bằng chứng độc lập.
- Kết quả dùng để hướng dẫn permission không được phép nâng quyền.

### 6.4 Mental models / trang kiến thức

Một mental model là câu trả lời được duy trì cho một câu hỏi được chọn trước; có thể refresh khi kiến thức liên quan thay đổi. [Mental models](https://hindsight.vectorize.io/developer/mental-models)

Ví dụ cho DevKit: “quy ước workspace”, “quyết định kiến trúc”, “các giới hạn triển khai hiện tại”, “bài học debug Linux”. Trang lưu dependency/source revisions để chỉ dựng lại phần bị ảnh hưởng. Đây là bản tổng hợp có nguồn, không phải chỉ thị có quyền cao hơn người dùng.

### 6.5 Hai ngân sách độc lập

Hindsight tách độ sâu tìm kiếm (`budget`) khỏi số tokens kết quả (`max_tokens`). DevKit có thể tìm rộng khi cần, rồi chỉ đưa phần phù hợp vào context. [Retrieval](https://hindsight.vectorize.io/developer/retrieval)

### 6.6 Những điều không nên suy diễn

- Provenance chứng minh xuất xứ; không tự xác minh tính đúng của kết luận.
- Temporal metadata không đồng nghĩa cơ chế CAS của DevKit; cần cả hai.
- Graph memory liên kết kiến thức khác call/reference graph do analyzer tạo.
- Điểm benchmark memory không chứng minh model 0.8B đủ tốt cho toàn pipeline.
- Tài liệu Documents nói có lưu chunks nguồn, trong khi Retain API có đoạn nói đầu vào không lưu verbatim. DevKit nên giữ evidence gốc riêng, không phụ thuộc giả định Hindsight là bản sao lưu byte-identical. [Documents](https://hindsight.vectorize.io/developer/api/documents), [Retain API](https://hindsight.vectorize.io/developer/api/retain)

### 6.7 Footprint nguyên hệ thống: thông tin tham khảo

Hindsight dùng Python/API và PostgreSQL/vector extension; full image có embeddings/reranker local. Tài liệu Installation hiện công bố API full tối thiểu 1,5 GB, PostgreSQL 512 MB; chưa gồm LLM local. FAQ có con số tổng quát khác, nên không coi chúng là benchmark thống nhất. [Installation](https://hindsight.vectorize.io/developer/installation), [FAQ](https://hindsight.vectorize.io/faq)

Các số này mô tả engine gốc, **không phải yêu cầu tài nguyên của bản tự xây cho DevKit**. Thiết kế Go/SQLite và runtime chia sẻ cần đo riêng.

## 7. Model local

### 7.1 Ba khái niệm

- Model nhỏ: ít tham số.
- Quantization: giảm độ chính xác biểu diễn trọng số/cache để giảm tài nguyên.
- Distillation: huấn luyện model học hành vi từ model khác; có cả quantization-aware distillation.

Dung lượng file model không bằng RAM peak. Bộ nhớ còn phụ thuộc cache, context, batch, runtime, concurrency và offload.

### 7.2 Các ứng viên đã khảo sát

| Model | Vai trò thử nghiệm | Ghi chú |
|---|---|---|
| [LFM2.5-230M](https://huggingface.co/LiquidAI/LFM2.5-230M) | Extraction/phân loại rất hẹp | Thông báo 25/6/2026 |
| [LFM2.5-350M](https://huggingface.co/LiquidAI/LFM2.5-350M) | Extraction, structured output | Nhà phát triển không khuyến nghị tác vụ knowledge-intensive/programming |
| [LFM2.5-1.2B-Instruct](https://huggingface.co/LiquidAI/LFM2.5-1.2B-Instruct) | Đối chứng extraction/tóm tắt | Lớn hơn; cần đo trên cùng tập dữ liệu |
| [Qwen3.5-0.8B](https://huggingface.co/Qwen/Qwen3.5-0.8B) | Memory và helper đa ngôn ngữ | Card định vị bản này cho prototyping/fine-tuning tác vụ |
| [Qwen3.5-2B](https://huggingface.co/Qwen/Qwen3.5-2B) | Đối chứng chất lượng | Chưa benchmark DevKit |
| [d1-omni-600M](https://www.liquid.ai/blog/d1-open) | Phân loại/chọn phương án | Phát hành 7/10/2026, experimental; không sinh tokens |

Bổ sung cần khảo sát: [LFM2.5 Q4_0 QAD](https://www.liquid.ai/blog/qad), công bố 19/8/2026. Cần kiểm tra checkpoint cụ thể trước khi chọn.

Danh sách này là shortlist phù hợp để thử, không khẳng định bao phủ mọi model mới nhất. Qwen đã có dòng 3.8 lớn hơn; chưa xác nhận một bản dưới 1B mới hơn 3.5-0.8B trong phạm vi nghiên cứu. [Danh mục Qwen](https://huggingface.co/Qwen)

LFM2.5-350M và 1.2B không liệt kê tiếng Việt trong danh sách ngôn ngữ trên model card đã đọc. Hỗ trợ đa ngôn ngữ của một family cũng không chứng minh chất lượng extraction tiếng Việt của checkpoint nhỏ. Phải thử dữ liệu Việt/Anh/code thực tế.

### 7.3 Model 0.8B nên làm gì?

Model nhận job nhỏ: đoạn hội thoại rõ người nói/thời gian, một symbol hoặc một tập candidates đã chọn. Output có schema, source IDs và khả năng trả rỗng khi thiếu bằng chứng.

Model có thể đề xuất facts, labels, summaries và câu truy vấn. Việc đọc membership, định danh symbol, CAS, RRF, ngân sách và permissions là trách nhiệm của host.

Structured output giúp kiểm soát cú pháp, không chứng minh nội dung đúng. [Grammar/JSON schema trong llama.cpp](https://github.com/ggml-org/llama.cpp/blob/master/grammars/README.md)

### 7.4 RAM: ước tính, không phải số đo

Giả định một phiên text-only, trọng số Q4, KV cache FP16, runtime tối ưu và inference tăng dần. Không áp dụng trực tiếp cho Transformers mặc định hoặc nhiều job đồng thời.

| Model | Context đang giữ | KV cache full-attention ước tính | Tổng bộ nhớ dự trù |
|---|---:|---:|---:|
| LFM2.5-350M | 4K | 48 MiB | 0,5–1 GB |
| LFM2.5-350M | 32K | 384 MiB | 0,8–1,5 GB |
| Qwen3.5-0.8B | 4K | 48 MiB | 0,8–1,5 GB |
| Qwen3.5-0.8B | 32K | 384 MiB | 1,1–2 GB |
| Qwen3.5-0.8B | 262K | 3 GiB | 3,8–5 GB |

Công thức phần cache attention:

```text
bytes = tokens × attention_layers × 2(K,V) × kv_heads × head_dim × bytes_per_element
```

Cấu hình đã đọc: LFM có 6 attention layers, 8 KV heads, head dimension suy ra 64; Qwen có 6 full-attention layers, 2 KV heads, head dimension 256. Vì vậy cache phần này tăng khoảng 12 KiB/token khi FP16. Các recurrent/conv states và buffers không nằm trong cột cache này. [Config LFM](https://huggingface.co/LiquidAI/LFM2.5-350M/blob/main/config.json), [Config Qwen](https://huggingface.co/Qwen/Qwen3.5-0.8B/blob/main/config.json)

LFM card công bố 32K, nhưng config đặt `max_position_embeddings=128000`. Nếu chạy 128K, cache attention khoảng 1,46 GiB; điều đó không xác nhận chất lượng ở độ dài ấy. Prefill/batch có thể làm peak cao hơn các khoảng dự trù. Q4 weights và quantization KV là hai cấu hình khác nhau.

Đề xuất đầu tiên: 4K–8K tokens/job, một job inference hoạt động, queue nền và unload khi rảnh. Đo RSS/PSS, VRAM, peak prefill, độ trễ, CPU và khả năng hủy trước khi công bố yêu cầu hệ thống.

## 8. AI local hỗ trợ CodeGraph và toàn DevKit

| Module | Việc nên thử với model nhỏ | Giới hạn |
|---|---|---|
| CodeGraph | Query expansion, mô tả symbol, diễn giải graph nhỏ có nguồn | Analyzer giữ quyền xác định symbols/edges |
| Search | Nhận diện ý định, tạo lexical candidates | Không đảm bảo mọi kết quả đúng |
| Memory | Extraction, labels, handoff | Consolidation phức tạp cần đối chứng model lớn hơn |
| Agent Hub | Tiêu đề, summary, blockers, việc còn dở | Không suy đoán kết quả tools bị thiếu |
| Git | Tóm tắt diff nhỏ, đề xuất message | Không quyết định guard/push/permission |
| Notes | Tags, tiêu đề, action items | Giữ nguồn và điều kiện |
| Database | Diễn giải schema nhỏ, chọn bảng/cột candidates | Không tự thực thi SQL hoặc xác nhận query an toàn |
| SSH/Kafka | Phân loại lỗi, chọn log đáng xem | Model không được mở rộng quyền vận hành |

Những khả năng trên chưa được xác nhận bằng benchmark 0.8B. Embeddings/reranker là vai trò riêng, không mặc định dùng cùng model sinh văn bản.

### Identity và tính còn hiệu lực

Liên kết memory với `workspaceId + folderId + normalizedRelPath`, revision và symbol ID khi analyzer có định danh phù hợp. Folder được thêm lại có ID mới; tên/path giống nhau không cho phép tự nhận memory cũ. Symbol identity qua rename/reindex cần thiết kế thêm, không giả định luôn ổn định.

CodeGraph không ghép references giữa folders bằng tên; cross-service links đi qua `internal/crossservice`. Kết quả suy luận của model phải tách khỏi cạnh analyzer, không biến text search thành references.

Khi source revision thay đổi, đánh dấu summary/memory cần đánh giá lại. Dùng cache key gồm scope, source revisions, model revision và prompt/schema version. Results từ workspace cũ phải bị bỏ theo generation/cancellation.

## 9. Nền tảng DevKit hiện tại và khoảng trống

| Đã có trong luồng kiểm tra | Khoảng trống đề xuất |
|---|---|
| SQLite/FTS5 và phân trang | Ranking theo mức liên quan tác vụ |
| CAS và immutable history | Hiệu lực kiến thức, superseded/stale trạng thái |
| Source attribution từ host | Evidence IDs và liên kết kết quả đã xác minh |
| Summary/body tách riêng | Recall tự động có scope và lý do chọn |
| Context index giới hạn 8 KiB | Token budget theo model/tokenizer |
| Fingerprint/refresh context | Cache và dependency invalidation theo code |

`Context()` hiện lấy danh sách không có query tác vụ; truy vấn summary sắp xếp theo thời gian cập nhật. Điều này chứng minh luồng context đã kiểm tra ưu tiên độ mới, không kết luận rằng mọi công cụ khác trong sản phẩm đều thiếu ranking.

Nguồn code đã đọc:

- [Memory service](../../internal/workspacememory/memory.go)
- [Summary queries](../../internal/workspacememory/sqlbrowse.go)
- [Browse kinds và scope](../../internal/workspacememory/browse.go)
- [Workspace context](../../internal/agenthub/workspace_context.go)
- [Context refresh](../../internal/agenthub/workspace_context_state.go)
- [WorkspaceCode seam](../../internal/app/workspacecode.go)
- [CodeGraph workspace design](../design/codegraph-workspace.md)

## 10. Kiến trúc đề xuất và thứ tự thử nghiệm

```text
Services / MCP / vendor events / source code
                  ↓
        Evidence có scope và revision
                  ↓
      Job extraction bằng model local
                  ↓
        Facts/memory versioned (CAS)
                  ↓
 FTS + code links + time + optional semantic
                  ↓
         RRF → token-budget selection
                  ↓
          Context / UI / agent tools

Consolidation và knowledge pages chạy nền từ facts/evidence.
```

Một runtime inference dùng chung, chia job theo vai trò thay vì nhúng model vào từng module. Interactive search ưu tiên trước extraction/consolidation; concurrency, timeout, cancellation và unload có giới hạn. Cloud fallback chỉ được bật rõ ràng, không tự gửi dữ liệu khi local thất bại.

Thứ tự đề xuất:

1. Đo baseline DevKit hiện tại trên tập tác vụ thực.
2. Thử lexical ranking + code links + RRF, chưa cần embeddings.
3. Thử extraction 230M/350M/0.8B, có model 2B đối chứng.
4. Thêm evidence lifecycle, stale detection và consolidation nhóm nhỏ.
5. Thử embeddings/reranking và knowledge pages khi có lợi ích đo được.

Hợp đồng job, schema/migrations và UI vẫn cần đặc tả riêng. Tài liệu này không phê duyệt thay đổi runtime hay storage.

## 11. Đánh giá chất lượng

Các điểm Hindsight công bố trên LongMemEval/BEAM là kết quả ở cấu hình benchmark tương ứng, không chuyển trực tiếp thành chất lượng DevKit hoặc model 0.8B. [Hindsight benchmarks](https://benchmarks.hindsight.vectorize.io/)

LongMemEval kiểm tra extraction, multi-session reasoning, thời gian, cập nhật kiến thức và abstention. Cần kiểm tra split/model/token budget và phân biệt retrieval score với answer score. [Benchmark gốc](https://github.com/xiaowu0162/LongMemEval)

Tập tác vụ DevKit cần có:

- Cùng tên file/symbol trong hai folders hoặc workspaces.
- Quyết định cũ bị thay thế, code đã đổi revision.
- Giả thuyết sau đó bị bác bỏ; agent nói test pass nhưng host không có kết quả.
- Tiếng Việt/Anh/code xen kẽ; phủ định và điều kiện áp dụng.
- Recall câu trả lời không có trong nguồn; hệ thống phải biết trả rỗng.
- Extraction thất bại, crash giữa job, replay trùng và concurrent CAS.
- Hủy tác vụ hoặc đổi workspace khi job đang chạy.

Chỉ số: precision/recall có nguồn, attribution, tỷ lệ bịa fact, stale/conflict resolution, cross-workspace leakage, tỷ lệ hoàn thành tác vụ, tokens, độ trễ p50/p95, peak RAM/VRAM, CPU và chi phí ghi memory. So sánh cùng dữ liệu, cùng ngân sách và cả trạng thái cold/warm; lưu model hash, runtime version và cấu hình.

## 12. Triển khai độc lập và pháp lý

Người dùng yêu cầu học nguyên lý và tự xây. Quy trình đề xuất: nghiên cứu nguồn công khai → mô tả thuật toán bằng lời riêng → đặc tả DevKit → tự viết code/schema/prompts/tests. Không sao chép code rồi dịch ngôn ngữ hoặc đổi tên để gọi đó là độc lập.

License chính Hindsight tại thời điểm đọc là MIT. License cho phép sử dụng/sửa/phân phối, kể cả thương mại, với điều kiện giữ copyright/permission notices trong bản sao hoặc phần đáng kể được dùng; điều đó không tự áp dụng cho mọi dependency, model hay tài nguyên bên ngoài. [License](https://github.com/vectorize-io/hindsight/blob/main/LICENSE)

Tài liệu U.S. Copyright Office phân biệt ý tưởng/phương pháp với cách thể hiện được bảo hộ. Đây là nguồn theo pháp luật Hoa Kỳ, không phải kết luận pháp lý cho mọi thị trường. [Circular 33](https://www.copyright.gov/circs/circ33.pdf)

Triển khai độc lập không đồng nghĩa đã rà soát patent/trademark. Nghiên cứu hiện chưa đưa ra cam kết không có rủi ro pháp lý. Khi phát hành cần kiểm tra license/version của dependencies và model weights dự kiến phân phối; ghi attribution cho phần thực sự dùng lại.

## 13. Kết luận nghiên cứu

Ưu tiên học từ Hindsight: **truy xuất nhiều đường + RRF, consolidation có evidence, knowledge pages cập nhật và ngân sách tìm kiếm/context riêng**. Xây trên memory có CAS và scope hiện tại; giữ CodeGraph analyzer là nguồn cấu trúc và dùng model local cho tác vụ hẹp.

Model 0.8B là ứng viên thử nghiệm hợp lý cho một số job, chưa được chọn mặc định. Hiệu quả phải được chứng minh trên dữ liệu DevKit, đặc biệt tiếng Việt, kiến thức lỗi thời, nguồn bằng chứng và workspace isolation.
