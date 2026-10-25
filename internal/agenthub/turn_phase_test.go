package agenthub_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
)

type pendingTurnCheckpoint struct{}

func (pendingTurnCheckpoint) ID() string                                  { return "checkpoint" }
func (pendingTurnCheckpoint) Ready() bool                                 { return false }
func (pendingTurnCheckpoint) Wait()                                       {}
func (pendingTurnCheckpoint) Finish(context.Context, agenthub.TurnResult) {}

type pendingCheckpoints struct{}

func (pendingCheckpoints) Enabled() bool   { return true }
func (pendingCheckpoints) Baseline(string) {}
func (pendingCheckpoints) Begin(context.Context, agenthub.TurnCheckpointSpec) agenthub.TurnCheckpoint {
	return pendingTurnCheckpoint{}
}
func (pendingCheckpoints) AttachReport(string, string, json.RawMessage) {}

func TestInvokeEmitsLivePhasesInOrder(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	adapter := agenthub.NewMockAdapter()
	reg := agenthub.NewRegistry()
	reg.Register(adapter)
	var mu sync.Mutex
	var phases []string
	svc := agenthub.NewService(reg, agenthub.NewSQLStore(db), func(_ string, raw json.RawMessage) {
		var update struct {
			Kind  string `json:"kind"`
			Phase string `json:"phase"`
		}
		if json.Unmarshal(raw, &update) == nil && update.Kind == "turn_phase_update" {
			mu.Lock()
			phases = append(phases, update.Phase)
			mu.Unlock()
		}
	})
	svc.SetCheckpoints(pendingCheckpoints{})
	t.Cleanup(func() { _ = svc.Shutdown(context.Background()) })
	session, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: session.ID, Prompt: "phase test"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), phases...)
	mu.Unlock()
	want := []string{"opening_agent", "saving_checkpoint", "waiting_for_model"}
	if len(got) != len(want) {
		t.Fatalf("phases = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("phases = %v, want %v", got, want)
		}
	}
}
