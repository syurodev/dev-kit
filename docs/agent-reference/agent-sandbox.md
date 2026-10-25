### Agent sandbox (`internal/sandbox`)
**Implemented** leaf package for OS-isolated subprocesses. Linux: **bubblewrap**
(`apt install bubblewrap`). macOS: Seatbelt / `sandbox-exec`. Landlock helper
exists but is **not** wired into bwrap Start/StartPiped.

```bash
go test ./internal/sandbox/ ./internal/arch/ -count=1
# meaningful smoke when bwrap is installed:
go test ./internal/sandbox/ -run 'TestSmokeAgentShapedProcess|TestBwrapCanWriteWorkDirUnderHome|TestStartPiped' -v
```

- `Start` — capture-and-wait helpers (buffered stdout/stderr).
- `StartPiped` — ACP / interactive stdio (Claude adapter path).
- Fail closed if `bwrap` missing / unsupported GOOS (`ErrUnavailable` / `ErrUnsupportedPlatform`).
- Private home default: tmpfs on home + forced `HOME`/`TMPDIR`; WorkDir under `$HOME` is re-bound after tmpfs.
- Default denies host `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.config/gcloud`; symlink binds into deny → `ErrInvalidPolicy`.
- **Agent Hub asks for more by default**: the real `$HOME` (`ShareHome`) and the Docker
  socket found via DOCKER_HOST / current docker context / OrbStack / Docker Desktop
  (`AllowDocker`, `agenthub.DetectDocker`). DevKit's data dir (except `runtimes/`) is always
  hidden (`agenthub.ProtectDataDir`). Settings → Agents → Sandbox turns either off.
- **Environment inspection**: Settings → Agents and the session menu expose a
  UI-only report (`agenthub/environment.go`, `AgentHubEnvironment`). The live
  report retains launch env/grants. Codex reports skills/MCP through bounded
  metadata calls; Claude discovery is configuration-only. Never label file
  discovery as runtime verification or include config values, env secrets or
  skill bodies in the report/audit. Shared-home PATH completion runs after
  vendor/session overrides and adds existing common toolchain bins.
- **Vendor customization is available again**: Claude uses `user`/`project`/`local`
  setting sources. Shared-home Codex sessions import portable user MCP/settings and
  global instructions, link user skills/plugins read-only, and keep DevKit's own
  login/state. `skills/` is persistent and writable for system/managed installs;
  native features are not forcibly disabled. Shared-home sessions inherit the
  host process's CLI environment; Codex provider credentials remain selected by
  DevKit. Codex defaults to Auto, and both adapters allow loopback dev servers.
- Product docs: `syurodev/base` → `/dev-kit/sandbox` (ADR-009).

### Agent sandbox
Host networking no longer reads all of `/var` and `/private/var`: DNS needs only
metadata on the `/var` parents, `resolv.conf` and the timezone database (see
`TestSmokeSeatbeltNetworkHostNarrowVar`). The shared `/tmp` and `/private/tmp`
are still read-write (OPT-3375a2, in progress): Claude Code ignores `TMPDIR` and
uses `/tmp` unless `CLAUDE_CODE_TMPDIR` is set.
Agent Hub gives macOS sessions a private, short `/private/tmp/dk-<session-prefix>`
(0700), with `TMPDIR` and `GOTMPDIR` set after environment inheritance. Unix
listeners are limited to that directory; PTYs and process signals/info are
limited to the session. Settings → Agents → Safety → **Allow developer
toolchains** (default on) adds read-only Command Line Tools and Xcode paths.
Existing credential and data-directory denies still apply. Changes take effect
on the next spawn/resume. Temp directories are removed on close and swept at
startup when the owner database has no open session, the owner PID is gone and
its marker lock is free. Cleanup skips server mode, foreign databases/users and
unmarked directories. The host checks the Unix-rule profile once; if rejected,
only that optional rule is dropped and Settings → Agents → Safety shows the
fallback. Own-directory local IPC is independent of NetworkMode.
Nested Seatbelt cannot run inside an agent session: run
`go test ./internal/sandbox -run Smoke -v` from a host terminal to verify native
cgo, Unix sockets (including the SBPL Unix address filter), PTYs, child signals
and user lookup. A passing profile-string test alone does not verify macOS enforcement.
Known macOS limitation: other processes' metadata and executable paths are
readable from the jail (`proc_pidinfo` returned 136 bytes, `proc_pidpath` 93
bytes, and `KERN_PROC_PID` returned data for a host PID). Removing
`process-info*` did not change these results; removing `sysctl-read` prevented
the Go probe from starting (`failed to get system page size`), so its role in
the libproc results remains unproven. Do not describe the profile as hiding
other processes' metadata or paths. The separate signal probe confirms a host
PID cannot be signalled. Reading another process's memory was not measured.
