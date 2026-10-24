package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/storage"
)

func pendingSQLStore(t *testing.T) (*SQLStore, *sql.DB) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLStore(db), db
}

func TestWaiterPersistenceStoresMetadataOnlyAndTerminalReasons(t *testing.T) {
	store, db := pendingSQLStore(t)
	ctx := context.Background()

	permissionOpen := make(chan PendingPermission, 1)
	permission := NewPermissionWaiter(func(p PendingPermission) { permissionOpen <- p })
	permission.Timeout = time.Minute
	permission.PersistOpen = store.RecordPendingRequest
	permission.PersistClose = store.ResolvePendingRequest
	allow := json.RawMessage(`{"outcome":{"outcome":"selected"}}`)
	permissionDone := make(chan error, 1)
	go func() {
		_, err := permission.Wait(ctx, "session-answer", json.RawMessage(`{"question":"SECRET QUESTION"}`))
		permissionDone <- err
	}()
	answeredPermission := <-permissionOpen
	if err := permission.Complete(answeredPermission.ID, allow); err != nil {
		t.Fatal(err)
	}
	if err := <-permissionDone; err != nil {
		t.Fatal(err)
	}

	expiringCtx, expire := context.WithTimeout(ctx, 30*time.Millisecond)
	defer expire()
	expiredOpen := make(chan PendingPermission, 1)
	expired := NewPermissionWaiter(func(p PendingPermission) { expiredOpen <- p })
	expired.PersistOpen = store.RecordPendingRequest
	expired.PersistClose = store.ResolvePendingRequest
	expiredDone := make(chan error, 1)
	go func() {
		_, err := expired.Wait(expiringCtx, "session-expired", json.RawMessage(`{"password":"SECRET VALUE"}`))
		expiredDone <- err
	}()
	expiredPermission := <-expiredOpen
	<-expiredDone

	withdrawCtx, withdraw := context.WithCancel(ctx)
	withdrawnOpen := make(chan PendingPermission, 1)
	withdrawn := NewPermissionWaiter(func(p PendingPermission) { withdrawnOpen <- p })
	withdrawn.PersistOpen = store.RecordPendingRequest
	withdrawn.PersistClose = store.ResolvePendingRequest
	withdrawnDone := make(chan error, 1)
	go func() {
		_, err := withdrawn.Wait(withdrawCtx, "session-withdrawn", json.RawMessage(`{"token":"DO NOT STORE"}`))
		withdrawnDone <- err
	}()
	withdrawnPermission := <-withdrawnOpen
	withdraw()
	<-withdrawnDone

	elicitationOpen := make(chan PendingElicitation, 1)
	elicitation := NewElicitationWaiter(func(p PendingElicitation) { elicitationOpen <- p }, nil)
	elicitation.PersistOpen = store.RecordPendingRequest
	elicitation.PersistClose = store.ResolvePendingRequest
	elicitationDone := make(chan error, 1)
	go func() {
		_, err := elicitation.Wait(ctx, "session-question", json.RawMessage(`{"message":"PRIVATE QUESTION","requestedSchema":{"secret":"PRIVATE"}}`))
		elicitationDone <- err
	}()
	question := <-elicitationOpen
	if err := elicitation.Complete(question.ID, json.RawMessage(`{"action":"accept","content":{"answer":"PRIVATE ANSWER"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := <-elicitationDone; err != nil {
		t.Fatal(err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	cancelledOpen := make(chan PendingElicitation, 1)
	cancelled := NewElicitationWaiter(func(p PendingElicitation) { cancelledOpen <- p }, nil)
	cancelled.PersistOpen = store.RecordPendingRequest
	cancelled.PersistClose = store.ResolvePendingRequest
	cancelledDone := make(chan error, 1)
	go func() {
		_, err := cancelled.Wait(cancelCtx, "session-question-cancelled", json.RawMessage(`{"message":"another private question"}`))
		cancelledDone <- err
	}()
	cancelledQuestion := <-cancelledOpen
	cancel()
	if err := <-cancelledDone; err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		answeredPermission.ID: "answered", expiredPermission.ID: "expired", withdrawnPermission.ID: "withdrawn",
		question.ID: "answered", cancelledQuestion.ID: "cancelled",
	}
	for id, reason := range want {
		var resolution, gotReason string
		if err := db.QueryRow(`SELECT resolution, reason FROM agent_pending_requests WHERE id = ?`, id).Scan(&resolution, &gotReason); err != nil {
			t.Fatal(err)
		}
		if resolution != "resolved" || gotReason != reason {
			t.Errorf("request %s = %s/%s, want resolved/%s", id, resolution, gotReason, reason)
		}
	}
	var createdAt, expiresAt string
	if err := db.QueryRow(`SELECT created_at, expires_at FROM agent_pending_requests WHERE id = ?`, answeredPermission.ID).Scan(&createdAt, &expiresAt); err != nil || createdAt == "" || expiresAt == "" {
		t.Fatalf("permission timestamps = %q/%q, err=%v", createdAt, expiresAt, err)
	}
	cols, err := db.Query(`PRAGMA table_info(agent_pending_requests)`)
	if err != nil {
		t.Fatal(err)
	}
	defer cols.Close()
	var names []string
	for cols.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := cols.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	for _, forbidden := range []string{"params", "question", "answer", "secret", "payload"} {
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), forbidden) {
				t.Fatalf("metadata schema includes forbidden payload column %q", name)
			}
		}
	}
	rows, err := db.Query(`SELECT id, session_id, kind, created_at, COALESCE(expires_at,''), resolution, reason FROM agent_pending_requests`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var row strings.Builder
		var id, sessionID, kind, createdAt, expiresAt, resolution, reason string
		if err := rows.Scan(&id, &sessionID, &kind, &createdAt, &expiresAt, &resolution, &reason); err != nil {
			t.Fatal(err)
		}
		row.WriteString(id + sessionID + kind + createdAt + expiresAt + resolution + reason)
		for _, secret := range []string{"SECRET QUESTION", "SECRET VALUE", "DO NOT STORE", "PRIVATE QUESTION", "PRIVATE ANSWER"} {
			if strings.Contains(row.String(), secret) {
				t.Fatalf("stored pending metadata contains request text %q", secret)
			}
		}
	}
}

func TestPendingRequestRecoveryExplainsAndInvalidatesOldPermissionAndQuestionIDs(t *testing.T) {
	store, _ := pendingSQLStore(t)
	ctx := context.Background()
	reg := NewRegistry()
	reg.Register(NewMockAdapter())
	svc := NewService(reg, store, nil)
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSessionStatus(ctx, sess.ID, SessionStatusAwaitingPermission, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPendingRequest(ctx, PendingRequestRecord{ID: "old-permission", SessionID: sess.ID, Kind: PendingRequestPermission, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPendingRequest(ctx, PendingRequestRecord{ID: "old-question", SessionID: sess.ID, Kind: PendingRequestElicitation, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(reg, store, nil)
	if err := restarted.RecoverStaleSessions(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.GetStatus(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SessionStatusIdle || got.Stop == nil || !strings.Contains(got.Stop.Message, "permission approval and a question") || !strings.Contains(got.Stop.Message, "can no longer be answered") {
		t.Fatalf("recovered session = %+v; want explicit sanitized wait interruption", got)
	}
	for _, id := range []string{"old-permission", "old-question"} {
		if err := store.ResolvePendingRequest(ctx, PendingRequestResolution{ID: id, Reason: "answered", ResolvedAt: time.Now().UTC()}); !errors.Is(err, ErrPendingRequestNotFound) {
			t.Errorf("old request %s remained answerable: %v", id, err)
		}
	}
	rows, err := store.ListPendingRequestSummaries(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("pending after recovery = %+v, %v", rows, err)
	}
}

func TestWaitersFailClosedWhenPendingMetadataCannotBeStored(t *testing.T) {
	writeErr := errors.New("disk full")
	permissionNotified := false
	permissionReported := false
	permission := NewPermissionWaiter(func(PendingPermission) { permissionNotified = true })
	permission.OnPersistenceError = func(error) { permissionReported = true }
	permission.PersistOpen = func(context.Context, PendingRequestRecord) error { return writeErr }
	got, err := permission.Wait(context.Background(), "s", json.RawMessage(`{"permission":"write"}`))
	if !errors.Is(err, ErrPermissionDenied) || !isDeniedPermissionOutcome(got) || permissionNotified || len(permission.PendingList()) != 0 || !permissionReported {
		t.Fatalf("permission open failure = %s, %v, notified=%v", got, err, permissionNotified)
	}
	elicitationNotified := false
	elicitationReported := false
	elicitation := NewElicitationWaiter(func(PendingElicitation) { elicitationNotified = true }, nil)
	elicitation.OnPersistenceError = func(error) { elicitationReported = true }
	elicitation.PersistOpen = func(context.Context, PendingRequestRecord) error { return writeErr }
	got, err = elicitation.Wait(context.Background(), "s", json.RawMessage(`{"message":"private"}`))
	if err != nil || string(got) != string(declinedElicitation) || elicitationNotified || len(elicitation.PendingList()) != 0 || !elicitationReported {
		t.Fatalf("elicitation open failure = %s, %v, notified=%v", got, err, elicitationNotified)
	}
}

func TestWaitersCannotReleaseAnswersWhenResolutionPersistenceFails(t *testing.T) {
	writeErr := errors.New("disk full")
	permissionOpen := make(chan PendingPermission, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	permission := NewPermissionWaiter(func(p PendingPermission) { permissionOpen <- p })
	permission.PersistOpen = func(context.Context, PendingRequestRecord) error { return nil }
	permission.PersistClose = func(context.Context, PendingRequestResolution) error { return writeErr }
	permissionDone := make(chan json.RawMessage, 1)
	go func() { out, _ := permission.Wait(ctx, "session", json.RawMessage(`{}`)); permissionDone <- out }()
	p := <-permissionOpen
	if err := permission.Complete(p.ID, json.RawMessage(`{"outcome":{"outcome":"selected"}}`)); !errors.Is(err, writeErr) {
		t.Fatalf("permission completion error = %v", err)
	}
	select {
	case out := <-permissionDone:
		t.Fatalf("permission answer escaped failed persistence: %s", out)
	default:
	}
	if len(permission.PendingList()) != 1 {
		t.Fatal("permission was removed after failed resolution write")
	}
	cancel()
	<-permissionDone

	questionOpen := make(chan PendingElicitation, 1)
	questionCtx, stopQuestion := context.WithCancel(context.Background())
	defer stopQuestion()
	question := NewElicitationWaiter(func(p PendingElicitation) { questionOpen <- p }, nil)
	question.PersistOpen = func(context.Context, PendingRequestRecord) error { return nil }
	question.PersistClose = func(context.Context, PendingRequestResolution) error { return writeErr }
	questionDone := make(chan json.RawMessage, 1)
	go func() {
		out, _ := question.Wait(questionCtx, "session", json.RawMessage(`{"message":"secret"}`))
		questionDone <- out
	}()
	q := <-questionOpen
	if err := question.Complete(q.ID, json.RawMessage(`{"action":"accept"}`)); !errors.Is(err, writeErr) {
		t.Fatalf("elicitation completion error = %v", err)
	}
	select {
	case out := <-questionDone:
		t.Fatalf("elicitation answer escaped failed persistence: %s", out)
	default:
	}
	if len(question.PendingList()) != 1 {
		t.Fatal("question was removed after failed resolution write")
	}
	stopQuestion()
	<-questionDone
}

type failPendingMarkStore struct {
	*SQLStore
	fail bool
}

func (s *failPendingMarkStore) MarkPendingRequestsInterrupted(ctx context.Context, sessionID string, at time.Time, reason string) error {
	if s.fail {
		return errors.New("metadata write failed")
	}
	return s.SQLStore.MarkPendingRequestsInterrupted(ctx, sessionID, at, reason)
}

func TestRecoveryKeepsSessionBusyWhenOldRequestCannotBeInvalidated(t *testing.T) {
	store, _ := pendingSQLStore(t)
	ctx := context.Background()
	reg := NewRegistry()
	reg.Register(NewMockAdapter())
	svc := NewService(reg, store, nil)
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSessionStatus(ctx, sess.ID, SessionStatusAwaitingPermission, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPendingRequest(ctx, PendingRequestRecord{ID: "old", SessionID: sess.ID, Kind: PendingRequestPermission, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	failingStore := &failPendingMarkStore{SQLStore: store, fail: true}
	restarted := NewService(reg, failingStore, nil)
	if err := restarted.RecoverStaleSessions(ctx); err == nil {
		t.Fatal("expected pending-request persistence failure")
	}
	got, err := store.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SessionStatusAwaitingPermission {
		t.Fatalf("status = %s, want still busy until old request is invalidated", got.Status)
	}
	failingStore.fail = false
	if err := restarted.RecoverStaleSessions(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetSession(ctx, sess.ID)
	if err != nil || got.Status != SessionStatusIdle || got.Stop == nil || !strings.Contains(got.Stop.Message, "permission approval") {
		t.Fatalf("recovery retry = %+v, err=%v", got, err)
	}
	entries, err := store.ListTranscript(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	notices := 0
	for _, entry := range entries {
		var update SessionUpdate
		if json.Unmarshal(entry.Payload, &update) == nil && update.Failure != nil && strings.HasPrefix(update.Failure.ID, "interrupted:") && strings.HasSuffix(update.Failure.ID, ":"+sess.ID) {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("interruption notices = %d, want one after retry", notices)
	}
	if err := store.UpdateSessionStatus(ctx, sess.ID, SessionStatusRunning, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	newProcess := NewService(reg, store, nil)
	if err := newProcess.RecoverStaleSessions(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err = store.ListTranscript(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, entry := range entries {
		var update SessionUpdate
		if json.Unmarshal(entry.Payload, &update) == nil && update.Failure != nil && strings.HasPrefix(update.Failure.ID, "interrupted:") && strings.HasSuffix(update.Failure.ID, ":"+sess.ID) {
			ids[update.Failure.ID] = true
		}
	}
	if len(ids) != 2 {
		t.Fatalf("distinct restart notices = %v, want two distinct recovery markers", ids)
	}
}

func TestRecoveryInvalidatesClosedAndOrphanRequestsWithoutChangingClosedStop(t *testing.T) {
	store, _ := pendingSQLStore(t)
	ctx := context.Background()
	reg := NewRegistry()
	reg.Register(NewMockAdapter())
	svc := NewService(reg, store, nil)
	closed, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	oldStop := SessionStop{Kind: StopCancelled, Retryable: true, Since: time.Now().UTC(), Message: "keep this stop"}
	if err := store.UpdateSessionStop(ctx, closed.ID, &oldStop, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateSessionStatus(ctx, closed.ID, SessionStatusClosed, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPendingRequest(ctx, PendingRequestRecord{ID: "closed-wait", SessionID: closed.ID, Kind: PendingRequestPermission, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordPendingRequest(ctx, PendingRequestRecord{ID: "orphan-wait", SessionID: "deleted-session", Kind: PendingRequestElicitation, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	restarted := NewService(reg, store, nil)
	if err := restarted.RecoverStaleSessions(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, closed.ID)
	if err != nil || got.Status != SessionStatusClosed || got.Stop == nil || got.Stop.Message != oldStop.Message {
		t.Fatalf("closed session changed: %+v, err=%v", got, err)
	}
	rows, err := store.ListPendingRequestSummaries(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("orphan/closed requests still pending: %+v, %v", rows, err)
	}
	entries, err := store.ListTranscript(ctx, closed.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		var update SessionUpdate
		if json.Unmarshal(entry.Payload, &update) == nil && update.Failure != nil && update.Failure.Category == "interrupted" {
			t.Fatal("closed session received a fake interrupted-running banner")
		}
	}
}

func TestPendingRequestRetentionKeepsActiveAndUnacknowledgedRecoveryRows(t *testing.T) {
	store, db := pendingSQLStore(t)
	ctx := context.Background()
	old := time.Now().UTC().Add(-90 * 24 * time.Hour)
	for _, r := range []PendingRequestRecord{
		{ID: "old-resolved", SessionID: "a", Kind: PendingRequestPermission, CreatedAt: old},
		{ID: "old-recovery", SessionID: "b", Kind: PendingRequestElicitation, CreatedAt: old},
		{ID: "old-pending", SessionID: "c", Kind: PendingRequestPermission, CreatedAt: old},
	} {
		if err := store.RecordPendingRequest(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ResolvePendingRequest(ctx, PendingRequestResolution{ID: "old-resolved", Reason: "answered", ResolvedAt: old}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPendingRequestsInterrupted(ctx, "b", old, "app_restarted"); err != nil {
		t.Fatal(err)
	}
	if err := store.PrunePendingRequests(ctx, time.Now().UTC().Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"old-resolved": false, "old-recovery": true, "old-pending": true} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM agent_pending_requests WHERE id = ?`, id).Scan(&n); err != nil || (n == 1) != want {
			t.Errorf("row %s retained=%v, err=%v; want %v", id, n == 1, err, want)
		}
	}
	if err := store.AcknowledgePendingRequestRecovery(ctx, "b"); err != nil {
		t.Fatal(err)
	}
	if err := store.PrunePendingRequests(ctx, time.Now().UTC().Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_pending_requests WHERE id = 'old-recovery'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("acknowledged old recovery row count=%d err=%v", n, err)
	}
}

func TestWaitersRecheckCancellationAfterBlockedPersistence(t *testing.T) {
	t.Run("permission open", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		started, release, notified := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
		w := NewPermissionWaiter(func(PendingPermission) { notified <- struct{}{} })
		w.PersistOpen = func(context.Context, PendingRequestRecord) error { close(started); <-release; return nil }
		done := make(chan json.RawMessage, 1)
		go func() { out, _ := w.Wait(ctx, "s", json.RawMessage(`{"secret":"never persist"}`)); done <- out }()
		<-started
		cancel()
		close(release)
		if out := <-done; !isDeniedPermissionOutcome(out) {
			t.Fatalf("permission open after cancel = %s", out)
		}
		select {
		case <-notified:
			t.Fatal("canceled request was published")
		default:
		}
	})

	t.Run("permission close", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		opened := make(chan PendingPermission, 1)
		closeStarted, release := make(chan struct{}), make(chan struct{})
		var mu sync.Mutex
		var reasons []string
		w := NewPermissionWaiter(func(p PendingPermission) { opened <- p })
		w.PersistOpen = func(context.Context, PendingRequestRecord) error { return nil }
		w.PersistClose = func(_ context.Context, r PendingRequestResolution) error {
			mu.Lock()
			reasons = append(reasons, r.Reason)
			mu.Unlock()
			if r.Reason == PermissionAnswered {
				close(closeStarted)
				<-release
			}
			return nil
		}
		waitDone := make(chan json.RawMessage, 1)
		go func() { out, _ := w.Wait(ctx, "s", json.RawMessage(`{}`)); waitDone <- out }()
		p := <-opened
		completeDone := make(chan error, 1)
		go func() { completeDone <- w.Complete(p.ID, json.RawMessage(`{"outcome":{"outcome":"selected"}}`)) }()
		<-closeStarted
		cancel()
		close(release)
		if err := <-completeDone; !errors.Is(err, ErrPermissionNotWaiting) {
			t.Fatalf("complete after cancel = %v", err)
		}
		if out := <-waitDone; !isDeniedPermissionOutcome(out) {
			t.Fatalf("permission answer escaped after cancel: %s", out)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(reasons) != 2 || reasons[0] != PermissionAnswered || reasons[1] != PermissionWithdrawn {
			t.Fatalf("stored close reasons = %v", reasons)
		}
	})

	t.Run("permission deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		opened := make(chan PendingPermission, 1)
		closeStarted, release := make(chan struct{}), make(chan struct{})
		var mu sync.Mutex
		var reasons []string
		w := NewPermissionWaiter(func(p PendingPermission) { opened <- p })
		w.PersistOpen = func(context.Context, PendingRequestRecord) error { return nil }
		w.PersistClose = func(_ context.Context, r PendingRequestResolution) error {
			mu.Lock()
			reasons = append(reasons, r.Reason)
			mu.Unlock()
			if r.Reason == PermissionAnswered {
				close(closeStarted)
				<-release
			}
			return nil
		}
		waitDone := make(chan json.RawMessage, 1)
		go func() { out, _ := w.Wait(ctx, "s", json.RawMessage(`{}`)); waitDone <- out }()
		p := <-opened
		completeDone := make(chan error, 1)
		go func() { completeDone <- w.Complete(p.ID, json.RawMessage(`{"outcome":{"outcome":"selected"}}`)) }()
		<-closeStarted
		<-ctx.Done()
		close(release)
		if err := <-completeDone; !errors.Is(err, ErrPermissionNotWaiting) {
			t.Fatalf("complete after expiry = %v", err)
		}
		if out := <-waitDone; !isDeniedPermissionOutcome(out) {
			t.Fatalf("permission answer escaped after expiry: %s", out)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(reasons) != 2 || reasons[0] != PermissionAnswered || reasons[1] != PermissionExpired {
			t.Fatalf("stored close reasons = %v", reasons)
		}
	})

	t.Run("elicitation open", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		started, release, notified := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
		w := NewElicitationWaiter(func(PendingElicitation) { notified <- struct{}{} }, nil)
		w.PersistOpen = func(context.Context, PendingRequestRecord) error { close(started); <-release; return nil }
		done := make(chan json.RawMessage, 1)
		go func() { out, _ := w.Wait(ctx, "s", json.RawMessage(`{"message":"private"}`)); done <- out }()
		<-started
		cancel()
		close(release)
		if out := <-done; string(out) != string(cancelledElicitation) {
			t.Fatalf("elicitation open after cancel = %s", out)
		}
		select {
		case <-notified:
			t.Fatal("canceled question was published")
		default:
		}
	})

	t.Run("elicitation close", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		opened := make(chan PendingElicitation, 1)
		closeStarted, release := make(chan struct{}), make(chan struct{})
		var mu sync.Mutex
		var reasons []string
		w := NewElicitationWaiter(func(p PendingElicitation) { opened <- p }, nil)
		w.PersistOpen = func(context.Context, PendingRequestRecord) error { return nil }
		w.PersistClose = func(_ context.Context, r PendingRequestResolution) error {
			mu.Lock()
			reasons = append(reasons, r.Reason)
			mu.Unlock()
			if r.Reason == ElicitationAnswered {
				close(closeStarted)
				<-release
			}
			return nil
		}
		waitDone := make(chan json.RawMessage, 1)
		go func() { out, _ := w.Wait(ctx, "s", json.RawMessage(`{"message":"private"}`)); waitDone <- out }()
		p := <-opened
		completeDone := make(chan error, 1)
		go func() { completeDone <- w.Complete(p.ID, json.RawMessage(`{"action":"accept"}`)) }()
		<-closeStarted
		cancel()
		close(release)
		if err := <-completeDone; err == nil {
			t.Fatal("elicitation completion succeeded after cancellation")
		}
		if out := <-waitDone; string(out) != string(cancelledElicitation) {
			t.Fatalf("elicitation answer escaped after cancel: %s", out)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(reasons) != 2 || reasons[0] != ElicitationAnswered || reasons[1] != ElicitationCancelled {
			t.Fatalf("stored close reasons = %v", reasons)
		}
	})
}
