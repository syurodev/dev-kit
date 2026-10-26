# Team orchestration baseline CLI

`cmd/devkit-team-baseline` produces a read-only, privacy-preserving report from an existing
DevKit SQLite database. It is an initial baseline for turn usage coverage, not a coordinator
profiler. It reads `agent_sessions.agent_type`, session IDs, turn IDs/timestamps, and the
`usage_json` and `raw_meta` columns of `agent_turns`. It never reads transcript rows, prompts,
tool bodies, credentials or account records. The database must already exist; SQLite is
explicitly opened with `mode=ro` and the tool does not apply migrations.

**Semantics version 2** (`semanticsVersion` in the output). Each turn is read in process with
`agenthub.DeriveTurnUsage`, the same derivation the Settings usage report uses, so the two
agree on what is known: the raw meta (the `raw_meta` column, else the copy kept inside
`usage_json`) is normalized again by the Claude adapter, and the stored structured fields are
used only when a row has no raw meta at all. A false structured `0` stored next to real numbers
in the raw meta therefore reports the real numbers, a pointer counter that is really `0` is
known, an absent pointer counter is unknown, and a meta with no tokens, no data or an unreadable
meta makes the turn "without usage" (counted in `turnsWithoutUsage` and as unknown in every
field, never as `0`). Raw metadata is parsed in memory only and no part of it, including model
names, is emitted. Version 1 reports (allowlisted stored counters, `invalidRows` from
non-numeric values) are not comparable with version 2 reports.

Token accounting stays **Claude-only**, as in the harness-tuning spec. Other vendors keep their
session and turn counts, but `tokenCoverage` is `unknown` and every turn counts as without
usage. This does not reinterpret the earlier Codex baseline (`OPT-400ff3`) as provider-token
coverage. `partialSplitTurns` counts turns whose per-model `model_usage` split is incomplete or
differs from the turn total (helper models can explain a difference, but a mismatch alone does
not); the split is never added to a total.

Build and run from the repository root:

```sh
go run ./cmd/devkit-team-baseline --db /absolute/path/to/devkit.db
```

Limit the report to selected sessions and a half-open time window (`--from` inclusive,
`--to` exclusive):

```sh
go run ./cmd/devkit-team-baseline --db /absolute/path/to/devkit.db \
  --session SESSION_ID_1 --session SESSION_ID_2 \
  --from 2026-10-01T00:00:00Z --to 2026-10-08T00:00:00Z
```

The report gives selected sessions (including sessions with no turns) and turns by vendor,
turns with/without usage, and each counter's known row count, unknown row count, `invalidRows`
(a sum that would overflow `int64`) and sum of known values. Time bounds filter turns by parsed
RFC3339 instant with nanosecond precision (`--from` inclusive, `--to` exclusive); session counts
still include every selected session with a turn in the database, even when all its turns are
outside the time window.

Counters are reported independently. Cached input and cache-write counts are not added to
`inputTokens`, and totals are not reconstructed from component counters. The report names its
source and returns explicit unknown entries for coordinator wakeups, coordinator
context/request size, model identity, cache-counter semantics, cumulative-versus-per-turn
scope, and token savings. These are tokens, not a share of the subscription limit, and this
tool starts no vendor runtime and runs no model.

## Comparable clean-session pilots

Use the same fixed workload and acceptance criteria for a control session and a managed-team
session. Create fresh sessions for each run; do not replay/fork an existing session as new
usage. Keep vendor/account, model, repository revision, prompt, tool permissions, and task
scope as similar as practical. Record whether each run is cold or warm cache and the session
IDs and timestamps in the experiment notes. Run each report against the same local database,
using the corresponding session IDs and exact turn window. Session totals include all selected
sessions even if no turn falls inside the window:

```sh
# Control: one coordinator/agent, one fresh session
GOFLAGS= go run ./cmd/devkit-team-baseline --db /absolute/path/to/devkit.db \
  --session CONTROL_SESSION_ID --from 2026-10-01T00:00:00Z --to 2026-10-01T02:00:00Z

# Managed: same workload, fresh managed group sessions; repeat --session for each member
GOFLAGS= go run ./cmd/devkit-team-baseline --db /absolute/path/to/devkit.db \
  --session MANAGED_COORDINATOR_SESSION_ID --session WORKER_SESSION_ID \
  --session REVIEWER_SESSION_ID --from 2026-10-01T03:00:00Z --to 2026-10-01T05:00:00Z
```

The times and IDs above are placeholders. Compare per-vendor, per-field totals and coverage
only when workload, cache state, and available usage sources are comparable. Include failed
and review turns in the session scope. Evaluate quality separately against the same acceptance
criteria, including missed findings and rework. The CLI does not compute a savings percentage
or decide whether two runs are equivalent.

## Measurement limits

The current local schema does not persist coordinator wake events/reasons or coordinator
context/request counters, so those appear as `unknown`. Do not substitute the number of turns
for wakeups, or infer a wake reason from nearby timestamps. The normalized turn counters can
be lower bounds when a vendor omits usage; `knownRows`, `unknownRows`, and `invalidRows` expose
that coverage.
The report groups by vendor only and does not emit model names.
Different vendors may define input/cache totals differently; this tool preserves their
reported fields and emits `cacheCounterSemantics` and `cumulativeOrPerTurn` as unknown rather
than guessing. It cannot measure quality,
latency, human wait time, duplicate dispatches, or token savings.

Do not copy database contents into the report. Keep pilot session IDs in the local experiment
notes; the report includes IDs only when explicitly supplied as `--session` scope.
