package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

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
	Attachments []string
	// Images are transient pasted/drop images. Data is base64 and is never
	// persisted in the session transcript or credential store.
	Images          []ImageAttachment
	CallerSessionID string // optional; used for CheckCycle on cross-session invoke
}

type ImageAttachment struct {
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
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
	// OnTurnFinished fires after the turn's final transcript flush, including
	// cancellation and errors, so shell consumers can clear running state once.
	OnTurnFinished func(sessionID string, status SessionStatus, outcome string)
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
	s := &Service{
		reg:       reg,
		store:     store,
		recorder:  NewRecorder(store),
		runtimes:  make(map[string]SessionRuntime),
		endpoints: make(map[string]func()),
		invoking:  make(map[string]struct{}),
		emit:      emit,
	}
	// A cadence flush has no caller to return its error to; route it to the
	// same hook Record errors take, read at call time because callers set
	// OnRecordError after NewService.
	s.recorder.OnWriteError = func(sessionID string, err error) {
		if s.OnRecordError != nil {
			s.OnRecordError(sessionID, err)
		}
	}
	return s
}

// Shutdown lands every session's buffered transcript and stops the recorder.
// The app calls it on quit, before the database closes: the recorder batches
// its writes, so without it a quit mid-stream lost the last cadence of text
// and left buffered tool calls "in progress" on reload.
func (s *Service) Shutdown(ctx context.Context) error {
	return s.recorder.FlushAll(ctx)
}

// RecoverStaleSessions resets sessions whose row still claims a turn is in
// flight (running / awaiting_permission). No ACP process survives the restart
// that runs this, so those rows describe turns that can never finish — and
// because Invoke refuses them with ErrSessionBusy before it ever reaches the
// resume path, a session left mid-turn was stuck forever. Idle is the truth:
// the transcript keeps whatever the turn managed to say, and the next prompt
// resumes the session on a fresh process.
//
// Startup-only by contract, mirroring outbox.RecoverStaleSyncing: calling it
// while turns are actually running would reset live sessions.
func (s *Service) RecoverStaleSessions(ctx context.Context) error {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return err
	}
	for _, sess := range list {
		if sess.Status != SessionStatusRunning && sess.Status != SessionStatusAwaitingPermission {
			continue
		}
		sess.Status = SessionStatusIdle
		sess.UpdatedAt = time.Now().UTC()
		if err := s.store.UpdateSession(ctx, sess); err != nil {
			return err
		}
	}
	return nil
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

const maxContextSuggestions = 100

// ListContextFiles returns a small, deterministic set of files below the
// session project. It is deliberately not a repository listing: the composer
// needs useful nearby suggestions, while the native picker remains the escape
// hatch for an exact path.
func (s *Service) ListContextFiles(ctx context.Context, id, query string) ([]ContextFile, error) {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return nil, err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	root := filepath.Clean(sess.ProjectPath)
	needle := strings.ToLower(strings.TrimSpace(query))
	paths := make([]string, 0, maxContextSuggestions)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (name == ".git" || name == "node_modules" || name == "vendor" || name == "target" || name == "dist" || name == "build") {
				return filepath.SkipDir
			}
			return nil
		}
		if len(paths) >= maxContextSuggestions {
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if needle != "" && !strings.Contains(strings.ToLower(rel), needle) {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]ContextFile, 0, len(paths))
	for _, rel := range paths {
		out = append(out, ContextFile{Path: filepath.Join(root, filepath.FromSlash(rel)), Name: filepath.Base(rel), Description: rel, Kind: "file"})
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

// ToolRawPreviewLimit is how much of a tool call's raw fields a transcript
// page carries. One huge terminal dump used to ride with every page load —
// the full output stays in the row and ToolCallDetail serves it.
const ToolRawPreviewLimit = 8 << 10

// TranscriptPage is the transcript read, bounded: the newest window of a
// session rather than the whole of it. A session with months of history used
// to pay for all of it on every open; the UI opens on the last page and walks
// backwards with beforeSeq from the page it holds.
func (s *Service) TranscriptPage(ctx context.Context, sessionID string, beforeSeq int64, limit int) (TranscriptPage, error) {
	if err := s.authorizeTarget(ctx, sessionID); err != nil {
		return TranscriptPage{}, err
	}
	if limit <= 0 {
		limit = DefaultTranscriptPageSize
	}
	if limit > MaxTranscriptPageSize {
		limit = MaxTranscriptPageSize
	}
	page, err := s.store.ListTranscriptPage(ctx, sessionID, beforeSeq, limit)
	if err != nil {
		return TranscriptPage{}, err
	}
	page.Entries = previewToolPayloads(page.Entries)
	return page, nil
}

// previewToolPayloads bounds what a page carries for each tool row: rawInput,
// rawOutput and text content are cut to ToolRawPreviewLimit, a diff too large
// to preview keeps only its path, and Raw is dropped outright — for tool rows
// it is a copy of those same fields, and rows written before the parser
// stopped keeping it would otherwise ship it whole. Anything cut marks the
// call truncated; ToolCallDetail still serves the row intact.
func previewToolPayloads(entries []TranscriptEntry) []TranscriptEntry {
	for i, entry := range entries {
		if entry.ToolCallID == "" || len(entry.Payload) == 0 {
			continue
		}
		var stored SessionUpdate
		if err := json.Unmarshal(entry.Payload, &stored); err != nil || stored.ToolCall == nil {
			continue
		}
		call := stored.ToolCall
		changed := len(stored.Raw) > 0
		stored.Raw = nil
		var inCut, outCut bool
		call.RawInput, inCut = previewRaw(call.RawInput)
		call.RawOutput, outCut = previewRaw(call.RawOutput)
		if contentCut := previewToolContent(call.Content); inCut || outCut || contentCut {
			call.Truncated = true
			changed = true
		}
		if !changed {
			continue
		}
		payload, err := json.Marshal(stored)
		if err != nil {
			continue
		}
		entries[i].Payload = payload
	}
	return entries
}

// previewRaw passes small values through untouched and turns big ones into a
// JSON string of their leading bytes — the field is vendor-shaped JSON of any
// type, and a byte slice of it would not parse. The string is encoded by
// encoding/json from a cut that ends on a rune boundary: strconv.Quote, used
// before, writes \xNN and \U escapes JSON does not have, so a cut through a
// multi-byte character failed the page marshal and sent the row out whole.
// The encoded preview, quotes and escapes included, fits the limit.
func previewRaw(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) <= ToolRawPreviewLimit {
		return raw, false
	}
	text := string(raw)
	n := ToolRawPreviewLimit
	for {
		encoded, _ := json.Marshal(truncateUTF8(text, n))
		if len(encoded) <= ToolRawPreviewLimit {
			return encoded, true
		}
		// Escapes grew it past the limit (HTML-escaped <>&, control
		// characters): shrink the cut in proportion. n strictly decreases,
		// and an empty cut encodes to two bytes, so this ends.
		n = n * ToolRawPreviewLimit / len(encoded)
	}
}

// previewToolContent shortens oversized content items in place and reports
// whether it cut anything. A diff is never cut inside — half an oldText against
// half a newText renders as a change nobody made — so one too large to preview
// keeps its path and is marked truncated; an image's data cannot be half-shown
// either and is dropped the same way.
func previewToolContent(content []ToolCallContent) bool {
	cut := false
	for i := range content {
		item := &content[i]
		if item.Type == "diff" {
			size := len(item.NewText)
			if item.OldText != nil {
				size += len(*item.OldText)
			}
			if size > ToolRawPreviewLimit {
				item.OldText, item.NewText, item.Truncated = nil, "", true
				cut = true
			}
			continue
		}
		block := item.Content
		if block == nil {
			continue
		}
		if len(block.Text) > ToolRawPreviewLimit {
			block.Text = truncateUTF8(block.Text, ToolRawPreviewLimit)
			item.Truncated = true
		}
		if len(block.Data) > ToolRawPreviewLimit {
			block.Data = ""
			item.Truncated = true
		}
		if block.Resource != nil && len(block.Resource.Text) > ToolRawPreviewLimit {
			block.Resource.Text = truncateUTF8(block.Resource.Text, ToolRawPreviewLimit)
			item.Truncated = true
		}
		if block.Resource != nil && len(block.Resource.Blob) > ToolRawPreviewLimit {
			block.Resource.Blob = ""
			item.Truncated = true
		}
		cut = cut || item.Truncated
	}
	return cut
}

// truncateUTF8 returns at most n bytes of s, backed off to the start of the rune
// the cut would split.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for i := 0; i < utf8.UTFMax-1 && n > 0 && !utf8.RuneStart(s[n]); i++ {
		n--
	}
	return s[:n]
}

// ToolCallDetail returns the stored row of one tool call, payload intact. The
// page reader shortens oversized raw fields to a bounded preview so one huge
// terminal dump cannot make a page heavy; this is where the full output still
// lives for the surface that asked to see it.
func (s *Service) ToolCallDetail(ctx context.Context, sessionID, toolCallID string) (TranscriptEntry, error) {
	if err := s.authorizeTarget(ctx, sessionID); err != nil {
		return TranscriptEntry{}, err
	}
	entry, ok, err := s.store.FindTranscriptToolCall(ctx, sessionID, toolCallID)
	if err != nil {
		return TranscriptEntry{}, err
	}
	if !ok {
		return TranscriptEntry{}, ErrTranscriptEntryNotFound
	}
	return entry, nil
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

	// The conversation must be complete on disk before the session becomes
	// history: closing is the one path after which no turn will ever flush.
	if err := s.recorder.Flush(ctx, id); err != nil {
		return err
	}

	if rt != nil {
		if err := rt.Close(ctx); err != nil {
			return err
		}
	}
	if closeEndpoint != nil {
		closeEndpoint()
	}
	// With the runtime gone nothing records for this session any more; land
	// whatever arrived during the close and drop its write state.
	if err := s.recorder.Forget(ctx, id); err != nil {
		return err
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

// RenameSession changes the human-authored title without touching the vendor
// runtime or its transcript.
func (s *Service) RenameSession(ctx context.Context, id, title string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 120 {
		return fmt.Errorf("%w: title must be between 1 and 120 characters", ErrInvalidSessionTitle)
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	sess.Title = &title
	sess.UpdatedAt = time.Now().UTC()
	return s.store.UpdateSession(ctx, sess)
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
	rt := s.liveRuntime(ctx, sessionID)

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
	s.mu.Unlock()
	rt := s.liveRuntime(ctx, in.SessionID)

	if rt == nil {
		// An orphan: the row outlived the process that served it, which is
		// what every restart does to every open session — and what a mid-turn
		// crash does too, since liveRuntime evicts the corpse. This used to
		// be the end of the conversation — ErrUnavailable, and the only way
		// forward was to start again somewhere else and lose the context.
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
	turnOutcome := "error"
	defer func() {
		s.mu.Lock()
		delete(s.invoking, in.SessionID)
		s.mu.Unlock()
		// The turn is over however it went: land the tail the recorder is
		// still holding. WithoutCancel because the error paths reach here on
		// a context that is already gone — the transcript of a failed turn is
		// exactly the one worth keeping.
		if err := s.recorder.Flush(context.WithoutCancel(ctx), in.SessionID); err != nil && s.OnRecordError != nil {
			s.OnRecordError(in.SessionID, err)
		}
		if s.OnTurnFinished != nil {
			if session, err := s.store.GetSession(context.WithoutCancel(ctx), in.SessionID); err == nil {
				s.OnTurnFinished(in.SessionID, session.Status, turnOutcome)
			}
		}
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
			// Emit before persisting: the screen must never wait on SQLite,
			// and the recorder batches its writes anyway — a reload lands
			// within one flush cadence of what the live event said.
			//
			// recordCtx, not ctx: the turn's context is cancelled the moment
			// the turn goes wrong, and the update that says *why* is the last
			// one to arrive. Writing it under ctx meant the one message worth
			// keeping was the one guaranteed to be dropped — the log read
			// "transcript write failed: context canceled" and the UI drew an
			// empty bubble where the agent's error should have been.
			s.emit(in.SessionID, update)
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
	prompt, err := buildPromptWithImages(in.Prompt, in.Attachments, in.Images, sess.ProjectPath, callerIsAgent(invoker))
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
	turnOutcome = "completed"

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

// liveRuntime returns the session's runtime unless its process has died, in
// which case the corpse is evicted — runtime and MCP endpoint both — and nil
// is reported, which sends Invoke down the resume path.
//
// Only CloseSession ever removed a runtime before, so a session whose agent
// crashed kept answering from the map forever: Retry prompted the dead process
// and failed with the crash's error, every time. Dead() is an optional
// interface (RuntimeLiveness) because an in-process runtime cannot die this
// way; one that cannot say simply stays.
func (s *Service) liveRuntime(ctx context.Context, id string) SessionRuntime {
	s.mu.Lock()
	rt := s.runtimes[id]
	liveness, canDie := rt.(RuntimeLiveness)
	dead := false
	if canDie {
		dead, _ = liveness.Dead()
	}
	if !dead {
		s.mu.Unlock()
		return rt
	}
	delete(s.runtimes, id)
	closeEndpoint := s.endpoints[id]
	delete(s.endpoints, id)
	s.mu.Unlock()

	// Outside the lock, same shape as CloseSession: Close parks the process
	// and the endpoint close tears down a socket; neither belongs under s.mu.
	_ = rt.Close(ctx)
	if closeEndpoint != nil {
		closeEndpoint()
	}
	return nil
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
	rt := s.liveRuntime(ctx, id)
	if rt == nil {
		return ErrUnavailable
	}
	return rt.Cancel(ctx)
}

// SetMode changes the live agent's mode without routing it through prompt.
func (s *Service) SetMode(ctx context.Context, id, modeID string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	rt := s.liveRuntime(ctx, id)
	configurator, ok := rt.(RuntimeConfigurator)
	if !ok {
		return fmt.Errorf("%w: agent %q does not support session mode", ErrUnsupported, sess.AgentType)
	}
	return configurator.SetMode(ctx, modeID)
}

// SetConfigOption changes one of the live agent's advertised settings.
func (s *Service) SetConfigOption(ctx context.Context, id, optionID, value string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	rt := s.liveRuntime(ctx, id)
	configurator, ok := rt.(RuntimeConfigurator)
	if !ok {
		return fmt.Errorf("%w: agent %q does not support config option", ErrUnsupported, sess.AgentType)
	}
	return configurator.SetConfigOption(ctx, optionID, value)
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
