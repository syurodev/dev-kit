package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"devkit/internal/harness"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// InventoryTool is one tool the server really registers: its name, harness
// group, the size of its definition as a client receives it, and the fixed
// result-budget exemption this package owns, if any.
type InventoryTool struct {
	Name            string
	Group           string
	DefinitionBytes int
	BudgetExempt    bool
	ExemptReason    string
}

// Inventory is the registry of one Server, read from what it actually
// registered (conditional modules included), so validation, serving and cost
// checks all use the same facts.
type Inventory struct{ Tools []InventoryTool }

// Registry is the inventory as the harness resolver wants it.
func (i Inventory) Registry() harness.Registry {
	reg := harness.Registry{Tools: make([]harness.Tool, 0, len(i.Tools))}
	for _, t := range i.Tools {
		reg.Tools = append(reg.Tools, harness.Tool{Name: t.Name, DefinitionBytes: t.DefinitionBytes, BudgetExempt: t.BudgetExempt})
	}
	return reg
}

// Has reports whether name is a registered tool.
func (i Inventory) Has(name string) bool {
	k := sort.Search(len(i.Tools), func(n int) bool { return i.Tools[n].Name >= name })
	return k < len(i.Tools) && i.Tools[k].Name == name
}

// Inventory lists the tools this server registers, computed once. It connects
// an in-memory client to a throwaway instance, which is the public way to read
// the registry the SDK holds; the instance is not served to anyone.
func (s *Server) Inventory() (Inventory, error) {
	s.invOnce.Do(func() {
		s.inv, s.invErr = s.readInventory()
	})
	return s.inv, s.invErr
}

func (s *Server) readInventory() (Inventory, error) {
	ctx := context.Background()
	ms := s.sdkServerWith(nil, "", nil)
	st, ct := sdk.NewInMemoryTransports()
	ss, err := ms.Connect(ctx, st, nil)
	if err != nil {
		return Inventory{}, err
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "inventory", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		return Inventory{}, err
	}
	defer cs.Close()
	var inv Inventory
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return Inventory{}, err
		}
		raw, err := json.Marshal(tool)
		if err != nil {
			return Inventory{}, err
		}
		reason, exempt := budgetExempt[tool.Name]
		inv.Tools = append(inv.Tools, InventoryTool{
			Name: tool.Name, Group: harness.Group(tool.Name), DefinitionBytes: len(raw),
			BudgetExempt: exempt, ExemptReason: reason,
		})
	}
	sort.Slice(inv.Tools, func(a, b int) bool { return inv.Tools[a].Name < inv.Tools[b].Name })
	return inv, nil
}

// servingSnapshot is everything one tool call needs from the endpoint's policy:
// the group decision, the budget and the W/S revisions it is measured under.
// It is immutable; a call loads it once at entry and uses no other source.
type servingSnapshot struct {
	eff harness.Effective
}

// serves reports whether the call may reach its handler. A tool the policy has
// never heard of is not decided here: it gets the SDK's normal unknown-tool
// answer. A known tool follows its group.
func (n *servingSnapshot) serves(tool string) bool {
	if _, known := n.eff.Budget(tool); !known {
		return true
	}
	return n.eff.Served(tool)
}

// admissionState is everything one call needs to be admitted, read with ONE
// atomic load: the policy, and whether admission is paused or closed. A call
// that loaded a ready state before a quarantine was stored was admitted before
// it and may finish under that policy; none can be admitted under the old policy
// after.
type admissionState struct {
	snap           *servingSnapshot
	paused, closed bool
}

// admission says why a call was or was not admitted.
type admission int

const (
	admitReady admission = iota
	admitPaused
	admitClosed
)

// servingHolder is an endpoint incarnation's policy. It belongs to one
// endpoint, so a session id alone never reaches a replacement's policy. After
// close it answers nothing; while paused (a quarantine, see quarantine) it
// admits nothing either.
type servingHolder struct {
	mu     sync.Mutex // serializes transitions; admit and load are lock-free
	st     atomic.Pointer[admissionState]
	seq    uint64
	ticket uint64
}

func newServingHolder(eff harness.Effective) *servingHolder {
	h := &servingHolder{}
	h.st.Store(&admissionState{snap: &servingSnapshot{eff: eff}})
	return h
}

// admit is the call-entry read: no lock, no I/O.
func (h *servingHolder) admit() (*servingSnapshot, admission) {
	st := h.st.Load()
	switch {
	case st == nil || st.closed:
		return nil, admitClosed
	case st.paused:
		return nil, admitPaused
	}
	return st.snap, admitReady
}

// load is the snapshot of a ready endpoint, nil when it is closed or paused.
func (h *servingHolder) load() *servingSnapshot {
	if h == nil {
		return nil
	}
	snap, _ := h.admit()
	return snap
}

// listable reports whether tools/list may be answered: only a ready endpoint,
// so a registry that is being rewritten is never offered.
func (h *servingHolder) listable() bool { _, a := h.admit(); return a == admitReady }

// replace publishes a new policy to this endpoint only. It reports false once
// the endpoint is closed.
func (h *servingHolder) replace(eff harness.Effective) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.st.Load()
	if st == nil || st.closed {
		return false
	}
	h.st.Store(&admissionState{snap: &servingSnapshot{eff: eff}, paused: st.paused})
	return true
}

func (h *servingHolder) close() {
	h.mu.Lock()
	h.st.Store(&admissionState{closed: true})
	h.mu.Unlock()
}

// quarantine closes admission and returns the ticket of this operation, which
// supersedes any earlier one. Running handlers are not waited for.
func (h *servingHolder) quarantine() (uint64, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.st.Load()
	if st == nil || st.closed {
		return 0, false
	}
	h.seq++
	h.ticket = h.seq
	h.st.Store(&admissionState{snap: st.snap, paused: true})
	return h.ticket, true
}

// restore reopens admission under the policy the endpoint held, only for the
// operation that closed it and only while the endpoint is still paused.
func (h *servingHolder) restore(ticket uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.st.Load()
	if st == nil || st.closed || !st.paused || h.ticket != ticket {
		return false
	}
	h.st.Store(&admissionState{snap: st.snap})
	return true
}

// install runs change (the registry and discovery update) while admission is
// paused for ticket, then publishes the new policy and reopens admission in one
// store. If change fails the endpoint stays paused.
func (h *servingHolder) install(ticket uint64, eff harness.Effective, change func() error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.st.Load()
	if st == nil || st.closed || !st.paused || h.ticket != ticket {
		return errTicketSuperseded
	}
	if err := change(); err != nil {
		return err
	}
	h.st.Store(&admissionState{snap: &servingSnapshot{eff: eff}})
	return nil
}

var errTicketSuperseded = errors.New("mcp: the operation on this endpoint was superseded or the endpoint closed")

// Refusals a served endpoint answers itself. Fixed sentences: the only
// variable part is a group name taken from the server's own registry.
func groupOffResult(tool string) *sdk.CallToolResult {
	err := &harness.Error{Code: harness.CodeGroupOff, Message: fmt.Sprintf("The %s tools are turned off for this session.", harness.Group(tool))}
	return refusalResult(err)
}

func endpointClosedResult() *sdk.CallToolResult {
	return refusalResult(&harness.Error{Code: harness.CodeSnapshotUnavailable, Message: "This session's tool connection is closed."})
}

// endpointPausedResult answers a call that arrives while a policy change is
// being installed. Nothing reached a handler or the gateway.
func endpointPausedResult() *sdk.CallToolResult {
	return refusalResult(&harness.Error{Code: harness.CodeSnapshotUnavailable, Message: "This session's tool access is being updated. Try again shortly."})
}

var errToolListUnavailable = &harness.Error{Code: harness.CodeSnapshotUnavailable, Message: "This session's tool list is being updated. Try again shortly."}

func refusalResult(err *harness.Error) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}}}
}

// hiddenTools are the registered tools a policy does not serve.
func hiddenTools(inv Inventory, eff harness.Effective) []string {
	var hidden []string
	for _, t := range inv.Tools {
		if !eff.Served(t.Name) {
			hidden = append(hidden, t.Name)
		}
	}
	return hidden
}

// checkPolicyCoversInventory refuses a policy resolved against some other
// registry: a registered tool the policy has never heard of has no group
// decision or budget, and serving it would be a guess.
func checkPolicyCoversInventory(inv Inventory, eff harness.Effective) error {
	for _, t := range inv.Tools {
		b, ok := eff.Budget(t.Name)
		if !ok || b.Exempt != t.BudgetExempt {
			return &harness.Error{Code: harness.CodeNotEnforceable, Message: "The harness policy was resolved against a different tool registry."}
		}
	}
	return nil
}
