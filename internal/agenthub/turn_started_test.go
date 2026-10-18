package agenthub

import (
	"context"
	"testing"

	"devkit/internal/identity"
)

// The window learns of a turn an agent's message or the terminal started from
// this hook; it used to hear only when the turn ended.
func TestATurnAnnouncesItsStartBeforeItsEnd(t *testing.T) {
	f := newChoiceFixture(t)
	var events []string
	f.svc.OnTurnStarted = func(id string) { events = append(events, "started:"+id) }
	f.svc.OnTurnFinished = func(id string, _ SessionStatus, outcome string) { events = append(events, "finished:"+id+":"+outcome) }

	ui := identity.WithCaller(context.Background(), identity.UI())
	if _, err := f.svc.Invoke(ui, InvokeInput{SessionID: f.parent.ID, Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0] != "started:"+f.parent.ID || events[1] != "finished:"+f.parent.ID+":completed" {
		t.Fatalf("events = %v", events)
	}
}
