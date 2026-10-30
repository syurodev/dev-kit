# Plan và prompt bàn giao: DevKit harness, team và Jev

Ngày: 2026-10-30. Trạng thái: **plan để review; chưa triển khai bởi tài liệu này**.

Thiết kế: [DevKit harness: team sessions và Jev](../specs/2026-10-30-devkit-harness-team-jev-design.md). Đây là continuation của TEAM/harness hiện có, không tạo một initiative implementation trùng với các task đang được agent khác sở hữu. Người dùng giao agent hiện tại viết doc/plan và review; implementation thuộc agent được người dùng giao riêng.

## 1. Các phần việc và điều kiện hoàn thành

| Phần | Deliverable | Điều kiện review |
| --- | --- | --- |
| P0 — Reconcile | Inventory source hiện tại, mapping backlog/owners và các lệch hợp đồng | Phân biệt foundation, thay đổi chưa review và capability thực tế; không sửa phần của agent khác |
| P1 — Tool delegation | Review công việc child tool groups hiện tại | Không nhầm on-only S với narrowing; test request/forward/locks/live/recovery/first dispatch; không có đường nâng quyền |
| P2 — Role/team | Spec cụ thể + slice implementation được người dùng giao sau review | Reuse canonical roles, model profiles, packages, Tasks và opt-in; role không là quyền |
| P3 — Rules và dispatch | Tiếp tục slice TEAM đang thiếu, rules-only read model/batching | Ownership, stop proof, operation reconciliation và gate pass trước dispatch; không ambiguous replay |
| P4 — Jev shadow | Decision port/adapter, fixtures, replay/eval report | Chưa action/wake theo Jev; live API chỉ sau cấu hình và data opt-in |
| P5 — Limited rollout | Event routing/wakeup opt-in nếu eval đạt | Host candidate validation, stale checks, fallback, no privilege/acceptance bypass; có baseline tổng usage |
| P6 — Team/Timeline | Một group document dùng read model thật | Shared shell, không gọi model để render, không phụ thuộc Jev |

P1/P2 có thể được thiết kế độc lập; không tự dispatch thêm agent để chạy song song chỉ vì bảng này. P3 tái sử dụng backlog đang có, không viết scheduler mới trong P4. UI role đơn giản có thể được xây trước P6, nhưng không được quảng bá autonomous team khi workflow chưa hoạt động. Những phần cần quyết định sản phẩm còn mở nằm ở §11 của thiết kế.

## 2. Checklist reviewer

- Identity có đến từ authenticated caller? Parent có đúng direct parent, đúng workspace và current owner không?
- Role hướng dẫn và quyền thực thi có tách biệt? QC mapping có vô tình tạo quyền write/approve không?
- `toolGroups` thực tế có giới hạn tool không, hay chỉ bật thêm S? UI/docs có nói đúng điều này không?
- Group requests có một ID/revision xuyên route, phân biệt decline và block, không resurrect sau restart?
- Runtime đã mở nhưng grant thất bại có nhận task/prompt trước khi policy đúng không?
- Live publication, saved configuration và vendor refresh có bị báo chung là success không?
- C1 hiện từ chối child có S, trong khi delegation ghi S: có thiết kế binding/package revision đã review trước khi nối hai phần không? Không được bỏ check hoặc sửa lịch sử để vượt preflight.
- Attempt/package/binding và evidence có đúng revision/generation? Không dùng `updatedAt` làm CAS.
- Pause/vault/user queue/pending interaction có chặn cả inference/wake và dispatch tại boundary không?
- Jev chỉ chọn từ candidate set host tính? Candidate set có được revalidate trước action không?
- Blocker/approval/user action có bị model phân loại nhầm rồi bỏ hoặc defer vô hạn không?
- Confidence có bị coi là authorization hoặc acceptance? Provider error có mặc định allow không?
- Payload provider có tối thiểu, được phép gửi và không lộ trong audit/JSONL không?
- Cost report có tính cả worker/Jev, chất lượng và blocker latency, thay vì chỉ giảm token cha không?
- Có thêm runner, ledger/outbox, model configuration hoặc sidebar/tab store trùng không?

## 3. Validation theo thay đổi

Docs-only: kiểm tra links, code fences và `git diff --check`; không claim runtime tests đã chạy.

Implementation: dùng commands trong AGENTS.md, gồm `go test ./internal/... ./cmd/...` và `go test ./internal/arch/`. Frontend thay đổi thì `cd frontend && bun test --isolate` và `bun run build`. AppService method đổi thì `wails3 generate bindings` trước frontend build. Race tests có mục tiêu nếu sửa launch/live publication, request CAS hoặc delivery.

Regression tests trọng tâm: explicit/omitted/empty tool selection; direct user và parent/forward requests; stale/duplicate decisions; block/revoke; scope/caller forgery; grandchild; missing harness/provider; reload/restart/rewind/account switch; ambiguous dispatch; lock/pause/user queue; real MCP listing và call cùng enforcement; capability/prod/unknown-env gates không đổi. Unknown public error code phải được published hoặc deliberately redacted.

Eval Jev riêng với deterministic tests: offline labeled fixtures và holdout; rules-only baseline; shadow; provider failures, Vietnamese/English và prompt injection; ghi model/question/policy version. Threshold và sample/acceptance plan chốt trước rollout. Mock, browser gallery và scripted vendor không thay native/live acceptance.

## 4. Prompt bàn giao cho agent khác

Prompt sau dùng để bắt đầu reconciliation/design của slice được giao. Chỉ đổi sang implementation khi người dùng giao rõ slice đó; không tự coi toàn bộ plan là lệnh triển khai.

```text
Bạn làm việc trong /home/ptvu3/Downloads/dev-kit.

Đọc AGENTS.md và:
- docs/superpowers/specs/2026-10-30-devkit-harness-team-jev-design.md
- docs/superpowers/plans/2026-10-30-devkit-harness-team-jev-plan.md
- Các reference trực tiếp liên quan được link trong thiết kế.

Mục tiêu sản phẩm: DevKit là harness quản lý session, tool, quyền, context,
Tasks và recovery. Team dùng sub-sessions hiện có; role presets/team templates
bổ sung vào nền đó. Jev là decision provider tùy chọn cho câu hỏi nhỏ, không
thay Coordinator lập kế hoạch, không authorize tool hoặc accept task.

Trước hết thực hiện P0 cho slice tôi giao:
1. Đọc git status và source thực tế. Không sửa/revert thay đổi agent khác.
2. Chỉ rõ capability nào đã có, cái nào pending review, cái nào còn thiếu.
3. Mapping vào backlog/implementation plan TEAM hiện có để tránh làm trùng.
4. Nêu quyết định còn mở có ảnh hưởng đến slice, và đề xuất nhỏ nhất.
5. Nếu tôi chỉ giao plan/review, dừng ở doc/report, không sửa code.
   Nếu tôi giao implementation, chỉ triển khai slice được chỉ định.

Những ranh giới bắt buộc:
- Không tạo alternate runner hoặc đi vòng service/gateway.
- Role là trách nhiệm; capability/grants/gates mới quyết định quyền.
- Child tool groups đang có implementation riêng: reuse/review, không viết lại.
  S on-only không phải narrowing; [] không được mô tả là không còn tool nếu
  workspace W vẫn bật nhóm. Thiếu tool không tự bật thêm.
- Con có thể xin thêm qua cha hoặc user. Chấp thuận phải là structured host
  operation, không phải peer text/Jev decision. Cha không ghi đè user block.
- Reuse canonical roles, model profiles, Tasks, packages, ledger và peer outbox.
  internal/agentwork giữ stdlib-only; provider transport ở adapter/composition.
- Reconcile C1: actual child có S hiện bị HarnessOverridePresent, còn luồng
  delegation mới ghi S. Không bỏ check để nối nhanh; thiết kế/review binding
  và immutable package revisions trước khi cho managed dispatch chạy.
- Managed team opt-in theo parent task UI; role/create-child không tự activate.
- Không replay ambiguous dispatch, không coi lease expiry là writer đã dừng,
  không restore pending permission cũ thành answerable, không vượt prod gate.
- Jev chỉ nhận bounded admitted state và finite candidate IDs do host tạo.
  Revalidate trước side effects. Provider lỗi/low confidence không default allow.
- P4 chỉ shadow: không action/wake theo Jev. API thật cần provider/data opt-in;
  không gửi task/code/log/transcript thật như bước thử ngầm.
- Không đổi TEAM1 acceptance/review/integration policy như tác dụng phụ.

Bàn giao để review:
- Phạm vi đã làm, files và semantics cuối cùng.
- Mapping hiện trạng/backlog và các quyết định còn mở.
- Tests/eval đã chạy kèm kết quả, baseline failures và giới hạn xác minh.
- Rủi ro migration, lifecycle, policy, dữ liệu và vendor refresh.
- Không tuyên bố production-ready hoặc giảm usage khi chưa đo.

Không push, merge, đóng task, bypass verifier hoặc tự mở rộng slice.
```

## 5. Hồ sơ validation của tài liệu

Hai tài liệu được viết từ việc đọc source và các nguồn đã link; không có runtime/code change do đợt viết doc này. Checkout có thay đổi implementation đồng thời của công việc child tool groups, được giữ nguyên. Kiểm tra formatting/links được ghi trong báo cáo bàn giao chat, không suy ra các test runtime của thay đổi đồng thời đã pass.
