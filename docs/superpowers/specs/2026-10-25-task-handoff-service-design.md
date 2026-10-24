# Task handoff orchestration service

## Goal

Move the `agenthub.createAgentSession` task handoff sequence out of the MCP handler into a reusable orchestration package. The handoff remains: check the task before creating a child, create the child, assign the task, optionally record its worktree branch, then send a bounded task brief as the first message.

## Boundary

Add `internal/agentwork`, a small orchestration package with local task/session/result DTOs and injected operations. It will not import `agenthub`, `tasks`, `git`, `storage`, `bridge`, `mcp`, `app`, or `crypto`. The caller (MCP now, another host later) supplies operations for task lookup, child creation, assignment, branch recording, brief delivery, and child close. This keeps the service reusable without creating a Tasks → Agent Hub dependency.

## Authorization and behavior

The service owns ordering and rollback only. Its injected operations retain the existing capability gates and call identity: task check and assign use the original parent context; branch recording uses the child identity; sending uses the original parent context with the existing peer-send grant; creation retains Agent Hub, worktree, model approval, and filesystem guards in MCP. It checks before create, closes the new child after assignment or branch recording fails, and keeps the claimed child if first-message delivery fails, returning a refusal in the result.

The task brief remains bounded at 8,000 runes and retains current wording. MCP maps the service result back to the existing `childOnTask` wire shape; non-task child creation stays on its current path. No durable queue is introduced.

## Verification

Service tests cover order, no-create on failed precheck, rollback on assignment/branch failure, and delivery refusal without rollback. MCP integration tests continue to verify capability scopes, identities, task title/brief, worktree branch, rollback, and unchanged serialized output. Run focused `internal/agentwork` and `internal/mcp` tests plus `internal/arch`.
