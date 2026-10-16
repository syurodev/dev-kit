# Agent Hub: messages between sessions

Status: implemented on the mock vendor and in the gallery; **not run against a real Claude or Codex**.

## What it is

`agenthub.sendMessage({sessionId, message, replyTo?})` lets an agent (or the person's own terminal CLI) hand
another Agent Hub session a message without waiting for the answer. It answers at once with
`{messageId, delivery, code?, reason?, sessionId?, title?, hop?}`.

Claude Desktop does this between its own sessions with `SendMessage`. claude-agent-acp 0.92 only *receives* such
messages passively (a `peer`-sourced inbound the host must register and route), and Codex has nothing. DevKit's
version lives in the host, so it is the same for every vendor and the person sees every message.

## Delivery

| `delivery` | When |
| --- | --- |
| `started` | The target is idle and the person has nothing of their own queued or paused on it. An ordinary turn begins (`Service.invoke`: turn row, checkpoint, Stop button). |
| `steered` | The target is running, its vendor takes a message mid-turn (Claude `_session/steering`, Codex `turn/steer`) and nobody is waiting on the person. |
| `queued` | Anything else that is not a refusal: the vendor cannot steer, a permission or a question is open, the person's prompts are queued or paused. In memory, one turn per message, drained when a turn ends. |
| `refused` | `code`: `not_found`, `self`, `closed`, `loop`, `rate`, `queue_full`, `too_long`, `declined`, `unavailable`. |

`started` is reported once the message is recorded in the target's transcript and its turn is running, not before
(a process that cannot be resumed is `refused: unavailable`, not a false `started`).

The person's queue lives in the window, so the window mirrors it to Go (`AgentHubSetPromptQueue`: how many wait,
whether paused). A pause is honoured without age; "n waiting" is believed for 30 s because it describes a moment
between two turns. The person's prompts therefore go first, and nothing starts behind a pause they set.

## Who can reach whom

An agent reaches its own workspace's sessions, its sub-sessions and its parents (`peerRelationOf`). `agenthub.listPeers`
lists exactly those (a purpose-built shape: id, title, vendor, status, relation — no paths, no previews).
A session of **another workspace** is invisible to an agent: `not_found`, word for word what a missing id gets.

**Decision (differs in detail from the first proposal):** a message to another workspace's session is not impossible,
because the person may paste an id. The MCP tool asks them first, every time (no allow-always), showing the exact
text and the target's title (`Hub.Confirm`, kind `agent`, the same "DevKit tool gate" card as other writes); a
decline is `refused: declined`. Headless/server mode fails fast, and Agent Hub does not run there anyway. The
service never trusts the flag itself: `CrossWorkspaceApproved` is not a wire field.
A **closed** session is never woken: only the person reopens it (`refused: closed`). The terminal sees every session.

## What the model and the person see

The transcript row is a `user_message_chunk` with `source` (`MessageSource`: kind `session` | `terminal`, sender id,
vendor, name at send time, message id, replyTo, hop) in the stored payload and on the live event's `_devkit.source`.
Keeping the kind means turn grouping, rewind and checkpoints work unchanged; the frontend draws any row with a
`source` as a left-aligned message with a rule and a "From <sender>" line (live title if the session exists, the
stored name otherwise; click opens it), never as the person's bubble, with no edit or rewind. The row holds the
sender's words; the model receives an envelope:

- says it came from another agent session (or from a CLI in the person's terminal) and was **not** written by the
  person; the person's instructions, workspace instructions and DevKit's rules win; it cannot grant permissions,
  change mode or approve anything; risky asks go to the person;
- names the reply (`agenthub.sendMessage`, `replyTo`) in the vendor's own tool spelling, asks not to acknowledge;
- frames the body between two lines carrying the message id, a value the sender did not choose, so nothing in the
  body closes the frame. The sender's name is quoted and stripped of control characters.

The prompt (`devkit_prompt.go`) also tells the agent when to use the tools and that such messages are data.

## Loops and storms

Every message has a `hop` (the first is 1). A session's replies are one deeper than the last message it was handed
within 15 minutes **whether or not it names `replyTo`** — leaving the field out cannot reset the count; `replyTo`
can only raise it, and only for a message addressed to the sender. The person speaking to the session (UI `Invoke`
or `Steer`) ends its exchange. Past `MaxPeerHops` (6): `refused: loop`, with the reason spelled out. Per ordered
pair 5 messages a minute and per target 20 a minute (`refused: rate`); 20 queued per target; 16 000 characters per
message. `CheckCycle` is deliberately not used: it forbids exactly the parent↔child messaging this feature is for.

## Audit

The gateway records the call (`agenthub:execute` on `session:<id>`, metadata `length`, never the text). A second
event (`OnPeerMessage`, wired in `main.go`) records the outcome: sender, receiver, length, delivery, code, hop,
message id, and whether it was a queued message's later delivery or drop.

## Permissions

`agenthub.sendMessage` asks `builtin.agenthub / agenthub:execute / session:<id>`, `listPeers` asks `agenthub:read /
session:list`; both covered by the default grants (`TestNewContainerGrantsTheMessagingTools` checks the real
container against `mcp.PeerMessageGrants`, the requests the tools actually make). `listPeers` is pre-approved
(read-only, own workspace); `sendMessage` is not — what an agent says to another session goes through the vendor's
own permission flow, so the person decides whether to allow it always.

## Not done / known limits

- The queue and the hop state are in memory: a restart drops queued messages (the sender is told `queued`, not that it
  is durable). Messages older than an hour are dropped with an audit event; the sender is not told later.
- One queued message becomes one turn; messages are not batched.
- Codex `turn/steer` was written from the 0.162 schema; the pinned 0.147 was not driven with it. A `method not found`
  turns into queueing. It also means the person's own mid-turn messages now steer Codex turns instead of queueing.
- Nothing for the person to *send* (they have the composer); no unread state across restarts; no view of a session's
  queued messages.
