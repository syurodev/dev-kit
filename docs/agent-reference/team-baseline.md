# Team orchestration baseline CLI

`cmd/devkit-team-baseline` produces a read-only, privacy-preserving report from an existing
DevKit SQLite database. It is an initial baseline for turn usage coverage, not a coordinator
profiler. It reads `agent_sessions.agent_type`, session IDs, turn IDs/timestamps, and only the
allowlisted numeric counters in `agent_turns.usage_json`. `NormalizedUsage` currently marshals
with Go's default exported-field names (`InputTokens`, `CachedInputTokens`, and so on); the
report also accepts historical lower-camel aliases. If both spellings occur with conflicting
values, that counter is marked invalid and is not counted twice. The usage object also embeds
`RawMeta`, but the CLI does not inspect or emit that value; it also never reads the separate
`raw_meta` column, transcript rows, prompts, tool bodies, credentials, or account records. The
database must already exist; SQLite is explicitly opened with `mode=ro` and the tool does not
apply migrations.

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
turns with/without a usage object, and each counter's known row count, unknown row count,
invalid row count, and sum of accepted values. Time bounds filter turns by parsed RFC3339
instant with nanosecond precision (`--from` inclusive, `--to` exclusive); session counts still
include every selected session with a turn in the database, even when all its turns are outside
the time window. A missing field or null counter remains unknown. Negative values, non-integer
values, out-of-range integers, and additions that would overflow are marked invalid and
excluded from the partial sum.

`InputTokens` and `OutputTokens` are non-pointer `int` fields in the current Go
`NormalizedUsage` type and serialize as zero even when an adapter did not supply a count. The
stored JSON therefore cannot always distinguish a measured zero from a default zero. The CLI
reports an explicit zero as recorded/known, but this is a source-data limitation, not proof that
a vendor measured zero. Counters are reported independently. In particular, cached input and cache-write counts are
not added to `inputTokens`, and totals are not reconstructed from component counters. The
report names the source of the normalized data and returns explicit unknown entries for
coordinator wakeups, coordinator context/request size, model identity, cache-counter semantics, cumulative-versus-per-turn scope, and token savings.

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
The source fields do not identify a model, so the report uses vendor only. `raw_meta` may hold
vendor payloads and is intentionally excluded, even where it might contain extra counters.
Different vendors may define input/cache totals differently; this tool preserves their
reported fields and emits `cacheCounterSemantics` and `cumulativeOrPerTurn` as unknown rather
than guessing. It cannot measure quality,
latency, human wait time, duplicate dispatches, or token savings.

Do not copy database contents into the report. Keep pilot session IDs in the local experiment
notes; the report includes IDs only when explicitly supplied as `--session` scope.
