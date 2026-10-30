package app

import (
	"context"

	"devkit/internal/harnessconfig"
)

// The port also forwards the child tool-group request controls. They live on the
// same facade as Controls; a bound controls value that lacks them answers
// ErrHarnessNotReady rather than panicking.

var _ harnessconfig.GroupRequestControls = (*HarnessPort)(nil)

func (p *HarnessPort) groups() (harnessconfig.GroupRequestControls, error) {
	c, err := p.get()
	if err != nil {
		return nil, err
	}
	g, ok := c.(harnessconfig.GroupRequestControls)
	if !ok {
		return nil, ErrHarnessNotReady
	}
	return g, nil
}

func (p *HarnessPort) CreateGroupRequest(ctx context.Context, groups []string, route, reason string) (harnessconfig.GroupRequestResult, error) {
	g, err := p.groups()
	if err != nil {
		return harnessconfig.GroupRequestResult{}, err
	}
	return g.CreateGroupRequest(ctx, groups, route, reason)
}

func (p *HarnessPort) GroupRequestStatus(ctx context.Context, id string) (harnessconfig.GroupRequestView, error) {
	g, err := p.groups()
	if err != nil {
		return harnessconfig.GroupRequestView{}, err
	}
	return g.GroupRequestStatus(ctx, id)
}

func (p *HarnessPort) ListGroupRequests(ctx context.Context) ([]harnessconfig.GroupRequestView, error) {
	g, err := p.groups()
	if err != nil {
		return nil, err
	}
	return g.ListGroupRequests(ctx)
}

func (p *HarnessPort) ResolveGroupRequest(ctx context.Context, id string, revision int64, decision string) (harnessconfig.GroupRequestResult, error) {
	g, err := p.groups()
	if err != nil {
		return harnessconfig.GroupRequestResult{}, err
	}
	return g.ResolveGroupRequest(ctx, id, revision, decision)
}

func (p *HarnessPort) AnswerGroupRequest(ctx context.Context, id string, revision int64, answer string) (harnessconfig.GroupRequestResult, error) {
	g, err := p.groups()
	if err != nil {
		return harnessconfig.GroupRequestResult{}, err
	}
	return g.AnswerGroupRequest(ctx, id, revision, answer)
}

func (p *HarnessPort) GroupLocks(ctx context.Context, sessionID string) ([]string, error) {
	g, err := p.groups()
	if err != nil {
		return nil, err
	}
	return g.GroupLocks(ctx, sessionID)
}

func (p *HarnessPort) SetGroupLock(ctx context.Context, sessionID, group string, locked bool) error {
	g, err := p.groups()
	if err != nil {
		return err
	}
	return g.SetGroupLock(ctx, sessionID, group, locked)
}

func (p *HarnessPort) PlanChildGroups(ctx context.Context, explicit []string, given bool) ([]string, error) {
	g, err := p.groups()
	if err != nil {
		return nil, err
	}
	return g.PlanChildGroups(ctx, explicit, given)
}

func (p *HarnessPort) GrantChildGroups(ctx context.Context, childID string, groups []string) (*harnessconfig.Outcome, error) {
	g, err := p.groups()
	if err != nil {
		return nil, err
	}
	return g.GrantChildGroups(ctx, childID, groups)
}
