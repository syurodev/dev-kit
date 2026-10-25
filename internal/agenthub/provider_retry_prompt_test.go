package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"devkit/internal/acp"
)

// promptAgent runs a runtime against an agent that reads one session/prompt
// and answers with reply (nil = never answers).
func promptAgent(t *testing.T, prepare func(rt *acpRuntime), reply func(id any) *acp.Response) ([]json.RawMessage, error) {
	t.Helper()
	client, agent := net.Pipe()
	t.Cleanup(func() { client.Close(); agent.Close() })

	rt := &acpRuntime{hooks: acpRuntimeHooks{Type: AgentTypeOpenCode}, acpSessionID: "acp-1", serveDone: make(chan struct{})}
	var (
		mu      sync.Mutex
		updates []json.RawMessage
	)
	rt.handlers.OnUpdate = func(update json.RawMessage, _ json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
	}
	rt.conn = acp.NewConn(client, silentHandler{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	go rt.conn.Serve(ctx)
	if prepare != nil {
		prepare(rt)
	}

	go func() {
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			return
		}
		var id any
		_ = json.Unmarshal(msg.ID, &id)
		if response := reply(id); response != nil {
			_ = acp.WriteMessage(agent, *response)
		}
	}()

	_, err := rt.Prompt(ctx, []acp.ContentBlock{{Type: "text", Text: "go"}}, nil)
	mu.Lock()
	defer mu.Unlock()
	return append([]json.RawMessage(nil), updates...), err
}

func rpcFailure(code int, message, data string) func(any) *acp.Response {
	return func(id any) *acp.Response {
		e := &acp.RPCError{Code: code, Message: message}
		if data != "" {
			e.Data = json.RawMessage(data)
		}
		return &acp.Response{JSONRPC: "2.0", ID: id, Error: e}
	}
}

// opencode's provider failed, it retried, and its prompt then failed. The
// process is fine: that is a turn it ended, not a crash, and the sentence is
// the vendor's.
func TestPromptRefusedByALiveAgentIsATurnFailureWithItsSentence(t *testing.T) {
	updates, err := promptAgent(t, nil, rpcFailure(-32603, "Upstream request failed: Endpoint is unavailable.", ""))
	if !errors.Is(err, ErrTurnFailed) || errors.Is(err, ErrProcessFailed) {
		t.Fatalf("err = %v, want ErrTurnFailed and not ErrProcessFailed", err)
	}
	if got := ErrorVendorMessage(err); got != "Upstream request failed: Endpoint is unavailable." {
		t.Fatalf("vendor message = %q", got)
	}
	if got := ErrorVendor(err); got != AgentTypeOpenCode {
		t.Fatalf("vendor = %q", got)
	}
	if got := PreviewFromError(err); got != "Upstream request failed: Endpoint is unavailable." {
		t.Fatalf("preview = %q", got)
	}
	// The banner is said in the session before the error goes back.
	if len(updates) != 1 {
		t.Fatalf("got %d updates, want the failure banner", len(updates))
	}
	failure := ParseSessionUpdate(updates[0]).Failure
	if failure == nil || failure.Category != "turn_failed" || failure.Details != "Upstream request failed: Endpoint is unavailable." || len(failure.Actions) != 1 || failure.Actions[0] != "retry" {
		t.Fatalf("failure = %+v", failure)
	}
}

// A bare "Internal error" says nothing; the provider's own words, shown while
// the agent retried, are the better sentence.
func TestPromptFailureUsesWhatTheProviderSaidWhileRetrying(t *testing.T) {
	// Fed the way the agent does: a notice before the answer, through the
	// connection's update handler.
	client, agent := net.Pipe()
	t.Cleanup(func() { client.Close(); agent.Close() })
	rt := &acpRuntime{hooks: acpRuntimeHooks{Type: AgentTypeOpenCode}, acpSessionID: "acp-1", serveDone: make(chan struct{})}
	rt.conn = acp.NewConn(client, &acpConnHandler{rt: rt})
	rt.handlers.OnUpdate = func(json.RawMessage, json.RawMessage) {}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	go rt.conn.Serve(ctx)
	go func() {
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			return
		}
		var id any
		_ = json.Unmarshal(msg.ID, &id)
		_ = acp.WriteMessage(agent, acp.Notification{JSONRPC: "2.0", Method: "session/update", Params: json.RawMessage(
			`{"sessionId":"acp-1","update":{"sessionUpdate":"session_info_update","_meta":{"opencode/retry":{"attempt":11,"nextRetryAt":"2026-10-18T06:28:00.642Z","error":{"type":"provider.internal","message":"Upstream request failed: Endpoint is unavailable.","status":503}}}}}`)})
		_ = acp.WriteMessage(agent, acp.Response{JSONRPC: "2.0", ID: id, Error: &acp.RPCError{Code: -32603, Message: "Internal error"}})
	}()

	_, err := rt.Prompt(ctx, []acp.ContentBlock{{Type: "text", Text: "go"}}, nil)
	if !errors.Is(err, ErrTurnFailed) {
		t.Fatalf("err = %v", err)
	}
	if got := PreviewFromError(err); got != "Upstream request failed: Endpoint is unavailable (503)" {
		t.Fatalf("preview = %q", got)
	}
}

// The error opencode 2.0.32 answered when its provider kept returning 503 and
// its eleven attempts were spent (recorded against the real binary).
func TestPromptFailureOfOpenCodeAfterItsRetries(t *testing.T) {
	_, err := promptAgent(t, nil, rpcFailure(-32603, "Internal error: Upstream request failed: Endpoint is unavailable.", `{"service":"session","errorName":"provider.internal"}`))
	if !errors.Is(err, ErrTurnFailed) || errors.Is(err, ErrProcessFailed) {
		t.Fatalf("err = %v", err)
	}
	if got := PreviewFromError(err); got != "Upstream request failed: Endpoint is unavailable." {
		t.Fatalf("preview = %q", got)
	}
}

func TestPromptFailureReadsTheReasonFromData(t *testing.T) {
	_, err := promptAgent(t, nil, rpcFailure(-32603, "Internal error", `{"details":"The context window is full."}`))
	if !errors.Is(err, ErrTurnFailed) || ErrorVendorMessage(err) != "The context window is full." {
		t.Fatalf("err = %v (%q)", err, ErrorVendorMessage(err))
	}
	// Nothing more specific than "Internal error": a turn failure without a
	// sentence, so the preview names the kind rather than echoing it.
	_, err = promptAgent(t, nil, rpcFailure(-32603, "Internal error", ""))
	if !errors.Is(err, ErrTurnFailed) || ErrorVendorMessage(err) != "" {
		t.Fatalf("err = %v (%q)", err, ErrorVendorMessage(err))
	}
	if got := PreviewFromError(err); got != "The agent could not finish this turn." {
		t.Fatalf("preview = %q", got)
	}
}

// The process leaving is still a process failure: a hung-up pipe, and an
// error response that arrives after the serve loop has ended.
func TestPromptOnAProcessThatLeftIsStillAProcessFailure(t *testing.T) {
	// The pipe to the process is gone: the request cannot even be written.
	updates, err := promptAgent(t, func(rt *acpRuntime) {
		rt.conn.Close()
	}, func(any) *acp.Response { return nil })
	if !errors.Is(err, ErrProcessFailed) || errors.Is(err, ErrTurnFailed) {
		t.Fatalf("pipe closed: err = %v, want ErrProcessFailed", err)
	}
	if len(updates) != 0 {
		t.Fatalf("a crash is not a vendor failure banner: %v", updates)
	}

	_, err = promptAgent(t, func(rt *acpRuntime) { close(rt.serveDone) }, rpcFailure(-32603, "Internal error", ""))
	if !errors.Is(err, ErrProcessFailed) || errors.Is(err, ErrTurnFailed) {
		t.Fatalf("serve loop ended: err = %v, want ErrProcessFailed", err)
	}
}

// Claude's expired login arrives the same way and stays a sign-in.
func TestPromptAuthFailureIsStillASignIn(t *testing.T) {
	_, err := promptAgent(t, nil, rpcFailure(-32603, "Internal error", `{"errorKind":"authentication_failed"}`))
	if !errors.Is(err, ErrAgentAuthRequired) || errors.Is(err, ErrTurnFailed) {
		t.Fatalf("err = %v, want ErrAgentAuthRequired", err)
	}
}

func TestClaudeTypedPromptFailureFromResultMetaIsATurnRefusal(t *testing.T) {
	meta := `{"jetbrains":{"air":{"sessionFailure":{"id":"limit-1","revision":3,"category":"limit","severity":"error","title":"You've hit your session limit. resets 9:40pm","actions":[]}}}}`
	updates, err := promptAgent(t, func(rt *acpRuntime) {
		rt.hooks = acpRuntimeHooks{Type: AgentTypeClaude, PromptFailure: claudePromptFailure}
	}, func(id any) *acp.Response {
		return &acp.Response{JSONRPC: "2.0", ID: id, Result: json.RawMessage(`{"stopReason":"end_turn","_meta":` + meta + `}`)}
	})
	if !errors.Is(err, ErrAgentUsageLimit) || errors.Is(err, ErrProcessFailed) {
		t.Fatalf("err = %v, want a usage-limit refusal", err)
	}
	if got := ErrorVendorMessage(err); got != "You've hit your session limit. resets 9:40pm" {
		t.Fatalf("vendor message = %q", got)
	}
	stop := StopFromError(err, time.Now(), false)
	if stop.Kind != StopUsageLimit || stop.RetryAfter != nil {
		t.Fatalf("stop = %+v", stop)
	}
	if len(updates) != 1 {
		t.Fatalf("updates = %d, want one failure update", len(updates))
	}
	got := ParseSessionUpdate(updates[0]).Failure
	if got == nil || got.ID != "limit-1" || got.Title != "You've hit your session limit. resets 9:40pm" {
		t.Fatalf("failure = %+v", got)
	}
}

func TestClaudeTypedFailureUsesRejectedUsageResetAndDeduplicatesNotification(t *testing.T) {
	client, agent := net.Pipe()
	t.Cleanup(func() { client.Close(); agent.Close() })
	rt := &acpRuntime{hooks: acpRuntimeHooks{Type: AgentTypeClaude, PromptFailure: claudePromptFailure}, acpSessionID: "acp-1", serveDone: make(chan struct{})}
	var mu sync.Mutex
	var updates []json.RawMessage
	rt.handlers.OnUpdate = func(update json.RawMessage, _ json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		updates = append(updates, update)
	}
	rt.conn = acp.NewConn(client, &acpConnHandler{rt: rt})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	go rt.conn.Serve(ctx)
	reset := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	weeklyReset := reset.Add(5 * 24 * time.Hour)
	failureMeta := `{"jetbrains":{"air":{"sessionFailure":{"id":"limit-2","revision":4,"category":"limit","severity":"error","title":"You've hit your session limit. resets 9:40pm"}}}}`
	go func() {
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			return
		}
		var id any
		_ = json.Unmarshal(msg.ID, &id)
		_ = acp.WriteMessage(agent, acp.Notification{JSONRPC: "2.0", Method: "session/update", Params: json.RawMessage(`{"sessionId":"acp-1","update":{"sessionUpdate":"usage_update","used":1,"size":2,"_meta":{"_claude/rateLimit":{"status":"rejected","resetsAt":` + fmt.Sprint(reset.Unix()) + `},"_devkit":{"limits":[{"label":"5-hour","usedPercent":100,"resetsAt":` + fmt.Sprint(reset.Unix()) + `,"status":"reached"},{"label":"Weekly","usedPercent":20,"resetsAt":` + fmt.Sprint(weeklyReset.Unix()) + `,"status":"reached"}]}}}}`)})
		_ = acp.WriteMessage(agent, acp.Notification{JSONRPC: "2.0", Method: "session/update", Params: json.RawMessage(`{"sessionId":"acp-1","update":{"sessionUpdate":"session_info_update","_meta":` + failureMeta + `}}`)})
		_ = acp.WriteMessage(agent, acp.Response{JSONRPC: "2.0", ID: id, Result: json.RawMessage(`{"stopReason":"end_turn","_meta":` + failureMeta + `}`)})
	}()
	_, err := rt.Prompt(ctx, []acp.ContentBlock{{Type: "text", Text: "go"}}, nil)
	if !errors.Is(err, ErrAgentUsageLimit) {
		t.Fatalf("Prompt err = %v", err)
	}
	stop := StopFromError(err, time.Now(), false)
	if stop.RetryAfter == nil || !stop.RetryAfter.Equal(reset) {
		t.Fatalf("stop = %+v, want reset %s", stop, reset)
	}
	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, update := range updates {
		if ParseSessionUpdate(update).Failure != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("failure updates = %d, want one; updates=%s", count, updates)
	}
}

func TestRejectedUsageResetFallbackUsesBlockingWindowAndAllowedSnapshotClearsIt(t *testing.T) {
	now := time.Now()
	short := now.Add(time.Hour).Truncate(time.Second)
	long := now.Add(5 * 24 * time.Hour).Truncate(time.Second)
	rt := &acpRuntime{}
	rt.noteRejectedUsageLimit(&Usage{
		RateLimit: &RateLimit{Status: "rejected"},
		Limits: []LimitWindow{
			{Label: "5-hour", UsedPercent: 100, ResetsAt: short.Unix(), Status: "reached"},
			{Label: "Weekly", UsedPercent: 20, ResetsAt: long.Unix(), Status: "reached"},
		},
	})
	if rt.limitRetryAfter == nil || !rt.limitRetryAfter.Equal(short.UTC()) {
		t.Fatalf("fallback reset = %v, want blocking window %s", rt.limitRetryAfter, short.UTC())
	}
	rt.noteRejectedUsageLimit(&Usage{RateLimit: &RateLimit{Status: "allowed"}})
	if rt.limitRetryAfter != nil {
		t.Fatalf("allowed usage snapshot left stale reset %s", rt.limitRetryAfter)
	}
}

type claudePipeAdapter struct {
	runtime *acpRuntime
	usage   *ClaudeAdapter
}

func (a *claudePipeAdapter) Type() AgentType { return AgentTypeClaude }
func (a *claudePipeAdapter) Open(context.Context, OpenSpec) (SessionRuntime, error) {
	return a.runtime, nil
}
func (a *claudePipeAdapter) NormalizeUsage(meta json.RawMessage) (NormalizedUsage, error) {
	return a.usage.NormalizeUsage(meta)
}
func (a *claudePipeAdapter) NormalizeToolSummary(meta, update json.RawMessage) ([]string, string, error) {
	return a.usage.NormalizeToolSummary(meta, update)
}
func (a *claudePipeAdapter) Extensions() []string { return nil }

func TestClaudeTypedFailureACPResultFlowsThroughServiceWithUsage(t *testing.T) {
	store := newInternalStore(t)
	client, agent := net.Pipe()
	t.Cleanup(func() { client.Close(); agent.Close() })
	rt := &acpRuntime{hooks: acpRuntimeHooks{Type: AgentTypeClaude, PromptFailure: claudePromptFailure}, acpSessionID: "acp-service", serveDone: make(chan struct{})}
	rt.conn = acp.NewConn(client, &acpConnHandler{rt: rt})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	go rt.conn.Serve(ctx)
	failureMeta := `{"jetbrains":{"air":{"sessionFailure":{"id":"limit-service","revision":1,"category":"limit","severity":"error","title":"You've hit your session limit. resets 9:40pm"}}},"quota":{"token_count":{"totalTokens":155,"inputTokens":100,"cachedInputTokens":30,"cachedWriteTokens":5,"outputTokens":20,"reasoningOutputTokens":4},"model_usage":[{"model":"claude-opus-5-5","token_count":{"totalTokens":155,"inputTokens":100,"cachedInputTokens":30,"cachedWriteTokens":5,"outputTokens":20,"reasoningOutputTokens":4}}]}}`
	go func() {
		msg, err := acp.ReadMessage(agent)
		if err != nil {
			return
		}
		var id any
		_ = json.Unmarshal(msg.ID, &id)
		_ = acp.WriteMessage(agent, acp.Response{JSONRPC: "2.0", ID: id, Result: json.RawMessage(`{"stopReason":"end_turn","_meta":` + failureMeta + `}`)})
	}()
	reg := NewRegistry()
	reg.Register(&claudePipeAdapter{runtime: rt, usage: NewClaudeAdapter(ClaudeOptions{})})
	svc := NewService(reg, store, nil)
	session, err := svc.CreateSession(ctx, CreateInput{AgentType: AgentTypeClaude, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Invoke(ctx, InvokeInput{SessionID: session.ID, Prompt: "go"})
	if !errors.Is(err, ErrAgentUsageLimit) {
		t.Fatalf("Invoke err = %v", err)
	}
	got, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SessionStatusIdle || got.Stop == nil || got.Stop.Kind != StopUsageLimit {
		t.Fatalf("session = status %s stop %+v; want idle usage_limit", got.Status, got.Stop)
	}
	turns, err := store.ListTurns(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || string(turns[0].RawMeta) != failureMeta || turns[0].StopReason != "failed" {
		t.Fatalf("turns = %+v", turns)
	}
	var usage NormalizedUsage
	if err := json.Unmarshal(turns[0].UsageJSON, &usage); err != nil {
		t.Fatal(err)
	}
	if intValue(usage.TotalTokens) != 155 || usage.InputTokens != 100 || usage.OutputTokens != 20 || intValue(usage.CachedInputTokens) != 30 || intValue(usage.CachedWriteTokens) != 5 || intValue(usage.ReasoningOutputTokens) != 4 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestClaudeWarningOrClearedPromptFailureDoesNotFailTurn(t *testing.T) {
	for _, meta := range []string{
		`{"jetbrains":{"air":{"sessionFailure":{"id":"notice","revision":1,"category":"limit","severity":"warning","title":"Approaching your limit"}}}}`,
		`{"jetbrains":{"air":{"sessionFailure":null}}}`,
	} {
		t.Run(meta, func(t *testing.T) {
			updates, err := promptAgent(t, func(rt *acpRuntime) {
				rt.hooks = acpRuntimeHooks{Type: AgentTypeClaude, PromptFailure: claudePromptFailure}
			}, func(id any) *acp.Response {
				return &acp.Response{JSONRPC: "2.0", ID: id, Result: json.RawMessage(`{"stopReason":"end_turn","_meta":` + meta + `}`)}
			})
			if err != nil || len(updates) != 0 {
				t.Fatalf("updates=%v err=%v; warning/clear should not fail", updates, err)
			}
		})
	}
}
