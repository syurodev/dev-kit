package usagemetrics_test

import (
	"context"
	"database/sql"
	"devkit/internal/storage"
	m "devkit/internal/usagemetrics"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func database(t *testing.T) *sql.DB {
	t.Helper()
	db, e := storage.Open(filepath.Join(t.TempDir(), "usage.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	if e = storage.Migrate(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	return db
}

type ambiguous struct {
	m.Store
	once sync.Once
}

func (s *ambiguous) Commit(ctx context.Context, b m.Batch) error {
	e := s.Store.Commit(ctx, b)
	s.once.Do(func() { e = errors.New("reply lost after commit") })
	return e
}
func observation(tool string) m.Observation {
	return m.Observation{At: time.Now(), Scope: m.Scope{Vendor: "claude", WorkspaceID: "w1"}, Tool: tool, Result: true, Text: true, Before: 100, After: 25, Cut: true}
}
func TestRetryAfterCommittedReplyLostDoesNotDoubleCount(t *testing.T) {
	ctx := context.Background()
	db := database(t)
	store := m.NewSQLStore(db)
	c, e := m.New(ctx, &ambiguous{Store: store})
	if e != nil {
		t.Fatal(e)
	}
	c.Record(observation("notes.list"))
	if c.Flush(ctx) == nil {
		t.Fatal("expected lost reply")
	}
	c.Record(observation("notes.list"))
	if e = c.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	if e = c.Shutdown(ctx); e != nil {
		t.Fatal(e)
	}
	r, e := store.Report(ctx, "claude", m.Query{})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Rows) != 1 || r.Rows[0].Stats.Calls != 2 || r.Rows[0].Stats.BeforeBytes != 200 || r.Health.WriteFailures != 1 {
		t.Fatalf("report %+v", r)
	}
	var keys int
	if e = db.QueryRow(`SELECT COUNT(*) FROM agent_tool_usage_daily WHERE profile_revision IS NULL AND profile_key='none'`).Scan(&keys); e != nil || keys != 1 {
		t.Fatalf("nullable key coalescing: %d %v", keys, e)
	}
	other := "w2"
	filtered, e := store.Report(ctx, "claude", m.Query{WorkspaceID: &other})
	if e != nil || len(filtered.Rows) != 0 {
		t.Fatalf("workspace filter %+v %v", filtered, e)
	}
}

type memoryStore struct {
	mu      sync.Mutex
	batches []m.Batch
	fail    bool
}

func (*memoryStore) Start(context.Context, string, time.Time) error { return nil }
func (*memoryStore) Close(context.Context, string, time.Time) error { return nil }
func (s *memoryStore) Commit(_ context.Context, b m.Batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, b)
	if s.fail {
		return errors.New("disk unavailable")
	}
	return nil
}
func TestBoundedActiveAndPendingWithConcurrentRecords(t *testing.T) {
	ctx := context.Background()
	s := &memoryStore{fail: true}
	c, e := m.New(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < m.MaxKeys+10; i++ {
		c.Record(observation(fmt.Sprintf("tool.%d", i)))
	}
	if c.Flush(ctx) == nil {
		t.Fatal("expected store failure")
	}
	var wg sync.WaitGroup
	for k := 0; k < 8; k++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			for i := 0; i < 1024; i++ {
				c.Record(observation(fmt.Sprintf("other.%d", k*1024+i)))
			}
		}(k)
	}
	wg.Wait()
	s.mu.Lock()
	s.fail = false
	s.mu.Unlock()
	if e = c.Shutdown(ctx); e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.batches) < 3 {
		t.Fatal("pending and active must both drain")
	}
	if len(s.batches[0].Rows) != m.MaxKeys || len(s.batches[len(s.batches)-1].Rows) != m.MaxKeys {
		t.Fatal("unbounded or lost buffers")
	}
	if s.batches[len(s.batches)-1].Dropped != 4106 {
		t.Fatalf("drops %+v", s.batches[len(s.batches)-1])
	}
}
func TestShutdownWaitsForEnteredObservation(t *testing.T) {
	s := &memoryStore{}
	c, e := m.New(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	if !c.Begin() {
		t.Fatal("begin")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e = c.Shutdown(ctx); !errors.Is(e, context.Canceled) {
		t.Fatalf("shutdown %v", e)
	}
	if c.Begin() {
		t.Fatal("accepted call after shutdown began")
	}
	c.Record(observation("late.result"))
	c.End()
	if e = c.Shutdown(context.Background()); e != nil {
		t.Fatal(e)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var calls int64
	for _, b := range s.batches {
		for _, r := range b.Rows {
			calls += r.Calls
		}
	}
	if calls != 1 {
		t.Fatalf("inflight result lost: %d", calls)
	}
}
func TestRunHealthAndSQLTopLimit(t *testing.T) {
	ctx := context.Background()
	s := m.NewSQLStore(database(t))
	if e := s.Start(ctx, "abandoned", time.Now()); e != nil {
		t.Fatal(e)
	}
	c, e := m.New(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	c.Record(observation("b"))
	c.Record(observation("a"))
	if e = c.Shutdown(ctx); e != nil {
		t.Fatal(e)
	}
	r, e := s.Report(ctx, "claude", m.Query{Limit: 1})
	if e != nil || !r.HasMore || len(r.Rows) != 1 || r.Rows[0].Tool != "a" || r.Health.UncleanRuns != 1 {
		t.Fatalf("report %+v %v", r, e)
	}
}
func TestWindowValidation(t *testing.T) {
	now := time.Date(2026, 10, 26, 23, 0, 0, 0, time.UTC)
	q, e := m.Window(m.Query{}, now)
	if e != nil || q.From != "2026-09-27" || q.To != "2026-10-26" || q.Limit != 100 {
		t.Fatalf("default %+v %v", q, e)
	}
	for _, q := range []m.Query{{From: "2026-01-01", To: "2026-04-01"}, {From: "bad"}, {From: "2026-10-27", To: "2026-10-26"}, {Limit: 101}, {To: "2026-02-30"}} {
		if _, e = m.Window(q, now); !errors.Is(e, m.ErrInvalidQuery) {
			t.Fatalf("accepted %+v", q)
		}
	}
}

func TestBlockedSQLiteWriterDoesNotBlockObservation(t *testing.T) {
	ctx := context.Background()
	db := database(t)
	s := m.NewSQLStore(db)
	c, e := m.New(ctx, s)
	if e != nil {
		t.Fatal(e)
	}
	var path string
	var seq int
	var name string
	rows, e := db.Query(`PRAGMA database_list`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		if e = rows.Scan(&seq, &name, &path); e != nil {
			t.Fatal(e)
		}
		if name == "main" {
			break
		}
	}
	rows.Close()
	blocker, e := storage.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Close()
	if _, e = blocker.Exec(`BEGIN IMMEDIATE`); e != nil {
		t.Fatal(e)
	}
	c.Record(observation("blocked"))
	flushCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- c.Flush(flushCtx) }()
	recorded := make(chan struct{})
	go func() { c.Record(observation("blocked")); close(recorded) }()
	select {
	case <-recorded:
	case <-time.After(time.Second):
		t.Fatal("observation waited on SQLite")
	}
	if e = <-finished; e == nil {
		t.Fatal("writer lock should fail flush")
	}
	if _, e = blocker.Exec(`ROLLBACK`); e != nil {
		t.Fatal(e)
	}
	if e = c.Shutdown(ctx); e != nil {
		t.Fatal(e)
	}
	r, e := s.Report(ctx, "claude", m.Query{})
	if e != nil || len(r.Rows) != 1 || r.Rows[0].Stats.Calls != 2 || r.Health.WriteFailures < 1 {
		t.Fatalf("recovery %+v %v", r, e)
	}
}
func TestProcessExitLosesOnlyUnflushedTailAndMarksUnclean(t *testing.T) {
	if path := os.Getenv("DEVKIT_METRICS_EXIT_FIXTURE"); path != "" {
		db, e := storage.Open(path)
		if e != nil {
			panic(e)
		}
		if e = storage.Migrate(context.Background(), db); e != nil {
			panic(e)
		}
		c, e := m.New(context.Background(), m.NewSQLStore(db))
		if e != nil {
			panic(e)
		}
		c.Record(observation("durable"))
		if e = c.Flush(context.Background()); e != nil {
			panic(e)
		}
		c.Record(observation("unflushed"))
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "crash.db")
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessExitLosesOnlyUnflushedTailAndMarksUnclean$")
	cmd.Env = append(os.Environ(), "DEVKIT_METRICS_EXIT_FIXTURE="+path)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("child failed: %v %s", e, out)
	}
	db, e := storage.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := m.NewSQLStore(db)
	c, e := m.New(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Shutdown(context.Background())
	r, e := s.Report(context.Background(), "claude", m.Query{})
	if e != nil || len(r.Rows) != 1 || r.Rows[0].Tool != "durable" || r.Rows[0].Stats.Calls != 1 || r.Health.UncleanRuns != 1 {
		t.Fatalf("crash recovery %+v %v", r, e)
	}
}

type signalledStore struct {
	*memoryStore
	saved chan m.Batch
}

func (s *signalledStore) Commit(ctx context.Context, b m.Batch) error {
	e := s.memoryStore.Commit(ctx, b)
	select {
	case s.saved <- b:
	default:
	}
	return e
}
func TestInjectedClockAndTickFlushWithoutSleeping(t *testing.T) {
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	ticks := make(chan time.Time, 1)
	s := &signalledStore{memoryStore: &memoryStore{}, saved: make(chan m.Batch, 1)}
	c, e := m.NewWithOptions(context.Background(), s, m.Options{Now: func() time.Time { return at }, Ticks: ticks})
	if e != nil {
		t.Fatal(e)
	}
	defer c.Shutdown(context.Background())
	c.Record(observation("sample"))
	ticks <- at
	select {
	case b := <-s.saved:
		if !b.At.Equal(at) || len(b.Rows) != 1 {
			t.Fatalf("batch %+v", b)
		}
	case <-time.After(time.Second):
		t.Fatal("tick did not flush")
	}
}
