package spike

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

const fakeCredential = "sk-ant-test-only-never-a-real-key"

func newTestClient(t *testing.T, baseURL string, httpClient *http.Client) anthropic.Client {
	t.Helper()
	return anthropic.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithoutOpenTelemetry(),
		option.WithAPIKey(fakeCredential),
		option.WithBaseURL(baseURL),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
		option.WithHeaderAdd("anthropic-beta", "prompt-caching-2024-07-31"),
	)
}

func requestParams() anthropic.MessageNewParams {
	cache := anthropic.NewCacheControlEphemeralParam()
	return anthropic.MessageNewParams{
		Model:     "claude-test-model",
		MaxTokens: 1200,
		System: []anthropic.TextBlockParam{{
			Text:         "synthetic system context",
			CacheControl: cache,
		}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("synthetic prompt")),
		},
		Thinking: anthropic.ThinkingConfigParamOfEnabled(1024),
	}
}

func TestRequestOptionsAndRawStreamEventFacts(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "environment-key-must-not-win")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Injected: environment-value")
	var capturedHeader http.Header
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("request-id", "req_synthetic_01")
		w.Header().Set("anthropic-ratelimit-requests-remaining", "17")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-test-model\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":11,\"output_tokens\":1,\"cache_read_input_tokens\":7,\"cache_creation_input_tokens\":3}}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"synthetic thought\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig-test\"}}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":9,\"cache_read_input_tokens\":7,\"cache_creation_input_tokens\":3}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, server.Client())
	var rawResponse *http.Response
	stream := client.Messages.NewStreaming(context.Background(), requestParams(), option.WithResponseInto(&rawResponse))
	defer stream.Close()
	assertResponseHeaders := func(when string) {
		t.Helper()
		if rawResponse == nil || rawResponse.StatusCode != http.StatusOK || rawResponse.Header.Get("request-id") != "req_synthetic_01" || rawResponse.Header.Get("anthropic-ratelimit-requests-remaining") != "17" {
			t.Fatalf("WithResponseInto did not preserve successful stream response status and headers %s stream consumption", when)
		}
	}
	assertResponseHeaders("before")
	var got []anthropic.MessageStreamEventUnion
	for stream.Next() {
		got = append(got, stream.Current())
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("events=%d, want 5", len(got))
	}
	if got[2].RawJSON() == "" || !strings.Contains(got[2].RawJSON(), `"signature":"sig-test"`) {
		t.Fatalf("raw signature event not preserved: %s", got[2].RawJSON())
	}
	if got[2].Delta.Signature != "sig-test" || got[1].Delta.Thinking != "synthetic thought" {
		t.Fatalf("thinking/signature fields lost: thinking=%q signature=%q", got[1].Delta.Thinking, got[2].Delta.Signature)
	}
	if got[3].Delta.StopReason != "end_turn" || got[3].Usage.OutputTokens != 9 {
		t.Fatalf("terminal facts lost: stop=%q usage=%+v", got[3].Delta.StopReason, got[3].Usage)
	}
	if got[0].Message.Usage.InputTokens != 11 || got[0].Message.Usage.CacheReadInputTokens != 7 || got[0].Message.Usage.CacheCreationInputTokens != 3 {
		t.Fatalf("initial token/cache usage lost: %+v", got[0].Message.Usage)
	}
	if capturedHeader.Get("Authorization") != "Bearer "+fakeCredential || capturedHeader.Get("X-Api-Key") != "" {
		t.Fatal("explicit API key authorization header missing or mismatched")
	}
	if capturedHeader.Get("X-Injected") != "" {
		t.Fatal("environment custom header was applied despite WithoutEnvironmentDefaults")
	}
	if capturedHeader.Get("anthropic-beta") != "prompt-caching-2024-07-31" {
		t.Fatalf("beta header=%q", capturedHeader.Get("anthropic-beta"))
	}
	if capturedHeader.Get("request-id") != "" {
		t.Fatal("response-only request id unexpectedly appeared in request headers")
	}
	assertResponseHeaders("after")
	if body["stream"] != true || body["model"] != "claude-test-model" || body["max_tokens"] != float64(1200) {
		t.Fatalf("request body missing expected fields: %#v", body)
	}
	thinking, _ := body["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != float64(1024) {
		t.Fatalf("thinking request fields=%#v", thinking)
	}
	system, _ := body["system"].([]any)
	if len(system) != 1 || system[0].(map[string]any)["cache_control"] == nil {
		t.Fatalf("system cache marker missing: %#v", body["system"])
	}
}

func TestSignedThinkingAndToolTurnCanBeSerializedForContinuation(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode continuation request: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_next\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-test-model\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":11,\"output_tokens\":0}}}\n\n")
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, server.Client())
	params := requestParams()
	params.Messages = []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("continue the prior turn")),
		anthropic.NewAssistantMessage(
			anthropic.NewThinkingBlock("signature-issued-by-provider", "prior signed reasoning"),
			anthropic.NewToolUseBlock("toolu_local_1", map[string]any{"query": "example"}, "local.lookup"),
		),
		// API tool results are user-role messages following the assistant tool call.
		anthropic.NewUserMessage(anthropic.NewToolResultBlock("toolu_local_1", "synthetic result", false)),
	}
	stream := client.Messages.NewStreaming(context.Background(), params)
	defer stream.Close()
	for stream.Next() {
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("continuation stream: %v", err)
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages=%d, want 3", len(messages))
	}
	assistant := messages[1].(map[string]any)
	blocks := assistant["content"].([]any)
	if blocks[0].(map[string]any)["signature"] != "signature-issued-by-provider" || blocks[1].(map[string]any)["type"] != "tool_use" {
		t.Fatalf("signed thinking/tool turn was not serialized intact: %#v", assistant)
	}
	result := messages[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if result["tool_use_id"] != "toolu_local_1" || result["type"] != "tool_result" {
		t.Fatalf("tool result link was not preserved: %#v", result)
	}
}

func TestSDKRetryDisabledAndRateHeadersRemainAvailable(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("request-id", "req_rate_limit")
		w.Header().Set("retry-after", "3")
		w.Header().Set("anthropic-ratelimit-requests-reset", "2026-10-31T12:00:00Z")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"synthetic limit"}}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, server.Client())
	stream := client.Messages.NewStreaming(context.Background(), requestParams())
	defer stream.Close()
	for stream.Next() {
	}
	var apiErr *anthropic.Error
	if !errors.As(stream.Err(), &apiErr) {
		t.Fatalf("error=%T %v, want *anthropic.Error", stream.Err(), stream.Err())
	}
	if calls.Load() != 1 {
		t.Fatalf("request count=%d; SDK retry was expected to be disabled", calls.Load())
	}
	if apiErr.StatusCode != http.StatusTooManyRequests || apiErr.RequestID != "req_rate_limit" {
		t.Fatalf("status/request id lost: status=%d id=%q", apiErr.StatusCode, apiErr.RequestID)
	}
	if apiErr.Response.Header.Get("retry-after") != "3" || apiErr.Response.Header.Get("anthropic-ratelimit-requests-reset") == "" {
		t.Fatalf("response rate headers unavailable: %#v", apiErr.Response.Header)
	}
}

func TestPartialStreamThenProviderErrorRemainsObservable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("request-id", "req_partial")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_partial\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-test-model\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial text\"}}\n\n")
		_, _ = io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"synthetic overload\"}}\n\n")
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, server.Client())
	stream := client.Messages.NewStreaming(context.Background(), requestParams())
	defer stream.Close()
	var partial bool
	for stream.Next() {
		if stream.Current().Delta.Text == "partial text" {
			partial = true
		}
	}
	var apiErr *anthropic.Error
	if !partial || !errors.As(stream.Err(), &apiErr) || apiErr.Type() != anthropic.ErrorTypeOverloadedError {
		t.Fatalf("partial=%t error=%T %v", partial, stream.Err(), stream.Err())
	}
	if apiErr.RequestID != "req_partial" || apiErr.StatusCode != http.StatusOK {
		t.Fatalf("SSE error response facts lost: status=%d requestID=%q", apiErr.StatusCode, apiErr.RequestID)
	}
}

func TestContextCancellationAndTimeoutStopInFlightRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, func())
		want error
	}{
		{name: "cancel", ctx: func() (context.Context, func()) {
			ctx, cancel := context.WithCancel(context.Background())
			return ctx, cancel
		}, want: context.Canceled},
		{name: "deadline", ctx: func() (context.Context, func()) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			return ctx, cancel
		}, want: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := make(chan struct{})
			httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				close(started)
				<-req.Context().Done()
				return nil, req.Context().Err()
			})}
			client := newTestClient(t, "https://anthropic.invalid/", httpClient)
			ctx, cancel := tc.ctx()
			defer cancel()
			result := make(chan error, 1)
			go func() {
				stream := client.Messages.NewStreaming(ctx, requestParams())
				defer stream.Close()
				for stream.Next() {
				}
				result <- stream.Err()
			}()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("request did not reach synthetic transport")
			}
			if tc.name == "cancel" {
				cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, tc.want) {
					t.Fatalf("stream error=%v, want %v", err, tc.want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("context end did not stop SDK call")
			}
		})
	}
}

func TestCleanEOFWithoutTerminalEventNeedsAdapterDetection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_truncated\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-test-model\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":5,\"output_tokens\":0}}}\n\n")
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, server.Client())
	stream := client.Messages.NewStreaming(context.Background(), requestParams())
	defer stream.Close()
	var sawStop bool
	for stream.Next() {
		if stream.Current().Type == "message_stop" {
			sawStop = true
		}
	}
	if stream.Err() != nil || sawStop {
		t.Fatalf("SDK result: stop=%t err=%v; expected clean EOF without terminal marker", sawStop, stream.Err())
	}
	// Contract implication: the adapter must track terminal events itself;
	// stream.Err()==nil alone is not proof that a response completed.
}

func TestTransportFailureIsSurfacedWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	clientHTTP := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("synthetic transport failure")
	})}
	client := newTestClient(t, "https://anthropic.invalid/", clientHTTP)
	stream := client.Messages.NewStreaming(context.Background(), requestParams())
	defer stream.Close()
	for stream.Next() {
	}
	if stream.Err() == nil || calls.Load() != 1 {
		t.Fatalf("stream error=%v, transport calls=%d", stream.Err(), calls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestProviderHostedToolsRejectedBeforeRequest(t *testing.T) {
	clientTool := anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
		Name:        "local.lookup",
		Type:        anthropic.ToolTypeCustom,
		InputSchema: anthropic.ToolInputSchemaParam{Type: "object"},
	}}
	if _, err := clientOnlyTools([]anthropic.ToolUnionParam{clientTool}); err != nil {
		t.Fatalf("ordinary client tool rejected: %v", err)
	}
	providerTool := anthropic.ToolUnionParam{OfWebSearchTool20250305: &anthropic.WebSearchTool20250305Param{}}
	if _, err := clientOnlyTools([]anthropic.ToolUnionParam{providerTool}); err == nil {
		t.Fatal("SDK provider-hosted tool variant was accepted")
	}
}

// The SDK exposes many server/hosted tool variants in ToolUnionParam and does
// not provide a client-wide disable switch. This narrow guard is the exact
// adapter-side policy the SDK alone cannot supply.
func clientOnlyTools(in []anthropic.ToolUnionParam) ([]anthropic.ToolUnionParam, error) {
	out := make([]anthropic.ToolUnionParam, 0, len(in))
	for _, tool := range in {
		if tool.OfTool == nil || tool.OfTool.Type != anthropic.ToolTypeCustom {
			return nil, errors.New("only custom client tools are permitted")
		}
		out = append(out, tool)
	}
	return out, nil
}
