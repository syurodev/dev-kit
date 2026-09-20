// Package codexhook is the contract between Codex's lifecycle hooks and
// DevKit.
//
// Codex runs in the user's own terminal — DevKit does not spawn it, does not
// sandbox it, and does not sit on its stdio. What it does instead is ask Codex
// to call back: a hook handler of type `mcp_tool` pointed at the DevKit MCP
// server DevKit already installs into `~/.codex/config.toml`. So DevKit sees
// the session, the prompts and every tool call without being in the way of any
// of them.
//
// Two facts from Codex's own source shape everything here
// (codex-rs/hooks/src/engine/mcp_runner.rs, codex-rs/config/src/hook_config.rs):
//
//   - An `mcp_tool` handler receives ONLY the arguments its config declares,
//     with `${field}` placeholders substituted from the event. It is not handed
//     the payload. So DevKit declares, per event, exactly which fields it
//     takes — and a placeholder naming a field the event does not carry FAILS
//     the hook, which is why the templates below are per event rather than one
//     shared list.
//   - A handler that errors is logged and the turn continues; only parsed
//     output can block. So DevKit being closed costs Codex a warning, never a
//     broken tool call. That is the property that makes this safe to install.
package codexhook

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Event is a Codex lifecycle hook, spelled the way Codex spells it in config.
type Event string

const (
	EventSessionStart     Event = "SessionStart"
	EventUserPromptSubmit Event = "UserPromptSubmit"
	EventPreToolUse       Event = "PreToolUse"
	EventPostToolUse      Event = "PostToolUse"
	EventStop             Event = "Stop"
	EventSessionEnd       Event = "SessionEnd"
)

// Registered is what DevKit asks Codex to send, in the order a session
// produces them.
//
// PermissionRequest is deliberately absent, and so is PreToolUse's ability to
// deny. A hook that receives a permission question and answers it is Codex's
// own gate replaced by DevKit's — and DevKit has no UI for that yet, so every
// answer would be "allow". Turning somebody's approvals off by installing an
// observer is not an acceptable accident. Watching comes first; deciding is a
// separate feature with a separate switch.
//
// Compaction, subagent and interrupt hooks are left out for a duller reason:
// nothing here reads them yet, and a hook that is registered and ignored is a
// subprocess call on somebody's hot path for no result.
var Registered = []Event{
	EventSessionStart,
	EventUserPromptSubmit,
	EventPreToolUse,
	EventPostToolUse,
	EventStop,
	EventSessionEnd,
}

// Arguments is the `input` template DevKit declares for one event.
//
// Every value is a complete `${placeholder}`, which Codex substitutes keeping
// the field's JSON type — `${tool_input}` arrives as an object, not as its
// text. The field names are Codex's, in snake_case, taken from the command
// input structs in codex-rs/hooks/src/schema.rs.
//
// Optional fields are never referenced. `agent_id` and `agent_type` are
// skipped when absent, and a placeholder for a missing field fails the whole
// hook rather than resolving to empty.
func Arguments(event Event) map[string]string {
	common := func(extra map[string]string) map[string]string {
		args := map[string]string{
			"event":     string(event),
			"sessionId": "${session_id}",
			"cwd":       "${cwd}",
		}
		for key, value := range extra {
			args[key] = value
		}
		return args
	}
	switch event {
	case EventSessionStart:
		return common(map[string]string{"model": "${model}", "source": "${source}"})
	case EventSessionEnd:
		// No model and no permission mode on this one: the session is over.
		return common(map[string]string{"reason": "${reason}"})
	case EventUserPromptSubmit:
		return common(map[string]string{
			"turnId": "${turn_id}", "model": "${model}", "prompt": "${prompt}",
		})
	case EventPreToolUse:
		return common(map[string]string{
			"turnId": "${turn_id}", "model": "${model}",
			"permissionMode": "${permission_mode}",
			"toolName":       "${tool_name}", "toolUseId": "${tool_use_id}",
			"toolInput": "${tool_input}",
		})
	case EventPostToolUse:
		return common(map[string]string{
			"turnId": "${turn_id}", "model": "${model}",
			"permissionMode": "${permission_mode}",
			"toolName":       "${tool_name}", "toolUseId": "${tool_use_id}",
			"toolInput": "${tool_input}", "toolResponse": "${tool_response}",
		})
	case EventStop:
		return common(map[string]string{
			"turnId": "${turn_id}", "model": "${model}",
			"lastMessage": "${last_assistant_message}",
		})
	default:
		return nil
	}
}

// MCPToolName is the tool DevKit's own MCP server exposes for these hooks to
// call. One tool for every event, not one per event, because the events share
// almost everything about how they are handled — decode, attribute to a
// session, summarize, store — and Arguments already tells the handler which
// event it received via the "event" field.
const MCPToolName = "codex.hookEvent"

// NeedsMatcher reports whether event's matcher field means anything to Codex.
//
// codex-rs's own HOOK_EVENT_NAMES_WITH_MATCHERS names PreToolUse, PostToolUse,
// SessionStart and SessionEnd among the events DevKit registers; Codex ignores
// the matcher on UserPromptSubmit and Stop entirely. Writing "*" only where it
// is read keeps the config file honest about what the field does.
func NeedsMatcher(event Event) bool {
	switch event {
	case EventPreToolUse, EventPostToolUse, EventSessionStart, EventSessionEnd:
		return true
	default:
		return false
	}
}

// Call is one hook arriving, already decoded.
type Call struct {
	Event     Event  `json:"event"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	TurnID    string `json:"turnId,omitempty"`
	Model     string `json:"model,omitempty"`
	// PermissionMode is Codex's own approval setting for the turn, which is
	// worth recording precisely because DevKit is not the one enforcing it.
	PermissionMode string `json:"permissionMode,omitempty"`
	ToolName       string `json:"toolName,omitempty"`
	ToolUseID      string `json:"toolUseId,omitempty"`
	// ToolInput and ToolResponse are whatever the tool takes and returns, so
	// they stay raw: this package does not know what a Codex tool's arguments
	// look like and must not pretend to.
	ToolInput    json.RawMessage `json:"toolInput,omitempty"`
	ToolResponse json.RawMessage `json:"toolResponse,omitempty"`
	Prompt       string          `json:"prompt,omitempty"`
	LastMessage  string          `json:"lastMessage,omitempty"`
	Source       string          `json:"source,omitempty"`
	Reason       string          `json:"reason,omitempty"`
}

// ErrUnknownEvent is an `event` argument that is not one DevKit registered.
type ErrUnknownEvent struct{ Event string }

func (e ErrUnknownEvent) Error() string { return "codexhook: unknown event " + e.Event }

// Decode reads one call's arguments and validates it. It exists for callers
// that receive raw JSON; a caller whose framework already unmarshaled into a
// Call — the MCP tool handler, which needs a proper object schema and so
// takes Call directly rather than json.RawMessage — calls Validate instead
// and skips the redundant unmarshal.
func Decode(raw json.RawMessage) (Call, error) {
	var call Call
	if err := json.Unmarshal(raw, &call); err != nil {
		return Call{}, err
	}
	if err := Validate(call); err != nil {
		return Call{}, err
	}
	return call, nil
}

// Validate checks a call already populated from JSON.
//
// A session id is required: without it the call cannot be attributed, and a
// row attributed to nothing is worse than no row.
func Validate(call Call) error {
	if !IsRegistered(call.Event) {
		return ErrUnknownEvent{Event: string(call.Event)}
	}
	if strings.TrimSpace(call.SessionID) == "" {
		return fmt.Errorf("codexhook: %s with no session id", call.Event)
	}
	return nil
}

func IsRegistered(event Event) bool {
	for _, candidate := range Registered {
		if candidate == event {
			return true
		}
	}
	return false
}

// Reply is the envelope Codex parses out of a hook's output.
//
// The field names are Codex's, in camelCase, from the command output wire
// structs — the same shape Claude Code's hooks answer with, which is not a
// coincidence: Codex's engine is called ClaudeHooksEngine.
type Reply struct {
	// Continue false stops the turn. DevKit never sends it today; see
	// Registered on why deciding is a separate feature.
	Continue bool `json:"continue"`
	// SystemMessage is shown to the user, not to the model. Empty is silent,
	// which is what an observer should be.
	SystemMessage string `json:"systemMessage,omitempty"`
}

// Observed is the answer to every hook DevKit takes today: seen, carry on.
func Observed() Reply { return Reply{Continue: true} }

// Summary is one line naming what happened, for the audit trail. It carries no
// tool arguments and no prompt text: an audit line that records what it was
// watching is a second copy of it.
func (c Call) Summary() string {
	switch c.Event {
	case EventSessionStart:
		return "session started" + suffix("from", c.Source)
	case EventSessionEnd:
		return "session ended" + suffix("because", c.Reason)
	case EventUserPromptSubmit:
		return fmt.Sprintf("prompt submitted (%d characters)", len([]rune(c.Prompt)))
	case EventPreToolUse:
		return "about to run " + toolLabel(c)
	case EventPostToolUse:
		return "ran " + toolLabel(c)
	case EventStop:
		return "turn finished"
	default:
		return string(c.Event)
	}
}

func toolLabel(c Call) string {
	if c.ToolName == "" {
		return "a tool"
	}
	return c.ToolName
}

func suffix(word, value string) string {
	if value == "" {
		return ""
	}
	return " " + word + " " + value
}

// Events lists the registered events sorted by name, for a config writer that
// wants a stable file.
func Events() []string {
	names := make([]string, 0, len(Registered))
	for _, event := range Registered {
		names = append(names, string(event))
	}
	sort.Strings(names)
	return names
}
