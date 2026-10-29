package app

import (
	"errors"

	"devkit/internal/agenthub"
)

// ErrHarnessStartup is the fixed refusal of a startup step that has no safe
// continuation: nothing is attached to Agent Hub, no port is bound, and no listener
// or session exists afterwards. (Background goroutines of the caller's earlier
// steps may already be running; the caller's Cleanup stops what it staged.)
var ErrHarnessStartup = errors.New("harness startup: a required step is missing; no listener or session was started")

// HarnessStartup is the ordered tail of the composition root, extracted so the
// order and its failure paths are testable without main. The caller has already
// built the MCP server (with Deps.Harness = Port, so the two public tools were in
// the registry before its inventory froze) and the session host, and has set
// Config.Agents.MCPEndpoints to that host.
type HarnessStartup struct {
	// Port is the container's HarnessPort, the same one handed to mcp.Deps.
	Port *HarnessPort
	// Config is the assembly's input; Config.Agents is the Agent Hub service.
	Config HarnessAssemblyConfig
	// Listen binds the shared listener and starts serving it. It is called only
	// after the graph is composed and attached, never before, and its error fails
	// the startup.
	Listen func() error
	// Cleanup closes what the caller staged before this call (the session host).
	// It runs on every failure, after the composition is undone.
	Cleanup func()
}

// StartHarness is the single startup call a composition root makes for the
// harness, in this order and no other:
//
//  1. ComposeHarness: build the graph from the real inventory, attach it to Agent
//     Hub (which refuses unless the session host can enforce a policy) and bind
//     the port, all or nothing.
//  2. Listen: only now bind the shared listener.
//
// Any failure undoes everything already done: Agent Hub gets its provider and
// lifecycle owner back as nil, the port is unbound (so the window offers no
// entry point and the tools refuse), the staged resources are closed and the
// error is returned for the caller to abort startup with. There is no
// continuation with Harness=nil, no unfiltered serving and no retry.
func StartHarness(s HarnessStartup) (*HarnessAssembly, error) {
	agents := s.Config.Agents
	fail := func(err error) (*HarnessAssembly, error) {
		if s.Cleanup != nil {
			s.Cleanup()
		}
		return nil, err
	}
	if s.Port == nil || agents == nil || s.Listen == nil {
		return fail(ErrHarnessStartup)
	}
	asm, err := ComposeHarness(s.Port, s.Config, agents)
	if err != nil {
		return fail(err)
	}
	if err := s.Listen(); err != nil {
		rollbackHarness(s.Port, agents)
		return fail(err)
	}
	return asm, nil
}

// rollbackHarness removes what ComposeHarness attached and bound. It is only
// called inside a failing StartHarness, before any listener or session exists.
func rollbackHarness(port *HarnessPort, agents *agenthub.Service) {
	agents.Harness, agents.HarnessLifecycle = nil, nil
	port.unbind()
}
