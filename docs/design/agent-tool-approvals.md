# Agent tool approvals

DevKit gates and vendor permissions share Agent Hub's permission broker. The
gate owns policy; the broker owns pending request IDs, presentation, cancellation
and the user's answer. The tool executes only after its gate validates the answer.

## Routing

`interaction.Hub.Confirm` keeps its caller/tool/profile grant scope and environment
policy. For an identity-bound Hub agent, it calls the injected `ConfirmAgent`
transport. `Service.ConfirmTool` builds a gate permission request and sends it to
`PermissionWaiter.ResolveGate`. This request uses the existing Agent Hub card,
notification, pending-list recovery and resolution endpoint. It does not also
appear in `InteractionPendingList`.

UI and user-terminal callers keep the InteractionHost popup. Profile and account
selection remain separate interactions. Headless and nil-prompter calls fail
closed before routing. The broker never converts a missing agent session into a
terminal or UI request.

Gate requests include origin, exact tool/summary, resolved environment, profile
ID and host-resolved display name. Production and custom locked environments
offer only a single allow or deny, and reject a forged persistent choice. Open
environments retain grants scoped to caller, tool and profile, held by the Hub.

## Vendor dispatch

The trusted MCP registry explicitly declares ownership with `gateOwnedTool` at
tool registration. A handler declared this way must confirm before executing.
Initially these are `db.query`, `ssh.runCommand`, and Kafka's live topic,
consumer-group, message-read and message-publish operations. Other tools are not
implicitly included merely because they belong to DevKit.

`SessionHost` supplies these declarations on the identity-bound endpoint, and
`Service` passes them in `OpenSpec.GateOwnedMCP` on creation and resume. No
declarations are supplied without a live agent gate transport or in headless
mode. Third-party server annotations and model input cannot declare ownership.

Codex delegates only a recognized MCP tool-call approval whose server and exact
tool match that host declaration. Older versions without tool-name metadata need
one unambiguous open call; missing, conflicting or ambiguous identities retain
the vendor permission path. The answer authorizes dispatch to the gate, not the
operation. Claude receives exact scoped SDK `allowedTools` names for the same
registered tools. Shell/file approvals and other MCP tools retain vendor policy.

## Lifecycle

Each independent tool gate has its own request ID. Parallel gates from one session
may queue; one response cannot resolve another. The session stays awaiting
permission until its final request finishes. Gate deadlines are carried into the
card, and cancel/expiry wins over a concurrently received allow. Stop cancels all
pending broker requests for the session. The target profile ID is captured by
the gate and used by the operation, even if the active target changes meanwhile.

## Validation

Tests cover production repeats and forged grants, terminal fallback, missing
identity/resolver, headless denial, scoped ownership and vendor delegation,
parallel requests and Stop, and real database-handler dispatch blocked until an
approval for the captured profile. Frontend checks keep the entire SQL visible
and use the actual locked environment's name.

Run `go test ./internal/interaction ./internal/agenthub ./internal/mcp ./internal/arch`
and the broker race tests. Claude's SDK forwarding is checked against its pinned
adapter source and config tests; a signed-in real-vendor inference turn is not
part of these checks.
