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
| Unattended policy | D1 / OPT-0a45f5 | codex/scoped-approvals-d1 @ `f296604e` | Integrated `6eb277f9`; exact `db.query` slice in review; independent security/UI reviews passed |

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

## D1 delivered scope and remaining work

The person explicitly approved the v1 policy before implementation. The delivered operation is exact `db.query`, issued from its live Agent Hub permission card with a finite count/lifetime and revocable in Settings. SQL, caller, resolved profile (including inherited parent/tunnel generations), and environment policy revisions bind each approval. Per-call reservation tokens prevent a duplicate start/cancel from consuming another call's use. Production manual confirmation remains functional; missing grants, blocked environments, server mode, locked vault, revoked/expired approval and revision drift cannot use a pre-approval.

The other operation classes in the approved matrix have not gained pre-approval support. This milestone does not implement the managed-work ledger/leases, context/result packages, scheduler batching or Team/Timeline UI, and does not prove a reduction in real model usage. C1 remains a design deliverable. No task was moved through a terminal acceptance gate; no push or main merge was performed.

D1 author/reviewer checks passed: scoped Go tests including the real card/bridge/MCP/gateway path; per-token replay, expiry and profile/tunnel/env ABA regressions; architecture; frontend build; 15 isolated UI/bridge/navigation tests. A server-tag broad run triggers desktop-only bridge test failures, so the final integrated native run is recorded separately below.

## Final integrated validation

PR #317 follow-up (OPT-67e0a0): merged `origin/main` at `b1c1ea3f` (#316) in `f6c84d6a`. The `Service` conflict retains both pending-recovery identity and the new session-temp test seam. Post-merge `TMPDIR=/private/tmp go test ./internal/agenthub ./internal/sandbox ./internal/storage ./internal/arch -count=1` passed all four packages, including the sandbox probe that failed on the earlier baseline below. Root `AGENTS.md` was reduced from 220,112 to 6,536 UTF-8 bytes (97.0% smaller). All 2,290 original lines were preserved across 23 focused `docs/agent-reference/` guides, with the module-shell relative link adjusted for its new location. The root has a 10 KiB maintenance budget and an on-demand task map. Independent review, line preservation, local link/fence checks and `git diff --check` passed. No fresh model session was run, so this is a measured instruction-byte reduction, not a measured token reduction.

- Native `TMPDIR=/private/tmp go test ./internal/... ./cmd/... -count=1`: every package passed except `internal/sandbox`, whose existing `TestSmokeSeatbeltDeveloperTools/cgo-process-info` failure was reproduced on unchanged main. Desktop bridge tests passed in this native run. Full log: `/private/tmp/devkit-team-foundations-go-tests.log` (local diagnostic, not a committed artifact).
- `go vet` passed for agentwork, agenthub, interaction, mcp, storage, database, ssh, environments, bridge and app.
- `TMPDIR=/private/tmp go test -tags server . -count=1` passed for the integrated root/composition package.
- `go test -race ./internal/interaction ./internal/mcp -run 'ScopedApproval|DBQueryManualProd' -count=1` passed. Earlier B5/B6 outbox/pending recovery race tests also passed.
- Frontend build (TypeScript, Vite and performance budget) passed. The five-file isolated subscriber/scoped approval/navigation run passed **43 tests, 0 failures**.
- Markdown local links/code fences and `git diff --check` passed.
- No native app restart or live external database/model request was performed. Settings list/revoke UI was reviewed in code; the available component tests use SSR, so no claim of a live desktop interaction test is made.
