# Durable Agent Hub peer outbox

Status: implementation design for OPT-040960 (B5).

## Goal

Keep accepted peer messages queued for delivery across an app restart while retaining the existing `Service.invoke` turn path and all peer authorization, hop, rate, expiration, closed-session, and user-queue rules. A stored approval belongs only to the exact accepted message; it is never a reusable workspace grant.

## Storage and privacy

Migration 47 adds a local `agent_peer_outbox` table. It records stable message id, sender source metadata, sender and target session/workspace ids, their relation at acceptance, body, enqueue/create/update timestamps, hop and exchange limit, sibling flag, exact-message cross-workspace approval, and state (`queued`, `dispatching`, `started`, `steered`, `expired`, `refused`, `ambiguous`). SQLStore implements an optional peer outbox interface so existing Store test doubles remain source-compatible. Active queued/dispatching rows keep full bodies; terminal rows are retained up to 2,000 for bounded history and reply lookup. Bodies are stored as local plaintext like the existing peer transcript, which already persists the same content; do not claim encryption. No body is written to logs or audit events.

Migration 47 also adds a small peer-state table for rate-window timestamps and pair exchange chains. This preserves rate and hop guards over restart. Message/reply lookup is reconstructed from durable outbox rows, bounded by the existing record limit. The store APIs use one SQL statement/transaction at a time; do not hold a transaction while reading sessions or invoking a vendor, because the application database has `MaxOpenConns(1)`.

## Delivery and recovery

`SendMessage` uses its generated MessageID as the idempotency key. The accepted row is durably inserted before delivery. State changes use compare-and-set transitions; a failed durable claim prevents invocation, and a failed queue transition never exposes an in-memory item to a competing drain. Queue delivery still calls `startPeerTurn`, which calls the existing `invoke` implementation. A claim moves `queued` to `dispatching` before handing control to the runtime. On startup, every `dispatching` row becomes `ambiguous`: a crash may have happened before or after the vendor accepted the prompt, so it is never blindly replayed. Queued rows are loaded in timestamp/id order, expired rows settle as expired, missing/closed targets settle without waking them, and the ordinary drain continues to honor a live user queue, pause, pending question, and session state. DevKit stop notices use the same outbox state transitions and original DevKit source metadata.

Every recovered agent message is revalidated against current session existence, closed state, and the exact sender/target workspace and relation captured at acceptance. The same check runs again immediately before a queued message is claimed for dispatch. The saved cross-workspace approval authorizes only that row and exact sender-target pair; changed identity or scope fails closed. Recovery does not create approval and cannot turn a terminal caller into an agent. Hop/rate state is loaded before the service accepts calls; sends and queue drains fail closed until recovery succeeds. Repeated recovery is idempotent.

## Shared delivery substrate

The persisted outbox is the common queue/state substrate for peer messages and future managed events. This change does not add another scheduler or vendor runner: an outbox item is claimed, revalidated, then passed through the same `deliverPeer` / `startPeerTurn` / `invoke` path. Peer messages and the existing DevKit stop notices are produced by this milestone.

## Limits

This is durable queueing, not exactly-once model execution. A crash around runtime acceptance can leave an ambiguous dispatch. Stable message ids provide durable deduplication for the accepted outbox row, but cannot prove whether an external model call happened. Reissuing the send tool creates a new UUID, so caller-level request retries are not deduplicated. Approval provenance is the service call's existing trusted confirmation bit and is persisted only per row; reconstructing the UI confirmation itself is out of scope. Pending UI prompts/paused queue state remains owned by the window and is honored when it is mirrored after startup; recovered targets stay held until that mirror arrives.
