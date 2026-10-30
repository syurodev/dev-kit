package harnessconfig

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"devkit/internal/harness"
	"devkit/internal/identity"
	"devkit/internal/storage"
)

type fakeDirectory struct {
	mu         sync.Mutex
	sessions   map[string]SessionScope
	workspaces map[string]bool
}

func (d *fakeDirectory) SessionScope(_ context.Context, id string) (SessionScope, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if sc, ok := d.sessions[id]; ok {
		return sc, nil
	}
	return SessionScope{}, errors.New("gone")
}

func (d *fakeDirectory) WorkspaceExists(_ context.Context, id string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.workspaces[id], nil
}

type fakeVendors struct {
	mu     sync.Mutex
	states map[string]VendorState
	err    error
}

func (v *fakeVendors) Names() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var n []string
	for k := range v.states {
		n = append(n, k)
	}
	return n
}

func (v *fakeVendors) State(_ context.Context, name string) (VendorState, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.err != nil {
		return VendorState{}, v.err
	}
	return v.states[name], nil
}

func (v *fakeVendors) set(name string, f func(*VendorState)) {
	v.mu.Lock()
	defer v.mu.Unlock()
	s := v.states[name]
	f(&s)
	v.states[name] = s
}

type fakePolicy struct {
	mu sync.Mutex
	p  Policy
}

func (f *fakePolicy) Policy(context.Context) (Policy, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.p, nil
}

type fakeValidator struct{ err error }

func (v fakeValidator) Validate(context.Context, harness.Profile) error { return v.err }

type env struct {
	t         *testing.T
	svc       *Service
	store     *storage.HarnessStore
	db        *sql.DB
	dir       *fakeDirectory
	vendors   *fakeVendors
	policy    *fakePolicy
	validator *fakeValidator
	canonical *canonicalMap
}

// canonicalMap is a symlink table the test can retarget.
type canonicalMap struct {
	mu sync.Mutex
	m  map[string]string
}

func (c *canonicalMap) resolve(p string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.m[p]; ok {
		return r, nil
	}
	return p, nil
}

func registry() harness.Registry {
	return harness.Registry{Tools: []harness.Tool{
		{Name: "git.status", DefinitionBytes: 900}, {Name: "git.commit", DefinitionBytes: 1100},
		{Name: "tasks.list", DefinitionBytes: 800},
		{Name: "codegraph.searchSymbol", DefinitionBytes: 2000}, {Name: "codegraph.readFile", DefinitionBytes: 700, BudgetExempt: true},
		{Name: "harness.profile.get", DefinitionBytes: 500}, {Name: harness.ProposeTool, DefinitionBytes: 600},
	}}
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,root) VALUES('w1','one','/one'),('w2','two','/two')`,
		`INSERT INTO agent_sessions(id,agent_type,project_path,status,depth,created_at,updated_at,workspace_id) VALUES
 ('s1','claude','/one','idle',0,'x','x','w1'),('s2','codex','/two','idle',0,'x','x','w2')`,
		// s1b is a child of s1: a delegated grant is fenced on the grantor being the open direct parent.
		`INSERT INTO agent_sessions(id,agent_type,project_path,status,depth,created_at,updated_at,workspace_id,parent_session_id) VALUES('s1b','claude','/one','idle',1,'x','x','w1','s1')`,
	} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	e := &env{t: t, db: db, store: storage.NewHarnessStore(db),
		dir: &fakeDirectory{
			sessions:   map[string]SessionScope{"s1": {"w1", "claude"}, "s1b": {"w1", "claude"}, "s2": {"w2", "codex"}},
			workspaces: map[string]bool{"w1": true, "w2": true},
		},
		vendors: &fakeVendors{states: map[string]VendorState{
			"claude": {Vendor: harness.Vendor{Revision: 4}, FactsComplete: true},
			"codex":  {Vendor: harness.Vendor{Revision: 2}, FactsComplete: true},
		}},
		policy:    &fakePolicy{p: Policy{Revision: "p1"}},
		validator: &fakeValidator{},
		canonical: &canonicalMap{m: map[string]string{}},
	}
	e.svc = New(Config{Store: e.store, Directory: e.dir, Vendors: e.vendors, Policy: e.policy, Validator: e.validator,
		Registry: registry, Canonical: e.canonical.resolve})
	return e
}

func agent(session string) context.Context {
	return identity.WithCaller(context.Background(), identity.Agent(session))
}

func ui() context.Context { return identity.WithCaller(context.Background(), identity.UI()) }

func boolp(b bool) *bool { return &b }

func (e *env) seedProfile(ws string, f func(*harness.Profile)) harness.Stamp {
	e.t.Helper()
	_, st, err := e.store.GetProfile(context.Background(), ws)
	if err != nil {
		e.t.Fatal(err)
	}
	p := &harness.Profile{Schema: harness.SchemaVersion}
	f(p)
	a, err := e.store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: ws, Profile: &harness.ProfileWrite{Expect: st, Value: p}})
	if err != nil {
		e.t.Fatal(err)
	}
	return a.Profile
}

func (e *env) seedOverride(ws, session string, groups ...string) harness.Stamp {
	e.t.Helper()
	_, st, err := e.store.GetOverride(context.Background(), session, ws)
	if err != nil {
		e.t.Fatal(err)
	}
	a, err := e.store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: ws, SessionID: session,
		Override: &harness.OverrideWrite{Expect: st, Value: &harness.Override{Schema: harness.SchemaVersion, Groups: groups}}})
	if err != nil {
		e.t.Fatal(err)
	}
	return a.Override
}

func (e *env) profile(ws string) (*harness.Profile, harness.Stamp) {
	e.t.Helper()
	p, st, err := e.store.GetProfile(context.Background(), ws)
	if err != nil {
		e.t.Fatal(err)
	}
	return p, st
}

func (e *env) override(ws, session string) (*harness.Override, harness.Stamp) {
	e.t.Helper()
	o, st, err := e.store.GetOverride(context.Background(), session, ws)
	if err != nil {
		e.t.Fatal(err)
	}
	return o, st
}

func code(t *testing.T, err error) string {
	t.Helper()
	var he *harness.Error
	if !errors.As(err, &he) {
		t.Fatalf("want *harness.Error, got %v", err)
	}
	return he.Code
}

func stdio(name, cmd string) harness.Server {
	return harness.Server{Name: name, Command: cmd, Enabled: true}
}

func enableGit() Request {
	return Request{Target: TargetSession, Patch: Patch{EnableGroups: []string{"git"}}}
}

func (e *env) storeDB() *sql.DB { return e.db }
