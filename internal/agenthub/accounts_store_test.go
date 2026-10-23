package agenthub_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
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

func TestAccountBookSaveUsageKeepsEveryWindowUnderConcurrency(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	if err := book.Put(ctx, agenthub.AccountRecord{ID: "a", Vendor: agenthub.AgentTypeClaude, Label: "A", HomeDir: "/h/a", SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	const n = 8
	at := time.Unix(1_800_000_000, 0)
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- book.SaveUsage(ctx, "a", []agenthub.LimitWindow{{Label: fmt.Sprintf("w%d", i), UsedPercent: float64(i)}}, "observed", at)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := book.Get(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, w := range got.Usage {
		seen[w.Label] = true
	}
	for i := 0; i < n; i++ {
		if label := fmt.Sprintf("w%d", i); !seen[label] {
			t.Errorf("window %s lost; usage = %+v", label, got.Usage)
		}
	}
}

func TestAccountBookSaveUsageOnAMissingAccount(t *testing.T) {
	book := newAccountBook(t)
	err := book.SaveUsage(context.Background(), "missing", []agenthub.LimitWindow{{Label: "Weekly"}}, "observed", time.Unix(1_800_000_000, 0))
	if !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("error = %v, want ErrAccountNotFound", err)
	}
}

func TestAccountBookListTwiceDoesNotDuplicateTheBuiltin(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		list, err := book.List(ctx, agenthub.AgentTypeClaude)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != agenthub.BuiltinAccountID(agenthub.AgentTypeClaude) {
			t.Fatalf("list %d = %+v, want exactly the built-in", i+1, list)
		}
	}
}

func TestAccountBookSignedInIsReadBackAfterPut(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	for i, signedIn := range []bool{true, false} {
		id := []string{"a", "b"}[i]
		if err := book.Put(ctx, agenthub.AccountRecord{ID: id, Vendor: agenthub.AgentTypeCodex, Label: id, HomeDir: "/h/" + id, SignedIn: signedIn}); err != nil {
			t.Fatal(err)
		}
		got, err := book.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.SignedIn != signedIn {
			t.Errorf("SignedIn = %v after Put(%v)", got.SignedIn, signedIn)
		}
	}
}

// After the row exists the sign-in state belongs to SetSignedIn: a Put made from an
// older read must not write a stale state back.
func TestAccountBookPutDoesNotOverwriteSignedInOfAnExistingRow(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	rec := agenthub.AccountRecord{ID: "a", Vendor: agenthub.AgentTypeCodex, Label: "A", HomeDir: "/h/a"}
	if err := book.Put(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := book.SetSignedIn(ctx, "a", true); err != nil {
		t.Fatal(err)
	}
	rec.Label = "A2"
	if err := book.Put(ctx, rec); err != nil { // rec still says SignedIn false
		t.Fatal(err)
	}
	got, _ := book.Get(ctx, "a")
	if !got.SignedIn || got.Label != "A2" {
		t.Errorf("got %+v, want signed in kept and the label updated", got)
	}
}

func TestAccountBookSetLabelAndSetSharedTouchOneFieldAndNeverCreateARow(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	if err := book.Put(ctx, agenthub.AccountRecord{ID: "a", Vendor: agenthub.AgentTypeCodex, Label: "A", HomeDir: "/h/a", SignedIn: true}); err != nil {
		t.Fatal(err)
	}
	if err := book.SetLabel(ctx, "a", "Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := book.SetShared(ctx, "a", true); err != nil {
		t.Fatal(err)
	}
	got, _ := book.Get(ctx, "a")
	if got.Label != "Renamed" || !got.Shared || !got.SignedIn || got.HomeDir != "/h/a" {
		t.Errorf("got %+v", got)
	}
	if err := book.SetLabel(ctx, "gone", "x"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("SetLabel of an unknown id: %v", err)
	}
	if err := book.SetShared(ctx, "gone", true); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("SetShared of an unknown id: %v", err)
	}
	if _, err := book.Get(ctx, "gone"); !errors.Is(err, agenthub.ErrAccountNotFound) {
		t.Errorf("a row was created: %v", err)
	}
}

func TestAccountBookPutRefusesRecordsThatBreakTheBuiltinRule(t *testing.T) {
	builtin := agenthub.BuiltinAccountID(agenthub.AgentTypeClaude)
	for _, tc := range []struct {
		name string
		rec  agenthub.AccountRecord
	}{
		{"empty id", agenthub.AccountRecord{Vendor: agenthub.AgentTypeClaude, Label: "x", HomeDir: "/h/x"}},
		{"empty vendor", agenthub.AccountRecord{ID: "x", Label: "x", HomeDir: "/h/x"}},
		{"built-in id with a home", agenthub.AccountRecord{ID: builtin, Vendor: agenthub.AgentTypeClaude, Label: "x", HomeDir: "/h/x"}},
		{"other id without a home", agenthub.AccountRecord{ID: "x", Vendor: agenthub.AgentTypeClaude, Label: "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			book := newAccountBook(t)
			if err := book.Put(context.Background(), tc.rec); !errors.Is(err, agenthub.ErrInvalidAccount) {
				t.Errorf("error = %v, want ErrInvalidAccount", err)
			}
		})
	}
}

func TestAccountBookPutAcceptsAnAddAndAnEditOfTheBuiltin(t *testing.T) {
	book := newAccountBook(t)
	ctx := context.Background()
	if err := book.Put(ctx, agenthub.AccountRecord{ID: "work", Vendor: agenthub.AgentTypeCodex, Label: "Work", HomeDir: "/h/work", SignedIn: true}); err != nil {
		t.Fatalf("valid add refused: %v", err)
	}
	id := agenthub.BuiltinAccountID(agenthub.AgentTypeCodex)
	if _, err := book.List(ctx, agenthub.AgentTypeCodex); err != nil { // creates the built-in
		t.Fatal(err)
	}
	if err := book.Put(ctx, agenthub.AccountRecord{ID: id, Vendor: agenthub.AgentTypeCodex, Label: "Personal", SignedIn: true, Shared: true}); err != nil {
		t.Fatalf("edit of the built-in refused: %v", err)
	}
	got, err := book.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "Personal" || !got.Shared || !got.Builtin() {
		t.Errorf("built-in after edit = %+v", got)
	}
}
