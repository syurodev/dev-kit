package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/apiclient"
	"devkit/internal/app"
)

func newAPIClientBridge(t *testing.T) *AppService {
	t.Helper()
	container := app.NewContainer()
	if err := app.RegisterBuiltins(container); err != nil {
		t.Fatal(err)
	}
	service := NewAppService(container)
	if _, err := service.SetupVault("master-pass"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}
	return service
}

// Export to file is written from Go, private to the user, and holds no secret
// value — the content never had to cross the bridge to get there.
func TestApiExportToFileWritesPrivatelyWithoutSecrets(t *testing.T) {
	service := newAPIClientBridge(t)
	env, err := service.ApiCreateEnvironment("Staging", "stg", map[string]string{"baseUrl": "https://stg"}, map[string]string{"token": "s3cr3t"})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "out.json")
	var suggested string
	service.saveFileDialog = func(name string) (string, error) {
		suggested = name
		return target, nil
	}
	saved, err := service.ApiExportToFile(apiclient.ExportScope{Kind: apiclient.ExportEnvironment, ID: env.ID}, "postman")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Path != target || suggested != "Staging.environment.json" || len(saved.Warnings) != 1 {
		t.Fatalf("saved = %+v, suggested %q", saved, suggested)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	data, _ := os.ReadFile(target)
	if strings.Contains(string(data), "s3cr3t") || !strings.Contains(string(data), "https://stg") {
		t.Fatalf("file:\n%s", data)
	}

	// Cancelling the dialog writes nothing and is not an error.
	service.saveFileDialog = func(string) (string, error) { return "", nil }
	cancelled, err := service.ApiExportToFile(apiclient.ExportScope{Kind: apiclient.ExportGlobals}, "postman")
	if err != nil || cancelled.Path != "" {
		t.Fatalf("cancelled = %+v, %v", cancelled, err)
	}

	// The file comes back in through the generic import, tag read from its name.
	result, err := service.ApiImport("", string(data), apiclient.ImportOptions{})
	if err != nil || result.Format != "postman" || result.EnvironmentID == "" {
		t.Fatalf("import = %+v, %v", result, err)
	}
}

// Server mode has no save dialog; the person is told to copy instead of being
// handed "An unexpected error occurred."
func TestApiExportToFileWithoutADialogSaysCopy(t *testing.T) {
	service := newAPIClientBridge(t)
	service.saveFileDialog = func(string) (string, error) { return "", errors.New("dialog service unavailable") }
	_, err := service.ApiExportToFile(apiclient.ExportScope{Kind: apiclient.ExportGlobals}, "postman")
	if err == nil || !strings.Contains(err.Error(), "use Copy instead") {
		t.Fatalf("err = %v", err)
	}
}
