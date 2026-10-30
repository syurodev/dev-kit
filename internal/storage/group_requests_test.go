package storage

import (
	"context"
	"path/filepath"
	"testing"
)

// Migration 58 on a database that already has open requests: they are closed (an
// open request never survives an upgrade or a restart) and the integrity rules hold.
func TestGroupRequestIntegrityMigrationClosesOpenRequestsAndEnforcesTheRules(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "gr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,root) VALUES('w1','one','/one')`,
		`INSERT INTO agent_sessions(id,agent_type,project_path,status,depth,created_at,updated_at,workspace_id) VALUES('c1','mock','/one','idle',0,'x','x','w1')`,
		// Roll back to the state after migration 57: no integrity objects, an open request.
		`DROP TRIGGER agent_group_requests_close`, `DROP TRIGGER agent_group_requests_quota`, `DROP TABLE agent_group_request_open_groups`,
		`DELETE FROM schema_migrations WHERE version>=58`,
		`INSERT INTO agent_group_requests(id,child_id,parent_id,groups,reason,route,state,revision,created_at,updated_at) VALUES('old','c1','p1','["git"]','','user','pending_user',1,'2026-10-30T00:00:00Z','2026-10-30T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var state, outcome, resolved string
	if err := db.QueryRowContext(ctx, `SELECT state,outcome,resolved_at FROM agent_group_requests WHERE id='old'`).Scan(&state, &outcome, &resolved); err != nil {
		t.Fatal(err)
	}
	if state != "invalidated" || outcome != "migration" || resolved == "" {
		t.Fatalf("old request = %s %s %q", state, outcome, resolved)
	}
	// Running it again changes nothing and fails nothing.
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	open := func(id, groups string) error {
		_, err := db.ExecContext(ctx, `INSERT INTO agent_group_requests(id,child_id,parent_id,groups,reason,route,state,revision,created_at,updated_at) VALUES(?,?,?,?,'','user','pending_user',1,'t','t')`, id, "c1", "p1", groups)
		return err
	}
	for i, g := range []string{"a", "b", "c", "d"} {
		if err := open("r"+string(rune('0'+i)), `["`+g+`"]`); err != nil {
			t.Fatal(err)
		}
	}
	if err := open("r9", `["e"]`); err == nil {
		t.Fatal("a fifth open request was accepted by SQL")
	}
	// Closing a request through any path frees what the open-group rows hold.
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_group_request_open_groups(request_id,child_id,group_name) VALUES('r0','c1','a')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO agent_group_request_open_groups(request_id,child_id,group_name) VALUES('r1','c1','a')`); err == nil {
		t.Fatal("two open requests claimed one group")
	}
	if _, err := db.ExecContext(ctx, `UPDATE agent_group_requests SET state='denied',resolved_at='t' WHERE id='r0'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM agent_group_request_open_groups WHERE request_id='r0'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a closed request still claims %d groups (%v)", n, err)
	}
}
