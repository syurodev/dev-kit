package codexapp

import (
	"encoding/json"
	"testing"
)

// items returns the item of every item/completed in session, by type.
func items(t *testing.T, lines []captureLine, session string) map[string][]json.RawMessage {
	t.Helper()
	out := map[string][]json.RawMessage{}
	for _, l := range lines {
		if l.Session != session || l.Msg.Method != MethodItemCompleted {
			continue
		}
		var n ItemNotification
		if err := json.Unmarshal(l.Msg.Params, &n); err != nil {
			t.Fatal(err)
		}
		var h ItemHeader
		if err := json.Unmarshal(n.Item, &h); err != nil || h.ID == "" {
			t.Fatalf("item header %s: %v", n.Item, err)
		}
		out[h.Type] = append(out[h.Type], n.Item)
	}
	return out
}

func TestCapturedItemsDecode(t *testing.T) {
	got := items(t, loadCapture(t), "main")

	var cmd CommandExecutionItem
	if err := json.Unmarshal(got[ItemCommandExecution][0], &cmd); err != nil {
		t.Fatal(err)
	}
	if cmd.Status != ItemCompleted || cmd.ExitCode == nil || *cmd.ExitCode != 0 ||
		cmd.AggregatedOutput == nil || len(cmd.CommandActions) == 0 || cmd.CommandActions[0].Command == "" {
		t.Fatalf("command item = %+v", cmd)
	}

	var fc FileChangeItem
	if err := json.Unmarshal(got[ItemFileChange][0], &fc); err != nil {
		t.Fatal(err)
	}
	if fc.Status != ItemCompleted || len(fc.Changes) != 2 {
		t.Fatalf("file change item = %+v", fc)
	}
	if fc.Changes[0].Kind.Type != "update" || fc.Changes[0].Diff != "@@ -2 +2,2 @@\n line two\n+line three\n" {
		t.Fatalf("update change = %+v", fc.Changes[0])
	}
	if fc.Changes[1].Kind.Type != "add" || fc.Changes[1].Kind.MovePath != nil {
		t.Fatalf("add change = %+v", fc.Changes[1])
	}

	var msg AgentMessageItem
	if err := json.Unmarshal(got[ItemAgentMessage][0], &msg); err != nil || msg.Text == "" {
		t.Fatalf("agent message = %+v, %v", msg, err)
	}
}

func TestCapturedApprovalAndTurnShapes(t *testing.T) {
	lines := loadCapture(t)
	var sawCommand, sawFile, sawUsage, sawInterrupted bool
	for _, l := range lines {
		switch l.Msg.Method {
		case MethodCommandApproval:
			var p CommandApprovalParams
			if err := json.Unmarshal(l.Msg.Params, &p); err != nil || p.ItemID == "" || len(p.AvailableDecisions) == 0 {
				t.Fatalf("command approval = %+v, %v", p, err)
			}
			sawCommand = true
		case MethodFileChangeApproval:
			var p FileChangeApprovalParams
			if err := json.Unmarshal(l.Msg.Params, &p); err != nil || p.ItemID == "" {
				t.Fatalf("file change approval = %+v, %v", p, err)
			}
			sawFile = true
		case MethodTokenUsageUpdated:
			var n TokenUsageUpdatedNotification
			if err := json.Unmarshal(l.Msg.Params, &n); err != nil || n.TokenUsage.Last.TotalTokens == 0 ||
				n.TokenUsage.ModelContextWindow == nil || *n.TokenUsage.ModelContextWindow == 0 {
				t.Fatalf("token usage = %+v, %v", n, err)
			}
			sawUsage = true
		case MethodTurnCompleted:
			var n TurnCompletedNotification
			if err := json.Unmarshal(l.Msg.Params, &n); err != nil {
				t.Fatal(err)
			}
			if n.Turn.Status == TurnInterrupted {
				sawInterrupted = true
			}
		}
	}
	if !sawCommand || !sawFile || !sawUsage || !sawInterrupted {
		t.Fatalf("capture coverage: command=%v file=%v usage=%v interrupted=%v", sawCommand, sawFile, sawUsage, sawInterrupted)
	}
}

func TestApprovalResponsesEncode(t *testing.T) {
	sameJSON(t, "approval", ApprovalResponse{Decision: DecisionDecline}, json.RawMessage(`{"decision":"decline"}`))
	sameJSON(t, "permissions", PermissionsApprovalResponse{Scope: "turn"}, json.RawMessage(`{"permissions":{},"scope":"turn"}`))
}
