package agenthub_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"devkit/internal/acp"
	"devkit/internal/agenthub"
)

// promptTexts is every text block the runtime was handed, in order.
func promptTexts(prompts [][]acp.ContentBlock) []string {
	var out []string
	for _, blocks := range prompts {
		for _, b := range blocks {
			if b.Type == "text" {
				out = append(out, b.Text)
			}
		}
	}
	return out
}

// A message from another session is written before the target's runtime exists.
// After a restart nothing has recorded what that runtime will serve, so the first
// rendering assumed the legacy launch and named the reply tool. The text the model
// reads must come from the runtime that receives it.
func TestPeerEnvelopeAfterRestartDoesNotNameAToolThePolicyHides(t *testing.T) {
	e := newLaunchEnv(t, policy)
	a, b := e.mustCreate(), e.mustCreate()

	// A new process on the same database: no in-memory record of what b was served.
	reg := agenthub.NewRegistry()
	reg.Register(e.adapter)
	restarted := agenthub.NewService(reg, agenthub.NewSQLStore(e.db), func(string, json.RawMessage) {})
	restarted.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	restarted.Harness = e.harness
	restarted.MCPEndpoints = policy(e.ev)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = restarted.WaitQuiet(ctx)
		_ = restarted.Shutdown(ctx)
	})

	if _, err := restarted.SendMessage(agentCtx(a.ID), agenthub.SendMessageInput{SessionID: b.ID, Message: "please review"}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := restarted.WaitQuiet(ctx); err != nil {
		t.Fatal(err)
	}

	var envelope string
	for _, text := range promptTexts(e.adapter.MockAdapter.LastRuntime().Prompts()) {
		if strings.Contains(text, "please review") {
			envelope = text
		}
	}
	if envelope == "" {
		t.Fatal("the resumed runtime was never handed the message")
	}
	if strings.Contains(envelope, "sendMessage") {
		t.Errorf("the envelope names a reply tool the prepared policy does not serve:\n%s", envelope)
	}
	if !strings.Contains(envelope, "not available in this session") {
		t.Errorf("the envelope does not say replying is unavailable:\n%s", envelope)
	}
}
