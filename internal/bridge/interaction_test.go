package bridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"devkit/internal/app"
	"devkit/internal/apperrors"
	"devkit/internal/interaction"
	"devkit/internal/permissions"
)

func TestInteractionPendingNilWithoutHub(t *testing.T) {
	service := newTestService(t)
	pending, err := service.InteractionPending()
	if err == nil {
		t.Fatal("expected error when hub is nil")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != codeInternalError {
		t.Fatalf("err = %+v", err)
	}
	if pending != nil {
		t.Fatalf("pending = %+v, want nil", pending)
	}
}

func TestInteractionBridgeSelectComplete(t *testing.T) {
	container := app.NewContainer()
	hub := interaction.NewHub(interaction.NoopPrompter{})
	container.Interaction = hub
	service := NewAppService(container)

	pending, err := service.InteractionPending()
	if err != nil {
		t.Fatalf("InteractionPending: %v", err)
	}
	if pending != nil {
		t.Fatalf("pending = %+v, want nil", pending)
	}

	done := make(chan string, 1)
	go func() {
		id, err := hub.SelectProfile(context.Background(), interaction.SelectRequest{Kind: interaction.KindSSH})
		if err != nil {
			t.Errorf("SelectProfile: %v", err)
			done <- ""
			return
		}
		done <- id
	}()

	var seen *interaction.Pending
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		seen, err = service.InteractionPending()
		if err != nil {
			t.Fatalf("InteractionPending: %v", err)
		}
		if seen != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if seen == nil || seen.Type != interaction.TypeSelect || seen.Kind != interaction.KindSSH {
		t.Fatalf("pending = %+v", seen)
	}

	if err := service.InteractionComplete(seen.ID, "profile-abc"); err != nil {
		t.Fatalf("InteractionComplete: %v", err)
	}

	select {
	case got := <-done:
		if got != "profile-abc" {
			t.Fatalf("selected = %q, want profile-abc", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SelectProfile did not unblock")
	}
}

func TestInteractionDenySelectUsesEmptyValue(t *testing.T) {
	container := app.NewContainer()
	hub := interaction.NewHub(interaction.NoopPrompter{})
	container.Interaction = hub
	service := NewAppService(container)

	errCh := make(chan error, 1)
	go func() {
		_, err := hub.SelectProfile(context.Background(), interaction.SelectRequest{Kind: interaction.KindDatabase})
		errCh <- err
	}()

	var seen *interaction.Pending
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		seen, err = service.InteractionPending()
		if err != nil {
			t.Fatalf("InteractionPending: %v", err)
		}
		if seen != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if seen == nil {
		t.Fatal("expected pending select")
	}
	if err := service.InteractionDeny(seen.ID); err != nil {
		t.Fatalf("InteractionDeny: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, interaction.ErrConfirmationDenied) {
			t.Fatalf("err = %v, want ErrConfirmationDenied", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SelectProfile did not unblock")
	}
}

func TestInteractionDenyConfirmUsesDeny(t *testing.T) {
	container := app.NewContainer()
	hub := interaction.NewHub(interaction.NoopPrompter{})
	container.Interaction = hub
	service := NewAppService(container)

	errCh := make(chan error, 1)
	go func() {
		errCh <- hub.Confirm(context.Background(), interaction.ConfirmRequest{
			Env: "prod", Tool: "ssh.runCommand", Summary: "rm -rf /", Kind: interaction.KindSSH, EnvBlocks: true,
		})
	}()

	var seen *interaction.Pending
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		seen, err = service.InteractionPending()
		if err != nil {
			t.Fatalf("InteractionPending: %v", err)
		}
		if seen != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if seen == nil || seen.AllowAlwaysOffered {
		t.Fatalf("pending = %+v", seen)
	}
	if err := service.InteractionDeny(seen.ID); err != nil {
		t.Fatalf("InteractionDeny: %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, interaction.ErrConfirmationDenied) {
			t.Fatalf("err = %v, want ErrConfirmationDenied", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Confirm did not unblock")
	}
}

func TestNewContainerGrantsInteractionScopes(t *testing.T) {
	container := app.NewContainer()
	for _, scope := range []string{"pending", "pending:complete"} {
		decision := container.Permissions.Check(permissions.Request{
			SubjectID:  "builtin.interaction",
			Capability: "interaction:respond",
			Scope:      scope,
		})
		if !decision.Allowed {
			t.Fatalf("scope %q denied", scope)
		}
	}
}
