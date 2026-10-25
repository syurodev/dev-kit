### CodeGraph: entry points
What the sidebar lists is `codegraph.EntryPoint` (`internal/codegraph/entrypoints.go`), not
"endpoints": where a flow starts because something outside the code calls in. `Kind` says
what calls in — `http`, `graphql`, `rpc`, `message`, `socket`, `schedule`, `plugin` (a Kong
phase) — `Verb`/`Target` are the chip and what it answers to (`GET /users`,
`ACCESS rate-limit`, `CRON 0 * * * *`), and `Service`/`Owner`/`Group`/`Order` place it in
the list. The UI groups kind → service → owner and does no guessing of its own.

- **An analyzer sets what it knows; `completeEntryPoints` fills the rest on every read**
  (`Service.ListEntryPoints`): kind from the verb, owner/group from a Java node id, the Java
  service from the package segment after the shared prefix. So an index written before
  these fields existed reads the same as a new one. The store moved `endpoints` into
  `entry_points` in place (rows copied, not dropped — a reopen serves the stored index).
- **Kong phases are `plugin` entries under their plugin, `Order`ed as Kong runs them**; a
  plugin's Admin API is `http` under the same plugin name.
- **Java HTTP keeps its wire format** (`endpoints`, `httpMethod`, `path`):
  `analyzer_subprocess.go` translates it, so older jars still read. Camel adds `entryPoints`
  with `kind=message` for Kafka (`camel` when the source component is unresolved), route/topic/group
  source expressions, and `entryPointSupport`/`entryPointWarnings`. A synthetic method per
  source route template owns runtime callback edges; `configure()` does not call the handler.
  `direct:`/`seda:` sources are internal flow nodes, not external entries. Rebuild the Java jar
  before running `go test ./internal/codegraph/ -run TestCamel -count=1`: this test invokes the
  real analyzer on `analyzer/src/test/resources/camel-project`, persists SQLite, and traverses
  Go flows. No Kafka/application process is needed. Details: `docs/design/codegraph-camel.md`.
- **Kept stable on purpose**: the MCP tool names (`codegraph.listEndpoints`, `getApiFlow`'s
  `endpointId` input — agents' allowlists name them), the entity-ref kind `"endpoint"`
  (agents and saved transcripts resolve it), and the API Client's `EndpointDraft` (an HTTP
  concept). Only `http` entries offer "Try in API Client". Tool-result cards read both the
  new (`verb`/`target`, `entryPointId`) and the old field names, since transcripts outlive builds.

### CodeGraph: guards on call edges
`Edge.Guard` is the condition a call runs under, read from the Java source around it
(`analyzer/.../Guards.java`): `if`/`else`, `? :`, `switch` entries, the right side of `&&`/`||`,
a `catch` body (`catch <Type>`, so a call made only when that exception was thrown says so) and the
guard clause (`if (!enabled) return;` guards what follows). Loops and the `try` block itself are not
guards. **Syntactic, never evaluated** — a guard names a flag, not its value. One edge per
`(from, to, kind)`: one unconditional call makes the edge unconditional, otherwise guards are joined
with `||` (`Guards.merge`). `edges.guard` is a migrated column (`migrateEdgeGuard`); old indexes read
as unguarded until a re-index, which a new DevKit build triggers anyway. `FlowStep` carries `Depth`
(the flat DFS list is a tree) and `Guard`, so the `catch` shape also reaches MCP `getApiFlow` output.
`call_sites` does not carry it; Lua and TypeScript do not set it. Design:
`docs/dev-kit/codegraph.mdx` § Guard trên cạnh gọi.

### CodeGraph: flow outline and steps
A flow tab has two modes, **Outline | Graph** (shadcn `Tabs`, Outline first; spec
`docs/superpowers/specs/2026-10-15-codegraph-flow-outline-design.md`, its Addendum lists what changed in the build).
- **Steps are call sites with more columns** (`call_sites.col/text/callee_key/receiver/result_use/frames/target/role`,
  `migrateCallSiteSteps`). Java fills them (`Steps.java`); Lua and TS do not yet and read as a flat outline. A
  library call with neither text nor callee key (what Lua/TS record) is hidden as `unknown call` unless an
  extractor effect lands on it. `frames` are language-neutral (`if, else, case, catch, finally, try, loop,
  closure, and, or, ternary, await`). A guard clause is a frame only for the calls inside it.
- **`Legacy`** (re-index prompt) is decided per `GetSteps` request: some asked method has non-override sites,
  none of them has text, and the detected language is `java`. The frontend asks in chunks (≤ 200 ids) and ORs
  `legacy` across them (`bridge/codegraph.ts`).
- **Positions are the call's NAME**: a method call's `line:col` is where its name starts (`a.b()` points at `b`),
  and call-level effect extractors record that same line and column. A closure's frame id is the `line:col` of
  the call it is passed to. Effect tables (`kafka_accesses`, `redis_accesses`, `outbound_calls`, table accesses)
  gained `col` and join to a call site by `(from, line, col)`; a col-0 effect (old index, declaration-level)
  attaches by line only, to the first library call on it, and only in a method none of whose sites has a column.
  **For agents**: after a re-index, the line reported for a method call — call sites, Kafka/Redis/table/outbound
  rows — is the line of the call's name, so for a multi-line chain the lines from references/definitions,
  def-use and `codegraph.find*Usage` move to the line the name is on, not the line the statement starts on.
- **`target` vs `to`**: a call into a body-less project method (a repository or client interface) has `to` = ""
  (a wall for flow walks) and `target` = the project node it names; `Step.Target` carries it so the outline can
  show its effect. `role = "override"` rows are dispatch links (overridden method → override), never returned as
  steps, only `Step.Impls`; `references.go` reads `role` and keeps the old line rule as a fallback.
- **Visibility is decided at read time** (`callimportance.go`, pure `Visibility`; `steps.go` supplies the facts).
  Rule order: your override → project call (a helper is hidden; else a body-less one — `to` empty, `target` set —
  with an extractor effect shows the effect label; else "project") → wraps a project call (closure) → extractor
  effect at the call → no text and no key (`unknown call`, hidden) → decides a branch → infrastructure table (a
  utility call discarded on a `field` falls through) → "writes to …" (receiver param/field/local-from-param, and
  only when the result is discarded) → common (≥ 8 calling methods and ≥ 15 % of the index's methods,
  `CallStats`) → side effect (result discarded) / accessor (only when the result use is known) / value. Every
  answer has a reason the outline shows.
- **Helper** = a call into a project method that has no overrides, whose call does not pass a closure holding a
  project call that is overridden, body-less or not itself a helper, and that is loaded (≤ 3 levels below the
  asked methods, ≤ 2000 methods in all), writes no field, has no extractor effect and no visible own step.
  Memoised by `(id, depth)`; a cycle (reaches the depth limit), an unloaded method or a body-less project call is
  never a helper.
- **`CallStats`** is cached on the `sqliteStore` instance and dropped by `ReplaceIndex`; a read that overlaps a
  `ReplaceIndex` (the generation moved while it ran) is returned but not kept.
- **Overrides are local preferences** (`codegraph_call_visibility`, `PreferencesGet/SetCallVisibility`, UI only,
  all projects). `preferences.Service.SetCallVisibility` trims the key and requires ≤ 300 bytes,
  `<type>#<member>` with non-blank text on both sides of the first `#` and no control characters (`Type#*` is
  member `*`, the whole type); mode is `show|hide`, `""` clears. The 500-entry cap refuses only adding a new key
  (re-setting or clearing at the cap works). Writes are serialised by a mutex on the Service.
- `CodeGraphGetSteps(nodeIds ≤ 200, servicePath)` asks the same Service as `CodeGraphGetNeighbors`.
- **Frontend** (pure, `bun test src/modules/codegraph`): `outline/buildOutline.ts` builds rows with opaque keys:
  a step's id is `(calleeKey || to || target || text)#n` (n = occurrence of that base in its method), a row's key
  is `parent + "\u0001" + "<kind>:" + id` (kinds call/fold/frame/impl/more, root `"root"`), so a re-fetch keeps
  expansion and selection. Frame keys use the frame's `line:col` (with a `~n` suffix when repeated) and are **not**
  stable across a re-index that moves code. Hidden steps fold into a `fold` row naming their top reasons.
  `outline/outlineNav.ts` is the keyboard/tree-role model; `OutlineView.tsx` draws it with the shared
  `TreeGuides` and row geometry (`components/tree-guides.tsx`, also the sidebar's). Motion classes
  (`outline-row-enter`, `outline-row-flash`, `flow-mode-enter`) are off under `prefers-reduced-motion`.
  `graph/graphView.ts` hides helpers and drops only the cards that hid disconnected.
- **Arrowhead labels are an explicit `FlowCanvas` prop** (`labelAt?: "arrow"`), passed only by `FlowDocument`;
  the plan and database canvases leave it off and keep midpoint labels. With it, `ConnectorEdge` moves a label to
  the arrowhead (as its number alone, the rest in the tooltip) only when the label is numbered (`^\d+( · |$)`,
  `arrowLabel`) and the edge enters its card from the left; anything else stays at the midpoint.

### CodeGraph: configuration
Java only. The analyzer records what config files SET (`ConfigKey`: `ConfigFiles.java`, YAML via
SnakeYAML `compose()` — never constructed, alias-bomb capped — plus `.properties`) and what the code
USES (`ConfigUsage`: `ConfigExtractor.java`). Two tables, `config_keys` and `config_usages`, joined
at read time on `CanonicalConfigKey` (letters and digits only, lower-cased — Spring's relaxed
binding), so an env var, a kebab key and a camelCase key are one setting.
- **Secrets never enter the index as values**: a key whose separator-stripped name contains
  `password|secret|token|apikey|…` is stored `redacted` with no value (its env var names are kept).
  `FindConfigUsage` and every other reader inherit that; do not add a path that returns raw file text.
- `Service.FindConfigUsage` (MCP `codegraph.findConfigUsage`): per use, readers (`field_accesses`, or
  a `@ConfigurationProperties` field's getter/record accessor), gated methods (conditions), calls whose
  `Edge.Guard` names the field; then a bounded reverse walk over call edges to the entry points.
  It reads source, never evaluates: "what depends on the flag", not "is it on".
- Known gaps, all noted in the answer or in `docs/dev-kit/codegraph.mdx` § Cấu hình là một loại node:
  Lombok/implicit record accessors are unresolvable so `properties` readers can be missing;
  `@ConfigurationProperties` on a `@Bean` method is not read; Lua and TypeScript record no config.

