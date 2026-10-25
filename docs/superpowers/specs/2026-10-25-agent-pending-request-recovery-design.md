# Agent pending-request recovery

## Goal

Survive an app restart during an Agent Hub permission or elicitation wait with an explicit, sanitized explanation. A request that existed before restart must never be restored as answerable.

## Storage boundary

Add a metadata-only `agent_pending_requests` table and a local optional `PendingRequestStore` interface implemented by SQLStore. Records contain request ID, session ID, kind (`permission` or `elicitation`), creation/deadline, and resolution time/reason. They never contain ACP params, question text, answers, or secrets. Migration 47 creates the peer outbox and guard state; migration 48 creates this table in order for existing databases.

Waiters record metadata before publishing a pending request and persist closure before releasing a user response. They recheck cancellation/deadline after persistence returns, so a slow write cannot turn an expired request into approval or an answer. A failed open denies permission or declines elicitation without publishing a request. A failed completion write leaves it pending and cannot release the response. Persistence errors are reported through a payload-free, nonblocking callback. Existing in-memory behavior remains available when hooks are absent in test fakes.

## Recovery

`RecoverStaleSessions` prunes terminal metadata older than 30 days, reads unresolved summaries, then processes sessions. Missing and closed sessions have their old request IDs invalidated without changing a closed session's Stop or adding an interruption banner. For an active or idle session with unresolved metadata, recovery stores a Stop explanation naming permission approval, a question, or both; writes the transcript notice; invalidates the old request IDs; returns an in-flight row to idle; and acknowledges recovery metadata. In-flight rows stay busy until old IDs are durably invalidated. The notice ID includes a per-Service recovery ID. SQLStore checks that exact marker with a targeted transcript query, so retries within one startup do not duplicate it and a later restart can still draw a fresh banner. Metadata rows from an unacknowledged recovery remain eligible for retry.

If required metadata, Stop, or transcript persistence fails, recovery returns an error and does not make a request answerable or an in-flight session look resumable. Recovery is startup-only and idempotent.

## Verification

Test SQL persistence for permission and elicitation metadata, answer/expiry/withdrawal reasons, restart interruption, orphan and closed-session handling, bounded retention, and old-ID invalidation. Inspect stored rows and schema to prove payload text and secrets are absent. Test waiter fail-closed behavior when open/close persistence fails or blocks through cancellation/deadline. Test recovery retry after partial writes and preserve normal interrupted-running-session behavior.
