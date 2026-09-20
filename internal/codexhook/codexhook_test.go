package codexhook

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestArgumentsOnlyReferenceFieldsTheEventActuallyCarries(t *testing.T) {
	// codex-rs's own command input structs, one per event
	// (codex-rs/hooks/src/schema.rs) — the ground truth this test pins
	// against. A placeholder naming a field outside this set fails the whole
	// hook in Codex, silently, because Arguments would be lying about what
	// the event carries.
	carried := map[Event]map[string]bool{
		EventSessionStart: {
			"session_id": true, "cwd": true, "model": true, "source": true,
		},
		EventSessionEnd: {
			"session_id": true, "cwd": true, "reason": true,
		},
		EventUserPromptSubmit: {
			"session_id": true, "cwd": true, "turn_id": true, "model": true, "prompt": true,
		},
		EventPreToolUse: {
			"session_id": true, "cwd": true, "turn_id": true, "model": true,
			"permission_mode": true, "tool_name": true, "tool_use_id": true, "tool_input": true,
		},
		EventPostToolUse: {
			"session_id": true, "cwd": true, "turn_id": true, "model": true,
			"permission_mode": true, "tool_name": true, "tool_use_id": true,
			"tool_input": true, "tool_response": true,
		},
		EventStop: {
			"session_id": true, "cwd": true, "turn_id": true, "model": true,
			"last_assistant_message": true,
		},
	}

	for _, event := range Registered {
		allowed, ok := carried[event]
		if !ok {
			t.Fatalf("no ground truth recorded for %s — the test itself is incomplete", event)
		}
		args := Arguments(event)
		if len(args) == 0 {
			t.Errorf("%s declared no arguments", event)
		}
		for key, template := range args {
			// "event" is DevKit's own literal constant, stamped in rather
			// than substituted — Codex's template expansion only touches
			// ${...} patterns and leaves a literal string as written.
			if key == "event" {
				if template != string(event) {
					t.Errorf("%s.event = %q, want %q", event, template, event)
				}
				continue
			}
			field, ok := placeholderField(template)
			if !ok {
				t.Errorf("%s.%s = %q is not a bare ${field} placeholder", event, key, template)
				continue
			}
			if !allowed[field] {
				t.Errorf("%s.%s references ${%s}, which that event's command input does not carry", event, key, field)
			}
		}
	}
}

// placeholderField extracts "x" from "${x}", the complete-placeholder shape
// mcp_runner.rs requires to preserve JSON type; a placeholder embedded in
// surrounding text renders as a string, which none of these want.
func placeholderField(template string) (string, bool) {
	if len(template) < 4 || template[0] != '$' || template[1] != '{' || template[len(template)-1] != '}' {
		return "", false
	}
	return template[2 : len(template)-1], true
}

func TestEveryRegisteredEventProducesArguments(t *testing.T) {
	for _, event := range Registered {
		if Arguments(event) == nil {
			t.Errorf("Registered contains %s but Arguments has nothing for it", event)
		}
	}
}

func TestArgumentsOfAnUnregisteredEventIsNil(t *testing.T) {
	if got := Arguments(Event("PermissionRequest")); got != nil {
		t.Fatalf("Arguments(PermissionRequest) = %v, want nil — DevKit does not register it", got)
	}
}

func TestDecodeRejectsAnEventDevKitDidNotRegister(t *testing.T) {
	raw := json.RawMessage(`{"event":"PermissionRequest","sessionId":"s1","cwd":"/tmp"}`)
	_, err := Decode(raw)
	var unknown ErrUnknownEvent
	if !errors.As(err, &unknown) {
		t.Fatalf("Decode(PermissionRequest) = %v, want ErrUnknownEvent", err)
	}
}

// A row attributed to no session is worse than no row: nothing in the UI can
// ever show it.
func TestDecodeRejectsAMissingSessionID(t *testing.T) {
	raw := json.RawMessage(`{"event":"Stop","cwd":"/tmp"}`)
	if _, err := Decode(raw); err == nil {
		t.Fatal("a hook call with no session id was accepted")
	}
}

func TestDecodeRoundTripsAPreToolUseCall(t *testing.T) {
	raw := json.RawMessage(`{
		"event": "PreToolUse", "sessionId": "s1", "cwd": "/repo", "turnId": "t1",
		"model": "gpt-5-codex", "permissionMode": "auto", "toolName": "shell",
		"toolUseId": "tu1", "toolInput": {"command": ["ls"]}
	}`)
	call, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if call.Event != EventPreToolUse || call.SessionID != "s1" || call.ToolName != "shell" {
		t.Fatalf("call = %+v", call)
	}
	if string(call.ToolInput) != `{"command": ["ls"]}` {
		t.Fatalf("toolInput was not preserved raw: %s", call.ToolInput)
	}
}

func TestSummaryCarriesNoToolArgumentsOrPromptText(t *testing.T) {
	// An audit line that records what it was watching is a second copy of it.
	secret := "the user's actual prompt text, which must never appear in the summary"
	call := Call{Event: EventUserPromptSubmit, SessionID: "s1", Prompt: secret}
	summary := call.Summary()
	if summary == "" {
		t.Fatal("empty summary")
	}
	if containsSubstring(summary, secret) {
		t.Fatalf("summary leaked the prompt text: %q", summary)
	}

	toolCall := Call{
		Event: EventPreToolUse, SessionID: "s1", ToolName: "shell",
		ToolInput: json.RawMessage(`{"command":["rm","-rf","/secret-path"]}`),
	}
	toolSummary := toolCall.Summary()
	if containsSubstring(toolSummary, "secret-path") {
		t.Fatalf("summary leaked tool input: %q", toolSummary)
	}
	if toolSummary != "about to run shell" {
		t.Fatalf("PreToolUse summary = %q", toolSummary)
	}
}

func TestSummaryNamesTheToolForPostToolUse(t *testing.T) {
	call := Call{Event: EventPostToolUse, SessionID: "s1", ToolName: "apply_patch"}
	if got := call.Summary(); got != "ran apply_patch" {
		t.Fatalf("Summary() = %q", got)
	}
}

func TestSummaryFallsBackWhenToolNameIsMissing(t *testing.T) {
	call := Call{Event: EventPreToolUse, SessionID: "s1"}
	if got := call.Summary(); got != "about to run a tool" {
		t.Fatalf("Summary() = %q", got)
	}
}

func TestObservedNeverStopsTheTurn(t *testing.T) {
	// DevKit has no UI to decide with yet; stopping somebody's turn from a
	// hook they cannot see is not a partial feature, it is a bug.
	if reply := Observed(); !reply.Continue {
		t.Fatalf("Observed() = %+v, must always continue", reply)
	}
}

func TestEventsIsSortedAndMatchesRegistered(t *testing.T) {
	names := Events()
	if len(names) != len(Registered) {
		t.Fatalf("Events() has %d, Registered has %d", len(names), len(Registered))
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("Events() is not sorted: %v", names)
		}
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestNeedsMatcherMatchesCodexsOwnList(t *testing.T) {
	// codex-rs's HOOK_EVENT_NAMES_WITH_MATCHERS, intersected with Registered.
	want := map[Event]bool{
		EventSessionStart:     true,
		EventUserPromptSubmit: false,
		EventPreToolUse:       true,
		EventPostToolUse:      true,
		EventStop:             false,
		EventSessionEnd:       true,
	}
	for event, expect := range want {
		if got := NeedsMatcher(event); got != expect {
			t.Errorf("NeedsMatcher(%s) = %v, want %v", event, got, expect)
		}
	}
}
