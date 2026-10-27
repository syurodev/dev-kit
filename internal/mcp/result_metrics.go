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

// withMeasuredResultBudget is the one result middleware: it cuts an over-budget
// result and reports what it measured. With a serving holder it also takes the
// endpoint's policy once, at call entry, and uses that single snapshot for the
// group decision, the tool's budget and the W/S revisions the call is measured
// under; a policy published while the call runs does not change it. With none
// (the terminal server and legacy endpoints) it is the fixed-budget middleware
// it always was.
func withMeasuredResultBudget(budget int, observer *ResultObservation, serving *servingHolder) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			obs := observer
			if method == "tools/list" && serving != nil && !serving.listable() {
				// Paused for a policy change or closed: no list at all rather than
				// one built from a registry that is being rewritten.
				return nil, errToolListUnavailable
			}
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			var snap *servingSnapshot
			why := admitReady
			if serving != nil {
				snap, why = serving.admit()
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
			if observed && snap != nil {
				scope.ProfileRevision, scope.OverrideRevision = snap.eff.Revisions.Workspace, snap.eff.Revisions.Session
			}
			name, named := "", false
			if r, ok := req.(*sdk.CallToolRequest); ok && r.Params != nil {
				name, named = r.Params.Name, true
			}
			at := time.Now().UTC()
			var res sdk.Result
			var err error
			switch {
			case serving != nil && snap == nil && why == admitPaused:
				res = endpointPausedResult()
			case serving != nil && snap == nil:
				res = endpointClosedResult()
			case snap != nil && !snap.serves(name):
				res = groupOffResult(name)
			default:
				res, err = next(ctx, method, req)
			}
			o := usagemetrics.Observation{Scope: scope, At: at, Tool: "dispatch-error", HandlerError: err != nil}
			call, ok := res.(*sdk.CallToolResult)
			if ok && call != nil {
				o.Result = true
				o.ToolError = call.IsError
				o.Before, o.Text, o.NonText = textSize(call)
				if err == nil && named {
					o.Tool = name
				}
				limit := budget
				// Exemptions are the server's own and fixed (budgetExempt): a
				// policy supplies a size, never a reason to skip the cut.
				_, exempt := budgetExempt[o.Tool]
				if snap != nil {
					if b, known := snap.eff.Budget(o.Tool); known {
						limit = b.Bytes
					}
				}
				o.Exempt = exempt || o.NonText
				before := call.Content
				if err == nil && !call.IsError && !exempt {
					limitResult(o.Tool, call, limit)
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
