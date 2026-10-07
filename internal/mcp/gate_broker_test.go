package mcp

import (
	"context"
	"devkit/internal/database"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"sync/atomic"
	"testing"
	"time"
)

type brokerDB struct {
	fakeDB
	calls      atomic.Int32
	executedID string
}

func (db *brokerDB) Query(_ context.Context, id, _ string, _ int) (database.QueryResult, error) {
	db.calls.Add(1)
	db.executedID = id
	return database.QueryResult{}, nil
}

func TestDBQueryDispatchStillWaitsForGateAndUsesApprovedProfile(t *testing.T) {
	for _, decision := range []string{"allow", "deny"} {
		t.Run(decision, func(t *testing.T) {
			h := interaction.NewHub(interaction.NoopPrompter{})
			requested := make(chan interaction.ConfirmRequest, 1)
			answer := make(chan string, 1)
			h.ConfirmAgent = func(ctx context.Context, req interaction.ConfirmRequest) (string, error) {
				requested <- req
				select {
				case value := <-answer:
					return value, nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}
			s, _ := newDBGateServer("prod", map[string]bool{"prod": true}, h)
			db := &brokerDB{fakeDB: fakeDB{activeID: prodDBID, profiles: []database.ProfileSummary{{ID: prodDBID, Env: "prod"}}}}
			s.deps.DB = db
			ctx := identity.WithCaller(context.Background(), identity.Agent("session-a"))
			done := make(chan error, 1)
			go func() {
				_, _, err := s.toolDBQuery(ctx, &sdk.CallToolRequest{}, dbQueryInput{SQL: "SELECT 1"})
				done <- err
			}()
			select {
			case req := <-requested:
				if req.Tool != "db.query" || req.Summary != "SELECT 1" || !req.EnvBlocks || req.ProfileID != prodDBID {
					t.Fatalf("gate request = %+v", req)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("gate was bypassed")
			}
			if db.calls.Load() != 0 || len(h.PendingList()) != 0 {
				t.Fatal("executed early or also opened popup")
			}
			// The gate approved one captured target, even if another becomes active.
			db.activeID = "other-profile"
			answer <- decision
			err := <-done
			if decision == "allow" {
				if err != nil || db.calls.Load() != 1 || db.executedID != prodDBID {
					t.Fatalf("execution: %v, %d, %s", err, db.calls.Load(), db.executedID)
				}
			} else if err == nil || db.calls.Load() != 0 {
				t.Fatal("denied query executed")
			}
		})
	}
}

func TestGateOwnershipComesFromExplicitRegistrationAndLiveBroker(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	s, _ := newDBGateServer("prod", map[string]bool{"prod": true}, h)
	if len(s.GateOwnedTools()) != 0 {
		t.Fatal("delegation offered without a broker")
	}
	h.ConfirmAgent = func(context.Context, interaction.ConfirmRequest) (string, error) { return "deny", nil }
	found := map[string]bool{}
	for _, name := range s.GateOwnedTools() {
		found[name] = true
	}
	if !found["db.query"] || !found["ssh.runCommand"] || !found["kafka.publishMessage"] || found["notes.delete"] {
		t.Fatalf("ownership = %v", found)
	}
	h.Headless = true
	if len(s.GateOwnedTools()) != 0 {
		t.Fatal("headless delegation offered")
	}
}
