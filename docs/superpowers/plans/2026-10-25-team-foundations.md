# Team orchestration foundations — implementation tracking

Started 2026-10-25 at the person's request. Implementation uses Codex Luna subagents; the parent reviews and integrates. This is not a claim that the complete Team/Timeline product is implemented.

## Work boundaries

| Work | Existing/new task | Branch | Owner |
| --- | --- | --- | --- |
| Claude quota and typed result failures | OPT-40f16b | codex/claude-usage-failure | Integrated `f2bfc440`; task in review |
| Shared handoff orchestration service | B7 / OPT-828dec | codex/team-handoff-b7 | Integrated `7d13ef73`; task in review |
| Durable peer outbox | B5 / OPT-040960 | codex/peer-outbox-b5 @ `b61dd768` | Integrated `558c35fd`; tests passed; task in review |
| Interrupted pending permission/question metadata | B6 / OPT-e67e8f | codex/pending-recovery-b6 @ `2d654717` | Integrated `26a7c4cc`; tests passed; task in review |
| Conversation-centred shell | C1 / OPT-484f67 | design deliverable | Concrete spec in review; no C3–C6 implementation implied |
| Unattended policy | D1 / OPT-0a45f5 | codex/scoped-approvals-d1 | Scope explicitly approved by the person on 2026-10-25; Luna implementing |

The active comment-paging, sandbox and conformance-follow-up tasks in other DevKit sessions remain with their current owners. External Codex subagents are not DevKit Agent Hub sessions: this MCP caller cannot claim tasks as an agent. Branch/status records are used without fabricating a session identity.

The initial implementation worktrees start at main 0601c248; D1 starts at integration f2bfc440 after explicit scope approval. Integration branch: codex/team-foundations. Main checkout contains the earlier draft spec and reference images; preserve them. Do not push, merge to main, close task gates or publish without the person's authorization.

## Required review

- Preserve capability-gateway calls, workspace scopes, model-choice gates and child identity in B7.
- A transcript echo does not prove a vendor accepted a prompt; crashed dispatch remains ambiguous.
- Restore queued messages only after scope and user-queue state are known. Preserve rate/hop guards over restart.
- Never restore an old permission into an answerable Allow action after its vendor process has died.
- Keep generic ACP runtime vendor-neutral; normalize cache counters separately and retain raw error accounting.
- Do not turn pending task comments or an agent's own test claim into accepted evidence.

## Validation record

- Baseline: `go test ./internal/arch/` passed on the main checkout.
- Integrated B7 (`7d13ef73`) and Claude quota/failure accounting (`f2bfc440`): `TMPDIR=/tmp go test ./internal/agentwork ./internal/agenthub ./internal/storage ./internal/arch -count=1` passed.
- Broader and final integration validation: record outcomes here after review.
- B5/B6 integrated with phase0/B7: `TMPDIR=/private/tmp go test ./internal/agentwork ./internal/agenthub ./internal/storage ./internal/mcp ./internal/arch -count=1` and `go vet` for the same packages passed. Subscriber tests passed (28); frontend TypeScript/Vite build passed.
- Race-enabled Agent Hub regressions for peer outbox/order/rate guards and pending-request recovery/cancellation passed (`go test -race`, targeted test names).
- Broad `TMPDIR=/tmp go test ./internal/... ./cmd/... -count=1` passed all packages except Git/MCP path-alias assertions and sandbox process-info. Retest with canonical `TMPDIR=/private/tmp go test ./internal/git ./internal/mcp -count=1` passed both: `/tmp` is a macOS symlink, so the first Git failures are not a guard regression.
- `TestSmokeSeatbeltDeveloperTools/cgo-process-info` also fails on the unchanged main baseline (`PROBE count=136 errno=0`, probe code 10). The remote PR316 sandbox follow-up is outside this branch's 0601c248 baseline; do not report this broad run as entirely green.
- `go vet ./internal/agentwork ./internal/agenthub ./internal/mcp ./internal/storage` passed after phase0/B7 integration.
- No runtime quota exhaustion experiment is needed; sanitized fixture shapes and ACP pipes cover refusals.
