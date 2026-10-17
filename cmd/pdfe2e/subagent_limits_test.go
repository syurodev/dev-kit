package main

// Measures what Codex's subagent limits really do, by running the real
// `codex app-server` (multi_agent_v2) against a scripted mock model — the same
// harness as subagent_test.go. The numbers in AGENTS.md ("Codex subagent
// limits") come from this file's log output.
//
//	DEVKIT_SUBAGENT_E2E_CODEX=<the native codex binary>
//	HOME=<a short fake home>
//	go test ./cmd/pdfe2e/ -run SubagentLimits -v -timeout 20m
//
// Each scenario hands a session extra `-c key=value` flags (exactly how DevKit
// applies them) and a script: how many subagents the root spawns, and how many
// each descendant spawns. The mock records, per request, which level of the
// tree asked, whether spawn_agent was offered, how many "concurrency slots" the
// developer message announced, and every spawn_agent result the model was given.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/preferences"
	"devkit/internal/sandbox"
)

type limitsScenario struct {
	name    string
	args    []string      // extra -c flags
	spawn   func(int) int // spawn_agent calls a level makes
	force   bool          // call spawn_agent even where it was not offered
	hold    time.Duration // how long an agent stays active before it answers
	wait    time.Duration // how long the root waits for the tree before ending
	pauseAt int           // the root waits `pause` before spawn number pauseAt, so earlier children can finish
	pause   time.Duration
}

type limitsEvent struct {
	Level    int
	Offered  bool
	Slots    string
	Tools    string
	Outputs  map[string]string // call id → what spawn_agent answered
	Finished bool
}

type limitsLog struct {
	mu     sync.Mutex
	events []limitsEvent
}

func (l *limitsLog) add(e limitsEvent) {
	l.mu.Lock()
	l.events = append(l.events, e)
	l.mu.Unlock()
}

var slotsRe = regexp.MustCompile(`There (?:are|is) (\d+) available concurrency slots?`)
var levelRe = regexp.MustCompile(`LEVEL-(\d+)`)

func limitsMock(sc limitsScenario, log *limitsLog) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			http.Error(w, "websocket transport is not offered", http.StatusUpgradeRequired)
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/responses") {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			if strings.HasSuffix(r.URL.Path, "/models") {
				_, _ = w.Write([]byte(`{"models":[],"data":[],"object":"list"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Input []json.RawMessage `json:"input"`
			Tools []json.RawMessage `json:"tools"`
		}
		_ = json.Unmarshal(raw, &req)

		var names []string
		namespace := ""
		var walk func(tools []json.RawMessage, ns string)
		walk = func(tools []json.RawMessage, ns string) {
			for _, tr := range tools {
				var t struct {
					Name  string            `json:"name"`
					Tools []json.RawMessage `json:"tools"`
				}
				_ = json.Unmarshal(tr, &t)
				names = append(names, t.Name)
				if t.Name == "spawn_agent" {
					namespace = ns
				}
				walk(t.Tools, t.Name)
			}
		}
		walk(req.Tools, "")

		level, isAgent := -1, false
		slots := ""
		outputs := map[string]string{}
		for _, rawItem := range req.Input {
			var item struct {
				Type    string            `json:"type"`
				Role    string            `json:"role"`
				CallID  string            `json:"call_id"`
				Output  string            `json:"output"`
				Tools   []json.RawMessage `json:"tools"`
				Content []struct {
					Text    string `json:"text"`
					Encrypt string `json:"encrypted_content"`
				} `json:"content"`
			}
			if json.Unmarshal(rawItem, &item) != nil {
				continue
			}
			switch item.Type {
			case "additional_tools":
				walk(item.Tools, "")
			case "function_call_output":
				outputs[item.CallID] = item.Output
			case "message":
				for _, c := range item.Content {
					if item.Role == "user" && strings.Contains(c.Text, rootMarker) {
						level, isAgent = 0, true
					}
					if m := slotsRe.FindStringSubmatch(c.Text); m != nil && slots == "" {
						slots = m[1]
					}
				}
			case "agent_message":
				for _, c := range item.Content {
					if m := levelRe.FindStringSubmatch(c.Encrypt + c.Text); m != nil {
						fmt.Sscanf(m[1], "%d", &level)
						isAgent = true
					}
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if !isAgent {
			openaiText(w, "ok")
			return
		}
		offered := false
		for _, n := range names {
			if n == "spawn_agent" {
				offered = true
			}
		}
		own := map[string]string{}
		prefix := fmt.Sprintf("call_L%d_", level)
		for id, out := range outputs {
			if strings.HasPrefix(id, prefix) {
				own[id] = out
			}
		}
		planned := sc.spawn(level)
		done := len(own)
		ev := limitsEvent{Level: level, Offered: offered, Slots: slots, Outputs: own, Tools: strings.Join(names, ",")}
		if done < planned && (offered || sc.force) {
			ev.Finished = false
			log.add(ev)
			if level == 0 && sc.pause > 0 && done == sc.pauseAt {
				time.Sleep(sc.pause)
			}
			id := fmt.Sprintf("%s%d", prefix, done)
			call := map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": id, "name": "spawn_agent", "status": "completed",
				"arguments": mustJSON(map[string]any{"fork_turns": "none", "task_name": fmt.Sprintf("l%d_w%d", level, done), "message": fmt.Sprintf("LEVEL-%d work item %d", level+1, done)})}
			if namespace != "" {
				call["namespace"] = namespace
			} else {
				call["namespace"] = "collaboration"
			}
			openaiCreated(w)
			sse(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": 0, "item": call})
			openaiCompleted(w, call)
			return
		}
		ev.Finished = true
		log.add(ev)
		if level == 0 {
			time.Sleep(sc.wait)
		} else {
			time.Sleep(sc.hold)
		}
		openaiText(w, fmt.Sprintf("level %d done", level))
	})
}

func limitsAdapter(t *testing.T, apiURL string, extra []string) *agenthub.CodexAdapter {
	bin := requireDir(t, "DEVKIT_SUBAGENT_E2E_CODEX")
	args := append([]string{"-c", `openai_base_url="` + apiURL + `/v1"`}, extra...)
	return agenthub.NewCodexAdapter(agenthub.CodexOptions{
		Command: bin,
		Home:    shortDir(t, "codexhome"),
		APIKey:  func(context.Context) (string, error) { return "sk-e2e", nil },
		Runner:  envRunner{inner: sandbox.DefaultRunner(), args: args},
		Version: "e2e",
	})
}

func runLimits(t *testing.T, sc limitsScenario) *limitsLog {
	log := &limitsLog{}
	api := newModelServer(t, func(*modelServer) http.Handler { return limitsMock(sc, log) })
	st := newStack(t, limitsAdapter(t, api.URL(), sc.args))
	sess := st.newSession(agenthub.AgentTypeCodex)
	res := st.invoke(sess, rootMarker+" do the work with subagents")
	t.Logf("[%s] turn status %q, args %v", sc.name, res.Status, sc.args)
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, e := range log.events {
		outs := make([]string, 0, len(e.Outputs))
		for id, o := range e.Outputs {
			outs = append(outs, id+"="+strings.TrimSpace(o))
		}
		t.Logf("[%s]   level %d offered=%v slots=%q finished=%v outputs=%v", sc.name, e.Level, e.Offered, e.Slots, e.Finished, outs)
		if e.Level == 0 && len(e.Outputs) == 0 {
			t.Logf("[%s]   root tools: %s", sc.name, e.Tools)
		}
	}
	return log
}

func chain(depth int) func(int) int {
	return func(level int) int {
		if level < depth {
			return 1
		}
		return 0
	}
}

func rootSpawns(n int) func(int) int {
	return func(level int) int {
		if level == 0 {
			return n
		}
		return 0
	}
}

func TestSubagentLimitsCount(t *testing.T) {
	for _, sc := range []limitsScenario{
		{name: "defaults/6", spawn: rootSpawns(6), hold: 12 * time.Second, wait: 20 * time.Second},
		{name: "agents.max_threads=2/4", args: []string{"-c", "agents.max_threads=2"}, spawn: rootSpawns(4), hold: 12 * time.Second, wait: 20 * time.Second},
		{name: "v2.max_concurrent=2/4", args: []string{"-c", "features.multi_agent_v2.max_concurrent_threads_per_session=2"}, spawn: rootSpawns(4), hold: 12 * time.Second, wait: 20 * time.Second},
	} {
		t.Run(sc.name, func(t *testing.T) { runLimits(t, sc) })
	}
}

func TestSubagentLimitsDepth(t *testing.T) {
	for _, sc := range []limitsScenario{
		{name: "defaults/chain4", spawn: chain(4), force: true, hold: 3 * time.Second, wait: 25 * time.Second},
		{name: "max_depth=2/chain4", args: []string{"-c", "agents.max_depth=2"}, spawn: chain(4), force: true, hold: 3 * time.Second, wait: 25 * time.Second},
		{name: "max_depth=3/chain4", args: []string{"-c", "agents.max_depth=3"}, spawn: chain(4), force: true, hold: 3 * time.Second, wait: 25 * time.Second},
	} {
		t.Run(sc.name, func(t *testing.T) { runLimits(t, sc) })
	}
}

// Depth with room to spare: the thread limit would otherwise end a chain first.
func TestSubagentLimitsDepthWithRoom(t *testing.T) {
	room := []string{"-c", "features.multi_agent_v2.max_concurrent_threads_per_session=20"}
	with := func(extra ...string) []string { return append(append([]string(nil), room...), extra...) }
	for _, sc := range []limitsScenario{
		{name: "room/default-depth/chain7", args: room, spawn: chain(7), force: true, hold: 2 * time.Second, wait: 30 * time.Second},
		{name: "room/max_depth=1/chain4", args: with("-c", "agents.max_depth=1"), spawn: chain(4), force: true, hold: 2 * time.Second, wait: 25 * time.Second},
		{name: "room/max_depth=2/chain4", args: with("-c", "agents.max_depth=2"), spawn: chain(4), force: true, hold: 2 * time.Second, wait: 25 * time.Second},
	} {
		t.Run(sc.name, func(t *testing.T) { runLimits(t, sc) })
	}
}

// Which key wins, whether a finished agent frees its slot, and how high a value goes.
func TestSubagentLimitsCountMore(t *testing.T) {
	for _, sc := range []limitsScenario{
		{name: "both/max_threads=5,v2=2/8", args: []string{"-c", "agents.max_threads=5", "-c", "features.multi_agent_v2.max_concurrent_threads_per_session=2"}, spawn: rootSpawns(8), hold: 12 * time.Second, wait: 20 * time.Second},
		{name: "max_threads=5/8", args: []string{"-c", "agents.max_threads=5"}, spawn: rootSpawns(8), hold: 12 * time.Second, wait: 20 * time.Second},
		{name: "defaults/finishing-children/8", spawn: rootSpawns(8), hold: 0, wait: 8 * time.Second},
		{name: "defaults/children-finish-then-spawn/5", spawn: rootSpawns(5), hold: 0, wait: 10 * time.Second, pauseAt: 3, pause: 6 * time.Second},
		{name: "max_threads=0/3", args: []string{"-c", "agents.max_threads=0"}, spawn: rootSpawns(3), hold: 5 * time.Second, wait: 8 * time.Second},
		{name: "v2=1/3", args: []string{"-c", "features.multi_agent_v2.max_concurrent_threads_per_session=1"}, spawn: rootSpawns(3), hold: 5 * time.Second, wait: 8 * time.Second},
		{name: "max_threads=500/12", args: []string{"-c", "agents.max_threads=500"}, spawn: rootSpawns(12), hold: 12 * time.Second, wait: 20 * time.Second},
	} {
		t.Run(sc.name, func(t *testing.T) { runLimits(t, sc) })
	}
}

// Other spellings, and the legacy (non-v2) mode, to see where max_depth bites.
func TestSubagentLimitsOtherKeys(t *testing.T) {
	for _, sc := range []limitsScenario{
		{name: "agents.max_concurrent=2/4", args: []string{"-c", "agents.max_concurrent_threads_per_session=2"}, spawn: rootSpawns(4), hold: 10 * time.Second, wait: 15 * time.Second},
		{name: "agents.max_concurrent=2,v2=3/4", args: []string{"-c", "agents.max_concurrent_threads_per_session=2", "-c", "features.multi_agent_v2.max_concurrent_threads_per_session=3"}, spawn: rootSpawns(4), hold: 10 * time.Second, wait: 15 * time.Second},
		{name: "room/max_depth=0/chain3", args: []string{"-c", "agents.max_depth=0", "-c", "agents.max_threads=20"}, spawn: chain(3), force: true, hold: 2 * time.Second, wait: 20 * time.Second},
	} {
		t.Run(sc.name, func(t *testing.T) { runLimits(t, sc) })
	}
}

// The path DevKit uses: Settings' cap → Service → the adapter's argv → the real
// Codex. A cap of 1 must let exactly one spawn_agent through, and the others
// must come back as the refusal the model reads.
func TestSubagentLimitFromSettingsReachesCodex(t *testing.T) {
	for _, tc := range []struct {
		limit    int
		wantOK   int
		wantSlot string
	}{{1, 1, "2"}, {0, 0, "1"}} {
		sc := limitsScenario{name: fmt.Sprintf("settings=%d", tc.limit), spawn: rootSpawns(3), hold: 8 * time.Second, wait: 12 * time.Second}
		log := &limitsLog{}
		api := newModelServer(t, func(*modelServer) http.Handler { return limitsMock(sc, log) })
		st := newStack(t, limitsAdapter(t, api.URL(), nil))
		limit := tc.limit
		st.svc.SubagentLimits = func() (preferences.AgentSubagentLimits, error) {
			return preferences.AgentSubagentLimits{Codex: preferences.CodexSubagentLimits{MaxSubagents: &limit}}, nil
		}
		sess := st.newSession(agenthub.AgentTypeCodex)
		st.invoke(sess, rootMarker+" do the work with subagents")

		log.mu.Lock()
		last := log.events[0]
		for _, e := range log.events {
			if e.Level == 0 && e.Finished {
				last = e
			}
		}
		log.mu.Unlock()
		ok, refused := 0, 0
		for _, out := range last.Outputs {
			switch {
			case strings.Contains(out, "agent thread limit reached"):
				refused++
			case strings.Contains(out, "task_name"):
				ok++
			}
		}
		t.Logf("[%s] slots=%q started=%d refused=%d", sc.name, last.Slots, ok, refused)
		if ok != tc.wantOK || refused != 3-tc.wantOK || last.Slots != tc.wantSlot {
			t.Errorf("[%s] started %d refused %d slots %q; want %d started, %d refused, slots %q", sc.name, ok, refused, last.Slots, tc.wantOK, 3-tc.wantOK, tc.wantSlot)
		}
	}
}
