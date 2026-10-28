# R2: real-runtime protocol evidence for the harness controls

Date: 2026-10-28. Delivery R2 of `docs/superpowers/plans/2026-10-27-harness-remaining-acceptance-team-implementation.md`.
Base: `origin/main` `eca9c10f` (#379). Production is inert and this change touches no production code: only `_test.go` files under `internal/app`.

**Status: all three vendors pass every row with a real process; not independently reviewed.** The same session wrote the fixtures and read the results, and a reviewer
cannot approve their own work, so this is evidence for review, not an acceptance. Row 5a is an in-process restart and the "Findings and limits" section lists what was not covered.
This report does not edit the shared AP1/AP3 ledgers.

## What is real and what is scripted

| Real | Scripted or stubbed |
| --- | --- |
| The vendor process (Codex native binary, `opencode acp`, Claude Code through `claude-agent-acp`) started through the existing adapter and the existing sandbox launcher | The model: a local fixture server on a unix socket that plays a fixed sequence of tool calls. No model, no provider, no account. |
| `bwrap --unshare-net`: the vendor sees only an empty `lo` | The person: a prompter that answers the hub's scope question, and permission responders that allow or deny |
| The real `devkit-mcp` stdio proxy, `SessionHost`, MCP server, gateway, grants, `ComposeHarness`, Agent Hub service, SQLite | MCP dependencies the harness does not use (the contract stubs), plus a counting probe for `workspace.memory.create` and the real `GetStatus` |
| Each vendor's own discovery, `tools/list_changed` handling and permission flow | **The restart in row 5a**: the host and Agent Hub are rebuilt over the same database inside one OS process, and the vendor processes are killed first (as an app exit would). The app process itself is not restarted. |

Not covered: native dialogs (R3), production wiring (R4), any real model or account. A stub executable or in-process adapter was not used for any row.

## Isolation and the no-network proof

- `env -i HOME=<tmp> PATH=/usr/bin:/bin` plus `DEVKIT_R2_*` paths. The tests skip with the real HOME, with any secret- or provider-looking variable, or without the sandbox.
- Vendor environment names (from the launch rows): Codex `CODEX_HOME HOME PATH USER`; opencode `HOME OPENCODE_CONFIG_CONTENT OPENCODE_DISABLE_CLAUDE_CODE PATH USER XDG_*`; Claude `ANTHROPIC_API_KEY` (a dummy value) `ANTHROPIC_BASE_URL` (the loopback relay) `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC DISABLE_AUTOUPDATER DISABLE_TELEMETRY HOME PATH USER`. Private home under `/tmp`, `network: none`.
- Before any prompt each run connects from inside the jail. All three vendors, identical: interfaces `["lo"]`; `1.1.1.1:443`, `8.8.8.8:53`, `192.168.0.1:80`, `[2606:4700:4700::1111]:443` all `network is unreachable`; `127.0.0.1:9` `connection refused`; DNS for a provider name `failed`.
- The only reachable thing is the host's model-fixture unix socket, relayed to a loopback port inside the jail.
- No credential, config or auth file was read. `~/.opencode/bin` was used only as the binary's location. A `find` over `$HOME` to locate the Claude ACP package listed directory names (including under `~/.config`) without opening any file.
- **Claude package copy (owner's decision):** only `~/.config/DevKit/runtimes/claude-acp/0.95.0` (the installed package and its `node_modules`, 350 MB) was copied to a temporary directory and mounted read-only; no other `~/.config` content was touched, and a name scan of the copy (two levels deep, for credential, auth and `.env` names) found nothing. The temporary HOME gets an empty `.claude` directory, because the adapter grants Claude Code's state directory only when it exists and a session cannot be resumed without it.

## Versions and candidate

| Vendor | Version |
| --- | --- |
| Codex | `codex-cli 0.147.0`, native musl binary under the global npm package |
| opencode | `v2.2.0` (`~/.opencode/bin/opencode`; `opencode2` is a 47-byte wrapper) |
| Claude | `@agentclientprotocol/claude-agent-acp` 0.95.0 on `@anthropic-ai/claude-agent-sdk` 0.3.309, run with Node v24.16.0 |

bubblewrap 0.11.1, Linux 7.0.0-38, Go 1.27.1.
Candidate diff against `eca9c10f`: `internal/app/harness_runtime_fixture_test.go`, `harness_runtime_e2e_test.go`, `harness_runtime_e2e_scenarios_test.go` (new) and `harness_composition_rig_test.go` (opt-in options, `hubStatus`, `memoryProbe`, a counter field).

## Reproduce

```
go build -o $T/devkit-mcp ./cmd/devkit-mcp && go test -c -o $T/app.test ./internal/app && cd internal/app
env -i HOME=$T/home PATH=/usr/bin:/bin DEVKIT_R2_PROXY_BIN=$T/devkit-mcp DEVKIT_R2_OUT=$T/out \
  DEVKIT_R2_CODEX_BIN=<native codex>  $T/app.test -test.run TestR2CodexRealRuntime -test.v
env -i ... DEVKIT_R2_OPENCODE_BIN=~/.opencode/bin/opencode $T/app.test -test.run TestR2OpenCodeRealRuntime -test.v
env -i ... DEVKIT_R2_CLAUDE_DIR=<copy of the claude-acp package> DEVKIT_R2_NODE=<node> $T/app.test -test.run TestR2ClaudeRealRuntime -test.v
```

Without those variables every `TestR2*` skips, so CI on a machine without the vendors stays green (`go test ./internal/app` passed in 49.5 s). A run whose vendor offers no DevKit tool on a default launch **fails** rather than passing.
Evidence rows are written as `<vendor>.jsonl` (counters, tool names, sentinel lines such as result length; no prompts, transcripts or result bodies). Final runs: Claude 31 s, Codex 21 s, opencode 41 s.

## Rows

Status key: P = passed with a real process; Obs = passed, with an observation for the owner.

| # | Row | Codex | opencode | Claude |
| --- | --- | --- | --- | --- |
| 1a | Default launch (no profile) | P: 156 DevKit tools offered, `vault.status` ran | P: same counts | P: tool loaded through `ToolSearch`, `vault.status` ran |
| 1b | Narrowed profile, fresh session | P: 145 offered, vault absent, handler 0 | P: same | P: `ToolSearch` answers "No matching deferred tools found", handler 0 |
| 2 / 4b | Revoke, then the vendor calls a tool it still holds | P: server saw 1 call, refused it, handler ran 0 times | P: same; refusal `harness.groupOff` | P: same; refusal `harness.groupOff` |
| 3 | `harness.profile.propose` over the wire | P: gate confirmation plus the scope prompt, handler 1 | P: same | P: same |
| 3b | Newly enabled write tool, no accidental pre-approval | P: the vendor's own ask fired and was denied; server saw 0 calls | **Obs**: no vendor ask exists; the call reached the server and the handler ran once, with no DevKit confirmation (the tool is not a gated operation) | P: the vendor's own ask (`mcp__devkit__workspace_memory_create`) fired and was denied; server saw 0 calls |
| 4a | Group served before revoke | P | P | P |
| 4c | Call-start snapshot: revoke committed while a call is held in its handler | P: the held call completed | P: needed an explicit Reload first; held call completed | P: needed an explicit Reload first; held call completed |
| 4d | Result budget from the server | P: budget 2048 B, result 1412 B against 2392 B without it | P: same numbers | P: same numbers; the result says it was cut to the 2 KB budget |
| 5a | Restart keeps the durable S | P: override `[vault, workspace]` stored and served (restart as described above) | P: same | P: same |
| 5b / 5c | Committed change whose publication fails; the paused endpoint | P: the propose result reports the saved change; the next turn is offered no group tool | P: the propose call returns the "saved, but tool access is paused" error; the next turn is offered no group tool | P: the propose call completed through the gate; the next turn finds no group tool |
| 5d / 5e / 5f | Reconcile, then discovery, then explicit Reload | P: reconcile `applied`, `live`, `installed`; **not offered right after**; offered after Reload | P: same | P: reconcile `applied`; the group **was** found right after (without Reload); also served after Reload |
| 6 | `tools/list_changed` versus actual discovery refresh, measured separately | `list_changed` delivered (1); client `tools/list` after it: 0; vault not offered until Reload (6a vs 6b) | `list_changed` delivered (1); client `tools/list`: 1; vault offered **without** Reload in 6a, but **not yet** in 4c and 5e | `list_changed` delivered (1); client `tools/list`: 1; vault found **without** Reload in 6a and 5e, but **not yet** in 4c (see finding 1) |

## Findings and limits

1. **Discovery.** Codex does not refresh its tool list on `list_changed`. opencode and Claude refreshed in some rows and were late in others (opencode 4c, 5e; Claude 4c, where the client answered "No such tool available ... does not offer this tool here" until the Reload). These are single observations, so timing is unmeasured. No vendor supports a claim of automatic discovery. **Keep `DiscoveryReloadRequired` and the explicit Reload** for all three.
2. **opencode surface.** opencode 2.x does not put MCP tools in the model's function list. It offers 12 tools in total (11 built-ins and `execute`, which runs JavaScript), with DevKit's tools as `tools.devkit.<name>`. The fixture drives that. The adapter's guidance already uses this form, so no adapter mismatch was found.
3. **Claude surface.** Claude Code keeps MCP tools deferred: the model sees `ToolSearch` and a placeholder, and a tool joins its list only after a `ToolSearch select:mcp__devkit__<name>`. The fixture issues that search first and records "not offered" only if the search finds nothing. Claude cached the loaded tool across turns, which is what rows 2 and 4b exercise.
4. **Approvals (owner decision).** Codex and Claude ask the person per call through the vendor's own flow, and DevKit's gate adds its own confirmations. opencode has no per-call vendor confirmation, so a tool enabled by a confirmed `harness.profile.propose` runs without another prompt unless DevKit gates that operation (row 3b). This is how the adapter is documented to work; please confirm it is acceptable for AP4.
5. **Restart fidelity.** Row 5a models an app exit by killing the vendor processes. Without that, the old Codex process still held the thread and `thread/resume` failed with "already has an active writer". That is a property of an in-process restart, not a product bug. A real restart was not run.
6. **Run-to-run counts.** `list_changed` counters in row 6a varied between runs (1 in the final runs); read them as "at least once".
7. **Rig notes.** The Claude rows needed an empty `.claude` directory in the temporary HOME (finding in "Isolation"). The shared rig gained opt-in options only; no existing test changed behavior (`go test ./internal/app` passed).
8. **Not covered by design:** account or paid-model behaviour, real model tool-choice, native approval dialogs (R3), activation (R4). Each runtime was exercised through one scripted model, so the evidence shows how each vendor's client handles DevKit's server, not how a real model would choose tools.

Nothing here changes the AP1 or AP3/AP4 ledger state. The R2 gate for review is: the three evidence files above, the candidate diff, and an independent read of the fixtures.
