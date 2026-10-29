package agenthub_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"devkit/internal/agenthub"
)

func TestAddExtraReadWritePathAppendsOnceAndKeepsWhatWasThere(t *testing.T) {
	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	ctx := context.Background()
	first := t.TempDir()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir(), ExtraReadWritePaths: []string{first}})
	if err != nil {
		t.Fatal(err)
	}
	added := t.TempDir()
	if ok, err := st.AddExtraReadWritePath(ctx, sess.ID, added); err != nil || !ok {
		t.Fatalf("first add = %v, %v", ok, err)
	}
	if ok, err := st.AddExtraReadWritePath(ctx, sess.ID, added); err != nil || ok {
		t.Fatalf("second add = %v, %v; want false (already listed)", ok, err)
	}
	got, err := st.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{first, added}; !slices.Equal(got.ExtraReadWritePaths, want) {
		t.Fatalf("extras = %v, want %v", got.ExtraReadWritePaths, want)
	}
}

func TestAddExtraReadWritePathToAnUnknownSession(t *testing.T) {
	_, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	if _, err := st.AddExtraReadWritePath(context.Background(), "no-such-session", "/w/x"); !errors.Is(err, agenthub.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestAddExtraReadWritePathDoesNotLoseARacingWriter(t *testing.T) {
	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	ctx := context.Background()
	sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	const n = 6
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.AddExtraReadWritePath(ctx, sess.ID, fmt.Sprintf("/w/folder-%d", i)); err != nil {
				t.Errorf("add %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	got, err := st.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ExtraReadWritePaths) != n {
		t.Fatalf("extras = %v, want all %d folders", got.ExtraReadWritePaths, n)
	}
}
