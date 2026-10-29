package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/identity"
	"devkit/internal/mcp"
	"devkit/internal/vault"
	"devkit/internal/workspace"
)

type vaultLocked struct{ mcp.VaultAPI }

func (vaultLocked) Status() (vault.State, error) { return vault.StateLocked, nil }

// dumpTables renders every row of the named tables, so a test can prove a code
// path wrote nothing.
func dumpTables(t *testing.T, db *sql.DB, tables ...string) string {
	t.Helper()
	var out strings.Builder
	for _, table := range tables {
		rows, err := db.Query("SELECT * FROM " + table + " ORDER BY 1, 2") // fixed table names
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		fmt.Fprintf(&out, "[%s]\n", table)
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&out, "%v\n", vals)
		}
		rows.Close()
	}
	return out.String()
}

var harnessDataTables = []string{"workspace_harness_profiles", "session_harness_overrides", "agent_session_harness_applied", "agent_sessions"}

// Startup is a read-free, write-free act on existing data: StartHarness composes
// the graph and binds the listener and touches no profile, override, reference or
// session row, whatever state the data and the vault are in. A stored profile
// that cannot be read does not stop the application; it is refused where it is
// used (a new session: TestACorruptStoredProfileRefusesANewSession...) and is
// never shown as an empty profile. The rows below are the "old data" of an
// application that ran before the harness was activated.
func TestStartupOnExistingDataWritesNothingAndKeepsEveryPathHonest(t *testing.T) {
	ctx := context.Background()
	cases := map[string]struct {
		vault   mcp.VaultAPI
		locked  bool
		setup   func(t *testing.T, r *startupRig, ws string)
		corrupt bool
	}{
		"no profile and no sessions":         {vault: vaultOK{}, setup: func(*testing.T, *startupRig, string) {}},
		"locked vault (every process start)": {vault: vaultLocked{}, locked: true, setup: func(*testing.T, *startupRig, string) {}},
		"old sessions with and without an applied reference": {vault: vaultOK{}, setup: func(t *testing.T, r *startupRig, ws string) {
			store := agenthub.NewSQLStore(r.parts.db)
			now := time.Now().UTC()
			for _, id := range []string{"00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"} {
				if err := store.InsertSession(ctx, agenthub.AgentSession{ID: id, AgentType: agenthub.AgentTypeClaude, ProjectPath: t.TempDir(),
					WorkspaceID: ws, Status: agenthub.SessionStatusIdle, CreatedAt: now, UpdatedAt: now}); err != nil {
					t.Fatal(err)
				}
			}
			// The first has the reference an activated build wrote; the second was
			// created while the harness was inert.
			id := "00000000-0000-4000-8000-000000000001"
			if err := store.SetHarnessApplied(ctx, id, harness.SnapshotRef{Schema: harness.SnapshotSchema, SessionID: id, Default: true}, false, now); err != nil {
				t.Fatal(err)
			}
		}},
		"a stored profile that cannot be read": {vault: vaultOK{}, corrupt: true, setup: func(t *testing.T, r *startupRig, ws string) {
			if _, err := r.parts.db.Exec(`INSERT INTO workspace_harness_profiles VALUES(?,1,1,0,'{nope','x','x')`, ws); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newStartupRigWithVault(t, tc.vault)
			r.parts.hub.SetVaultLocked(tc.locked)
			// Workspaces in the database, as the application has them: sessions and
			// profiles reference them by foreign key.
			r.parts.ws = workspace.NewService(workspace.NewSQLRepository(r.parts.db))
			w, _, err := r.parts.ws.AddWorkspace("one", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			tc.setup(t, r, w.ID)
			before := dumpTables(t, r.parts.db, harnessDataTables...)

			if _, err := StartHarness(r.startup()); err != nil {
				t.Fatalf("startup must succeed on existing data: %v", err)
			}
			if !r.port.Bound() {
				t.Fatal("the port must be bound")
			}
			if after := dumpTables(t, r.parts.db, harnessDataTables...); after != before {
				t.Fatalf("startup wrote to existing data:\nbefore:\n%s\nafter:\n%s", before, after)
			}

			ui := identity.WithCaller(r.ctx, identity.UI())
			view, err := r.port.WorkspaceProfile(ui, w.ID)
			if tc.corrupt {
				if err == nil {
					t.Fatalf("an unreadable stored profile was shown as a profile: %+v", view)
				}
				return
			}
			if err != nil {
				t.Fatalf("the window's read: %v", err)
			}
			if view.ProfilePresent || view.ProfileRevision != 0 || len(view.Groups) == 0 {
				t.Fatalf("no saved profile must read as the defaults, not as a saved one: %+v", view)
			}
			// The same read twice changes nothing either: reading is not saving.
			if _, err := r.port.WorkspaceProfile(ui, w.ID); err != nil {
				t.Fatal(err)
			}
			if after := dumpTables(t, r.parts.db, harnessDataTables...); after != before {
				t.Fatal("reading the profile wrote to existing data")
			}
		})
	}
}
