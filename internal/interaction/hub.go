package interaction

import (
	"context"
	"sort"
	"sync"
	"time"

	"devkit/internal/identity"
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
	// KindGit is a commit on a guarded branch or a push to a tagged remote.
	KindGit Kind = "git"
	// KindAPI is an HTTP request going out on a gated environment.
	KindAPI Kind = "api"
	// KindTask is an agent closing a task on a board with no verify command.
	KindTask Kind = "task"
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
	// ProfileID scopes allow_always: a grant on one profile never covers another.
	ProfileID string
	// EnvBlocks is true when RequiresConfirmation(env) (incl. fail-closed).
	// When true, hub ignores allow_always cache and does not offer allow_always.
	EnvBlocks bool
}

type Pending struct {
	ID                 string    `json:"id"`
	Type               Type      `json:"type"`
	Kind               Kind      `json:"kind,omitempty"`
	Env                string    `json:"env,omitempty"`
	Tool               string    `json:"tool,omitempty"`
	Summary            string    `json:"summary,omitempty"`
	AllowAlwaysOffered bool      `json:"allowAlwaysOffered"`
	Caller             string    `json:"caller,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
}

// allowKey scopes an allow_always grant: it never crosses callers, tools or profiles.
type allowKey struct{ caller, tool, profile string }

type Hub struct {
	Timeout  time.Duration
	Headless bool
	prompter Prompter

	mu          sync.Mutex
	pending     map[string]*pendingWait // by Pending.ID; at most one per caller
	allowAlways map[allowKey]struct{}   // cleared per caller by ForgetCaller, fully on restart
}

type pendingWait struct {
	Pending
	result   chan string
	finished bool
}

func NewHub(p Prompter) *Hub {
	return &Hub{
		Timeout:     DefaultTimeout,
		prompter:    p,
		pending:     map[string]*pendingWait{},
		allowAlways: map[allowKey]struct{}{},
	}
}

// PendingList returns every pending interaction, oldest first.
func (h *Hub) PendingList() []Pending {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Pending, 0, len(h.pending))
	for _, w := range h.pending {
		out = append(out, w.Pending)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Pending returns the oldest pending interaction, or nil. Kept for callers
// that only ever showed one; new code should use PendingList.
func (h *Hub) Pending() *Pending {
	list := h.PendingList()
	if len(list) == 0 {
		return nil
	}
	return &list[0]
}

// PendingByID returns one pending interaction, or nil.
func (h *Hub) PendingByID(id string) *Pending {
	h.mu.Lock()
	defer h.mu.Unlock()
	w, ok := h.pending[id]
	if !ok {
		return nil
	}
	cp := w.Pending
	return &cp
}

func (h *Hub) Complete(id, value string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.pending) == 0 {
		return ErrNoPendingInteraction
	}
	w, ok := h.pending[id]
	if !ok || w.finished {
		return ErrInvalidInteractionID
	}
	select {
	case w.result <- value:
		w.finished = true
		return nil
	default:
		return ErrNoPendingInteraction
	}
}

// ForgetCaller drops every allow_always grant held by caller (a closed agent session).
func (h *Hub) ForgetCaller(caller string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for key := range h.allowAlways {
		if key.caller == caller {
			delete(h.allowAlways, key)
		}
	}
}

func (h *Hub) invalidatePending(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w, ok := h.pending[id]; ok {
		w.finished = true
		delete(h.pending, id)
	}
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
	key := allowKey{caller: identity.Label(ctx), tool: req.Tool, profile: req.ProfileID}
	// Env block always wins: never short-circuit on allow_always.
	if !req.EnvBlocks {
		h.mu.Lock()
		_, ok := h.allowAlways[key]
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
			h.allowAlways[key] = struct{}{}
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
	p.Caller = identity.Label(ctx)
	p.CreatedAt = time.Now().UTC()
	resCh := make(chan string, 1)

	h.mu.Lock()
	for _, w := range h.pending {
		if w.Caller == p.Caller {
			h.mu.Unlock()
			return "", ErrCallerBusy
		}
	}
	h.pending[id] = &pendingWait{Pending: p, result: resCh}
	h.mu.Unlock()

	defer h.invalidatePending(id)

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
