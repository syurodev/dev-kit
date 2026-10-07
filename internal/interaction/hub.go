package interaction

import (
	"context"
	"sort"
	"strings"
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
	// KindGitAccount is a git remote that more than one account could
	// authenticate: the person says which, because choosing the wrong identity
	// is worse than asking once.
	KindGitAccount Kind = "git_account"
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

// AccountOption is one account offered in a selection. A label and a subject
// (the login the provider reported) — never a secret, never an id the person
// has to read.
type AccountOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
	Subject  string `json:"subject,omitempty"`
}

// SelectAccountRequest asks which account authenticates a git remote.
type SelectAccountRequest struct {
	// Summary is the question, in the person's terms ("Push acme-corp/api to
	// github.com").
	Summary string
	// Remember names what an answer would be remembered for ("acme-corp on
	// github.com"). Empty means the answer cannot be remembered and the UI
	// must not offer to.
	Remember string
	Options  []AccountOption
}

// AccountChoice is the answer to a SelectAccount.
type AccountChoice struct {
	ID string
	// Remember is true when the person asked for the answer to be kept.
	Remember bool
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
	// Options and Remember are set on a KindGitAccount selection only: the
	// accounts to choose between, and what a kept answer would be kept for.
	Options  []AccountOption `json:"options,omitempty"`
	Remember string          `json:"remember,omitempty"`
}

// allowKey scopes an allow_always grant: it never crosses callers, tools or profiles.
type allowKey struct{ caller, tool, profile string }

type Hub struct {
	Timeout  time.Duration
	Headless bool
	// ConfirmAgent routes an identity-bound Agent Hub caller to the shared
	// agent permission broker. Other callers keep the popup transport.
	ConfirmAgent func(context.Context, ConfirmRequest) (string, error)
	prompter     Prompter

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

// AccountAnswer is the value a UI completes a KindGitAccount selection with:
// "once:<account id>" or "remember:<account id>".
func AccountAnswer(id string, remember bool) string {
	if remember {
		return "remember:" + id
	}
	return "once:" + id
}

// SelectAccount asks the person which account to use. Only an option that was
// offered is accepted, so the answer cannot name an account the question never
// listed — the same rule Accounts keeps for a remembered choice.
//
// Headless is ErrSelectionRequired, like every other selection: a prompt
// nobody can answer must not turn into a guess.
func (h *Hub) SelectAccount(ctx context.Context, req SelectAccountRequest) (AccountChoice, error) {
	if h.Headless || h.prompter == nil {
		return AccountChoice{}, ErrSelectionRequired
	}
	if len(req.Options) == 0 {
		return AccountChoice{}, ErrSelectionRequired
	}
	options := append([]AccountOption(nil), req.Options...)
	var choice AccountChoice
	_, err := h.wait(ctx, Pending{
		Type: TypeSelect, Kind: KindGitAccount, Summary: req.Summary,
		Options: options, Remember: req.Remember,
	}, func(v string) (string, error) {
		mode, id, ok := strings.Cut(v, ":")
		if !ok || id == "" {
			return "", ErrConfirmationDenied
		}
		if mode != "once" && mode != "remember" {
			return "", ErrConfirmationDenied
		}
		// An answer to keep is honoured only when the question offered to.
		remember := mode == "remember" && req.Remember != ""
		for _, option := range options {
			if option.ID == id {
				choice = AccountChoice{ID: id, Remember: remember}
				return id, nil
			}
		}
		return "", ErrConfirmationDenied
	})
	if err != nil {
		return AccountChoice{}, err
	}
	return choice, nil
}

func (h *Hub) Confirm(ctx context.Context, req ConfirmRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	mapResult := func(v string) (string, error) {
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
	}
	if caller, ok := identity.FromContext(ctx); ok && caller.IsAgent() && h.ConfirmAgent != nil {
		timeout := h.Timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}
		waitCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		answer, err := h.ConfirmAgent(waitCtx, req)
		if err != nil {
			return err
		}
		if err := waitCtx.Err(); err != nil {
			return err
		}
		_, err = mapResult(answer)
		return err
	}
	_, err := h.wait(ctx, Pending{
		Type: TypeConfirm, Kind: req.Kind, Env: req.Env, Tool: req.Tool, Summary: req.Summary,
		AllowAlwaysOffered: !req.EnvBlocks,
	}, mapResult)
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
