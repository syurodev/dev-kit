package app

import (
	"context"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

type overrideClearer interface {
	ClearOverride(ctx context.Context, sessionID string) (harness.Stamp, error)
}

// harnessLifecycle is Agent Hub's HarnessLifecycle: an explicit session close
// removes the session's override through the store's lifecycle path (a
// tombstone with the next revision), never through a proposal. Reload, resume,
// rewind and account moves do not reach it.
type harnessLifecycle struct{ store overrideClearer }

func (l harnessLifecycle) SessionClosed(ctx context.Context, sessionID string) error {
	_, err := l.store.ClearOverride(ctx, sessionID)
	return err
}

var _ agenthub.HarnessLifecycle = harnessLifecycle{}
