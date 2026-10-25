package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

func TestCancellationDuringPersistOpenCleansCommittedOrMissingRows(t *testing.T) {
	for _, waiterKind := range []string{"permission", "elicitation"} {
		for _, commitRow := range []bool{true, false} {
			name := waiterKind + "/"
			if commitRow {
				name += "commit-then-context-error"
			} else {
				name += "no-row"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				openStarted := make(chan struct{})
				closed := make(chan PendingRequestResolution, 1)
				notified := make(chan struct{}, 1)
				persistenceErrors := make(chan error, 1)
				var mu sync.Mutex
				rowExists := false
				open := func(ctx context.Context, _ PendingRequestRecord) error {
					close(openStarted)
					<-ctx.Done()
					if commitRow {
						mu.Lock()
						rowExists = true // The insert may commit despite caller cancellation.
						mu.Unlock()
					}
					return ctx.Err()
				}
				closeRow := func(ctx context.Context, resolution PendingRequestResolution) error {
					if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil {
						t.Error("cleanup did not use a live context with a deadline")
					}
					mu.Lock()
					defer mu.Unlock()
					if !rowExists {
						return ErrPendingRequestNotFound
					}
					rowExists = false
					closed <- resolution
					return nil
				}

				var permission *PermissionWaiter
				var elicitation *ElicitationWaiter
				if waiterKind == "permission" {
					permission = NewPermissionWaiter(func(PendingPermission) { notified <- struct{}{} })
					permission.PersistOpen = open
					permission.PersistClose = closeRow
					permission.OnPersistenceError = func(err error) { persistenceErrors <- err }
				} else {
					elicitation = NewElicitationWaiter(func(PendingElicitation) { notified <- struct{}{} }, nil)
					elicitation.PersistOpen = open
					elicitation.PersistClose = closeRow
					elicitation.OnPersistenceError = func(err error) { persistenceErrors <- err }
				}

				done := make(chan json.RawMessage, 1)
				permissionErr := make(chan error, 1)
				go func() {
					if permission != nil {
						response, err := permission.Wait(ctx, "session", json.RawMessage(`{"secret":"never log"}`))
						done <- response
						permissionErr <- err
						return
					}
					response, err := elicitation.Wait(ctx, "session", json.RawMessage(`{"message":"private"}`))
					done <- response
					permissionErr <- err
				}()

				awaitPendingTest(t, openStarted, "PersistOpen start")
				cancel()
				response := awaitPendingTest(t, done, "canceled waiter response")
				err := awaitPendingTest(t, permissionErr, "canceled waiter result")
				if waiterKind == "permission" {
					if !isDeniedPermissionOutcome(response) || !errors.Is(err, ErrPermissionDenied) {
						t.Fatalf("permission cancellation = %s, %v", response, err)
					}
				} else if string(response) != string(cancelledElicitation) || err != nil {
					t.Fatalf("elicitation cancellation = %s, %v", response, err)
				}
				select {
				case <-notified:
					t.Fatal("canceled request was published")
				default:
				}

				if commitRow {
					resolution := awaitPendingTest(t, closed, "committed-row cleanup")
					want := PermissionWithdrawn
					if waiterKind == "elicitation" {
						want = ElicitationCancelled
					}
					if resolution.Reason != want {
						t.Fatalf("cleanup reason = %q, want %q", resolution.Reason, want)
					}
					mu.Lock()
					left := rowExists
					mu.Unlock()
					if left {
						t.Fatal("committed pending row was left unresolved")
					}
				} else {
					select {
					case resolution := <-closed:
						t.Fatalf("no-row cleanup unexpectedly resolved %+v", resolution)
					default:
					}
				}
				select {
				case err := <-persistenceErrors:
					t.Fatalf("cancellation was reported as persistence failure: %v", err)
				default:
				}
			})
		}
	}
}

func TestCancellationCleanupReportsRealCloseFailures(t *testing.T) {
	for _, waiterKind := range []string{"permission", "elicitation"} {
		t.Run(waiterKind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			started := make(chan struct{})
			reported := make(chan error, 1)
			failure := errors.New("close store unavailable")
			if waiterKind == "permission" {
				w := NewPermissionWaiter(nil)
				w.PersistOpen = func(ctx context.Context, _ PendingRequestRecord) error {
					close(started)
					<-ctx.Done()
					return ctx.Err()
				}
				w.PersistClose = func(context.Context, PendingRequestResolution) error { return failure }
				w.OnPersistenceError = func(err error) { reported <- err }
				go func() { _, _ = w.Wait(ctx, "session", json.RawMessage(`{}`)) }()
			} else {
				w := NewElicitationWaiter(nil, nil)
				w.PersistOpen = func(ctx context.Context, _ PendingRequestRecord) error {
					close(started)
					<-ctx.Done()
					return ctx.Err()
				}
				w.PersistClose = func(context.Context, PendingRequestResolution) error { return failure }
				w.OnPersistenceError = func(err error) { reported <- err }
				go func() { _, _ = w.Wait(ctx, "session", json.RawMessage(`{}`)) }()
			}
			awaitPendingTest(t, started, "PersistOpen start")
			cancel()
			if got := awaitPendingTest(t, reported, "cleanup persistence error"); !errors.Is(got, failure) {
				t.Fatalf("reported error = %v, want %v", got, failure)
			}
		})
	}
}
