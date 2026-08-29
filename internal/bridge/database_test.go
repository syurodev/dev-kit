package bridge

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"devkit/internal/app"
	"devkit/internal/database"
	"devkit/internal/mcp"
)

func TestDBUpdateProfileOptionsAuditsAndPersists(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	if _, err := service.SetupVault("master-pass"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}

	created, err := service.DBCreateProfile(database.CreateProfileInput{
		Name:     "analytics-db",
		Host:     "127.0.0.1",
		Port:     5432,
		Database: "analytics",
		Username: "devkit",
		Password: "secret-password",
		SSLMode:  "disable",
		Env:      "dev",
	})
	if err != nil {
		t.Fatalf("DBCreateProfile: %v", err)
	}
	if created.ReadOnly {
		t.Fatal("expected default ReadOnly to be false")
	}

	// Update profile options (policy change to prod, enable read_only safe-mode)
	updated, err := service.DBUpdateProfileOptions(created.ID, database.UpdateProfileOptionsInput{
		Env:      "prod",
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("DBUpdateProfileOptions: %v", err)
	}
	if updated.Env != "prod" || !updated.ReadOnly {
		t.Fatalf("updated = %+v, want Env=prod and ReadOnly=true", updated)
	}

	// Check audit events
	events := container.Audit.Events()
	var foundAudit bool
	for _, event := range events {
		if event.SubjectID == "builtin.database" &&
			event.Capability == "db:query" &&
			event.Scope == "profile:"+recordID(created.ID) &&
			event.Metadata["action"] == "update_profile_options" {
			foundAudit = true
			if event.Outcome != "allowed" {
				t.Fatalf("expected audit outcome allowed, got %q", event.Outcome)
			}
			if event.Metadata["env"] != "prod" {
				t.Fatalf("audit event metadata env = %v, want prod", event.Metadata["env"])
			}
			if event.Metadata["read_only"] != true {
				t.Fatalf("audit event metadata read_only = %v, want true", event.Metadata["read_only"])
			}
		}
	}
	if !foundAudit {
		t.Fatalf("DBUpdateProfileOptions did not emit expected audit event; events = %#v", events)
	}
}

// Both methods below are human-only by design: DBUpdateProfileOptions can lower
// a profile's env tag and with it the confirmation requirement, and
// DBSaveExportFile writes arbitrary bytes to disk. Neither may ever reach an
// agent, so this enumerates the real tool list rather than trusting the wiring.
func TestDatabaseWriteMethodsAreAbsentFromMCPTools(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	server := mcp.NewServer(mcp.Deps{
		Caller: service,
		Envs:   container.Environments,
		DB:     container.Database,
	})
	if server == nil {
		t.Fatal("expected non-nil MCP server")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientTransport, serverTransport := sdk.NewInMemoryTransports()
	go func() { _ = server.MCP().Run(ctx, serverTransport) }()

	client := sdk.NewClient(&sdk.Implementation{Name: "mcp-surface-test", Version: "0.0.1"}, nil)
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

	// Guards the guard: if the database tools stop being registered at all, the
	// absence checks below would pass for the wrong reason.
	for _, required := range []string{
		"db.listProfiles", "db.history.list", "db.savedQuery.list",
		"db.listForeignKeys", "db.schema.get", "db.explain",
	} {
		if !names[required] {
			t.Fatalf("%s missing — the database tool surface did not register as expected, so the absence checks below prove nothing. tools = %v", required, names)
		}
	}

	for _, forbidden := range []string{
		"db.updateProfileOptions",
		"db.saveExportFile",
		"db.export",
		"db.deleteProfile",
		"db.createProfile",
		"db.setActiveProfile",
		// Deciding what is worth keeping is a human act.
		"db.saveQuery",
		"db.deleteSavedQuery",
		"db.history.delete",
		// ANALYZE executes the statement, and DevKit cannot tell whether a
		// statement mutates.
		"db.explainAnalyze",
		// Activity rows carry other people's SQL, literals included; cancel and
		// terminate end other people's work.
		"db.listActivity",
		"db.cancelQuery",
		"db.terminateSession",
		"db.indexSchema",
	} {
		if names[forbidden] {
			t.Errorf("%s is exposed over MCP but must stay human-only", forbidden)
		}
	}
}

func TestDBSaveExportFileAuditsAndWritesToDisk(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	tmpDir := t.TempDir()
	targetPath := tmpDir + "/exported_query.csv"
	var receivedName string

	service.saveFileDialog = func(suggestedName string) (string, error) {
		receivedName = suggestedName
		return targetPath, nil
	}

	content := "id,name\r\n1,Alice\r\n2,Bob"
	writtenPath, err := service.DBSaveExportFile("query-result.csv", content)
	if err != nil {
		t.Fatalf("DBSaveExportFile: %v", err)
	}
	if writtenPath != targetPath {
		t.Fatalf("writtenPath = %q, want %q", writtenPath, targetPath)
	}
	if receivedName != "query-result.csv" {
		t.Fatalf("receivedName = %q, want query-result.csv", receivedName)
	}

	// Verify file was actually written to disk with exact bytes
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != content {
		t.Fatalf("disk content = %q, want %q", string(data), content)
	}

	// Verify audit log
	events := container.Audit.Events()
	var foundAudit bool
	for _, event := range events {
		if event.SubjectID == "builtin.database" &&
			event.Capability == "db:query" &&
			event.Scope == "profile:export" &&
			event.Metadata["action"] == "save_export_file" {
			foundAudit = true
			if event.Outcome != "allowed" {
				t.Fatalf("expected audit outcome allowed, got %q", event.Outcome)
			}
			if event.Metadata["suggested_name"] != "query-result.csv" {
				t.Fatalf("audit suggested_name = %v, want query-result.csv", event.Metadata["suggested_name"])
			}
		}
	}
	if !foundAudit {
		t.Fatalf("expected audit event for save_export_file, got %#v", events)
	}

	// An export holds whatever the query returned, so it must not be
	// world-readable. Same posture as the vault database and the MCP socket.
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("export file mode = %#o, want 0600", perm)
	}

	// The audit trail must never carry the exported rows themselves.
	for _, event := range events {
		for key, value := range event.Metadata {
			if str, ok := value.(string); ok && strings.Contains(str, "Alice") {
				t.Fatalf("audit metadata %q leaked exported content: %q", key, str)
			}
		}
	}
}

func TestDBSaveExportFileUserCancelled(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)

	service.saveFileDialog = func(suggestedName string) (string, error) {
		return "", nil // user cancelled
	}

	writtenPath, err := service.DBSaveExportFile("query-result.csv", "test content")
	if err != nil {
		t.Fatalf("unexpected error on cancel: %v", err)
	}
	if writtenPath != "" {
		t.Fatalf("expected empty path on cancel, got %q", writtenPath)
	}
}
