package interaction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"devkit/internal/identity"
	"devkit/internal/vaultitem"
)

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
	// KindAgent is an agent session's message to a session outside its workspace,
	// or its choice of a model or effort above its own for a session it starts.
	KindAgent Kind = "agent"
	// KindGitAccount is a git remote that more than one account could
	// authenticate: the person says which, because choosing the wrong identity
	// is worse than asking once.
	KindGitAccount Kind = "git_account"
	// KindHarness is a change to the workspace harness profile or a session's
	// tool groups. It is always asked with EnvBlocks, so it never offers an
	// "always" answer and never reuses one.
	KindHarness Kind = "harness"
	// KindHarnessScope is the person's choice of where an agent's request for tool
	// groups is saved: this session only, or the workspace. It is a selection, not
	// an approval: the change is prepared and confirmed afterwards (KindHarness).
	KindHarnessScope Kind = "harness_scope"
)

// The two answers of a KindHarnessScope selection.
const (
	HarnessScopeSession   = "session"
	HarnessScopeWorkspace = "workspace"
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

type Target struct {
	Repo        string `json:"repo,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Remote      string `json:"remote,omitempty"`
	Destination string `json:"destination,omitempty"`
	Action      string `json:"action,omitempty"`
}

type ConfirmRequest struct {
	Target  Target
	Env     string
	Tool    string
	Summary string
	Kind    Kind
	// ProfileID scopes allow_always: a grant on one profile never covers another.
	ProfileID string
	// EnvBlocks is true when RequiresConfirmation(env) (incl. fail-closed).
	// When true, hub ignores allow_always cache and does not offer allow_always.
	EnvBlocks bool
	// These digests are computed by trusted handlers from complete execution
	// inputs and resolved targets, never from caller-supplied approval values.
	ApprovalFingerprint string
	TargetRevision      string
	EnvRevision         string
}

type Pending struct {
	Target             Target `json:"target"`
	ID                 string `json:"id"`
	Type               Type   `json:"type"`
	Kind               Kind   `json:"kind,omitempty"`
	Env                string `json:"env,omitempty"`
	Tool               string `json:"tool,omitempty"`
	Summary            string `json:"summary,omitempty"`
	AllowAlwaysOffered bool   `json:"allowAlwaysOffered"`
	Caller             string `json:"caller,omitempty"`
	// ExpiresAt is zero (and omitted) when nothing ends the wait: a question put
	// to a person has no deadline of its own, the agent asking it has none
	// either. It is set only when Hub.Timeout is configured or the caller's
	// context carries a deadline.
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	CreatedAt time.Time `json:"createdAt"`
	// Options and Remember are set on a KindGitAccount selection only: the
	// accounts to choose between, and what a kept answer would be kept for.
	Options  []AccountOption `json:"options,omitempty"`
	Remember string          `json:"remember,omitempty"`
}

// allowKey scopes an allow_always grant: it never crosses callers, tools or profiles.
type allowKey struct{ caller, tool, profile string }

type Hub struct {
	// Timeout ends a wait on its own. Zero, the default, is no deadline: a
	// permission popup stays until the person answers it or the caller's
	// context is cancelled (the session is stopped), the way the agent's own
	// turn waits. A positive value is for tests and embedders that want one.
	Timeout     time.Duration
	Headless    bool
	vaultLocked bool
	// ConfirmAgent routes an identity-bound Agent Hub caller to the shared
	// agent permission broker. Other callers keep the popup transport.
	ConfirmAgent          func(context.Context, ConfirmRequest) (string, error)
	OnScopedApprovalEvent func(ScopedApprovalEvent)
	prompter              Prompter

	mu          sync.Mutex
	pending     map[string]*pendingWait // by Pending.ID; at most one per caller
	allowAlways map[allowKey]struct{}   // cleared per caller by ForgetCaller, fully on restart
	approvals   map[string]*approvalRecord
}

type pendingWait struct {
	Pending
	result   chan string
	finished bool
	ctx      context.Context
}

type approvalScope struct{ caller, tool, profile, env, envRevision, fingerprint, targetRevision string }
type approvalRecord struct {
	ScopedApproval
	fingerprint, targetRevision string
	envRevision                 string
	reservations                map[string]struct{}
}

type ScopedApprovalEvent struct{ ID, Caller, Tool, ProfileID, Env, Action string }

// ScopedApproval is the UI representation; it contains no request body or secret.
type ScopedApproval struct {
	ID          string    `json:"id"`
	Caller      string    `json:"caller"`
	Tool        string    `json:"tool"`
	ProfileID   string    `json:"profileId"`
	Env         string    `json:"env"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"createdAt"`
	LastUsedAt  time.Time `json:"lastUsedAt,omitzero"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Remaining   int       `json:"remaining"`
}

func NewHub(p Prompter) *Hub {
	return &Hub{
		prompter:    p,
		pending:     map[string]*pendingWait{},
		allowAlways: map[allowKey]struct{}{},
		approvals:   map[string]*approvalRecord{},
	}
}

// PendingList returns every pending interaction, oldest first.
func (h *Hub) PendingList() []Pending {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Pending, 0, len(h.pending))
	for _, w := range h.pending {
		if !w.finished && w.ctx.Err() == nil {
			out = append(out, w.Pending)
		}
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
	if !ok || w.finished || w.ctx.Err() != nil || (!w.ExpiresAt.IsZero() && !time.Now().Before(w.ExpiresAt)) {
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
	for id, record := range h.approvals {
		if record.Caller == caller {
			delete(h.approvals, id)
		}
	}
}

// ClearScopedApprovals invalidates all in-memory approvals on vault lock.
func (h *Hub) ClearScopedApprovals() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.approvals = map[string]*approvalRecord{}
}

func (h *Hub) SetVaultLocked(locked bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vaultLocked = locked
	if locked {
		h.approvals = map[string]*approvalRecord{}
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

// SelectHarnessScope asks the person where a request for tool groups is saved.
// Only the two offered answers are accepted; anything else, a cancel and a
// closed prompt are all refusals. Headless is ErrSelectionRequired: nobody can
// choose, and a prompt nobody can answer must not turn into a default. summary
// is host-authored text (an agent's own words, if shown, are labelled by the
// caller).
func (h *Hub) SelectHarnessScope(ctx context.Context, summary string) (string, error) {
	if h == nil || h.Headless || h.prompter == nil {
		return "", ErrSelectionRequired
	}
	options := []AccountOption{
		{ID: HarnessScopeSession, Label: "This session only"},
		{ID: HarnessScopeWorkspace, Label: "Save to this workspace"},
	}
	return h.wait(ctx, Pending{Type: TypeSelect, Kind: KindHarnessScope, Summary: summary, Options: options}, func(v string) (string, error) {
		if v != HarnessScopeSession && v != HarnessScopeWorkspace {
			return "", ErrConfirmationDenied
		}
		return v, nil
	})
}

// CanAsk reports whether a person could answer a confirmation: there is a
// prompt handler and the hub is not headless (server mode).
func (h *Hub) CanAsk() bool { return h != nil && !h.Headless && h.prompter != nil }

func (h *Hub) Confirm(ctx context.Context, req ConfirmRequest) error {
	_, _, err := h.ConfirmDecision(ctx, req)
	return err
}

// ConfirmDecision reports whether this call was allowed by a scoped approval;
// callers use that signal for the stricter pre-execution revision recheck.
func (h *Hub) ConfirmDecision(ctx context.Context, req ConfirmRequest) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	if h.Headless || h.prompter == nil {
		return false, "", ErrConfirmationRequired
	}
	if scope, ok := approvalScopeFor(ctx, req); ok && !req.EnvBlocks {
		if token, reserved := h.reserveApproval(scope); reserved {
			return true, token, nil
		}
	}
	key := allowKey{caller: identity.Label(ctx), tool: req.Tool, profile: req.ProfileID}
	// Env block always wins: never short-circuit on allow_always.
	if !req.EnvBlocks {
		h.mu.Lock()
		_, ok := h.allowAlways[key]
		h.mu.Unlock()
		if ok {
			return false, "", nil
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
		waitCtx := ctx
		if h.Timeout > 0 {
			var cancel context.CancelFunc
			waitCtx, cancel = context.WithTimeout(ctx, h.Timeout)
			defer cancel()
		}
		answer, err := h.ConfirmAgent(waitCtx, req)
		if err != nil {
			return false, "", err
		}
		if err := waitCtx.Err(); err != nil {
			return false, "", err
		}
		if answer == "allow_scoped" {
			scope, ok := approvalScopeFor(ctx, req)
			if ok && !req.EnvBlocks {
				if token, reserved := h.reserveApproval(scope); reserved {
					return true, token, nil
				}
			}
			return false, "", ErrConfirmationDenied
		}
		_, err = mapResult(answer)
		return false, "", err
	}
	_, err := h.wait(ctx, Pending{
		Type: TypeConfirm, Kind: req.Kind, Env: req.Env, Tool: req.Tool, Summary: req.Summary,
		AllowAlwaysOffered: !req.EnvBlocks, Target: req.Target,
	}, mapResult)
	return false, "", err
}

// CreateScopedApprovalFromPending binds the approval to a request displayed by
// the desktop interaction UI. Bodies are not retained in the approval record.
// CreateScopedApprovalForAgent is the only approval issuer used by v1. The
// trusted Agent Hub bridge passes a live gate request read from PermissionWaiter;
// terminal MCP callers and request arguments cannot choose the scope.
func (h *Hub) CreateScopedApprovalForAgent(sessionID string, req ConfirmRequest, uses int, lifetime time.Duration) (ScopedApproval, error) {
	if h == nil || h.Headless || h.isVaultLocked() {
		return ScopedApproval{}, ErrConfirmationRequired
	}
	if sessionID == "" {
		return ScopedApproval{}, ErrInvalidInteractionID
	}
	ctx := identity.WithCaller(context.Background(), identity.Agent(sessionID))
	scope, ok := approvalScopeFor(ctx, req)
	if !ok || req.EnvBlocks {
		return ScopedApproval{}, ErrInvalidInteractionID
	}
	return h.createScopedApproval(scope, uses, lifetime)
}

func (h *Hub) createScopedApproval(scope approvalScope, uses int, lifetime time.Duration) (ScopedApproval, error) {
	if uses < 1 || uses > 100 {
		return ScopedApproval{}, errors.New("use count must be between 1 and 100")
	}
	if lifetime == 0 {
		lifetime = 30 * time.Minute
	}
	if lifetime < time.Minute || lifetime > 8*time.Hour {
		return ScopedApproval{}, errors.New("approval lifetime must be between 1 minute and 8 hours")
	}
	newID, err := vaultitem.NewID()
	if err != nil {
		return ScopedApproval{}, err
	}
	now := time.Now().UTC()
	r := &approvalRecord{ScopedApproval: ScopedApproval{ID: newID, Caller: scope.caller, Tool: scope.tool, ProfileID: scope.profile, Env: scope.env, Fingerprint: scope.fingerprint, CreatedAt: now, ExpiresAt: now.Add(lifetime), Remaining: uses}, fingerprint: scope.fingerprint, targetRevision: scope.targetRevision, envRevision: scope.envRevision, reservations: map[string]struct{}{}}
	h.mu.Lock()
	if h.vaultLocked {
		h.mu.Unlock()
		return ScopedApproval{}, ErrConfirmationRequired
	}
	h.approvals[newID] = r
	h.mu.Unlock()
	h.emitApproval(ScopedApprovalEvent{ID: newID, Caller: scope.caller, Tool: scope.tool, ProfileID: scope.profile, Env: scope.env, Action: "created"})
	return r.ScopedApproval, nil
}

func (h *Hub) ScopedApprovals() []ScopedApproval {
	if h == nil || h.Headless || h.isVaultLocked() {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	out := make([]ScopedApproval, 0, len(h.approvals))
	for id, r := range h.approvals {
		if !now.Before(r.ExpiresAt) || r.Remaining <= 0 {
			delete(h.approvals, id)
			continue
		}
		copy := r.ScopedApproval
		copy.Remaining -= len(r.reservations)
		out = append(out, copy)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (h *Hub) RevokeScopedApproval(id string) error {
	if h == nil || h.Headless || h.isVaultLocked() {
		return ErrConfirmationRequired
	}
	h.mu.Lock()
	if h.vaultLocked {
		h.mu.Unlock()
		return ErrConfirmationRequired
	}
	r, ok := h.approvals[id]
	if !ok {
		h.mu.Unlock()
		return ErrInvalidInteractionID
	}
	delete(h.approvals, id)
	h.mu.Unlock()
	h.emitApproval(ScopedApprovalEvent{ID: id, Caller: r.Caller, Tool: r.Tool, ProfileID: r.ProfileID, Env: r.Env, Action: "revoked"})
	return nil
}

// approvalScopeFor is the central fail-closed eligibility registry for v1.
func approvalScopeFor(ctx context.Context, req ConfirmRequest) (approvalScope, bool) {
	if req.Tool != "db.query" || req.Kind != KindDatabase || req.ProfileID == "" || req.Env == "" || req.EnvRevision == "" || req.ApprovalFingerprint == "" || req.TargetRevision == "" {
		return approvalScope{}, false
	}
	caller := identity.Label(ctx)
	if caller == "" || caller == "unknown" {
		return approvalScope{}, false
	}
	return approvalScope{caller: caller, tool: req.Tool, profile: req.ProfileID, env: req.Env, envRevision: req.EnvRevision, fingerprint: req.ApprovalFingerprint, targetRevision: req.TargetRevision}, true
}

func (h *Hub) consumeApproval(scope approvalScope, token string) bool {
	if token == "" {
		return false
	}
	h.mu.Lock()
	if h.vaultLocked {
		h.mu.Unlock()
		return false
	}
	now := time.Now()
	for id, r := range h.approvals {
		if !now.Before(r.ExpiresAt) || r.Remaining <= 0 {
			delete(h.approvals, id)
			continue
		}
		_, reserved := r.reservations[token]
		if reserved && r.Caller == scope.caller && r.Tool == scope.tool && r.ProfileID == scope.profile && r.Env == scope.env && r.envRevision == scope.envRevision && r.fingerprint == scope.fingerprint && r.targetRevision == scope.targetRevision {
			delete(r.reservations, token)
			r.Remaining--
			r.LastUsedAt = now.UTC()
			if r.Remaining == 0 {
				delete(h.approvals, id)
			}
			h.mu.Unlock()
			h.emitApproval(ScopedApprovalEvent{ID: id, Caller: scope.caller, Tool: scope.tool, ProfileID: scope.profile, Env: scope.env, Action: "started"})
			return true
		}
	}
	h.mu.Unlock()
	return false
}

func (h *Hub) reserveApproval(scope approvalScope) (string, bool) {
	token, err := vaultitem.NewID()
	if err != nil {
		return "", false
	}
	h.mu.Lock()
	if h.vaultLocked {
		h.mu.Unlock()
		return "", false
	}
	now := time.Now()
	for id, r := range h.approvals {
		if !now.Before(r.ExpiresAt) || r.Remaining <= 0 {
			delete(h.approvals, id)
			continue
		}
		if r.Caller == scope.caller && r.Tool == scope.tool && r.ProfileID == scope.profile && r.Env == scope.env && r.envRevision == scope.envRevision && r.fingerprint == scope.fingerprint && r.targetRevision == scope.targetRevision && len(r.reservations) < r.Remaining {
			r.reservations[token] = struct{}{}
			h.mu.Unlock()
			h.emitApproval(ScopedApprovalEvent{ID: id, Caller: scope.caller, Tool: scope.tool, ProfileID: scope.profile, Env: scope.env, Action: "reserved"})
			return token, true
		}
	}
	h.mu.Unlock()
	return "", false
}

func (h *Hub) emitApproval(event ScopedApprovalEvent) {
	if h.OnScopedApprovalEvent != nil {
		h.OnScopedApprovalEvent(event)
	}
}

func (h *Hub) isVaultLocked() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.vaultLocked }

// StartScopedApproval atomically rechecks revocation/expiry and consumes one
// reserved use at the external side-effect boundary.
func (h *Hub) StartScopedApproval(ctx context.Context, req ConfirmRequest, token string) bool {
	scope, ok := approvalScopeFor(ctx, req)
	if !ok || req.EnvBlocks {
		return false
	}
	return h.consumeApproval(scope, token)
}

// CancelScopedApproval releases a reservation if capability authorization or
// target revalidation stopped execution before the side effect began.
func (h *Hub) CancelScopedApproval(ctx context.Context, req ConfirmRequest, token string) {
	scope, ok := approvalScopeFor(ctx, req)
	if !ok || token == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.approvals {
		if _, exists := r.reservations[token]; exists && r.Caller == scope.caller && r.Tool == scope.tool && r.ProfileID == scope.profile && r.Env == scope.env && r.envRevision == scope.envRevision && r.fingerprint == scope.fingerprint && r.targetRevision == scope.targetRevision {
			delete(r.reservations, token)
			return
		}
	}
}

func (h *Hub) RevokeMatchingScopedApproval(ctx context.Context, req ConfirmRequest) {
	scope, ok := approvalScopeFor(ctx, req)
	if !ok {
		return
	}
	h.mu.Lock()
	var events []ScopedApprovalEvent
	for id, r := range h.approvals {
		if r.Caller == scope.caller && r.Tool == scope.tool && r.ProfileID == scope.profile && r.Env == scope.env && r.envRevision == scope.envRevision && r.fingerprint == scope.fingerprint && r.targetRevision == scope.targetRevision {
			delete(h.approvals, id)
			events = append(events, ScopedApprovalEvent{ID: id, Caller: r.Caller, Tool: r.Tool, ProfileID: r.ProfileID, Env: r.Env, Action: "invalidated"})
		}
	}
	h.mu.Unlock()
	for _, event := range events {
		h.emitApproval(event)
	}
}

// InvalidateChangedScopedApprovals removes approvals whose captured revisions
// no longer describe the resolved profile or environment policy.
func (h *Hub) InvalidateChangedScopedApprovals(profile, env, targetRevision, envRevision string) {
	if h == nil {
		return
	}
	var events []ScopedApprovalEvent
	h.mu.Lock()
	for id, r := range h.approvals {
		if r.ProfileID == profile && (r.Env != env || r.targetRevision != targetRevision || r.envRevision != envRevision) {
			delete(h.approvals, id)
			events = append(events, ScopedApprovalEvent{ID: id, Caller: r.Caller, Tool: r.Tool, ProfileID: r.ProfileID, Env: r.Env, Action: "invalidated"})
		}
	}
	h.mu.Unlock()
	for _, event := range events {
		h.emitApproval(event)
	}
}

func Fingerprint(input []byte) string { sum := sha256.Sum256(input); return hex.EncodeToString(sum[:]) }

func (h *Hub) wait(ctx context.Context, p Pending, mapResult func(string) (string, error)) (string, error) {
	id, err := vaultitem.NewID()
	if err != nil {
		return "", err
	}
	p.ID = id
	p.Caller = identity.Label(ctx)
	p.CreatedAt = time.Now().UTC()
	if h.Timeout > 0 {
		p.ExpiresAt = p.CreatedAt.Add(h.Timeout)
	}
	if deadline, ok := ctx.Deadline(); ok && (p.ExpiresAt.IsZero() || deadline.Before(p.ExpiresAt)) {
		p.ExpiresAt = deadline
	}
	resCh := make(chan string, 1)

	h.mu.Lock()
	for _, w := range h.pending {
		if w.Caller == p.Caller {
			h.mu.Unlock()
			return "", ErrCallerBusy
		}
	}
	h.pending[id] = &pendingWait{Pending: p, result: resCh, ctx: ctx}
	h.mu.Unlock()

	defer h.invalidatePending(id)

	if err := h.prompter.Prompt(p); err != nil {
		return "", err
	}

	// A nil channel never fires: with no deadline only an answer or ctx ends it.
	var expired <-chan time.Time
	if !p.ExpiresAt.IsZero() {
		timer := time.NewTimer(time.Until(p.ExpiresAt))
		defer timer.Stop()
		expired = timer.C
	}

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-expired:
		return "", ErrInteractionTimeout
	case v := <-resCh:
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !p.ExpiresAt.IsZero() && !time.Now().Before(p.ExpiresAt) {
			return "", ErrInteractionTimeout
		}
		return mapResult(v)
	}
}
