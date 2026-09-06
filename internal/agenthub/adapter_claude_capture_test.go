package agenthub_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

// TestClaudeACPCapture records every session/update a real Claude session emits
// and writes them as JSONL. It exists because the transcript types in update.go
// were written from the ACP spec, not from traffic: the spec says a tool call
// may carry `diff` and `terminal` content, but nothing in this repo proved that
// Claude actually sends either. The dump is the proof, or the correction.
//
// Run it deliberately:
//
//	DEVKIT_CLAUDE_CAPTURE=1 go test ./internal/agenthub/ -run TestClaudeACPCapture -v -timeout 15m
//
// DEVKIT_CLAUDE_ACP_BIN may point at an absolute claude-code-acp binary;
// otherwise npx is used. DEVKIT_CLAUDE_CAPTURE_OUT overrides the output path.
func TestClaudeACPCapture(t *testing.T) {
	if os.Getenv("DEVKIT_CLAUDE_CAPTURE") != "1" {
		t.Skip("set DEVKIT_CLAUDE_CAPTURE=1 to record a live Claude session")
	}

	opts := agenthub.ClaudeOptions{}
	if bin := os.Getenv("DEVKIT_CLAUDE_ACP_BIN"); bin != "" {
		if !filepath.IsAbs(bin) {
			t.Fatalf("DEVKIT_CLAUDE_ACP_BIN must be absolute, got %q", bin)
		}
		opts.Command = bin
	} else {
		if _, err := exec.LookPath("npx"); err != nil {
			t.Skip("npx not on PATH and DEVKIT_CLAUDE_ACP_BIN unset")
		}
		opts.AllowNpx = true
		opts.Command = "npx"
	}

	// A project with something to read, edit and run. The prompt below asks for
	// all three because each is a different tool-call kind, and `diff` /
	// `terminal` content only appear for edit / execute.
	proj := t.TempDir()
	seed := filepath.Join(proj, "rounding.py")
	if err := os.WriteFile(seed, []byte("def total(cents):\n    return round(cents / 100)\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Open gets its own deadline. Without one a vendor process that dies on
	// spawn leaves acp.Conn.Call blocked on a pipe that will never answer, and
	// the run burns the whole -timeout before saying anything useful. Measured:
	// a missing binary hung Open for the full 15m.
	openCtx, openCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer openCancel()
	rt, err := agenthub.NewClaudeAdapter(opts).Open(openCtx, agenthub.OpenSpec{Cwd: proj})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rt.Close(context.Background()) }()

	var (
		mu      sync.Mutex
		records []json.RawMessage
	)
	rt.SetHandlers(agenthub.RuntimeHandlers{
		OnUpdate: func(update json.RawMessage, meta json.RawMessage) {
			mu.Lock()
			defer mu.Unlock()
			records = append(records, append(json.RawMessage(nil), update...))
			if len(meta) > 0 {
				records = append(records, wrapMeta(meta))
			}
		},
		// Approve everything: this is a measurement of what Claude emits, and a
		// denial would cut the run short before any edit or command appears.
		OnPermission: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			mu.Lock()
			records = append(records, wrapPermission(params))
			mu.Unlock()
			return allowOutcome(params), nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	out, promptErr := rt.Prompt(ctx, "Read rounding.py, then change round() to use two decimal places, "+
		"then run `python3 -c \"import rounding; print(rounding.total(1234))\"` and tell me the result.")
	if promptErr != nil {
		t.Logf("Prompt returned error (still writing whatever was captured): %v", promptErr)
	} else {
		t.Logf("stopReason=%s", out.StopReason)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(records) == 0 {
		t.Fatal("captured nothing; the adapter emitted no updates")
	}

	path := os.Getenv("DEVKIT_CLAUDE_CAPTURE_OUT")
	if path == "" {
		path = filepath.Join("testdata", "claude_session_capture.jsonl")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, rec := range records {
		if _, err := f.Write(append(rec, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	t.Logf("wrote %d records to %s", len(records), path)

	reportCapture(t, records)

	// Fail last, after the dump and the tally are safely on disk. A turn that
	// died before running a tool measured nothing, and a green run here would
	// be read as "diff and terminal do not happen" rather than "we never got
	// far enough to see". Silence is not evidence.
	if promptErr != nil {
		t.Fatalf("turn did not complete, so nothing was measured: %v", promptErr)
	}
}

// reportCapture is the point of the whole test: a tally of what actually
// arrived, so the answer is a number rather than an impression.
func reportCapture(t *testing.T, records []json.RawMessage) {
	t.Helper()
	kinds := map[string]int{}
	toolKinds := map[string]int{}
	contentTypes := map[string]int{}
	blockTypes := map[string]int{}

	for _, rec := range records {
		parsed := agenthub.ParseSessionUpdate(rec)
		kinds[string(parsed.Kind)]++
		if parsed.Content != nil {
			blockTypes[parsed.Content.Type]++
		}
		if parsed.ToolCall != nil {
			if parsed.ToolCall.Kind != "" {
				toolKinds[parsed.ToolCall.Kind]++
			}
			for _, c := range parsed.ToolCall.Content {
				contentTypes[c.Type]++
			}
		}
	}

	t.Log("--- capture summary ---")
	logTally(t, "sessionUpdate kind", kinds)
	logTally(t, "toolCall.kind", toolKinds)
	logTally(t, "toolCall content type", contentTypes)
	logTally(t, "content block type", blockTypes)

	if contentTypes["diff"] == 0 {
		t.Log("NOTE: no `diff` tool content in this run — Agent / Tool Diff is still unproven")
	}
	if contentTypes["terminal"] == 0 {
		t.Log("NOTE: no `terminal` tool content in this run — Agent / Tool Terminal is still unproven")
	}
	if kinds[string(agenthub.UpdateUnknown)] > 0 {
		t.Log("NOTE: unmodelled kinds arrived; inspect the JSONL for their sessionUpdate values")
	}
}

func logTally(t *testing.T, label string, tally map[string]int) {
	t.Helper()
	if len(tally) == 0 {
		t.Logf("%s: none", label)
		return
	}
	keys := make([]string, 0, len(tally))
	for k := range tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%s: %-28s %d", label, k, tally[k])
	}
}

// wrapMeta and wrapPermission keep non-update traffic in the same file without
// letting it be mistaken for a session/update: both get a marker key that no
// ACP notification uses.
func wrapMeta(meta json.RawMessage) json.RawMessage {
	out, err := json.Marshal(map[string]json.RawMessage{"_captureMeta": meta})
	if err != nil {
		return json.RawMessage(`{"_captureMeta":null}`)
	}
	return out
}

func wrapPermission(params json.RawMessage) json.RawMessage {
	out, err := json.Marshal(map[string]json.RawMessage{"_capturePermission": params})
	if err != nil {
		return json.RawMessage(`{"_capturePermission":null}`)
	}
	return out
}

// allowOutcome picks the least-privileged allow option Claude offered. It reads
// the option list rather than hardcoding "allow" because the ids are vendor
// strings, and picking allow_always would leave a grant behind after the run.
func allowOutcome(params json.RawMessage) json.RawMessage {
	var shaped struct {
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
			Name     string `json:"name"`
		} `json:"options"`
	}
	_ = json.Unmarshal(params, &shaped)

	pick := ""
	for _, opt := range shaped.Options {
		if opt.Kind == "allow_once" {
			pick = opt.OptionID
			break
		}
		if pick == "" && opt.Kind == "allow_always" {
			pick = opt.OptionID
		}
	}
	if pick == "" && len(shaped.Options) > 0 {
		pick = shaped.Options[0].OptionID
	}
	out, err := json.Marshal(map[string]any{
		"outcome": map[string]any{"outcome": "selected", "optionId": pick},
	})
	if err != nil {
		return json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`)
	}
	return out
}
