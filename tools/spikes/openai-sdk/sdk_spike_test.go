package sdkspike

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

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

const dummyToken = "dummy-spike-token"

func testClient(t *testing.T, server *httptest.Server, extras ...option.RequestOption) openai.Client {
	t.Helper()
	opts := []option.RequestOption{
		option.WithAPIKey(dummyToken),
		option.WithBaseURL(server.URL + "/v1"),
		option.WithHTTPClient(server.Client()),
		option.WithMaxRetries(0),
		option.WithUnsafeAllowHTTP(), // Test-only: allows the client to use the local httptest HTTP server.
	}
	opts = append(opts, extras...)
	return openai.NewClient(opts...)
}

func basicResponse() string {
	return `{"id":"resp_fixture","object":"response","created_at":1,"status":"completed","model":"fixture-model","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
}

func TestRetriesDisabledAndExplicitBearerRequestPath(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+dummyToken {
			t.Error("Authorization header missing or incorrect")
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"fixture error","type":"server_error"}}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	_, err := client.Responses.New(context.Background(), responses.ResponseNewParams{Model: "fixture-model", Input: responses.ResponseNewParamsInputUnion{OfString: param.NewOpt("fixture")}})
	if err == nil {
		t.Fatal("expected fixture 500")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("request count = %d, want exactly 1", got)
	}
}

func TestAPIErrorCapturesStatusCodeCodeRequestIDAndRetryAfter(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("x-request-id", "req-fixture-1")
				w.Header().Set("Retry-After", "17")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"fixture response","type":"fixture_error","code":"fixture_code","param":"fixture_param"}}`)
			}))
			defer server.Close()
			client := testClient(t, server)
			_, err := client.Responses.New(context.Background(), responses.ResponseNewParams{Model: "fixture-model", Input: responses.ResponseNewParamsInputUnion{OfString: param.NewOpt("fixture")}})
			var apiErr *openai.Error
			if !errors.As(err, &apiErr) {
				t.Fatal("expected SDK API error")
			}
			if apiErr.StatusCode != status || apiErr.Code != "fixture_code" || apiErr.Type != "fixture_error" {
				t.Fatal("SDK API error fields did not match fixture")
			}
			if apiErr.Response == nil || apiErr.Response.Header.Get("x-request-id") != "req-fixture-1" || apiErr.Response.Header.Get("Retry-After") != "17" {
				t.Fatal("request ID or Retry-After response header was not preserved")
			}
			if calls.Load() != 1 {
				t.Fatalf("request count = %d, want 1", calls.Load())
			}
		})
	}
}

func TestExplicitCredentialOverridesEnvironment(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "ambient-dummy-do-not-use")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+dummyToken {
			t.Error("Authorization header missing or incorrect")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, basicResponse())
	}))
	defer server.Close()
	client := testClient(t, server)
	_, err := client.Responses.New(context.Background(), responses.ResponseNewParams{Model: "fixture-model", Input: responses.ResponseNewParamsInputUnion{OfString: param.NewOpt("fixture")}})
	if err != nil {
		t.Fatal("fixture request failed")
	}
}

func TestRawResponseHeaderCapture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Fixture-Request", "fixture-id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, basicResponse())
	}))
	defer server.Close()
	client := testClient(t, server)
	var raw *http.Response
	_, err := client.Responses.New(context.Background(), responses.ResponseNewParams{Model: "fixture-model", Input: responses.ResponseNewParamsInputUnion{OfString: param.NewOpt("fixture")}}, option.WithResponseInto(&raw))
	if err != nil {
		t.Fatal("fixture request failed")
	}
	if raw == nil || raw.Header.Get("X-Fixture-Request") != "fixture-id" {
		t.Fatal("raw response header was not captured")
	}
}

func TestRequestCancellationAndTimeout(t *testing.T) {
	started := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-time.After(250 * time.Millisecond):
		}
	}))
	defer server.Close()
	client := testClient(t, server, option.WithRequestTimeout(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.Responses.New(ctx, responses.ResponseNewParams{Model: "fixture-model", Input: responses.ResponseNewParamsInputUnion{OfString: param.NewOpt("fixture")}})
		result <- err
	}()
	select {
	case <-started:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("cancel fixture handler was not reached")
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled request unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop request")
	}
	_, err := client.Responses.New(context.Background(), responses.ResponseNewParams{Model: "fixture-model", Input: responses.ResponseNewParamsInputUnion{OfString: param.NewOpt("fixture")}})
	if err == nil {
		t.Fatal("timed out request unexpectedly succeeded")
	}
}

func TestStreamingPreservesPartialAndTerminalEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/responses" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if strings.TrimSpace(r.Header.Get("Accept")) == "" {
			t.Error("missing Accept header")
		}
		if r.Header.Get("OpenAI-Beta") != "fixture=v1" {
			t.Errorf("OpenAI-Beta header = %q", r.Header.Get("OpenAI-Beta"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error("decode streaming request")
		}
		if request["stream"] != true {
			t.Errorf("stream field = %#v", request["stream"])
		}
		if request["store"] != false {
			t.Errorf("store field = %#v", request["store"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"frag\",\"sequence_number\":1}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":{"+strings.TrimPrefix(basicResponse(), "{")+"}\n\n")
	}))
	defer server.Close()
	client := testClient(t, server)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{Model: "fixture-model", Input: responses.ResponseNewParamsInputUnion{OfString: param.NewOpt("fixture")}, Store: param.NewOpt(false)}, option.WithHeader("OpenAI-Beta", "fixture=v1"))
	var types []string
	var delta string
	for stream.Next() {
		event := stream.Current()
		types = append(types, event.Type)
		if event.Type == "response.output_text.delta" {
			delta = event.Delta
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal("stream decode failed")
	}
	if strings.Join(types, ",") != "response.output_text.delta,response.completed" {
		t.Fatalf("events = %v", types)
	}
	if delta != "frag" {
		t.Fatalf("partial delta = %q", delta)
	}
}

func TestStreamingFailureAndTruncationAreNotCompletion(t *testing.T) {
	fixtures := map[string]string{
		"failed":    "event: response.failed\ndata: {\"type\":\"response.failed\",\"sequence_number\":1,\"response\":{" + strings.TrimPrefix(strings.Replace(basicResponse(), `"status":"completed"`, `"status":"failed"`, 1), "{") + "}\n\n",
		"truncated": "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\",\"sequence_number\":1}\n\n",
	}
	for name, body := range fixtures {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			client := testClient(t, server)
			stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{Model: "fixture-model", Input: responses.ResponseNewParamsInputUnion{OfString: param.NewOpt("fixture")}})
			completed, failed, failedStatus, partial := false, false, "", ""
			for stream.Next() {
				event := stream.Current()
				if event.Type == "response.completed" {
					completed = true
				}
				if event.Type == "response.failed" {
					failed = true
					failedStatus = string(event.Response.Status)
				}
				if event.Type == "response.output_text.delta" {
					partial += event.Delta
				}
			}
			if err := stream.Err(); err != nil {
				t.Fatal("fixture stream decode failed")
			}
			if completed {
				t.Fatal("non-complete fixture was treated as completed")
			}
			switch name {
			case "failed":
				if !failed || failedStatus != "failed" {
					t.Fatal("response.failed event/status was not preserved")
				}
			case "truncated":
				if partial != "partial" {
					t.Fatal("partial delta was not preserved before stream end")
				}
			}
		})
	}
}

func TestEncryptedReasoningRoundTripsWithStoreFalse(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Error("decode request fixture")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, basicResponse())
	}))
	defer server.Close()
	var item responses.ResponseReasoningItem
	if err := json.Unmarshal([]byte(`{"id":"rs_fixture","type":"reasoning","summary":[],"encrypted_content":"opaque-fixture-ciphertext","status":"completed"}`), &item); err != nil {
		t.Fatal("decode reasoning fixture")
	}
	inputItem := item.ToParam()
	input := responses.ResponseNewParamsInputUnion{OfInputItemList: responses.ResponseInputParam{responses.ResponseInputItemUnionParam{OfReasoning: &inputItem}}}
	client := testClient(t, server)
	_, err := client.Responses.New(context.Background(), responses.ResponseNewParams{Model: "fixture-model", Input: input, Store: param.NewOpt(false), Include: []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent}})
	if err != nil {
		t.Fatal("fixture request failed")
	}
	if captured["store"] != false {
		t.Fatalf("store field = %#v", captured["store"])
	}
	inputJSON, _ := json.Marshal(captured["input"])
	if !strings.Contains(string(inputJSON), `"encrypted_content":"opaque-fixture-ciphertext"`) {
		t.Fatalf("encrypted content did not round-trip in JSON: %s", inputJSON)
	}
	if !strings.Contains(string(mustJSON(t, captured["include"])), `reasoning.encrypted_content`) {
		t.Fatal("encrypted reasoning include missing")
	}
}

func TestPromptCacheKeyAndAdditionalToolsSerialize(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tools []responses.ToolUnionParam
	}{
		{name: "empty", tools: []responses.ToolUnionParam{}},
		{name: "approved_function", tools: []responses.ToolUnionParam{responses.ToolParamOfFunction("fixture_lookup", map[string]any{"type": "object", "properties": map[string]any{}}, false)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&captured)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, basicResponse())
			}))
			defer server.Close()
			additional := responses.ResponseInputItemParamOfAdditionalTools(tc.tools)
			input := responses.ResponseNewParamsInputUnion{OfInputItemList: responses.ResponseInputParam{additional}}
			client := testClient(t, server)
			_, err := client.Responses.New(context.Background(), responses.ResponseNewParams{Model: "fixture-model", Input: input, PromptCacheKey: param.NewOpt("stable-fixture-key"), Store: param.NewOpt(false)})
			if err != nil {
				t.Fatal("fixture request failed")
			}
			if captured["prompt_cache_key"] != "stable-fixture-key" {
				t.Fatal("prompt_cache_key missing")
			}
			items, ok := captured["input"].([]any)
			if !ok || len(items) != 1 {
				t.Fatal("input item missing")
			}
			item, ok := items[0].(map[string]any)
			if !ok || item["type"] != "additional_tools" || item["role"] != "developer" {
				t.Fatal("additional_tools item fields missing")
			}
			serializedTools, ok := item["tools"].([]any)
			if !ok || len(serializedTools) != len(tc.tools) {
				t.Fatal("additional_tools list did not round-trip")
			}
			if tc.name == "approved_function" {
				tool, ok := serializedTools[0].(map[string]any)
				if !ok || tool["type"] != "function" || tool["name"] != "fixture_lookup" {
					t.Fatal("function tool replacement did not round-trip")
				}
			}
		})
	}
}

func TestModelsListUsesV1ModelsAndDetectsSIWCShapeMismatch(t *testing.T) {
	var path string
	authorized := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authorized = r.Header.Get("Authorization") == "Bearer "+dummyToken
		w.Header().Set("Content-Type", "application/json")
		// SIWC's documented catalog shape, deliberately unlike the generic SDK Model page.
		_, _ = io.WriteString(w, `{"models":[{"slug":"fixture-model","display_name":"Fixture","visibility":"list"}]}`)
	}))
	defer server.Close()
	client := testClient(t, server)
	page, err := client.Models.List(context.Background())
	if err != nil {
		t.Fatal("fixture request failed")
	}
	if path != "/v1/models" {
		t.Fatalf("path = %q", path)
	}
	if !authorized {
		t.Fatal("Authorization header missing or incorrect")
	}
	if len(page.Data) != 0 {
		t.Fatalf("unexpected generic data parsed from SIWC catalog: %#v", page.Data)
	}
	var fixture map[string]any
	_ = json.Unmarshal([]byte(`{"models":[{"slug":"fixture-model","display_name":"Fixture","visibility":"list"}]}`), &fixture)
	if len(fixture["models"].([]any)) != 1 {
		t.Fatal("bad SIWC fixture")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal("marshal fixture")
	}
	return b
}
