package agenthub

import (
	"context"
	"encoding/json"
	"testing"

	"devkit/internal/identity"
)

// A subagent's tool call is marked with _devkit.origin on the wire like its
// words are (markOrigin tags every update from a session other than the
// runtime's own). The recorder's flush rewrote the row from the call alone and
// dropped the origin, so after a reload the subagent's pane had its words and
// none of its steps. Measured in a real database: 333 message chunks carried
// an origin, none of 7,048 tool calls did.
func TestSubagentToolCallKeepsItsOriginThroughFlushAndReload(t *testing.T) {
	const sub = `,"_devkit":{"origin":"sub1"}`
	ctx := context.Background()
	svc, _, id, _ := newSubagentStateRig(t)

	svc.HandleUpdateForTest(ctx, id, json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"main1","title":"Read a.go","kind":"read","status":"in_progress"}`))
	svc.HandleUpdateForTest(ctx, id, json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"sub-t1","title":"go test","kind":"execute","status":"in_progress"`+sub+`}`))
	svc.HandleUpdateForTest(ctx, id, json.RawMessage(`{"sessionUpdate":"tool_call_update","toolCallId":"sub-t1","status":"completed"`+sub+`}`))

	// A cadence flush between the call and a later update continues the row
	// from storage; the update must not lose the origin either.
	if err := svc.recorder.Flush(ctx, id); err != nil {
		t.Fatal(err)
	}
	svc.HandleUpdateForTest(ctx, id, json.RawMessage(`{"sessionUpdate":"tool_call","toolCallId":"sub-t2","title":"ls","kind":"execute","status":"in_progress"`+sub+`}`))
	if err := svc.recorder.Flush(ctx, id); err != nil {
		t.Fatal(err)
	}
	svc.HandleUpdateForTest(ctx, id, json.RawMessage(`{"sessionUpdate":"tool_call_update","toolCallId":"sub-t2","status":"completed"`+sub+`}`))

	page, err := svc.TranscriptPage(identity.WithCaller(ctx, identity.UI()), id, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	origins := map[string]string{}
	for _, e := range page.Entries {
		if e.ToolCallID == "" {
			continue
		}
		var got SessionUpdate
		if err := json.Unmarshal(e.Payload, &got); err != nil {
			t.Fatal(err)
		}
		origins[e.ToolCallID] = got.Origin
	}
	want := map[string]string{"main1": "", "sub-t1": "sub1", "sub-t2": "sub1"}
	if len(origins) != len(want) {
		t.Fatalf("page holds tool rows %v, want %d", origins, len(want))
	}
	for call, origin := range want {
		if got, ok := origins[call]; !ok || got != origin {
			t.Errorf("tool call %s: stored origin %q (present %v), want %q", call, got, ok, origin)
		}
	}
}
