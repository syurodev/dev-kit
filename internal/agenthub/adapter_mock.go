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
type MockAdapter struct{}

// NewMockAdapter returns the built-in mock vendor adapter.
func NewMockAdapter() *MockAdapter {
	return &MockAdapter{}
}

func (a *MockAdapter) Type() AgentType { return AgentTypeMock }

func (a *MockAdapter) Open(ctx context.Context, cwd string) (SessionRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = cwd
	id := fmt.Sprintf("mock-acp-%d", mockSessionSeq.Add(1))
	return &mockRuntime{acpSessionID: id}, nil
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
	_, _ = meta, update
	return nil, "", nil
}

func (a *MockAdapter) Extensions() []string { return nil }

type mockRuntime struct {
	acpSessionID string
	mu           sync.Mutex
	handlers     RuntimeHandlers
	closed       bool
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
	r.mu.Unlock()

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
		if _, err := h.OnPermission(ctx, params); err != nil {
			return PromptOutcome{}, err
		}
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
	return ctx.Err()
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
