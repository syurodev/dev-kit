package mcp

import (
	"context"
	"devkit/internal/usagemetrics"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"time"
)

// ResultObservation is deliberately value-only. No result or context escapes.
type ResultObservation struct {
	Begin  func() bool
	End    func()
	Scope  func() (usagemetrics.Scope, bool)
	Record func(usagemetrics.Observation)
	Drop   func()
}

func textSize(call *sdk.CallToolResult) (n int64, text, nonText bool) {
	for _, c := range call.Content {
		if v, ok := c.(*sdk.TextContent); ok {
			n += int64(len(v.Text))
			text = true
		} else {
			nonText = true
		}
	}
	if len(call.Content) == 0 && call.StructuredContent != nil {
		nonText = true
	}
	return
}
func contentChanged(a, b []sdk.Content) bool {
	if len(a) != len(b) {
		return true
	}
	for i, c := range a {
		av, aok := c.(*sdk.TextContent)
		bv, bok := b[i].(*sdk.TextContent)
		if aok != bok {
			return true
		}
		if aok && av.Text != bv.Text {
			return true
		}
	}
	return false
}
func withMeasuredResultBudget(budget int, observer *ResultObservation) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			obs := observer
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			if obs != nil && obs.Begin != nil {
				if !obs.Begin() {
					obs = nil
				} else if obs.End != nil {
					defer obs.End()
				}
			}
			var scope usagemetrics.Scope
			observed := false
			if obs != nil && obs.Scope != nil {
				scope, observed = obs.Scope()
			}
			at := time.Now().UTC()
			res, err := next(ctx, method, req)
			o := usagemetrics.Observation{Scope: scope, At: at, Tool: "dispatch-error", HandlerError: err != nil}
			call, ok := res.(*sdk.CallToolResult)
			if ok && call != nil {
				o.Result = true
				o.ToolError = call.IsError
				o.Before, o.Text, o.NonText = textSize(call)
				if r, ok := req.(*sdk.CallToolRequest); ok && r.Params != nil && err == nil {
					o.Tool = r.Params.Name
				}
				_, exempt := budgetExempt[o.Tool]
				o.Exempt = exempt || o.NonText
				before := call.Content
				if err == nil && !call.IsError && !exempt {
					limitResult(o.Tool, call, budget)
				}
				o.After, _, _ = textSize(call)
				o.Cut = contentChanged(before, call.Content)
			}
			if obs != nil {
				if observed && obs.Record != nil {
					obs.Record(o)
				} else if obs.Drop != nil {
					obs.Drop()
				}
			}
			return res, err
		}
	}
}
