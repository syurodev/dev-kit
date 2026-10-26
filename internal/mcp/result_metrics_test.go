package mcp

import (
	"context"
	"devkit/internal/identity"
	"devkit/internal/usagemetrics"
	"errors"
	"fmt"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func clientForMetrics(t *testing.T, ms *sdk.Server) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := sdk.NewInMemoryTransports()
	ss, e := ms.Connect(ctx, st, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ss.Close() })
	cs, e := sdk.NewClient(&sdk.Implementation{Name: "metrics-test", Version: "0"}, nil).Connect(ctx, ct, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}
func TestMeasuredSDKPathCountsServedUTF8AndPreservesMixedResults(t *testing.T) {
	var mu sync.Mutex
	var observed []usagemetrics.Observation
	ms := sdk.NewServer(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	ms.AddReceivingMiddleware(withMeasuredResultBudget(4096, &ResultObservation{Scope: func() (usagemetrics.Scope, bool) {
		return usagemetrics.Scope{Vendor: "claude", WorkspaceID: "w1"}, true
	}, Record: func(o usagemetrics.Observation) { mu.Lock(); observed = append(observed, o); mu.Unlock() }}))
	sdk.AddTool(ms, &sdk.Tool{Name: "big.text"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: strings.Repeat("Việt ", 3000)}}}, nil, nil
	})
	sdk.AddTool(ms, &sdk.Tool{Name: "mixed"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "Việt"}, &sdk.ImageContent{Data: []byte("image bytes"), MIMEType: "image/png"}}}, nil, nil
	})
	sdk.AddTool(ms, &sdk.Tool{Name: "tool.error"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "refused"}}}, nil, nil
	})
	sdk.AddTool(ms, &sdk.Tool{Name: "handler.error"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
		return nil, nil, errors.New("handler refused")
	})
	cs := clientForMetrics(t, ms)
	for _, name := range []string{"big.text", "mixed", "tool.error", "handler.error", "not.registered"} {
		_, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name})
	}
	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 5 {
		t.Fatalf("observations %+v", observed)
	}
	a := observed[0]
	if a.Before != int64(len(strings.Repeat("Việt ", 3000))) || a.After > 4096 || !a.Cut || a.Exempt {
		t.Fatalf("cut %+v", a)
	}
	b := observed[1]
	if b.Before != int64(len("Việt")) || b.After != b.Before || !b.NonText || !b.Text || !b.Exempt || b.Cut {
		t.Fatalf("mixed %+v", b)
	}
	if !observed[2].ToolError || observed[2].HandlerError {
		t.Fatalf("tool error %+v", observed[2])
	}
	// The SDK maps handler errors to tool error results; dispatch errors remain handler errors.
	if !observed[3].ToolError && !observed[3].HandlerError {
		t.Fatalf("handler error %+v", observed[3])
	}
	if observed[4].Tool != "dispatch-error" || !observed[4].HandlerError {
		t.Fatalf("dispatch %+v", observed[4])
	}
}
func TestOnlyHubSocketCollectsAndScopeIsCapturedBeforeHandler(t *testing.T) {
	var recorded, dropped int
	active := true
	srv := NewServer(Deps{UsageScope: func(id, token string) (usagemetrics.Scope, bool) {
		return usagemetrics.Scope{Vendor: "mock", WorkspaceID: "before"}, active && id == "session" && token == "token"
	}, RecordUsage: func(o usagemetrics.Observation) {
		recorded++
		if o.Scope.WorkspaceID != "before" {
			t.Error("scope resolved after handler")
		}
	}, DropUsage: func() { dropped++ }})
	for _, tc := range []struct {
		caller identity.Caller
		token  string
		want   int
	}{{identity.UserTerminal(), "token", 0}, {identity.Agent("session"), "stale", 0}, {identity.Agent("session"), "token", 1}} {
		active = true
		ms := srv.sdkServerFor(tc.caller, tc.token)
		sdk.AddTool(ms, &sdk.Tool{Name: "snapshot.scope"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
			active = false
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ok"}}}, nil, nil
		})
		cs := clientForMetrics(t, ms)
		before := recorded
		if _, e := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "snapshot.scope"}); e != nil {
			t.Fatal(e)
		}
		if recorded-before != tc.want {
			t.Fatalf("caller %+v recorded %d", tc.caller, recorded-before)
		}
	}
	if dropped != 1 {
		t.Fatalf("stale endpoint gaps %d", dropped)
	}
}

func BenchmarkResultMeasurement(b *testing.B) {
	for _, kind := range []string{"small", "large-cut", "exempt-image"} {
		for _, measured := range []bool{false, true} {
			b.Run(fmt.Sprintf("%s/observer=%v", kind, measured), func(b *testing.B) {
				var obs *ResultObservation
				if measured {
					obs = &ResultObservation{Scope: func() (usagemetrics.Scope, bool) { return usagemetrics.Scope{Vendor: "claude"}, true }, Record: func(usagemetrics.Observation) {}}
				}
				text := "small result"
				if kind == "large-cut" {
					text = strings.Repeat("data ", 20000)
				}
				next := func(context.Context, string, sdk.Request) (sdk.Result, error) {
					content := []sdk.Content{&sdk.TextContent{Text: text}}
					if kind == "exempt-image" {
						content = append(content, &sdk.ImageContent{Data: []byte("image"), MIMEType: "image/png"})
					}
					return &sdk.CallToolResult{Content: content}, nil
				}
				handler := withMeasuredResultBudget(ResultBudget, obs)(next)
				req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "test.result"}}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _ = handler(context.Background(), "tools/call", req)
				}
			})
		}
	}
}

func TestStructuredSDKResultIdenticalWithAndWithoutObserver(t *testing.T) {
	var o usagemetrics.Observation
	call := func(observer *ResultObservation) *sdk.CallToolResult {
		ms := sdk.NewServer(&sdk.Implementation{Name: "t", Version: "0"}, nil)
		ms.AddReceivingMiddleware(withMeasuredResultBudget(ResultBudget, observer))
		sdk.AddTool(ms, &sdk.Tool{Name: "big.page"}, func(context.Context, *sdk.CallToolRequest, struct{}) (*sdk.CallToolResult, any, error) {
			return nil, bigSearchPage(300), nil
		})
		r, e := clientForMetrics(t, ms).CallTool(context.Background(), &sdk.CallToolParams{Name: "big.page"})
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	baseline := call(nil)
	measured := call(&ResultObservation{Scope: func() (usagemetrics.Scope, bool) { return usagemetrics.Scope{Vendor: "claude"}, true }, Record: func(v usagemetrics.Observation) { o = v }})
	if !reflect.DeepEqual(baseline, measured) {
		t.Fatal("observer changed SDK result")
	}
	if !o.Cut || o.After != int64(textOf(measured)) || o.Before <= o.After || o.NonText {
		t.Fatalf("structured measurement %+v", o)
	}
}
