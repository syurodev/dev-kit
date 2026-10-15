# CodeGraph flow — an outline that reads like the code, and a graph that says something

Status: approved in conversation 2026-10-15. Worked example throughout:
`fsap-event-router` (Camel route `organization-fanout-{domain}` →
`AbstractResilientForwardProcessor.process`).

## Problem

A flow tab draws the call graph behind an entry point. On a real service it is
a grid of cards that look the same and arrows that say nothing:

- **Names lose the part that identifies them.** A card shows `Class.method` in
  268px; with `AbstractResilientForwardProcessor` the method name is cut, so six
  of ten cards read `AbstractResilientForwardPr…`. The second line is the file
  name, which in Java repeats the class.
- **`UNKNOWN` on almost every card.** `LayerClassifier` knows
  `@Controller/@Service/@Repository` only; Camel processors, `@Component`s and
  abstract bases spend a coloured caps line saying nothing.
- **Edges carry no order and no condition.** `call_sites.line` exists but is
  not shown. `Edge.Guard` exists in Go and in the bindings, but the TS
  `CodeGraphEdge` type drops it. Guards are merged per `(from, to, kind)`, so
  `process → quarantine` — called in three places, all on failure paths — is one
  unguarded edge, and `catch` is not a guard shape at all.
- **Utility calls weigh the same as the flow.** `counter`, `metricPrefix`,
  `kafkaEndpoint`, `quarantineCounter` are cards beside `decide` and `deliver`.
- **What a node is for is hidden.** `Node.summary` (Javadoc) is on most of these
  methods; the card never shows it.
- **Library calls are invisible.** `producerTemplate.send(...)` (the quarantine
  send, the delivery) and `in.setHeader(...)` (what the router actually does to a
  message) are anonymous `library` call sites.

## Goals

1. A flow can be read top to bottom in source order, with branches as branches,
   implementations under the abstract call, and the library calls that matter.
2. Noise is folded automatically, with the reason shown, and never requires
   marking anything. Marking is an optional override.
3. The graph keeps its role as a map and becomes legible.
4. The data contract is language-neutral: Lua and JS/TS analyzers can fill it
   later with no change to the struct, the bridge or the frontend.

## Non-goals (this round)

- Lua and TypeScript analyzers filling the new columns (they get the flat,
  line-ordered outline only).
- Teaching `KafkaExtractor` that `ProducerTemplate.send(uriVariable)` is a Kafka
  produce; classifying Camel `Processor` / `@Component` into a layer.
- Changing MCP `codegraph.getApiFlow` (it may consume steps later).
- Evaluating conditions. Frames name a condition as written, like `Edge.Guard`.

## Decisions

| Question | Decision |
| --- | --- |
| Reading mode | Two modes of one flow tab, **Outline \| Graph**, switched with shadcn `Tabs` (one shown at a time). Outline is the default. |
| Selection | Shared: selecting in one mode selects (and centres / scrolls to) the same node in the other. |
| Source | The existing inspector split panel, for both modes. |
| Library calls | All recorded; folded by an automatic score with a reason; optional per-method / per-type override. |
| Folding | Consecutive hidden steps inside one branch fold into a single `+N · reasons` row, expandable in place. Runs never merge across branches. |
| Scope | Java analyzer + Go + frontend. |

## 1. Data: one step per call site (analyzer + store)

`call_sites` already holds one row per call. It gains columns, all filled by the
Java analyzer:

| Column | Type | Example | Purpose |
| --- | --- | --- | --- |
| `col` | int | `13` | Order calls that share a line |
| `text` | text ≤ 120 chars | `producerTemplate.send(endpointUri, exchange)` | Show the call as written (cut with `…`) |
| `callee_key` | text | `org.apache.camel.ProducerTemplate#send` | Scoring and overrides |
| `receiver` | enum text | `param` | Side-effect signal |
| `result_use` | enum text | `discarded` | Role signal |
| `frames` | JSON text | see below | Outline nesting |

Enums (language-neutral; an analyzer that does not know writes `unknown`):

- `receiver`: `param`, `field`, `local-from-param`, `local`, `static`, `this`,
  `unknown`. `local-from-param` = a local assigned from a parameter or a call on
  one (`Message in = exchange.getIn()`).
- `result_use`: `discarded`, `condition`, `argument`, `assigned`, `returned`,
  `unknown`.

`callee_key` is `<module-or-type>#<member>`: Java uses the resolved declaring
type's qualified name (or the declared receiver type's text when unresolved);
Lua will use the require path (`resty.http#request_uri`, `ngx#log`); TS the
module specifier and export (`@nestjs/microservices.ClientProxy#send`,
`console#log`). A project call keeps its `to_id` as today; `callee_key` is set
for it too.

### Frames

`frames` is the path of enclosing control structures, outermost first:

```json
[{"id":"72:9","kind":"try","text":"try"},
 {"id":"91:13","kind":"closure","text":"breaker.executeCallable(…)"}]
```

- `id` — `line:col` of the structure's start, unique within the method; steps
  sharing a frame id are grouped under one heading.
- `kind` — `if`, `else`, `case`, `catch`, `finally`, `try`, `loop`, `closure`,
  `and`, `or`, `ternary`, `await`. The set is open: the frontend renders an
  unknown kind as its `text`.
- `text` — the condition / heading as written, ≤ 120 chars
  (`decision instanceof Skip(String reason)`, `catch AclLookupException`,
  `for domainRoute : routingProperties.domains()`).
- `exits` — true when the branch ends in `return` / `throw` / `break` /
  `continue`; drawn as `↩`.

A **guard clause** (`if (x) { …; return; }`) is a frame with `exits: true` for
the calls inside it; the calls after it carry **no** frame for it. The outline
shows the early exit above and keeps the rest flat, instead of a staircase of
`!Skip && !Reject`. `Edge.Guard` is unchanged (it still records the implied
negation), so graph tooltips and MCP keep their meaning.

`Guards.java` also learns `catch`: a call inside a catch block is guarded by
`catch <Type>`. This fixes `process → quarantine` reading as unconditional.

### Implementations

The analyzer already writes a call site from an abstract/interface method to
each override (`ProjectAnalyzer` ~702). The read side folds these into the
calling step's `Impls` rather than showing them as separate calls.

### Migration and old indexes

Columns are added the way `migrateEdgeGuard` adds `edges.guard`, defaulting to
empty. An index written before has no `text`/`frames`; the outline then renders
flat by line with a notice "Re-index to see branches". A new DevKit build
re-indexes anyway. The writer (`AnalysisResultWriter`) and `analyzer_subprocess.go`
carry the new fields; an older jar's output (fields missing) decodes as empty.

## 2. Visibility scoring (Go, at read time)

New pure function in `internal/codegraph/callimportance.go`:
`Visibility(step StepFacts, stats ProjectCallStats, overrides Overrides) (visible bool, reason string)`.
It runs when steps are read, never stored, so tuning needs no re-index.

`ProjectCallStats` is computed once per index revision from `call_sites`: for
each `callee_key`, the number of distinct calling methods, and the total method
count. Cached on the service, invalidated with the index.

First matching rule wins:

1. **Override** (method key beats `Type#*`) → as chosen. Reason `you chose`.
2. **Project call** → visible, except a **pure helper**: a project method none of
   whose own steps are visible and whose result here is `argument` / `returned`
   / `assigned`, or which only calls hidden things. Reason `helper`. (Computed
   bottom-up over the subgraph's methods; a cycle counts as visible.)
3. **Structural** → visible: a `closure` argument containing a visible project
   call (`executeCallable`), an extractor-recognised effect (Kafka/HTTP/Redis/DB),
   or `result_use = condition`. Reason `wraps deliver`, `Kafka produce`,
   `decides a branch`.
4. **Known infrastructure** → hidden, by `callee_key` prefix table (one table,
   rows per language): logging (`org.slf4j`, `org.apache.logging`,
   `java.util.logging`, `org.apache.commons.logging`, `ngx#log`, `kong.log`,
   `console`), metrics/tracing (`io.micrometer`, `io.opentelemetry`),
   language utilities (`java.lang.String`, `java.util.Objects`,
   `java.util.Optional`, `java.util.` collections and streams), builders
   (`#builder`, `#build`, `.Builder#`). Reason `logging` / `metrics` /
   `utility` / `builder`.
5. **Writes to input or state** → visible: `receiver` in `param`, `field`,
   `local-from-param` and `result_use = discarded`. Reason `writes to <receiver name>`.
6. **Common in this project** → hidden: called from ≥ 15% of methods and ≥ 8
   methods (constants `commonShare`, `commonMin`). Reason `common (N methods)`.
7. **Otherwise**: an accessor (`get*` / `is*` / `has*`) whose result is used →
   hidden (`accessor`); `result_use = discarded` → visible (`side effect`);
   else hidden (`value`).

A signal whose input is `unknown` is skipped, so a language that fills less
still gets a sensible answer.

### Overrides

Stored locally in `app_preferences` under `codegraph_call_visibility`
(`{"org.apache.camel.Message#setHeader":"show","org.apache.camel.Message#*":"hide"}`),
not synced, applying to every project (a library method behaves the same
everywhere). Bridge `PreferencesGetCallVisibility` /
`PreferencesSetCallVisibility(key, mode)` (`mode` = `show` | `hide` | `""` to
reset); UI only, no MCP tool. Keys are validated (`#` present, ≤ 300 chars,
≤ 500 entries).

### Expected result on the worked example

Visible: every `in.setHeader(...)` (writes to `in`), both
`producerTemplate.send(...)` (writes to `producerTemplate`),
`breaker.executeCallable` (wraps `deliver`), and the project calls `decide`,
`deliver`, `quarantine`, `ensurePartitioningKey`, `aclStore.find`.
Hidden: `log.*` (logging), `Counter…increment` (metrics), `getIn` / `getHeader`
(accessor), `metricPrefix`, `kafkaEndpoint`, `counter`, `quarantineCounter`
(helper), and `circuitBreakerRegistry.circuitBreaker(...)` (`value`: its result
is assigned, rule 7). The conversation expected the last one visible; the rules
as written hide it, and an "Always show" override is the intended remedy rather
than a data-flow rule this round. This list is the golden test.

## 3. API

```go
// CodeGraphGetSteps returns the ordered steps of each requested method.
// Only methods in the caller's current flow are asked for.
func (a *AppService) CodeGraphGetSteps(nodeIds []string, servicePath string) (StepsResult, error)

type StepsResult struct {
    Steps     map[string][]Step `json:"steps"`     // node id → steps in source order
    Truncated []string          `json:"truncated"` // node ids whose steps were capped
    Legacy    bool              `json:"legacy"`    // index predates frames: flat outline
}

type Step struct {
    Line      int         `json:"line"`
    Col       int         `json:"col"`
    Text      string      `json:"text"`
    CalleeKey string      `json:"calleeKey"`
    To        string      `json:"to,omitempty"`    // project node id
    Impls     []string    `json:"impls,omitempty"` // overriding node ids when To is abstract
    Frames    []StepFrame `json:"frames"`
    Visible   bool        `json:"visible"`
    Reason    string      `json:"reason"`
    Effect    *StepEffect `json:"effect,omitempty"`
}

type StepFrame struct {
    ID    string `json:"id"`
    Kind  string `json:"kind"`
    Text  string `json:"text"`
    Exits bool   `json:"exits,omitempty"`
}

type StepEffect struct {
    Kind   string `json:"kind"`   // kafka | http | redis | db
    Role   string `json:"role"`   // produce | consume | call | read | write
    Target string `json:"target"` // topic, path, key pattern or table, as recorded
}
```

- Steps are sorted by `(line, col)`. ≤ 300 steps per method, ≤ 200 node ids per
  call (`invalid_argument` beyond).
- `Effect` is joined from `kafka_accesses` / `outbound_calls` /
  `redis_accesses` / `table_accesses` by `(from_id, line)`.
- Read from the on-disk index like `EntryPointsOf` / `OutlineOf` (no Service
  needed); `ErrUnsupportedProject` as for other graph queries.
- Granted to the UI with the other `CodeGraph*` methods
  (`TestEveryWorkspaceBridgeMethodIsGrantedToTheUI` style coverage).
- `CodeGraphEdge` in `frontend/src/bridge/codegraph.ts` gains `guard?: string`
  (already in the bindings).

## 4. Frontend

### Outline model (pure, `modules/codegraph/outline/buildOutline.ts`)

Input: root id, subgraph nodes, `StepsResult`, expansion state. Output: a flat
list of visible rows (for virtualisation), each with `depth`, `kind`, `nodeId`,
optional `stepIndex`.

- A method's steps become a tree by `frames[].id`; a frame heading row is
  emitted once per group (`catch AclLookupException ↩`).
- Consecutive hidden steps in one group fold into one row
  `+N · <reasons, most frequent first, ≤ 3>`; expanded, they show dimmed with a
  reason tag.
- A project step is expandable into the callee's steps; an abstract one shows
  `↳` rows for `Impls`.
- A method already expanded elsewhere in the tree renders once; later
  occurrences are a `shown above ↑` row that jumps to it. A call back into a
  method on the current path is `↻ recursion`.
- Default expansion: three levels of project calls; deeper on demand. State is
  in the flow's document store, memory only.
- `Legacy` or a language without frames: one group per method, ordered by line.

Row kinds: project call (bold, ▸), implementation (`↳`), frame heading
(`if …`, `catch …`, `try`, `for …`, `↩`), visible library call (dimmed, as
written), effect badge (`Kafka produce`, `HTTP`, `Redis`, `DB`), fold row. The
line number is right-aligned; the full path is in the tooltip.

Interaction: ↑/↓ move, → / ← expand / collapse, Enter opens the inspector,
click selects. Context menu (rows built on open, like `referenceMenu.ts`):
Always show / Always hide `Type#method`, Always show / hide every call on
`Type`, Reset, Open in editor, Show in Graph. Rows are windowed past 500.

### Flow document

`FlowDocument` gains a `Tabs` strip (Outline | Graph) at the right of the
header; mode is per document, memory only, default Outline. Selection lives in
the flow store (it already holds `inspector.nodeId`); Graph centres the
selected card on switch (`fitView` on that node), Outline scrolls to the first
row of that node. Steps are fetched with the subgraph and again after
`loadMoreNeighbors`, for the new node ids only.

### Graph changes

- `ApiNode`: class (short) in the small top line with the line number at the
  right; method name as the title with params dimmed; the first sentence of
  `summary` instead of the file chip; effect badges; `+N hidden calls` footer.
  Unknown layer shows no caps label (known layers keep colour and icon).
- Edges: a numbered order chip (the caller's step order of its first call to
  that target) and the innermost frame as the label; the full `guard` in the
  tooltip. Caller → implementation edges are dashed with label `implements`.
- Hidden project helpers (rule 2) are not drawn; their callers' `+N` counts them.
  `rankGraph` / `place` are unchanged; they receive the filtered node set.

## Error handling

- `CodeGraphGetSteps` failure: the outline shows an inline error with Retry; the
  Graph mode keeps working (it does not depend on steps for drawing, only for
  hiding helpers and order chips, which then fall back to today's behaviour).
- Malformed `frames` JSON on a row: that row is returned with no frames (logged
  once per index), never failing the method.
- Override write failure: toast, state unchanged.
- Truncated steps: a `…N more steps not shown` row at the end of that method.

## Testing

- **Java** (`analyzer` tests): a fixture modelled on the worked example —
  `try`/`catch` with a guard clause, an `instanceof` pattern, a lambda passed to
  a library call, a `for` loop, `&&` short-circuit — asserting `col`, `text`,
  `callee_key`, `receiver`, `result_use`, `frames` (ids, kinds, `exits`) and the
  new `catch` guard. Existing `Guards` tests stay green.
- **Go**: `callimportance_test.go` table test (the golden list above, plus one
  row per rule and the `unknown` skips); store migration test (old index opens,
  `Legacy` true); `CodeGraphGetSteps` ordering, caps, `Impls` folding, effect
  join; bridge grant test.
- **Frontend** (`bun test src/modules/codegraph`): `buildOutline.test.ts` —
  grouping by frame id, fold runs not crossing groups, `shown above`,
  recursion, default depth, legacy flat; `ApiNode` / label tests updated.
- **Manual**: `fsap-event-router` in the desktop app — the outline matches §2's
  expected list; switching modes keeps the selection.
- Commands: `go test ./internal/codegraph/ ./internal/bridge/ ./internal/arch/ -count=1`,
  `cd frontend && bun test src/modules/codegraph && bun run build`, and the
  analyzer's Maven tests.

## Later

- Lua (`analyzer_lua_resolve.go`) and TS (`analyzer_ts_model.go` already holds
  call expressions) fill `col`, `text`, `callee_key`, `receiver`,
  `result_use`, `frames`; nothing above the store changes.
- `KafkaExtractor`: `ProducerTemplate.send` with a URI built by a project
  helper returning `"kafka:" + …`.
- MCP `getApiFlow` returning steps.
- Tune `commonShare` / `commonMin` on two or three real projects.

## Addendum — decisions made while planning (2026-10-15)

- **`role` column.** `call_sites.role` is `""` for a call and `"override"` for the
  dispatch link the analyzer writes from an overridden method to its override.
  `Step.Impls` is built from these rows. They are never returned as steps.
- **Closure frame id.** A closure passed as an argument has the frame id of the
  call it is passed to (`line:col` of that call). "Wraps a project call" (rule 3)
  matches the frame id to the wrapping step's position, and the outline draws
  the closure's steps under that call row instead of under a separate heading.
- **Pure helper, as implemented.** A project callee is a helper when it is
  loaded (≤ 3 levels below the asked methods, ≤ 2000 methods in all), has no
  overrides, writes no field (`field_accesses`), and none of its own steps is
  visible. A cycle or an unloaded method counts as not a helper.
- **Utility on a field.** The utility category (rule 4) does not hide a
  discarded call on a `field` receiver (`cache.put(k, v)`): that falls through
  to rule 5 as a state write.
- **Unresolved Java types.** When the declaring type does not resolve, the
  callee key uses the declared type's simple name (`Counter#increment`), and the
  infrastructure table also matches a few simple names (`Logger`, `Counter`,
  `MeterRegistry`, …). Lombok's `log` is keyed `org.slf4j.Logger#<method>`.
- **`GetSteps` goes through the Service** that `CodeGraphGetNeighbors` uses
  (same `servicePath`), not the on-disk index reader.
- **Click on an outline row** selects it and opens the inspector, as a click on a
  card does. Enter does the same from the keyboard.
- **Windowing** uses the shared `useVirtualRows` threshold (100 rows).
- **Steps are re-fetched** for the whole subgraph after "Load more", a refresh,
  or an override change.
