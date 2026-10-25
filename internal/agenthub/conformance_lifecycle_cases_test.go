package agenthub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/storage"
)

type resumeSelectionAdapter struct{ opened, loaded int }

func (*resumeSelectionAdapter) Type() AgentType { return AgentTypeCodex }
func (a *resumeSelectionAdapter) Open(context.Context, OpenSpec) (SessionRuntime, error) {
	a.opened++
	return selectionRuntime{"new"}, nil
}
func (a *resumeSelectionAdapter) Load(_ context.Context, _ OpenSpec, id string) (SessionRuntime, error) {
	a.loaded++
	return selectionRuntime{id}, nil
}
func (*resumeSelectionAdapter) NormalizeUsage(json.RawMessage) (NormalizedUsage, error) {
	return NormalizedUsage{}, nil
}
func (*resumeSelectionAdapter) NormalizeToolSummary(json.RawMessage, json.RawMessage) ([]string, string, error) {
	return nil, "", nil
}
func (*resumeSelectionAdapter) Extensions() []string { return nil }

type selectionRuntime struct{ id string }

func (r selectionRuntime) ACPSessionID() string { return r.id }
func (selectionRuntime) Prompt(context.Context, []acp.ContentBlock, json.RawMessage) (PromptOutcome, error) {
	return PromptOutcome{}, nil
}
func (selectionRuntime) Cancel(context.Context) error { return nil }
func (selectionRuntime) Close(context.Context) error  { return nil }
func (selectionRuntime) SetHandlers(RuntimeHandlers)  {}

func runResumeSelectionConformance(t *testing.T, _ VendorQuirks) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := NewSQLStore(db)
	adapter := &resumeSelectionAdapter{}
	reg := NewRegistry()
	reg.Register(adapter)
	svc := NewService(reg, store, nil)
	for _, prompted := range []bool{false, true} {
		id := "fresh"
		acpID := "old-fresh"
		if prompted {
			id, acpID = "prompted", "old-prompted"
		}
		sess := AgentSession{ID: id, AgentType: AgentTypeCodex, ProjectPath: t.TempDir(), Status: SessionStatusIdle, ACPSessionID: acpID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := store.InsertSession(ctx, sess); err != nil {
			t.Fatal(err)
		}
		if prompted {
			if err := store.InsertTurn(ctx, TurnRecord{ID: "turn-prompted", SessionID: id, CreatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := svc.resumeRuntime(ctx, sess); err != nil {
			t.Fatalf("resume prompted=%v: %v", prompted, err)
		}
		_ = svc.CloseSession(ctx, id)
	}
	if adapter.opened != 1 || adapter.loaded != 1 {
		t.Fatalf("Open=%d Load=%d, want one each", adapter.opened, adapter.loaded)
	}
}

func runProcessDeathConformance(t *testing.T, v VendorQuirks) {
	rt, err := v.Open(t, []string{"die"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err = rt.Prompt(context.Background(), TextPrompt("go"), nil)
	if !errors.Is(err, ErrProcessFailed) || errors.Is(err, ErrTurnFailed) {
		t.Fatalf("Prompt error = %v, want ErrProcessFailed only", err)
	}
	liveness, ok := rt.(RuntimeLiveness)
	if !ok {
		t.Fatal("runtime lacks RuntimeLiveness")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		dead, _ := liveness.Dead()
		if dead {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("runtime never reported Dead()")
}

func init() {
	conformanceCases = append(conformanceCases,
		ConformanceCase{
			Name: "open",
			Run: func(t *testing.T, v VendorQuirks) {
				rt, err := v.Open(t, nil)
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				if rt.ACPSessionID() == "" {
					t.Fatal("ACPSessionID is empty")
				}
				_ = rt.Close(context.Background())
			},
		},
		ConformanceCase{Name: "service_resume_selection", SkipIf: func(v VendorQuirks) (bool, string) {
			if v.Name != "codex" {
				return true, "service-level case runs once under codex"
			}
			return false, ""
		}, Run: func(t *testing.T, v VendorQuirks) { runResumeSelectionConformance(t, v) }, RequiredCap: CapLoadResume},
		ConformanceCase{Name: "opencode_process_death", SkipIf: func(v VendorQuirks) (bool, string) {
			if v.Name != "opencode" {
				return true, "opencode-only process-death follow-up"
			}
			return false, ""
		}, Run: func(t *testing.T, v VendorQuirks) { runProcessDeathConformance(t, v) }, RequiredCap: CapProcessDeath},
		ConformanceCase{
			Name: "load_resume",
			SkipIf: func(v VendorQuirks) (bool, string) {
				if !v.Supports.LoadResume {
					return true, "vendor has no SessionLoader"
				}
				return false, ""
			},
			Run: func(t *testing.T, v VendorQuirks) {
				// First open a session to get an ACP session ID
				rt, err := v.Open(t, nil)
				if err != nil {
					t.Fatalf("Open for load: %v", err)
				}
				id := rt.ACPSessionID()
				_ = rt.Close(context.Background())

				// Now load/resume it
				rt2, err := v.Load(t, id)
				if err != nil {
					t.Fatalf("Load: %v", err)
				}
				if rt2.ACPSessionID() != id {
					t.Fatalf("ACPSessionID = %q, want %q", rt2.ACPSessionID(), id)
				}
				_ = rt2.Close(context.Background())
			},
			RequiredCap: CapLoadResume,
		},
		ConformanceCase{
			Name: "load_resume_never_prompted",
			SkipIf: func(v VendorQuirks) (bool, string) {
				if !v.Supports.LoadResume {
					return true, "vendor has no SessionLoader"
				}
				return false, ""
			},
			Run: func(t *testing.T, v VendorQuirks) {
				// A session that was never prompted (no turns) must be opened with Open,
				// not Load. The service does this; here we verify the adapter doesn't
				// require a session ID for a never-prompted session.
				rt, err := v.Open(t, nil)
				if err != nil {
					t.Fatalf("Open (never prompted): %v", err)
				}
				if rt.ACPSessionID() == "" {
					t.Fatal("ACPSessionID is empty")
				}
				_ = rt.Close(context.Background())
			},
			RequiredCap: CapLoadResume,
		},
	)
}
