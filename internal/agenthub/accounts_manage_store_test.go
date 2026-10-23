package agenthub_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

func newAccountDB(t *testing.T) (*agenthub.SQLAccountStore, *agenthub.SQLStore, *sql.DB) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return agenthub.NewSQLAccountStore(db), agenthub.NewSQLStore(db), db
}

func addWork(t *testing.T, book *agenthub.SQLAccountStore) {
	t.Helper()
	if err := book.Put(context.Background(), agenthub.AccountRecord{ID: "codex-work", Vendor: agenthub.AgentTypeCodex, Label: "Work", HomeDir: "/h/work", SignedIn: true}); err != nil {
		t.Fatal(err)
	}
}

func TestAccountBookDeleteReturnsItsSessionsToTheBuiltInAccount(t *testing.T) {
	book, store, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	bound := sampleSession("sess-on-work")
	bound.AccountID = "codex-work"
	other := sampleSession("sess-elsewhere")
	other.AccountID = "codex-other"
	for _, s := range []agenthub.AgentSession{bound, other} {
		if err := store.InsertSession(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if err := book.Delete(ctx, "codex-work"); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Get(ctx, "codex-work"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Fatalf("the row must be gone, got %v", err)
	}
	got, _ := store.GetSession(ctx, "sess-on-work")
	if got.AccountID != "" {
		t.Errorf("its session is bound to %q, want the built-in account (empty)", got.AccountID)
	}
	if got, _ := store.GetSession(ctx, "sess-elsewhere"); got.AccountID != "codex-other" {
		t.Errorf("another account's session was touched: %q", got.AccountID)
	}
}

func TestAccountBookDeleteRefusesTheBuiltInAndTheUnknown(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	if _, err := book.List(ctx, agenthub.AgentTypeCodex); err != nil { // creates the built-in row
		t.Fatal(err)
	}
	if err := book.Delete(ctx, agenthub.BuiltinAccountID(agenthub.AgentTypeCodex)); !errors.Is(err, agenthub.ErrInvalidAccount) {
		t.Errorf("deleting the built-in account: %v, want ErrInvalidAccount", err)
	}
	if err := book.Delete(ctx, "nope"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("deleting an unknown account: %v, want ErrAccountNotFound", err)
	}
}

func TestAccountBookSetSignedIn(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	if err := book.SetSignedIn(ctx, "codex-work", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := book.Get(ctx, "codex-work"); got.SignedIn {
		t.Error("SetSignedIn(false) did not stick")
	}
	if err := book.SetSignedIn(ctx, "codex-work", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := book.Get(ctx, "codex-work"); !got.SignedIn {
		t.Error("SetSignedIn(true) did not stick")
	}
	if err := book.SetSignedIn(ctx, "missing", true); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("unknown account: %v, want ErrAccountNotFound", err)
	}
}

func TestAccountBookReplaceUsageStoresTheWholeReading(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	at := time.Unix(1_800_000_000, 0)
	if err := book.SaveUsage(ctx, "codex-work", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 90}, {Label: "Weekly", UsedPercent: 40}}, "observed", at); err != nil {
		t.Fatal(err)
	}
	if err := book.ReplaceUsage(ctx, "codex-work", []agenthub.LimitWindow{{Label: "5-hour", UsedPercent: 5}}, "checked", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ := book.Get(ctx, "codex-work")
	if len(got.Usage) != 1 || got.Usage[0].UsedPercent != 5 {
		t.Fatalf("usage = %+v, want exactly the checked 5-hour window (the weekly one is not in the new reading)", got.Usage)
	}
	if got.UsageSource != "checked" || !got.ObservedAt.Equal(at.Add(time.Minute)) {
		t.Errorf("source/observedAt = %q / %v", got.UsageSource, got.ObservedAt)
	}
	if err := book.ReplaceUsage(ctx, "missing", nil, "checked", at); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("unknown account: %v, want ErrAccountNotFound", err)
	}
}

func TestAccountBookMarkCooldownOnlyMovesLaterAndIsNotAnExhaustion(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	late, early := time.Unix(1_800_003_600, 0), time.Unix(1_800_000_600, 0)
	if err := book.MarkCooldown(ctx, "codex-work", late); err != nil {
		t.Fatal(err)
	}
	if err := book.MarkCooldown(ctx, "codex-work", early); err != nil {
		t.Fatal(err)
	}
	got, _ := book.Get(ctx, "codex-work")
	if !got.CooldownUntil.Equal(late) {
		t.Errorf("cooldown_until = %v, want %v (never earlier)", got.CooldownUntil, late)
	}
	if !got.ExhaustedUntil.IsZero() {
		t.Errorf("a cooldown must not mark the account as having reached a limit: %v", got.ExhaustedUntil)
	}
	if err := book.MarkCooldown(ctx, "missing", late); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("unknown account: %v, want ErrAccountNotFound", err)
	}
}

func TestAccountBookSaveUsageKeepsObservedAtWhenEveryWindowWasStale(t *testing.T) {
	book, _, _ := newAccountDB(t)
	ctx := context.Background()
	addWork(t, book)
	first := time.Unix(1_800_000_000, 0)
	w := agenthub.LimitWindow{Label: "5-hour", UsedPercent: 90, ResetsAt: 1_800_010_000}
	if err := book.SaveUsage(ctx, "codex-work", []agenthub.LimitWindow{w}, "observed", first); err != nil {
		t.Fatal(err)
	}
	stale := w
	stale.UsedPercent = 10
	if err := book.SaveUsage(ctx, "codex-work", []agenthub.LimitWindow{stale}, "observed", first.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ := book.Get(ctx, "codex-work")
	if !got.ObservedAt.Equal(first) || got.Usage[0].UsedPercent != 90 {
		t.Errorf("a stale reading must not make the usage look newer: observedAt=%v usage=%+v", got.ObservedAt, got.Usage)
	}
}
