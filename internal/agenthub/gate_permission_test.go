package agenthub_test

import (
	"context"
	"devkit/internal/agenthub"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestGatePermissionUsesAgentCardAndValidatesProductionChoice(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan agenthub.PendingPermission, 2)
	w := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) { requests <- p })
	svc.ResolveGatePermission = w.ResolveGate
	h := interaction.NewHub(interaction.NoopPrompter{})
	h.ConfirmAgent = svc.ConfirmTool
	ctx := identity.WithCaller(context.Background(), identity.Agent(sess.ID))
	req := interaction.ConfirmRequest{Env: "custom-locked", EnvBlocks: true, Tool: "db.query", ProfileID: "profile-a", Summary: "SELECT 1", Kind: interaction.KindDatabase}
	for _, choice := range []string{"allow", "allow_always", "deny"} {
		done := make(chan error, 1)
		go func() { done <- h.Confirm(ctx, req) }()
		var p agenthub.PendingPermission
		select {
		case p = <-requests:
		case <-time.After(2 * time.Second):
			t.Fatal("gate card missing")
		}
		var params struct {
			Origin  string `json:"origin"`
			Locked  bool   `json:"locked"`
			Summary string `json:"summary"`
			Options []struct {
				OptionID string `json:"optionId"`
			} `json:"options"`
		}
		if err := json.Unmarshal(p.Params, &params); err != nil {
			t.Fatal(err)
		}
		if params.Origin != "devkit_gate" || !params.Locked || params.Summary != req.Summary || p.ExpiresAt.IsZero() || len(h.PendingList()) != 0 {
			t.Fatalf("bad gate card: %s", p.Params)
		}
		for _, opt := range params.Options {
			if opt.OptionID == "allow_always" {
				t.Fatal("locked gate offered persistent grant")
			}
		}
		if got := sessionStatus(t, svc, sess.ID); got != agenthub.SessionStatusAwaitingPermission {
			t.Fatalf("status = %s", got)
		}
		answer, _ := json.Marshal(map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": choice}})
		if err := w.Complete(p.ID, answer); err != nil {
			t.Fatal(err)
		}
		err := <-done
		if choice == "allow" && err != nil {
			t.Fatal(err)
		}
		if choice != "allow" && !errors.Is(err, interaction.ErrConfirmationDenied) {
			t.Fatalf("choice %s = %v", choice, err)
		}
		if got := sessionStatus(t, svc, sess.ID); got != agenthub.SessionStatusIdle {
			t.Fatalf("idle caller not restored: %s", got)
		}
	}
}

func TestGatePermissionRequiresAgentIdentityAndResolver(t *testing.T) {
	svc := newTestService(t)
	for _, ctx := range []context.Context{context.Background(), identity.WithCaller(context.Background(), identity.UserTerminal()), identity.WithCaller(context.Background(), identity.Agent("missing"))} {
		if _, err := svc.ConfirmTool(ctx, interaction.ConfirmRequest{}); !errors.Is(err, interaction.ErrConfirmationDenied) {
			t.Fatalf("unroutable caller = %v", err)
		}
	}
}

func TestGateExpiryClearsCardAndRestoresAnIdleConversation(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	requested := make(chan agenthub.PendingPermission, 1)
	w := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) { requested <- p })
	svc.ResolveGatePermission = w.ResolveGate
	h := interaction.NewHub(interaction.NoopPrompter{})
	h.ConfirmAgent = svc.ConfirmTool
	h.Timeout = 30 * time.Millisecond
	done := make(chan error, 1)
	go func() {
		done <- h.Confirm(identity.WithCaller(context.Background(), identity.Agent(sess.ID)), interaction.ConfirmRequest{Tool: "db.query", EnvBlocks: true})
	}()
	p := <-requested
	if err := <-done; err == nil {
		t.Fatal("expired gate accepted")
	}
	if len(w.PendingList()) != 0 {
		t.Fatal("expired request remains visible")
	}
	if err := w.Complete(p.ID, json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`)); err == nil {
		t.Fatal("late approval accepted")
	}
	if got := sessionStatus(t, svc, sess.ID); got != agenthub.SessionStatusIdle {
		t.Fatalf("expired gate left session %s", got)
	}
}

func TestClosingConversationDeniesPendingAndFutureToolGates(t *testing.T) {
	svc := newTestService(t)
	sess, err := svc.CreateSession(context.Background(), agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	requested := make(chan agenthub.PendingPermission, 1)
	w := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) { requested <- p })
	svc.ResolveGatePermission = w.ResolveGate
	svc.CancelPendingPermissions = w.CancelSession
	ctx := identity.WithCaller(context.Background(), identity.Agent(sess.ID))
	done := make(chan error, 1)
	go func() {
		_, err := svc.ConfirmTool(ctx, interaction.ConfirmRequest{Tool: "db.query", EnvBlocks: true})
		done <- err
	}()
	<-requested
	if err := svc.CloseSession(context.Background(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("closed session's gate accepted")
	}
	if _, err := svc.ConfirmTool(ctx, interaction.ConfirmRequest{}); !errors.Is(err, interaction.ErrConfirmationDenied) {
		t.Fatalf("closed session asked again: %v", err)
	}
	if got := sessionStatus(t, svc, sess.ID); got != agenthub.SessionStatusClosed {
		t.Fatalf("closed session was reopened: %s", got)
	}
}
