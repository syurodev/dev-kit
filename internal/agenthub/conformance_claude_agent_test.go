package agenthub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"devkit/internal/sandbox"
)

const (
	claudeConformanceAgentEnv    = "DEVKIT_CLAUDE_CONFORMANCE_AGENT"
	claudeConformanceScenarioEnv = "DEVKIT_CLAUDE_CONFORMANCE_SCENARIO"
	claudeConformanceLogEnv      = "DEVKIT_CLAUDE_CONFORMANCE_LOG"
)

// TestClaudeConformanceAgent runs as the Claude ACP child process for conformance tests.
func TestClaudeConformanceAgent(t *testing.T) {
	if os.Getenv(claudeConformanceAgentEnv) != "1" {
		t.Skip("child process helper")
	}
	if err := serveClaudeConformanceAgent(os.Stdin, os.Stdout, os.Getenv(claudeConformanceScenarioEnv), os.Getenv(claudeConformanceLogEnv)); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

type claudeConformanceRunner struct{ scenario, log string }

func (r claudeConformanceRunner) StartPiped(_ context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
	if err := p.ValidateCmd(c); err != nil {
		return nil, err
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestClaudeConformanceAgent$")
	cmd.Env = append(os.Environ(), claudeConformanceAgentEnv+"=1", claudeConformanceScenarioEnv+"="+r.scenario, claudeConformanceLogEnv+"="+r.log)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	proc := sandbox.NewProcPiped(cmd, in, out, &bytes.Buffer{})
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return proc, nil
}

type claudeConformanceFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func serveClaudeConformanceAgent(in *os.File, out *os.File, scenario, logPath string) error {
	var log *os.File
	var err error
	if logPath != "" {
		log, err = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer log.Close()
	}
	s := bufio.NewScanner(in)
	s.Buffer(make([]byte, 4096), 4<<20)
	var promptID, interactionID json.RawMessage
	interactionMethod := ""
	turns := 0
	for s.Scan() {
		line := append([]byte(nil), s.Bytes()...)
		if log != nil {
			if _, err := log.Write(append(line, '\n')); err != nil {
				return err
			}
		}
		var f claudeConformanceFrame
		if err := json.Unmarshal(line, &f); err != nil {
			return fmt.Errorf("decode client frame: %w", err)
		}
		if f.Method == "" { // response to a permission or elicitation request
			if len(interactionID) != 0 && bytes.Equal(f.ID, interactionID) {
				interactionID = nil
				if interactionMethod == "elicitation/create" {
					interactionMethod = ""
				}
				if len(promptID) != 0 {
					if err := sendClaudeConformanceResponse(out, promptID, claudeConformancePromptResult()); err != nil {
						return err
					}
					promptID = nil
				}
			}
			continue
		}
		if len(f.ID) == 0 { // notifications, including cancel
			if f.Method == "session/cancel" && len(promptID) != 0 {
				if err := sendClaudeConformanceResponse(out, promptID, map[string]any{"stopReason": "cancelled"}); err != nil {
					return err
				}
				// Cancellation ends this scripted turn process, as the adapter's
				// shutdown ladder expects a cancelled child to exit promptly.
				return nil
			}
			continue
		}
		switch f.Method {
		case "initialize":
			if err := sendClaudeConformanceResponse(out, f.ID, map[string]any{"protocolVersion": 1}); err != nil {
				return err
			}
		case "session/new", "session/load":
			sid := "fresh-1"
			if f.Method == "session/load" {
				var p struct {
					SessionID string `json:"sessionId"`
				}
				_ = json.Unmarshal(f.Params, &p)
				if p.SessionID != "" {
					sid = p.SessionID
				}
			}
			result := map[string]any{"sessionId": sid, "modes": map[string]any{"currentModeId": "ask", "availableModes": []any{map[string]any{"id": "ask", "name": "Ask"}, map[string]any{"id": "auto", "name": "Auto"}, map[string]any{"id": "bypassPermissions", "name": "Bypass permissions"}}}, "configOptions": []any{map[string]any{"id": "model", "name": "Model", "category": "model", "type": "select", "currentValue": "model-a", "options": []any{map[string]any{"value": "model-a", "name": "Model A"}, map[string]any{"value": "model-b", "name": "Model B"}}}, map[string]any{"id": "effort", "name": "Effort", "category": "thought_level", "type": "select", "currentValue": "low", "options": []any{map[string]any{"value": "low", "name": "Low"}, map[string]any{"value": "high", "name": "High"}}}}}
			if err := sendClaudeConformanceResponse(out, f.ID, result); err != nil {
				return err
			}
		case "session/prompt":
			turns++
			switch scenario {
			case "refusal":
				if turns == 1 {
					if err := sendClaudeConformanceError(out, f.ID, -32603, "The provider is unavailable: 503"); err != nil {
						return err
					}
				} else {
					if err := sendClaudeConformanceTurn(out, f.ID, "recovered"); err != nil {
						return err
					}
				}
			case "permission", "elicitation", "cancel":
				promptID = append(json.RawMessage(nil), f.ID...)
				if err := sendClaudeConformanceUpdate(out, "agent_message_chunk", map[string]any{"content": map[string]any{"type": "text", "text": "working"}}); err != nil {
					return err
				}
				if scenario == "permission" {
					interactionID = json.RawMessage("17")
					interactionMethod = "session/request_permission"
					if err := sendClaudeConformanceRequest(out, interactionID, interactionMethod, json.RawMessage(`{"sessionId":"fresh-1","toolCall":{"toolCallId":"tool-1","title":"Run command"},"options":[{"optionId":"allow","name":"Allow once","kind":"allow_once"},{"optionId":"reject","name":"Reject","kind":"reject_once"}]}`)); err != nil {
						return err
					}
				}
				if scenario == "elicitation" {
					interactionID = json.RawMessage("18")
					interactionMethod = "elicitation/create"
					if err := sendClaudeConformanceRequest(out, interactionID, interactionMethod, json.RawMessage(`{"mode":"form","sessionId":"fresh-1","message":"Question?","requestedSchema":{"type":"object","properties":{}}}`)); err != nil {
						return err
					}
				}
			case "die":
				return nil
			default:
				if err := sendClaudeConformanceTurn(out, f.ID, "ok"); err != nil {
					return err
				}
			}
		case "session/set_mode":
			var p struct {
				ModeID string `json:"modeId"`
			}
			_ = json.Unmarshal(f.Params, &p)
			if err := sendClaudeConformanceResponse(out, f.ID, map[string]any{}); err != nil {
				return err
			}
			if err := sendClaudeConformanceUpdate(out, "current_mode_update", map[string]any{"currentModeId": p.ModeID}); err != nil {
				return err
			}
		case "session/set_config_option":
			var p struct{ ConfigID, Value string }
			_ = json.Unmarshal(f.Params, &p)
			options := []any{map[string]any{"id": p.ConfigID, "name": p.ConfigID, "category": "model", "type": "select", "currentValue": p.Value, "options": []any{map[string]any{"value": p.Value, "name": p.Value}}}}
			if err := sendClaudeConformanceResponse(out, f.ID, map[string]any{"configOptions": options}); err != nil {
				return err
			}
		case "session/close", "session/delete", "session/fork":
			if err := sendClaudeConformanceResponse(out, f.ID, map[string]any{}); err != nil {
				return err
			}
		default:
			if err := sendClaudeConformanceError(out, f.ID, -32601, "method not found"); err != nil {
				return err
			}
		}
	}
	return s.Err()
}

func claudeConformancePromptResult() any {
	return map[string]any{"stopReason": "end_turn", "_meta": map[string]any{"claudeCode": map[string]any{"inputTokens": 12, "outputTokens": 4}}}
}
func sendClaudeConformanceResponse(out *os.File, id json.RawMessage, result any) error {
	return json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}
func sendClaudeConformanceError(out *os.File, id json.RawMessage, code int, msg string) error {
	return json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}
func sendClaudeConformanceRequest(out *os.File, id json.RawMessage, method string, params json.RawMessage) error {
	return json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}
func sendClaudeConformanceUpdate(out *os.File, kind string, payload map[string]any) error {
	payload["sessionUpdate"] = kind
	return json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "fresh-1", "update": payload}})
}
func sendClaudeConformanceTurn(out *os.File, id json.RawMessage, text string) error {
	if err := sendClaudeConformanceUpdate(out, "agent_message_chunk", map[string]any{"content": map[string]any{"type": "text", "text": text}}); err != nil {
		return err
	}
	return sendClaudeConformanceResponse(out, id, claudeConformancePromptResult())
}

func claudeConformanceScenario(script []string) string {
	if len(script) == 0 {
		return "turn"
	}
	switch {
	case strings.Contains(script[0], "NEED_PERMISSION"):
		return "permission"
	case strings.Contains(script[0], "ELICITATION"):
		return "elicitation"
	case strings.Contains(script[0], "HOLD_AFTER"):
		return "cancel"
	case strings.Contains(script[0], "die"):
		return "die"
	case strings.Contains(script[0], "FAIL_PROVIDER_503"):
		return "refusal"
	default:
		return "turn"
	}
}
