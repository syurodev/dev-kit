package harnessconfig

import (
	"errors"
	"testing"
)

func TestProposalPreparedWhileStagedIsStaleAfterRuntimePublication(t *testing.T) {
	l := newLiveEnv(t)
	l.seedProfile("w1", offW("git"))
	l.activate("s1", "w1")
	l.live.edit("s1", func(target *LiveTarget) { target.Staged = true })
	p := l.prepare("s1", enableGit())
	// The same incarnation and S now belong to a successfully opened runtime.
	// The proposal must not silently change from a staged to a live operation.
	l.live.edit("s1", func(target *LiveTarget) { target.Staged = false })
	if _, err := l.svc.Apply(agent("s1"), p.ID); !errors.Is(err, ErrStale) {
		t.Fatalf("staged approval carried into a published runtime: %v", err)
	}
	if events := l.live.events(); len(events) != 0 {
		t.Fatalf("stale proposal reached publication: %v", events)
	}
}
