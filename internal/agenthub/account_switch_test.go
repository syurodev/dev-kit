package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
	"devkit/internal/preferences"
	"devkit/internal/storage"
)

type accountRig struct {
	svc     *agenthub.Service
	mock    *agenthub.MockAdapter
	book    *agenthub.SQLAccountStore
	home    string // the "work" account's home
	mu      sync.Mutex
	emitted []json.RawMessage
	opens   *atomic.Int32 // Open and Load calls of the vendor
	vendor  *vendorHook   // what the vendor's Open and Load do besides opening
	reg     *agenthub.Registry
	store   agenthub.Store
	refusal string
	signIn  *signInFake // what the vendor does to sign in and report its status (nil: nothing)
}

// rigOptions changes what newAccountRigWith builds; the zero value is newAccountRig.
type rigOptions struct {
	// store wraps the SQL store the service uses (to make one call fail).
	store func(agenthub.Store) agenthub.Store
	// noWork leaves the "work" account out: the vendor has only its built-in one.
	noWork bool
	// probe makes the vendor answer usage checks with probe.answer; nil leaves
	// it unable to ("not checkable"), as before.
	probe *usageProbe
	// signIn makes the vendor sign in and report its status through the fake; nil
	// leaves it unable to ("auth method unknown"), as a vendor with no such support.
	signIn *signInFake
}

// countedMock is resumableMock counting every process it opens, and doing
// what its vendorHook says before each.
type countedMock struct {
	resumableMock
	opens  *atomic.Int32
	hook   *vendorHook
	probe  *usageProbe
	signIn *signInFake
}

// usageProbe is what a rig's vendor answers to CheckUsage, and what it was asked.
type usageProbe struct {
	calls  atomic.Int32
	mu     sync.Mutex
	specs  []agenthub.OpenSpec
	answer func(ctx context.Context, spec agenthub.OpenSpec) ([]agenthub.LimitWindow, error)
}

func (p *usageProbe) check(ctx context.Context, spec agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
	p.calls.Add(1)
	p.mu.Lock()
	p.specs = append(p.specs, spec)
	p.mu.Unlock()
	return p.answer(ctx, spec)
}

// signInFake is what a rig's vendor does to sign in and to report a home's status, and
// what it was asked.
type signInFake struct {
	mu             sync.Mutex
	state          agenthub.SignInState
	checkErr       error
	home           string // home the terminal command was asked for
	startedHome    string
	startedAccount string
	startedSpec    agenthub.OpenSpec
	checkedHome    string
	checkedSpec    agenthub.OpenSpec
	checks         int
}

func (f *signInFake) commandFor(methodID, home string) (agenthub.SignInCommand, error) {
	if methodID != "login-method" {
		return agenthub.SignInCommand{}, agenthub.ErrAuthMethodUnknown
	}
	f.mu.Lock()
	f.home = home
	f.mu.Unlock()
	return agenthub.SignInCommand{Path: "/bin/true", Label: "fake login", Release: func() {}}, nil
}

func (f *signInFake) start(spec agenthub.OpenSpec, accountID string) (agenthub.SignInFlow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startedHome, f.startedAccount, f.startedSpec = spec.AccountHome, accountID, spec
	return agenthub.SignInFlow{LoginID: "login-1"}, nil
}

func (f *signInFake) check(spec agenthub.OpenSpec) (agenthub.SignInState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	f.checkedHome, f.checkedSpec = spec.AccountHome, spec
	return f.state, f.checkErr
}

func (c countedMock) SignInCommandFor(_ context.Context, methodID, home string) (agenthub.SignInCommand, error) {
	if c.signIn == nil {
		return agenthub.SignInCommand{}, agenthub.ErrAuthMethodUnknown
	}
	return c.signIn.commandFor(methodID, home)
}

func (c countedMock) StartBrowserSignInAt(_ context.Context, spec agenthub.OpenSpec, accountID string) (agenthub.SignInFlow, error) {
	if c.signIn == nil {
		return agenthub.SignInFlow{}, agenthub.ErrAuthMethodUnknown
	}
	return c.signIn.start(spec, accountID)
}

func (c countedMock) StartDeviceSignInAt(_ context.Context, spec agenthub.OpenSpec, accountID string) (agenthub.SignInFlow, error) {
	if c.signIn == nil {
		return agenthub.SignInFlow{}, agenthub.ErrAuthMethodUnknown
	}
	return c.signIn.start(spec, accountID)
}

func (c countedMock) CheckSignIn(_ context.Context, spec agenthub.OpenSpec) (agenthub.SignInState, error) {
	if c.signIn == nil {
		return agenthub.SignInState{}, agenthub.ErrAuthMethodUnknown
	}
	return c.signIn.check(spec)
}

func (c countedMock) CheckUsage(ctx context.Context, spec agenthub.OpenSpec) ([]agenthub.LimitWindow, error) {
	if c.probe == nil {
		return nil, agenthub.ErrUsageNotCheckable
	}
	return c.probe.check(ctx, spec)
}

func (c countedMock) Open(ctx context.Context, spec agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	c.opens.Add(1)
	if err := c.hook.enter(ctx, spec); err != nil {
		return nil, err
	}
	return c.hook.opened(c.resumableMock.Open(ctx, spec))(spec)
}

func (c countedMock) Load(ctx context.Context, spec agenthub.OpenSpec, id string) (agenthub.SessionRuntime, error) {
	c.opens.Add(1)
	if err := c.hook.enter(ctx, spec); err != nil {
		return nil, err
	}
	return c.hook.opened(c.resumableMock.Load(ctx, spec, id))(spec)
}

// errAccountHomeBroken is what the vendor answers when vendorHook.failHome is set.
var errAccountHomeBroken = errors.New("the account's sign-in has expired")

// vendorHook lets a test hold an open, refuse the ones in an account home, and
// read back which home each runtime was opened in.
type vendorHook struct {
	homeOpens atomic.Int32 // opens (attempted) in an added account's home
	failHome  atomic.Bool  // refuse every open in an added account's home
	ctxEnds   atomic.Bool  // an open in an added account's home answers its context's error once that has ended

	mu      sync.Mutex
	block   chan struct{} // the next open waits for it to close
	started chan struct{} // closed when that open has begun
	homes   []runtimeHome // every runtime opened, in order
}

type runtimeHome struct {
	rt   agenthub.SessionRuntime
	home string
}

// holdNextOpen makes the next Open or Load wait until release is called;
// started is closed once that open is waiting.
func (h *vendorHook) holdNextOpen() (started <-chan struct{}, release func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.block, h.started = make(chan struct{}), make(chan struct{})
	block := h.block
	var once sync.Once
	return h.started, func() { once.Do(func() { close(block) }) }
}

func (h *vendorHook) enter(ctx context.Context, spec agenthub.OpenSpec) error {
	if spec.AccountHome != "" {
		h.homeOpens.Add(1)
		if h.failHome.Load() {
			return errAccountHomeBroken
		}
	}
	h.mu.Lock()
	block, started := h.block, h.started
	h.block, h.started = nil, nil
	h.mu.Unlock()
	if block != nil {
		close(started)
		<-block
	}
	if spec.AccountHome != "" && h.ctxEnds.Load() && ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func (h *vendorHook) opened(rt agenthub.SessionRuntime, err error) func(agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
	return func(spec agenthub.OpenSpec) (agenthub.SessionRuntime, error) {
		if err == nil {
			h.mu.Lock()
			h.homes = append(h.homes, runtimeHome{rt, spec.AccountHome})
			h.mu.Unlock()
		}
		return rt, err
	}
}

// promptedHomes is the home of every runtime that was sent a prompt, in the
// order they were opened.
func (h *vendorHook) promptedHomes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.homes {
		if p, ok := r.rt.(interface{ Prompts() [][]acp.ContentBlock }); ok && len(p.Prompts()) > 0 {
			out = append(out, r.home)
		}
	}
	return out
}

// updates returns what the service emitted so far (used by the switching tests of later tasks).
func (r *accountRig) updates() []json.RawMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]json.RawMessage(nil), r.emitted...)
}

// newAccountRig: a mock-vendor service with its built-in account and one added
// account "work", switching enabled at 90%.
func newAccountRig(t *testing.T, refusal string) *accountRig {
	t.Helper()
	return newAccountRigWith(t, refusal, rigOptions{})
}

func newAccountRigWith(t *testing.T, refusal string, opts rigOptions) *accountRig {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	rig := &accountRig{
		mock: agenthub.NewMockAdapter(), book: agenthub.NewSQLAccountStore(db), home: t.TempDir(),
		opens: new(atomic.Int32), vendor: new(vendorHook), reg: agenthub.NewRegistry(), refusal: refusal,
	}
	// reload needs a vendor that can reopen a conversation
	rig.signIn = opts.signIn
	rig.reg.Register(countedMock{resumableMock{rig.mock}, rig.opens, rig.vendor, opts.probe, opts.signIn})
	rig.store = agenthub.NewSQLStore(db)
	if opts.store != nil {
		rig.store = opts.store(rig.store)
	}
	rig.svc = rig.newService(t)
	if opts.noWork {
		return rig
	}
	ctx := context.Background()
	if err := rig.book.Put(ctx, agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeMock, Label: "Work", HomeDir: rig.home, SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	return rig
}

// newService builds the rig's service over its registry and store, as the app
// does at start.
func (r *accountRig) newService(t *testing.T) *agenthub.Service {
	t.Helper()
	svc := agenthub.NewService(r.reg, r.store, func(_ string, u json.RawMessage) {
		r.mu.Lock()
		r.emitted = append(r.emitted, u)
		r.mu.Unlock()
	})
	svc.Accounts = r.book
	svc.AccountSharedHome = func(agenthub.AgentType) string { return t.TempDir() }
	svc.AccountPolicy = func() (preferences.AgentAccountPolicy, error) {
		return preferences.AgentAccountPolicy{Vendors: map[string]preferences.AccountVendorPolicy{
			string(agenthub.AgentTypeMock): {
				Enabled: true, ThresholdPercent: 90, UsageMaxAgeSeconds: 300, OnLimitRefusal: r.refusal,
				Order: []string{agenthub.BuiltinAccountID(agenthub.AgentTypeMock), "work"},
			},
		}}, nil
	}
	return svc
}

// restart replaces the rig's service with a new one over the same store, the
// way the app restarts: every session is left without a process.
func (r *accountRig) restart(t *testing.T) {
	t.Helper()
	r.svc = r.newService(t)
}

// fillBuiltin reports the built-in account at pct in its weekly window (used by the switching tests of later tasks).
func (r *accountRig) fillBuiltin(t *testing.T, pct float64) {
	t.Helper()
	w := agenthub.LimitWindow{Label: "Weekly", UsedPercent: pct, ResetsAt: time.Now().Add(24 * time.Hour).Unix()}
	if _, err := r.book.List(context.Background(), agenthub.AgentTypeMock); err != nil { // creates the built-in row
		t.Fatal(err)
	}
	if err := r.book.SaveUsage(context.Background(), agenthub.BuiltinAccountID(agenthub.AgentTypeMock), []agenthub.LimitWindow{w}, "observed", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestResumedSessionOpensInItsAccountsHome(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	if rig.mock.LastSpec().AccountHome != "" {
		t.Fatalf("a new session runs as the built-in account, got home %q", rig.mock.LastSpec().AccountHome)
	}
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := rig.mock.LastSpec().AccountHome; got != rig.home {
		t.Errorf("OpenSpec.AccountHome = %q, want the account's home %q", got, rig.home)
	}
}

func TestResumedSessionOnTheBuiltinOrAMissingAccountLeavesTheSpecAlone(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string) {
		t.Helper()
		if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		spec := rig.mock.LastSpec()
		if spec.AccountHome != "" || len(spec.AccountSharedPaths) != 0 {
			t.Errorf("%s: spec carries home %q / shared %v, want the built-in account", what, spec.AccountHome, spec.AccountSharedPaths)
		}
	}
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)
	cached := func(want string) {
		t.Helper()
		if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); !ok || got != want {
			t.Errorf("cached account = %q (%v), want %q", got, ok, want)
		}
	}
	check("session on the built-in account")
	cached(builtin)
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, "gone"); err != nil {
		t.Fatal(err)
	}
	check("session whose account row is gone")
	cached(builtin) // the runtime runs as the built-in account, not as "gone"
}

func TestSessionAccountCacheSaysWhatRunsAndIsForgottenOnClose(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); !ok || got != "work" {
		t.Fatalf("cached account = %q (%v), want work", got, ok)
	}
	if err := rig.svc.CloseSession(uiCtx(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); ok {
		t.Errorf("after CloseSession the cache still holds %q", got)
	}
}

func TestDeleteSessionForgetsItsCachedAccount(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := rig.svc.SessionAccountCachedForTest(sess.ID); !ok {
		t.Fatal("an opened session should be cached")
	}
	if err := rig.svc.DeleteSession(uiCtx(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); ok {
		t.Errorf("after DeleteSession the cache still holds %q", got)
	}
}

// The environment report is a read: it must not say which account a runtime
// runs as. Only a path that opens a runtime records that.
func TestEnvironmentReportDoesNotWriteTheSessionAccountCache(t *testing.T) {
	rig := newAccountRig(t, preferences.RefusalNone)
	sess, err := rig.svc.CreateSession(uiCtx(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: projectDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rig.svc.SessionAccountCachedForTest(sess.ID); ok {
		t.Fatal("test premise: CreateSession must not record an account")
	}
	// Row on "work", no runtime opened for it: the report still describes the account.
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.Environment(uiCtx(), agenthub.AgentTypeMock, sess.ID); err != nil {
		t.Fatal(err)
	}
	if got, ok := rig.svc.SessionAccountCachedForTest(sess.ID); ok {
		t.Errorf("Environment recorded %q for a session no runtime was opened for", got)
	}

	// A runtime opened as the built-in account, then the row moves on (as a
	// concurrent move does before it records): the report must not overwrite it.
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.ResumeSession(uiCtx(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeMock)
	if got, _ := rig.svc.SessionAccountCachedForTest(sess.ID); got != builtin {
		t.Fatalf("test premise: cached account = %q, want %q", got, builtin)
	}
	if err := rig.svc.BindSessionAccountForTest(context.Background(), sess.ID, "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.svc.Environment(uiCtx(), agenthub.AgentTypeMock, sess.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := rig.svc.SessionAccountCachedForTest(sess.ID); got != builtin {
		t.Errorf("Environment changed the cached account to %q, want it left as %q", got, builtin)
	}
}
