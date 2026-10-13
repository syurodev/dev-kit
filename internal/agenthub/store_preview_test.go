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
