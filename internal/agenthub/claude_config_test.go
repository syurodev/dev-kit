package agenthub

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/sandbox"
)

// catalogVendor is claude-agent-acp as far as the model picker can see it: a
// fresh session on sonnet, and an effort list that follows whichever model it
// is switched to (none at all on haiku).
type catalogVendor struct{ capture string }

func (v catalogVendor) StartPiped(context.Context, sandbox.Policy, sandbox.Cmd) (*sandbox.Proc, error) {
	script := `
model='{"id":"model","name":"Model","category":"model","type":"select","currentValue":"sonnet","options":[{"value":"sonnet","name":"Sonnet"},{"value":"opus","name":"Opus"},{"value":"haiku","name":"Haiku"}]}'
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$0"
  id=$(printf '%s' "$line" | sed -n 's/^{"jsonrpc":"2.0","id":\([0-9]*\).*/\1/p')
  [ -z "$id" ] && continue
  case "$line" in
    *'"method":"initialize"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1}}\n' "$id" ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"cat-1","configOptions":[{"id":"mode","type":"select","currentValue":"default","options":[{"value":"default"}]},%s,{"id":"effort","name":"Effort","category":"thought_level","type":"select","currentValue":"default","options":[{"value":"default","name":"Default"},{"value":"low","name":"Low"},{"value":"high","name":"High"}]}]}}\n' "$id" "$model" ;;
    *'"method":"session/set_config_option"'*)
      case "$line" in
        *'"value":"opus"'*) printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[{"id":"effort","category":"thought_level","type":"select","currentValue":"default","options":[{"value":"default"},{"value":"low"},{"value":"high"},{"value":"xhigh"}]}]}}\n' "$id" ;;
        *) printf '{"jsonrpc":"2.0","id":%s,"result":{"configOptions":[]}}\n' "$id" ;;
      esac ;;
    *) printf '{"jsonrpc":"2.0","id":%s,"result":{}}\n' "$id" ;;
  esac
done
`
	cmd := exec.Command("sh", "-c", script, v.capture)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return sandbox.NewProcPiped(cmd, stdin, stdout, &bytes.Buffer{}), nil
}

func TestClaudeConfigCatalogListsEachModelWithItsOwnEfforts(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "sent.jsonl")
	bin := filepath.Join(dir, "claude-agent-acp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: catalogVendor{capture: capture}})

	options, err := adapter.ConfigCatalog(context.Background(), OpenSpec{})
	if err != nil {
		t.Fatalf("ConfigCatalog: %v", err)
	}
	if len(options) != 1 || options[0].ID != "model" || options[0].Category != "model" {
		t.Fatalf("catalog = %+v, want the model option alone", options)
	}
	model := options[0]
	if model.DefaultValue != "sonnet" || string(model.CurrentValue) != `"sonnet"` {
		t.Fatalf("default = %q current = %s", model.DefaultValue, model.CurrentValue)
	}
	values := func(c ConfigSelectOption) string {
		var out []string
		for _, e := range c.SupportedEfforts {
			out = append(out, e.Value)
		}
		return strings.Join(out, ",")
	}
	want := map[string]string{"sonnet": "default,low,high", "opus": "default,low,high,xhigh", "haiku": ""}
	for _, choice := range model.Options {
		if got := values(choice); got != want[choice.Value] {
			t.Errorf("%s efforts = %q, want %q", choice.Value, got, want[choice.Value])
		}
	}
	if model.Options[0].DefaultEffort != "default" {
		t.Errorf("sonnet default effort = %q", model.Options[0].DefaultEffort)
	}

	// No prompt: the point of the catalog is that nothing is inferred.
	if sent, _ := os.ReadFile(capture); strings.Contains(string(sent), "session/prompt") {
		t.Fatalf("the catalog sent a prompt: %s", sent)
	}
}

// The draft picker names the effort reasoning_effort for every vendor; Claude's
// own id for it is effort, and anything else is "Unknown config option".
func TestClaudeRuntimeMapsDraftEffortToItsOwnID(t *testing.T) {
	call := func(optionID string) string {
		var seen string
		dir := t.TempDir()
		capture := filepath.Join(dir, "sent.jsonl")
		bin := filepath.Join(dir, "claude-agent-acp")
		_ = os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755)
		rt, err := NewClaudeAdapter(ClaudeOptions{Command: bin, Runner: catalogVendor{capture: capture}}).Open(context.Background(), OpenSpec{Cwd: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		defer rt.Close(context.Background())
		if err := rt.(RuntimeConfigurator).SetConfigOption(context.Background(), optionID, "high"); err != nil {
			t.Fatal(err)
		}
		for _, frame := range sentFrames(t, capture) {
			var call struct {
				Method string `json:"method"`
				Params struct {
					ConfigID string `json:"configId"`
				} `json:"params"`
			}
			if json.Unmarshal([]byte(frame), &call) == nil && call.Method == "session/set_config_option" {
				seen = call.Params.ConfigID
			}
		}
		return seen
	}
	if got := call("reasoning_effort"); got != "effort" {
		t.Errorf("reasoning_effort went out as %q", got)
	}
	if got := call("model"); got != "model" {
		t.Errorf("model went out as %q", got)
	}
}
