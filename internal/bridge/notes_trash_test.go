package bridge

import (
	"context"
	"strings"
	"testing"
	"time"

	"devkit/internal/app"
	"devkit/internal/mcp"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Emptying the trash and deleting forever are the only irreversible acts in
// this module. They belong to the Trash screen and must stay off the agent
// surface, the same rule that keeps db.saveQuery's siblings off it.
func TestTrashToolsAreAbsentFromMCPTools(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	server := mcp.NewServer(mcp.Deps{
		Caller: NewCaller(service),
		Envs:   container.Environments,
		DB:     container.Database,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	go func() { _ = server.MCP().Run(ctx, serverTransport) }()

	client := sdk.NewClient(&sdk.Implementation{Name: "notes-surface-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	defer session.Close()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make(map[string]bool, len(listed.Tools))
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}

	// Guards the guard: if the notes tools stopped registering entirely, the
	// absence checks below would pass for the wrong reason.
	for _, required := range []string{"notes.list", "notes.delete", "notes.folder.list"} {
		if !names[required] {
			t.Fatalf("%s missing — the notes tool surface did not register as expected, so the absence checks prove nothing. tools = %v", required, names)
		}
	}

	for name := range names {
		lowered := strings.ToLower(name)
		if strings.Contains(lowered, "purge") || strings.Contains(lowered, "trash") ||
			strings.Contains(lowered, "restore") {
			t.Fatalf("tool %q puts the trash on the agent surface", name)
		}
	}
}
