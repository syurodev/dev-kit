# Research: Open-source sandboxes for Agent Hub

**Date:** 2026-08-12  
**Decision context:** Agent Hub spawns third-party agent CLIs (Claude Code / Codex / …) with execute-level power. Capability Gateway + “no auto-approve” is **not** enough — those gate *DevKit-mediated* actions, not arbitrary bash/fs inside the agent process.  
**Status:** Agent Hub implementation plan **postponed** until a sandbox strategy is chosen.

**Primary sources:**
- [Local AI Agent Sandboxes Compared (Ry Walker, Jun 2026)](https://rywalker.com/research/local-agent-sandboxes)
- [anthropic-experimental/sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime) (`@anthropic-ai/sandbox-runtime`)
- [containers/bubblewrap](https://github.com/containers/bubblewrap)
- [ai-jail sandbox-alternatives notes](https://github.com/akitaonrails/ai-jail/blob/master/docs/sandbox-alternatives.md)
- Agent wrappers: [ai-bwrap](https://github.com/didvc/ai-bwrap), [bubblewrap-ai](https://github.com/umago/bubblewrap-ai), [nono](https://github.com/always-further/nono) (per Ry Walker), [fence](https://github.com/c0dearm/fence), [yolobox](https://github.com/yolobox-dev/yolobox), [agent-jail](https://github.com/Michaelliv/agent-jail), [sandkasten](https://github.com/DatanoiseTV/sandkasten)

---

## Threat model (DevKit Agent Hub)

| Threat | Without sandbox | Soft controls (current plan) help? |
|---|---|---|
| Agent `rm -rf ~` / rewrite SSH keys | Full user access | No (once allowed or agent-native tools) |
| Read vault DB / other projects under `$HOME` | Likely | No |
| Exfil secrets via network | Likely | No |
| Escape via child processes | Same user | Partial only if OS policy inherits |
| Kernel exploit escape | N/A | Shared-kernel sandboxes also fail here |

**Requirement for Hub v1:** every agent subprocess (and children) must inherit an OS-enforced policy: **cwd (+ explicit allowlist) RW**, rest of home **invisible or RO-deny**, optional **egress allowlist**, secrets not in env unless brokered.

---

## Architecture tiers

```text
Stronger isolation ↑
  microVM (Firecracker / Kata / full VM)     — shared nothing; heavy for desktop tabs
  OCI container (Podman/Docker)              — good box; image/toolchain friction
  OS primitives (bwrap / Landlock / Seatbelt)— best fit for local agent CLIs
  App-level prompts only                     — insufficient for Agent Hub
Weaker ↓
```

Market consensus (2026): healthy agent sandboxes converged on **OS primitives**, not containers, for local coding agents. Containers remain valid for “stronger box / parallel workspaces” (yolobox) or cloud (E2B — out of scope for desktop-first DevKit).

---

## Building blocks (embeddable)

| Tech | Platform | What it does | Fit for DevKit |
|---|---|---|---|
| **bubblewrap (`bwrap`)** | Linux | Unprivileged user/mount/pid/net namespaces + bind mounts; Flatpak’s engine; ~tiny C binary | **Primary Linux primitive** to wrap agent spawn |
| **Landlock LSM** | Linux 5.13+ | Kernel path allowlist; inherits to children; complements bwrap | **Second layer** (defense in depth) |
| **seccomp-BPF** | Linux | Syscall filter | Optional harden; easy to break agent toolchains if naive |
| **Seatbelt + `sandbox-exec`** | macOS | Kernel MAC profiles; used by Claude Code / srt / nono | **Primary macOS primitive** (API deprecated but still what everyone uses) |
| **Windows WFP + restricted user** | Windows | srt’s approach (alpha) | Later; DevKit desktop may prioritize macOS+Linux first |
| **gVisor** | Linux | User-space kernel | Too heavy / awkward for interactive coding CLI |
| **Firecracker / Kata** | Linux | microVM | Strongest; poor UX for multi-tab local Hub unless dedicated “remote workspace” mode |
| **Firejail** | Linux | Profiles for desktop apps; historically setuid | **Avoid as Hub dependency** (attack surface / model mismatch) |
| **nsjail** | Linux | Google; protobuf configs; CTF/server oriented | Overkill vs bwrap for CLI wrap |

---

## Ready-made OSS products (agent-oriented)

### A. Reference / first-party adjacent

| Project | License / form | Isolation | Network | Notes for DevKit |
|---|---|---|---|---|
| **[@anthropic-ai/sandbox-runtime](https://github.com/anthropic-experimental/sandbox-runtime)** (`srt`) | OSS (Anthropic experimental); npm TS | Seatbelt (macOS), bwrap (Linux), Windows alpha | Domain allowlist via local proxy | Powers Claude Code `/sandbox`. Closest “industry reference”. TS/npm — awkward as **core Go** dependency; better as **subprocess wrapper** or policy inspiration. Beta; expect API churn. |
| **Codex built-in** | Part of Codex CLI | Landlock+seccomp (Linux), Seatbelt (macOS) | Workspace-scoped | Not a reusable library for wrapping *other* agents. |

### B. Cross-agent wrappers (healthy as of mid-2026 surveys)

| Project | Lang | Isolation | macOS | Differentiator |
|---|---|---|---|---|
| **nono** | Rust | Landlock + Seatbelt | Yes | Credential-injection proxy (API keys stay outside jail) — high value for Hub |
| **fence** | Go | Seatbelt + bwrap | Yes | Default-deny + domain proxy; command deny / SSH filtering |
| **ai-jail** | (multi) | bwrap + Landlock (Linux), sandbox-exec (macOS) | Yes | Explicit multi-OS; documents why not Firejail/nsjail |
| **sandkasten** | Rust | Seatbelt / bwrap+Landlock+seccomp | Yes | TOML profiles; pasta/slirp netns options |
| **agent-jail** | (static binary) | Landlock / Seatbelt / uid fallback | Yes | “Pick strongest backend”; good mental model for DevKit `SandboxBackend` interface |
| **ai-bwrap / bubblewrap-ai** | Shell/JSON | bwrap only | No | Thin Linux wrappers; good **policy examples**, not cross-platform Hub core |
| **yolobox** | Go | Docker/Podman/Apple Containers | Yes | Container path; heavier, stronger “box”, parallel-agent workbench vibe |
| **katosh/agent_sandbox** | — | bwrap (+ firejail/Landlock fallbacks) | Limited | HPC/Slurm-aware; niche |

### C. Stalled / treat carefully

| Project | Issue |
|---|---|
| **landrun** | Surveyed stalled (Landlock ABI lag) — kernel still enforces old rules, but project risk |
| **yolo-cage** | VM/K8s; surveyed stalled |
| Many one-week wrappers | Copy bwrap flags; no maintenance → don’t bet the product |

### D. Cloud (not local Hub default)

E2B, Daytona, Sprites — remote VMs via API. Useful later for “run agent in cloud workspace”; **not** a substitute for local OS jail when DevKit is local-first.

---

## Comparison for DevKit (Wails / Go / multi-vendor ACP)

| Option | Effort | Isolation | Cross-agent | Embed in Go | macOS+Linux | Risk |
|---|---|---|---|---|---|---|
| **1. Thin `Sandbox` interface + shell out to `bwrap` / `sandbox-exec`** | Medium | Good if policy solid | Yes | Excellent | Yes (two backends) | You own policy bugs |
| **2. Wrap agents with `srt` CLI** | Low–medium | Good + net proxy | Yes (any argv) | Subprocess only | Yes (+ Win alpha) | npm/beta; dual runtime |
| **3. Depend on `nono` / `fence` / `ai-jail` binary** | Low | Good | Yes | Subprocess | Yes | External binary lifecycle; product coupling |
| **4. Podman/Docker per session** | High UX cost | Stronger box | Yes | OK | Via VM on macOS | Image/toolchain; slow tab open |
| **5. Rely on Claude `/sandbox` only** | Lowest | Only for Claude | No | N/A | Yes for CC | Fails FR-7 multi-vendor Hub |
| **6. microVM per tab** | Very high | Strongest | Yes | Hard | Painful on macOS | Overkill for Phase 4 |

---

## Recommendation (for when Hub resumes)

### ✅ Decision locked (2026-08-12)

Product owner accepted **Option 1 — thin Go `internal/sandbox` + OS primitives**:

| Layer | Choice |
|---|---|
| Package | `internal/sandbox` (leaf; Agent Hub / ACP adapters call it; no bridge/mcp imports) |
| API | `Runner.Start(ctx, Policy, Cmd) (*Proc, error)` — policy allowlist, scrubbed env |
| Linux | **bubblewrap** required; **Landlock** best-effort second layer when kernel supports |
| macOS | **Seatbelt** profiles via `sandbox-exec` |
| Network v1 | `NetworkNone` or `NetworkHost` (boolean); domain-allowlist HTTP proxy = v1.1 |
| Windows | Out of scope v1 (fail with clear `ErrUnsupportedPlatform`) |
| Not used as core | Firejail, gVisor, Firecracker, embedding `@anthropic-ai/sandbox-runtime` TS |
| Inspiration only | srt / nono / fence / ai-jail policy shapes |

Implementation plan: `docs/superpowers/plans/2026-08-12-agent-sandbox.md`.  
ADR: base `architecture-decisions.mdx` → **ADR-009**.

### Must have before implementing Agent Hub

1. **Sandbox is a hard dependency for non-mock vendors** — mock adapter may run in-process without jail; `claude`/future vendors **must** spawn via sandbox backend (fail closed if backend missing, unless explicit “unsafe unsandboxed” opt-in behind scary UI).
2. **Ship `internal/sandbox` first** (plan above) before ACP Hub tasks.
3. **Do not** make Firejail / gVisor / Firecracker the v1 path.
4. **Gateway permission UI remains** — sandbox ≠ product consent; keep fail-closed ACP permissions for mediated tools; sandbox covers agent-native bash/fs.

### Explicit non-goals for sandbox v1

- Perfect kernel exploit resistance  
- Windows parity (can trail)  
- Replacing Capability Gateway  
- Auto `--dangerously-skip-permissions` without sandbox (wrappers that skip app perms **only** make sense *inside* a jail)

---

## Spike plan (next work — before Agent Hub)

1. **Spike A (1–2 sessions):** Inventory broken workflows under a strict jail: `git`, `gh`, `npm`/`bun`, agent auth files, MCP unix socket to DevKit host.  
2. **Spike B:** Prototype `internal/sandbox` with bwrap on Linux CI/cloud agent + Seatbelt on a macOS host; golden tests for “cannot read `~/.ssh`”, “can write cwd”.  
3. **Spike C:** Spawn `@agentclientprotocol/claude-agent-acp` **inside** jail; confirm ACP stdio still works; document required bind mounts.  
4. **Decision record:** pick Option 1 vs 2/3; only then un-postpone `docs/superpowers/plans/2026-08-12-agent-hub.md` and add a Task 0 “sandbox required for vendor spawn”.

---

## Impact on Agent Hub plan

- Implementation of `2026-08-12-agent-hub.md` is **on hold**.  
- Phase 4 remains the product destination, but **sandbox research/spike is the next engineering priority**.  
- When resumed, Hub plan must gain: sandbox backend wiring in Claude adapter `Open()`, fail-closed without backend, UI “sandbox active” indicator, and tests that prove home secrets are unreachable.

## Ops (host packages)

- Linux: `apt install bubblewrap` (required for DefaultRunner)
- macOS: `sandbox-exec` (system)
- Landlock: best-effort; EPERM/ENOSYS → skip
