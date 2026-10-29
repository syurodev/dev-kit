package agenthub

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/storage"
)

func newSubagentStateRig(t *testing.T) (*Service, *SQLStore, string, *[]json.RawMessage) {
	t.Helper()
	ctx := context.Background()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := NewSQLStore(db)
	reg := NewRegistry()
	reg.Register(NewMockAdapter())
	emitted := &[]json.RawMessage{}
	svc := NewService(reg, store, func(_ string, update json.RawMessage) { *emitted = append(*emitted, update) })
	sess, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, sess.ID, emitted
}

func storedStateRow(t *testing.T, store *SQLStore, id string, kind UpdateKind) (SessionUpdate, bool) {
	t.Helper()
	row, found, err := store.FindTranscriptKind(context.Background(), id, kind)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		return SessionUpdate{}, false
	}
	var stored SessionUpdate
	if err := json.Unmarshal(row.Payload, &stored); err != nil {
		t.Fatal(err)
	}
	return stored, true
}

// The wire marks a subagent's update with _devkit.origin (markOrigin), not a
// top-level field. A subagent announces its own settings and mode on the
// parent's connection; the session's stored row is the session's, so only an
// update without an origin may replace it.
func TestSubagentSessionStateNeverReplacesTheSessionsRow(t *testing.T) {
	const sub = `,"_devkit":{"origin":"sub1"}`
	cases := []struct {
		name string
		kind UpdateKind
		own  func(v string) string // the session's own update for a value
		// value reads back the one value the update carried.
		value func(SessionUpdate) string
	}{
		{
			name: "config_option_update",
			kind: UpdateConfigOptions,
			own: func(v string) string {
				return `{"sessionUpdate":"config_option_update","configOptions":[{"id":"model","type":"select","currentValue":"` + v + `","options":[]}]`
			},
			value: func(u SessionUpdate) string {
				if len(u.ConfigOptions) == 0 {
					return ""
				}
				return strings.Trim(string(u.ConfigOptions[0].CurrentValue), `"`)
			},
		},
		{
			name: "current_mode_update",
			kind: UpdateCurrentMode,
			own: func(v string) string {
				return `{"sessionUpdate":"current_mode_update","currentModeId":"` + v + `"`
			},
			value: func(u SessionUpdate) string { return u.Mode },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, store, id, emitted := newSubagentStateRig(t)

			// A subagent speaking first leaves no session row at all.
			svc.HandleUpdateForTest(ctx, id, json.RawMessage(tc.own("sonnet")+sub+`}`))
			if _, found := storedStateRow(t, store, id, tc.kind); found {
				t.Fatal("a subagent's update created the session's row")
			}

			svc.HandleUpdateForTest(ctx, id, json.RawMessage(tc.own("opus")+`}`))
			if got, _ := storedStateRow(t, store, id, tc.kind); tc.value(got) != "opus" {
				t.Fatalf("own update stored %q, want opus", tc.value(got))
			}

			before := len(*emitted)
			svc.HandleUpdateForTest(ctx, id, json.RawMessage(tc.own("sonnet")+sub+`}`))
			if got, _ := storedStateRow(t, store, id, tc.kind); tc.value(got) != "opus" {
				t.Fatalf("after a subagent's update the row holds %q, want opus kept", tc.value(got))
			}
			// The window still hears it (it carries its origin and the reducer
			// ignores it); only the stored state is the session's alone.
			if len(*emitted) != before+1 {
				t.Fatalf("emitted %d events for the subagent's update, want 1", len(*emitted)-before)
			}

			svc.HandleUpdateForTest(ctx, id, json.RawMessage(tc.own("haiku")+`}`))
			if got, _ := storedStateRow(t, store, id, tc.kind); tc.value(got) != "haiku" {
				t.Fatalf("own update after a subagent's stored %q, want haiku", tc.value(got))
			}
		})
	}
}

// A session whose row already holds a subagent's options (stored before the
// guard) heals when the main agent announces its own: PrepareSession's
// RefreshConfigOptions and SetHandlers' first announcement both go through the
// attached handler to handleUpdate with no origin, which replaces the row.
func TestMainAgentAnnouncementReplacesAStoredSubagentConfigRow(t *testing.T) {
	ctx := context.Background()
	svc, store, id, _ := newSubagentStateRig(t)

	damaged := SessionUpdate{
		Kind:   UpdateConfigOptions,
		Origin: "a1851634a85452625",
		ConfigOptions: []ConfigOption{{
			ID: "model", CurrentValue: json.RawMessage(`"sonnet"`),
			Options: []ConfigSelectOption{{Value: "sonnet", Name: "Sonnet 5.5"}},
		}},
	}
	payload, err := json.Marshal(damaged)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTranscript(ctx, TranscriptEntry{
		ID: "damaged", SessionID: id, Seq: 1, Kind: UpdateConfigOptions, Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}

	rt := &acpRuntime{options: parseConfigOptions(json.RawMessage(`[{"id":"model","type":"select","currentValue":"opus","options":[]}]`))}
	svc.attachUpdates(id, rt)
	if err := rt.RefreshConfigOptions(ctx); err != nil {
		t.Fatal(err)
	}
	got, found := storedStateRow(t, store, id, UpdateConfigOptions)
	if !found || got.Origin != "" || len(got.ConfigOptions) != 1 || strings.Trim(string(got.ConfigOptions[0].CurrentValue), `"`) != "opus" {
		t.Fatalf("stored = %+v, want the main agent's opus options", got)
	}
}
