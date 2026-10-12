package telegramcontrol

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/workspace"
)

func withEffort(agents *fakeAgents, id string) {
	agents.models = append(agents.models, agenthub.ConfigOption{ID: id, Category: "thought_level", Type: "select", CurrentValue: json.RawMessage(`"low"`), Options: []agenthub.ConfigSelectOption{{Value: "low"}, {Value: "high"}}})
}
func TestEffortUsesAdvertisedVendorOption(t *testing.T) {
	for _, id := range []string{"reasoning_effort", "effort"} {
		t.Run(id, func(t *testing.T) {
			s, c, agents, messages := modelHarness(t)
			withEffort(agents, id)
			ctx := context.Background()
			s.handle(ctx, c, command("/effort"))
			if !strings.Contains((*messages)[0], "Hiện tại: low") {
				t.Fatal("no current effort")
			}
			s.handle(ctx, c, command("/effort invented"))
			if agents.modelChanges != 0 {
				t.Fatal("unadvertised effort accepted")
			}
			s.handle(ctx, c, command("/effort high"))
			if agents.changedOption != id || agents.changedModel != "high" {
				t.Fatal("wrong vendor option")
			}
		})
	}
}
func TestEffortMenuBecomesStaleAfterModelChange(t *testing.T) {
	s, c, agents, _ := modelHarness(t)
	withEffort(agents, "effort")
	ctx := context.Background()
	s.showSetting(ctx, c, "", "effort", false)
	var a action
	for _, item := range s.actions {
		if item.source == "effort" && item.value == "high" {
			a = item
		}
	}
	// Same effort choices, but the model changed on the desktop.
	agents.models[0].CurrentValue = json.RawMessage(`"b"`)
	if err := s.complete(ctx, c, a); err == nil {
		t.Fatal("effort menu from another model accepted")
	}
	if agents.modelChanges != 0 {
		t.Fatal("stale effort reached vendor")
	}
	agents.models[0].CurrentValue = json.RawMessage(`"a"`)
	agents.status = agenthub.SessionStatusRunning
	if err := s.complete(ctx, c, a); err == nil {
		t.Fatal("busy session effort accepted")
	}
}
func TestEffortUnsupportedByCurrentModel(t *testing.T) {
	s, c, agents, messages := modelHarness(t)
	s.handle(context.Background(), c, command("/effort high"))
	if agents.modelChanges != 0 || !strings.Contains((*messages)[0], "chưa cung cấp") {
		t.Fatal("invented effort for unsupported model")
	}
}

type creationWorkspaces struct{}

func (creationWorkspaces) ListWorkspaces() ([]workspace.Workspace, error) {
	return []workspace.Workspace{{ID: "ws", Name: "Workspace"}, {ID: "outside", Name: "Other"}}, nil
}
func (creationWorkspaces) ListServices(id string) ([]workspace.WorkspaceService, error) {
	return []workspace.WorkspaceService{{ID: "primary", WorkspaceID: id, Path: "/allowed/primary"}, {ID: "secondary", WorkspaceID: id, Path: "/allowed/secondary"}}, nil
}
func creationHarness(t *testing.T) (*Service, *Client, *fakeAgents) {
	s, c, agents, _ := modelHarness(t)
	s.deps.Workspaces = creationWorkspaces{}
	withEffort(agents, "reasoning_effort")
	return s, c, agents
}
func pressAction(t *testing.T, s *Service, c *Client, source, value string) (string, action) {
	t.Helper()
	for token, a := range s.actions {
		if a.source == source && (value == "" || a.value == value) {
			s.handle(context.Background(), c, callback(token, a, 7, 7))
			return token, a
		}
	}
	t.Fatalf("no action %s/%s", source, value)
	return "", action{}
}
func TestNewSessionWizardDoesNotCreateTwice(t *testing.T) {
	s, c, agents := creationHarness(t)
	ctx := context.Background()
	s.handle(ctx, c, command("/new"))
	if len(agents.creations) != 0 {
		t.Fatal("created before selection")
	}
	for _, a := range s.actions {
		if a.source == "new:workspace" && a.id == "outside" {
			t.Fatal("outside workspace offered")
		}
	}
	pressAction(t, s, c, "new:workspace", "")
	token, a := pressAction(t, s, c, "new:create", "codex")
	s.handle(ctx, c, callback(token, a, 7, 7))
	if len(agents.creations) != 1 {
		t.Fatalf("created %d sessions", len(agents.creations))
	}
	in := agents.creations[0]
	if in.WorkspaceID != "ws" || in.Cwd != "/allowed/primary" || in.AgentType != agenthub.AgentTypeCodex || len(in.ExtraReadWritePaths) != 0 {
		t.Fatalf("input=%+v", in)
	}
	pressAction(t, s, c, "model", "b")
	pressAction(t, s, c, "effort", "high")
	if agents.changedOption != "reasoning_effort" || agents.changedModel != "high" || s.selected != "created-session" {
		t.Fatal("wizard configuration failed")
	}
}
func TestNewSessionRechecksScopeAndSender(t *testing.T) {
	s, c, agents := creationHarness(t)
	u := command("/new codex ws")
	u.Message.From.ID = 99
	s.handle(context.Background(), c, u)
	if len(agents.creations) != 0 {
		t.Fatal("unpaired sender created session")
	}
	s.handle(context.Background(), c, command("/new"))
	pressAction(t, s, c, "new:workspace", "")
	s.state.Config.Workspaces = nil
	pressAction(t, s, c, "new:create", "codex")
	if len(agents.creations) != 0 {
		t.Fatal("revoked workspace created session")
	}
}
func TestNewSessionDirectCommandConfiguresModelAndEffort(t *testing.T) {
	s, c, agents := creationHarness(t)
	s.handle(context.Background(), c, command("/new codex ws b high"))
	if !slices.Contains(s.state.Config.SessionIDs, "created-session") {
		t.Fatal("Telegram-created session was not opted in")
	}
	if len(agents.creations) != 1 || agents.modelChanges != 2 || agents.changedOption != "reasoning_effort" || agents.changedModel != "high" {
		t.Fatal("direct configuration failed")
	}
	s.handle(context.Background(), c, command("/new unknown ws"))
	s.handle(context.Background(), c, command("/new codex outside"))
	if len(agents.creations) != 1 {
		t.Fatal("invalid creation accepted")
	}
}
