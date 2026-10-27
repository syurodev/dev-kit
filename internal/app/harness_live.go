package app

import (
	"context"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
)

// harnessLive binds harnessconfig.Live to Agent Hub's per-session coordinator and
// the endpoint it holds. It adds no behavior: every decision is made by the
// coordinator (agenthub/serving_live.go) and the endpoint (mcp/serving_live.go).
type harnessLive struct{ svc *agenthub.Service }

var _ harnessconfig.Live = harnessLive{}

func (l harnessLive) Target(_ context.Context, sessionID string) (harnessconfig.LiveTarget, error) {
	return liveTarget(l.svc.ServingTarget(sessionID)), nil
}

func (l harnessLive) Coordinate(ctx context.Context, sessionID string, fn func(harnessconfig.LiveTxn) error) error {
	return l.svc.CoordinateServing(ctx, sessionID, func(t *agenthub.ServingTxn) error { return fn(liveTxn{t}) })
}

func (l harnessLive) Reconcile(ctx context.Context, sessionID string, read func(context.Context) (*harness.Override, harness.Stamp, error)) (harnessconfig.Publication, error) {
	p, err := l.svc.ReconcileServing(ctx, sessionID, read)
	return publication(p), err
}

func liveTarget(v agenthub.ServingView) harnessconfig.LiveTarget {
	return harnessconfig.LiveTarget{Active: v.Active, Staged: v.Staged, Incarnation: v.Incarnation, Epoch: v.Epoch, Basis: v.Basis, Serving: v.Serving, Quarantined: v.Quarantined}
}

func publication(p agenthub.ServingPublication) harnessconfig.Publication {
	out := harnessconfig.Publication{Serving: p.Serving, ToolsChanged: p.ToolsChanged, NotificationScheduled: p.NotificationScheduled}
	switch {
	case p.Installed && p.Staged:
		out.State = harnessconfig.PublicationStaged
	case p.Installed:
		out.State = harnessconfig.PublicationInstalled
	case p.Pending:
		out.State = harnessconfig.PublicationPending
	}
	return out
}

type liveTxn struct{ t *agenthub.ServingTxn }

func (x liveTxn) Target() harnessconfig.LiveTarget { return liveTarget(x.t.Target()) }
func (x liveTxn) Quarantine() error                { return x.t.Quarantine() }
func (x liveTxn) Restore() error                   { return x.t.Restore() }
func (x liveTxn) MarkPending()                     { x.t.MarkPending() }
func (x liveTxn) Publish(stamp harness.Stamp, durable *harness.Override) (harnessconfig.Publication, error) {
	p, err := x.t.Publish(stamp, durable)
	return publication(p), err
}
