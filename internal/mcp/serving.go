package mcp

import (
	"context"
	"encoding/json"
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

// servingHolder is an endpoint incarnation's policy. It belongs to one
// endpoint, so a session id alone never reaches a replacement's policy. After
// close it answers nothing.
type servingHolder struct {
	mu   sync.Mutex // serializes replace against close; load is lock-free
	cur  atomic.Pointer[servingSnapshot]
	done atomic.Bool
}

func newServingHolder(eff harness.Effective) *servingHolder {
	h := &servingHolder{}
	h.cur.Store(&servingSnapshot{eff: eff})
	return h
}

// load is the call-entry read: no lock, no I/O.
func (h *servingHolder) load() *servingSnapshot {
	if h == nil || h.done.Load() {
		return nil
	}
	return h.cur.Load()
}

// replace publishes a new policy to this endpoint only. It reports false once
// the endpoint is closed.
func (h *servingHolder) replace(eff harness.Effective) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.done.Load() {
		return false
	}
	h.cur.Store(&servingSnapshot{eff: eff})
	return true
}

func (h *servingHolder) close() {
	h.mu.Lock()
	h.done.Store(true)
	h.mu.Unlock()
}

// Refusals a served endpoint answers itself. Fixed sentences: the only
// variable part is a group name taken from the server's own registry.
func groupOffResult(tool string) *sdk.CallToolResult {
	err := &harness.Error{Code: harness.CodeGroupOff, Message: fmt.Sprintf("The %s tools are turned off for this session.", harness.Group(tool))}
	return refusalResult(err)
}

func endpointClosedResult() *sdk.CallToolResult {
	return refusalResult(&harness.Error{Code: harness.CodeSnapshotUnavailable, Message: "This session's tool connection is closed."})
}

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
