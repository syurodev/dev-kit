package interaction

import (
	"context"
	"errors"
	"sync"
	"time"

	"devkit/internal/vaultitem"
)

const DefaultTimeout = 60 * time.Second

type Type string

const (
	TypeSelect  Type = "select"
	TypeConfirm Type = "confirm"
)

type Kind string

const (
	KindSSH      Kind = "ssh"
	KindDatabase Kind = "database"
	KindKafka    Kind = "kafka"
)

// Prompter notifies the UI that a human decision is needed.
// Implementations emit a Wails event (or no-op in tests that Complete via API).
type Prompter interface {
	Prompt(pending Pending) error
}

// NoopPrompter is a Prompter that does nothing. Use it in tests that Complete
// via the API so Select/Confirm block until Complete or timeout.
type NoopPrompter struct{}

func (NoopPrompter) Prompt(Pending) error { return nil }

type SelectRequest struct {
	Kind Kind
}

type ConfirmRequest struct {
	Env     string
	Tool    string
	Summary string
	Kind    Kind
	// EnvBlocks is true when RequiresConfirmation(env) (incl. fail-closed).
	// When true, hub ignores allow_always cache and does not offer allow_always.
	EnvBlocks bool
}

type Pending struct {
	ID                 string `json:"id"`
	Type               Type   `json:"type"`
	Kind               Kind   `json:"kind,omitempty"`
	Env                string `json:"env,omitempty"`
	Tool               string `json:"tool,omitempty"`
	Summary            string `json:"summary,omitempty"`
	AllowAlwaysOffered bool   `json:"allowAlwaysOffered"`
}

type Hub struct {
	Timeout  time.Duration
	Headless bool
	prompter Prompter

	mu          sync.Mutex
	pending     *pendingWait
	allowAlways map[string]struct{} // key = Tool (v1); cleared on process restart only
}

type pendingWait struct {
	Pending
	result chan string
}

func NewHub(p Prompter) *Hub {
	return &Hub{
		Timeout:     DefaultTimeout,
		prompter:    p,
		allowAlways: map[string]struct{}{},
	}
}

func (h *Hub) Pending() *Pending {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == nil {
		return nil
	}
	cp := h.pending.Pending
	return &cp
}

func (h *Hub) Complete(id, value string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pending == nil {
		return ErrNoPendingInteraction
	}
	if h.pending.ID != id {
		return ErrInvalidInteractionID
	}
	select {
	case h.pending.result <- value:
	default:
	}
	return nil
}

func (h *Hub) SelectProfile(ctx context.Context, req SelectRequest) (string, error) {
	if h.Headless || h.prompter == nil {
		return "", ErrSelectionRequired
	}
	return h.wait(ctx, Pending{Type: TypeSelect, Kind: req.Kind}, func(v string) (string, error) {
		if v == "" {
			return "", ErrConfirmationDenied
		}
		return v, nil
	})
}

func (h *Hub) Confirm(ctx context.Context, req ConfirmRequest) error {
	if h.Headless || h.prompter == nil {
		return ErrConfirmationRequired
	}
	// Env block always wins: never short-circuit on allow_always.
	if !req.EnvBlocks {
		h.mu.Lock()
		_, ok := h.allowAlways[req.Tool]
		h.mu.Unlock()
		if ok {
			return nil
		}
	}
	_, err := h.wait(ctx, Pending{
		Type: TypeConfirm, Kind: req.Kind, Env: req.Env, Tool: req.Tool, Summary: req.Summary,
		AllowAlwaysOffered: !req.EnvBlocks,
	}, func(v string) (string, error) {
		switch v {
		case "allow":
			return v, nil
		case "allow_always":
			if req.EnvBlocks {
				// UI should not send this when EnvBlocks; treat as single allow.
				return "allow", nil
			}
			h.mu.Lock()
			h.allowAlways[req.Tool] = struct{}{}
			h.mu.Unlock()
			return v, nil
		default:
			return "", ErrConfirmationDenied
		}
	})
	return err
}

func (h *Hub) wait(ctx context.Context, p Pending, mapResult func(string) (string, error)) (string, error) {
	id, err := vaultitem.NewID()
	if err != nil {
		return "", err
	}
	p.ID = id
	resCh := make(chan string, 1)

	h.mu.Lock()
	if h.pending != nil {
		h.mu.Unlock()
		return "", errors.New("another interaction is already pending")
	}
	h.pending = &pendingWait{Pending: p, result: resCh}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		h.pending = nil
		h.mu.Unlock()
	}()

	if err := h.prompter.Prompt(p); err != nil {
		return "", err
	}

	timeout := h.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", ErrInteractionTimeout
	case v := <-resCh:
		return mapResult(v)
	}
}
