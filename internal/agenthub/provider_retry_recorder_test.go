package agenthub_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"devkit/internal/agenthub"
)

// The payloads are opencode's retry notice as the person's own database
// recorded it during a provider outage (session 399558b3…, opencode V2): a
// notice per attempt, a null when that attempt began.
const (
	retryNoticeWire = `{"sessionUpdate":"session_info_update","_meta":{"opencode/retry":{"attempt":2,"nextRetryAt":"2026-10-18T06:28:00.642Z","error":{"type":"provider.rate-limit","message":"Upstream request failed: Endpoint is unavailable.","status":429,"response":{"body":"{\"error\":{\"type\":\"server_error\",\"message\":\"Upstream request failed: Endpoint is unavailable.\"}}"}}}}}`
	retryClearWire  = `{"sessionUpdate":"session_info_update","_meta":{"opencode/retry":null}}`
)

func TestOpenCodeRetryNoticeParsesAsItsOwnKind(t *testing.T) {
	got := agenthub.ParseSessionUpdate(json.RawMessage(retryNoticeWire))
	if got.Kind != agenthub.UpdateProviderRetry {
		t.Fatalf("kind = %q, want %q", got.Kind, agenthub.UpdateProviderRetry)
	}
	want := agenthub.ProviderRetry{
		Attempt:     2,
		NextRetryAt: "2026-10-18T06:28:00.642Z",
		Message:     "Upstream request failed: Endpoint is unavailable.",
		Status:      429,
		Type:        "provider.rate-limit",
	}
	if got.Retry == nil || *got.Retry != want {
		t.Fatalf("retry = %+v, want %+v", got.Retry, want)
	}
	if got.Raw != nil {
		t.Fatalf("raw = %s: the wire blob (with the provider's whole response) should not be stored", got.Raw)
	}
	if sentence := got.Retry.Sentence(); sentence != "Upstream request failed: Endpoint is unavailable (429)" {
		t.Fatalf("sentence = %q", sentence)
	}
}

func TestOpenCodeRetryNullClears(t *testing.T) {
	got := agenthub.ParseSessionUpdate(json.RawMessage(retryClearWire))
	if got.Kind != agenthub.UpdateProviderRetry || got.Retry != nil {
		t.Fatalf("got kind %q retry %+v, want a cleared provider_retry_update", got.Kind, got.Retry)
	}
}

func TestSessionInfoWithoutRetryStaysSessionInfo(t *testing.T) {
	got := agenthub.ParseSessionUpdate(json.RawMessage(`{"sessionUpdate":"session_info_update","title":"Fix the parser"}`))
	if got.Kind != agenthub.UpdateSessionInfo || got.Retry != nil || got.Title != "Fix the parser" {
		t.Fatalf("got %+v", got)
	}
	// A notice beside a title is still a title: the title is not dropped.
	both := agenthub.ParseSessionUpdate(json.RawMessage(`{"sessionUpdate":"session_info_update","title":"T","_meta":{"opencode/retry":{"attempt":3}}}`))
	if both.Kind != agenthub.UpdateSessionInfo || both.Title != "T" || both.Retry == nil || both.Retry.Attempt != 3 {
		t.Fatalf("both = %+v", both)
	}
}

// One outage was 128 near-identical rows in the person's database. The notice
// is the current state of one thing, so the transcript keeps one row for it.
func TestRetryNoticesAreOneRowReplacedInPlace(t *testing.T) {
	store := newTestStore(t)
	session := seedSession(t, store)

	raws := []string{`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"go"}}`}
	for attempt := 2; attempt <= 11; attempt++ {
		raws = append(raws,
			fmt.Sprintf(`{"sessionUpdate":"session_info_update","_meta":{"opencode/retry":{"attempt":%d,"nextRetryAt":"2026-10-18T06:28:00.642Z","error":{"message":"Upstream request failed: Endpoint is unavailable.","status":503}}}}`, attempt),
			retryClearWire,
		)
	}
	raws = append(raws, retryNoticeWire)
	recordAll(t, store, session, raws...)

	entries, err := store.ListTranscript(context.Background(), session)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var rows []agenthub.TranscriptEntry
	for _, entry := range entries {
		if entry.Kind == agenthub.UpdateProviderRetry {
			rows = append(rows, entry)
		}
	}
	if len(rows) != 1 || len(entries) != 2 {
		t.Fatalf("got %d retry rows among %d entries, want 1 among 2", len(rows), len(entries))
	}
	var last agenthub.SessionUpdate
	if err := json.Unmarshal(rows[0].Payload, &last); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if last.Retry == nil || last.Retry.Attempt != 2 {
		t.Fatalf("the row should hold the latest notice, got %+v", last.Retry)
	}
}
