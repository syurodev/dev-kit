package agenthub_test

import (
	"context"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

func TestUpdateSessionPreviewRoundTrips(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	s := sampleSession("sess-preview")
	if err := st.InsertSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if err := st.UpdateSessionPreview(ctx, s.ID, "Fixed the TTL.", at); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Preview != "Fixed the TTL." {
		t.Fatalf("preview = %q", got.Preview)
	}
	if err := st.UpdateSessionPreview(ctx, "missing", "x", at); err != agenthub.ErrSessionNotFound {
		t.Fatalf("missing session err = %v", err)
	}
}

func TestUpdateSessionStopRoundTripsAndClears(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	s := sampleSession("sess-stop")
	if err := st.InsertSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetSession(ctx, s.ID); got.Stop != nil {
		t.Fatalf("a new session has a stop: %+v", got.Stop)
	}
	at := time.Date(2026, 10, 18, 20, 0, 0, 0, time.UTC)
	retry := at.Add(time.Hour)
	want := agenthub.SessionStop{Kind: agenthub.StopUsageLimit, Message: "Try again at 21:00.", Retryable: false, RetryAfter: &retry, Since: at, Attempts: 2}
	if err := st.UpdateSessionStop(ctx, s.ID, &want, at); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stop == nil || got.Stop.Kind != want.Kind || got.Stop.Message != want.Message || got.Stop.Attempts != 2 || got.Stop.RetryAfter == nil || !got.Stop.RetryAfter.Equal(retry) || !got.Stop.Since.Equal(at) {
		t.Fatalf("stop = %+v", got.Stop)
	}
	if err := st.UpdateSessionStop(ctx, s.ID, nil, at); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetSession(ctx, s.ID); got.Stop != nil {
		t.Fatalf("cleared stop = %+v", got.Stop)
	}
	if err := st.UpdateSessionStop(ctx, "missing", &want, at); err != agenthub.ErrSessionNotFound {
		t.Fatalf("missing session err = %v", err)
	}
}
