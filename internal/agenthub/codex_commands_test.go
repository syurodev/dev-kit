package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/codexapp"
)

func commandNames(cs []AvailableCommand) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

// Codex announces no commands of its own; DevKit announces the ones it runs, in
// the shape Claude's arrive in, once per process.
func TestCodexAnnouncesItsSessionCommands(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt := f.open(t)
	var log updateLog
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add}) // a second attach must not announce again

	var announced []SessionUpdate
	for _, raw := range log.updates {
		if u := ParseSessionUpdate(raw); u.Kind == UpdateAvailableCommands {
			announced = append(announced, u)
		}
	}
	if len(announced) != 1 {
		t.Fatalf("announced %d times, want once: %v", len(announced), log.kinds())
	}
	cmds := announced[0].Commands
	if !slices.Equal(commandNames(cmds), []string{"compact", "review"}) {
		t.Fatalf("commands = %v", commandNames(cmds))
	}
	if cmds[0].Input != nil {
		t.Errorf("/compact takes no text, but advertises a hint %+v", cmds[0].Input)
	}
	if cmds[1].Input == nil || !strings.Contains(cmds[1].Input.Hint, "base <branch>") {
		t.Errorf("/review must say what it takes: %+v", cmds[1])
	}
	if !slices.Contains(sessionStateKinds, UpdateAvailableCommands) {
		t.Fatal("the announcement would not be kept as session state")
	}
	for _, c := range cmds {
		if cmd, _, ok := codexCommandOf(TextPrompt("/" + c.Name)); !ok || cmd.Name != c.Name {
			t.Errorf("/%s is announced but not implemented", c.Name)
		}
	}
}

func TestCodexCompactRunsTheCommandNotATurn(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt := f.open(t)
	var log updateLog
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})
	// Something to compact first, so the meter has a figure to replace.
	if _, err := rt.Prompt(context.Background(), TextPrompt("make x"), nil); err != nil {
		t.Fatal(err)
	}
	mark := len(log.updates)

	out, err := rt.Prompt(context.Background(), TextPrompt("/compact"), nil)
	if err != nil || out.StopReason != "end_turn" {
		t.Fatalf("/compact = %+v, %v", out, err)
	}
	if n := len(f.runner.sent(t, "turn/start")); n != 1 {
		t.Fatalf("/compact sent %d turn/starts in all, want only the first prompt's", n)
	}
	var params codexapp.ThreadCompactStartParams
	sent := f.runner.sent(t, "thread/compact/start")
	if len(sent) != 1 || json.Unmarshal(sent[0], &params) != nil || params.ThreadID != "th-new" {
		t.Fatalf("thread/compact/start = %s", sent)
	}

	// One consistent card with Claude's: a `compact` call, started and completed.
	var started, completed, usage bool
	var lastKind UpdateKind
	for _, raw := range log.updates[mark:] {
		u := ParseSessionUpdate(raw)
		lastKind = u.Kind
		switch {
		case u.Kind == UpdateToolCall && u.ToolCall.Name == "compact":
			started = u.ToolCall.Status == "in_progress" && u.ToolCall.Title == "Compact conversation" && u.ToolCall.Kind == "think"
		case u.Kind == UpdateToolCallUpdate && u.ToolCall.ID == "cc-1":
			completed = u.ToolCall.Status == "completed"
		case u.Kind == UpdateUsage:
			usage = true
		}
	}
	if !started || !completed {
		t.Fatalf("no compact card (started=%v completed=%v):\n%s", started, completed, log.joined())
	}
	if !usage || lastKind != UpdateUsage {
		t.Fatalf("the context meter was not published after the compaction (last update %q)", lastKind)
	}
	if strings.Contains(log.joined(), `"title":"contextCompaction"`) {
		t.Fatal("the compaction drew the generic unknown-item card")
	}
	// The window shrank: the meter reads the new figure.
	last := ParseSessionUpdate(log.updates[len(log.updates)-1])
	if last.Usage == nil || last.Usage.Used != 70 {
		t.Fatalf("usage after compaction = %+v", last.Usage)
	}
}

// Only a prompt that is exactly a command is one.
func TestCodexOrdinaryTextWithASlashIsAPrompt(t *testing.T) {
	for _, text := range []string{"see /compact", "/etc/hosts", "/unknown thing", "//compact", "please /review", "/"} {
		f := newCodexFixture(t, "turn")
		rt := f.open(t)
		if _, err := rt.Prompt(context.Background(), TextPrompt(text), nil); err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if len(f.runner.sent(t, "turn/start")) != 1 || len(f.runner.sent(t, "thread/compact/start")) != 0 || len(f.runner.sent(t, "review/start")) != 0 {
			t.Errorf("%q was not sent as a plain turn", text)
		}
	}
	// A command with something attached is a message about the attachment.
	f := newCodexFixture(t, "turn")
	rt := f.open(t)
	withImage := []acp.ContentBlock{{Type: "text", Text: "/compact"}, {Type: "image", Data: "AAAA", MimeType: "image/png"}}
	if _, err := rt.Prompt(context.Background(), withImage, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.runner.sent(t, "turn/start")) != 1 || len(f.runner.sent(t, "thread/compact/start")) != 0 {
		t.Error("a command with an attachment ran as a command")
	}
}

func TestCodexCompactTakesNoText(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt := f.open(t)
	_, err := rt.Prompt(context.Background(), TextPrompt("/compact keep the schema"), nil)
	if !errors.Is(err, ErrInvalidPrompt) {
		t.Fatalf("err = %v, want ErrInvalidPrompt", err)
	}
	if len(f.runner.sent(t, "turn/start"))+len(f.runner.sent(t, "thread/compact/start")) != 0 {
		t.Fatal("a refused command reached the app-server")
	}
}

// Codex treats a second start as an interrupt of the running turn (measured),
// so a command during a turn must be refused before it reaches the app-server.
func TestCodexCommandsRefuseABusyThread(t *testing.T) {
	f := newCodexFixture(t, "interrupt")
	rt := f.open(t)
	started := make(chan struct{}, 1)
	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(u, _ json.RawMessage) {
		if ParseSessionUpdate(u).Kind == UpdateToolCall {
			started <- struct{}{}
		}
	}})
	done := make(chan error, 1)
	go func() {
		_, err := rt.Prompt(context.Background(), TextPrompt("sleep"), nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never started")
	}
	for _, text := range []string{"/compact", "/review", "/review base main"} {
		if _, err := rt.Prompt(context.Background(), TextPrompt(text), nil); !errors.Is(err, ErrSessionBusy) {
			t.Errorf("%s during a turn: err = %v, want ErrSessionBusy", text, err)
		}
	}
	if len(f.runner.sent(t, "thread/compact/start"))+len(f.runner.sent(t, "review/start")) != 0 {
		t.Fatal("a command was sent beside the running turn")
	}
	_ = rt.Cancel(context.Background())
	if err := <-done; err != nil {
		t.Fatalf("the running turn: %v", err)
	}
	if len(f.runner.sent(t, "turn/interrupt")) != 1 {
		t.Fatalf("interrupts = %d", len(f.runner.sent(t, "turn/interrupt")))
	}
}

func TestCodexCompactRefusedByTheAppServerIsNotACrash(t *testing.T) {
	f := newCodexFixture(t, "compactRefused")
	rt := f.open(t)
	_, err := rt.Prompt(context.Background(), TextPrompt("/compact"), nil)
	if !errors.Is(err, ErrTurnFailed) || errors.Is(err, ErrProcessFailed) {
		t.Fatalf("err = %v, want a refused turn rather than a failed process", err)
	}
	if !strings.Contains(err.Error(), "invalid thread id") {
		t.Errorf("the vendor's reason is lost: %v", err)
	}
	if dead, _ := rt.(RuntimeLiveness).Dead(); dead {
		t.Fatal("a refused command killed the session")
	}
	// The thread is free again.
	if _, err := rt.Prompt(context.Background(), TextPrompt("still here"), nil); err != nil {
		t.Fatalf("a prompt after the refusal: %v", err)
	}
}

func TestCodexCompactThatNeverStartsDoesNotHang(t *testing.T) {
	defer func(old time.Duration) { codexAdoptWait = old }(codexAdoptWait)
	codexAdoptWait = 150 * time.Millisecond
	f := newCodexFixture(t, "compactSilent")
	rt := f.open(t)
	done := make(chan error, 1)
	go func() {
		_, err := rt.Prompt(context.Background(), TextPrompt("/compact"), nil)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrTurnFailed) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("/compact hung on a turn that never started")
	}
}

func TestCodexStopEndsACompaction(t *testing.T) {
	f := newCodexFixture(t, "compactHangs")
	rt := f.open(t)
	started := make(chan struct{}, 1)
	rt.SetHandlers(RuntimeHandlers{OnUpdate: func(u, _ json.RawMessage) {
		if ParseSessionUpdate(u).Kind == UpdateToolCall {
			started <- struct{}{}
		}
	}})
	type result struct {
		out PromptOutcome
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := rt.Prompt(context.Background(), TextPrompt("/compact"), nil)
		done <- result{out, err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the compaction never started")
	}
	_ = rt.Cancel(context.Background())
	select {
	case r := <-done:
		if r.err != nil || r.out.StopReason != "cancelled" {
			t.Fatalf("Prompt = %+v, %v; want a cancelled stop", r.out, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not end the compaction")
	}
	var p codexapp.TurnInterruptParams
	sent := f.runner.sent(t, "turn/interrupt")
	if len(sent) != 1 || json.Unmarshal(sent[0], &p) != nil || !strings.HasPrefix(p.TurnID, "tu-c") {
		t.Fatalf("turn/interrupt = %s, want it to name the compaction's turn", sent)
	}
}

func TestCodexReviewRunsTheReview(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt := f.open(t)
	var log updateLog
	rt.SetHandlers(RuntimeHandlers{OnUpdate: log.add})

	out, err := rt.Prompt(context.Background(), TextPrompt("/review"), nil)
	if err != nil || out.StopReason != "end_turn" {
		t.Fatalf("/review = %+v, %v", out, err)
	}
	if len(f.runner.sent(t, "turn/start")) != 0 {
		t.Fatal("/review went to the model as a prompt")
	}
	var params codexapp.ReviewStartParams
	sent := f.runner.sent(t, "review/start")
	if len(sent) != 1 || json.Unmarshal(sent[0], &params) != nil {
		t.Fatalf("review/start = %s", sent)
	}
	if params.ThreadID != "th-new" || params.Delivery != "inline" || params.Target.Type != "uncommittedChanges" {
		t.Fatalf("params = %+v", params)
	}

	joined := log.joined()
	if !strings.Contains(joined, `"title":"Review: current changes"`) {
		t.Errorf("no review card:\n%s", joined)
	}
	if strings.Count(joined, "One blocking issue.") != 1 {
		t.Errorf("the findings must be drawn once (the agent message), not also as the exitedReviewMode item:\n%s", joined)
	}
	if strings.Contains(joined, "exitedReviewMode") || strings.Contains(joined, "enteredReviewMode") {
		t.Errorf("a review item drew the generic unknown-item card:\n%s", joined)
	}
	// Every card the review opened is closed: nothing spins.
	for _, raw := range log.updates {
		if u := ParseSessionUpdate(raw); u.Kind == UpdateToolCall && u.ToolCall.Name == "review" && u.ToolCall.Status != "in_progress" {
			t.Errorf("review card started as %q", u.ToolCall.Status)
		}
	}
	if !strings.Contains(joined, `"toolCallId":"er-1","status":"completed"`) && !strings.Contains(joined, `"status":"completed","toolCallId":"er-1"`) {
		t.Errorf("the review card was never completed:\n%s", joined)
	}
}

func TestCodexReviewTargets(t *testing.T) {
	cases := []struct {
		args string
		want codexapp.ReviewTarget
		err  bool
	}{
		{"", codexapp.ReviewTarget{Type: "uncommittedChanges"}, false},
		{"base main", codexapp.ReviewTarget{Type: "baseBranch", Branch: "main"}, false},
		{"BASE origin/release-1.2", codexapp.ReviewTarget{Type: "baseBranch", Branch: "origin/release-1.2"}, false},
		{"commit 52d4e2c", codexapp.ReviewTarget{Type: "commit", SHA: "52d4e2c"}, false},
		// Two words are a branch or a commit; a sentence that starts with the word is not.
		{"base on the style guide", codexapp.ReviewTarget{Type: "custom", Instructions: "base on the style guide"}, false},
		{"commit message quality", codexapp.ReviewTarget{Type: "custom", Instructions: "commit message quality"}, false},
		{"look only at naming", codexapp.ReviewTarget{Type: "custom", Instructions: "look only at naming"}, false},
		// A name that is not a ref is refused, not handed to the model's shell.
		{"base $(whoami)", codexapp.ReviewTarget{}, true},
		{"base -rf", codexapp.ReviewTarget{}, true},
	}
	for _, c := range cases {
		got, err := codexReviewTarget(c.args)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("/review %q = %+v, %v; want %+v (err %v)", c.args, got, err, c.want, c.err)
		}
		if err != nil && !errors.Is(err, ErrInvalidPrompt) {
			t.Errorf("/review %q: %v is not ErrInvalidPrompt", c.args, err)
		}
	}
}

func TestCodexReviewPassesTheTarget(t *testing.T) {
	f := newCodexFixture(t, "turn")
	rt := f.open(t)
	if _, err := rt.Prompt(context.Background(), TextPrompt("/review base main"), nil); err != nil {
		t.Fatal(err)
	}
	var params codexapp.ReviewStartParams
	_ = json.Unmarshal(f.runner.sent(t, "review/start")[0], &params)
	if params.Target.Type != "baseBranch" || params.Target.Branch != "main" {
		t.Fatalf("target = %+v", params.Target)
	}
}

// The reading Service uses to leave a command unwrapped.
func TestSlashCommandOf(t *testing.T) {
	cases := []struct {
		blocks    []acp.ContentBlock
		name      string
		args      string
		isCommand bool
	}{
		{TextPrompt("/compact"), "compact", "", true},
		{TextPrompt("  /Compact  "), "compact", "", true},
		{TextPrompt("/review base main"), "review", "base main", true},
		{TextPrompt("/review\nlook at naming"), "review", "look at naming", true},
		{TextPrompt("/mcp:server:cmd arg"), "mcp:server:cmd", "arg", true},
		{TextPrompt("/etc/hosts"), "", "", false},
		{TextPrompt("//x"), "", "", false},
		{TextPrompt("/"), "", "", false},
		{TextPrompt("fix /compact"), "", "", false},
		{TextPrompt(""), "", "", false},
		{[]acp.ContentBlock{{Type: "text", Text: "/compact"}, {Type: "text", Text: "more"}}, "", "", false},
		{[]acp.ContentBlock{{Type: "image", Data: "x"}}, "", "", false},
	}
	for _, c := range cases {
		name, args, ok := slashCommandOf(c.blocks)
		if ok != c.isCommand || name != c.name || args != c.args {
			t.Errorf("%+v = (%q, %q, %v), want (%q, %q, %v)", c.blocks, name, args, ok, c.name, c.args, c.isCommand)
		}
	}
}
