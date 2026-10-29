package app

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"devkit/internal/harness"
	"devkit/internal/mcp"
)

// startupRig is the production construction order in miniature, built from the
// real owners: the real MCP server (given the port before its inventory freezes),
// the real SessionHost as Agent Hub's endpoints, the real assembly parts, and the
// real unix listener. Only Listen's bind goes through the same mcp functions
// main.go uses.
type startupRig struct {
	t       *testing.T
	ctx     context.Context
	parts   assemblyParts
	port    *HarnessPort
	server  *mcp.Server
	host    *mcp.SessionHost
	sock    string
	cleaned int
	listens int
	// atListen is what the world looked like at the instant Listen was called.
	atListen struct {
		bound, harness, lifecycle, sockExists bool
	}
}

func newStartupRig(t *testing.T) *startupRig {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &startupRig{t: t, ctx: ctx, parts: newAssemblyParts(t), port: NewHarnessPort()}
	// The port is passed to the server before it is built: the tools are in the
	// registry that Inventory freezes.
	r.server = mcp.NewServer(mcp.Deps{Hub: r.parts.hub, Vault: vaultOK{}, Harness: r.port})
	dir, err := os.MkdirTemp("/tmp", "h3s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	r.sock = filepath.Join(dir, "mcp.sock")
	r.host = mcp.NewSessionHost(ctx, r.server, dir, "/usr/bin/true")
	t.Cleanup(r.host.Close)
	r.parts.agents.MCPEndpoints = r.host
	return r
}

func (r *startupRig) inventory() (harness.Registry, error) {
	inv, err := r.server.Inventory()
	if err != nil {
		return harness.Registry{}, err
	}
	return inv.Registry(), nil
}

func (r *startupRig) startup() HarnessStartup {
	cfg := r.parts.config()
	cfg.Inventory = r.inventory
	return HarnessStartup{
		Port:   r.port,
		Config: cfg,
		Listen: func() error {
			r.listens++
			_, statErr := os.Stat(r.sock)
			r.atListen.bound = r.port.Bound()
			r.atListen.harness = r.parts.agents.Harness != nil
			r.atListen.lifecycle = r.parts.agents.HarnessLifecycle != nil
			r.atListen.sockExists = statErr == nil
			ln, err := mcp.ListenUnix(r.sock)
			if err != nil {
				return err
			}
			go func() { _ = mcp.ServeListener(r.ctx, ln, r.server) }()
			return nil
		},
		Cleanup: func() { r.cleaned++; r.host.Close() },
	}
}

// nothingReachable asserts the failure contract: no provider or lifecycle owner on
// Agent Hub, the port refuses, and there is no socket anyone could connect to.
func (r *startupRig) nothingReachable() {
	r.t.Helper()
	if r.parts.agents.Harness != nil || r.parts.agents.HarnessLifecycle != nil {
		r.t.Error("Agent Hub kept a harness provider or lifecycle owner after a failed startup")
	}
	if r.port.Bound() {
		r.t.Error("the port stayed bound after a failed startup")
	}
	if _, err := r.port.Profile(r.ctx); !errors.Is(err, ErrHarnessNotReady) {
		r.t.Errorf("an unbound port must refuse with the fixed sentence, got %v", err)
	}
	if _, err := os.Stat(r.sock); err == nil {
		r.t.Error("a socket exists after a failed startup")
	}
}

func TestTheHarnessIsComposedAndAttachedBeforeTheSharedListenerBinds(t *testing.T) {
	r := newStartupRig(t)
	if r.port.Bound() {
		t.Fatal("the port must start unbound")
	}
	asm, err := StartHarness(r.startup())
	if err != nil {
		t.Fatal(err)
	}
	if asm == nil || r.listens != 1 || r.cleaned != 0 {
		t.Fatalf("assembly=%v listens=%d cleaned=%d", asm, r.listens, r.cleaned)
	}
	if !r.atListen.bound || !r.atListen.harness || !r.atListen.lifecycle {
		t.Errorf("the listener was started before the graph was composed, attached and bound: %+v", r.atListen)
	}
	if r.atListen.sockExists {
		t.Error("the socket existed before Listen was called")
	}
	if !r.port.Bound() || r.parts.agents.Harness == nil || r.parts.agents.HarnessLifecycle == nil {
		t.Fatal("a successful startup must leave the graph attached and the port bound")
	}

	// The tools exist in the real inventory and are served on the real listener.
	inv, err := r.server.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range inv.Registry().Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"harness.profile.get", harness.ProposeTool} {
		if !slices.Contains(names, want) {
			t.Errorf("%s is not in the inventory the assembly resolved against", want)
		}
	}
	conn, err := net.Dial("unix", r.sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(r.ctx, &sdk.IOTransport{Reader: conn, Writer: conn}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	listed := []string{}
	for tool, err := range cs.Tools(r.ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		listed = append(listed, tool.Name)
	}
	if !slices.Contains(listed, "harness.profile.get") {
		t.Errorf("the shared listener does not list the harness tools: %v", listed)
	}
}

func TestAComposeFailureLeavesNothingReachableAndNeverBindsTheListener(t *testing.T) {
	cases := map[string]func(r *startupRig, s *HarnessStartup){
		"unreadable inventory": func(r *startupRig, s *HarnessStartup) {
			s.Config.Inventory = func() (harness.Registry, error) { return harness.Registry{}, errors.New("boom") }
		},
		"empty inventory": func(r *startupRig, s *HarnessStartup) {
			s.Config.Inventory = func() (harness.Registry, error) { return harness.Registry{}, nil }
		},
		"session host cannot enforce a policy": func(r *startupRig, s *HarnessStartup) {
			r.parts.agents.MCPEndpoints = nil
		},
		"incomplete collaborator": func(r *startupRig, s *HarnessStartup) { s.Config.Hub = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := newStartupRig(t)
			s := r.startup()
			mutate(r, &s)
			asm, err := StartHarness(s)
			if err == nil || asm != nil {
				t.Fatalf("startup must fail closed, got asm=%v err=%v", asm, err)
			}
			if r.listens != 0 {
				t.Error("the listener must not be started when composition failed")
			}
			if r.cleaned != 1 {
				t.Errorf("staged resources must be cleaned exactly once, got %d", r.cleaned)
			}
			r.nothingReachable()
		})
	}
}

func TestAListenerFailureUndoesTheCompositionAndFailsStartup(t *testing.T) {
	r := newStartupRig(t)
	// Another live owner of the socket: the bind must be refused, never stolen.
	owner, err := net.Listen("unix", r.sock)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	asm, err := StartHarness(r.startup())
	if err == nil || asm != nil {
		t.Fatalf("a listener failure must fail startup, got asm=%v err=%v", asm, err)
	}
	if r.listens != 1 {
		t.Fatalf("listens=%d", r.listens)
	}
	if !r.atListen.bound || !r.atListen.harness {
		t.Error("the listener must be tried after composition, not before")
	}
	if r.cleaned != 1 {
		t.Errorf("staged resources must be cleaned exactly once, got %d", r.cleaned)
	}
	if r.parts.agents.Harness != nil || r.parts.agents.HarnessLifecycle != nil {
		t.Error("Agent Hub kept the provider after the listener failed")
	}
	if r.port.Bound() {
		t.Error("the port stayed bound after the listener failed")
	}
	if _, err := r.port.Profile(r.ctx); !errors.Is(err, ErrHarnessNotReady) {
		t.Errorf("unbound port must refuse with the fixed sentence, got %v", err)
	}
	// The other owner's socket is untouched.
	if _, err := os.Stat(r.sock); err != nil {
		t.Errorf("the failed bind removed another owner's socket: %v", err)
	}
}

func TestStartupRefusesAMissingPieceAndCleansWhatItWasHanded(t *testing.T) {
	r := newStartupRig(t)
	cases := map[string]func(s *HarnessStartup){
		"no port":     func(s *HarnessStartup) { s.Port = nil },
		"no listener": func(s *HarnessStartup) { s.Listen = nil },
		"no agents":   func(s *HarnessStartup) { s.Config.Agents = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			before := r.cleaned
			s := r.startup()
			mutate(&s)
			if _, err := StartHarness(s); !errors.Is(err, ErrHarnessStartup) {
				t.Fatalf("got %v", err)
			}
			if r.cleaned != before+1 {
				t.Error("staged resources were not cleaned")
			}
			if r.listens != 0 || r.port.Bound() || r.parts.agents.Harness != nil {
				t.Error("a refused startup must start and attach nothing")
			}
		})
	}
}

func TestAnAlreadyBoundPortIsRefusedAndKeepsItsOwner(t *testing.T) {
	r := newStartupRig(t)
	if _, err := StartHarness(r.startup()); err != nil {
		t.Fatal(err)
	}
	s := r.startup()
	r.listens = 0
	if _, err := StartHarness(s); !errors.Is(err, ErrHarnessAlreadyAttached) {
		t.Fatalf("a second startup must be refused, got %v", err)
	}
	if r.listens != 0 {
		t.Error("a refused second startup must not bind another listener")
	}
	// A refused second composition must not unbind the first.
	if !r.port.Bound() || r.parts.agents.Harness == nil {
		t.Error("a refused second startup tore down the working graph")
	}
}
