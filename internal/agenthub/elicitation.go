package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"devkit/internal/vaultitem"
)

// ElicitationResolver mediates ACP elicitation/create against the UI.
// nil means there is nobody to ask, and the question is declined.
type ElicitationResolver func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error)

// The two answers DevKit gives on the person's behalf.
//
// decline is "skipped": the agent runs the tool with no answers and the turn
// carries on. cancel is "abandoned": the agent aborts the tool call. They are
// not interchangeable — declining a question the user stopped the turn over
// would let the tool run anyway.
var (
	declinedElicitation  = json.RawMessage(`{"action":"decline"}`)
	cancelledElicitation = json.RawMessage(`{"action":"cancel"}`)
)

// Why a pending elicitation stopped being pending, for the UI to drop its card.
const (
	ElicitationAnswered    = "answered"
	ElicitationCancelled   = "cancelled"    // the agent took it back, or its connection went
	ElicitationTurnStopped = "turn_stopped" // the user pressed Stop
)

// PendingElicitation is a question an agent is waiting for a person to answer.
//
// No ExpiresAt, unlike PendingPermission, and that is the whole difference in
// one field. A permission is an authorization boundary: silence has to mean no,
// so it fails closed after a deadline. A question grants nothing. A deadline on
// it would protect nothing and throw away an answer someone was halfway through
// typing, and the vendor keeps none either — it waits until it is answered,
// skipped, or cancelled.
type PendingElicitation struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	// ToolCallID ties the question to the tool call the agent emitted just
	// before asking (AskUserQuestion). Empty for an MCP server's question and
	// for the refusal-fallback prompt, which belong to no tool call.
	ToolCallID string `json:"toolCallId,omitempty"`
	// Mode is "form" — the only mode DevKit declares.
	Mode    string `json:"mode"`
	Message string `json:"message"`
	// Params is the request as the agent sent it, requestedSchema included. The
	// UI reads the schema; this package only reads enough of it to refuse to
	// send back what the agent would silently throw away.
	Params    json.RawMessage `json:"params"`
	CreatedAt time.Time       `json:"createdAt"`
}

// ElicitationClosed says a pending elicitation is gone and why.
//
// Permission has no such event, because its deadline takes a stale card down.
// A question has no deadline, so without this a card would outlive a turn the
// user stopped, or a question the agent withdrew, until someone answered
// something nobody was waiting for.
type ElicitationClosed struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

// ElicitationWaiter holds agent questions until a person answers them.
//
// A sibling of PermissionWaiter, not a generalisation of it: the two disagree on
// every default that matters. A permission fails closed, has a deadline, and
// is one at a time per session because a session prompts synchronously. A
// question fails to "skipped", has no deadline, and queues — a subagent and an
// MCP server can each ask while the main turn keeps working. Sharing one waiter
// would mean sharing the wrong defaults with one of them.
type ElicitationWaiter struct {
	notify func(PendingElicitation)
	closed func(ElicitationClosed)
	// Persistence hooks receive metadata only and run before publication or
	// before releasing an answer to the agent.
	PersistOpen  func(context.Context, PendingRequestRecord) error
	PersistClose func(context.Context, PendingRequestResolution) error
	// OnPersistenceError reports metadata durability failures without request
	// params, question text, or response data. It must be nonblocking and must
	// not re-enter the waiter; some paths report while holding its lock.
	OnPersistenceError func(error)

	mu      sync.Mutex
	pending map[string]*elicitationWait
}

type elicitationWait struct {
	PendingElicitation
	result         chan json.RawMessage
	finished       bool
	ctx            context.Context
	persistedClose bool
}

// NewElicitationWaiter creates a waiter. Either callback may be nil.
//
// Both are called without the waiter's lock held, and Wait starts waiting only
// after notify returns, so neither may block on anything slow.
func NewElicitationWaiter(notify func(PendingElicitation), closed func(ElicitationClosed)) *ElicitationWaiter {
	return &ElicitationWaiter{notify: notify, closed: closed, pending: map[string]*elicitationWait{}}
}

// Resolve implements ElicitationResolver for Service.ResolveElicitation.
func (w *ElicitationWaiter) Resolve(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	return w.Wait(ctx, sessionID, params)
}

// Wait registers a question and blocks until it is answered, the session's
// turn is stopped, or ctx ends.
//
// ctx ending is the agent taking the question back ($/cancel_request) or its
// connection going away, and the answer is cancel. That is a normal outcome,
// not an error: the response is still written, because the agent keeps a
// request it cancelled pending until one arrives.
func (w *ElicitationWaiter) Wait(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	if ctx.Err() != nil {
		return append(json.RawMessage(nil), cancelledElicitation...), nil
	}
	id, err := vaultitem.NewID()
	if err != nil {
		return nil, err
	}
	var head struct {
		Mode       string `json:"mode"`
		Message    string `json:"message"`
		ToolCallID string `json:"toolCallId"`
	}
	_ = json.Unmarshal(params, &head) // best effort; the UI reads Params itself

	p := PendingElicitation{
		ID:         id,
		SessionID:  sessionID,
		ToolCallID: head.ToolCallID,
		Mode:       head.Mode,
		Message:    head.Message,
		Params:     append(json.RawMessage(nil), params...),
		CreatedAt:  time.Now().UTC(),
	}
	if w.PersistOpen != nil {
		if err := w.PersistOpen(ctx, elicitationRecord(p)); err != nil {
			w.reportPersistenceError(err)
			return append(json.RawMessage(nil), declinedElicitation...), nil
		}
	}
	if ctx.Err() != nil {
		if w.PersistClose != nil {
			w.reportPersistenceError(w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: ElicitationCancelled, ResolvedAt: time.Now().UTC()}))
		}
		return append(json.RawMessage(nil), cancelledElicitation...), nil
	}
	resCh := make(chan json.RawMessage, 1)

	w.mu.Lock()
	w.pending[id] = &elicitationWait{PendingElicitation: p, result: resCh, ctx: ctx}
	w.mu.Unlock()

	if w.notify != nil {
		w.notify(p)
	}

	select {
	case <-ctx.Done():
		if w.finish(id, ElicitationCancelled) {
			w.fireClosed(ElicitationClosed{ID: id, SessionID: sessionID, Reason: ElicitationCancelled})
		}
		return append(json.RawMessage(nil), cancelledElicitation...), nil
	case response := <-resCh:
		if ctx.Err() != nil {
			return append(json.RawMessage(nil), cancelledElicitation...), nil
		}
		return response, nil
	}
}

// Complete answers a pending question with an ACP elicitation response body.
//
// The body is passed through as given; Service normalises it against the
// question's schema before it reaches the agent. The error strings match
// PermissionWaiter's so the bridge can show them the same way.
func (w *ElicitationWaiter) Complete(id string, response json.RawMessage) error {
	w.mu.Lock()
	p, ok := w.pending[id]
	if !ok || p.finished || p.ctx.Err() != nil {
		empty := len(w.pending) == 0
		w.mu.Unlock()
		if empty {
			return errors.New("agenthub: no pending elicitation")
		}
		return errors.New("agenthub: invalid elicitation id")
	}
	if w.PersistClose != nil {
		if err := w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: ElicitationAnswered, ResolvedAt: time.Now().UTC()}); err != nil {
			w.mu.Unlock()
			w.reportPersistenceError(err)
			return err
		}
		p.persistedClose = true
	}
	if p.ctx.Err() != nil {
		if w.PersistClose != nil {
			err := w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: ElicitationCancelled, ResolvedAt: time.Now().UTC()})
			w.reportPersistenceError(err)
			p.persistedClose = err == nil
		}
		w.mu.Unlock()
		return errors.New("agenthub: elicitation no longer waiting")
	}
	p.finished = true
	delete(w.pending, id)
	p.result <- append(json.RawMessage(nil), response...) // buffered, and only one sender wins finished
	sessionID := p.SessionID
	w.mu.Unlock()

	w.fireClosed(ElicitationClosed{ID: id, SessionID: sessionID, Reason: ElicitationAnswered})
	return nil
}

// CancelSession answers every question this session has open with cancel, and
// reports how many there were.
//
// Cancel, not decline. Stop means "abandon what you are doing"; a declined
// question would let the tool it belongs to run on with empty answers, which
// is the turn continuing after the user stopped it.
func (w *ElicitationWaiter) CancelSession(sessionID string) int {
	w.mu.Lock()
	var gone []ElicitationClosed
	for id, p := range w.pending {
		if p.SessionID != sessionID || p.finished {
			continue
		}
		closed := ElicitationClosed{ID: id, SessionID: sessionID, Reason: ElicitationTurnStopped}
		if w.PersistClose != nil {
			persistErr := w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: closed.Reason, ResolvedAt: time.Now().UTC()})
			p.persistedClose = persistErr == nil
			w.reportPersistenceError(persistErr)
		}
		p.finished = true
		delete(w.pending, id)
		p.result <- append(json.RawMessage(nil), cancelledElicitation...)
		gone = append(gone, closed)
	}
	w.mu.Unlock()

	for _, c := range gone {
		w.fireClosed(c)
	}
	return len(gone)
}

// HasSession reports whether an agent of the session is waiting on the person
// to answer a question.
func (w *ElicitationWaiter) HasSession(sessionID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range w.pending {
		if p.SessionID == sessionID && !p.finished {
			return true
		}
	}
	return false
}

// PendingList returns every open question, oldest first.
func (w *ElicitationWaiter) PendingList() []PendingElicitation {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]PendingElicitation, 0, len(w.pending))
	for _, p := range w.pending {
		cp := p.PendingElicitation
		cp.Params = append(json.RawMessage(nil), cp.Params...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Pending returns the oldest open question, or nil.
func (w *ElicitationWaiter) Pending() *PendingElicitation {
	list := w.PendingList()
	if len(list) == 0 {
		return nil
	}
	return &list[0]
}

// finish marks id resolved and reports whether this call was the one that did.
// Exactly one of Complete, CancelSession and a ctx ending wins, so a card gets
// exactly one closed event.
func (w *ElicitationWaiter) finish(id, reason string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	p, ok := w.pending[id]
	if !ok || p.finished {
		return false
	}
	if w.PersistClose != nil && !p.persistedClose {
		err := w.PersistClose(context.Background(), PendingRequestResolution{ID: id, Reason: reason, ResolvedAt: time.Now().UTC()})
		w.reportPersistenceError(err)
		p.persistedClose = err == nil
	}
	p.finished = true
	delete(w.pending, id)
	return true
}

func (w *ElicitationWaiter) fireClosed(c ElicitationClosed) {
	if w.closed != nil {
		w.closed(c)
	}
}

// mediateElicitation puts an agent's question to the person and returns the
// answer the agent will read.
//
// Much thinner than mediatePermission, deliberately. No session status change:
// the turn is still running — the agent is working around the question, not
// stalled on it — and awaiting_permission carries fail-closed recovery
// semantics that are wrong for a question. No deadline (see
// PendingElicitation). And no failure path that ends in an error: anything that
// goes wrong short of an answer is a skip, never an invented answer and never a
// crashed tool.
func (s *Service) mediateElicitation(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	if s.ResolveElicitation == nil {
		return append(json.RawMessage(nil), declinedElicitation...), nil
	}
	response, err := s.ResolveElicitation(ctx, sessionID, params)
	if ctx.Err() != nil {
		return append(json.RawMessage(nil), cancelledElicitation...), nil
	}
	if err != nil {
		return append(json.RawMessage(nil), declinedElicitation...), nil
	}
	return normalizeElicitationResponse(response, params), nil
}

// elicitHandler is the OnElicit every runtime of a session gets, in and out of
// a turn: there is nothing to authorise in a question, so "nobody to ask" is a
// worse answer than asking.
func (s *Service) elicitHandler(sessionID string) func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	return func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
		return s.mediateElicitation(ctx, sessionID, params)
	}
}

// normalizeElicitationResponse keeps exactly what the agent will read.
//
// The agent accepts content values of four kinds — string, number, boolean,
// string array — and anything else fails its guard, which it treats as no
// content at all. That failure has no symptom: the tool runs, the model is told
// the user answered nothing, and nobody sees why. So the shape is enforced here,
// where a test can see it, against the schema of the question being answered:
//
//   - action is accept, decline or cancel; anything else, or a body that does
//     not parse, is decline — never a fabricated answer;
//   - decline and cancel carry no content;
//   - accept keeps a key only if the question asked for it, and a value only if
//     its kind matches the property's declared type. An integer must be whole.
//     A property of a type this build does not know keeps any of the four kinds.
func normalizeElicitationResponse(raw, request json.RawMessage) json.RawMessage {
	var body struct {
		Action  string                     `json:"action"`
		Content map[string]json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return append(json.RawMessage(nil), declinedElicitation...)
	}
	switch strings.ToLower(strings.TrimSpace(body.Action)) {
	case "accept":
	case "cancel":
		return append(json.RawMessage(nil), cancelledElicitation...)
	default:
		return append(json.RawMessage(nil), declinedElicitation...)
	}

	var req struct {
		RequestedSchema struct {
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		} `json:"requestedSchema"`
	}
	_ = json.Unmarshal(request, &req)

	content := map[string]any{}
	for key, value := range body.Content {
		property, asked := req.RequestedSchema.Properties[key]
		if !asked {
			continue
		}
		if kept, ok := elicitationValue(property.Type, value); ok {
			content[key] = kept
		}
	}
	out, err := json.Marshal(map[string]any{"action": "accept", "content": content})
	if err != nil {
		return append(json.RawMessage(nil), declinedElicitation...)
	}
	return out
}

// elicitationValue decodes one content value and reports whether it is a kind
// the property's type allows.
func elicitationValue(propertyType string, raw json.RawMessage) (any, bool) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	switch propertyType {
	case "string":
		s, ok := v.(string)
		return s, ok
	case "number":
		n, ok := v.(float64)
		return n, ok
	case "integer":
		n, ok := v.(float64)
		if !ok || n != math.Trunc(n) {
			return nil, false
		}
		return n, true
	case "boolean":
		b, ok := v.(bool)
		return b, ok
	case "array":
		return stringArray(v)
	default:
		switch t := v.(type) {
		case string, float64, bool:
			return t, true
		default:
			return stringArray(v)
		}
	}
}

func stringArray(v any) (any, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func elicitationRecord(p PendingElicitation) PendingRequestRecord {
	return PendingRequestRecord{ID: p.ID, SessionID: p.SessionID, Kind: PendingRequestElicitation, CreatedAt: p.CreatedAt}
}

func (w *ElicitationWaiter) reportPersistenceError(err error) {
	if err != nil && w.OnPersistenceError != nil {
		w.OnPersistenceError(err)
	}
}
