package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"devkit/internal/crypto"
)

func TestPushOperationExcludesLocalOutboxState(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	change := validChange("wire-safe", now)
	change.Attempts = 7
	change.LastError = FailureNetwork
	change.SyncingSince = now

	operation, err := pushOperationFromChange(change)
	if err != nil {
		t.Fatal(err)
	}
	serialized, err := json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range [][]byte{
		[]byte("attempts"), []byte("last_error"), []byte("syncing_since"),
		[]byte("next_attempt_at"), []byte("content_digest"), []byte("status"),
		[]byte("created_at"),
	} {
		if bytes.Contains(serialized, forbidden) {
			t.Fatalf("wire operation exposed local field %q: %s", forbidden, serialized)
		}
	}
}

func TestPushOperationRejectsMetadataThatDiffersFromEnvelope(t *testing.T) {
	change := validChange("wire-mismatch", time.Unix(100, 0).UTC())
	change.RecordID = "other-record"
	if _, err := pushOperationFromChange(change); err == nil {
		t.Fatal("mismatched public metadata must not reach transport")
	}
}

func TestMemoryTransportPushIsIdempotentAndKeepsPayloadOpaque(t *testing.T) {
	transport := NewMemoryTransport()
	now := time.Unix(100, 0).UTC()
	change := validChange("change-1", now)
	operation, err := pushOperationFromChange(change)
	if err != nil {
		t.Fatal(err)
	}
	original := clonePushOperation(operation)
	payload := append([]byte(nil), operation.Envelope.Ciphertext...)
	result, err := transport.Push(context.Background(), []PushOperation{operation})
	if err != nil {
		t.Fatalf("first push: %v", err)
	}
	if len(result.SentIdempotencyKeys) != 1 || result.SentIdempotencyKeys[0] != change.IdempotencyKey {
		t.Fatalf("first result = %+v", result)
	}
	operation.Envelope.Ciphertext[0] ^= 0xff
	result, err = transport.Push(context.Background(), []PushOperation{original})
	if err != nil || len(result.SentIdempotencyKeys) != 1 {
		t.Fatalf("repeated push = %+v, err %v", result, err)
	}
	received := transport.Received()
	if len(received) != 1 || string(received[0].Envelope.Ciphertext) != string(payload) {
		t.Fatalf("received payload = %x, want %x", received[0].Envelope.Ciphertext, payload)
	}
	received[0].Envelope.Ciphertext[0] ^= 0xff
	if transport.Received()[0].Envelope.Ciphertext[0] != payload[0] {
		t.Fatal("received payload was not clone-safe")
	}
}

func TestMemoryTransportForcedErrorAndConflict(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	conflict := Conflict{
		ID:              "conflict-1",
		RecordID:        "record-1",
		RecordType:      "note",
		LocalChangeKey:  "change-1",
		RemoteChangeKey: "remote-1",
		DetectedAt:      now,
	}
	transport := NewMemoryTransport(MemoryTransportConfig{ForcedConflicts: []Conflict{conflict}})
	operation, err := pushOperationFromChange(validChange("change-1", now))
	if err != nil {
		t.Fatal(err)
	}
	result, err := transport.Push(context.Background(), []PushOperation{operation})
	if err != nil || len(result.Conflicts) != 1 || result.Conflicts[0].ID != conflict.ID {
		t.Fatalf("conflict result = %+v, err %v", result, err)
	}
	if len(transport.Received()) != 0 {
		t.Fatal("conflicted change was accepted")
	}
	if err := transport.SetForcedError(FailureNetwork); err != nil {
		t.Fatal(err)
	}
	operation, err = pushOperationFromChange(validChange("change-2", now))
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Push(context.Background(), []PushOperation{operation})
	var transportErr TransportError
	if !errors.As(err, &transportErr) || transportErr.FailureReason() != FailureNetwork {
		t.Fatalf("forced error = %v", err)
	}
}

func TestRunnerPushesAndIsIdempotentOnRerun(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	outbox := NewMemoryOutbox()
	for _, key := range []string{"b", "a"} {
		if err := outbox.Enqueue(validChange(key, now)); err != nil {
			t.Fatal(err)
		}
	}
	transport := NewMemoryTransport()
	runner := NewRunner(outbox, transport, RunnerConfig{Clock: func() time.Time { return now }})
	report, err := runner.Run(context.Background(), 0)
	if err != nil || report.Attempted != 2 || report.Sent != 2 || report.Retried != 0 || report.Conflicts != 0 {
		t.Fatalf("first run = %+v, err %v", report, err)
	}
	if rerun, err := runner.Run(context.Background(), 0); err != nil || rerun != (RunReport{}) {
		t.Fatalf("rerun = %+v, err %v", rerun, err)
	}
	for _, key := range []string{"a", "b"} {
		change, err := outbox.Get(key)
		if err != nil || change.Status != StatusSynced {
			t.Fatalf("change %s = %+v, err %v", key, change, err)
		}
	}
}

func TestRunnerRetriesTransportErrorWithSafeReason(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	outbox := NewMemoryOutbox()
	if err := outbox.Enqueue(validChange("retry", now)); err != nil {
		t.Fatal(err)
	}
	transport := NewMemoryTransport(MemoryTransportConfig{ForcedError: FailureNetwork})
	runner := NewRunner(outbox, transport, RunnerConfig{
		Clock:                 func() time.Time { return now },
		MaxInlinePushAttempts: 1,
	})
	report, err := runner.Run(context.Background(), 1)
	if err == nil || report.Attempted != 1 || report.Retried != 1 {
		t.Fatalf("run = %+v, err %v", report, err)
	}
	change, _ := outbox.Get("retry")
	if change.Status != StatusPending || change.LastError != FailureNetwork || change.Attempts != 1 {
		t.Fatalf("retry state = %+v", change)
	}
}

func TestRunnerRetriesInlineOnTransientTransportError(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	outbox := NewMemoryOutbox()
	if err := outbox.Enqueue(validChange("inline", now)); err != nil {
		t.Fatal(err)
	}
	transport := &countingTransport{
		inner:          NewMemoryTransport(),
		failuresBefore: 2,
		reason:         FailureNetwork,
	}
	runner := NewRunner(outbox, transport, RunnerConfig{
		Clock:                 func() time.Time { return now },
		MaxInlinePushAttempts: 3,
		InlinePushBackoff:     0,
	})
	report, err := runner.Run(context.Background(), 1)
	if err != nil || report.Attempted != 1 || report.Sent != 1 || report.Retried != 0 {
		t.Fatalf("run = %+v, err %v, push attempts = %d", report, err, transport.attempts)
	}
	if transport.attempts != 3 {
		t.Fatalf("push attempts = %d, want 3", transport.attempts)
	}
}

type countingTransport struct {
	inner          Transport
	failuresBefore int
	reason         FailureReason
	attempts       int
}

func (t *countingTransport) Push(ctx context.Context, operations []PushOperation) (PushResult, error) {
	t.attempts++
	if t.attempts <= t.failuresBefore {
		return PushResult{}, TransportError{Reason: t.reason}
	}
	return t.inner.Push(ctx, operations)
}

func TestRunnerMarksConflict(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	change := validChange("conflict-key", now)
	conflict := Conflict{ID: "c1", RecordID: change.RecordID, RecordType: change.RecordType, LocalChangeKey: change.IdempotencyKey, RemoteChangeKey: "remote", DetectedAt: now}
	outbox := NewMemoryOutbox()
	if err := outbox.Enqueue(change); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(outbox, NewMemoryTransport(MemoryTransportConfig{ForcedConflicts: []Conflict{conflict}}), RunnerConfig{Clock: func() time.Time { return now }})
	report, err := runner.Run(context.Background(), 1)
	if err != nil || report.Conflicts != 1 || report.Sent != 0 {
		t.Fatalf("run = %+v, err %v", report, err)
	}
	stored, _ := outbox.Get(change.IdempotencyKey)
	if stored.Status != StatusConflict || stored.Conflict == nil {
		t.Fatalf("conflict state = %+v", stored)
	}
}

func TestRunnerRejectsPartialResponseAndRetriesMissing(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	outbox := NewMemoryOutbox()
	for _, key := range []string{"sent", "missing"} {
		if err := outbox.Enqueue(validChange(key, now)); err != nil {
			t.Fatal(err)
		}
	}
	transport := &scriptedTransport{push: func(_ context.Context, operations []PushOperation) (PushResult, error) {
		return PushResult{SentIdempotencyKeys: []string{operations[1].IdempotencyKey}}, nil
	}}
	runner := NewRunner(outbox, transport, RunnerConfig{Clock: func() time.Time { return now }})
	report, err := runner.Run(context.Background(), 0)
	if !errors.Is(err, ErrInvalidTransportResponse) || report.Sent != 1 || report.Retried != 1 {
		t.Fatalf("run = %+v, err %v", report, err)
	}
	sent, _ := outbox.Get("sent")
	missing, _ := outbox.Get("missing")
	if sent.Status != StatusSynced || missing.Status != StatusPending || missing.LastError != FailureInvalidResponse {
		t.Fatalf("states = sent %+v, missing %+v", sent, missing)
	}
}

func TestRunnerRejectsMalformedResponseWithoutMarkingSent(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	outbox := NewMemoryOutbox()
	if err := outbox.Enqueue(validChange("one", now)); err != nil {
		t.Fatal(err)
	}
	transport := &scriptedTransport{push: func(context.Context, []PushOperation) (PushResult, error) {
		return PushResult{SentIdempotencyKeys: []string{"unknown"}}, nil
	}}
	runner := NewRunner(outbox, transport, RunnerConfig{Clock: func() time.Time { return now }})
	report, err := runner.Run(context.Background(), 1)
	if !errors.Is(err, ErrInvalidTransportResponse) || report.Sent != 0 || report.Retried != 1 {
		t.Fatalf("run = %+v, err %v", report, err)
	}
	change, _ := outbox.Get("one")
	if change.Status != StatusPending || change.LastError != FailureInvalidResponse {
		t.Fatalf("state = %+v", change)
	}
}

func TestRunnerHandlesCancellationWithoutPersistingRawError(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	outbox := NewMemoryOutbox()
	if err := outbox.Enqueue(validChange("cancelled", now)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := NewRunner(outbox, NewMemoryTransport(), RunnerConfig{Clock: func() time.Time { return now }})
	if _, err := runner.Run(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled run error = %v", err)
	}
	change, _ := outbox.Get("cancelled")
	if change.Status != StatusPending || change.LastError != "" {
		t.Fatalf("cancelled state = %+v", change)
	}

	transport := &scriptedTransport{push: func(ctx context.Context, _ []PushOperation) (PushResult, error) {
		return PushResult{}, context.Canceled
	}}
	runner = NewRunner(outbox, transport, RunnerConfig{Clock: func() time.Time { return now }})
	report, err := runner.Run(context.Background(), 1)
	if !errors.Is(err, context.Canceled) || report.Retried != 1 {
		t.Fatalf("transport cancellation run = %+v, err %v", report, err)
	}
	change, _ = outbox.Get("cancelled")
	if change.LastError != FailureTimeout || change.Status != StatusPending {
		t.Fatalf("cancel retry state = %+v", change)
	}
}

func TestRunnerPreservesAuthenticationErrorWhenRetryPersistenceFails(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	memory := NewMemoryOutbox()
	if err := memory.Enqueue(validChange("auth-rejected", now)); err != nil {
		t.Fatal(err)
	}
	persistErr := errors.New("retry persistence failed")
	outbox := &retryFailingOutbox{Outbox: memory, err: persistErr}
	transport := &scriptedTransport{push: func(context.Context, []PushOperation) (PushResult, error) {
		return PushResult{}, ErrAuthentication
	}}

	_, err := NewRunner(outbox, transport, RunnerConfig{Clock: func() time.Time { return now }}).Run(context.Background(), 1)
	if !errors.Is(err, ErrAuthentication) || !errors.Is(err, persistErr) {
		t.Fatalf("Run error = %v, want authentication and persistence errors", err)
	}
}

func TestRunnerSplitsAggregateWireBudgetBeforeMarkingSyncing(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	outbox := NewMemoryOutbox()
	for i := 0; i < 6; i++ {
		change := validChange(fmt.Sprintf("large-%d", i), now.Add(time.Duration(i)*time.Nanosecond))
		var envelope ReplicationEnvelope
		if err := json.Unmarshal(change.Payload, &envelope); err != nil {
			t.Fatal(err)
		}
		var nested crypto.Envelope
		if err := json.Unmarshal(envelope.Ciphertext, &nested); err != nil {
			t.Fatal(err)
		}
		// Model a maximum-size plaintext after authenticated encryption. The
		// nested and outer JSON encodings both add base64 overhead, so several
		// individually valid operations can exceed the aggregate HTTP budget.
		nested.Ciphertext = bytes.Repeat([]byte{byte(i + 1)}, MaxRecordSnapshotBytes+16)
		envelope.Ciphertext, _ = json.Marshal(nested)
		envelope.RecordID = fmt.Sprintf("record-large-%d", i)
		change.RecordID = envelope.RecordID
		change.Payload, _ = json.Marshal(envelope)
		if err := outbox.Enqueue(change); err != nil {
			t.Fatal(err)
		}
	}
	batchSizes := make([]int, 0, 2)
	transport := &scriptedTransport{push: func(_ context.Context, operations []PushOperation) (PushResult, error) {
		body, err := json.Marshal(pushRequest{Operations: operations})
		if err != nil {
			t.Fatal(err)
		}
		if len(body) > maxHTTPRequestBytes {
			t.Fatalf("push body = %d bytes, limit = %d", len(body), maxHTTPRequestBytes)
		}
		batchSizes = append(batchSizes, len(operations))
		result := PushResult{SentIdempotencyKeys: make([]string, 0, len(operations))}
		for _, operation := range operations {
			result.SentIdempotencyKeys = append(result.SentIdempotencyKeys, operation.IdempotencyKey)
		}
		return result, nil
	}}
	runner := NewRunner(outbox, transport, RunnerConfig{Clock: func() time.Time { return now.Add(time.Second) }})
	for len(batchSizes) < 2 {
		if _, err := runner.Run(context.Background(), 100); err != nil {
			t.Fatal(err)
		}
	}
	if batchSizes[0] <= 0 || batchSizes[0] >= 6 || batchSizes[0]+batchSizes[1] != 6 {
		t.Fatalf("wire-bounded batch sizes = %v", batchSizes)
	}
}

type retryFailingOutbox struct {
	Outbox
	err error
}

func (o *retryFailingOutbox) MarkRetry(string, time.Time, FailureReason) error { return o.err }

type scriptedTransport struct {
	push func(context.Context, []PushOperation) (PushResult, error)
}

func (t *scriptedTransport) Push(ctx context.Context, operations []PushOperation) (PushResult, error) {
	return t.push(ctx, operations)
}

func TestRunnerDeadLettersPermanentlyUnpreparableChange(t *testing.T) {
	base := time.Unix(100, 0).UTC()
	outbox := NewMemoryOutbox()
	healthy := validChange("healthy", base)
	if err := outbox.Enqueue(healthy); err != nil {
		t.Fatal(err)
	}
	// A poison row (corrupt payload that cannot encode a push operation) is
	// injected directly: Enqueue's own validation would reject it, which is
	// exactly the corruption the dead-letter path guards against.
	poison := validChange("poison", base)
	poison.Payload = nil
	outbox.changes["poison"] = poison

	clock := base
	runner := NewRunner(outbox, NewMemoryTransport(), RunnerConfig{Clock: func() time.Time { return clock }})

	for attempt := 1; attempt <= 4; attempt++ {
		clock = base.Add(time.Duration(attempt) * time.Hour)
		report, err := runner.Run(context.Background(), 0)
		if err != nil {
			t.Fatalf("run %d: %v", attempt, err)
		}
		if report.Failed != 0 || report.Retried != 1 {
			t.Fatalf("run %d report = %+v", attempt, report)
		}
		if attempt == 1 && report.Sent != 1 {
			t.Fatalf("run 1 report = %+v", report)
		}
	}

	// The fifth consecutive preparation failure dead-letters the row; the
	// healthy change already synced and is not affected.
	clock = base.Add(5 * time.Hour)
	report, err := runner.Run(context.Background(), 0)
	if err != nil {
		t.Fatalf("dead-letter run: %v", err)
	}
	if report.Failed != 1 || report.Retried != 0 {
		t.Fatalf("dead-letter report = %+v", report)
	}
	dead, err := outbox.Get("poison")
	if err != nil || dead.Status != StatusFailed || dead.LastError != FailureInvalidResponse {
		t.Fatalf("poison state = %+v, err %v", dead, err)
	}

	// The failed row never resurfaces in later runs.
	clock = base.Add(6 * time.Hour)
	rerun, err := runner.Run(context.Background(), 0)
	if err != nil || rerun != (RunReport{}) {
		t.Fatalf("rerun after dead-letter = %+v, err %v", rerun, err)
	}
}
