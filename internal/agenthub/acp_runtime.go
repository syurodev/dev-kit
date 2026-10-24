package agenthub

// The generic ACP runtime. Claude and opencode both speak ACP over stdio to a
// sandboxed process, and everything here is what they have in common:
// handshake bookkeeping, the update stream, the person's permission answers,
// prompts, cancel and close. What a vendor differs in either stays in its own
// adapter (opencode's modes and cancel, Claude's jail and _meta), or is a hook
// on acpRuntimeHooks below.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"devkit/internal/acp"
	"devkit/internal/sandbox"
)

// stdioPipe adapts separate stdin writer + stdout reader into one ReadWriteCloser
// for acp.NewConn (write → agent stdin, read ← agent stdout).
type stdioPipe struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p *stdioPipe) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *stdioPipe) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *stdioPipe) Close() error {
	errW := p.w.Close()
	errR := p.r.Close()
	if errW != nil {
		return errW
	}
	return errR
}

// procKiller adapts *sandbox.Proc to acp.ProcessKiller (adds Signal).
type procKiller struct {
	proc *sandbox.Proc
}

func (k *procKiller) PID() int    { return k.proc.PID() }
func (k *procKiller) Kill() error { return k.proc.Kill() }
func (k *procKiller) Wait() error { return k.proc.Wait() }
func (k *procKiller) Signal(sig os.Signal) error {
	pid := k.proc.PID()
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(sig)
}

// acpRuntimeHooks is everything a vendor adapter changes about the generic
// runtime. The zero value is Claude: a runtime with no hooks reports Claude's
// errors and passes the person's permission answer through untouched.
type acpRuntimeHooks struct {
	// Type names whose agent the runtime talks to. The runtime itself is the
	// generic ACP one (opencode reuses it), and only its messages and logs need
	// to know which vendor they are about.
	Type AgentType
	// PermissionBefore and PermissionAfter let a vendor adapter set the policy
	// around the person's answer: Before may rewrite the request or answer it
	// itself (answer non-nil), After may adjust what the person chose. Both nil
	// for Claude, whose own modes decide what is asked.
	PermissionBefore func(ctx context.Context, params json.RawMessage) (rewritten, answer json.RawMessage)
	PermissionAfter  func(answer json.RawMessage) json.RawMessage
}

type acpRuntime struct {
	// hooks is what the vendor adapter running this runtime changes. The zero
	// value is Claude's behaviour.
	hooks acpRuntimeHooks

	environment  *environmentSnapshot
	conn         *acp.Conn
	proc         *sandbox.Proc
	killer       acp.ProcessKiller
	acpSessionID string
	// modes is the handshake's answer, guarded by mu: set before the runtime is
	// returned, then its CurrentModeID follows SetMode and vendor updates (a
	// copy is swapped in, never mutated), so Modes() names the mode the agent
	// is in now rather than the one it started in. steering and goal are read from the same answer:
	// both are extensions the agent advertises in initialize's top-level _meta,
	// and calling either without that is a method the agent would refuse.
	modes        *acp.SessionModeState
	modeRevision uint64 // mode changes/notifications observed, guarded by mu
	pendingMode  bool   // publish current mode when an update handler attaches
	// configOptions is what session/new said the agent's settings are, as the
	// agent wrote it. ConfigCatalog reads it for the effort levels it starts
	// from; options (below) is the live copy.
	configOptions json.RawMessage
	steering      bool
	goalControl   string
	cwd           string
	cancelServe   context.CancelFunc
	serveDone     chan struct{}

	// lastRetry is the provider failure the agent last said it was retrying
	// during the running prompt (opencode's retry notice).
	retryMu   sync.Mutex
	lastRetry *ProviderRetry

	mu       sync.Mutex
	handlers RuntimeHandlers
	closed   bool
	// options is the agent's settings kept current: what the handshake said,
	// changed by every setConfigOption answer. claude-agent-acp sends the model
	// list once, in the answer to session/new or session/load, and never again
	// until something changes — so DevKit has to keep it itself, or a session
	// that has not been prompted yet has no model choices anywhere.
	options []ConfigOption
	// announced: the first update handler has been given options.
	announced bool
	// closing records that Close asked the serve loop to stop. Without it the
	// only evidence of a deliberate shutdown was Serve's error, and Serve
	// reports a clean EOF as no error at all.
	closing bool
	exitErr error
}

// vendorType is the vendor this runtime reports errors for.
func (r *acpRuntime) vendorType() AgentType {
	if r.hooks.Type == "" {
		return AgentTypeClaude
	}
	return r.hooks.Type
}

// noteServeExit records that the serve loop ended. Everything after this is a
// runtime with no process behind it: Prompt and the settings calls refuse, and
// Dead tells Service to resume the session on a fresh one.
func (r *acpRuntime) noteServeExit(serveErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	// A deliberate Close cancels the serve context first; only record the exit
	// as abnormal when Serve failed for another reason.
	if serveErr != nil && !errors.Is(serveErr, context.Canceled) && r.exitErr == nil {
		r.exitErr = serveErr
	}
}

// Dead reports whether the agent process went away without Close asking it to
// — a crash, an OOM kill, the shutdown ladder Cancel runs, or the agent simply
// exiting. Service.liveRuntime evicts a dead runtime and resumes the session on
// a fresh process, so Retry never prompts the corpse.
//
// The test is "the serve loop ended and nobody asked it to", NOT "it ended with
// an error": acp.Conn.Serve reports a clean EOF as a nil error, which is what a
// process exiting on its own looks like from here. Reading the error instead
// left the corpse in the live map for good — every later prompt on that session
// answered agenthub.sessionNotFound, and no retry could clear it, because
// nothing ever evicted the runtime that was refusing.
func (r *acpRuntime) Dead() (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed && !r.closing, r.exitErr
}

func (r *acpRuntime) ACPSessionID() string { return r.acpSessionID }

// Modes satisfies RuntimeModes.
func (r *acpRuntime) Modes() *acp.SessionModeState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.modes
}

func (r *acpRuntime) SetMode(ctx context.Context, modeID string) error {
	r.mu.Lock()
	conn, sid, closed := r.conn, r.acpSessionID, r.closed
	revision := r.modeRevision
	r.mu.Unlock()
	if closed {
		return ErrSessionNotFound
	}
	var result json.RawMessage
	if err := conn.Call(ctx, "session/set_mode", acp.SessionSetModeRequest{SessionID: sid, ModeID: modeID}, &result); err != nil {
		return fmt.Errorf("%w: session/set_mode: %v", ErrProcessFailed, err)
	}
	r.recordSetMode(modeID, revision)
	return nil
}

func (r *acpRuntime) SetConfigOption(ctx context.Context, optionID, value string) error {
	// The draft picker is vendor-neutral and says reasoning_effort; Claude's own
	// name for it is effort.
	if optionID == codexEffortOption {
		optionID = claudeEffortOption
	}
	_, err := r.setConfigOption(ctx, optionID, value)
	return err
}

// setConfigOption answers with the agent's settings after the change: the
// effort levels on offer follow the model, so this is how a model's levels are
// read without running a turn.
func (r *acpRuntime) setConfigOption(ctx context.Context, optionID, value string) ([]ConfigOption, error) {
	r.mu.Lock()
	conn, sid, closed := r.conn, r.acpSessionID, r.closed
	r.mu.Unlock()
	if closed {
		return nil, ErrSessionNotFound
	}
	var result struct {
		ConfigOptions json.RawMessage `json:"configOptions"`
	}
	if err := conn.Call(ctx, "session/set_config_option", acp.SessionSetConfigOptionRequest{SessionID: sid, ConfigID: optionID, Value: value}, &result); err != nil {
		return nil, fmt.Errorf("%w: session/set_config_option: %v", ErrProcessFailed, err)
	}
	answer := parseConfigOptions(result.ConfigOptions)
	r.noteConfigChange(optionID, value, answer)
	return answer, nil
}

// SetHandlers installs the callbacks. The first time an update handler arrives
// the agent's settings are announced through it, as Codex's runtime does: the
// handshake carried them, no config_option_update ever will, and a session
// opened by an agent has nobody listening until Service attaches.
func (r *acpRuntime) SetHandlers(h RuntimeHandlers) {
	r.mu.Lock()
	r.handlers = h
	mode := r.takePendingModeLocked()
	var announce json.RawMessage
	if h.OnUpdate != nil && !r.announced && len(r.options) > 0 {
		r.announced = true
		announce = r.configOptionsUpdateLocked()
	}
	r.mu.Unlock()
	if mode != nil {
		h.OnUpdate(mode, nil)
	}
	if announce != nil {
		h.OnUpdate(announce, nil)
	}
}

// RefreshConfigOptions publishes the settings the runtime holds again, for a
// reader that missed the announcement (a window opened later). Nothing is
// asked of the agent: it has no call that lists them without changing one.
func (r *acpRuntime) RefreshConfigOptions(context.Context) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	var update json.RawMessage
	if len(r.options) > 0 {
		update = r.configOptionsUpdateLocked()
	}
	onUpdate := r.handlers.OnUpdate
	r.mu.Unlock()
	if update != nil && onUpdate != nil {
		onUpdate(update, nil)
	}
	return nil
}

func (r *acpRuntime) configOptionsUpdateLocked() json.RawMessage {
	return marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateConfigOptions),
		"configOptions": r.options,
	})
}

// setHandshakeOptions records what session/new, session/load or session/fork
// answered with the settings.
func (r *acpRuntime) setHandshakeOptions(raw json.RawMessage) {
	r.configOptions = raw
	parsed := parseConfigOptions(raw)
	r.mu.Lock()
	r.options = parsed
	r.mu.Unlock()
}

// noteConfigChange folds a session/set_config_option answer into the held
// settings. The answer lists the agent's settings after the change — the effort
// levels on offer follow the model — but only what it names is replaced, so an
// answer that leaves the rest out cannot erase it. The changed option's value
// is set from the request, since an answer that omits it still means it took.
func (r *acpRuntime) noteConfigChange(optionID, value string, answer []ConfigOption) {
	r.mu.Lock()
	defer r.mu.Unlock()
	chosen, _ := json.Marshal(value)
	for i := range r.options {
		if r.options[i].ID == optionID {
			r.options[i].CurrentValue = chosen
		}
	}
	r.options = mergeConfigOptions(r.options, answer)
}

// mergeConfigOptions replaces the options base has under the ids update names
// and appends the ones it does not have, keeping base's order.
func mergeConfigOptions(base, update []ConfigOption) []ConfigOption {
	out := append([]ConfigOption(nil), base...)
	for _, u := range update {
		replaced := false
		for i := range out {
			if out[i].ID == u.ID {
				out[i] = u
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, u)
		}
	}
	return out
}

// Delete asks Claude to remove its own copy of the session, satisfying
// RuntimeDeleter.
//
// DevKit's DeleteSession removed its rows and left this behind: the vendor
// keeps every session as a file under its own directory, so a conversation the
// user deleted "for good" stayed readable there. The dialog says it cannot be
// undone, and until this it could be.
//
// ponytail: only reachable while a runtime is live. Deleting a session whose
// process is gone would mean spawning one to clean up after it — resumeRuntime
// makes that possible if the leftovers ever matter more than the cost.
func (r *acpRuntime) Delete(ctx context.Context) error {
	return r.DeleteSession(ctx, r.acpSessionID)
}

// DeleteSession asks this process's agent to forget any session, not just its
// own — what a rewind needs for the conversation it replaced, whose own process
// is already gone.
func (r *acpRuntime) DeleteSession(ctx context.Context, acpSessionID string) error {
	if acpSessionID == "" {
		return nil
	}
	var out json.RawMessage
	if err := r.conn.Call(ctx, "session/delete", acp.SessionDeleteRequest{
		SessionID: acpSessionID,
	}, &out); err != nil {
		return fmt.Errorf("%w: session/delete: %v", ErrProcessFailed, err)
	}
	return nil
}

func (r *acpRuntime) handlersSnapshot() RuntimeHandlers {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handlers
}

func (r *acpRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, meta json.RawMessage) (PromptOutcome, error) {
	if err := ctx.Err(); err != nil {
		return PromptOutcome{}, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return PromptOutcome{}, ErrSessionNotFound
	}
	conn := r.conn
	sid := r.acpSessionID
	r.mu.Unlock()

	// session/prompt has no timeout on purpose — a turn takes as long as the
	// work does — so a turn that never answers is indistinguishable from one
	// still thinking. These two lines bracket the call: "sent" with nothing
	// after it says the agent has the prompt and is not replying, which is a
	// different bug from never having sent it at all.
	log.Printf("agenthub: %s session/prompt sent (session %s)", r.vendorType(), sid)
	r.retryMu.Lock()
	r.lastRetry = nil
	r.retryMu.Unlock()
	var res acp.PromptResult
	err := conn.Call(ctx, "session/prompt", acp.PromptRequest{
		SessionID: sid,
		Prompt:    prompt,
		Meta:      meta,
	}, &res)
	if err == nil {
		log.Printf("agenthub: %s session/prompt returned (session %s, stop %q)", r.vendorType(), sid, res.StopReason)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return PromptOutcome{}, ErrCancelled
		}
		// A prompt is where the SDK actually starts Claude Code, so an agent that
		// dies here is the common failure after a clean handshake. Its stderr is
		// the only record of why.
		reason := ""
		select {
		case <-r.serveDone:
			if r.proc != nil {
				reason = stderrReason(r.proc)
				logStderrTail(string(r.vendorType())+" session/prompt", r.proc)
			}
		default:
		}
		log.Printf("agenthub: %s session/prompt (session %s): %v%s", r.vendorType(), sid, err, reason)
		if isAuthRequired(err) {
			return PromptOutcome{}, vendorError(r.vendorType(), fmt.Errorf("%w: %v", ErrAgentAuthRequired, err))
		}
		if r.refusedByLiveAgent(err) {
			return PromptOutcome{}, r.turnRefusal(err)
		}
		return PromptOutcome{}, fmt.Errorf("%w: session/prompt: %v%s", ErrProcessFailed, err, reason)
	}
	return PromptOutcome{StopReason: res.StopReason, Meta: res.Meta}, nil
}

// refusedByLiveAgent is whether a failed session/prompt was an answer from a
// process that is still running — a JSON-RPC error response — rather than a
// process that went away. The first is a turn the agent ended (its provider
// failed, its context is full); calling it a crash put "the agent stopped
// unexpectedly" and an error state on a session that was fine, and hid the
// sentence the agent gave. A transport error (closed pipe, EOF) or a serve
// loop that has already ended stays a process failure.
func (r *acpRuntime) refusedByLiveAgent(err error) bool {
	var rpcErr *acp.RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	select {
	case <-r.serveDone:
		return false
	default:
		return true
	}
}

// turnRefusal is a turn the live agent ended, as ErrTurnFailed carrying its own
// sentence, and says it in the session first: the banner is what the
// transcript keeps, where the error only reaches the caller of this prompt
// (the same order as codexTurnFailure).
func (r *acpRuntime) turnRefusal(err error) error {
	message := rpcSentence(err)
	if message == "" {
		message = r.lastRetrySentence()
	}
	failure := SessionFailure{
		ID:       "agent-turn-failed",
		Revision: int(time.Now().Unix()),
		Category: "turn_failed",
		Severity: "error",
		Title:    "The turn didn't finish",
		Details:  message,
		Actions:  []string{"retry"},
	}
	if on := r.handlersSnapshot().OnUpdate; on != nil {
		on(sessionFailureUpdate(failure), nil)
	}
	failed := vendorMessageError(r.vendorType(), fmt.Errorf("%w: session/prompt: %s", ErrTurnFailed, message), message)
	r.retryMu.Lock()
	retry := r.lastRetry
	r.retryMu.Unlock()
	return WithStopHint(failed, retry.stopHint())
}

// lastRetrySentence is what the provider said the last time the agent retried
// during this prompt, or "". An agent that retried its provider until it gave
// up answers with a bare "Internal error"; the provider's own sentence is the
// one it showed while retrying.
func (r *acpRuntime) lastRetrySentence() string {
	r.retryMu.Lock()
	retry := r.lastRetry
	r.retryMu.Unlock()
	if retry == nil {
		return ""
	}
	return retry.Sentence()
}

// noteRetry remembers the agent's retry notice of the running prompt. A null
// notice (the next attempt began) leaves the last one in place: it is what the
// final failure was about.
func (r *acpRuntime) noteRetry(update json.RawMessage) {
	if !bytes.Contains(update, []byte("opencode/retry")) {
		return
	}
	if parsed := ParseSessionUpdate(update); parsed.Retry != nil {
		r.retryMu.Lock()
		r.lastRetry = parsed.Retry
		r.retryMu.Unlock()
	}
}

// rpcSentence is the part of a JSON-RPC error worth showing a person: the
// error's message, or — when that is the generic "Internal error" — the reason
// the agent put in data. "" when there is nothing more specific than
// "Internal error".
func rpcSentence(err error) string {
	var rpcErr *acp.RPCError
	if !errors.As(err, &rpcErr) {
		return ""
	}
	generic := func(s string) bool {
		switch strings.ToLower(strings.TrimRight(strings.TrimSpace(s), ".")) {
		case "", "internal error", "error":
			return true
		}
		return false
	}
	// opencode words it "Internal error: <reason>" (measured against 2.0.32 with
	// a provider answering 503): the prefix is JSON-RPC's code, not information.
	message := strings.TrimSpace(strings.TrimPrefix(oneLine(rpcErr.Message), "Internal error:"))
	if !generic(message) {
		return clipRunes(message, 300)
	}
	if len(rpcErr.Data) > 0 {
		var text string
		if json.Unmarshal(rpcErr.Data, &text) == nil && !generic(text) {
			return clipRunes(oneLine(text), 300)
		}
		var fields struct {
			Details string `json:"details"`
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(rpcErr.Data, &fields) == nil {
			for _, candidate := range []string{fields.Details, fields.Message, fields.Error} {
				if !generic(candidate) {
					return clipRunes(oneLine(candidate), 300)
				}
			}
		}
	}
	return ""
}

func (r *acpRuntime) Cancel(ctx context.Context) error {
	r.mu.Lock()
	conn := r.conn
	sid := r.acpSessionID
	killer := r.killer
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil
	}

	// Best-effort ACP cancel, then OS shutdown ladder on the sandbox supervisor PID.
	if conn != nil && sid != "" {
		_ = conn.Notify(context.Background(), "session/cancel", map[string]any{
			"sessionId": sid,
		})
	}
	if killer != nil {
		cfg := acp.ShutdownConfig{
			CancelWait: 5 * time.Second,
			TermWait:   5 * time.Second,
		}
		// Honor caller ctx for overall cancel, but Shutdown escalates even if cancelled.
		_ = ctx
		return acp.Shutdown(context.Background(), killer, cfg)
	}
	return nil
}

func (r *acpRuntime) Close(ctx context.Context) error {
	_ = ctx
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.closing = true
	cancel := r.cancelServe
	conn := r.conn
	proc := r.proc
	done := r.serveDone
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	if proc != nil {
		_ = proc.Kill()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

// acpConnHandler bridges agent→client ACP methods onto RuntimeHandlers.
type acpConnHandler struct {
	rt              *acpRuntime
	onUnknownMethod func(method string, params json.RawMessage)
}

func (h *acpConnHandler) RequestPermission(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	handlers := h.rt.handlersSnapshot()
	if handlers.OnPermission == nil {
		return nil, ErrPermissionDenied
	}
	if before := h.rt.hooks.PermissionBefore; before != nil {
		rewritten, answer := before(ctx, params)
		if answer != nil {
			return answer, nil
		}
		if rewritten != nil {
			params = rewritten
		}
	}
	answer, err := handlers.OnPermission(ctx, params)
	if err == nil && h.rt.hooks.PermissionAfter != nil {
		answer = h.rt.hooks.PermissionAfter(answer)
	}
	return answer, err
}

// Elicit implements acp.ElicitationHandler, so Conn answers elicitation/create
// off its read loop.
//
// A runtime with no OnElicit declines: the person skipped. Never an error — the
// vendor would read that as a failure to present the question and abort the
// tool — and never a wait, because nothing would ever end it.
func (h *acpConnHandler) Elicit(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	handlers := h.rt.handlersSnapshot()
	if handlers.OnElicit == nil {
		return append(json.RawMessage(nil), declinedElicitation...), nil
	}
	return handlers.OnElicit(ctx, params)
}

func (h *acpConnHandler) SessionUpdate(ctx context.Context, params json.RawMessage) error {
	_ = ctx
	var p acp.SessionUpdateParams
	if err := json.Unmarshal(params, &p); err != nil {
		// Forward raw blob if shape is unexpected.
		if on := h.rt.handlersSnapshot().OnUpdate; on != nil {
			on(params, nil)
		}
		return nil
	}
	update := p.Update
	// SetMode can run before SetHandlers on creation and resume. Observe its
	// own notifications even then; only forwarding depends on a handler.
	h.rt.mu.Lock()
	sid := h.rt.acpSessionID
	h.rt.mu.Unlock()
	if sid != "" && (p.SessionID == "" || p.SessionID == sid) && !h.rt.observeModeUpdate(update) {
		return nil
	}
	handlers := h.rt.handlersSnapshot()
	if handlers.OnUpdate == nil {
		return nil
	}
	h.rt.noteRetry(update)
	if p.SessionID != "" && p.SessionID != sid {
		update = markOrigin(update, p.SessionID)
	}
	handlers.OnUpdate(update, p.Meta)
	return nil
}

// markOrigin tags an update that came from a session other than the runtime's
// own. claude-agent-acp streams a subagent's messages on the same connection
// under the subagent's sessionId; without the tag they are recorded exactly
// like the parent's, and a rewind that picked one of their message ids would
// fork at a message the parent thread does not contain. An update that is not
// a JSON object is passed on unchanged.
func markOrigin(update json.RawMessage, origin string) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(update, &fields); err != nil || fields == nil {
		return update
	}
	tag, err := json.Marshal(map[string]string{"origin": origin})
	if err != nil {
		return update
	}
	fields[DevKitWireKey] = tag
	out, err := json.Marshal(fields)
	if err != nil {
		return update
	}
	return out
}

// UnknownMethod refuses agent→client methods this build does not implement,
// and — unlike before — says so somewhere a person can see.
//
// The refusal itself is correct: answering fs/read_text_file with a made-up
// result would be worse than an error. What was wrong is that the error went
// only to the agent, so an ACP surface DevKit is specified to serve could have
// been requested on every turn since Phase 4 without leaving a trace.
func (h *acpConnHandler) UnknownMethod(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	_ = ctx
	if _, known := knownUnservedNotifications[method]; known {
		return nil, fmt.Errorf("unsupported method %q", method)
	}
	if h.onUnknownMethod != nil && markMethodSeen(method) {
		h.onUnknownMethod(method, params)
	}
	return nil, fmt.Errorf("unsupported method %q", method)
}

// knownUnservedNotifications are methods DevKit has looked at and decided not
// to act on, so they are not reported as unknown. A notification's error goes
// nowhere; the entry only keeps the "unimplemented ACP method" log for things
// nobody has looked at yet.
//
//   - _auth/status_update (claude-agent-acp 0.75+): the adapter re-checks the
//     sign-in in the background and pushes the result. DevKit learns a lapsed
//     sign-in from the error the next session/new or prompt returns
//     (ErrAgentAuthRequired), which is when it can do something about it.
var knownUnservedNotifications = map[string]struct{}{
	"_auth/status_update": {},
}

// unknownMethodSeen dedupes the report. Process-wide rather than per session:
// the question these answer is "does this vendor use this method at all", and
// the second occurrence adds nothing to it.
var unknownMethodSeen sync.Map

func markMethodSeen(method string) bool {
	_, loaded := unknownMethodSeen.LoadOrStore(method, struct{}{})
	return !loaded
}

// parseAgentExtensions reads the two extensions DevKit drives from the agent's
// initialize answer: steering (inject a message into the running turn) and
// goal (an objective the session works towards).
//
// Both are advertised in the top-level _meta rather than in agentCapabilities,
// because both are extensions rather than ACP proper. The goal's own control
// method comes with it instead of being assumed.
func parseAgentExtensions(meta json.RawMessage) (steering bool, goalControl string) {
	if len(meta) == 0 {
		return false, ""
	}
	var parsed struct {
		Steering struct {
			Supported bool `json:"supported"`
		} `json:"steering"`
		Goal struct {
			ControlMethod string   `json:"controlMethod"`
			Actions       []string `json:"actions"`
		} `json:"goal"`
	}
	if err := json.Unmarshal(meta, &parsed); err != nil {
		return false, ""
	}
	control := parsed.Goal.ControlMethod
	if control != "" && !slices.Contains(parsed.Goal.Actions, "set") {
		// Advertised without the action DevKit would use: treat it as absent
		// rather than calling a method the agent will refuse.
		control = ""
	}
	return parsed.Steering.Supported, control
}

// Steer injects a message into the turn that is running, instead of queueing
// it behind that turn. The agent's own answer says what became of it:
// "injected" into the running turn, "startedNewTurn" when it had already
// finished, or "promptRequired" — the opt-in DevKit asks for — which means the
// session was idle and the message is still the caller's to send.
func (r *acpRuntime) Steer(ctx context.Context, prompt []acp.ContentBlock) (string, error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return "", ErrSessionNotFound
	}
	conn, sid, supported := r.conn, r.acpSessionID, r.steering
	r.mu.Unlock()
	if !supported {
		return "", ErrUnsupported
	}

	var res struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	// idleBehavior promptRequired: without it an idle session starts a turn of
	// its own that nothing here owns — no turn record, no checkpoint, and the
	// composer would show nothing running while it ran.
	req := map[string]any{
		"sessionId": sid,
		"prompt":    prompt,
		"_meta":     map[string]any{"steering": map[string]any{"idleBehavior": "promptRequired"}},
	}
	if err := conn.Call(ctx, "_session/steering", req, &res); err != nil {
		return "", err
	}
	return res.Outcome, nil
}

// SetGoal sets or clears the session's objective through the goal extension.
// An empty objective clears it.
func (r *acpRuntime) SetGoal(ctx context.Context, objective string) error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	conn, sid, control := r.conn, r.acpSessionID, r.goalControl
	r.mu.Unlock()
	if control == "" {
		return ErrUnsupported
	}

	req := map[string]any{"sessionId": sid, "action": "clear"}
	if objective != "" {
		req = map[string]any{"sessionId": sid, "action": "set", "objective": objective}
	}
	var res struct{}
	return conn.Call(ctx, control, req, &res)
}
