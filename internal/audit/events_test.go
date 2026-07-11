package audit

import "testing"

func TestMemorySinkRedactsMetadata(t *testing.T) {
	sink := NewMemorySink()
	metadata := map[string]any{
		"action":   "read",
		"password": "do-not-record",
	}
	sink.Record(Event{
		Outcome:  OutcomeAllowed,
		Metadata: metadata,
	})
	metadata["action"] = "mutated"

	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Metadata["password"] != RedactedValue {
		t.Fatalf("password metadata = %v, want redacted", events[0].Metadata["password"])
	}
	if events[0].Metadata["action"] != "read" {
		t.Fatalf("action metadata = %v, want read", events[0].Metadata["action"])
	}
}

func TestMemorySinkEventsReturnsSliceCopy(t *testing.T) {
	sink := NewMemorySink()
	sink.Record(Event{
		Outcome: OutcomeAllowed,
		Metadata: map[string]any{
			"action": "read",
		},
	})

	events := sink.Events()
	events[0].Outcome = OutcomeDenied
	events[0].Metadata["action"] = "mutated"

	stored := sink.Events()
	if stored[0].Outcome != OutcomeAllowed {
		t.Fatalf("stored outcome = %q, want %q", stored[0].Outcome, OutcomeAllowed)
	}
	if stored[0].Metadata["action"] != "read" {
		t.Fatalf("stored action = %v, want read", stored[0].Metadata["action"])
	}
}
