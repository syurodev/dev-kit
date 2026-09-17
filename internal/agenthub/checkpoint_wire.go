// internal/agenthub/checkpoint_wire.go
package agenthub

import (
	"encoding/json"
	"sort"
	"sync"
)

// The two pieces of wire the checkpoint feature adds to a turn.
//
// Both live under the vendor's `_meta.jetbrains.air` namespace — the same
// extension that already carries subagents and async tasks. DevKit did not
// choose the namespace and does not own the shape, so what goes out is exact
// and what comes back is read defensively: a vendor that reshapes its report
// must be able to make DevKit know less, never to make it claim more.

// fileChangeReportVersion is the extension version DevKit speaks.
const fileChangeReportVersion = 1

// fileChangeReportRequest is the `_meta` a prompt carries to ask the agent for
// its own account of the files it changed, tagged with this checkpoint's id.
//
// nil for an empty id, which is what a turn with no checkpoint has: asking for
// a report nothing will read would switch on the vendor's file history for
// nothing.
func fileChangeReportRequest(checkpointID string) json.RawMessage {
	if checkpointID == "" {
		return nil
	}
	raw, err := json.Marshal(map[string]any{
		"jetbrains": map[string]any{
			"air": map[string]any{
				"agentFileChangeReportRequest": map[string]any{
					"version":   fileChangeReportVersion,
					"requestId": checkpointID,
				},
			},
		},
	})
	if err != nil {
		return nil
	}
	return raw
}

// fileChangeReport pulls the agent's report out of an update.
//
// Measured (spike S7): it rides `update._meta.jetbrains.air.agentFileChangeReport`
// on a session_info_update with no title. The notification's own `params._meta`
// is checked too, because which of the two a vendor uses is its choice and
// looking in both costs one map lookup.
func fileChangeReport(update, meta json.RawMessage) (requestID string, report json.RawMessage) {
	for _, source := range []json.RawMessage{update, meta} {
		if len(source) == 0 {
			continue
		}
		var body struct {
			Meta struct {
				JetBrains struct {
					Air struct {
						Report json.RawMessage `json:"agentFileChangeReport"`
					} `json:"air"`
				} `json:"jetbrains"`
			} `json:"_meta"`
		}
		if err := json.Unmarshal(source, &body); err != nil {
			continue
		}
		if len(body.Meta.JetBrains.Air.Report) == 0 {
			continue
		}
		return reportRequestID(body.Meta.JetBrains.Air.Report), body.Meta.JetBrains.Air.Report
	}
	return "", nil
}

func reportRequestID(report json.RawMessage) string {
	var body struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(report, &body); err != nil {
		return ""
	}
	return body.RequestID
}

// touchedPaths collects the paths a turn's tool calls named. Written from the
// update handler, which runs on the connection's goroutine, and read once the
// prompt returns.
type touchedPaths struct {
	mu    sync.Mutex
	paths map[string]struct{}
}

func (t *touchedPaths) add(path string) {
	if path == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.paths == nil {
		t.paths = map[string]struct{}{}
	}
	t.paths[path] = struct{}{}
}

func (t *touchedPaths) list() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.paths))
	for path := range t.paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
