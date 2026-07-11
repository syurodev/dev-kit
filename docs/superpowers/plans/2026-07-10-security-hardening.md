# Security Hardening Implementation Plan

> For agentic workers: REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax.

Goal: Enforce runtime authorization, safe errors, audit, SSH verification, database transport/resource limits, encrypted-record integrity, file permissions, and loopback-only server mode.

Architecture: Wails bridge methods call a typed gateway helper. The helper supplies built-in subject/capability/scope, checks policy, audits redacted metadata, and converts internal causes to structured public errors. V2 encryption binds record identity through AAD while V1 remains readable.

Tech stack: Go, Wails v3, x/crypto/ssh, XChaCha20-Poly1305, pgxpool, SQLite.

## Global constraints

- Never log or return passwords, recovery keys, private keys, passphrases, SQL, or shell command text.
- Every production change starts with a failing regression test.
- Preserve V1 envelope readability; new records use V2 AAD.
- Server mode is loopback-only and not a remote deployment surface.
- Every bridge call carries a fixed subject, capability, and bounded scope.
- Verification uses GOCACHE=/private/tmp/devkit-go-cache.

## Task 1: Gateway and audit sink

Files: create internal/audit/events.go and events_test.go; modify internal/capabilities/gateway.go, gateway_test.go, and internal/app/container.go.

Step 1: Add a failing test proving denied calls are audited and their handlers do not run. Construct a memory sink, call Gateway.Call with a policy that has no matching grant, assert an error, one event with Outcome denied, and zero handler calls.

Step 2: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/capabilities -run TestGatewayAuditsDeniedCallWithoutRunningHandler -count=1. Expected failure because the sink and constructor argument do not exist.

Step 3: Add audit.Event, Sink, MemorySink, and Gateway.sink. Record allowed or denied outcomes before invoking the handler. Copy event slices on read and run audit.RedactMap on metadata.

Step 4: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/audit ./internal/capabilities -count=1. Expected PASS.

Step 5: Commit with git add internal/audit internal/capabilities internal/app/container.go and git commit -m "feat: audit capability decisions".

## Task 2: Bridge choke point and structured errors

Files: create internal/bridge/gateway.go and errors.go; modify internal/bridge/{notes,database,ssh,vault,app_service}.go, bridge tests, internal/apperrors/errors.go, and container grants.

Step 1: Add failing tests proving NotesList is denied when its grant is absent and that an internal URL/driver error is not present in the returned AppError.

Step 2: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/bridge -run 'TestNotesBridgeUsesGateway|TestPublicErrorRedacts' -count=1. Expected failure because bridge methods currently call services directly.

Step 3: Implement AppService.call(ctx, subject, capability, scope, metadata, handler). Route all notes, DB, SSH, vault, and command methods through it. Use fixed subjects builtin.notes, builtin.database, builtin.ssh, builtin.vault, and builtin.tools; use scopes note:<id>, profile:<id>, vault, and command:<id>. Map causes to validation_failed, vault_locked, permission_denied, connection_failed, operation_cancelled, and internal_error. Audit action plus opaque IDs only.

Step 4: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/bridge ./internal/apperrors -count=1. Expected PASS.

Step 5: Commit bridge and error changes.

## Task 3: Vault audit and SQLite mode

Files: modify internal/bridge/vault.go and tests, internal/storage/sqlite.go and tests, and main.go.

Step 1: Add a failing test that opens a temp DB and asserts os.Stat(path).Mode().Perm() equals 0600.

Step 2: Run the focused storage test and confirm failure.

Step 3: chmod the SQLite path to 0600 after open/create, closing on chmod failure. Audit setup, unlock, recovery unlock, and lock without credential metadata.

Step 4: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/storage ./internal/bridge -count=1.

Step 5: Commit storage and vault changes.

## Task 4: V2 AAD envelope

Files: modify internal/crypto/{envelope,aead}.go and tests, internal/vault/service.go, and notes/database/SSH service seal/open call sites.

Step 1: Add failing tests for OpenWithAD rejecting a changed record ID and reading an existing V1 envelope.

Step 2: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/crypto -run TestOpenWithAD -count=1 and confirm failure.

Step 3: Add AssociatedData{RecordType,RecordID,KeyID}, SealV2, and OpenWithAD. Canonicalize as record_type + NUL + record_id + NUL + key_id. V1 opens with nil AAD. Remove AES-256-GCM from accepted algorithms. Allocate IDs before sealing and bind each service record type/id/key id.

Step 4: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/crypto ./internal/services/notes ./internal/services/database ./internal/services/ssh -count=1.

Step 5: Commit crypto and service changes.

## Task 5: SSH host-key and passphrase support

Files: create internal/services/ssh/hostkeys.go and tests; modify SSH repository/record/service/tests, bridge SSH, frontend bridge and SSH view.

Step 1: Add failing tests proving unknown/changed host keys are rejected and encrypted PEM requires the correct passphrase.

Step 2: Run the focused SSH tests and confirm failure against InsecureIgnoreHostKey and ParsePrivateKey-only behavior.

Step 3: Store HostKeyFingerprint, expose observe/trust operations, use gossh.FingerprintSHA256 and an exact pinned callback, and reject empty/mismatched pins before authentication. Try ParsePrivateKey then ParsePrivateKeyWithPassphrase; zero the temporary passphrase bytes. Send passphrase only for one operation and never persist it.

Step 4: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/ssh ./internal/bridge -count=1 && npm --prefix frontend run build.

Step 5: Commit SSH and frontend changes.

## Task 6: Database TLS, pooling, and cancellation

Files: create internal/services/database/pool.go and tests; modify database service/repository/tests, bridge database, frontend DB bridge, main.go, go.mod, and go.sum.

Step 1: Add failing tests that remote profiles default to verify-full and reject explicit disable, while localhost remains allowed for local development.

Step 2: Run the focused database tests and confirm failure.

Step 3: Add loopback detection using localhost and parsed loopback IPs. Reject remote disable. Create one pgxpool.Pool per profile with MaxConns 4, MinConns 0, and MaxConnLifetime 30 minutes. Reuse pools, apply a 30-second context deadline, preserve cancellation, close rows, and close pools during shutdown.

Step 4: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/services/database ./internal/bridge -count=1.

Step 5: Commit database and dependency changes.

## Task 7: Loopback-only server mode

Files: create internal/servermode/servermode.go and tests; modify main.go, build/docker/Dockerfile.server, README.md, and core-foundation documentation.

Step 1: Add failing tests for ValidateHost blank -> 127.0.0.1, loopback acceptance, and 0.0.0.0/public address rejection.

Step 2: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/servermode -count=1 and confirm failure.

Step 3: Validate WAILS_SERVER_HOST before Wails startup, default to 127.0.0.1, set Docker ENV to 127.0.0.1, remove public EXPOSE, and document that non-loopback operation is unsupported until a separate authenticated/TLS server design.

Step 4: Run GOCACHE=/private/tmp/devkit-go-cache go test ./internal/servermode ./... && GOCACHE=/private/tmp/devkit-go-cache go build -tags server ./....

Step 5: Commit server-mode and documentation changes.

## Task 8: Integration verification

Files: modify internal/bridge/app_service_test.go and architecture documents.

Step 1: Add an end-to-end test with memory audit sink proving one allowed and one denied sensitive bridge call, and assert serialized events contain neither secret input nor SQL/command text.

Step 2: Run the focused test and confirm its initial failure before wiring.

Step 3: Keep this test at the bridge/gateway boundary; do not duplicate service implementation tests.

Step 4: Run GOCACHE=/private/tmp/devkit-go-cache go test ./... && npm --prefix frontend run build && GOCACHE=/private/tmp/devkit-go-cache go build -tags server ./....

Step 5: Update architecture guarantees for choke point, audit redaction, V1/V2 compatibility, loopback server, DB TLS, and SSH pins, then commit.
