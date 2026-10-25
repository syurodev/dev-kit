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
	openCodeConformanceAgentEnv    = "DEVKIT_OPENCODE_CONFORMANCE_AGENT"
	openCodeConformanceScenarioEnv = "DEVKIT_OPENCODE_CONFORMANCE_SCENARIO"
	openCodeConformanceLogEnv      = "DEVKIT_OPENCODE_CONFORMANCE_LOG"
)

// TestOpenCodeConformanceAgent is the method-aware scripted opencode process
// started by openCodeConformanceRunner. It deliberately handles requests by
// JSON-RPC method instead of relying on an assumed request order.
func TestOpenCodeConformanceAgent(t *testing.T) {
	if os.Getenv(openCodeConformanceAgentEnv) != "1" {
		t.Skip("child process helper")
	}
	if err := serveOpenCodeConformanceAgent(os.Stdin, os.Stdout, os.Getenv(openCodeConformanceScenarioEnv), os.Getenv(openCodeConformanceLogEnv)); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

type openCodeConformanceRunner struct {
	scenario string
	log      string
}

func (r openCodeConformanceRunner) StartPiped(_ context.Context, p sandbox.Policy, c sandbox.Cmd) (*sandbox.Proc, error) {
	// This fixture launches the test binary as its scripted agent. The runner
	// is not the production sandbox; validating its synthetic policy would make
	// the protocol test depend on host-specific jail paths.
	cmd := exec.Command(os.Args[0], "-test.run=^TestOpenCodeConformanceAgent$")
	cmd.Env = append(os.Environ(),
		openCodeConformanceAgentEnv+"=1",
		openCodeConformanceScenarioEnv+"="+r.scenario,
		openCodeConformanceLogEnv+"="+r.log,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	proc := sandbox.NewProcPiped(cmd, stdin, stdout, &bytes.Buffer{})
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return proc, nil
}

type openCodeConformanceFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func serveOpenCodeConformanceAgent(in *os.File, out *os.File, scenario, logPath string) error {
	var logFile *os.File
	var err error
	if logPath != "" {
		logFile, err = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer logFile.Close()
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	turns := 0
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if logFile != nil {
			if _, err := logFile.Write(append(line, '\n')); err != nil {
				return err
			}
		}
		var frame openCodeConformanceFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			return fmt.Errorf("decode client frame: %w", err)
		}
		if frame.Method == "" || len(frame.ID) == 0 { // a response or notification
			continue
		}
		switch frame.Method {
		case "initialize":
			if err := sendOpenCodeConformanceResponse(out, frame.ID, json.RawMessage(openCodeInitResult)); err != nil {
				return err
			}
		case "session/new", "session/load":
			if err := sendOpenCodeConformanceResponse(out, frame.ID, json.RawMessage(`{"sessionId":"ses_conformance","configOptions":`+openCodeOptionsWire+`}`)); err != nil {
				return err
			}
		case "session/set_config_option":
			var params struct {
				ConfigID string `json:"configId"`
				Value    string `json:"value"`
			}
			if err := json.Unmarshal(frame.Params, &params); err != nil {
				return err
			}
			var options []map[string]any
			if err := json.Unmarshal([]byte(openCodeOptionsWire), &options); err != nil {
				return err
			}
			for _, option := range options {
				if option["id"] == params.ConfigID {
					option["currentValue"] = params.Value
					for _, raw := range option["options"].([]any) {
						choice := raw.(map[string]any)
						if choice["value"] == params.Value {
							return sendOpenCodeConformanceResponse(out, frame.ID, map[string]any{"configOptions": options})
						}
					}
					option["options"] = append(option["options"].([]any), map[string]any{"value": params.Value, "name": params.Value})
					break
				}
			}
			if err := sendOpenCodeConformanceResponse(out, frame.ID, map[string]any{"configOptions": options}); err != nil {
				return err
			}
		case "session/prompt":
			turns++
			switch scenario {
			case "die":
				os.Exit(1) // terminate the child process after receiving the turn
			case "FAIL_PROVIDER_503":
				if turns == 1 {
					if err := sendOpenCodeConformanceError(out, frame.ID, -32603, "The provider is unavailable: 503"); err != nil {
						return err
					}
				} else if err := sendOpenCodeConformanceTurn(out, frame.ID, "recovered", false); err != nil {
					return err
				}
			case "NEED_PERMISSION":
				if err := sendOpenCodeConformanceRequest(out, 77, "session/request_permission", json.RawMessage(`{"sessionId":"ses_conformance","toolCall":{"toolCallId":"call","kind":"execute"},"options":[{"optionId":"allow","kind":"allow_once","name":"Allow once"},{"optionId":"reject","kind":"reject_once","name":"Reject"}]}`)); err != nil {
					return err
				}
				if err := awaitOpenCodeConformanceResponse(scanner, logFile, 77); err != nil {
					return err
				}
				if err := sendOpenCodeConformanceResponse(out, frame.ID, map[string]any{"stopReason": "end_turn"}); err != nil {
					return err
				}
			case "ELICITATION":
				if err := sendOpenCodeConformanceRequest(out, 78, "elicitation/create", json.RawMessage(`{"mode":"form","sessionId":"ses_conformance","message":"Question?","requestedSchema":{"type":"object","properties":{}}}`)); err != nil {
					return err
				}
				if err := awaitOpenCodeConformanceResponse(scanner, logFile, 78); err != nil {
					return err
				}
				if err := sendOpenCodeConformanceResponse(out, frame.ID, map[string]any{"stopReason": "end_turn"}); err != nil {
					return err
				}
			case "HOLD_AFTER":
				if err := sendOpenCodeConformanceUpdate(out, "agent_message_chunk", map[string]any{"content": map[string]any{"type": "text", "text": "working"}}); err != nil {
					return err
				}
				for scanner.Scan() {
					cancelLine := append([]byte(nil), scanner.Bytes()...)
					if logFile != nil {
						if _, err := logFile.Write(append(cancelLine, '\n')); err != nil {
							return err
						}
					}
					var cancel openCodeConformanceFrame
					if err := json.Unmarshal(cancelLine, &cancel); err != nil {
						return err
					}
					if cancel.Method == "session/cancel" {
						if err := sendOpenCodeConformanceResponse(out, frame.ID, map[string]any{"stopReason": "cancelled"}); err != nil {
							return err
						}
						break
					}
				}
			default:
				if err := sendOpenCodeConformanceTurn(out, frame.ID, "ok", true); err != nil {
					return err
				}
			}
		case "session/set_mode", "session/delete", "session/close":
			if err := sendOpenCodeConformanceResponse(out, frame.ID, map[string]any{}); err != nil {
				return err
			}
		default:
			if err := sendOpenCodeConformanceResponse(out, frame.ID, map[string]any{}); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

func awaitOpenCodeConformanceResponse(scanner *bufio.Scanner, logFile *os.File, id int) error {
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		if logFile != nil {
			if _, err := logFile.Write(append(line, '\n')); err != nil {
				return err
			}
		}
		var frame struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			return err
		}
		if frame.Method == "" && strings.Trim(string(frame.ID), `"`) == fmt.Sprint(id) {
			return nil
		}
	}
	return scanner.Err()
}

func sendOpenCodeConformanceResponse(out *os.File, id json.RawMessage, result any) error {
	return json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func sendOpenCodeConformanceError(out *os.File, id json.RawMessage, code int, message string) error {
	return json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func sendOpenCodeConformanceRequest(out *os.File, id int, method string, params json.RawMessage) error {
	return json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}

func sendOpenCodeConformanceTurn(out *os.File, id json.RawMessage, text string, withUsage bool) error {
	if err := sendOpenCodeConformanceUpdate(out, "agent_message_chunk", map[string]any{"content": map[string]any{"type": "text", "text": text}}); err != nil {
		return err
	}
	if withUsage {
		if err := sendOpenCodeConformanceUpdate(out, "usage_update", map[string]any{"used": 10, "size": 100}); err != nil {
			return err
		}
	}
	return sendOpenCodeConformanceResponse(out, id, map[string]any{"stopReason": "end_turn"})
}

func sendOpenCodeConformanceUpdate(out *os.File, kind string, update map[string]any) error {
	update["sessionUpdate"] = kind
	return json.NewEncoder(out).Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "ses_conformance", "update": update}})
}

const openCodeInitResult = `{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"mcpCapabilities":{"http":true,"sse":false},"promptCapabilities":{"embeddedContext":true,"image":true},"sessionCapabilities":{"additionalDirectories":{},"close":{},"delete":{},"fork":{},"list":{},"resume":{}}},"authMethods":[{"id":"opencode-login","name":"Login with opencode","description":"Run opencode auth login","type":"terminal","args":["--login"]}],"agentInfo":{"name":"OpenCode","version":"9.9.9"}}`
