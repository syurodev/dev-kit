package agenthub_test

import (
	"context"
	"sync"
	"testing"

	"devkit/internal/agenthub"
)

// closeProbe is a vendor runtime whose Close honours its context, as a real adapter's
// does: a cancelled context leaves the process running.
type closeProbe struct {
	agenthub.SessionRuntime
	mu         sync.Mutex
	closed     bool
	refusedErr error
}

func (c *closeProbe) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		c.mu.Lock()
		c.refusedErr = err
		c.mu.Unlock()
		return err
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return c.SessionRuntime.Close(ctx)
}

func (c *closeProbe) state() (closed bool, refused error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed, c.refusedErr
}

// A resume that cannot be completed must close the process it started, even if the caller
// has gone away meanwhile: a cancelled context must not leave a vendor process running
// that nothing points at. Two ways to fail after the process is up: the first resume of a
// session that was never prompted (its new vendor id cannot be stored) and a resume of a
// prompted one whose launch is refused at publication (S moved while it loaded).
func TestAResumeThatFailsClosesItsProcessForACancelledCaller(t *testing.T) {
	for name, prompted := range map[string]bool{"never prompted": false, "refused at publication": true} {
		t.Run(name, func(t *testing.T) {
			e := newLaunchEnv(t, policy)
			s := e.mustCreate()
			if prompted {
				if _, err := e.svc.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: s.ID, Prompt: "hello"}); err != nil {
					t.Fatal(err)
				}
			}
			e.adapter.kill() // the process died; the resume reopens it

			var probes []*closeProbe
			var mu sync.Mutex
			ctx, cancel := context.WithCancel(uiCtx())
			defer cancel()
			e.adapter.wrapRT = func(rt agenthub.SessionRuntime) agenthub.SessionRuntime {
				p := &closeProbe{SessionRuntime: rt}
				mu.Lock()
				probes = append(probes, p)
				mu.Unlock()
				// The process is up. Before the launch is published, S commits and the
				// caller goes away.
				if err := e.svc.CoordinateServing(context.Background(), s.ID, func(txn *agenthub.ServingTxn) error { txn.MarkPending(); return nil }); err != nil {
					t.Error(err)
				}
				cancel()
				return p
			}

			if err := e.svc.ResumeSession(ctx, s.ID, false); err == nil {
				t.Fatal("a resume that cannot be completed must be refused")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(probes) != 1 {
				t.Fatalf("runtimes started = %d, want 1", len(probes))
			}
			if closed, refused := probes[0].state(); !closed {
				t.Fatalf("the refused resume left its process running (Close refused with %v)", refused)
			}
		})
	}
}
