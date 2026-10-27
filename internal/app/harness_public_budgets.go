package app

import (
	"context"
	"encoding/base64"
	"sort"
	"strings"

	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
)

// The budget half of the public facade: bounded reads of a workspace's result
// limits, and budget-only proposals. Effective values come from harness.Resolve;
// the only thing derived here is which level an explicit rule sits at, read from
// the profile's own fields. A budget edit targets the saved workspace profile and
// nothing else: it never changes a running endpoint's pinned limits, and S has no
// budget field.

const (
	defaultBudgetPage = 50
	maxBudgetPage     = 100
	maxBudgetEditsAsk = 16
)

var errHarnessBudgetCursor = &harness.Error{Code: harness.CodeProfileInvalid, Message: "That page cursor does not belong to this request."}

func (c *harnessControls) WorkspaceBudgets(ctx context.Context, workspaceID string, f harnessconfig.BudgetFilter) (harnessconfig.BudgetPage, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.BudgetPage{}, err
	}
	if err := c.requireWorkspace(ctx, workspaceID); err != nil {
		return harnessconfig.BudgetPage{}, err
	}
	return c.budgetPage(ctx, workspaceID, f)
}

func (c *harnessControls) ProfileWithBudgets(ctx context.Context, f harnessconfig.BudgetFilter) (harnessconfig.ProfileView, error) {
	sessionID, scope, err := c.agentScope(ctx)
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	v, err := c.view(ctx, scope.WorkspaceID, sessionID, scope.Vendor)
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	page, err := c.budgetPage(ctx, scope.WorkspaceID, f)
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	v.Budgets = &page
	return v, nil
}

func (c *harnessControls) PrepareBudgetEdits(ctx context.Context, workspaceID string, edits []harnessconfig.BudgetEdit) (harnessconfig.Review, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.Review{}, err
	}
	if err := c.requireWorkspace(ctx, workspaceID); err != nil {
		return harnessconfig.Review{}, err
	}
	if len(edits) == 0 {
		return harnessconfig.Review{}, harnessconfig.ErrInvalid
	}
	return c.prepare(ctx, harnessconfig.Request{WorkspaceID: workspaceID, Target: harnessconfig.TargetWorkspace, Patch: harnessconfig.Patch{BudgetEdits: edits}})
}

// requestBudgets is an agent's budget proposal. There is no scope to choose: it
// edits the saved profile of the agent's own workspace, and the review says so.
// Whether anyone is asked is the service's decision (the person's opt-in covers a
// narrowing edit only); this never skips Prepare or Apply.
func (c *harnessControls) requestBudgets(ctx context.Context, sessionID string, scope harnessconfig.SessionScope, req harnessconfig.GroupRequest) (harnessconfig.RequestResult, error) {
	if len(req.Budgets) > maxBudgetEditsAsk {
		return harnessconfig.RequestResult{}, harnessconfig.ErrInvalid
	}
	r := harnessconfig.Request{SessionID: sessionID, Target: harnessconfig.TargetWorkspace, Rationale: req.Rationale,
		Patch: harnessconfig.Patch{BudgetEdits: req.Budgets}}
	prepared, err := c.svc.Prepare(ctx, r)
	if err != nil {
		return harnessconfig.RequestResult{}, err
	}
	res, err := c.svc.Apply(ctx, prepared.ID)
	out, oerr := harnessconfig.OutcomeOf(harnessconfig.TargetWorkspace, res, err)
	if oerr != nil {
		return harnessconfig.RequestResult{}, oerr // nothing was saved, and nothing is claimed
	}
	// The requester keeps the limits it opened with: a saved profile edit reaches
	// it at its next allowed open or reload, and the outcome says so.
	out.Live = false
	view, verr := c.view(ctx, scope.WorkspaceID, sessionID, scope.Vendor)
	if verr != nil {
		view = harnessconfig.ProfileView{WorkspaceID: scope.WorkspaceID, SessionID: sessionID}
	}
	return harnessconfig.RequestResult{Profile: view, Outcome: &out}, nil
}

// budgetPage reads one bounded page. Tools are in name order; the cursor names the
// last tool returned and the filter it belongs to.
func (c *harnessControls) budgetPage(ctx context.Context, workspaceID string, f harnessconfig.BudgetFilter) (harnessconfig.BudgetPage, error) {
	w, stamp, err := c.store.GetProfile(ctx, workspaceID)
	if err != nil {
		return harnessconfig.BudgetPage{}, harnessconfigStoreErr(err)
	}
	reg := c.registry()
	eff, err := harness.Resolve(harness.Inputs{Registry: reg, Vendor: harness.Vendor{}, Workspace: withRev(w, stamp.Revision)})
	if err != nil {
		return harnessconfig.BudgetPage{}, harnessconfig.ErrStoredUnreadable
	}
	var tools, groups map[string]harness.BudgetRule
	if w != nil && w.ResultBudget != nil {
		tools, groups = w.ResultBudget.Tools, w.ResultBudget.Groups
	}
	known := c.groupSet()
	if f.Group != "" && !known[f.Group] {
		return harnessconfig.BudgetPage{}, errHarnessGroupUnknown
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultBudgetPage
	}
	if limit > maxBudgetPage {
		limit = maxBudgetPage
	}
	after, err := decodeBudgetCursor(f.Cursor, f.Group)
	if err != nil {
		return harnessconfig.BudgetPage{}, err
	}

	page := harnessconfig.BudgetPage{WorkspaceID: workspaceID, ProfileRevision: stamp.Revision, ProfilePresent: stamp.Present,
		DefaultBytes: harness.DefaultResultBudget, MinBytes: harness.MinResultBudget, MaxBytes: harness.MaxResultBudget,
		Group: f.Group, Tools: []harnessconfig.ToolBudgetRow{}}
	sorted := append([]harness.Tool(nil), reg.Tools...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	if f.Cursor == "" {
		byGroup := map[string]*harnessconfig.GroupBudgetRow{}
		for _, t := range sorted {
			g := harness.Group(t.Name)
			row := byGroup[g]
			if row == nil {
				row = &harnessconfig.GroupBudgetRow{Group: g}
				byGroup[g] = row
			}
			row.Tools++
			if !t.BudgetExempt {
				row.Cuttable++
			}
		}
		names := make([]string, 0, len(byGroup))
		for g := range byGroup {
			names = append(names, g)
		}
		sort.Strings(names)
		for _, g := range names {
			row := *byGroup[g]
			if rule, ok := groups[g]; ok {
				row.Bytes, row.Cut = copyInt(rule.Bytes), string(rule.Cut)
			}
			if f.Group == "" || f.Group == g {
				page.Groups = append(page.Groups, row)
			}
		}
	}

	for _, t := range sorted {
		g := harness.Group(t.Name)
		if (f.Group != "" && g != f.Group) || t.Name <= after {
			continue
		}
		if len(page.Tools) == limit {
			page.NextCursor = encodeBudgetCursor(f.Group, page.Tools[len(page.Tools)-1].Tool)
			break
		}
		row := harnessconfig.ToolBudgetRow{Tool: t.Name, Group: g, DefinitionBytes: t.DefinitionBytes, Exempt: t.BudgetExempt}
		if !t.BudgetExempt {
			tr, gr := tools[t.Name], groups[g]
			row.ToolBytes, row.ToolCut = copyInt(tr.Bytes), string(tr.Cut)
			row.GroupBytes, row.GroupCut = copyInt(gr.Bytes), string(gr.Cut)
			if b, ok := eff.Budget(t.Name); ok {
				row.EffectiveBytes, row.EffectiveCut = b.Bytes, string(b.Cut)
			}
			row.BytesFrom = sourceOf(tr.Bytes != nil, gr.Bytes != nil)
			row.CutFrom = sourceOf(tr.Cut != harness.CutDefault, gr.Cut != harness.CutDefault)
		}
		page.Tools = append(page.Tools, row)
	}
	return page, nil
}

func sourceOf(tool, group bool) harnessconfig.BudgetSource {
	switch {
	case tool:
		return harnessconfig.BudgetFromTool
	case group:
		return harnessconfig.BudgetFromGroup
	}
	return harnessconfig.BudgetFromDefault
}

func copyInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func withRev(w *harness.Profile, rev int64) *harness.Profile {
	if w == nil {
		return nil
	}
	c := *w
	c.Revision = rev
	return &c
}

func encodeBudgetCursor(group, last string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(group + "\x00" + last))
}

// decodeBudgetCursor returns the tool after which the page starts. A cursor made
// for another group (or garbled) is refused, never read as the start of this list.
func decodeBudgetCursor(cursor, group string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", errHarnessBudgetCursor
	}
	g, last, ok := strings.Cut(string(raw), "\x00")
	if !ok || g != group || last == "" {
		return "", errHarnessBudgetCursor
	}
	return last, nil
}
