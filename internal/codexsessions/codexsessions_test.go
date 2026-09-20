package codexsessions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devkit/internal/codexhook"
	"devkit/internal/crypto"
	"devkit/internal/storage"
	"devkit/internal/vault"
)

func newService(t *testing.T) (*Service, *vault.Service) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(t.TempDir() + "/devkit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return NewService(db, storage.TableCodexHookEvents, v), v
}

func call(event codexhook.Event, sessionID string, extra func(*codexhook.Call)) codexhook.Call {
	c := codexhook.Call{Event: event, SessionID: sessionID, CWD: "/repo"}
	if extra != nil {
		extra(&c)
	}
	return c
}

// Prompt text and tool input/output are sealed because a prompt can as easily
// carry a secret as a URL query string can, and DevKit did not write either.
func TestSensitiveFieldsAreSealedAtRest(t *testing.T) {
	svc, v := newService(t)
	secret := "the user's actual prompt, including anything they pasted"
	err := svc.Record(call(codexhook.EventUserPromptSubmit, "s1", func(c *codexhook.Call) {
		c.Prompt = secret
	}))
	if err != nil {
		t.Fatal(err)
	}

	events, err := svc.Events("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Prompt != secret {
		t.Fatalf("events = %+v", events)
	}

	var raw string
	if err := svc.db.QueryRow(
		`SELECT detail_envelope FROM ` + storage.TableCodexHookEvents).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, secret) {
		t.Fatalf("the prompt was stored in plaintext: %s", raw)
	}

	// A locked vault gives the row back without the prompt: a locked vault,
	// not a broken row.
	v.Lock()
	events, err = svc.Events("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Prompt != "" {
		t.Fatalf("a locked vault handed back the prompt: %+v", events)
	}
	if events[0].Event != codexhook.EventUserPromptSubmit {
		t.Fatalf("a locked vault lost the row entirely: %+v", events)
	}
}

// A SessionStart carries nothing sensitive; sealing an empty struct would be
// wasted work and a row that always fails to open for no reason.
func TestAnEventWithNoSensitiveFieldsSealsNothing(t *testing.T) {
	svc, _ := newService(t)
	if err := svc.Record(call(codexhook.EventSessionStart, "s1", func(c *codexhook.Call) {
		c.Model, c.Source = "gpt-5-codex", "cli"
	})); err != nil {
		t.Fatal(err)
	}
	var envelope string
	if err := svc.db.QueryRow(
		`SELECT detail_envelope FROM ` + storage.TableCodexHookEvents).Scan(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope != "" {
		t.Fatalf("an event with nothing sensitive was sealed anyway: %q", envelope)
	}
}

// The summary is plaintext on purpose: it is what lets the list render
// without unlocking the vault.
func TestSummaryIsReadableWithoutOpeningTheVault(t *testing.T) {
	svc, v := newService(t)
	if err := svc.Record(call(codexhook.EventPreToolUse, "s1", func(c *codexhook.Call) {
		c.ToolName = "shell"
	})); err != nil {
		t.Fatal(err)
	}
	v.Lock()
	events, err := svc.Events("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Summary != "about to run shell" {
		t.Fatalf("events = %+v", events)
	}
}

func TestSessionsSummarizesEachSessionFromItsLatestEvent(t *testing.T) {
	svc, _ := newService(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Insert out of chronological order to prove the query sorts by time, not
	// by insertion order.
	must(recordAt(svc, call(codexhook.EventSessionStart, "s1", func(c *codexhook.Call) {
		c.Model, c.CWD = "gpt-5-codex", "/repo-a"
	}), base))
	must(recordAt(svc, call(codexhook.EventStop, "s2", func(c *codexhook.Call) {
		c.Model, c.CWD = "gpt-5.1-codex-max", "/repo-b"
	}), base.Add(30*time.Minute)))
	must(recordAt(svc, call(codexhook.EventPreToolUse, "s1", func(c *codexhook.Call) {
		c.Model, c.CWD, c.ToolName = "gpt-5-codex", "/repo-a", "shell"
	}), base.Add(time.Minute)))

	sessions, err := svc.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %+v", sessions)
	}
	// Newest activity first: s2's Stop at +30m outranks s1's PreToolUse at +1m.
	if sessions[0].ID != "s2" || sessions[0].LastEvent != codexhook.EventStop {
		t.Fatalf("sessions[0] = %+v", sessions[0])
	}
	if sessions[1].ID != "s1" || sessions[1].LastEvent != codexhook.EventPreToolUse {
		t.Fatalf("sessions[1] = %+v", sessions[1])
	}
	if sessions[1].EventCount != 2 {
		t.Fatalf("s1 event count = %d, want 2", sessions[1].EventCount)
	}
	if !sessions[1].StartedAt.Equal(base) {
		t.Fatalf("s1 started at = %v, want %v", sessions[1].StartedAt, base)
	}
}

func TestEventsReturnsOneSessionInChronologicalOrder(t *testing.T) {
	svc, _ := newService(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := recordAt(svc, call(codexhook.EventPostToolUse, "s1", nil), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := recordAt(svc, call(codexhook.EventPreToolUse, "s1", nil), base); err != nil {
		t.Fatal(err)
	}
	if err := recordAt(svc, call(codexhook.EventStop, "s2", nil), base); err != nil {
		t.Fatal(err)
	}

	events, err := svc.Events("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Event != codexhook.EventPreToolUse || events[1].Event != codexhook.EventPostToolUse {
		t.Fatalf("events = %+v", events)
	}
}

// The row that pushes one session past the limit is the only moment the
// limit can be exceeded, so the trim runs there.
func TestEventsAreCappedPerSessionAndOtherSessionsAreUntouched(t *testing.T) {
	svc, _ := newService(t)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < EventLimit+5; i++ {
		if err := recordAt(svc, call(codexhook.EventPostToolUse, "s1", nil), base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := recordAt(svc, call(codexhook.EventStop, "s2", nil), base); err != nil {
		t.Fatal(err)
	}

	events, err := svc.Events("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != EventLimit {
		t.Fatalf("kept %d events, want %d", len(events), EventLimit)
	}
	others, err := svc.Events("s2", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(others) != 1 {
		t.Fatalf("trimming s1 touched s2: %d", len(others))
	}
}

func TestClearSessionAndClearRemoveWhatTheyName(t *testing.T) {
	svc, _ := newService(t)
	if err := svc.Record(call(codexhook.EventStop, "s1", nil)); err != nil {
		t.Fatal(err)
	}
	if err := svc.Record(call(codexhook.EventStop, "s2", nil)); err != nil {
		t.Fatal(err)
	}

	if err := svc.ClearSession("s1"); err != nil {
		t.Fatal(err)
	}
	if events, err := svc.Events("s1", 0); err != nil || len(events) != 0 {
		t.Fatalf("ClearSession left events: %v %v", events, err)
	}
	if events, err := svc.Events("s2", 0); err != nil || len(events) != 1 {
		t.Fatalf("ClearSession touched another session: %v %v", events, err)
	}

	if err := svc.Clear(); err != nil {
		t.Fatal(err)
	}
	if sessions, err := svc.Sessions(); err != nil || len(sessions) != 0 {
		t.Fatalf("Clear left sessions: %v %v", sessions, err)
	}
}

// Nothing wired must not panic — installing Codex support without an open
// vault, or before main has called SetHistory, is the normal state on first
// run.
func TestNilSafeWithoutADatabase(t *testing.T) {
	var svc *Service
	if err := svc.Record(call(codexhook.EventStop, "s1", nil)); err != nil {
		t.Fatal(err)
	}
	if sessions, err := svc.Sessions(); err != nil || sessions != nil {
		t.Fatalf("Sessions() on a nil service = %v, %v", sessions, err)
	}
	if events, err := svc.Events("s1", 0); err != nil || events != nil {
		t.Fatalf("Events() on a nil service = %v, %v", events, err)
	}
	if err := svc.ClearSession("s1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Clear(); err != nil {
		t.Fatal(err)
	}
}

// recordAt is Record with an explicit timestamp, for tests that need to
// control ordering. It reaches past the public API on purpose: production
// code always uses time.Now, and only tests need to backdate.
func recordAt(s *Service, c codexhook.Call, at time.Time) error {
	id := eventID()
	detail := sensitive{Prompt: c.Prompt, ToolInput: c.ToolInput, ToolResponse: c.ToolResponse, LastMessage: c.LastMessage}
	envelope := ""
	if !detail.empty() && s.sealer != nil {
		plaintext, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		ad := crypto.AssociatedData{RecordType: DetailRecordType, RecordID: id, KeyID: DetailKeyID}
		sealed, err := s.sealer.SealV2(ad, plaintext)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(sealed)
		if err != nil {
			return err
		}
		envelope = string(raw)
	}
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO `+s.table+` (id, session_id, event, cwd, turn_id, model,
			permission_mode, tool_name, summary, detail_envelope, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, c.SessionID, string(c.Event), c.CWD, c.TurnID, c.Model,
		c.PermissionMode, c.ToolName, c.Summary(), envelope,
		at.UTC().Format(time.RFC3339Nano),
	)
	return err
}
