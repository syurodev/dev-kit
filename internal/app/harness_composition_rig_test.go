package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/audit"
	"devkit/internal/capabilities"
	"devkit/internal/database"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/mcp"
	"devkit/internal/permissions"
	"devkit/internal/preferences"
	"devkit/internal/storage"
	"devkit/internal/usagemetrics"
	"devkit/internal/vault"
	"devkit/internal/workspace"
	"devkit/internal/workspacememory"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The composed graph: real migrations and SQLite (HarnessStore, Agent Hub store),
// the real preferences service and its writer boundary, the real workspace
// service, the shared interaction hub, a real MCP server and SessionHost behind
// the real capability gateway and default grants, the real provider and the H4c
// coordinator, assembled by NewHarnessAssembly and attached by Attach exactly as
// main.go would. Only the vendor transports (scripted runtimes) and the person's
// answers are scripted.

// vaultOK is the one stubbed domain operation the composed tests call behind the
// real gateway.
type vaultOK struct{ mcp.VaultAPI }

func (vaultOK) Status() (vault.State, error) { return vault.StateUnlocked, nil }

// compGate is the production caller shape (the real gateway, the same call the
// bridge makes) with a counter of handlers that actually ran.
type compGate struct {
	gw      *capabilities.Gateway
	handled atomic.Int32
	hold    atomic.Pointer[holdGate]
}

type holdGate struct {
	entered chan struct{}
	release chan struct{}
}

func (g *compGate) holdNext() (entered <-chan struct{}, release func()) {
	h := &holdGate{entered: make(chan struct{}, 1), release: make(chan struct{})}
	g.hold.Store(h)
	var once sync.Once
	return h.entered, func() { once.Do(func() { close(h.release) }) }
}

func (g *compGate) Call(ctx context.Context, subject, capability, scope string, metadata map[string]any, handler capabilities.Handler) (any, error) {
	return g.gw.Call(ctx, capabilities.CallRequest{SubjectID: subject, Capability: capability, Scope: scope, Metadata: metadata,
		Handler: func(ctx context.Context) (any, error) {
			g.handled.Add(1)
			if h := g.hold.Load(); h != nil {
				select {
				case h.entered <- struct{}{}:
				default:
				}
				<-h.release
			}
			return handler(ctx)
		}})
}

func (g *compGate) CallOperation(ctx context.Context, r capabilities.CallRequest) (any, error) {
	return g.gw.Call(ctx, r)
}

func (g *compGate) PublishError(_ context.Context, _ string, err error) error { return err }

// scriptedVendor is a vendor transport with a resumable conversation. Its type is
// a real vendor type so the composed graph treats it as that vendor. It counts
// opens and loads, can run a hook as the vendor starts, and can pretend one of
// its processes died.
type scriptedVendor struct {
	*agenthub.MockAdapter
	typ    agenthub.AgentType
	opens  atomic.Int32
	loads  atomic.Int32
	forks  atomic.Int32
	mu     sync.Mutex
	onOpen func()
	// onPrompt runs as each prompt starts, on any process of this vendor: a test's
	// agent that writes files during its turn.
	onPrompt func()
	rts      []*mortal
}

func (v *scriptedVendor) Type() agenthub.AgentType { return v.typ }

func (v *scriptedVendor) start(rt agenthub.SessionRuntime) agenthub.SessionRuntime {
	m := &mortal{SessionRuntime: rt, vendor: v}
	v.mu.Lock()
	v.rts = append(v.rts, m)
	v.mu.Unlock()
	return m
}

// kill makes the newest process report that it exited.
func (v *scriptedVendor) kill() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.rts[len(v.rts)-1].dead.Store(true)
}

func (v *scriptedVendor) Open(ctx context.Context, spec agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	v.mu.Lock()
	hook := v.onOpen
	v.mu.Unlock()
	if hook != nil {
		hook()
	}
	v.opens.Add(1)
	rt, err := v.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return v.start(rt), nil
}

func (v *scriptedVendor) Load(ctx context.Context, spec agenthub.OpenSpec, _ string) (agenthub.SessionRuntime, error) {
	v.mu.Lock()
	hook := v.onOpen
	v.mu.Unlock()
	if hook != nil {
		hook()
	}
	v.loads.Add(1)
	rt, err := v.MockAdapter.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	return v.start(rt), nil
}

// mortal is a scripted vendor process. It reports that it exited (Agent Hub's
// optional Dead()), and answers each prompt with an assistant message carrying a
// vendor message id, which is what a rewind forks at.
type mortal struct {
	agenthub.SessionRuntime
	dead     atomic.Bool
	handlers agenthub.RuntimeHandlers
	replies  int
	vendor   *scriptedVendor
}

func (m *mortal) Dead() (bool, error) { return m.dead.Load(), nil }

func (m *mortal) Unwrap() agenthub.SessionRuntime { return m.SessionRuntime }

func (m *mortal) SetHandlers(h agenthub.RuntimeHandlers) {
	m.handlers = h
	m.SessionRuntime.SetHandlers(h)
}

func (m *mortal) Prompt(ctx context.Context, prompt []acp.ContentBlock, meta json.RawMessage) (agenthub.PromptOutcome, error) {
	m.replies++
	if m.vendor != nil {
		m.vendor.mu.Lock()
		hook := m.vendor.onPrompt
		m.vendor.mu.Unlock()
		if hook != nil {
			hook()
		}
	}
	if m.handlers.OnUpdate != nil {
		update := fmt.Sprintf(`{"sessionUpdate":"agent_message_chunk","messageId":"msg_%s_%d","content":{"type":"text","text":"reply"}}`, m.ACPSessionID(), m.replies)
		m.handlers.OnUpdate(json.RawMessage(update), nil)
	}
	return m.SessionRuntime.Prompt(ctx, prompt, meta)
}

// The optional interfaces are forwarded by hand: embedding an interface does not
// carry the methods the concrete runtime has beyond it.
func (m *mortal) DeleteSession(ctx context.Context, id string) error {
	if d, ok := m.SessionRuntime.(agenthub.VendorSessionDeleter); ok {
		return d.DeleteSession(ctx, id)
	}
	return nil
}

func (m *mortal) Delete(ctx context.Context) error {
	if d, ok := m.SessionRuntime.(agenthub.RuntimeDeleter); ok {
		return d.Delete(ctx)
	}
	return nil
}

func (m *mortal) SetMode(ctx context.Context, mode string) error {
	if c, ok := m.SessionRuntime.(agenthub.RuntimeConfigurator); ok {
		return c.SetMode(ctx, mode)
	}
	return nil
}

func (m *mortal) SetConfigOption(ctx context.Context, id, value string) error {
	if c, ok := m.SessionRuntime.(agenthub.RuntimeConfigurator); ok {
		return c.SetConfigOption(ctx, id, value)
	}
	return nil
}

// Fork branches the conversation at a vendor message id, as a rewind does.
func (v *scriptedVendor) Fork(ctx context.Context, spec agenthub.OpenSpec, src, messageID string) (agenthub.SessionRuntime, error) {
	v.mu.Lock()
	hook := v.onOpen
	v.mu.Unlock()
	if hook != nil {
		hook()
	}
	v.forks.Add(1)
	rt, err := v.MockAdapter.Fork(ctx, spec, src, messageID)
	if err != nil {
		return nil, err
	}
	return v.start(rt), nil
}

type usageLog struct {
	mu  sync.Mutex
	obs []usagemetrics.Observation
}

func (u *usageLog) record(o usagemetrics.Observation) {
	u.mu.Lock()
	u.obs = append(u.obs, o)
	u.mu.Unlock()
}
func (u *usageLog) all() []usagemetrics.Observation {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]usagemetrics.Observation(nil), u.obs...)
}

type compRig struct {
	// memCreates counts workspace.memory.create handlers that ran (liveStatus rigs only).
	memCreates atomic.Int32
	t          *testing.T
	ctx        context.Context
	db         *sql.DB
	prefs      *preferences.Service
	ws         *workspace.Service
	wsID       string
	root       string
	hub        *interaction.Hub
	prompter   *completingPrompter
	gate       *compGate
	server     *mcp.Server
	host       *mcp.SessionHost
	agents     *agenthub.Service
	vendors    map[agenthub.AgentType]*scriptedVendor
	asm        *HarnessAssembly
	port       *HarnessPort
	dbEnv      atomic.Value // string: the env tag of the stub database profile
	book       *agenthub.SQLAccountStore
	home       string
	sockDir    string
	metrics    *usagemetrics.Collector
	usage      *usageLog
	events     []harnessconfig.Event
	evMu       sync.Mutex
	opt        compOptions
	work       *workRig
}

type compOptions struct {
	grants     []permissions.Grant // nil: the application's default grants
	noAttach   bool                // build the graph but leave Agent Hub legacy
	extraTypes []agenthub.AgentType
	// accounts adds a second signed-in Claude account ("work") and turns account
	// switching on at 90%, as the Accounts settings would.
	accounts bool
	// noPDF leaves the optional PDF module unwired, so its tools are not registered.
	noPDF bool
	// liveStatus answers the MCP server's session lookups (GetStatus) from the real Agent Hub, for
	// tools that scope a call by its session; every other hub method stays the contract stub.
	liveStatus bool
	// realEnvs wires the application's real environment-tag policy and a database
	// stub whose profile env is compRig.dbEnv.
	realEnvs bool
	// serverMode refuses agent sessions the way the server-mode adapters do.
	serverMode bool
	// controls registers the public harness.profile.* tools through the startup
	// port and composes the graph with ComposeHarness.
	controls bool
	// wrapStore puts a fault-injecting store in front of the SQL HarnessStore.
	wrapStore func(harnessPersistence) harnessPersistence
	// work wires the real Tasks and Git services (workRig) behind the MCP tools in
	// place of the exposure stubs.
	work bool
	// wrapGitRepos puts a hook in front of the Git module the MCP tools open
	// repositories through (needs work).
	wrapGitRepos func(mcp.GitRepos) mcp.GitRepos
	// realAdapters registers these vendor adapters, and only these, in place of
	// the scripted ones (R2 real-runtime evidence). proxyPath is the stdio proxy
	// the session host hands the vendors ("" keeps the inert /usr/bin/true) and
	// resolvePermission answers the vendors' own permission requests.
	realAdapters      map[agenthub.AgentType]agenthub.VendorAdapter
	proxyPath         string
	resolvePermission agenthub.PermissionResolver
	// resolveGate wires the production confirmation route: the hub sends an
	// agent's confirmation through Agent Hub's ConfirmTool, which asks this
	// resolver (main.go: hub.ConfirmAgent = agentSvc.ConfirmTool). It is also what
	// makes the gate-owned tools delegable to a vendor.
	resolveGate agenthub.PermissionResolver
	// auditSink receives the proposal outcomes through HarnessAuditSink, the same
	// adapter main.go gives the assembly.
	auditSink audit.Sink
}

func newCompRig(t *testing.T, opt compOptions) *compRig {
	t.Helper()
	r := &compRig{t: t, usage: &usageLog{}, opt: opt}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.ctx = ctx

	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	r.db = db
	r.prefs = preferences.NewService(preferences.NewSQLRepository(db))
	r.ws = workspace.NewService(workspace.NewSQLRepository(db))
	r.root = t.TempDir()
	r.home = t.TempDir()
	w, _, err := r.ws.AddWorkspace("one", r.root)
	if err != nil {
		t.Fatal(err)
	}
	r.wsID = w.ID

	container := NewContainer()
	policy := container.Permissions
	if opt.grants != nil {
		policy = permissions.NewPolicy(opt.grants)
	}
	r.gate = &compGate{gw: capabilities.NewGateway(policy, audit.NewMemorySink())}

	r.prompter = &completingPrompter{answer: "allow"}
	r.hub = interaction.NewHub(r.prompter)
	r.prompter.hub = r.hub
	r.build()
	return r
}

// build constructs everything that a process start constructs: the vendors, Agent
// Hub, the MCP server and host, and the harness assembly. The database,
// preferences, workspaces, gateway and hub are the application's and survive it.
func (r *compRig) build() {
	t := r.t
	t.Helper()
	r.vendors = map[agenthub.AgentType]*scriptedVendor{}
	reg := agenthub.NewRegistry()
	types := append([]agenthub.AgentType{agenthub.AgentTypeClaude, agenthub.AgentTypeOpenCode}, r.opt.extraTypes...)
	if r.opt.realAdapters != nil {
		types = nil
		for _, a := range r.opt.realAdapters {
			reg.Register(a)
		}
	}
	for _, typ := range types {
		v := &scriptedVendor{MockAdapter: agenthub.NewMockAdapter(), typ: typ}
		r.vendors[typ] = v
		if r.opt.serverMode {
			// The same wrapper main.go applies: the types stay, the sessions refuse.
			reg.Register(agenthub.ForServerMode(v)[0])
			continue
		}
		reg.Register(v)
	}
	r.agents = agenthub.NewService(reg, agenthub.NewSQLStore(r.db), func(string, json.RawMessage) {})
	r.agents.MatchingWorkspaces = r.ws.MatchingWorkspaceIDs
	if r.opt.resolvePermission != nil {
		r.agents.ResolvePermission = r.opt.resolvePermission
	}
	if r.opt.resolveGate != nil {
		r.agents.ResolveGatePermission = r.opt.resolveGate
		r.hub.ConfirmAgent = r.agents.ConfirmTool
	}
	r.agents.SandboxExtras = func() preferences.AgentSandboxSettings {
		s, _ := r.prefs.AgentSandbox()
		return s
	}
	if r.opt.accounts {
		r.book = agenthub.NewSQLAccountStore(r.db)
		r.agents.Accounts = r.book
		r.agents.AccountSharedHome = func(agenthub.AgentType) string { return t.TempDir() }
		r.agents.AccountVendors = []agenthub.AgentType{agenthub.AgentTypeClaude}
		r.agents.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
			return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
				string(agenthub.AgentTypeClaude): {
					Enabled: true, ThresholdPercent: 90, UsageMaxAgeSeconds: 300, OnLimitRefusal: preferences.RefusalNone,
					Order: []string{agenthub.BuiltinAccountID(agenthub.AgentTypeClaude), "work"},
				},
			}}, nil
		}
		if err := r.book.Put(r.ctx, agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeClaude, Label: "Work", HomeDir: r.home, SignedIn: true}); err != nil {
			t.Fatal(err)
		}
	}

	// The real collector over the real tables, as main.go builds it.
	store := usagemetrics.NewSQLStore(r.db)
	collector, err := usagemetrics.New(r.ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	r.metrics = collector
	r.agents.ToolMetrics = store

	deps := exposureDeps(r.gate)
	if r.opt.realEnvs {
		// The real environment-tag policy and a database whose profile carries the
		// tag a test names, so the confirmation rule is the application's own.
		deps.Envs = NewContainer().Environments
		deps.DB = dbWithEnv{rig: r}
	}
	if r.opt.liveStatus {
		deps.AgentHub = hubStatus{AgentHubAPI: deps.AgentHub, svc: r.agents}
		deps.WorkspaceMemory = memoryProbe{WorkspaceMemoryAPI: deps.WorkspaceMemory, n: &r.memCreates}
	}
	deps.Vault = vaultOK{}
	deps.Hub = r.hub
	if r.opt.work {
		r.wireWork(&deps)
	}
	deps.UsageScope = r.agents.ToolUsageScope
	deps.RecordUsage = func(o usagemetrics.Observation) { r.usage.record(o); collector.Record(o) }
	deps.BeginUsage = collector.Begin
	deps.EndUsage = collector.End
	deps.DropUsage = collector.Drop
	if r.opt.noPDF {
		deps.PDFDocuments = nil // the optional module is not wired
	}
	r.port = nil
	if r.opt.controls {
		// Created before the server, bound after the graph is built from the
		// server's own inventory: the order production has to follow.
		r.port = NewHarnessPort()
		deps.Harness = r.port
	}
	r.server = mcp.NewServer(deps)

	dir, err := os.MkdirTemp("/tmp", "h4d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	r.sockDir = dir
	proxy := "/usr/bin/true"
	if r.opt.proxyPath != "" {
		proxy = r.opt.proxyPath
	}
	r.host = mcp.NewSessionHost(r.ctx, r.server, dir, proxy)
	t.Cleanup(r.host.Close)
	r.agents.MCPEndpoints = r.host

	var hstore harnessPersistence = storage.NewHarnessStore(r.db)
	if r.opt.wrapStore != nil {
		hstore = r.opt.wrapStore(hstore)
	}
	cfg := HarnessAssemblyConfig{
		Store: hstore, Preferences: r.prefs, Hub: r.hub, Workspaces: r.ws, Agents: r.agents, Vendors: productionVendors,
		Inventory: r.inventory,
		Audit:     func(e harnessconfig.Event) { r.evMu.Lock(); r.events = append(r.events, e); r.evMu.Unlock() },
	}
	if r.opt.auditSink != nil {
		record, sink := cfg.Audit, HarnessAuditSink(r.opt.auditSink)
		cfg.Audit = func(e harnessconfig.Event) { record(e); sink(e) }
	}
	if r.opt.controls {
		if r.asm, err = ComposeHarness(r.port, cfg, r.agents); err != nil {
			t.Fatal(err)
		}
		return
	}
	r.asm, err = NewHarnessAssembly(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !r.opt.noAttach {
		if err := r.asm.Attach(r.agents); err != nil {
			t.Fatal(err)
		}
	}
}

// restart is a process restart: every in-memory owner is rebuilt over the same
// database and preferences, so only what is durable survives.
func (r *compRig) restart() {
	r.t.Helper()
	r.host.Close()
	r.drain()
	r.build()
}

// drain lands the collector's buffered observations, as main.go's shutdown does.
func (r *compRig) drain() {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.metrics.Shutdown(ctx); err != nil {
		r.t.Fatalf("usage collection did not drain: %v", err)
	}
}

// usageRows is the whole tool-usage table, ordered, for before/after comparison.
func (r *compRig) usageRows() []string {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.metrics.Flush(ctx); err != nil {
		r.t.Fatal(err)
	}
	rows, err := r.db.Query(`SELECT day,vendor,tool,profile_key,override_key,calls,tool_errors,before_bytes FROM agent_tool_usage_daily ORDER BY tool,profile_key,override_key`)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var day, vendor, tool, pk, ok string
		var calls, errs, before int64
		if err := rows.Scan(&day, &vendor, &tool, &pk, &ok, &calls, &errs, &before); err != nil {
			r.t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s|%s|%s|%s|%s|calls=%d|errs=%d|before=%d", day, vendor, tool, pk, ok, calls, errs, before))
	}
	return out
}

// turnFacts is the override revision each stored turn started with.
func (r *compRig) turnFacts(sessionID string) []string {
	r.t.Helper()
	rows, err := r.db.Query(`SELECT id, COALESCE(CAST(harness_override_revision AS TEXT),'NULL') FROM agent_turns WHERE session_id=? ORDER BY rowid`, sessionID)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, rev string
		if err := rows.Scan(&id, &rev); err != nil {
			r.t.Fatal(err)
		}
		out = append(out, id+"="+rev)
	}
	return out
}

func (r *compRig) inventory() (harness.Registry, error) {
	inv, err := r.server.Inventory()
	if err != nil {
		return harness.Registry{}, err
	}
	return inv.Registry(), nil
}

func (r *compRig) ui() context.Context {
	return identity.WithCaller(r.ctx, identity.UI())
}

func (r *compRig) agentCtx(sessionID string) context.Context {
	return identity.WithCaller(r.ctx, identity.Agent(sessionID))
}

func (r *compRig) create(typ agenthub.AgentType) (agenthub.AgentSession, agenthub.OpenSpec) {
	r.t.Helper()
	sess, err := r.agents.CreateSession(r.ui(), agenthub.CreateInput{AgentType: typ, Cwd: r.root, WorkspaceID: r.wsID})
	if err != nil {
		r.t.Fatal(err)
	}
	return sess, r.vendors[typ].LastSpec()
}

func (r *compRig) connect(spec agenthub.OpenSpec) *sdk.ClientSession {
	r.t.Helper()
	conn, err := net.Dial("unix", spec.AllowUnixSockets[0])
	if err != nil {
		r.t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(r.ctx, &sdk.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { cs.Close() })
	return cs
}

func (r *compRig) listed(cs *sdk.ClientSession) []string {
	r.t.Helper()
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			r.t.Fatalf("list tools: %v", err)
		}
		names = append(names, tool.Name)
	}
	return names
}

// dbWithEnv is the exposure stub database with a profile of the rig's chosen env.
type dbWithEnv struct {
	stubDB
	rig *compRig
}

func (d dbWithEnv) env() string {
	if v, ok := d.rig.dbEnv.Load().(string); ok {
		return v
	}
	return "development"
}

func (d dbWithEnv) ListProfiles() ([]database.ProfileSummary, error) {
	return []database.ProfileSummary{{ID: probeProfileID, Env: d.env()}}, nil
}

func (dbWithEnv) TestConnection(context.Context, string) error { return nil }

func (r *compRig) call(cs *sdk.ClientSession, tool string) string {
	return r.callWith(cs, tool, map[string]any{})
}

func (r *compRig) callWith(cs *sdk.ClientSession, tool string, args map[string]any) string {
	r.t.Helper()
	res, err := cs.CallTool(r.ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		r.t.Fatalf("call %s: %v", tool, err)
	}
	var out string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			out += tc.Text
		}
	}
	return out
}

// workspaceOff stores a workspace profile with the groups off.
func (r *compRig) workspaceOff(groups ...string) harness.Stamp {
	r.t.Helper()
	off := map[string]bool{}
	for _, g := range groups {
		off[g] = false
	}
	_, st, err := r.asm.Store.GetProfile(context.Background(), r.wsID)
	if err != nil {
		r.t.Fatal(err)
	}
	a, err := r.asm.Store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: r.wsID,
		Profile: &harness.ProfileWrite{Expect: st, Value: &harness.Profile{Schema: harness.SchemaVersion, ToolGroups: off}}})
	if err != nil {
		r.t.Fatal(err)
	}
	return a.Profile
}

func (r *compRig) override(sessionID string) (*harness.Override, harness.Stamp) {
	r.t.Helper()
	o, st, err := r.asm.Store.GetOverride(context.Background(), sessionID, r.wsID)
	if err != nil {
		r.t.Fatal(err)
	}
	return o, st
}

func (r *compRig) propose(sessionID string, enable, revoke []string) (harnessconfig.Prepared, error) {
	return r.asm.Service.Prepare(r.agentCtx(sessionID), harnessconfig.Request{
		Target: harnessconfig.TargetSession, Patch: harnessconfig.Patch{EnableGroups: enable, RevokeGroups: revoke}})
}

func (r *compRig) apply(sessionID string, enable, revoke []string) (harnessconfig.Result, error) {
	r.t.Helper()
	p, err := r.propose(sessionID, enable, revoke)
	if err != nil {
		r.t.Fatalf("Prepare: %v", err)
	}
	ctx, cancel := context.WithTimeout(r.agentCtx(sessionID), 20*time.Second)
	defer cancel()
	return r.asm.Service.Apply(ctx, p.ID)
}

func hasGroup(names []string, group string) bool {
	for _, n := range names {
		if harness.Group(n) == group {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// hubStatus is the contract stub with the one lookup a session-scoped tool needs.
type hubStatus struct {
	mcp.AgentHubAPI
	svc *agenthub.Service
}

func (h hubStatus) GetStatus(ctx context.Context, id string) (agenthub.AgentSession, error) {
	return h.svc.GetStatus(ctx, id)
}

// memoryProbe records that a create reached the memory service and stores nothing.
type memoryProbe struct {
	mcp.WorkspaceMemoryAPI
	n *atomic.Int32
}

func (m memoryProbe) Create(context.Context, string, workspacememory.Input, workspacememory.Source) (workspacememory.Entry, error) {
	m.n.Add(1)
	return workspacememory.Entry{}, nil
}
