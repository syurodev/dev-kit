### CodeGraph: database tables
Java only, same shape as configuration: the analyzer records `EntityTable` (`@Entity`/`@Table`, Spring
Boot's snake_case default, secondary/join/collection tables) and `TableAccess` (`TableExtractor.java`,
SQL read by `SqlTables.java`), two tables `entity_tables` / `table_accesses`, joined with the call graph at
read time by `Service.FindTableUsage` (MCP `codegraph.findTableUsage`).
- **SQL is read by tokens, not parsed**: FROM/JOIN/USING read, UPDATE/INSERT INTO/DELETE/MERGE/TRUNCATE write.
  Anything that looks like a clause but is not (`EXTRACT(.. FROM ..)`, `FOR UPDATE`, CTE names, string
  literals) has a test in `SqlTablesTest`; add one before adding a rule.
- **Inherited Spring Data CRUD has no node and no edge** — `repo.save(x)` is recorded at the CALL
  (`repository-crud`), recognised by the receiver's type from the solver. A method the repository
  declares is attributed to that method, and its callers come from the call edges, never twice.
- **A name that is also one of an entity's tables asks about that table** (`FindTableUsage`): with the
  default naming class, entity and table are one word. An entity name that differs brings all its tables.
- Not read: `@NamedQuery`, `EntityManager.persist/merge`, tables named by a variable, triggers/cascades/views,
  Liquibase. Gaps are in `docs/dev-kit/codegraph.mdx` § Truy vấn nối với bảng DB.

### CodeGraph: calls between services
`internal/crossservice` joins what each service's index says — the routes it serves, the HTTP calls it
makes (`OutboundCall`, table `outbound_calls`) — into edges, **when asked**; nothing stores them.
`codegraph.ServiceSnapshot` is what one service offers the join: from a resident service, or read from
its index on disk with no Service opened (`Registry.Snapshot`; no watcher, no resident slot).
- Callers: Java (`OutboundCallExtractor`: Feign, Spring HTTP interfaces, RestTemplate, WebClient,
  RestClient) and Lua (`analyzer_lua_http.go`: `resty.http`, recognised by provenance of
  `require "resty.http"`, never by a method name). A `foldString` hole is `{expr}`, the shape Java uses.
- `workspace.ResolveTarget` resolves a host against the CALLER's config. With no host (a plugin's
  `{conf.url}`, an HTTP interface) the route alone links it, as `path-unique`, only when it has ≥ 2
  literal segments and exactly one other service serves it; several is `ambiguous-path`. Never pick one.
- An unreadable service is named, not skipped (`not-indexed`), and every edge carries how old the
  other end's index is. A service nothing watches can be wrong without saying so.
- Layering: `crossservice` imports `codegraph` and `workspace`; `codegraph` still does not import
  `workspace` (arch test). The MCP tool resolves "the asker's workspace" from `workspace.list`'s logic.

### CodeGraph: shared Redis keys
Same shape as the HTTP join: each service records `RedisAccess` (table `redis_accesses`; Java
`RedisExtractor`, Lua `recordRedis`), `crossservice.RunRedis` joins at read time (MCP
`codegraph.findRedisKeyUsage`). A key is a PATTERN (`fsap:access:{org}:{api}`); two are one resource
when some string matches both (`overlaps`, a DP over `*`), and a pattern needs ≥ 3 fixed leading
characters to count — `*` and `{key}` overlap everything, so they are dropped and a query by one is refused.
- Redis is recognised by the receiver (an `opsFor*()` chain, a declared template/client/command type,
  Lua provenance of `require "resty.redis"`), never by a method name like `get`.
- Not seen: keys built elsewhere and passed in, `@Cacheable`, Lua `EVAL`, pub/sub, Redisson. Say so in
  the answer's notes instead of widening the match.

### CodeGraph: Kafka topics
Same shape as Redis (table `kafka_accesses`; Java `KafkaExtractor`, Lua `recordKafka`;
`crossservice.RunKafka`, MCP `codegraph.findKafkaTopicUsage`), with two differences:
- **The topic name is folded against the config of the service that WROTE it** (`Service.Config.Resolve`),
  never against another's — two services spell one topic by different properties. A property the
  service's config does not set is `unresolved`, listed, not dropped.
- **Topics have a direction**, so the answer carries `gap` (`no-consumer` / `no-producer`). It is a gap in
  THIS workspace only; say so (the notes do) instead of calling the topic dead.
- Kafka is recognised by annotation, constructor or declared receiver type, never by a method name. Kafka's
  own `Producer`/`Consumer` interfaces are NOT in the type sets on purpose: a project's own `Producer.send`
  would become a topic. Not seen: Spring Cloud Stream bindings, `@SendTo`, `sendDefault`, `@RetryableTopic`.

### CodeGraph: opening a project
`Service.OpenProject` does three things before any analyzer runs, each because the version
without it was worse.

- **Detection gates the subprocess** (`internal/codegraph/detect.go`) and now also picks the
  analyzer. Without it, opening any folder started a JVM to walk it and reported the empty
  result as an empty project rather than as the wrong folder. A `Toolchain` declares
  `Markers` (build files) and `SourceExt`; a directory holding a marker is a module root —
  for Lua that includes `handler.lua`, so a repository of Kong plugins reads as one module
  per plugin. First toolchain to claim the folder wins, and Java is ahead of Lua because
  its markers are build files while Lua's fallback is "any folder holding a .lua".
  An unrecognised folder (Terraform, Helm, manifests…) still opens, **for its files only**
  (`Service.openForBrowsing`): no analyzer and no watcher run, the tree/editor/git status
  work, `IndexInfo.Unsupported` is true, and graph queries and `WaitForIndexReady` answer
  `ErrUnsupportedProject` (bridge code `unsupported_project`). That is a state, not a
  failure: the UI labels it "Not supported" (neutral tone), hides the Entry points tab,
  and never shows an error card or a retry for it; its messages name no language. Detection is skipped when no
  toolchain is registered — an empty registry knows nothing and must not refuse everything.
- **A folder of microservices is one index, not none.** `ProjectAnalyzer` discovers every
  `src/main/java` below the root (skipping `target/`, `node_modules/`, dot-dirs; depth 6),
  falling back to the root itself for a bare folder of `.java` files. All roots go into one
  `CombinedTypeSolver`, which is what keeps a call from one service into a shared module
  resolvable — one subprocess per service would lose that. `filePath` is relative to the
  project root, so roots never collide.
- **A known project reuses its stored index, then re-indexes in the background.** The index
  is persisted per project (`ProjectDBPath`); re-deriving it on every open cost as much as
  the first index with the UI blocked throughout. Reuse requires BOTH `HasIndex` and
  `IndexedProject == this project` — the per-project file is a naming convention, and a
  convention is not something the code can check, so the index records which project it
  describes (`index_meta`). An index written before that table simply re-indexes once.

