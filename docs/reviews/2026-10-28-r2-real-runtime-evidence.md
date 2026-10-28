# R2: real-runtime protocol evidence for the harness controls

Date: 2026-10-28. Delivery R2 of `docs/superpowers/plans/2026-10-27-harness-remaining-acceptance-team-implementation.md`.
Base: `origin/main` `eca9c10f` (#379). Production is inert and this change touches no production code: only `_test.go` files under `internal/app`.

**Status: R2 incomplete.** Codex 0.147.0 and opencode v2.2.0 pass every row below with real processes. **Claude is not attempted** (blocker below), so the
mandatory real-runtime gate is not satisfied for all three vendors. This report does not edit the shared AP1/AP3 ledgers.

## What is real and what is scripted

| Real | Scripted or stubbed |
| --- | --- |
| The vendor process (Codex native binary, `opencode acp`) started through the existing adapter and the existing sandbox launcher | The model: a local fixture server on a unix socket that plays a fixed sequence of tool calls. No model, no provider, no account. |
| `bwrap --unshare-net`: the vendor sees only an empty `lo` | The person: a prompter that answers the hub's scope question, and permission responders that allow or deny |
| The real `devkit-mcp` stdio proxy, `SessionHost`, MCP server, gateway, grants, `ComposeHarness`, Agent Hub service, SQLite | MCP dependencies the harness does not use (the contract stubs), plus a counting probe for `workspace.memory.create` and the real `GetStatus` |
| Each vendor's own discovery, `tools/list_changed` handling and permission flow | **The restart in row 5a**: the host and Agent Hub are rebuilt over the same database inside one OS process, and the vendor processes are killed first (as an app exit would). The app process itself is not restarted. |

Not covered: native dialogs (R3), production wiring (R4), any real model or account. A stub executable or in-process adapter was not used for any row.

## Isolation and the no-network proof

- `env -i HOME=<tmp> PATH=/usr/bin:/bin` plus `DEVKIT_R2_*` paths. The tests skip with the real HOME, with any secret- or provider-looking variable, or without the sandbox.
- Vendor environment names (from the launch rows): Codex `CODEX_HOME HOME PATH USER`; opencode `HOME OPENCODE_CONFIG_CONTENT OPENCODE_DISABLE_CLAUDE_CODE PATH USER XDG_*`. Private home under `/tmp`, `network: none`.
- Before any prompt each run connects from inside the jail. Both vendors, identical: interfaces `["lo"]`; `1.1.1.1:443`, `8.8.8.8:53`, `192.168.0.1:80`, `[2606:4700:4700::1111]:443` all `network is unreachable`; `127.0.0.1:9` `connection refused`; DNS for a provider name `failed`.
- The only reachable thing is the host's model-fixture unix socket, relayed to a loopback port inside the jail.
- No credential, config or auth file was read. `~/.opencode/bin` was used only as the binary's location. To find the Claude ACP package a `find` over `$HOME` listed directory names (including under `~/.config`); no file content there was opened or copied.

## Versions and candidate

Codex `codex-cli 0.147.0` (native musl binary under the global npm package). opencode `v2.2.0` (`~/.opencode/bin/opencode`; `opencode2` is a 47-byte wrapper). Claude Code 2.1.312 is installed but not used. bubblewrap 0.11.1, Linux 7.0.0-38, Go 1.27.1.
Candidate diff against `eca9c10f`: `internal/app/harness_runtime_fixture_test.go`, `harness_runtime_e2e_test.go`, `harness_runtime_e2e_scenarios_test.go` (new) and `harness_composition_rig_test.go` (opt-in `liveStatus`, `hubStatus`, `memoryProbe`, a counter field). Not committed yet.

## Reproduce

```
go build -o $T/devkit-mcp ./cmd/devkit-mcp && go test -c -o $T/app.test ./internal/app && cd internal/app
env -i HOME=$T/home PATH=/usr/bin:/bin DEVKIT_R2_PROXY_BIN=$T/devkit-mcp DEVKIT_R2_OUT=$T/out \
  DEVKIT_R2_CODEX_BIN=<native codex>  $T/app.test -test.run TestR2CodexRealRuntime -test.v
env -i ... DEVKIT_R2_OPENCODE_BIN=~/.opencode/bin/opencode $T/app.test -test.run TestR2OpenCodeRealRuntime -test.v
```

Without those variables every `TestR2*` skips, so CI on a machine without the vendors stays green (`go test ./internal/app` passed in 49.5 s). A run whose vendor offers no DevKit tool on a default launch **fails** rather than passing.
Evidence rows are written as `<vendor>.jsonl` (counters, tool names, sentinel lines such as result length; no prompts, transcripts or result bodies).

## Rows

Status key: P = passed with a real process; Obs = passed, with an observation for the owner.

| # | Row | Codex 0.147.0 | opencode v2.2.0 |
| --- | --- | --- | --- |
| 1a | Default launch (no profile) | P: 156 DevKit tools, `vault.status` ran | P: same counts |
| 1b | Narrowed profile, fresh session | P: 145 offered, vault absent, handler 0 | P: same |
| 2 / 4b | Revoke, then the vendor calls a tool it still holds | P: server saw 1 call, refused it, handler ran 0 times | P: same; refusal `harness.groupOff` |
| 3 | `harness.profile.propose` over the wire | P: gate confirmation `harness.profile.propose` plus the scope prompt, handler 1 | P: same |
| 3b | Newly enabled write tool, no accidental pre-approval | P: the vendor's own ask (`devkit.workspace.memory.create`) fired and was denied; server saw 0 calls | **Obs**: no vendor ask exists; the call reached the server and the handler ran once, with no DevKit confirmation (the tool is not a gated operation) |
| 4a | Group served before revoke | P | P |
| 4c | Call-start snapshot: revoke committed while a call is held in its handler | P: the held call completed | P: needed an explicit Reload first (runtime had not refreshed after the re-enable); held call completed |
| 4d | Result budget from the server | P: budget 2048 B, result 1412 B against 2392 B without it | P: same numbers |
| 5a | Restart keeps the durable S | P: override `[vault, workspace]` stored and served (restart as described above) | P: same |
| 5b / 5c | Committed change whose publication fails; the paused endpoint | P: the propose result reports the saved change; the next turn is offered no group tool (`NOT-OFFERED`) | P: the propose call returns the "saved, but tool access is paused" error; the next turn is offered no group tool |
| 5d / 5e / 5f | Reconcile, then discovery, then explicit Reload | P: reconcile `applied`, `live`, `installed`; **not offered right after**; offered after Reload | P: same |
| 6 | `tools/list_changed` versus actual discovery refresh, measured separately | `list_changed` delivered (1); client `tools/list` after it: 0; vault not offered until Reload (6a vs 6b) | `list_changed` delivered (1); client `tools/list`: 1; vault offered **without** Reload in 6a, but **not yet** in 4c and 5e |

## Findings and limits

1. **Discovery.** Codex does not refresh its tool list on `list_changed`. opencode refreshed once (6a) and was late twice (4c, 5e); one observation each, so timing is unmeasured. Neither supports a claim of automatic discovery. **Keep `DiscoveryReloadRequired` and the explicit Reload** for both. After a successful reconcile the next turn still lacked the group on both vendors until Reload (5e versus 5f).
2. **opencode surface.** opencode 2.x does not put MCP tools in the model's function list. It offers 12 tools in total (11 built-ins and `execute`, which runs JavaScript), with DevKit's tools as `tools.devkit.<name>`. The fixture drives that. The adapter's guidance already uses this form, so no adapter mismatch was found. Production code was not changed.
3. **opencode approvals (owner decision).** On opencode there is no per-call vendor confirmation, so a tool enabled by a confirmed `harness.profile.propose` runs without another prompt unless DevKit gates that operation. Codex asks per call. This is how the adapter is documented to work, but row 3b shows the consequence: please confirm it is acceptable for AP4.
4. **Restart fidelity.** Row 5a models an app exit by killing the vendor processes. Without that, the old Codex process still held the thread and `thread/resume` failed with "already has an active writer". That is a property of an in-process restart, not a product bug. A real restart was not run.
5. **Run-to-run counts.** `list_changed` counters in row 6a varied between runs (1 in the latest); read them as "at least once".

## Blocked or not attempted

- **Claude (all rows): not attempted.** The fixture needs `claude-agent-acp`, which exists only in DevKit's own runtime directory `~/.config/DevKit/runtimes/claude-acp/<version>/`. The R2 brief forbids reading `~/.config/**`. That is a package, not a credential, but the rule's wording covers it, so it was not used. Needs the owner's say-so to bind that directory read-only, to copy it to a temporary directory, or to install the package fresh (a download). `DEVKIT_R2_CLAUDE_DIR`, `DEVKIT_R2_NODE` and the Anthropic Messages fixture are already written and have not been exercised.
- **Not covered by design:** account or paid-model behaviour, real model tool-choice, native approval dialogs (R3), activation (R4).

R2 stays incomplete until the Claude rows are run. Nothing here changes the AP1 or AP3/AP4 ledger state.
