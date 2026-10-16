package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"devkit/internal/acp"
)

var mockSessionSeq atomic.Uint64

// MockAdapter is an in-process VendorAdapter (no sandbox, no real CLI).
type MockAdapter struct {
	mu          sync.Mutex
	lastSpec    OpenSpec
	lastRuntime *mockRuntime
	// forks records every branch asked for, and forkErr fails the next one.
	// A rewind is a sequence whose steps have to unwind correctly when a later
	// one fails, and the fork is the first step that can fail for real reasons
	// — so a test needs to be able to make it.
	forks     []MockFork
	forkErr   error
	deleted   []string
	deleteErr error
	// noSteer makes every runtime refuse a mid-turn message, like a vendor that
	// cannot take one (SetSteering).
	noSteer bool
}

// SetSteering turns mid-turn messages on (the default) or off for runtimes
// opened afterwards, so a test can ask for the vendor that cannot steer.
func (a *MockAdapter) SetSteering(on bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.noSteer = !on
}

// MockFork is one branch the mock was asked for.
type MockFork struct {
	SourceACPSessionID string
	MessageID          string
	NewACPSessionID    string
}

// NewMockAdapter returns the built-in mock vendor adapter.
func NewMockAdapter() *MockAdapter {
	return &MockAdapter{}
}

func (a *MockAdapter) Type() AgentType { return AgentTypeMock }

// Fork satisfies RuntimeForker.
//
// Deliberately without a Load beside it: two tests pin that a vendor with no
// SessionLoader stays unavailable to a session whose process died, and the mock
// is what they use. Forking and resuming are separate vendor facts, and this
// keeps them separate here too.
func (a *MockAdapter) Fork(ctx context.Context, spec OpenSpec, srcACPSessionID, messageID string) (SessionRuntime, error) {
	a.mu.Lock()
	failWith := a.forkErr
	a.forkErr = nil
	a.mu.Unlock()
	if failWith != nil {
		return nil, failWith
	}
	rt, err := a.Open(ctx, spec)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.forks = append(a.forks, MockFork{
		SourceACPSessionID: srcACPSessionID,
		MessageID:          messageID,
		NewACPSessionID:    rt.ACPSessionID(),
	})
	a.mu.Unlock()
	return rt, nil
}

// FailNextFork makes the next Fork fail, so a test can watch the steps taken
// before it unwind.
func (a *MockAdapter) FailNextFork(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.forkErr = err
}

// Forks returns every branch the mock was asked for.
func (a *MockAdapter) Forks() []MockFork {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]MockFork(nil), a.forks...)
}

// Deleted returns the vendor sessions the mock was told to destroy.
func (a *MockAdapter) Deleted() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.deleted...)
}

func (a *MockAdapter) noteDeleted(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleted = append(a.deleted, id)
	return a.deleteErr
}

func (a *MockAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.lastSpec = spec
	a.mu.Unlock()
	id := fmt.Sprintf("mock-acp-%d", mockSessionSeq.Add(1))
	a.mu.Lock()
	runtime := &mockRuntime{acpSessionID: id, adapter: a, noSteer: a.noSteer}
	a.lastRuntime = runtime
	a.mu.Unlock()
	return runtime, nil
}

// LastSpec returns the spec of the most recent Open (tests).
func (a *MockAdapter) LastSpec() OpenSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastSpec
}

// LastRuntime returns the most recently opened runtime (tests only).
func (a *MockAdapter) LastRuntime() *mockRuntime {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastRuntime
}

type mockMeta struct {
	CostUSD      float64 `json:"costUsd"`
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
}

type mockMetaRoot struct {
	Mock mockMeta `json:"mock"`
}

func (a *MockAdapter) NormalizeUsage(meta json.RawMessage) (NormalizedUsage, error) {
	out := NormalizedUsage{RawMeta: append(json.RawMessage(nil), meta...)}
	if len(meta) == 0 {
		return out, nil
	}
	var root mockMetaRoot
	if err := json.Unmarshal(meta, &root); err != nil {
		return out, err
	}
	out.CostUSD = root.Mock.CostUSD
	out.InputTokens = root.Mock.InputTokens
	out.OutputTokens = root.Mock.OutputTokens
	return out, nil
}

func (a *MockAdapter) NormalizeToolSummary(meta json.RawMessage, update json.RawMessage) ([]string, string, error) {
	_ = meta
	if len(update) == 0 {
		return nil, "mock reply", nil
	}
	return nil, "mock reply", nil
}

func (a *MockAdapter) Extensions() []string { return nil }

type mockRuntime struct {
	acpSessionID string
	// adapter is set so a runtime can report its own deletion back to the
	// adapter the test holds.
	adapter    *MockAdapter
	mu         sync.Mutex
	handlers   RuntimeHandlers
	closed     bool
	cancelCh   chan struct{}
	setMode    string
	setOptions map[string]string
	// promptMetas is every `_meta` a prompt carried, in order. The mock is what
	// the service's tests run against, and what DevKit asks the vendor for on a
	// turn is part of the turn's contract.
	promptMetas []json.RawMessage
	// prompts is every prompt's content blocks, in order — what the vendor
	// would have been sent.
	prompts [][]acp.ContentBlock
	// turnRunning says a prompt is in flight, which is the only state in which
	// steering injects rather than reporting promptRequired — the same rule the
	// real agent applies.
	turnRunning bool
	noSteer     bool
	steered     []string
	goal        string
}

// Steer implements SessionSteerer the way the vendor does: into the running
// turn, or "promptRequired" when there is none.
func (r *mockRuntime) Steer(ctx context.Context, prompt []acp.ContentBlock) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", ErrSessionNotFound
	}
	if r.noSteer {
		return "", ErrUnsupported
	}
	if !r.turnRunning {
		return "promptRequired", nil
	}
	for _, block := range prompt {
		r.steered = append(r.steered, block.Text)
	}
	return "injected", nil
}

// SetGoal implements SessionSteerer; an empty objective clears.
func (r *mockRuntime) SetGoal(ctx context.Context, objective string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrSessionNotFound
	}
	r.goal = objective
	return nil
}

// InTurn says a prompt is being run: the turn is past the service's own set-up
// and Cancel will reach it. A session's status turns running earlier than that,
// so a test that cancels as soon as it reads "running" can cancel nothing at all.
func (r *mockRuntime) InTurn() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelCh != nil
}

// Steered is what the mock was told mid-turn, for the tests.
// Prompts is every prompt sent to the runtime, as content blocks.
func (r *mockRuntime) Prompts() [][]acp.ContentBlock {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]acp.ContentBlock(nil), r.prompts...)
}

func (r *mockRuntime) Steered() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.steered...)
}

// Goal is the objective the mock holds, for the tests.
func (r *mockRuntime) Goal() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.goal
}

func (r *mockRuntime) recordPromptMeta(meta json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.promptMetas = append(r.promptMetas, append(json.RawMessage(nil), meta...))
}

// Delete satisfies RuntimeDeleter: a rewind drops the vendor session it left
// behind, and a test needs to see that it did.
func (r *mockRuntime) Delete(ctx context.Context) error {
	return r.DeleteSession(ctx, r.acpSessionID)
}

// DeleteSession satisfies VendorSessionDeleter.
func (r *mockRuntime) DeleteSession(ctx context.Context, acpSessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.adapter == nil || acpSessionID == "" {
		return nil
	}
	return r.adapter.noteDeleted(acpSessionID)
}

// MockPromptMetas returns the `_meta` of every prompt a mock runtime has been
// sent. Exported for the service tests, which hold the runtime through the
// adapter rather than directly.
func MockPromptMetas(rt SessionRuntime) []json.RawMessage {
	// Tests wrap the runtime to hook a prompt; unwrap until the mock shows.
	for {
		if _, ok := rt.(*mockRuntime); ok {
			break
		}
		wrapper, ok := rt.(interface{ Unwrap() SessionRuntime })
		if !ok {
			return nil
		}
		rt = wrapper.Unwrap()
	}
	mock, ok := rt.(*mockRuntime)
	if !ok {
		return nil
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	return append([]json.RawMessage(nil), mock.promptMetas...)
}

func (r *mockRuntime) ACPSessionID() string { return r.acpSessionID }

func (r *mockRuntime) SetMode(ctx context.Context, modeID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	r.setMode = modeID
	h := r.handlers
	r.mu.Unlock()
	r.echo(h, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": modeID})
	return nil
}

// echo sends the update a real agent answers a settings call with, before the
// call returns — which is when a runtime resumed for that call has to be
// listening already.
func (r *mockRuntime) echo(h RuntimeHandlers, update map[string]any) {
	if h.OnUpdate == nil {
		return
	}
	if raw, err := json.Marshal(update); err == nil {
		h.OnUpdate(raw, nil)
	}
}

func (r *mockRuntime) SetConfigOption(ctx context.Context, optionID, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return ErrSessionNotFound
	}
	if r.setOptions == nil {
		r.setOptions = make(map[string]string)
	}
	r.setOptions[optionID] = value
	h := r.handlers
	r.mu.Unlock()
	r.echo(h, map[string]any{
		"sessionUpdate": "config_option_update",
		"configOptions": []map[string]any{{"id": optionID, "type": "select", "currentValue": value}},
	})
	return nil
}

func (r *mockRuntime) SetModeValue() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setMode
}

func (r *mockRuntime) SetOptionValue(optionID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setOptions[optionID]
}

func (r *mockRuntime) SetHandlers(h RuntimeHandlers) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = h
}

func (r *mockRuntime) Prompt(ctx context.Context, prompt []acp.ContentBlock, promptMeta json.RawMessage) (PromptOutcome, error) {
	// The mock's behaviour is driven by markers in the text; attachments are
	// carried but not interpreted, which is what a stand-in should do.
	text := PromptText(prompt)
	r.recordPromptMeta(promptMeta)
	r.mu.Lock()
	r.prompts = append(r.prompts, append([]acp.ContentBlock(nil), prompt...))
	r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return PromptOutcome{}, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return PromptOutcome{}, ErrSessionNotFound
	}
	r.turnRunning = true
	defer func() {
		r.mu.Lock()
		r.turnRunning = false
		r.mu.Unlock()
	}()
	h := r.handlers
	sid := r.acpSessionID
	r.cancelCh = make(chan struct{})
	cancelCh := r.cancelCh
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.cancelCh = nil
		r.mu.Unlock()
	}()

	if strings.Contains(text, "BLOCK") {
		select {
		case <-cancelCh:
			return PromptOutcome{}, ErrCancelled
		case <-ctx.Done():
			return PromptOutcome{}, ErrCancelled
		}
	}

	update, err := json.Marshal(map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"content": map[string]any{
			"type": "text",
			"text": "mock reply",
		},
	})
	if err != nil {
		return PromptOutcome{}, err
	}
	if h.OnUpdate != nil {
		h.OnUpdate(update, nil)
	}

	if strings.Contains(text, "NEED_PERMISSION") && h.OnPermission != nil {
		params, err := json.Marshal(map[string]any{
			"sessionId": sid,
			"options": []map[string]any{
				{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
				{"optionId": "deny", "name": "Deny", "kind": "reject_once"},
			},
			"toolCall": map[string]any{
				"toolCallId": "mock-tool-1",
				"title":      "Mock tool",
			},
		})
		if err != nil {
			return PromptOutcome{}, err
		}
		// A refusal ends the tool call, not the turn — which is what a real
		// vendor does with the cancelled outcome ACP hands it (see
		// acp.preferResult). This used to abort the prompt, so every test
		// about a denial was really testing a runtime that crashes on one.
		if _, err := h.OnPermission(ctx, params); err != nil && !errors.Is(err, ErrPermissionDenied) {
			return PromptOutcome{}, err
		}
		// The turn goes on after the answer, until stopped: what a test
		// needs to see the session's status while the agent is still working.
		if strings.Contains(text, "HOLD_AFTER") {
			select {
			case <-cancelCh:
				return PromptOutcome{}, ErrCancelled
			case <-ctx.Done():
				return PromptOutcome{}, ErrCancelled
			}
		}
	}

	// Cancellation may have raced in while we were awaiting permission.
	select {
	case <-cancelCh:
		return PromptOutcome{}, ErrCancelled
	default:
	}

	meta, err := json.Marshal(mockMetaRoot{
		Mock: mockMeta{CostUSD: 0.001, InputTokens: 1, OutputTokens: 1},
	})
	if err != nil {
		return PromptOutcome{}, err
	}
	return PromptOutcome{StopReason: "end_turn", Meta: meta}, nil
}

func (r *mockRuntime) Cancel(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelCh != nil {
		select {
		case <-r.cancelCh:
			// already cancelled
		default:
			close(r.cancelCh)
		}
	}
	return nil
}

func (r *mockRuntime) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}
