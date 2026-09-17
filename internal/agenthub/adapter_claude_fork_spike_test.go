package agenthub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/acp"
)

// TestClaudeACPForkSpike measures what rewind will stand on, against a real
// claude-agent-acp, before any of it is built: whether session/fork at a
// message works from a process that never loaded the source session, what the
// forked session remembers, whether a turn killed mid-tool can be forked, what
// a subagent's messageId looks like, whether a fork can reach behind a
// compaction, whether session/delete works on a session not loaded, and where
// agentFileChangeReport arrives.
//
// It runs the adapter directly, outside the jail: the questions are about the
// vendor's behaviour, not DevKit's sandbox. It spends real model turns.
//
//	DEVKIT_AGENT_FORK_SPIKE=1 DEVKIT_CLAUDE_ACP_BIN=<abs>/node_modules/.bin/claude-agent-acp \
//	  go test ./internal/agenthub/ -run TestClaudeACPForkSpike -v -timeout 30m
//
// DEVKIT_AGENT_FORK_SPIKE_ONLY=S1,S3 runs a subset.
func TestClaudeACPForkSpike(t *testing.T) {
	if os.Getenv("DEVKIT_AGENT_FORK_SPIKE") != "1" {
		t.Skip("set DEVKIT_AGENT_FORK_SPIKE=1 and DEVKIT_CLAUDE_ACP_BIN to run the fork spike")
	}
	bin := os.Getenv("DEVKIT_CLAUDE_ACP_BIN")
	if !filepath.IsAbs(bin) {
		t.Fatal("DEVKIT_CLAUDE_ACP_BIN must be an absolute path")
	}
	only := map[string]bool{}
	for _, s := range strings.Split(os.Getenv("DEVKIT_AGENT_FORK_SPIKE_ONLY"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			only[s] = true
		}
	}
	run := func(name string, fn func(t *testing.T)) {
		if len(only) > 0 && !only[name] {
			return
		}
		t.Run(name, fn)
	}
	cwd := t.TempDir()

	run("S1_fork_remembers_only_up_to_the_message", func(t *testing.T) {
		a := startSpikeAgent(t, bin)
		sid := a.newSession(t, cwd)
		m1 := a.prompt(t, sid, "Remember the word APPLE. Reply with just OK.", nil).lastAgentMessageID
		a.prompt(t, sid, "Also remember the word BANANA. Reply with just OK.", nil)
		a.close()

		b := startSpikeAgent(t, bin)
		var fork json.RawMessage
		err := b.conn.Call(b.ctx, "session/fork", map[string]any{
			"sessionId": sid, "cwd": cwd, "mcpServers": []any{},
			"_meta": map[string]any{"jetbrains": map[string]any{"air": map[string]any{"fork": map[string]any{"version": 1, "messageId": m1}}}},
		}, &fork)
		t.Logf("S1 fork at %s → err=%v result=%s", m1, err, fork)
		if err != nil {
			return
		}
		var res struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(fork, &res)
		var load json.RawMessage
		err = b.conn.Call(b.ctx, "session/load", map[string]any{"sessionId": res.SessionID, "cwd": cwd, "mcpServers": []any{}, "_meta": spikeMeta()}, &load)
		t.Logf("S1 load forked → err=%v result=%.600s", err, load)
		out := b.prompt(t, res.SessionID, "List every word I asked you to remember, comma separated, nothing else.", nil)
		t.Logf("S1 forked session answers: %q (want APPLE only)", out.text)

		// The original must still be usable.
		orig := b.loadAndPrompt(t, sid, cwd, "List every word I asked you to remember, comma separated, nothing else.")
		t.Logf("S1 original session answers: %q (want APPLE, BANANA)", orig)
		b.close()
	})

	run("S2_modes_and_config_after_fork_and_new", func(t *testing.T) {
		a := startSpikeAgent(t, bin)
		sid := a.newSession(t, cwd)
		err := a.conn.Call(a.ctx, "session/set_mode", map[string]any{"sessionId": sid, "modeId": "acceptEdits"}, nil)
		t.Logf("S2 set_mode acceptEdits → %v", err)
		m := a.prompt(t, sid, "Reply with just OK.", nil).lastAgentMessageID
		a.close()

		b := startSpikeAgent(t, bin)
		var fork struct {
			SessionID string `json:"sessionId"`
		}
		err = b.conn.Call(b.ctx, "session/fork", map[string]any{
			"sessionId": sid, "cwd": cwd, "mcpServers": []any{},
			"_meta": map[string]any{"jetbrains": map[string]any{"air": map[string]any{"fork": map[string]any{"version": 1, "messageId": m}}}},
		}, &fork)
		if err != nil {
			t.Logf("S2 fork err=%v", err)
			return
		}
		var load json.RawMessage
		_ = b.conn.Call(b.ctx, "session/load", map[string]any{"sessionId": fork.SessionID, "cwd": cwd, "mcpServers": []any{}, "_meta": spikeMeta()}, &load)
		t.Logf("S2 forked load result (modes): %.800s", load)
		time.Sleep(time.Second)
		for _, u := range b.updatesOf(fork.SessionID, "current_mode_update", "config_option_update") {
			t.Logf("S2 forked update: %.500s", u)
		}
		var fresh json.RawMessage
		_ = b.conn.Call(b.ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}, "_meta": spikeMeta()}, &fresh)
		t.Logf("S2 session/new result (modes): %.500s", fresh)
		b.close()
	})

	run("S3_fork_a_turn_killed_mid_tool", func(t *testing.T) {
		a := startSpikeAgent(t, bin)
		sid := a.newSession(t, cwd)
		a.prompt(t, sid, "Remember the word CHERRY. Reply with just OK.", nil)
		done := make(chan spikeTurn, 1)
		go func() {
			done <- a.prompt(t, sid, "First say the word STARTING, then run the shell command `sleep 30` with the Bash tool, then say DONE.", nil)
		}()
		var toolStarted bool
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) && !toolStarted {
			for _, u := range a.updatesOf(sid, "tool_call", "tool_call_update") {
				if strings.Contains(string(u), "sleep 30") || strings.Contains(string(u), `"execute"`) {
					toolStarted = true
				}
			}
			time.Sleep(200 * time.Millisecond)
		}
		mid := a.lastAgentMessageID(sid)
		t.Logf("S3 tool started=%v, last agent messageId before kill: %s", toolStarted, mid)
		_ = a.conn.Notify(a.ctx, "session/cancel", map[string]any{"sessionId": sid})
		a.kill()

		b := startSpikeAgent(t, bin)
		var fork struct {
			SessionID string `json:"sessionId"`
		}
		err := b.conn.Call(b.ctx, "session/fork", map[string]any{
			"sessionId": sid, "cwd": cwd, "mcpServers": []any{},
			"_meta": map[string]any{"jetbrains": map[string]any{"air": map[string]any{"fork": map[string]any{"version": 1, "messageId": mid}}}},
		}, &fork)
		t.Logf("S3 fork at %s → err=%v id=%s", mid, err, fork.SessionID)
		if err == nil {
			ans := b.loadAndPrompt(t, fork.SessionID, cwd, "Which word did I ask you to remember? Reply with just the word.")
			t.Logf("S3 forked session answers: %q (want CHERRY, and no error)", ans)
		}
		b.close()
	})

	run("S4_subagent_message_ids", func(t *testing.T) {
		a := startSpikeAgent(t, bin)
		sid := a.newSession(t, cwd)
		a.prompt(t, sid, "You must use the Agent tool (the subagent tool) exactly once, with a general-purpose subagent whose task is to write a two-sentence description of the colour blue. Do not answer yourself. After it returns, quote what it said.", nil)
		ids := map[string][]string{}
		for _, raw := range a.rawUpdates(sid) {
			var p struct {
				Meta   json.RawMessage `json:"_meta"`
				Update struct {
					Kind      string          `json:"sessionUpdate"`
					MessageID string          `json:"messageId"`
					Meta      json.RawMessage `json:"_meta"`
				} `json:"update"`
			}
			_ = json.Unmarshal(raw, &p)
			if p.Update.Kind == "agent_message_chunk" {
				key := fmt.Sprintf("params._meta=%s update._meta=%s", p.Meta, p.Update.Meta)
				if !contains(ids[key], p.Update.MessageID) {
					ids[key] = append(ids[key], p.Update.MessageID)
				}
			}
		}
		for k, v := range ids {
			t.Logf("S4 chunks with %.300s → messageIds %v", k, v)
		}
		tally := map[string]int{}
		for _, raw := range a.rawUpdates(sid) {
			var p struct {
				Update struct {
					Kind string `json:"sessionUpdate"`
				} `json:"update"`
			}
			_ = json.Unmarshal(raw, &p)
			tally[p.Update.Kind]++
			if strings.HasPrefix(p.Update.Kind, "subagent_") {
				t.Logf("S4 %s: %.400s", p.Update.Kind, raw)
			}
		}
		t.Logf("S4 kinds: %v", tally)
		a.mu.Lock()
		other := map[string]map[string]int{}
		otherIDs := map[string][]string{}
		for _, e := range a.events {
			var p struct {
				SessionID string `json:"sessionId"`
				Update    struct {
					Kind      string `json:"sessionUpdate"`
					MessageID string `json:"messageId"`
				} `json:"update"`
			}
			if json.Unmarshal(e.raw, &p) != nil || p.SessionID == sid || p.SessionID == "" {
				continue
			}
			if other[p.SessionID] == nil {
				other[p.SessionID] = map[string]int{}
			}
			other[p.SessionID][p.Update.Kind]++
			if p.Update.MessageID != "" && !contains(otherIDs[p.SessionID], p.Update.MessageID) {
				otherIDs[p.SessionID] = append(otherIDs[p.SessionID], p.Update.MessageID)
			}
		}
		a.mu.Unlock()
		for id, k := range other {
			t.Logf("S4 updates under other session %s: %v messageIds=%v", id, k, otherIDs[id])
		}
		for _, raw := range a.rawUpdates(sid) {
			if strings.Contains(string(raw), "agent_message_chunk") && strings.Contains(string(raw), "subagent") {
				t.Logf("S4 subagent-tagged chunk: %.400s", raw)
				break
			}
		}
		a.close()
	})

	run("S5_fork_behind_a_compaction", func(t *testing.T) {
		a := startSpikeAgent(t, bin)
		sid := a.newSession(t, cwd)
		m1 := a.prompt(t, sid, "Remember the word DATE. Reply with just OK.", nil).lastAgentMessageID
		a.prompt(t, sid, "Remember the word ELDER. Reply with just OK.", nil)
		c := a.prompt(t, sid, "/compact", nil)
		t.Logf("S5 /compact stop=%s", c.stopReason)
		a.prompt(t, sid, "Remember the word FIG. Reply with just OK.", nil)
		a.close()

		b := startSpikeAgent(t, bin)
		var fork struct {
			SessionID string `json:"sessionId"`
		}
		err := b.conn.Call(b.ctx, "session/fork", map[string]any{
			"sessionId": sid, "cwd": cwd, "mcpServers": []any{},
			"_meta": map[string]any{"jetbrains": map[string]any{"air": map[string]any{"fork": map[string]any{"version": 1, "messageId": m1}}}},
		}, &fork)
		t.Logf("S5 fork at pre-compaction %s → err=%v id=%s", m1, err, fork.SessionID)
		if err == nil {
			ans := b.loadAndPrompt(t, fork.SessionID, cwd, "List every word I asked you to remember, comma separated, nothing else.")
			t.Logf("S5 forked session answers: %q (want DATE only)", ans)
		}
		b.close()
	})

	run("S6_delete_a_session_not_loaded", func(t *testing.T) {
		a := startSpikeAgent(t, bin)
		sid := a.newSession(t, cwd)
		a.prompt(t, sid, "Reply with just OK.", nil)
		a.close()
		b := startSpikeAgent(t, bin)
		var res json.RawMessage
		err := b.conn.Call(b.ctx, "session/delete", map[string]any{"sessionId": sid, "cwd": cwd}, &res)
		t.Logf("S6 delete not-loaded → err=%v result=%s", err, res)
		var load json.RawMessage
		err = b.conn.Call(b.ctx, "session/load", map[string]any{"sessionId": sid, "cwd": cwd, "mcpServers": []any{}, "_meta": spikeMeta()}, &load)
		t.Logf("S6 load after delete → err=%v", err)
		b.close()
	})

	run("S7_file_change_report", func(t *testing.T) {
		a := startSpikeAgent(t, bin)
		sid := a.newSession(t, cwd)
		meta := map[string]any{"jetbrains": map[string]any{"air": map[string]any{"agentFileChangeReportRequest": map[string]any{"version": 1, "requestId": "req-s7-1"}}}}
		turn := a.prompt(t, sid, "Use the Write tool to create a.txt containing x. Then use the Bash tool to run `echo y > b.txt`. Reply DONE.", meta)
		t.Logf("S7 prompt returned at %s, stop=%s, result=%.300s", turn.returnedAt.Format(time.StampMilli), turn.stopReason, turn.result)
		for _, e := range a.timeline(sid) {
			if strings.Contains(string(e.raw), "agentFileChangeReport") {
				t.Logf("S7 report at %s: %.900s", e.at.Format(time.StampMilli), e.raw)
			}
		}
		// A cancelled turn.
		meta2 := map[string]any{"jetbrains": map[string]any{"air": map[string]any{"agentFileChangeReportRequest": map[string]any{"version": 1, "requestId": "req-s7-2"}}}}
		go func() {
			time.Sleep(4 * time.Second)
			_ = a.conn.Notify(a.ctx, "session/cancel", map[string]any{"sessionId": sid})
		}()
		c := a.prompt(t, sid, "Run the shell command `sleep 20` with the Bash tool, then reply DONE.", meta2)
		time.Sleep(3 * time.Second)
		t.Logf("S7 cancelled prompt stop=%s at %s", c.stopReason, c.returnedAt.Format(time.StampMilli))
		for _, e := range a.timeline(sid) {
			if strings.Contains(string(e.raw), "req-s7-2") {
				t.Logf("S7 cancelled report at %s: %.600s", e.at.Format(time.StampMilli), e.raw)
			}
		}
		a.close()
	})
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func spikeMeta() map[string]any {
	return map[string]any{"claudeCode": map[string]any{"options": map[string]any{"settingSources": []string{}}}}
}

type spikeEvent struct {
	at  time.Time
	raw json.RawMessage
}

type spikeTurn struct {
	stopReason         string
	result             json.RawMessage
	text               string
	lastAgentMessageID string
	returnedAt         time.Time
}

type spikeAgent struct {
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd
	conn   *acp.Conn

	mu      sync.Mutex
	events  []spikeEvent
	unknown []string
}

type spikeRW struct {
	io.Reader
	io.WriteCloser
}

func startSpikeAgent(t *testing.T, bin string) *spikeAgent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	cmd := exec.CommandContext(ctx, bin)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start: %v", err)
	}
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			_ = sc.Text()
		}
	}()
	a := &spikeAgent{ctx: ctx, cancel: cancel, cmd: cmd}
	a.conn = acp.NewConn(spikeRW{stdout, stdin}, a)
	go func() { _ = a.conn.Serve(ctx) }()

	// The spike always asks for the report: measuring S7 is the point of it.
	caps := devkitClientCapabilities(true)
	var init json.RawMessage
	if err := a.conn.Call(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": caps}, &init); err != nil {
		a.close()
		t.Fatalf("initialize: %v", err)
	}
	return a
}

func (a *spikeAgent) close() {
	_ = a.conn.Close()
	a.cancel()
	_ = a.cmd.Wait()
}

func (a *spikeAgent) kill() {
	if a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
	a.close()
}

func (a *spikeAgent) newSession(t *testing.T, cwd string) string {
	t.Helper()
	var res struct {
		SessionID string `json:"sessionId"`
	}
	if err := a.conn.Call(a.ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}, "_meta": spikeMeta()}, &res); err != nil {
		t.Fatalf("session/new: %v", err)
	}
	return res.SessionID
}

func (a *spikeAgent) loadAndPrompt(t *testing.T, sid, cwd, text string) string {
	t.Helper()
	var load json.RawMessage
	if err := a.conn.Call(a.ctx, "session/load", map[string]any{"sessionId": sid, "cwd": cwd, "mcpServers": []any{}, "_meta": spikeMeta()}, &load); err != nil {
		return "load error: " + err.Error()
	}
	return a.prompt(t, sid, text, nil).text
}

func (a *spikeAgent) prompt(t *testing.T, sid, text string, meta map[string]any) spikeTurn {
	t.Helper()
	a.mu.Lock()
	start := len(a.events)
	a.mu.Unlock()
	req := map[string]any{"sessionId": sid, "prompt": []map[string]any{{"type": "text", "text": text}}}
	if meta != nil {
		req["_meta"] = meta
	}
	var res json.RawMessage
	err := a.conn.Call(a.ctx, "session/prompt", req, &res)
	turn := spikeTurn{result: res, returnedAt: time.Now()}
	if err != nil {
		turn.stopReason = "error: " + err.Error()
	} else {
		var r struct {
			StopReason string `json:"stopReason"`
		}
		_ = json.Unmarshal(res, &r)
		turn.stopReason = r.StopReason
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var sb strings.Builder
	for _, e := range a.events[start:] {
		var p struct {
			SessionID string `json:"sessionId"`
			Update    struct {
				Kind      string `json:"sessionUpdate"`
				MessageID string `json:"messageId"`
				Content   struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"update"`
		}
		if json.Unmarshal(e.raw, &p) != nil || p.SessionID != sid {
			continue
		}
		if p.Update.Kind == "agent_message_chunk" {
			sb.WriteString(p.Update.Content.Text)
			if p.Update.MessageID != "" {
				turn.lastAgentMessageID = p.Update.MessageID
			}
		}
	}
	turn.text = strings.TrimSpace(sb.String())
	return turn
}

func (a *spikeAgent) rawUpdates(sid string) []json.RawMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []json.RawMessage
	for _, e := range a.events {
		if strings.Contains(string(e.raw), sid) {
			out = append(out, e.raw)
		}
	}
	return out
}

func (a *spikeAgent) timeline(sid string) []spikeEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []spikeEvent
	for _, e := range a.events {
		if strings.Contains(string(e.raw), sid) {
			out = append(out, e)
		}
	}
	return out
}

func (a *spikeAgent) updatesOf(sid string, kinds ...string) []json.RawMessage {
	var out []json.RawMessage
	for _, raw := range a.rawUpdates(sid) {
		for _, k := range kinds {
			if strings.Contains(string(raw), `"sessionUpdate":"`+k+`"`) {
				out = append(out, raw)
			}
		}
	}
	return out
}

func (a *spikeAgent) lastAgentMessageID(sid string) string {
	var id string
	for _, raw := range a.updatesOf(sid, "agent_message_chunk") {
		var p struct {
			Update struct {
				MessageID string `json:"messageId"`
			} `json:"update"`
		}
		if json.Unmarshal(raw, &p) == nil && p.Update.MessageID != "" {
			id = p.Update.MessageID
		}
	}
	return id
}

// acp.ClientHandler: allow every permission, record every update.

func (a *spikeAgent) RequestPermission(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Options []struct {
			OptionID string `json:"optionId"`
			Kind     string `json:"kind"`
		} `json:"options"`
	}
	_ = json.Unmarshal(params, &p)
	choice := ""
	for _, o := range p.Options {
		if strings.HasPrefix(o.Kind, "allow") {
			choice = o.OptionID
			break
		}
	}
	return json.Marshal(map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": choice}})
}

func (a *spikeAgent) SessionUpdate(_ context.Context, params json.RawMessage) error {
	a.mu.Lock()
	a.events = append(a.events, spikeEvent{at: time.Now(), raw: append(json.RawMessage(nil), params...)})
	a.mu.Unlock()
	return nil
}

func (a *spikeAgent) UnknownMethod(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	a.mu.Lock()
	a.unknown = append(a.unknown, method)
	a.events = append(a.events, spikeEvent{at: time.Now(), raw: append(json.RawMessage(`{"_method":"`+method+`","params":`), append(params, '}')...)})
	a.mu.Unlock()
	return nil, fmt.Errorf("unsupported method %q", method)
}
