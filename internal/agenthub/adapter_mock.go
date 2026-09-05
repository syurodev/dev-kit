package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

var mockSessionSeq atomic.Uint64

// MockAdapter is an in-process VendorAdapter (no sandbox, no real CLI).
type MockAdapter struct {
	mu       sync.Mutex
	lastSpec OpenSpec
}

// NewMockAdapter returns the built-in mock vendor adapter.
func NewMockAdapter() *MockAdapter {
	return &MockAdapter{}
}

func (a *MockAdapter) Type() AgentType { return AgentTypeMock }

func (a *MockAdapter) Open(ctx context.Context, spec OpenSpec) (SessionRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.lastSpec = spec
	a.mu.Unlock()
	id := fmt.Sprintf("mock-acp-%d", mockSessionSeq.Add(1))
	return &mockRuntime{acpSessionID: id}, nil
}

// LastSpec returns the spec of the most recent Open (tests).
func (a *MockAdapter) LastSpec() OpenSpec {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastSpec
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
	mu           sync.Mutex
	handlers     RuntimeHandlers
	closed       bool
	cancelCh     chan struct{}
}

func (r *mockRuntime) ACPSessionID() string { return r.acpSessionID }

func (r *mockRuntime) SetHandlers(h RuntimeHandlers) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = h
}

func (r *mockRuntime) Prompt(ctx context.Context, text string) (PromptOutcome, error) {
	if err := ctx.Err(); err != nil {
		return PromptOutcome{}, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return PromptOutcome{}, ErrSessionNotFound
	}
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
		outcome, err := h.OnPermission(ctx, params)
		if err != nil || isDeniedPermissionOutcome(outcome) {
			if err == nil {
				err = ErrPermissionDenied
			}
			return PromptOutcome{}, err
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
