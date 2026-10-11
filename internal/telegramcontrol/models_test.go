package telegramcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

func modelHarness(t *testing.T) (*Service, *Client, *fakeAgents, *[]string) {
	s, c, agents, messages := harness(t)
	s.selected = "session"
	agents.models = []agenthub.ConfigOption{{ID: "vendor_model", Category: "model", Type: "select", CurrentValue: json.RawMessage(`"a"`), Options: []agenthub.ConfigSelectOption{{Value: "a", Name: "Model A"}, {Value: "b", Name: "Model B"}}}}
	return s, c, agents, messages
}
func command(text string) Update {
	m := &Message{Text: text, Date: time.Now().Unix(), From: User{ID: 7}}
	m.Chat.ID = 7
	m.Chat.Type = "private"
	return Update{Message: m}
}
func TestModelMenuAndAuthenticatedSelection(t *testing.T) {
	s, c, agents, messages := modelHarness(t)
	ctx := context.Background()
	// Model metadata remains available without opting into full transcript export.
	s.state.Config.ReadTranscript = false
	s.handle(ctx, c, command("/models"))
	if len(*messages) == 0 || !strings.Contains((*messages)[0], "Hiện tại: a") {
		t.Fatal("current model missing")
	}
	var token string
	var a action
	for key, item := range s.actions {
		if item.source == "model" && item.value == "b" {
			token = key
			a = item
		}
	}
	if token == "" {
		t.Fatal("no model selector")
	}
	s.handle(ctx, c, callback(token, a, 8, 7))
	if agents.modelChanges != 0 {
		t.Fatal("wrong user changed model")
	}
	s.handle(ctx, c, callback(token, a, 7, 7))
	if agents.modelChanges != 1 || agents.changedOption != "vendor_model" || agents.changedModel != "b" {
		t.Fatalf("model changes=%d option=%s value=%s", agents.modelChanges, agents.changedOption, agents.changedModel)
	}
	s.handle(ctx, c, callback(token, a, 7, 7))
	if agents.modelChanges != 1 {
		t.Fatal("old button executed twice")
	}
}
func TestModelCommandRejectsUnadvertisedValue(t *testing.T) {
	s, c, agents, _ := modelHarness(t)
	ctx := context.Background()
	s.handle(ctx, c, command("/model invented"))
	if agents.modelChanges != 0 {
		t.Fatal("unadvertised model accepted")
	}
	s.handle(ctx, c, command("/model b"))
	if agents.modelChanges != 1 || agents.changedModel != "b" {
		t.Fatal("model command did not reach shared configurator")
	}
}
func TestModelSelectionRejectsBusyWrongScopeAndStaleMenu(t *testing.T) {
	s, c, agents, _ := modelHarness(t)
	ctx := context.Background()
	s.showModels(ctx, c, "")
	var a action
	for _, item := range s.actions {
		if item.value == "b" {
			a = item
		}
	}
	agents.status = agenthub.SessionStatusRunning
	if err := s.complete(ctx, c, a); err == nil {
		t.Fatal("running session changed model")
	}
	agents.status = agenthub.SessionStatusIdle
	s.ownTurns["session"] = true
	if err := s.complete(ctx, c, a); err == nil {
		t.Fatal("scheduled turn changed model")
	}
	delete(s.ownTurns, "session")
	agents.workspaceID = "outside"
	if err := s.complete(ctx, c, a); err == nil {
		t.Fatal("scope removal did not revoke menu")
	}
	agents.workspaceID = "ws"
	s.selected = "another"
	if err := s.complete(ctx, c, a); err == nil {
		t.Fatal("old selected session accepted")
	}
	s.selected = "session"
	agents.models[0].CurrentValue = json.RawMessage(`"b"`)
	if err := s.complete(ctx, c, a); err == nil {
		t.Fatal("stale configuration accepted")
	}
	if agents.modelChanges != 0 {
		t.Fatal("rejected request reached vendor")
	}
}
func TestModelVendorFailureDoesNotClaimSuccess(t *testing.T) {
	s, c, agents, messages := modelHarness(t)
	agents.changeErr = errors.New("private vendor error")
	s.modelCommand(context.Background(), c, "b")
	if agents.modelChanges != 0 || len(*messages) == 0 || !strings.Contains((*messages)[0], "Không chuyển được") || strings.Contains((*messages)[0], "private") {
		t.Fatal("vendor failure mishandled")
	}
}
func TestModelMenuPagination(t *testing.T) {
	s, c, agents, messages := modelHarness(t)
	for i := 0; i < 12; i++ {
		agents.models[0].Options = append(agents.models[0].Options, agenthub.ConfigSelectOption{Value: strings.Repeat("x", i+1)})
	}
	s.showModels(context.Background(), c, "")
	if !strings.Contains((*messages)[0], "/models 10") || len(s.actions) != 10 {
		t.Fatal("first page is not bounded")
	}
	s.showModels(context.Background(), c, "10")
	if len(s.actions) != 4 {
		t.Fatalf("second page actions=%d", len(s.actions))
	}
}
