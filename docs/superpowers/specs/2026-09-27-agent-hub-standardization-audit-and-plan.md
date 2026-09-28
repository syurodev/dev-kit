# Agent Hub runtime standardization — implementation and remaining boundaries

Date: 2026-09-27. This document replaces the initial audit's incorrect completion claims.
Scope of this change: artifact lifecycle protection, managed Node, reproducible tested
installs, rollback UX, and verification. Claude authentication/home migration remains
separate; this change does not move or copy credentials or session history.

## Corrected baseline

Before this change, DevKit already installed both vendor packages into its own runtime
folder. Claude defaulted to the SDK-bundled binary, with an explicit machine-binary option.
Codex used its native app-server binary and a separate CODEX_HOME. Both installers still
required host Node/npm; Claude ALSO required that host Node at run time. An absolute path
is not a managed dependency. The original audit's assertion that neither vendor depended
on host Node at runtime was incorrect.

A package-lock created during each fresh npm install records that install's resolution;
it does not freeze resolution across machines or dates. The original reproducibility
claim was also incorrect. Existing code and prior design non-goals did not satisfy the
new requirements merely because those choices had been intentional.

## Design and implementation

### 1. Artifact leases

`Manager.ResolveLeased` reads current.json and acquires a hold on that version under the
same mutex used by removal and replacement. Resolution failure releases the hold. The
returned `Resolved.Release` is idempotent. `Resolve` is retained for read-only inspection;
production adapters use the leased resolver.

The lease starts BEFORE process spawn. Codex transfers it to its process reaper for chat,
account probes and browser/device login. Claude retains it through the ACP process exit;
auth discovery holds it through the probe, and terminal login transfers it to the terminal
waiter. Errors before spawn release immediately. Process errors and explicit close release
only after reaping, rather than releasing after a timeout while the process might live.
`Proc.Wait` is now safe for concurrent waiters (cleanup, error reporting and lease release).

Both Remove and same-version reinstall refuse a leased version. The reinstall check is
at activation under the mutex, since a new session can start while an install is downloading.
Startup GC also skips held versions and refuses to run during installation.

This protection is required on every OS: lazy module/data loads need the installed files
even when an OS retains an unlinked executable. No claim about universal macOS SIGKILL
behavior is made. The subprocess test verifies refusal and process survival; it is not
an unlink experiment.

### 2. Managed Node/npm

DevKit downloads official Node 24.14.1 distributions on demand under
`<data>/runtimes/node/24.14.1-<platform>/`. SHA-256 checksums are pinned in source from
https://nodejs.org/dist/v24.14.1/SHASUMS256.txt. Download size and expanded size are bounded;
HTTP cancellation propagates; extraction confines ordinary files to the archive root and
skips symlinks/special files. The staged node must execute and report the pinned version
before atomic activation. Failed/cancelled staging is removed.

npm is invoked as `managed-node /absolute/path/to/npm-cli.js`, including on Windows; no
shell shim or host Node/npm lookup is involved. Both vendors share the immutable managed
Node installation. Sessions and Status never initiate downloads. Node is not garbage
collected by vendor runtime GC, so existing installs can continue referencing its version.

Artifacts are mapped for macOS/Linux/Windows x64 and arm64. This does not extend the app's
sandbox support: Windows remains subject to its existing unsupported sandbox behavior.
Linux distributions must meet the official Node binary's runtime requirements; musl is
not newly supported. Only macOS arm64 has been exercised live in this change.

Existing agent installations keep the Node path recorded in their manifest. A missing
legacy Node produces a reinstall instruction instead of silently discovering another host
installation. Reinstall moves future launches to managed Node; running leases block that
replacement until the processes exit. No user CLI, config, history or credential is removed.

### 3. Frozen tested dependencies

The repository embeds package.json and package-lock.json for the tested releases:

| Vendor | Package | Version | Runtime dependency |
| --- | --- | --- | --- |
| Claude | @agentclientprotocol/claude-agent-acp | 0.78.0 | SDK 0.3.270, bundled Claude 2.1.270, managed Node |
| Codex | @openai/codex | 0.147.0 | native platform binary; managed Node only during install |

A tested install copies these files into staging BEFORE running `npm ci --ignore-scripts`
with optional platform dependencies included. The registry is explicitly npmjs.org;
lockfile integrity covers the dependency artifacts. Upgrading a tested version requires
regenerating/reviewing its lock and running live handshake checks.

The existing user-selected UNTESTED version flow remains available. It uses npm install,
and its dependency graph is not claimed to be reproducible across fresh installs. The UI
now states this distinction. See https://docs.npmjs.com/cli/v11/commands/npm-ci/ for npm's
frozen-install contract.

### 4. Readiness and rollback UX

Status no longer probes global Node/npm or blocks installed Codex on their absence.
Claude reports a missing recorded Node as a runtime repair requirement. Settings explains
managed dependencies and migration by reinstall.

Settings asks for confirmation before switching to an older previous/tested version.
The warning accurately says older runtimes may not read newer session data. IsDowngrade
remains an informational backend API; neither it nor confirmation proves storage-format
compatibility. There is no automatic session-format migration or rollback guarantee.

## Verification

Added regression coverage for:
- leases obtained before spawn, activation then removal, same-version reinstall;
- resolution failures releasing their holds;
- Codex chat, failed initialization, unusable key, account probe and pending sign-in;
- Claude failed handshake;
- concurrent process waiters observing the same exit;
- lockfile present before npm ci, pinned SDK and dependency integrity;
- download checksum mismatch/cancellation and archive traversal/symlink handling.

Live opt-in test `TestManagedRuntimesWithoutGlobalNode` sets PATH to an empty directory,
installs both vendors into a temporary root, and executes their real binaries. It passed
on macOS arm64: Codex 0.147.0, Claude Code 2.1.270. No login or model request is performed.
The separate `TestAgentRuntimeLiveInstall` verifies Claude's actual ACP initialize inside
the real sandbox, in staging and after activation. Final test results are recorded in the
implementation handoff; cross-platform and authenticated model turns are not implied.

## Remaining scope

- Claude config/auth/history still use their existing paths and supported flows. A separate
  migration must preserve old session access and must not copy credentials automatically.
- API-key selection through Accounts for Claude is not added here.
- Host instructions/DevKit onboarding are not added here.
- Downgrade compatibility and keeping old runtime versions for dormant sessions remain
  separate work; current/previous retention does not guarantee every historical resume.
- Untested versions are explicitly outside the frozen tested-release contract.

No commit, push, PR or release is part of this change.
