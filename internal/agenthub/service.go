package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devkit/internal/acp"
	"devkit/internal/identity"

	"github.com/google/uuid"
)

// DefaultInvokeTimeout bounds Invoke when the caller context has no deadline.
// Overridable in tests.
var DefaultInvokeTimeout = 10 * time.Minute

// CreateInput is the request to open a new agent session.
type CreateInput struct {
	AgentType       AgentType
	Cwd             string
	ParentSessionID string // empty = root session
	Title           *string
}

// InvokeInput is a blocking prompt against an existing hub session.
type InvokeInput struct {
	SessionID string
	Prompt    string
	// Attachments are absolute paths DevKit reads and sends with the prompt.
	// Read by DevKit rather than by the agent on purpose: a path is only useful
	// to the agent if the file is inside its jail, and attaching something from
	// elsewhere on the machine is the case a path cannot serve. See
	// attachment.go for the check that keeps that from becoming a way around
	// the sandbox.
	Attachments     []string
	CallerSessionID string // optional; used for CheckCycle on cross-session invoke
}

// InvokeResult is the MCP/UI-facing outcome of a blocking Invoke.
type InvokeResult struct {
	Status       string   `json:"status"` // completed | cancelled | error
	Summary      string   `json:"summary"`
	FilesChanged []string `json:"filesChanged"`
	TurnCount    int      `json:"turnCount"`
	StopReason   string   `json:"stopReason"`
}

// MCPEndpoint is the identity-bound MCP socket handed to one vendor process.
type MCPEndpoint struct {
	SocketPath string
	Server     acp.McpServerStdio
	Close      func()
}

// MCPEndpoints opens one MCPEndpoint per session. nil means sessions get no
// DevKit MCP server (tests only; production wires mcp.SessionHost).
type MCPEndpoints interface {
	Open(sessionID string) (MCPEndpoint, error)
}

// Service orchestrates vendor adapters, session persistence, and live runtimes.
type Service struct {
	reg               *Registry
	store             Store
	recorder          *Recorder
	unknownSeen       map[string]struct{}
	runtimes          map[string]SessionRuntime
	endpoints         map[string]func() // per-session MCP socket close
	invoking          map[string]struct{}
	mu                sync.Mutex
	emit              func(sessionID string, update json.RawMessage)
	ResolvePermission PermissionResolver // nil = fail closed (no auto-approve)
	PermissionTimeout time.Duration      // 0 = interaction.DefaultTimeout (60s)
	// MCPEndpoints opens the per-session MCP socket before the vendor spawns.
	MCPEndpoints MCPEndpoints
	// OnSessionClosed fires after a session is closed (interaction.Hub forgets its allow_always).
	OnSessionClosed func(sessionID string)
	// OnUnknownUpdate reports a session/update kind this build does not model,
	// once per kind per session. It exists because the gap used to be wide and
	// silent: of the sixteen kinds claude-agent-acp 0.74 emits this build parsed
	// six, and the other ten — subagent_spawned, async_task_*, session_info_update
	// among them — degraded to UpdateUnknown and vanished without a trace, so
	// nothing on screen or in the log said the vendor was sending something new.
	// All sixteen are modelled now; this stays for the next version's additions.
	//
	// A hook rather than a log call for the same reason OnRecordError is one:
	// internal/agenthub owns no logger.
	OnUnknownUpdate func(sessionID, wireKind string, raw json.RawMessage)

	// OnRecordError reports a transcript write that failed. It is a hook, not
	// an error return: losing a row must not abort a turn the vendor is
	// already running, but it must not be silent either.
	OnRecordError func(sessionID string, err error)
}

// NewService wires registry + store. emit may be nil (no-op).
func NewService(reg *Registry, store Store, emit func(sessionID string, update json.RawMessage)) *Service {
	if emit == nil {
		emit = func(string, json.RawMessage) {}
	}
	return &Service{
		reg:       reg,
		store:     store,
		recorder:  NewRecorder(store),
		runtimes:  make(map[string]SessionRuntime),
		endpoints: make(map[string]func()),
		invoking:  make(map[string]struct{}),
		emit:      emit,
	}
}

// CreateSession opens a vendor runtime and persists an idle hub session.
//
// Order matters: the session id is generated first because the MCP socket is
// named after it and the sandbox must know that path before the vendor spawns.
func (s *Service) CreateSession(ctx context.Context, in CreateInput) (AgentSession, error) {
	adapter, ok := s.reg.Get(in.AgentType)
	if !ok {
		return AgentSession{}, ErrVendorUnknown
	}

	caller, _ := identity.FromContext(ctx)
	parentID := in.ParentSessionID
	if caller.IsAgent() {
		// An agent never chooses its parent: the parent is the caller.
		if parentID != "" && parentID != caller.SessionID {
			return AgentSession{}, ErrCallerMismatch
		}
		parentID = caller.SessionID
	}

	depth := 0
	var parent *AgentSession
	if parentID != "" {
		p, err := s.store.GetSession(ctx, parentID)
		if err != nil {
			return AgentSession{}, err
		}
		if err := CheckDepth(p.Depth, MaxDepth); err != nil {
			return AgentSession{}, err
		}
		depth = p.Depth + 1
		parent = &p
	}

	cwd, err := resolveCwd(in.Cwd, parent, caller.IsAgent())
	if err != nil {
		return AgentSession{}, err
	}

	id := uuid.NewString()
	spec := OpenSpec{Cwd: cwd}
	closeEndpoint := func() {}
	if s.MCPEndpoints != nil {
		ep, err := s.MCPEndpoints.Open(id)
		if err != nil {
			log.Printf("agenthub: %s session: open MCP endpoint: %v", in.AgentType, err)
			return AgentSession{}, err
		}
		closeEndpoint = ep.Close
		spec.AllowUnixSockets = []string{ep.SocketPath}
		spec.McpServers = []acp.McpServerStdio{ep.Server}
		spec.ReadOnlyPaths = []string{ep.Server.Command}
	}

	rt, err := adapter.Open(ctx, spec)
	if err != nil {
		// The bridge redacts this to one sentence; the log keeps the cause.
		log.Printf("agenthub: open %s session in %s: %v", in.AgentType, cwd, err)
		closeEndpoint()
		return AgentSession{}, err
	}

	now := time.Now().UTC()
	sess := AgentSession{
		ID:           id,
		AgentType:    in.AgentType,
		ProjectPath:  cwd,
		Status:       SessionStatusIdle,
		Depth:        depth,
		Title:        in.Title,
		ACPSessionID: rt.ACPSessionID(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if parentID != "" {
		pid := parentID
		sess.ParentSessionID = &pid
	}

	if err := s.store.InsertSession(ctx, sess); err != nil {
		_ = rt.Close(ctx)
		closeEndpoint()
		return AgentSession{}, err
	}

	s.mu.Lock()
	s.runtimes[sess.ID] = rt
	s.endpoints[sess.ID] = closeEndpoint
	s.mu.Unlock()

	return sess, nil
}

// resolveCwd applies the cwd rules. Nobody gets "/" or the home directory.
// An agent caller with no cwd inherits its parent's project; with one, it
// must stay inside the parent's project (symlinks resolved on both sides).
func resolveCwd(cwd string, parent *AgentSession, agentCaller bool) (string, error) {
	if agentCaller && cwd == "" && parent != nil {
		cwd = parent.ProjectPath
	}
	if cwd == "" || !filepath.IsAbs(cwd) {
		return "", ErrInvalidCwd
	}
	clean := filepath.Clean(cwd)
	if clean == "/" {
		return "", ErrInvalidCwd
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return "", ErrInvalidCwd
	}
	if agentCaller && parent != nil {
		resolved, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return "", ErrInvalidCwd
		}
		parentResolved, err := filepath.EvalSymlinks(parent.ProjectPath)
		if err != nil {
			return "", ErrInvalidCwd
		}
		rel, err := filepath.Rel(parentResolved, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", ErrCwdOutsideParent
		}
	}
	return clean, nil
}

// sessionsByID loads every session keyed by id (for ancestry walks).
func (s *Service) sessionsByID(ctx context.Context) (map[string]AgentSession, error) {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]AgentSession, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	return byID, nil
}

// authorizeTarget enforces the descendant rule: an agent caller may only act
// on itself or sessions inside its subtree. UI and terminal callers pass.
func (s *Service) authorizeTarget(ctx context.Context, targetID string) error {
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() {
		return nil
	}
	if targetID == caller.SessionID {
		return nil
	}
	byID, err := s.sessionsByID(ctx)
	if err != nil {
		return err
	}
	if ancestorContains(byID, targetID, caller.SessionID) {
		return nil
	}
	return ErrNotDescendant
}

// ListSessions returns all persisted hub sessions visible to the caller.
// Agents only see themselves and their descendants.
//
// Note: after a process restart, store rows may outlive in-memory runtimes
// (orphan sessions). Invoke/Cancel return ErrUnavailable for those; callers
// should not assume every listed session has a live runtime.
func (s *Service) ListSessions(ctx context.Context) ([]AgentSession, error) {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() {
		return list, nil
	}
	byID := make(map[string]AgentSession, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	out := make([]AgentSession, 0, len(list))
	for _, item := range list {
		if item.ID == caller.SessionID || ancestorContains(byID, item.ID, caller.SessionID) {
			out = append(out, item)
		}
	}
	return out, nil
}

// ListVendorTypes returns registered adapter types in stable order.
func (s *Service) ListVendorTypes() []AgentType {
	return s.reg.Types()
}

// GetStatus returns the persisted session record by id.
func (s *Service) GetStatus(ctx context.Context, id string) (AgentSession, error) {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return AgentSession{}, err
	}
	return s.store.GetSession(ctx, id)
}

// Transcript returns what was said in a session, oldest first. It is the read
// side of the recorder: the UI calls it on mount and then follows live events,
// which is what makes a reload mid-turn show the same thing it showed before.
func (s *Service) Transcript(ctx context.Context, id string) ([]TranscriptEntry, error) {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return nil, err
	}
	return s.store.ListTranscript(ctx, id)
}

// CloseSession closes the live runtime (if any) and marks the session closed.
func (s *Service) CloseSession(ctx context.Context, id string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	rt := s.runtimes[id]
	delete(s.runtimes, id)
	closeEndpoint := s.endpoints[id]
	delete(s.endpoints, id)
	s.mu.Unlock()

	if rt != nil {
		if err := rt.Close(ctx); err != nil {
			return err
		}
	}
	if closeEndpoint != nil {
		closeEndpoint()
	}

	sess.Status = SessionStatusClosed
	sess.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return err
	}
	if s.OnSessionClosed != nil {
		s.OnSessionClosed(id)
	}
	return nil
}

// DeleteSession permanently removes a session, its turns and its transcript,
// along with every descendant session.
//
// The subtree goes with it on purpose. A child session exists only because its
// parent spawned it, and deleting the parent alone would leave children whose
// lineage points at a row that is gone — the sidebar renders those as roots,
// so the swarm would silently reshape itself. Deleting one at a time in the
// right order is work the user should not have to do.
//
// Each session is closed first: the row is what the UI reads, but the process
// is what holds the port, the MCP endpoint and the sandbox. Deleting the row
// while the runtime lives would leave an agent running that nothing can reach
// or stop. A close that fails because the session was already closed is not an
// error here — that is the state we want.
func (s *Service) DeleteSession(ctx context.Context, id string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	byID, err := s.sessionsByID(ctx)
	if err != nil {
		return err
	}
	if _, ok := byID[id]; !ok {
		return ErrSessionNotFound
	}

	ids := descendantsOf(byID, id)
	for _, target := range ids {
		// Ask the vendor to forget it before the connection goes away. Best
		// effort on purpose: the user asked DevKit to delete a session, and
		// failing the whole operation because the agent could not tidy its own
		// copy would leave the rows behind too — the worse of the two states.
		s.deleteVendorCopy(ctx, target)
		if err := s.CloseSession(ctx, target); err != nil && !errors.Is(err, ErrSessionNotFound) {
			return err
		}
	}
	return s.store.DeleteSessions(ctx, ids)
}

// deleteVendorCopy tells the vendor to destroy its own record of a session.
//
// DevKit's DeleteSession removed its rows and stopped there, so a conversation
// deleted "for good" — the words in the confirmation — stayed readable in the
// vendor's own store. Only reachable while a runtime is live: a session whose
// process already died keeps its vendor-side copy, which is recorded rather
// than hidden.
func (s *Service) deleteVendorCopy(ctx context.Context, sessionID string) {
	s.mu.Lock()
	rt := s.runtimes[sessionID]
	s.mu.Unlock()

	deleter, ok := rt.(RuntimeDeleter)
	if !ok {
		return
	}
	if err := deleter.Delete(ctx); err != nil && s.OnRecordError != nil {
		s.OnRecordError(sessionID, err)
	}
}

// descendantsOf returns id and everything beneath it// descendantsOf returns id and everything beneath it, deepest last, so callers
// that must respect parent links can walk the slice backwards.
func descendantsOf(byID map[string]AgentSession, root string) []string {
	out := []string{root}
	for i := 0; i < len(out); i++ {
		for childID, child := range byID {
			if child.ParentSessionID != nil && *child.ParentSessionID == out[i] {
				out = append(out, childID)
			}
		}
	}
	return out
}

// Invoke runs a blocking prompt turn on an existing session.// Invoke runs a blocking prompt turn on an existing session.
func (s *Service) Invoke(ctx context.Context, in InvokeInput) (InvokeResult, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && DefaultInvokeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultInvokeTimeout)
		defer cancel()
	}

	sess, err := s.store.GetSession(ctx, in.SessionID)
	if err != nil {
		return InvokeResult{}, err
	}
	if err := s.authorizeTarget(ctx, in.SessionID); err != nil {
		return InvokeResult{}, err
	}
	if caller, _ := identity.FromContext(ctx); caller.IsAgent() {
		if in.CallerSessionID != "" && in.CallerSessionID != caller.SessionID {
			return InvokeResult{}, ErrCallerMismatch
		}
		in.CallerSessionID = caller.SessionID // CheckCycle below runs on the real caller
	}
	if sess.Status == SessionStatusClosed {
		return InvokeResult{}, ErrSessionNotFound
	}
	if sess.Status == SessionStatusRunning || sess.Status == SessionStatusAwaitingPermission {
		return InvokeResult{}, ErrSessionBusy
	}

	if in.CallerSessionID != "" {
		list, err := s.store.ListSessions(ctx)
		if err != nil {
			return InvokeResult{}, err
		}
		byID := make(map[string]AgentSession, len(list))
		for _, item := range list {
			byID[item.ID] = item
		}
		if err := CheckCycle(byID, in.CallerSessionID, in.SessionID); err != nil {
			return InvokeResult{}, err
		}
	}

	s.mu.Lock()
	if _, busy := s.invoking[in.SessionID]; busy {
		s.mu.Unlock()
		return InvokeResult{}, ErrSessionBusy
	}
	rt := s.runtimes[in.SessionID]
	s.mu.Unlock()

	if rt == nil {
		// An orphan: the row outlived the process that served it, which is
		// what every restart does to every open session. This used to be the
		// end of the conversation — ErrUnavailable, and the only way forward
		// was to start again somewhere else and lose the context.
		var err error
		if rt, err = s.resumeRuntime(ctx, sess); err != nil {
			return InvokeResult{}, err
		}
	}

	s.mu.Lock()
	if _, busy := s.invoking[in.SessionID]; busy {
		s.mu.Unlock()
		return InvokeResult{}, ErrSessionBusy
	}
	s.invoking[in.SessionID] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.invoking, in.SessionID)
		s.mu.Unlock()
	}()

	adapter, ok := s.reg.Get(sess.AgentType)
	if !ok {
		return InvokeResult{}, ErrVendorUnknown
	}

	// Everything that records what happened has to outlive whatever went
	// wrong. Values (caller identity, capability scope) are kept; only the
	// cancellation is dropped. SQLite's own busy_timeout bounds these writes,
	// so no deadline of ours is needed to stop one hanging forever.
	recordCtx := context.WithoutCancel(ctx)

	if err := s.setSessionStatus(ctx, in.SessionID, SessionStatusRunning); err != nil {
		return InvokeResult{}, err
	}

	var (
		lastUpdateMu sync.Mutex
		lastUpdate   json.RawMessage
		permDenied   atomic.Bool
	)
	rt.SetHandlers(RuntimeHandlers{
		OnUpdate: func(update json.RawMessage, meta json.RawMessage) {
			_ = meta
			lastUpdateMu.Lock()
			lastUpdate = append(json.RawMessage(nil), update...)
			lastUpdateMu.Unlock()
			// Persist before emitting: a UI that reloads mid-turn must find
			// the same transcript the live event described. Recording is
			// best-effort — a write failure must not abort a turn the agent is
			// already running, so the error rides the audit trail, not Prompt.
			//
			// recordCtx, not ctx: the turn's context is cancelled the moment
			// the turn goes wrong, and the update that says *why* is the last
			// one to arrive. Writing it under ctx meant the one message worth
			// keeping was the one guaranteed to be dropped — the log read
			// "transcript write failed: context canceled" and the UI drew an
			// empty bubble where the agent's error should have been.
			parsed := ParseSessionUpdate(update)
			if parsed.Kind == UpdateSessionInfo && parsed.Title != "" {
				if err := s.adoptSessionTitle(recordCtx, in.SessionID, parsed.Title); err != nil && s.OnRecordError != nil {
					s.OnRecordError(in.SessionID, err)
				}
			}
			if parsed.Kind == UpdateUnknown && s.OnUnknownUpdate != nil {
				// Once per kind per session: a stream of chunks of one unknown
				// kind is one fact, not a thousand log lines.
				if s.markUnknownSeen(in.SessionID, parsed.WireKind) {
					s.OnUnknownUpdate(in.SessionID, parsed.WireKind, parsed.Raw)
				}
			}
			if err := s.recorder.Record(recordCtx, in.SessionID, parsed); err != nil && s.OnRecordError != nil {
				s.OnRecordError(in.SessionID, err)
			}
			s.emit(in.SessionID, update)
		},
		OnPermission: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			outcome, err := s.mediatePermission(ctx, in.SessionID, params)
			if err != nil || isDeniedPermissionOutcome(outcome) {
				permDenied.Store(true)
				if len(outcome) == 0 {
					outcome = append(json.RawMessage(nil), deniedPermissionOutcome...)
				}
				return outcome, ErrPermissionDenied
			}
			return outcome, nil
		},
	})

	// Record what the human said before the agent gets a chance to answer.
	// ACP has a user_message_chunk kind but claude-agent-acp never echoes the
	// prompt back — session/update only carries what the agent emits — so a
	// transcript reloaded from the store showed the replies with nothing to
	// reply to. Written under recordCtx like every other transcript write, and
	// best-effort for the same reason: losing the echo must not fail the turn.
	if text := strings.TrimSpace(in.Prompt); text != "" {
		userEcho := SessionUpdate{
			Kind:      UpdateUserMessageChunk,
			Content:   &ContentBlock{Type: "text", Text: in.Prompt},
			MessageID: uuid.NewString(),
		}
		if err := s.recorder.Record(recordCtx, in.SessionID, userEcho); err != nil && s.OnRecordError != nil {
			s.OnRecordError(in.SessionID, err)
		}
		s.emit(in.SessionID, userEchoUpdate(in.Prompt, userEcho.MessageID))
	}

	invoker, _ := identity.FromContext(ctx)
	prompt, err := buildPrompt(in.Prompt, in.Attachments, sess.ProjectPath, callerIsAgent(invoker))
	if err != nil {
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusIdle)
		return InvokeResult{Status: "error"}, err
	}

	outcome, promptErr := rt.Prompt(ctx, prompt)
	if permDenied.Load() {
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, ErrPermissionDenied
	}
	if promptErr != nil {
		finalStatus := SessionStatusError
		if errors.Is(promptErr, ErrCancelled) || errors.Is(promptErr, context.Canceled) {
			finalStatus = SessionStatusIdle
			promptErr = ErrCancelled
		}
		if finalStatus == SessionStatusError {
			log.Printf("agenthub: invoke %s session %s: %v", sess.AgentType, in.SessionID, promptErr)
		}
		_ = s.setSessionStatus(recordCtx, in.SessionID, finalStatus)
		return InvokeResult{Status: statusForInvokeErr(promptErr)}, promptErr
	}

	usage, err := adapter.NormalizeUsage(outcome.Meta)
	if err != nil {
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}
	lastUpdateMu.Lock()
	updateCopy := append(json.RawMessage(nil), lastUpdate...)
	lastUpdateMu.Unlock()
	files, summary, err := adapter.NormalizeToolSummary(outcome.Meta, updateCopy)
	if err != nil {
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}
	if summary == "" {
		summary = "turn completed"
	}

	usageJSON, err := json.Marshal(usage)
	if err != nil {
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}

	turn := TurnRecord{
		ID:         uuid.NewString(),
		SessionID:  in.SessionID,
		StopReason: outcome.StopReason,
		UsageJSON:  usageJSON,
		RawMeta:    append(json.RawMessage(nil), outcome.Meta...),
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.store.InsertTurn(recordCtx, turn); err != nil {
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}

	turns, err := s.store.ListTurns(recordCtx, in.SessionID)
	if err != nil {
		_ = s.setSessionStatus(recordCtx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}

	if err := s.setSessionStatus(recordCtx, in.SessionID, SessionStatusIdle); err != nil {
		return InvokeResult{Status: "error"}, err
	}

	return InvokeResult{
		Status:       "completed",
		Summary:      summary,
		FilesChanged: files,
		TurnCount:    len(turns),
		StopReason:   outcome.StopReason,
	}, nil
}

// userEchoUpdate is the same echo in ACP's own wire shape, for the live event.
// The UI parses session/update notifications, not SessionUpdate values, so the
// echo has to arrive looking like one or the open rail would show the prompt
// only after a reload.
func userEchoUpdate(text, messageID string) json.RawMessage {
	body, err := json.Marshal(map[string]any{
		"sessionUpdate": string(UpdateUserMessageChunk),
		"content":       map[string]any{"type": "text", "text": text},
		"messageId":     messageID,
	})
	if err != nil {
		return nil
	}
	return body
}

// adoptSessionTitle takes the vendor's generated name for a session that has
// none. The agent produces it after the first exchange, from what was actually
// said — which is why sessions read "Session 1d5b9d50" until now: nothing else
// ever names them.
//
// Only fills an empty title, never replaces one. A name passed to CreateSession
// was chosen by a person, and the agent regenerates its own as a conversation
// drifts; letting the later guess overwrite the deliberate choice would make a
// named session quietly rename itself.
func (s *Service) adoptSessionTitle(ctx context.Context, sessionID, title string) error {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.Title != nil && strings.TrimSpace(*sess.Title) != "" {
		return nil
	}
	sess.Title = &title
	sess.UpdatedAt = time.Now().UTC()
	// No event of its own: the same update is already emitted to the UI on the
	// line below this call, carrying the title in its parsed form. A second
	// channel for one string would be a second thing to keep in sync.
	return s.store.UpdateSession(ctx, sess)
}

// markUnknownSeen reports whether this is the first time the session has seen
// this wire kind, and records it. Keyed by session so a long-lived hub still
// reports a new kind on the next session rather than staying quiet forever.
func (s *Service) markUnknownSeen(sessionID, wireKind string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unknownSeen == nil {
		s.unknownSeen = make(map[string]struct{})
	}
	key := sessionID + "\x00" + wireKind
	if _, ok := s.unknownSeen[key]; ok {
		return false
	}
	s.unknownSeen[key] = struct{}{}
	return true
}

// resumeRuntime reopens a session whose process is gone, using the vendor's own
// session id from the store.
//
// Fails with ErrUnavailable, not something new: to a caller this is still "no
// runtime", and only the reason differs — vendor cannot resume, no id was
// recorded, or the attempt failed. The old behaviour is the floor, so a vendor
// without SessionLoader is exactly as available as before.
//
// A fresh MCP endpoint is opened for the resumed session because the old one
// died with its process, and the identity-bound socket is what ties the agent's
// tool calls back to this session in the audit log.
func (s *Service) resumeRuntime(ctx context.Context, sess AgentSession) (SessionRuntime, error) {
	if sess.ACPSessionID == "" {
		return nil, ErrUnavailable
	}
	adapter, ok := s.reg.Get(sess.AgentType)
	if !ok {
		return nil, ErrVendorUnknown
	}
	loader, ok := adapter.(SessionLoader)
	if !ok {
		return nil, ErrUnavailable
	}

	spec := OpenSpec{Cwd: sess.ProjectPath}
	closeEndpoint := func() {}
	if s.MCPEndpoints != nil {
		ep, err := s.MCPEndpoints.Open(sess.ID)
		if err != nil {
			return nil, err
		}
		closeEndpoint = ep.Close
		spec.AllowUnixSockets = []string{ep.SocketPath}
		spec.McpServers = []acp.McpServerStdio{ep.Server}
		spec.ReadOnlyPaths = []string{ep.Server.Command}
	}

	rt, err := loader.Load(ctx, spec, sess.ACPSessionID)
	if err != nil {
		log.Printf("agenthub: resume %s session %s: %v", sess.AgentType, sess.ID, err)
		closeEndpoint()
		return nil, err
	}

	s.mu.Lock()
	// Another Invoke may have resumed the same session while this one was
	// spawning a process. The first one wins; a second live runtime for one
	// session would answer with two agents.
	if existing := s.runtimes[sess.ID]; existing != nil {
		s.mu.Unlock()
		_ = rt.Close(ctx)
		closeEndpoint()
		return existing, nil
	}
	s.runtimes[sess.ID] = rt
	s.endpoints[sess.ID] = closeEndpoint
	s.mu.Unlock()
	return rt, nil
}

// Cancel requests cancellation of the in-flight prompt on a session.// Cancel requests cancellation of the in-flight prompt on a session.// Cancel requests cancellation of the in-flight prompt on a session.
func (s *Service) Cancel(ctx context.Context, id string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	if _, err := s.store.GetSession(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	rt := s.runtimes[id]
	s.mu.Unlock()
	if rt == nil {
		return ErrUnavailable
	}
	return rt.Cancel(ctx)
}

func (s *Service) setSessionStatus(ctx context.Context, id string, status SessionStatus) error {
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	sess.Status = status
	sess.UpdatedAt = time.Now().UTC()
	return s.store.UpdateSession(ctx, sess)
}

func statusForInvokeErr(err error) string {
	if errors.Is(err, ErrCancelled) {
		return "cancelled"
	}
	return "error"
}
