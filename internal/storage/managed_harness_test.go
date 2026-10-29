package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"devkit/internal/agentwork"
	"devkit/internal/storage"
)

// The ledger is the store the harness binding service runs on.
var _ agentwork.ManagedHarnessStore = (*storage.ManagedLedger)(nil)

func rev(v int64) *int64 { return &v }

func harnessFacts() agentwork.HarnessFacts {
	return agentwork.HarnessFacts{
		SourceSchema: agentwork.HarnessSourceSchemaSupported, Vendor: "claude", WorkspaceID: "payload-workspace",
		VendorRevision: 3, WorkspaceRevision: rev(2), Fingerprint: "fp-1", ExecutableDigest: "ex-1",
	}
}

// harnessFixture is a package fixture with its package built (attempt reserved).
type harnessFixture struct {
	*managedPackageFixture
	pkg agentwork.ContextPackage
}

func newHarnessFixture(t *testing.T) *harnessFixture {
	t.Helper()
	f := newManagedPackageFixture(t)
	pkg, err := f.service.BuildContextPackage(context.Background(), f.attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &harnessFixture{managedPackageFixture: f, pkg: pkg}
}

func (f *harnessFixture) binding(phase string) agentwork.HarnessBinding {
	b := agentwork.HarnessBinding{
		ID: "hb-" + phase, SchemaVersion: agentwork.HarnessBindingSchemaVersion, Phase: phase,
		WorkspaceID: f.group.WorkspaceID, BoardID: f.group.BoardID, ParentTaskID: f.group.ParentTaskID, GroupID: f.group.ID,
		WorkItemID: f.item.ID, TaskID: f.item.TaskID, AttemptID: f.attempt.ID, PackageID: f.pkg.ID, Role: "implementer",
		Required: []string{"agenthub", "git", "tasks"}, Facts: harnessFacts(), CreatedAt: time.Now().UTC(),
	}
	if phase == agentwork.HarnessPhaseActual {
		b.SessionID = f.attempt.SessionID
	}
	return b
}

func (f *harnessFixture) advance(t *testing.T, to string) {
	t.Helper()
	var err error
	f.attempt, err = f.ledger.TransitionAttempt(context.Background(), f.attempt.ID, f.attempt.Revision, to)
	if err != nil {
		t.Fatal(err)
	}
}

func (f *harnessFixture) rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM agent_work_harness_bindings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestManagedHarnessBindingsMigrationFreshAndUpgradeFrom55(t *testing.T) {
	f := newHarnessFixture(t)
	ctx := context.Background()
	var version int
	if err := f.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 56 {
		t.Fatalf("fresh version = %d, err = %v", version, err)
	}
	var packageHash string
	if err := f.db.QueryRow(`SELECT body_hash FROM agent_work_packages WHERE id=?`, f.pkg.ID).Scan(&packageHash); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DROP TABLE agent_work_harness_bindings`,
		`DELETE FROM schema_migrations WHERE version>=56`,
		`INSERT INTO ui_state(key,value) VALUES('upgrade-sentinel','{"retained":true}')`,
	} {
		if _, err := f.db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := storage.Migrate(ctx, f.db); err != nil {
			t.Fatalf("migrate %d: %v", i, err)
		}
	}
	var tables int
	if err := f.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('agent_work_harness_bindings','agent_work_harness_bindings_no_update')`).Scan(&tables); err != nil || tables != 2 {
		t.Fatalf("table and trigger = %d, err = %v", tables, err)
	}
	if err := f.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 56 {
		t.Fatalf("upgraded version = %d, err = %v", version, err)
	}
	var after string
	if err := f.db.QueryRow(`SELECT body_hash FROM agent_work_packages WHERE id=?`, f.pkg.ID).Scan(&after); err != nil || after != packageHash {
		t.Fatalf("package row from migration 53 changed: %v", err)
	}
	var kept string
	if err := f.db.QueryRow(`SELECT value FROM ui_state WHERE key='upgrade-sentinel'`).Scan(&kept); err != nil || kept != `{"retained":true}` {
		t.Fatalf("existing data changed: %v", err)
	}
	if err := f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseIntended), f.attempt.Revision); err != nil {
		t.Fatalf("a binding can be written after the upgrade: %v", err)
	}
}

func TestHarnessBindingTableHasNoModelAccountOrSecretColumns(t *testing.T) {
	f := newHarnessFixture(t)
	rows, err := f.db.Query(`SELECT name FROM pragma_table_info('agent_work_harness_bindings')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	want := []string{"id", "workspace_id", "board_id", "parent_task_id", "group_id", "work_item_id", "task_id", "attempt_id", "package_id", "phase",
		"binding_schema", "source_schema", "role", "session_id", "vendor", "harness_workspace_id", "vendor_revision", "workspace_revision",
		"session_revision", "fingerprint", "executable_digest", "facts_complete", "is_default", "required_groups", "created_at"}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("columns = %v, want exactly %v", got, want)
	}
}

func TestHarnessBindingRowIsImmutable(t *testing.T) {
	f := newHarnessFixture(t)
	ctx := context.Background()
	first := f.binding(agentwork.HarnessPhaseIntended)
	if err := f.ledger.InsertHarnessBinding(ctx, first, f.attempt.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(ctx, `UPDATE agent_work_harness_bindings SET fingerprint='changed' WHERE id=?`, first.ID); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("update must abort as immutable, got %v", err)
	}
	second := f.binding(agentwork.HarnessPhaseIntended)
	second.ID = "hb-second"
	second.Facts.Fingerprint = "fp-replacement"
	if err := f.ledger.InsertHarnessBinding(ctx, second, f.attempt.Revision); !errors.Is(err, agentwork.ErrHarnessBindingExists) {
		t.Fatalf("second insert for the same attempt and phase = %v", err)
	}
	got, err := f.ledger.GetHarnessBinding(ctx, f.attempt.ID, agentwork.HarnessPhaseIntended)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != first.ID || got.Facts.Fingerprint != "fp-1" || !reflect.DeepEqual(got.Required, first.Required) || !reflect.DeepEqual(got.Facts, first.Facts) {
		t.Fatalf("stored binding changed: %+v", got)
	}
	if _, err = f.ledger.GetHarnessBinding(ctx, f.attempt.ID, agentwork.HarnessPhaseActual); !errors.Is(err, agentwork.ErrManagedRecordNotFound) {
		t.Fatalf("absent phase = %v", err)
	}
}

func TestHarnessBindingRejectsCrossScopeAndForeignPackage(t *testing.T) {
	f := newHarnessFixture(t)
	ctx := context.Background()
	// A second work item with its own package.
	reqs2 := f.reqs
	reqs2.TaskID = "payload-task-2"
	digest2, err := agentwork.RequirementsDigest(reqs2)
	if err != nil {
		t.Fatal(err)
	}
	item2, err := f.ledger.CreateWorkItem(ctx, agentwork.WorkItem{ID: "payload-item-2", GroupID: f.group.ID, WorkspaceID: f.group.WorkspaceID, BoardID: f.group.BoardID, ParentTaskID: f.group.ParentTaskID, TaskID: reqs2.TaskID, Role: "implementer", TaskRevision: 1, RequirementsRevision: digest2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.ledger.BeginAttempt(ctx, agentwork.Attempt{ID: "payload-attempt-2", WorkItemID: item2.ID, SessionID: "worker-session-2", TaskRevision: item2.TaskRevision, RequirementsRevision: item2.RequirementsRevision}, item2.Revision); err != nil {
		t.Fatal(err)
	}
	f.host.source.Requirements = reqs2
	pkg2, err := f.service.BuildContextPackage(ctx, "payload-attempt-2")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*agentwork.HarnessBinding){
		"package of another attempt": func(b *agentwork.HarnessBinding) { b.PackageID = pkg2.ID },
		"unknown package":            func(b *agentwork.HarnessBinding) { b.PackageID = "no-such-package" },
		"other workspace":            func(b *agentwork.HarnessBinding) { b.WorkspaceID = "other-workspace" },
		"other group":                func(b *agentwork.HarnessBinding) { b.GroupID = "other-group" },
		"other task":                 func(b *agentwork.HarnessBinding) { b.TaskID = "payload-task-2" },
		"other work item":            func(b *agentwork.HarnessBinding) { b.WorkItemID = item2.ID },
		"other role":                 func(b *agentwork.HarnessBinding) { b.Role = "reviewer" },
	}
	for name, mutate := range cases {
		b := f.binding(agentwork.HarnessPhaseIntended)
		mutate(&b)
		err = f.ledger.InsertHarnessBinding(ctx, b, f.attempt.Revision)
		if err == nil || (!errors.Is(err, storage.ErrManagedScope) && !errors.Is(err, storage.ErrManagedNotFound)) {
			t.Fatalf("%s: err = %v, want a scope refusal", name, err)
		}
	}
	if f.rows(t) != 0 {
		t.Fatalf("a refused binding must leave no row")
	}
}

func TestInsertHarnessBindingFencesAttemptRevisionStateAndPause(t *testing.T) {
	ctx := context.Background()
	t.Run("stale attempt revision", func(t *testing.T) {
		f := newHarnessFixture(t)
		if err := f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseIntended), f.attempt.Revision+1); !errors.Is(err, storage.ErrManagedConflict) {
			t.Fatalf("err = %v", err)
		}
		if f.rows(t) != 0 {
			t.Fatal("row written")
		}
	})
	t.Run("actual while reserved", func(t *testing.T) {
		f := newHarnessFixture(t)
		if err := f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseActual), f.attempt.Revision); !errors.Is(err, agentwork.ErrHarnessAttemptState) {
			t.Fatalf("err = %v", err)
		}
		if f.rows(t) != 0 {
			t.Fatal("row written")
		}
	})
	t.Run("intended after the session exists", func(t *testing.T) {
		f := newHarnessFixture(t)
		f.advance(t, "session_created")
		if err := f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseIntended), f.attempt.Revision); !errors.Is(err, agentwork.ErrHarnessAttemptState) {
			t.Fatalf("err = %v", err)
		}
		if f.rows(t) != 0 {
			t.Fatal("row written")
		}
	})
	t.Run("actual without an intended binding or with another session", func(t *testing.T) {
		f := newHarnessFixture(t)
		f.advance(t, "session_created")
		if err := f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseActual), f.attempt.Revision); !errors.Is(err, storage.ErrManagedScope) {
			t.Fatalf("no intended binding: err = %v", err)
		}
		f2 := newHarnessFixture(t)
		if err := f2.ledger.InsertHarnessBinding(ctx, f2.binding(agentwork.HarnessPhaseIntended), f2.attempt.Revision); err != nil {
			t.Fatal(err)
		}
		f2.advance(t, "session_created")
		b := f2.binding(agentwork.HarnessPhaseActual)
		b.SessionID = "a-different-session"
		if err := f2.ledger.InsertHarnessBinding(ctx, b, f2.attempt.Revision); !errors.Is(err, storage.ErrManagedScope) {
			t.Fatalf("other session: err = %v", err)
		}
		if f2.rows(t) != 1 || f.rows(t) != 0 {
			t.Fatal("unexpected rows")
		}
		if err := f2.ledger.InsertHarnessBinding(ctx, f2.binding(agentwork.HarnessPhaseActual), f2.attempt.Revision); err != nil {
			t.Fatalf("a matching actual binding must be accepted: %v", err)
		}
	})
	t.Run("paused and disabled group", func(t *testing.T) {
		for name, flags := range map[string][2]bool{"paused": {true, true}, "disabled": {false, false}} { // {enabled, paused}
			f := newHarnessFixture(t)
			g, err := f.ledger.GetWorkGroup(ctx, f.group.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.ledger.UpdateGroupPolicy(ctx, g.ID, g.Revision, g.PolicyRevision+1, flags[0], flags[1]); err != nil {
				t.Fatal(err)
			}
			if err = f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseIntended), f.attempt.Revision); !errors.Is(err, agentwork.ErrHarnessPolicyClosed) {
				t.Fatalf("%s: err = %v", name, err)
			}
			if f.rows(t) != 0 {
				t.Fatalf("%s: row written", name)
			}
		}
	})
	t.Run("tombstoned item", func(t *testing.T) {
		f := newHarnessFixture(t)
		item, err := f.ledger.GetWorkItem(ctx, f.item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.ledger.UpdateWorkItemHostState(ctx, item.ID, item.Revision, item.TaskRevision, item.RequirementsRevision, "todo", "ended", agentwork.TombstoneUserEnded); err != nil {
			t.Fatal(err)
		}
		if err = f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseIntended), f.attempt.Revision); !errors.Is(err, agentwork.ErrHarnessPolicyClosed) {
			t.Fatalf("a tombstoned item must refuse the binding: %v", err)
		}
		if f.rows(t) != 0 {
			t.Fatal("row written")
		}
	})
}

func TestHarnessBindingZeroWorkspaceRevisionIsRejected(t *testing.T) {
	f := newHarnessFixture(t)
	ctx := context.Background()
	b := f.binding(agentwork.HarnessPhaseIntended)
	b.Facts.WorkspaceRevision = rev(0)
	if err := f.ledger.InsertHarnessBinding(ctx, b, f.attempt.Revision); !errors.Is(err, storage.ErrManagedInvalid) {
		t.Fatalf("zero revision via the store = %v", err)
	}
	b = f.binding(agentwork.HarnessPhaseIntended)
	b.Facts.SessionRevision = rev(0)
	if err := f.ledger.InsertHarnessBinding(ctx, b, f.attempt.Revision); !errors.Is(err, storage.ErrManagedInvalid) {
		t.Fatalf("zero session revision via the store = %v", err)
	}
	// The table itself refuses it too, not only the Go code.
	_, err := f.db.ExecContext(ctx, `INSERT INTO agent_work_harness_bindings
		(id,workspace_id,board_id,parent_task_id,group_id,work_item_id,task_id,attempt_id,package_id,phase,binding_schema,source_schema,role,session_id,vendor,harness_workspace_id,vendor_revision,workspace_revision,session_revision,fingerprint,executable_digest,facts_complete,is_default,required_groups,created_at)
		VALUES('raw',?,?,?,?,?,?,?,?,'intended',1,1,'implementer',NULL,'claude',?,1,0,NULL,'fp','ex',0,0,'[]','2026-10-29T00:00:00Z')`,
		f.group.WorkspaceID, f.group.BoardID, f.group.ParentTaskID, f.group.ID, f.item.ID, f.item.TaskID, f.attempt.ID, f.pkg.ID, f.group.WorkspaceID)
	if err == nil {
		t.Fatal("a workspace revision of 0 must violate the table check")
	}
	if f.rows(t) != 0 {
		t.Fatal("row written")
	}
}

func TestHarnessBindingUnknownSchemaFailsClosedOnRead(t *testing.T) {
	for _, column := range []string{"binding_schema", "source_schema"} {
		f := newHarnessFixture(t)
		ctx := context.Background()
		if err := f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseIntended), f.attempt.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.ExecContext(ctx, `DROP TRIGGER agent_work_harness_bindings_no_update`); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.ExecContext(ctx, fmt.Sprintf(`UPDATE agent_work_harness_bindings SET %s=99`, column)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.ledger.GetHarnessBinding(ctx, f.attempt.ID, agentwork.HarnessPhaseIntended); !errors.Is(err, storage.ErrManagedInvalid) {
			t.Fatalf("%s=99: err = %v", column, err)
		}
	}
	f := newHarnessFixture(t)
	bad := f.binding(agentwork.HarnessPhaseIntended)
	bad.SchemaVersion = 99
	if err := f.ledger.InsertHarnessBinding(context.Background(), bad, f.attempt.Revision); !errors.Is(err, storage.ErrManagedInvalid) {
		t.Fatalf("an unknown schema must not be written: %v", err)
	}
	bad = f.binding(agentwork.HarnessPhaseIntended)
	bad.Facts.SourceSchema = 99
	if err := f.ledger.InsertHarnessBinding(context.Background(), bad, f.attempt.Revision); !errors.Is(err, storage.ErrManagedInvalid) {
		t.Fatalf("an unknown source schema must not be written: %v", err)
	}
}

func (f *harnessFixture) snapshot(t *testing.T, query string) string {
	t.Helper()
	rows, err := f.db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out strings.Builder
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out.WriteString(fmt.Sprintf("%v\n", vals))
	}
	return out.String()
}

func TestPackageRowAndLeaseSurviveBinding(t *testing.T) {
	f := newHarnessFixture(t)
	ctx := context.Background()
	if err := f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseIntended), f.attempt.Revision); err != nil {
		t.Fatal(err)
	}
	f.advance(t, "session_created")
	f.advance(t, "task_assigned")
	insertActiveArtifactLease(t, f.managedPackageFixture)
	packageQuery := `SELECT * FROM agent_work_packages ORDER BY id`
	leaseQuery := `SELECT * FROM agent_work_leases ORDER BY id`
	var sealedBefore []byte
	if err := f.db.QueryRow(`SELECT sealed_body FROM agent_work_packages WHERE id=?`, f.pkg.ID).Scan(&sealedBefore); err != nil {
		t.Fatal(err)
	}
	packageBefore, leaseBefore := f.snapshot(t, packageQuery), f.snapshot(t, leaseQuery)
	if err := f.ledger.InsertHarnessBinding(ctx, f.binding(agentwork.HarnessPhaseActual), f.attempt.Revision); err != nil {
		t.Fatal(err)
	}
	if packageBefore != f.snapshot(t, packageQuery) || leaseBefore != f.snapshot(t, leaseQuery) {
		t.Fatal("inserting a binding changed a package or lease row")
	}
	var sealedAfter []byte
	if err := f.db.QueryRow(`SELECT sealed_body FROM agent_work_packages WHERE id=?`, f.pkg.ID).Scan(&sealedAfter); err != nil || !bytes.Equal(sealedBefore, sealedAfter) {
		t.Fatalf("sealed package body changed: %v", err)
	}
	stored, err := f.ledger.GetContextPackage(ctx, f.pkg.ID)
	if err != nil || stored.BodyHash != f.pkg.BodyHash || stored.SchemaVersion != agentwork.ManagedPackageSchemaVersion {
		t.Fatalf("package no longer reads as before: %v", err)
	}
}
