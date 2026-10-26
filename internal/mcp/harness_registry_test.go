package mcp

import (
	"encoding/json"
	"testing"

	"devkit/internal/harness"
)

// harnessRegistry is the registry fact set the host will hand to
// harness.Resolve: every tool the product can offer, with the cost of its
// definition and the fixed exemption this package owns.
func harnessRegistry(t *testing.T) harness.Registry {
	t.Helper()
	var reg harness.Registry
	for _, tool := range listAllTools(t) {
		in, _ := json.Marshal(tool.InputSchema)
		out, _ := json.Marshal(tool.OutputSchema)
		_, exempt := budgetExempt[tool.Name]
		reg.Tools = append(reg.Tools, harness.Tool{
			Name:            tool.Name,
			DefinitionBytes: len(tool.Description) + len(in) + len(out),
			BudgetExempt:    exempt,
		})
	}
	return reg
}

// The harness contract and the MCP server must agree on the numbers a profile
// is validated against, or a profile could promise a budget the server does not
// apply.
func TestHarnessDefaultBudgetIsTheServersResultBudget(t *testing.T) {
	if harness.DefaultResultBudget != ResultBudget {
		t.Fatalf("harness default %d != ResultBudget %d", harness.DefaultResultBudget, ResultBudget)
	}
}

func TestHarnessResolvesTheRealRegistryLikeTodaysBehavior(t *testing.T) {
	reg := harnessRegistry(t)
	eff, err := harness.Resolve(harness.Inputs{Registry: reg})
	if err != nil {
		t.Fatalf("Resolve with no profile: %v", err)
	}
	exempt := 0
	for _, tool := range reg.Tools {
		if !eff.Served(tool.Name) {
			t.Errorf("%s is not served with no profile", tool.Name)
		}
		b, _ := eff.Budget(tool.Name)
		switch {
		case tool.BudgetExempt:
			exempt++
			if !b.Exempt {
				t.Errorf("%s lost its exemption", tool.Name)
			}
		case b.Bytes != ResultBudget:
			t.Errorf("%s budget %d, want %d", tool.Name, b.Bytes, ResultBudget)
		}
	}
	if exempt != len(budgetExempt) {
		t.Errorf("registry exposes %d exempt tools, budgetExempt names %d: an exemption names a tool that does not exist", exempt, len(budgetExempt))
	}
	total := 0
	for _, g := range eff.Groups {
		total += g.DefinitionBytes
		if !g.On || g.Tools == 0 {
			t.Errorf("group %+v", g)
		}
	}
	t.Logf("%d groups, %d bytes of definitions across the full registry", len(eff.Groups), total)
}
