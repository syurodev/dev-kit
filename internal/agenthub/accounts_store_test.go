package agenthub_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

func newAccountBook(t *testing.T) *agenthub.SQLAccountStore {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return agenthub.NewSQLAccountStore(db)
}

func TestAccountBookListsTheBuiltinAccountFirstAndSignedIn(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	if err := book.Put(ctx, agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeCodex, Label: "Work", HomeDir: "/h/work", SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	list, err := book.List(ctx, agenthub.AgentTypeCodex)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != agenthub.BuiltinAccountID(agenthub.AgentTypeCodex) || list[1].ID != "work" {
		t.Fatalf("list = %+v, want built-in then work", list)
	}
	if !list[0].Builtin() || !list[0].SignedIn {
		t.Errorf("built-in = %+v, want builtin and signed in", list[0])
	}
	other, _ := book.List(ctx, agenthub.AgentTypeClaude)
	if len(other) != 1 {
		t.Errorf("claude list = %d accounts, want only its built-in", len(other))
	}
}

func TestAccountBookPutGetKeepsUsageAndMarker(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	rec := agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeCodex, Label: "Work", HomeDir: "/h/work", SignedIn: true, Shared: true}
	if err := book.Put(ctx, rec); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1_800_000_000, 0)
	if err := book.SaveUsage(ctx, "work", []agenthub.LimitWindow{{Label: "Weekly", UsedPercent: 40, ResetsAt: 1_800_100_000}}, "observed", at); err != nil {
		t.Fatal(err)
	}
	if err := book.MarkExhausted(ctx, "work", at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rec.Label = "Work (renamed)"
	if err := book.Put(ctx, rec); err != nil { // an edit must not wipe usage or the marker
		t.Fatal(err)
	}
	got, err := book.Get(ctx, "work")
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "Work (renamed)" || !got.Shared {
		t.Errorf("got %+v", got)
	}
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 40 || got.UsageSource != "observed" || !got.ObservedAt.Equal(at) {
		t.Errorf("usage lost by Put: %+v", got)
	}
	if !got.ExhaustedUntil.Equal(at.Add(time.Hour)) {
		t.Errorf("marker lost by Put: %v", got.ExhaustedUntil)
	}
}

func TestAccountBookSaveUsageMergesByLabel(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	_ = book.Put(ctx, agenthub.AccountRecord{ID: "a", Vendor: agenthub.AgentTypeClaude, Label: "A", HomeDir: "/h/a", SignedIn: true})
	at := time.Unix(1_800_000_000, 0)
	_ = book.SaveUsage(ctx, "a", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 10}}, "observed", at)
	_ = book.SaveUsage(ctx, "a", []agenthub.LimitWindow{{Label: "Weekly", UsedPercent: 50}}, "observed", at)
	_ = book.SaveUsage(ctx, "a", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 20}}, "observed", at)
	got, _ := book.Get(ctx, "a")
	if len(got.Usage) != 2 {
		t.Fatalf("usage = %+v, want two windows", got.Usage)
	}
	for _, w := range got.Usage {
		if w.Label == "5-hour" && w.UsedPercent != 20 {
			t.Errorf("5-hour = %v, want the newer 20", w.UsedPercent)
		}
	}
}

func TestAccountBookMarkExhaustedOnlyMovesLater(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	_ = book.Put(ctx, agenthub.AccountRecord{ID: "a", Vendor: agenthub.AgentTypeClaude, Label: "A", HomeDir: "/h/a", SignedIn: true})
	late, early := time.Unix(1_800_003_600, 0), time.Unix(1_800_000_600, 0)
	_ = book.MarkExhausted(ctx, "a", late)
	_ = book.MarkExhausted(ctx, "a", early)
	got, _ := book.Get(ctx, "a")
	if !got.ExhaustedUntil.Equal(late) {
		t.Errorf("exhausted_until = %v, want %v (it must never move earlier)", got.ExhaustedUntil, late)
	}
	if err := book.MarkExhausted(ctx, "missing", late); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("missing account error = %v, want ErrAccountNotFound", err)
	}
}
