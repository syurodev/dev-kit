package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"devkit/internal/interaction"
)

func waitForPending(t *testing.T, h *interaction.Hub) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.Pending() != nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for pending interaction")
}

type fakeEnvConfirmer map[string]bool

func (f fakeEnvConfirmer) RequiresConfirmation(name string) bool {
	if v, ok := f[name]; ok {
		return v
	}
	return true // fail closed
}

var errProfileNotFound = errors.New("profile not found")

type fakeProfileEnvReader map[string]string

func (f fakeProfileEnvReader) EnvForProfile(id string) (string, error) {
	env, ok := f[id]
	if !ok {
		return "", errProfileNotFound
	}
	return env, nil
}

type fakeActiveTarget struct {
	id     string
	ok     bool
	ensure func(ctx context.Context) (string, error)
}

func (f *fakeActiveTarget) ActiveID() (string, bool) {
	return f.id, f.ok
}

func (f *fakeActiveTarget) EnsureActive(ctx context.Context) (string, error) {
	if f.ensure != nil {
		return f.ensure(ctx)
	}
	return f.id, nil
}

func TestGatePromptsDevThenAllowAlways(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	ctx := context.Background()

	active := &fakeActiveTarget{id: "profile-1"}
	env := fakeEnvConfirmer{"dev": false}
	profiles := fakeProfileEnvReader{"profile-1": "dev"}

	go func() {
		waitForPending(t, h)
		p := h.Pending()
		if p == nil || p.Type != interaction.TypeConfirm {
			t.Errorf("pending = %+v", p)
		}
		if !p.AllowAlwaysOffered {
			t.Errorf("dev env should offer allow_always")
		}
		if err := h.Complete(p.ID, "allow_always"); err != nil {
			t.Errorf("complete: %v", err)
		}
	}()

	if _, err := GateActiveMCP(ctx, h, env, profiles, active, "db.query", "select 1", interaction.KindDatabase); err != nil {
		t.Fatalf("first gate: %v", err)
	}

	if _, err := GateActiveMCP(ctx, h, env, profiles, active, "db.query", "select 2", interaction.KindDatabase); err != nil {
		t.Fatalf("second gate: %v", err)
	}
	if h.Pending() != nil {
		t.Fatal("second gate should not leave pending after allow_always")
	}
}

func TestGateConfirmsWhenProdEveryTime(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	ctx := context.Background()

	active := &fakeActiveTarget{id: "profile-prod"}
	env := fakeEnvConfirmer{"prod": true}
	profiles := fakeProfileEnvReader{"profile-prod": "prod"}

	go func() {
		waitForPending(t, h)
		p := h.Pending()
		if p.AllowAlwaysOffered {
			t.Errorf("prod must not offer allow_always")
		}
		if err := h.Complete(p.ID, "deny"); err != nil {
			t.Errorf("complete deny: %v", err)
		}
	}()

	_, err := GateActiveMCP(ctx, h, env, profiles, active, "ssh.runCommand", "rm -rf", interaction.KindSSH)
	if !errors.Is(err, interaction.ErrConfirmationDenied) {
		t.Fatalf("deny err = %v, want ErrConfirmationDenied", err)
	}

	prompted := make(chan struct{}, 1)
	go func() {
		waitForPending(t, h)
		p := h.Pending()
		if p.AllowAlwaysOffered {
			t.Errorf("prod must not offer allow_always")
		}
		prompted <- struct{}{}
		if err := h.Complete(p.ID, "allow"); err != nil {
			t.Errorf("complete allow: %v", err)
		}
	}()

	if _, err := GateActiveMCP(ctx, h, env, profiles, active, "ssh.runCommand", "ls", interaction.KindSSH); err != nil {
		t.Fatalf("first allow gate: %v", err)
	}
	select {
	case <-prompted:
	default:
		t.Fatal("prod must prompt on first allow call")
	}

	prompted2 := make(chan struct{}, 1)
	go func() {
		waitForPending(t, h)
		prompted2 <- struct{}{}
		if err := h.Complete(h.Pending().ID, "allow"); err != nil {
			t.Errorf("complete allow: %v", err)
		}
	}()

	if _, err := GateActiveMCP(ctx, h, env, profiles, active, "ssh.runCommand", "ls again", interaction.KindSSH); err != nil {
		t.Fatalf("second gate: %v", err)
	}
	select {
	case <-prompted2:
	default:
		t.Fatal("prod must prompt again after allow (no allow_always cache)")
	}
}

func TestGateFailClosedUnknownEnv(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	ctx := context.Background()

	active := &fakeActiveTarget{id: "profile-x"}
	env := fakeEnvConfirmer{} // unknown → fail closed (true)
	profiles := fakeProfileEnvReader{"profile-x": "no-such-tag"}

	prompted := make(chan struct{}, 1)
	go func() {
		waitForPending(t, h)
		p := h.Pending()
		if p.AllowAlwaysOffered {
			t.Errorf("unknown env must not offer allow_always")
		}
		prompted <- struct{}{}
		if err := h.Complete(p.ID, "allow"); err != nil {
			t.Errorf("complete: %v", err)
		}
	}()

	if _, err := GateActiveMCP(ctx, h, env, profiles, active, "kafka.publishMessage", "payload", interaction.KindKafka); err != nil {
		t.Fatalf("gate: %v", err)
	}
	select {
	case <-prompted:
	default:
		t.Fatal("unknown env must require confirmation")
	}
}

func TestGateEnsureActiveErrorPropagates(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	ctx := context.Background()

	wantErr := errors.New("selection required")
	active := &fakeActiveTarget{
		ensure: func(context.Context) (string, error) {
			return "", wantErr
		},
	}
	env := fakeEnvConfirmer{"dev": false}
	profiles := fakeProfileEnvReader{"profile-1": "dev"}

	_, err := GateActiveMCP(ctx, h, env, profiles, active, "db.query", "select 1", interaction.KindDatabase)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if h.Pending() != nil {
		t.Fatal("EnsureActive error should not leave pending confirmation")
	}
}

func TestGateEnvForProfileErrorPropagates(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	ctx := context.Background()

	active := &fakeActiveTarget{id: "missing"}
	env := fakeEnvConfirmer{"dev": false}
	profiles := fakeProfileEnvReader{}

	_, err := GateActiveMCP(ctx, h, env, profiles, active, "db.query", "select 1", interaction.KindDatabase)
	if !errors.Is(err, errProfileNotFound) {
		t.Fatalf("err = %v, want %v", err, errProfileNotFound)
	}
	if h.Pending() != nil {
		t.Fatal("EnvForProfile error should not leave pending confirmation")
	}
}

// TestGateReturnsConfirmedProfileID pins the gate's contract: the returned id
// is the one the env confirmation covered, so callers never re-read the sticky
// active target (a concurrent SetActiveProfile swap between confirmation and
// execution must not run against an unconfirmed profile).
func TestGateReturnsConfirmedProfileID(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	ctx := context.Background()

	// EnsureActive resolves "profile-A" (the one confirmed), while a stale
	// re-read of the sticky state would see "profile-B".
	active := &fakeActiveTarget{ensure: func(context.Context) (string, error) {
		return "profile-A", nil
	}}
	env := fakeEnvConfirmer{"dev": false}
	profiles := fakeProfileEnvReader{"profile-A": "dev", "profile-B": "prod"}

	go func() {
		waitForPending(t, h)
		if err := h.Complete(h.Pending().ID, "allow"); err != nil {
			t.Errorf("complete allow: %v", err)
		}
	}()
	id, err := GateActiveMCP(ctx, h, env, profiles, active, "ssh.runCommand", "ls", interaction.KindSSH)
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if id != "profile-A" {
		t.Fatalf("confirmed id = %q, want profile-A", id)
	}
}

func TestGateProfileSkipsPromptWhenEnvDoesNotBlock(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	env := fakeEnvConfirmer{"dev": false}
	profiles := fakeProfileEnvReader{"p-dev": "dev"}

	if err := GateProfileMCP(context.Background(), h, env, profiles, "p-dev", "db.explain", "select 1", interaction.KindDatabase); err != nil {
		t.Fatalf("dev gate: %v", err)
	}
	if h.Pending() != nil {
		t.Fatal("a non-blocking env must not prompt for a read-only tool")
	}
}

func TestGateProfileConfirmsProdEveryCall(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	env := fakeEnvConfirmer{"prod": true}
	profiles := fakeProfileEnvReader{"p-prod": "prod"}

	for i, answer := range []string{"allow", "allow"} {
		prompted := make(chan struct{}, 1)
		go func() {
			waitForPending(t, h)
			p := h.Pending()
			if p.AllowAlwaysOffered {
				t.Errorf("prod must not offer allow_always")
			}
			if p.Env != "prod" || p.Tool != "db.explain" {
				t.Errorf("pending env/tool = %q/%q, want prod/db.explain", p.Env, p.Tool)
			}
			prompted <- struct{}{}
			_ = h.Complete(p.ID, answer)
		}()
		if err := GateProfileMCP(context.Background(), h, env, profiles, "p-prod", "db.explain", "select 1", interaction.KindDatabase); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		select {
		case <-prompted:
		default:
			t.Fatalf("call %d: prod must prompt every call", i)
		}
	}
}

func TestGateProfileProdDenied(t *testing.T) {
	h := interaction.NewHub(interaction.NoopPrompter{})
	go func() {
		waitForPending(t, h)
		_ = h.Complete(h.Pending().ID, "deny")
	}()
	err := GateProfileMCP(context.Background(), h, fakeEnvConfirmer{"prod": true}, fakeProfileEnvReader{"p": "prod"}, "p", "ssh.testConnection", "test connection", interaction.KindSSH)
	if !errors.Is(err, interaction.ErrConfirmationDenied) {
		t.Fatalf("err = %v, want ErrConfirmationDenied", err)
	}
}

func TestGateProfileHeadlessProdFailsClosed(t *testing.T) {
	err := GateProfileMCP(context.Background(), nil, fakeEnvConfirmer{"prod": true}, fakeProfileEnvReader{"p": "prod"}, "p", "db.testConnection", "test connection", interaction.KindDatabase)
	if !errors.Is(err, interaction.ErrConfirmationRequired) {
		t.Fatalf("err = %v, want ErrConfirmationRequired", err)
	}
}

func TestGateProfileUnknownProfile(t *testing.T) {
	err := GateProfileMCP(context.Background(), nil, fakeEnvConfirmer{}, fakeProfileEnvReader{}, "missing", "db.explain", "", interaction.KindDatabase)
	if !errors.Is(err, errProfileNotFound) {
		t.Fatalf("err = %v, want errProfileNotFound", err)
	}
}

func TestGateProfileUnresolvableEnvFailsClosed(t *testing.T) {
	// fakeEnvConfirmer answers true for any name it does not know, as the real
	// environments.Service does for a deleted or unreadable tag.
	err := GateProfileMCP(context.Background(), nil, fakeEnvConfirmer{}, fakeProfileEnvReader{"p": "ghost-tag"}, "p", "db.explain", "select 1", interaction.KindDatabase)
	if !errors.Is(err, interaction.ErrConfirmationRequired) {
		t.Fatalf("err = %v, want ErrConfirmationRequired", err)
	}
}
