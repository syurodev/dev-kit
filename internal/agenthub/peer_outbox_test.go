package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/identity"
)

type failPeerClaimStore struct {
	*agenthub.SQLStore
	fail bool
}

type failPeerReadStore struct {
	*agenthub.SQLStore
	mu            sync.Mutex
	listErr       error
	getSessionErr error
}

func (s *failPeerReadStore) ListSessions(ctx context.Context) ([]agenthub.AgentSession, error) {
	s.mu.Lock()
	err := s.listErr
	s.listErr = nil
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.SQLStore.ListSessions(ctx)
}

func (s *failPeerReadStore) GetSession(ctx context.Context, id string) (agenthub.AgentSession, error) {
	s.mu.Lock()
	err := s.getSessionErr
	s.getSessionErr = nil
	s.mu.Unlock()
	if err != nil {
		return agenthub.AgentSession{}, err
	}
	return s.SQLStore.GetSession(ctx, id)
}

func (s *failPeerClaimStore) TransitionPeerOutbox(ctx context.Context, id, from, to, code string, at time.Time) (bool, error) {
	if s.fail && from == "queued" && to == "dispatching" {
		return false, errors.New("injected claim write failure")
	}
	return s.SQLStore.TransitionPeerOutbox(ctx, id, from, to, code, at)
}

func TestPeerOutboxRecoversQueuedMessageAndHonorsPausedUserQueue(t *testing.T) {
	r := newPeerRig(t)
	r.svc.SetPromptQueue(r.b.ID, 1, true)
	first, err := r.svc.SendMessage(identity.WithCaller(context.Background(), identity.UserTerminal()), agenthub.SendMessageInput{SessionID: r.b.ID, Message: "queued across restart"})
	if err != nil || first.Delivery != agenthub.DeliveryQueued {
		t.Fatalf("send: result=%+v err=%v", first, err)
	}

	reg := agenthub.NewRegistry()
	reg.Register(r.mock)
	restarted := agenthub.NewService(reg, r.store, nil)
	if err := restarted.RecoverPeerOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer restarted.Shutdown(context.Background())
	// Even an unrelated peer send and its own drain kick cannot release the
	// recovered queue before the window explicitly mirrors user queue state.
	second, err := restarted.SendMessage(agentCtx(r.a.ID), agenthub.SendMessageInput{SessionID: r.b.ID, Message: "arrived before queue mirror"})
	if err != nil || second.Delivery != agenthub.DeliveryQueued {
		t.Fatalf("send before queue mirror: result=%+v err=%v", second, err)
	}
	if _, err := restarted.Invoke(uiCtx(), agenthub.InvokeInput{SessionID: r.b.ID, Prompt: "person turn before queue mirror"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	idle := false
	for time.Now().Before(deadline) {
		status, err := restarted.GetStatus(uiCtx(), r.b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.Status == agenthub.SessionStatusIdle {
			idle = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !idle {
		t.Fatal("person turn did not finish before queue mirror")
	}
	statuses, err := r.store.ListPeerOutboxStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.ID == first.MessageID && status.State != string(agenthub.DeliveryQueued) {
			t.Fatalf("turn completion woke an unmirrored recovered queue: %+v", status)
		}
	}
	restarted.SetPromptQueue(r.b.ID, 1, true)
	if got, err := restarted.GetStatus(uiCtx(), r.b.ID); err != nil || got.Status != agenthub.SessionStatusIdle {
		t.Fatalf("recovered queue started behind pause: status=%v err=%v", got.Status, err)
	}
	restarted.SetPromptQueue(r.b.ID, 0, false)
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := r.store.ListTranscript(context.Background(), r.b.ID)
		if err != nil {
			t.Fatal(err)
		}
		seq := map[string]int64{}
		sources := map[string]string{}
		for _, row := range rows {
			var update agenthub.SessionUpdate
			if json.Unmarshal(row.Payload, &update) == nil && (update.MessageID == first.MessageID || update.MessageID == second.MessageID) {
				seq[update.MessageID] = row.Seq
				if update.Source != nil {
					sources[update.MessageID] = update.Source.Kind
				}
			}
		}
		if seq[first.MessageID] != 0 && seq[second.MessageID] != 0 {
			if seq[first.MessageID] >= seq[second.MessageID] {
				t.Fatalf("newer message overtook recovered message: first=%d second=%d", seq[first.MessageID], seq[second.MessageID])
			}
			if sources[first.MessageID] != "terminal" || sources[second.MessageID] != "session" {
				t.Fatalf("recovered source was not preserved: first=%q second=%q", sources[first.MessageID], sources[second.MessageID])
			}
			statuses, err := r.store.ListPeerOutboxStatuses(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range statuses {
				if status.ID == first.MessageID && status.State != string(agenthub.DeliveryStarted) {
					t.Fatalf("queue completion regressed to %q", status.State)
				}
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("recovered peer queue did not deliver both messages after the user queue cleared")
}

func TestPeerOutboxRecoveryExpiresAndRefusesClosedTargets(t *testing.T) {
	r := newPeerRig(t)
	if err := r.svc.CloseSession(uiCtx(), r.c.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []agenthub.PeerOutboxRecord{
		{ID: "expired-item", From: agenthub.MessageSource{Kind: "terminal", MessageID: "expired-item"}, To: r.b.ID, Text: "old", At: now.Add(-2 * time.Hour), UpdatedAt: now, State: "queued", Limit: agenthub.MaxPeerHops, Relation: "terminal"},
		{ID: "closed-item", From: agenthub.MessageSource{Kind: "terminal", MessageID: "closed-item"}, To: r.c.ID, Text: "closed", At: now, UpdatedAt: now, State: "queued", Limit: agenthub.MaxPeerHops, Relation: "terminal"},
	}
	for _, row := range rows {
		if err := r.store.SavePeerOutbox(context.Background(), row); err != nil {
			t.Fatal(err)
		}
	}
	restarted := agenthub.NewService(agenthub.NewRegistry(), r.store, nil)
	if err := restarted.RecoverPeerOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := r.store.ListPeerOutboxStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, row := range stored {
		states[row.ID] = row.State + ":" + row.Code
	}
	if states["expired-item"] != "expired:"+agenthub.PeerUndelivery {
		t.Fatalf("expired item state = %q", states["expired-item"])
	}
	if states["closed-item"] != "refused:"+agenthub.PeerClosed {
		t.Fatalf("closed item state = %q", states["closed-item"])
	}
}

func TestPeerOutboxCrashDispatchStaysAmbiguousAndHopGuardSurvivesRestart(t *testing.T) {
	r := newPeerRig(t)
	r.svc.SetPromptQueue(r.b.ID, 1, true)
	queued := r.send(r.a, r.b, "first", "")
	if queued.Delivery != agenthub.DeliveryQueued || queued.Hop != 1 {
		t.Fatalf("first send = %+v", queued)
	}
	now := time.Now().UTC()
	if err := r.store.SavePeerOutbox(context.Background(), agenthub.PeerOutboxRecord{
		ID: "crashed-dispatch", From: agenthub.MessageSource{Kind: "terminal", MessageID: "crashed-dispatch"}, To: r.b.ID,
		Text: "maybe dispatched", At: now, UpdatedAt: now, State: "dispatching", Limit: agenthub.MaxPeerHops, Relation: "terminal",
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.store.SavePeerOutbox(context.Background(), agenthub.PeerOutboxRecord{
		ID: "crashed-dispatch", From: agenthub.MessageSource{Kind: "terminal", MessageID: "crashed-dispatch"}, To: r.b.ID,
		Text: "duplicate must not replace original", At: now, UpdatedAt: now, State: "dispatching", Limit: agenthub.MaxPeerHops, Relation: "terminal",
	}); err != nil {
		t.Fatal(err)
	}
	active, err := r.store.ListPeerOutbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range active {
		if row.ID == "crashed-dispatch" && row.Text != "maybe dispatched" {
			t.Fatalf("stable message id was not idempotent: %+v", row)
		}
	}
	reg := agenthub.NewRegistry()
	reg.Register(r.mock)
	restarted := agenthub.NewService(reg, r.store, nil)
	if err := restarted.RecoverPeerOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer restarted.Shutdown(context.Background())
	rows, err := r.store.ListPeerOutboxStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "crashed-dispatch" && (row.State != "ambiguous" || row.Code != "dispatch_outcome_unknown") {
			t.Fatalf("crashed dispatch must not be blindly replayed: %+v", row)
		}
	}
	second, err := restarted.SendMessage(identity.WithCaller(context.Background(), identity.Agent(r.b.ID)), agenthub.SendMessageInput{SessionID: r.a.ID, Message: "reply"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Hop != 2 {
		t.Fatalf("restart reset pair hop guard: %+v", second)
	}
}

func TestPeerRateGuardSurvivesRestart(t *testing.T) {
	r := newPeerRig(t)
	r.svc.SetPromptQueue(r.b.ID, 1, true)
	for i := 0; i < 5; i++ {
		res := r.send(r.a, r.b, "rate window", "")
		if res.Delivery != agenthub.DeliveryQueued {
			t.Fatalf("send %d = %+v", i, res)
		}
	}
	reg := agenthub.NewRegistry()
	reg.Register(r.mock)
	restarted := agenthub.NewService(reg, r.store, nil)
	if err := restarted.RecoverPeerOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer restarted.Shutdown(context.Background())
	res, err := restarted.SendMessage(agentCtx(r.a.ID), agenthub.SendMessageInput{SessionID: r.b.ID, Message: "over the limit"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Delivery != agenthub.DeliveryRefused || res.Code != agenthub.PeerRate {
		t.Fatalf("restart reset rate guard: %+v", res)
	}
}

func TestNewPeerSendStaysBehindAnExistingQueueWhenItStartsDraining(t *testing.T) {
	r := newPeerRig(t)
	r.svc.SetPromptQueue(r.b.ID, 1, true)
	first, err := r.svc.SendMessage(agentCtx(r.a.ID), agenthub.SendMessageInput{SessionID: r.b.ID, Message: "older queued message"})
	if err != nil || first.Delivery != agenthub.DeliveryQueued {
		t.Fatalf("first send: result=%+v err=%v", first, err)
	}
	r.svc.SetPromptQueue(r.b.ID, 0, false) // starts the asynchronous drain
	second, err := r.svc.SendMessage(agentCtx(r.a.ID), agenthub.SendMessageInput{SessionID: r.b.ID, Message: "newer message"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Delivery == agenthub.DeliveryStarted {
		t.Fatalf("new send overtook an existing queue: %+v", second)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := r.store.ListTranscript(context.Background(), r.b.ID)
		if err != nil {
			t.Fatal(err)
		}
		seq := map[string]int64{}
		for _, row := range rows {
			var update agenthub.SessionUpdate
			if json.Unmarshal(row.Payload, &update) == nil && (update.MessageID == first.MessageID || update.MessageID == second.MessageID) {
				seq[update.MessageID] = row.Seq
			}
		}
		if seq[first.MessageID] != 0 && seq[second.MessageID] != 0 {
			if seq[first.MessageID] >= seq[second.MessageID] {
				t.Fatalf("new message arrived before existing queue: first=%d second=%d", seq[first.MessageID], seq[second.MessageID])
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("peer queue did not drain both messages")
}

func TestPeerOutboxDoesNotReuseApprovalAfterWorkspaceScopeChanges(t *testing.T) {
	r := newPeerRig(t)
	r.svc.SetPromptQueue(r.c.ID, 1, true)
	res, err := r.svc.SendMessage(identity.WithCaller(context.Background(), identity.Agent(r.a.ID)), agenthub.SendMessageInput{
		SessionID: r.c.ID, Message: "approved exact message", CrossWorkspaceApproved: true,
	})
	if err != nil || res.Delivery != agenthub.DeliveryQueued {
		t.Fatalf("send: result=%+v err=%v", res, err)
	}
	target, err := r.store.GetSession(context.Background(), r.c.ID)
	if err != nil {
		t.Fatal(err)
	}
	target.WorkspaceID = "ws1"
	if err := r.store.UpdateSession(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	restarted := agenthub.NewService(agenthub.NewRegistry(), r.store, nil)
	if err := restarted.RecoverPeerOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := r.store.ListPeerOutboxStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == res.MessageID {
			if row.State != "refused" || row.Code != agenthub.PeerNotFound {
				t.Fatalf("workspace change reused an old approval: %+v", row)
			}
			return
		}
	}
	t.Fatal("accepted message was not persisted")
}

func TestPeerOutboxFailedClaimDoesNotStartTurn(t *testing.T) {
	r := newPeerRig(t)
	r.svc.SetPromptQueue(r.b.ID, 1, true)
	queued := r.send(r.a, r.b, "claim failure", "")
	if queued.Delivery != agenthub.DeliveryQueued {
		t.Fatalf("send = %+v", queued)
	}
	store := &failPeerClaimStore{SQLStore: r.store, fail: true}
	reg := agenthub.NewRegistry()
	reg.Register(r.mock)
	restarted := agenthub.NewService(reg, store, nil)
	if err := restarted.RecoverPeerOutbox(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := restarted.WaitQuiet(ctx); err != nil {
			t.Errorf("restarted service did not become quiet: %v", err)
		}
		if err := restarted.Shutdown(ctx); err != nil {
			t.Errorf("shutdown restarted service: %v", err)
		}
	}()
	restarted.SetPromptQueue(r.b.ID, 1, true)
	restarted.SetPromptQueue(r.b.ID, 0, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := restarted.WaitQuiet(ctx); err != nil {
		t.Fatalf("drain did not finish: %v", err)
	}
	got, err := r.store.ListPeerOutboxStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range got {
		if row.ID == queued.MessageID && row.State != "queued" {
			t.Fatalf("claim failure changed durable queue state: %+v", row)
		}
	}
	if status, err := restarted.GetStatus(uiCtx(), r.b.ID); err != nil || status.Status != agenthub.SessionStatusIdle {
		t.Fatalf("claim failure started target: status=%v err=%v", status.Status, err)
	}
}

func TestPeerOutboxStoreReadErrorsPreserveQueuedMessages(t *testing.T) {
	t.Run("recovery retries session listing", func(t *testing.T) {
		r := newPeerRig(t)
		r.svc.SetPromptQueue(r.b.ID, 1, true)
		queued := r.send(r.a, r.b, "recovery read failure", "")

		store := &failPeerReadStore{SQLStore: r.store, listErr: errors.New("injected list failure")}
		reg := agenthub.NewRegistry()
		reg.Register(r.mock)
		restarted := agenthub.NewService(reg, store, nil)
		defer restarted.Shutdown(context.Background())
		if err := restarted.RecoverPeerOutbox(context.Background()); err == nil {
			t.Fatal("expected injected recovery read error")
		}
		assertPeerOutboxState(t, r.store, queued.MessageID, "queued")
		if err := restarted.RecoverPeerOutbox(context.Background()); err != nil {
			t.Fatalf("retry recovery: %v", err)
		}
		assertPeerOutboxState(t, r.store, queued.MessageID, "queued")
	})

	t.Run("drain retries scope read", func(t *testing.T) {
		r := newPeerRig(t)
		r.svc.SetPromptQueue(r.b.ID, 1, true)
		queued := r.send(r.a, r.b, "drain read failure", "")

		store := &failPeerReadStore{SQLStore: r.store}
		reg := agenthub.NewRegistry()
		reg.Register(r.mock)
		restarted := agenthub.NewService(reg, store, nil)
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := restarted.WaitQuiet(ctx); err != nil {
				t.Errorf("restarted service did not become quiet: %v", err)
			}
			_ = restarted.Shutdown(ctx)
		}()
		if err := restarted.RecoverPeerOutbox(context.Background()); err != nil {
			t.Fatal(err)
		}
		store.mu.Lock()
		store.listErr = errors.New("injected drain scope read failure")
		store.mu.Unlock()
		restarted.SetPromptQueue(r.b.ID, 0, false)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := restarted.WaitQuiet(ctx); err != nil {
			t.Fatalf("first drain did not finish: %v", err)
		}
		assertPeerOutboxState(t, r.store, queued.MessageID, "queued")
		if status, err := restarted.GetStatus(uiCtx(), r.b.ID); err != nil || status.Status != agenthub.SessionStatusIdle {
			t.Fatalf("scope read failure started target: status=%v err=%v", status.Status, err)
		}
		// A fresh user queue mirror is the next explicit drain opportunity.
		restarted.SetPromptQueue(r.b.ID, 0, false)
		waitForPeerOutboxState(t, r.store, queued.MessageID, "started")
	})
}

func TestPeerOutboxKnownPreDispatchErrorIsRefused(t *testing.T) {
	r := newPeerRig(t)
	store := &failPeerReadStore{SQLStore: r.store, getSessionErr: errors.New("injected target read failure")}
	reg := agenthub.NewRegistry()
	reg.Register(r.mock)
	svc := agenthub.NewService(reg, store, nil)
	defer svc.Shutdown(context.Background())
	result, err := svc.SendMessage(identity.WithCaller(context.Background(), identity.UserTerminal()), agenthub.SendMessageInput{SessionID: r.b.ID, Message: "known pre-dispatch failure"})
	if err == nil {
		t.Fatalf("expected target read failure, got result %+v", result)
	}
	statuses, listErr := r.store.ListPeerOutboxStatuses(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(statuses) != 1 || statuses[0].State != "refused" || statuses[0].Code != agenthub.PeerUndelivery {
		t.Fatalf("known pre-dispatch failure was not durably refused: %+v", statuses)
	}
}

func assertPeerOutboxState(t *testing.T, store *agenthub.SQLStore, id, want string) {
	t.Helper()
	statuses, err := store.ListPeerOutboxStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.ID == id {
			if status.State != want {
				t.Fatalf("outbox state = %q, want %q: %+v", status.State, want, status)
			}
			return
		}
	}
	t.Fatalf("outbox row %q not found", id)
}

func waitForPeerOutboxState(t *testing.T, store *agenthub.SQLStore, id, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		statuses, err := store.ListPeerOutboxStatuses(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, status := range statuses {
			if status.ID == id && status.State == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("outbox row %q did not reach %q", id, want)
}
