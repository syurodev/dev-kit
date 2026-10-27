package preferences_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/preferences"
	"devkit/internal/storage"
)

func policyDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestTheHarnessPolicyIsOffUntilThePersonTurnsItOnAndSurvivesARestart(t *testing.T) {
	db := policyDB(t)
	svc := preferences.NewService(preferences.NewSQLRepository(db))
	if p, err := svc.AgentHarnessPolicy(); err != nil || p.AutoNarrowing || p.Revision != 0 {
		t.Fatalf("a profile that never set it is off: %+v %v", p, err)
	}
	on, err := svc.SetAgentHarnessAutoNarrowing(0, true)
	if err != nil || !on.AutoNarrowing || on.Revision != 1 {
		t.Fatalf("turning it on: %+v %v", on, err)
	}
	// A new service over the same database is a restart.
	again := preferences.NewService(preferences.NewSQLRepository(db))
	if p, err := again.AgentHarnessPolicy(); err != nil || p != on {
		t.Fatalf("the choice must persist: %+v %v", p, err)
	}
}

func TestEveryChangeAdvancesTheRevisionSoOffAndOnAgainIsNotTheSamePolicy(t *testing.T) {
	svc := preferences.NewService(preferences.NewMemoryRepository())
	p1, _ := svc.SetAgentHarnessAutoNarrowing(0, true)
	p2, _ := svc.SetAgentHarnessAutoNarrowing(p1.Revision, false)
	p3, err := svc.SetAgentHarnessAutoNarrowing(p2.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if p3.AutoNarrowing != p1.AutoNarrowing || p3.Revision <= p1.Revision || !(p1.Revision < p2.Revision && p2.Revision < p3.Revision) {
		t.Fatalf("off→on must come back as a different revision: %d, %d, %d", p1.Revision, p2.Revision, p3.Revision)
	}
	// Setting the value it already has is not a change.
	same, err := svc.SetAgentHarnessAutoNarrowing(p3.Revision, true)
	if err != nil || same != p3 {
		t.Fatalf("a no-op must not advance the revision: %+v %v", same, err)
	}
}

func TestAStaleRevisionIsAConflictNotAWrite(t *testing.T) {
	svc := preferences.NewService(preferences.NewMemoryRepository())
	if _, err := svc.SetAgentHarnessAutoNarrowing(0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetAgentHarnessAutoNarrowing(0, false); !errors.Is(err, preferences.ErrHarnessPolicyConflict) {
		t.Fatalf("a stale revision must conflict, got %v", err)
	}
	if p, _ := svc.AgentHarnessPolicy(); !p.AutoNarrowing || p.Revision != 1 {
		t.Fatalf("a refused write changed the policy: %+v", p)
	}
}

// Two windows that both read revision 0 cannot both win.
func TestConcurrentSettersCommitExactlyOnce(t *testing.T) {
	svc := preferences.NewService(preferences.NewMemoryRepository())
	var wins, conflicts atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(on bool) {
			defer wg.Done()
			switch _, err := svc.SetAgentHarnessAutoNarrowing(0, on); {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, preferences.ErrHarnessPolicyConflict):
				conflicts.Add(1)
			default:
				t.Error(err)
			}
		}(i%2 == 0)
	}
	wg.Wait()
	if wins.Load() < 1 || wins.Load()+conflicts.Load() != 16 {
		t.Fatalf("wins %d conflicts %d", wins.Load(), conflicts.Load())
	}
	// "Off" over an unset policy is a no-op that succeeds without a new revision, so
	// several calls may "win"; the policy itself changes exactly once, to on.
	if p, _ := svc.AgentHarnessPolicy(); p.Revision != 1 || !p.AutoNarrowing {
		t.Fatalf("exactly one change expected, got %+v", p)
	}
}

type failingPolicyRepo struct {
	preferences.Repository
	fail atomic.Bool
}

func (r *failingPolicyRepo) SetAgentHarnessPolicy(p preferences.AgentHarnessPolicy) error {
	if r.fail.Load() {
		return errors.New("disk full")
	}
	return r.Repository.SetAgentHarnessPolicy(p)
}

func TestAPersistenceFailureLeavesThePreviousPolicyInEffect(t *testing.T) {
	repo := &failingPolicyRepo{Repository: preferences.NewMemoryRepository()}
	svc := preferences.NewService(repo)
	if _, err := svc.SetAgentHarnessAutoNarrowing(0, true); err != nil {
		t.Fatal(err)
	}
	repo.fail.Store(true)
	if _, err := svc.SetAgentHarnessAutoNarrowing(1, false); err == nil {
		t.Fatal("the failure must be reported")
	}
	if p, _ := svc.AgentHarnessPolicy(); !p.AutoNarrowing || p.Revision != 1 {
		t.Fatalf("the previous policy must stay in effect: %+v", p)
	}
}

func TestAnUnreadableStoredPolicyIsOffNeverOn(t *testing.T) {
	db := policyDB(t)
	for _, bad := range []string{`not json`, `{"revision":-3,"autoNarrowing":true}`, `[]`} {
		if _, err := db.Exec(`INSERT INTO app_preferences(key,value) VALUES('agent_harness_policy',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, bad); err != nil {
			t.Fatal(err)
		}
		svc := preferences.NewService(preferences.NewSQLRepository(db))
		if p, err := svc.AgentHarnessPolicy(); err != nil || p.AutoNarrowing {
			t.Fatalf("%q must read as off: %+v %v", bad, p, err)
		}
	}
}

// The setter takes the lock the harness's final recheck and commit run under, so
// it cannot land between them; and it is not held while a person decides.
func TestTheSetterWaitsForTheHarnessWriterBoundary(t *testing.T) {
	svc := preferences.NewService(preferences.NewMemoryRepository())
	entered, release := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- svc.HoldAgentCustomization(func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	done := make(chan error, 1)
	go func() { _, err := svc.SetAgentHarnessAutoNarrowing(0, true); done <- err }()
	select {
	case <-done:
		t.Fatal("the policy changed while the final recheck and commit held the boundary")
	case <-time.After(100 * time.Millisecond):
	}
	if p, _ := svc.AgentHarnessPolicy(); p.AutoNarrowing { // reads do not wait
		t.Fatal("setter wrote before the boundary was released")
	}
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p, _ := svc.AgentHarnessPolicy(); !p.AutoNarrowing || p.Revision != 1 {
		t.Fatalf("the setter must apply once the boundary is free: %+v", p)
	}
}
