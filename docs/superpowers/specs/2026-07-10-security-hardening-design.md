# Security Hardening Design

## Scope

This change makes the existing trusted, local-first application enforce its
security contracts at runtime. It covers the bridge/capability boundary, safe
server-mode defaults, SSH host verification, database transport and resource
limits, encrypted-record integrity, filesystem permissions, structured bridge
errors, and local audit events.

Public or remotely accessible server deployment is explicitly out of scope.
Server mode remains a loopback-only development/headless convenience until a
separate design introduces authentication, authorization, TLS, rate limiting,
and an operational secret-management model.

## Trust Boundaries

The frontend calls typed Wails bridge methods. Each method invokes the
Capability Gateway with a fixed built-in subject, capability, and validated
scope. The gateway checks policy, emits a redacted audit event, and only then
executes the service handler. Core services remain inaccessible from bridge
methods except through this gateway.

The bridge maps internal errors to a small structured public error contract:
`code`, `message`, and `recoverable`. Error causes remain server-side only.
Input validation errors preserve a safe user-facing explanation; vault state,
permission, connection, and unexpected failures receive stable public codes.

## Server Mode

The server build defaults to `127.0.0.1`. Docker no longer exposes a public
port by default. A non-loopback `WAILS_SERVER_HOST` is rejected at startup.
Documentation states that this mode is not a remote API and must not be placed
behind port publishing as a substitute for authentication and TLS.

## SSH

SSH profiles store a pinned host-key fingerprint after a first, explicit TOFU
trust decision. Subsequent connections require an exact key match; mismatch
fails before authentication. The bridge exposes a safe host-key observation
and confirmation flow; it never accepts an arbitrary key silently.

Private-key authentication supports encrypted PEM through a one-time
passphrase parameter. The passphrase is used only to build the signer and is
not persisted, returned, or audited.

## Database Connections

New remote PostgreSQL profiles default to `verify-full`; loopback profiles may
default to `disable` for local development. An explicit insecure mode remains
possible only for loopback addresses and is rejected for remote hosts.

The database service owns bounded `pgxpool` pools keyed by profile ID, uses
context cancellation, caps concurrent connections and query rows, and closes
pools during application shutdown. A cancelled query returns a structured,
safe bridge error.

## Encrypted Records

Version 1 envelopes remain readable. New Version 2 XChaCha envelopes
authenticate canonical associated data that contains the record type, record
ID, and key ID. Services supply the same binding when opening an envelope.
New and updated notes, SSH profiles, and DB profiles use Version 2; a
successfully read Version 1 record is migrated atomically on its next write.
The unsupported AES-256-GCM declaration is removed from validation so the
advertised envelope algorithms match the implementation.

## Audit and Storage

The gateway writes append-only local audit events for capability calls and
their allow/deny result. Vault setup, unlock, recovery unlock, and lock are
also audited. Event metadata passes through `audit.RedactMap`; secrets,
passwords, recovery keys, private keys, SQL text, and command text are never
recorded.

SQLite is created and chmodded to `0600`, while its parent remains `0700`.

## Verification

Tests prove that bridge calls cannot bypass permission checks; unauthorized
calls do not reach handlers; audit events are redacted; raw service errors do
not cross the bridge; server host validation rejects non-loopback; SSH rejects
unknown or changed host keys; encrypted private keys require a supplied
passphrase; remote insecure DB profiles are rejected; cancellation and pool
bounds work; V1 remains decryptable and V2 rejects record swapping; and the
SQLite file permissions are restrictive. The complete Go suite and relevant
server-mode smoke build are run before delivery.
