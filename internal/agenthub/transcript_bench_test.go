package agenthub_test

import (
	"context"
	"path/filepath"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

// BenchmarkRecordTenThousandTokens is the streaming shape of one long agent
// message: ten thousand chunks of the same messageId, then the turn ends.
// The recorder used to SELECT the last row and rewrite the whole accumulated
// text for every chunk — O(len) per token, O(len²) per message — so this
// benchmark is the before/after evidence for the batched tail. Run with
// -benchtime=1x; one iteration is already the full 10k-token message.
func BenchmarkRecordTenThousandTokens(b *testing.B) {
	db, err := storage.Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		b.Fatalf("migrate: %v", err)
	}
	store := agenthub.NewSQLStore(db)
	if err := store.InsertSession(context.Background(), sampleSession("bench")); err != nil {
		b.Fatalf("seed session: %v", err)
	}
	rec := agenthub.NewRecorder(store)
	ctx := context.Background()
	chunk := agenthub.SessionUpdate{
		Kind:      agenthub.UpdateAgentMessageChunk,
		Content:   &agenthub.ContentBlock{Type: "text", Text: "token "},
		MessageID: "m1",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 10000; j++ {
			if err := rec.Record(ctx, "bench", chunk); err != nil {
				b.Fatalf("record: %v", err)
			}
		}
		// Turn end. Before batching this was implicit — every Record wrote
		// through — so the loop above is the whole before-cost; afterwards the
		// deferred tail lands here.
		if err := rec.Flush(ctx, "bench"); err != nil {
			b.Fatalf("flush: %v", err)
		}
	}
}
