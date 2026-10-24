# Team orchestration foundations — implementation tracking

Started 2026-10-25 at the person's request. Implementation uses Codex Luna subagents; the parent reviews and integrates. This is not a claim that the complete Team/Timeline product is implemented.

## Work boundaries

| Work | Existing/new task | Branch | Owner |
| --- | --- | --- | --- |
| Claude quota and typed result failures | OPT-40f16b | codex/claude-usage-failure | Luna; parent review |
| Shared handoff orchestration service | B7 / OPT-828dec | codex/team-handoff-b7 | Luna; parent review |
| Durable peer outbox | B5 / OPT-040960 | codex/peer-outbox-b5 | Luna; parent review |
| Interrupted pending permission/question metadata | B6 / OPT-e67e8f | codex/pending-recovery-b6 | Luna; parent review |
| Conversation-centred shell | C1 / OPT-484f67 | design deliverable | Concrete spec; no C3–C6 implementation implied |
| Unattended policy | D1 / OPT-0a45f5 | design deliverable | Concrete spec for review before policy code |

The active comment-paging, sandbox and conformance-follow-up tasks in other DevKit sessions remain with their current owners. External Codex subagents are not DevKit Agent Hub sessions: this MCP caller cannot claim tasks as an agent. Branch/status records are used without fabricating a session identity.

Each implementation worktree starts at main 0601c248. Integration branch: codex/team-foundations. Main checkout contains the earlier draft spec and reference images; preserve them. Do not push, merge to main, close task gates or publish without the person's authorization.

## Required review

- Preserve capability-gateway calls, workspace scopes, model-choice gates and child identity in B7.
- A transcript echo does not prove a vendor accepted a prompt; crashed dispatch remains ambiguous.
- Restore queued messages only after scope and user-queue state are known. Preserve rate/hop guards over restart.
- Never restore an old permission into an answerable Allow action after its vendor process has died.
- Keep generic ACP runtime vendor-neutral; normalize cache counters separately and retain raw error accounting.
- Do not turn pending task comments or an agent's own test claim into accepted evidence.

## Validation record

- Baseline: `go test ./internal/arch/` passed on the main checkout.
- Targeted implementations and integration validation: record outcomes here after review.
- No runtime quota exhaustion experiment is needed; sanitized fixture shapes and ACP pipes cover refusals.
