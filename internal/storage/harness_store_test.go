package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"devkit/internal/harness"
)

func harnessFixture(t *testing.T) (*HarnessStore, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,root) VALUES('w1','one','/one'),('w2','two','/two')`,
		`INSERT INTO agent_sessions(id,agent_type,project_path,status,depth,created_at,updated_at,workspace_id) VALUES
 ('s1','mock','/one','idle',0,'x','x','w1'),('s2','mock','/two','idle',0,'x','x','w2'),('s0','mock','/none','idle',0,'x','x',NULL)`,
	} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	return NewHarnessStore(db), ctx
}

func hp(f func(*harness.Profile)) *harness.Profile {
	p := &harness.Profile{Schema: harness.SchemaVersion}
	if f != nil {
		f(p)
	}
	return p
}

func ho(groups ...string) *harness.Override {
	return &harness.Override{Schema: harness.SchemaVersion, Groups: groups}
}

func profileState(t *testing.T, s *HarnessStore, ctx context.Context, ws string) (*harness.Profile, harness.Stamp) {
	t.Helper()
	p, st, err := s.GetProfile(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	return p, st
}

func overrideState(t *testing.T, s *HarnessStore, ctx context.Context, session, ws string) (*harness.Override, harness.Stamp) {
	t.Helper()
	o, st, err := s.GetOverride(ctx, session, ws)
	if err != nil {
		t.Fatal(err)
	}
	return o, st
}

func TestHarnessMigrationIsIdempotentAndKeepsEarlierRows(t *testing.T) {
	s, ctx := harnessFixture(t)
	db := s.db
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_tool_usage_runs(id,started_at,batch_seq,dropped) VALUES('keep','2026-10-26T00:00:00Z',9,2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Value: hp(nil)}}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP TABLE workspace_harness_profiles`, `DROP TABLE session_harness_overrides`, `DELETE FROM schema_migrations WHERE version>=54`} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(ctx, db); err != nil {
			t.Fatalf("migrate #%d: %v", i, err)
		}
	}
	var batch int
	if err := db.QueryRowContext(ctx, `SELECT batch_seq FROM agent_tool_usage_runs WHERE id='keep'`).Scan(&batch); err != nil || batch != 9 {
		t.Fatalf("earlier rows changed: %d %v", batch, err)
	}
	if p, st := profileState(t, s, ctx, "w1"); p != nil || st.Revision != 0 {
		t.Fatalf("a fresh upgrade has no profile: %+v %+v", p, st)
	}
}

func TestMissingRecordsAreAbsentNotEmptyAndNotRevisionZeroDocuments(t *testing.T) {
	s, ctx := harnessFixture(t)
	p, st := profileState(t, s, ctx, "w1")
	if p != nil || st.Present || st.Revision != 0 {
		t.Fatalf("W = %+v %+v", p, st)
	}
	o, st := overrideState(t, s, ctx, "s1", "w1")
	if o != nil || st.Present || st.Revision != 0 {
		t.Fatalf("S = %+v %+v", o, st)
	}
}

func TestAbsentAndPresentEmptySurviveTheStore(t *testing.T) {
	s, ctx := harnessFixture(t)
	empty := hp(func(p *harness.Profile) {
		p.ToolGroups = map[string]bool{}
		p.DisabledMCP = []string{}
		p.Servers = []harness.Server{}
	})
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Value: empty}}); err != nil {
		t.Fatal(err)
	}
	got, st := profileState(t, s, ctx, "w1")
	if st.Revision != 1 || !st.Present || got.Revision != 1 {
		t.Fatalf("stamp %+v revision %d", st, got.Revision)
	}
	if got.ToolGroups == nil || got.DisabledMCP == nil || got.Servers == nil {
		t.Errorf("present-empty fields came back absent: %+v", got)
	}
	if got.SkillFolders != nil || got.DisabledSkills != nil || got.ResultBudget != nil {
		t.Errorf("absent fields came back present: %+v", got)
	}
	// Overrides: sorted, de-duplicated, present-empty kept.
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Value: ho("tasks", "git", "tasks")}}); err != nil {
		t.Fatal(err)
	}
	o, _ := overrideState(t, s, ctx, "s1", "w1")
	if len(o.Groups) != 2 || o.Groups[0] != "git" || o.Groups[1] != "tasks" {
		t.Errorf("groups = %v", o.Groups)
	}
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Expect: harness.Stamp{Revision: 1}, Value: &harness.Override{Schema: harness.SchemaVersion, Groups: []string{}}}}); err != nil {
		t.Fatal(err)
	}
	if o, _ = overrideState(t, s, ctx, "s1", "w1"); o.Groups == nil {
		t.Error("an empty S must stay present")
	}
}

func TestUnreadableStoredRecordsAreCorruptNeverDefaults(t *testing.T) {
	s, ctx := harnessFixture(t)
	for name, q := range map[string]string{
		"future schema":    `INSERT INTO workspace_harness_profiles VALUES('w1',9,1,0,'{"schema":9}','x','x')`,
		"bad json":         `INSERT INTO workspace_harness_profiles VALUES('w1',1,1,0,'{nope','x','x')`,
		"unknown field":    `INSERT INTO workspace_harness_profiles VALUES('w1',1,1,0,'{"schema":1,"toolGroupz":{}}','x','x')`,
		"schema skew":      `INSERT INTO workspace_harness_profiles VALUES('w1',1,1,0,'{"schema":2}','x','x')`,
		"trailing data":    `INSERT INTO workspace_harness_profiles VALUES('w1',1,1,0,'{"schema":1}{"schema":1}','x','x')`,
		"stray bracket":    `INSERT INTO workspace_harness_profiles VALUES('w1',1,1,0,'{"schema":1}]','x','x')`,
		"stray brace":      `INSERT INTO workspace_harness_profiles VALUES('w1',1,1,0,'{"schema":1}}','x','x')`,
		"stray text":       `INSERT INTO workspace_harness_profiles VALUES('w1',1,1,0,'{"schema":1} x','x','x')`,
		"future tombstone": `INSERT INTO workspace_harness_profiles VALUES('w1',9,3,1,'','x','x')`,
	} {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM workspace_harness_profiles`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p, _, err := s.GetProfile(ctx, "w1"); !errors.Is(err, harness.ErrStoreCorrupt) || p != nil {
			t.Errorf("%s: %+v %v", name, p, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO session_harness_overrides VALUES('s1','w1',1,1,0,'{"schema":1,"groups":"git"}','x','x')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetOverride(ctx, "s1", "w1"); !errors.Is(err, harness.ErrStoreCorrupt) {
		t.Errorf("malformed S: %v", err)
	}
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Value: &harness.Profile{Schema: 7}}}); !errors.Is(err, harness.ErrStoreCorrupt) {
		t.Errorf("writing an unsupported schema: %v", err)
	}
}

func TestCompareAndSwapAndDeleteRecreateNeverReusesAToken(t *testing.T) {
	s, ctx := harnessFixture(t)
	write := func(expect int64, v *harness.Profile) (harness.Applied, error) {
		return s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Expect: harness.Stamp{Revision: expect}, Value: v}})
	}
	if a, err := write(0, hp(nil)); err != nil || a.Profile.Revision != 1 {
		t.Fatalf("create: %+v %v", a, err)
	}
	if _, err := write(0, hp(nil)); !errors.Is(err, harness.ErrStoreConflict) {
		t.Fatalf("a second create from absent must lose: %v", err)
	}
	if a, err := write(1, hp(func(p *harness.Profile) { p.DisabledMCP = []string{"x"} })); err != nil || a.Profile.Revision != 2 {
		t.Fatalf("update: %+v %v", a, err)
	}
	if _, err := write(1, hp(nil)); !errors.Is(err, harness.ErrStoreConflict) {
		t.Fatalf("a stale update must lose: %v", err)
	}
	if a, err := write(2, nil); err != nil || a.Profile.Revision != 3 || a.Profile.Present {
		t.Fatalf("delete: %+v %v", a, err)
	}
	p, st := profileState(t, s, ctx, "w1")
	if p != nil || st.Present || st.Revision != 3 {
		t.Fatalf("a deleted profile is absent but its token keeps counting: %+v %+v", p, st)
	}
	// The ABA case: someone who read "absent" (0) before the profile existed.
	if _, err := write(0, hp(nil)); !errors.Is(err, harness.ErrStoreConflict) {
		t.Fatalf("a token from before the delete must not recreate: %v", err)
	}
	if _, err := write(1, hp(nil)); !errors.Is(err, harness.ErrStoreConflict) {
		t.Fatalf("an old present token must not recreate: %v", err)
	}
	if a, err := write(3, hp(nil)); err != nil || a.Profile.Revision != 4 {
		t.Fatalf("recreate from the tombstone token: %+v %v", a, err)
	}
	// Deleting what is absent changes nothing and keeps its token.
	if _, err := write(4, nil); err != nil {
		t.Fatal(err)
	}
	if a, err := write(5, nil); err != nil || a.Profile.Revision != 5 {
		t.Fatalf("delete of absent: %+v %v", a, err)
	}
	var conflict *harness.ConflictError
	if _, err := write(99, hp(nil)); !errors.As(err, &conflict) || conflict.Target != "profile" {
		t.Fatalf("conflict must name W: %v", err)
	}
}

func TestConcurrentCreatesAndUpdatesHaveExactlyOneWinner(t *testing.T) {
	s, ctx := harnessFixture(t)
	race := func(expect int64) (wins int) {
		var mu sync.Mutex
		var wg sync.WaitGroup
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Expect: harness.Stamp{Revision: expect}, Value: hp(nil)}})
				if err == nil {
					mu.Lock()
					wins++
					mu.Unlock()
				} else if !errors.Is(err, harness.ErrStoreConflict) {
					t.Errorf("unexpected: %v", err)
				}
			}()
		}
		wg.Wait()
		return wins
	}
	if w := race(0); w != 1 {
		t.Fatalf("create winners = %d", w)
	}
	if w := race(1); w != 1 {
		t.Fatalf("update winners = %d", w)
	}
	if _, st := profileState(t, s, ctx, "w1"); st.Revision != 2 {
		t.Fatalf("revision = %d", st.Revision)
	}
}

func TestSaveToWorkspaceIsAllOrNothing(t *testing.T) {
	s, ctx := harnessFixture(t)
	both := func(pe, oe int64) harness.StoreWrite {
		return harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1",
			Profile:  &harness.ProfileWrite{Expect: harness.Stamp{Revision: pe}, Value: hp(func(p *harness.Profile) { p.DisabledMCP = []string{"a"} })},
			Override: &harness.OverrideWrite{Expect: harness.Stamp{Revision: oe}, Value: ho("git")}}
	}
	a, err := s.Apply(ctx, both(0, 0))
	if err != nil || a.Profile.Revision != 1 || a.Override.Revision != 1 {
		t.Fatalf("both: %+v %v", a, err)
	}

	// S loses its CAS after W's update was applied inside the transaction.
	if _, err = s.Apply(ctx, both(1, 0)); !errors.Is(err, harness.ErrStoreConflict) {
		t.Fatalf("S conflict: %v", err)
	}
	var conflict *harness.ConflictError
	if !errors.As(err, &conflict) || conflict.Target != "override" {
		t.Fatalf("the conflict must name S: %v", err)
	}
	if _, st := profileState(t, s, ctx, "w1"); st.Revision != 1 {
		t.Fatalf("W was written although S lost: %+v", st)
	}

	// W loses: S must not be written.
	before, _ := overrideState(t, s, ctx, "s1", "w1")
	if _, err = s.Apply(ctx, both(0, 1)); !errors.Is(err, harness.ErrStoreConflict) {
		t.Fatal(err)
	}
	after, st := overrideState(t, s, ctx, "s1", "w1")
	if st.Revision != 1 || len(after.Groups) != len(before.Groups) {
		t.Fatalf("S changed although W lost: %+v", st)
	}

	// A SQL failure on S (a trigger, not a mock) rolls W back.
	if _, err = s.db.ExecContext(ctx, `CREATE TRIGGER fail_s BEFORE UPDATE ON session_harness_overrides BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Apply(ctx, both(1, 1)); err == nil || errors.Is(err, harness.ErrStoreConflict) {
		t.Fatalf("expected an injected SQL failure, got %v", err)
	}
	if p, st := profileState(t, s, ctx, "w1"); st.Revision != 1 || len(p.DisabledMCP) != 1 {
		t.Fatalf("W survived a failed S write: %+v", st)
	}
	if _, err = s.db.ExecContext(ctx, `DROP TRIGGER fail_s`); err != nil {
		t.Fatal(err)
	}

	// And the in-transaction hook between the two writes.
	s.betweenWrites = func() error { return errors.New("injected") }
	if _, err = s.Apply(ctx, both(1, 1)); err == nil {
		t.Fatal("expected the hook failure")
	}
	s.betweenWrites = nil
	if _, st := profileState(t, s, ctx, "w1"); st.Revision != 1 {
		t.Fatalf("W committed before S: %+v", st)
	}
	if a, err = s.Apply(ctx, both(1, 1)); err != nil || a.Profile.Revision != 2 || a.Override.Revision != 2 {
		t.Fatalf("the store still works after rollbacks: %+v %v", a, err)
	}
}

func TestSessionOnlyWriteLeavesTheProfileAlone(t *testing.T) {
	s, ctx := harnessFixture(t)
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Value: ho("git")}}); err != nil {
		t.Fatal(err)
	}
	if p, st := profileState(t, s, ctx, "w1"); p != nil || st.Revision != 0 {
		t.Fatalf("W touched by an S-only write: %+v %+v", p, st)
	}
}

func TestScopeIsEnforcedInsideTheTransaction(t *testing.T) {
	s, ctx := harnessFixture(t)
	bad := map[string]harness.StoreWrite{
		"session of another workspace": {WorkspaceID: "w1", SessionID: "s2", Override: &harness.OverrideWrite{Value: ho("git")}},
		"session without a workspace":  {WorkspaceID: "w1", SessionID: "s0", Override: &harness.OverrideWrite{Value: ho("git")}},
		"unknown session":              {WorkspaceID: "w1", SessionID: "nope", Override: &harness.OverrideWrite{Value: ho("git")}},
		"unknown workspace":            {WorkspaceID: "nope", Profile: &harness.ProfileWrite{Value: hp(nil)}},
		"override without a session":   {WorkspaceID: "w1", Override: &harness.OverrideWrite{Value: ho("git")}},
		"no workspace":                 {Profile: &harness.ProfileWrite{Value: hp(nil)}},
		"nothing to write":             {WorkspaceID: "w1"},
	}
	for name, w := range bad {
		if _, err := s.Apply(ctx, w); !errors.Is(err, harness.ErrStoreScope) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A rejected S must not have written the W that rode along.
	_, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s2",
		Profile:  &harness.ProfileWrite{Value: hp(nil)},
		Override: &harness.OverrideWrite{Value: ho("git")}})
	if !errors.Is(err, harness.ErrStoreScope) {
		t.Fatal(err)
	}
	if p, _ := profileState(t, s, ctx, "w1"); p != nil {
		t.Fatal("W written beside a rejected S")
	}
}

func TestAnOverrideDoesNotFollowASessionToAnotherWorkspace(t *testing.T) {
	s, ctx := harnessFixture(t)
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Value: ho("git")}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetOverride(ctx, "s1", "w2"); !errors.Is(err, harness.ErrStoreScope) {
		t.Fatalf("reading S under another workspace: %v", err)
	}
	// The session moved to w2: writing S there needs the stamp the store holds.
	if _, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET workspace_id='w2' WHERE id='s1'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetOverride(ctx, "s1", "w2"); !errors.Is(err, harness.ErrStoreScope) {
		t.Fatalf("S must not be transferred silently: %v", err)
	}
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w2", SessionID: "s1", Override: &harness.OverrideWrite{Value: ho("tasks")}}); !errors.Is(err, harness.ErrStoreConflict) {
		t.Fatalf("a blind write must not replace S written for another workspace: %v", err)
	}
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w2", SessionID: "s1", Override: &harness.OverrideWrite{Expect: harness.Stamp{Revision: 1}, Value: nil}}); err != nil {
		t.Fatalf("an explicit delete with the stored token clears it: %v", err)
	}
}

func TestDeletingTheSessionOrWorkspaceRemovesItsRecords(t *testing.T) {
	s, ctx := harnessFixture(t)
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1",
		Profile: &harness.ProfileWrite{Value: hp(nil)}, Override: &harness.OverrideWrite{Value: ho("git")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM agent_sessions WHERE id='s1'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM session_harness_overrides`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("S outlived its session: %d %v", n, err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM workspaces WHERE id='w1'`); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM workspace_harness_profiles`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("W outlived its workspace: %d %v", n, err)
	}
}

func TestReopeningTheDatabaseKeepsTheRecords(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "reopen.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO workspaces(id,name,root) VALUES('w1','one','/one')`); err != nil {
		t.Fatal(err)
	}
	s := NewHarnessStore(db)
	if _, err = s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Value: hp(func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false} })}}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if db, err = Open(path); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	p, st, err := NewHarnessStore(db).GetProfile(ctx, "w1")
	if err != nil || st.Revision != 1 || p.ToolGroups["git"] {
		t.Fatalf("after reopen: %+v %+v %v", p, st, err)
	}
}

// A tombstone is "absent" only when this build understands it; one written under
// another schema must not let a consumer fall back to the defaults.
func TestUnknownSchemaTombstonesAreCorruptNotAbsent(t *testing.T) {
	s, ctx := harnessFixture(t)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO session_harness_overrides VALUES('s1','w1',9,2,1,'','x','x')`); err != nil {
		t.Fatal(err)
	}
	if o, _, err := s.GetOverride(ctx, "s1", "w1"); !errors.Is(err, harness.ErrStoreCorrupt) || o != nil {
		t.Fatalf("future-schema override tombstone: %+v %v", o, err)
	}
	// A current-schema tombstone is still plain absence with its token.
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Value: hp(nil)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Expect: harness.Stamp{Revision: 1}}}); err != nil {
		t.Fatal(err)
	}
	if p, st, err := s.GetProfile(ctx, "w1"); err != nil || p != nil || st.Revision != 2 || st.Present {
		t.Fatalf("a current tombstone: %+v %+v %v", p, st, err)
	}
	// A tombstone that somehow holds a body is not trusted either.
	if _, err := s.db.ExecContext(ctx, `PRAGMA ignore_check_constraints=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE workspace_harness_profiles SET body='{"schema":1}' WHERE workspace_id='w1'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetProfile(ctx, "w1"); !errors.Is(err, harness.ErrStoreCorrupt) {
		t.Fatalf("a tombstone with a body: %v", err)
	}
}

func closeSession(t *testing.T, s *HarnessStore, ctx context.Context, id string) {
	t.Helper()
	if _, err := s.db.ExecContext(ctx, `UPDATE agent_sessions SET status='closed' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
}

func TestAClosedSessionCanNotReceiveAnOverride(t *testing.T) {
	s, ctx := harnessFixture(t)
	closeSession(t, s, ctx, "s1")
	_, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Value: ho("git")}})
	if !errors.Is(err, harness.ErrStoreScope) {
		t.Fatalf("a closed session accepted an override: %v", err)
	}
	// W is untouched by the same refused write.
	_, err = s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1",
		Profile: &harness.ProfileWrite{Value: hp(nil)}, Override: &harness.OverrideWrite{Value: ho("git")}})
	if !errors.Is(err, harness.ErrStoreScope) {
		t.Fatal(err)
	}
	if p, _ := profileState(t, s, ctx, "w1"); p != nil {
		t.Fatal("W was written beside a refused S")
	}
	// Other sessions of the workspace are unaffected.
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", Profile: &harness.ProfileWrite{Value: hp(nil)}}); err != nil {
		t.Fatal(err)
	}
}

func TestClearOverrideTombstonesMonotonicallyAndIsIdempotent(t *testing.T) {
	s, ctx := harnessFixture(t)
	if st, err := s.ClearOverride(ctx, "s1"); err != nil || st.Revision != 0 || st.Present {
		t.Fatalf("clearing what is absent: %+v %v", st, err)
	}
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Value: ho("git")}}); err != nil {
		t.Fatal(err)
	}
	st, err := s.ClearOverride(ctx, "s1")
	if err != nil || st.Revision != 2 || st.Present {
		t.Fatalf("clear: %+v %v", st, err)
	}
	if again, err := s.ClearOverride(ctx, "s1"); err != nil || again.Revision != 2 {
		t.Fatalf("a retry must keep the token: %+v %v", again, err)
	}
	if o, got := overrideState(t, s, ctx, "s1", "w1"); o != nil || got.Revision != 2 {
		t.Fatalf("after clear: %+v %+v", o, got)
	}
	// A writer that read the override before the close cannot bring it back.
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Expect: harness.Stamp{Revision: 1}, Value: ho("tasks")}}); !errors.Is(err, harness.ErrStoreConflict) {
		t.Fatalf("a stale token resurrected the override: %v", err)
	}
	for _, id := range []string{""} {
		if _, err := s.ClearOverride(ctx, id); !errors.Is(err, harness.ErrStoreScope) {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestAFailedClearReportsTheFailureAndChangesNothing(t *testing.T) {
	s, ctx := harnessFixture(t)
	if _, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Value: ho("git")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER fail_clear BEFORE UPDATE ON session_harness_overrides BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClearOverride(ctx, "s1"); err == nil {
		t.Fatal("a failed cleanup was reported as success")
	}
	if o, st := overrideState(t, s, ctx, "s1", "w1"); o == nil || st.Revision != 1 {
		t.Fatalf("the override changed although the clear failed: %+v %+v", o, st)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER fail_clear`); err != nil {
		t.Fatal(err)
	}
	if st, err := s.ClearOverride(ctx, "s1"); err != nil || st.Revision != 2 {
		t.Fatalf("a retry after the failure: %+v %v", st, err)
	}
}

// Close versus a late approval, in both orders and under contention.
func TestCloseAndApplyNeverLeaveAnOverrideOnAClosedSession(t *testing.T) {
	for order := 0; order < 2; order++ {
		s, ctx := harnessFixture(t)
		write := func() error {
			_, err := s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Value: ho("git")}})
			return err
		}
		closeAndClear := func() {
			closeSession(t, s, ctx, "s1")
			if _, err := s.ClearOverride(ctx, "s1"); err != nil {
				t.Error(err)
			}
		}
		if order == 0 {
			_ = write()
			closeAndClear()
		} else {
			closeAndClear()
			if err := write(); !errors.Is(err, harness.ErrStoreScope) {
				t.Fatalf("apply after close: %v", err)
			}
		}
		if o, _, _ := s.GetOverride(ctx, "s1", "w1"); o != nil {
			t.Fatalf("order %d left an override on a closed session", order)
		}
	}
	for i := 0; i < 3; i++ {
		s, ctx := harnessFixture(t)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.Apply(ctx, harness.StoreWrite{WorkspaceID: "w1", SessionID: "s1", Override: &harness.OverrideWrite{Value: ho("git")}})
		}()
		go func() {
			defer wg.Done()
			_, _ = s.db.ExecContext(ctx, `UPDATE agent_sessions SET status='closed' WHERE id='s1'`)
			_, _ = s.ClearOverride(ctx, "s1")
		}()
		wg.Wait()
		if o, _, _ := s.GetOverride(ctx, "s1", "w1"); o != nil {
			t.Fatalf("run %d: a racing apply outlived the close cleanup", i)
		}
	}
}
