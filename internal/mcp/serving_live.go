package mcp

import (
	"errors"
	"fmt"
	"sort"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

// The session endpoint's live-publication handle (agenthub.ServingEndpoint).
// A change is installed on one endpoint incarnation under a quarantine: admission
// closes first (running handlers are not waited for), the SDK registry that this
// endpoint really serves is updated, the policy is swapped, and admission reopens
// in one store. If any step fails the endpoint stays closed.

var _ agenthub.ServingEndpoint = (*sessionEndpoint)(nil)

var (
	errNoPolicy       = errors.New("mcp: this endpoint serves no harness policy")
	errEndpointClosed = errors.New("mcp: the endpoint is closed")
)

func (e *sessionEndpoint) Incarnation() string { return e.incarnation }

func (e *sessionEndpoint) State() agenthub.EndpointState {
	st := agenthub.EndpointState{Incarnation: e.incarnation}
	e.mu.Lock()
	st.Closed = e.closed
	e.mu.Unlock()
	if e.serving != nil {
		a := e.serving.st.Load()
		switch {
		case a == nil || a.closed:
			st.Closed = true
		default:
			st.Paused = a.paused
			if a.snap != nil {
				st.Fingerprint = a.snap.eff.Fingerprint
			}
		}
	}
	return st
}

func (e *sessionEndpoint) Quarantine() (agenthub.EndpointTicket, error) {
	if e.serving == nil {
		return 0, errNoPolicy
	}
	t, ok := e.serving.quarantine()
	if !ok {
		return 0, errEndpointClosed
	}
	return agenthub.EndpointTicket(t), nil
}

func (e *sessionEndpoint) Restore(t agenthub.EndpointTicket) error {
	if e.serving == nil {
		return errNoPolicy
	}
	if !e.serving.restore(uint64(t)) {
		return errTicketSuperseded
	}
	return nil
}

// Install makes this endpoint serve eff: the registry the SDK instance holds,
// the policy a call is admitted under, and discovery, and only then reopens
// admission. eff must cover this server's registry exactly. Nothing is installed
// on a replacement endpoint: the receiver is one incarnation.
func (e *sessionEndpoint) Install(t agenthub.EndpointTicket, eff harness.Effective, _ harness.ServingIdentity) (agenthub.InstallReport, error) {
	var rep agenthub.InstallReport
	if e.serving == nil {
		return rep, errNoPolicy
	}
	inv, err := e.host.server.Inventory()
	if err != nil {
		return rep, err
	}
	if err := checkPolicyCoversInventory(inv, eff); err != nil {
		return rep, err
	}
	err = e.serving.install(uint64(t), eff, func() error { return e.syncRegistry(inv, eff, &rep) })
	return rep, err
}

// syncRegistry brings the SDK registry to exactly the tools eff serves, with the
// supported AddTool/RemoveTools only. Tools that must come back are re-registered
// from the server's own declarations; tools that must go are removed. A panic or
// a hook failure leaves the endpoint paused, and visible stays a superset of the
// truth so the next install removes whatever should not be there.
func (e *sessionEndpoint) syncRegistry(inv Inventory, eff harness.Effective, rep *agenthub.InstallReport) (err error) {
	hook := func(stage string) error {
		if e.host.installHook != nil {
			return e.host.installHook(stage)
		}
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			for _, t := range inv.Tools {
				e.visible[t.Name] = true
			}
			err = fmt.Errorf("mcp: updating the tool registry failed: %v", r)
		}
	}()
	want := make(map[string]bool, len(inv.Tools))
	for _, t := range inv.Tools {
		if eff.Served(t.Name) {
			want[t.Name] = true
		}
	}
	if err := hook("begin"); err != nil {
		return err
	}
	prev := make(map[string]bool, len(e.visible))
	for n := range e.visible {
		prev[n] = true
	}
	add := false
	for n := range want {
		if !e.visible[n] {
			add = true
			break
		}
	}
	if add {
		// Registered under a superset first: the instance is paused, so no list or
		// call sees the intermediate registry.
		for _, t := range inv.Tools {
			e.visible[t.Name] = true
		}
		e.host.server.registerAll(e.ms)
		if err := hook("registered"); err != nil {
			return err
		}
	}
	var hide []string
	for n := range e.visible {
		if !want[n] {
			hide = append(hide, n)
		}
	}
	sort.Strings(hide)
	if len(hide) > 0 {
		e.ms.RemoveTools(hide...)
		for _, n := range hide {
			delete(e.visible, n)
		}
		if err := hook("hidden"); err != nil {
			return err
		}
	}
	// Changed means what a client lists differs from before, not that a call was made.
	rep.ToolsChanged = len(prev) != len(e.visible)
	for n := range e.visible {
		rep.ToolsChanged = rep.ToolsChanged || !prev[n]
	}
	// HasTools is set on a policy endpoint, so the SDK advertises list-changed and
	// schedules the notification itself. That is an attempt, not an acknowledgement.
	rep.NotificationScheduled = rep.ToolsChanged
	return hook("policy")
}
