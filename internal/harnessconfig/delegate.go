package harnessconfig

import (
	"context"
	"slices"

	"devkit/internal/harness"
	"devkit/internal/identity"
)

// Delegation is a parent handing tool groups it serves to one of its children.
// Every field comes from the host (the request ledger and the session rows), not
// from a model. The reason a child gave is deliberately absent.
type Delegation struct {
	WorkspaceID      string
	ChildSessionID   string
	Groups           []string
	GrantorSessionID string
	RequestID        string
	// Grantable reads the set the grantor may hand out. It is called once to
	// refuse early and again inside the commit, under the V boundary and the
	// child's coordinator, so a grantor that lost a group or a child that was
	// blocked or closed meanwhile is not carried over from an earlier read. A
	// nil Grantable grants nothing.
	Grantable func(ctx context.Context) ([]string, error)
}

// ApplyDelegated enables Groups in the child's session override without asking
// the person, because the parent is handing over what it serves itself.
//
// It replaces only the person's question. The change is prepared and judged
// exactly like any other (the same evaluation, tier, live effect and
// compare-and-swap), and it commits through the same V boundary, session
// coordinator and publication, so a changed W, S or policy makes it stale and a
// failed publication is reported the same way.
//
// Narrow on purpose: the caller must be the authenticated grantor, the target a
// session override, and the patch only EnableGroups inside Grantable. A budget,
// a W edit, a revoke or an executable never goes through here.
func (s *Service) ApplyDelegated(ctx context.Context, d Delegation) (Result, error) {
	caller, ok := identity.FromContext(ctx)
	if !ok || !caller.IsAgent() || caller.SessionID != d.GrantorSessionID || d.GrantorSessionID == "" ||
		d.ChildSessionID == "" || d.ChildSessionID == d.GrantorSessionID || d.RequestID == "" || d.WorkspaceID == "" {
		return Result{}, ErrDenied
	}
	groups := setOf(d.Groups)
	if len(groups) == 0 || len(groups) > maxPatchList {
		return Result{}, ErrInvalid
	}
	within := func(ctx context.Context) error {
		if d.Grantable == nil {
			return ErrDenied
		}
		grantable, err := d.Grantable(ctx)
		if err != nil {
			return ErrDenied
		}
		for _, g := range groups {
			if !slices.Contains(grantable, g) {
				return ErrDenied
			}
		}
		return nil
	}
	if err := within(ctx); err != nil {
		return Result{}, err
	}
	if s.cfg.Boundary == nil {
		return Result{}, ErrConfirmUnavailable
	}
	req := Request{WorkspaceID: d.WorkspaceID, SessionID: d.ChildSessionID, Target: TargetSession, Patch: Patch{EnableGroups: groups}}
	host := identity.UI() // the evaluation names a session the grantor does not own; the host does.
	p := &pendingProposal{id: "delegated:" + d.RequestID, caller: caller.String(), req: req, taken: true,
		grantor: d.GrantorSessionID, requestID: d.RequestID, precommit: within}
	refuse := func(err error) (Result, error) {
		s.audit(p, harness.Applied{}, outcomeOf(err), "")
		return Result{}, err
	}
	ev, policy, err := s.evaluate(ctx, host, req, s.liveReaderFor(ctx))
	if err != nil {
		p.bound = ev
		return refuse(err)
	}
	if ev.Tier == harness.TierExecutable {
		if !policy.AllowExecutable {
			return refuse(ErrExecutableDenied)
		}
		return refuse(ErrExecutableNotReady)
	}
	view, err := buildView(ev, policy)
	if err != nil {
		return refuse(err)
	}
	p.bound, p.view = ev, view
	if err := ctx.Err(); err != nil {
		return refuse(ErrNotConfirmed)
	}
	return s.runCommit(ctx, host, p)
}
