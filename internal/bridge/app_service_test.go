package bridge

import (
	"context"
	"testing"

	"changeme/internal/app"
)

func newTestService(t *testing.T) *AppService {
	t.Helper()
	container := app.NewContainer()
	if err := app.RegisterBuiltins(container); err != nil {
		t.Fatalf("RegisterBuiltins returned error: %v", err)
	}
	return NewAppService(container)
}

func TestAppServiceListsModules(t *testing.T) {
	service := newTestService(t)
	modules := service.ListModules()

	if len(modules) != 4 {
		t.Fatalf("len(modules) = %d, want 4", len(modules))
	}
	found := map[string]bool{}
	for _, m := range modules {
		found[m.ID] = true
	}
	if !found["tools"] || !found["notes"] || !found["database"] || !found["ssh"] {
		t.Fatalf("modules = %+v, want tools, notes, database and ssh", modules)
	}
}

func TestAppServiceExecutesHashCommand(t *testing.T) {
	service := newTestService(t)
	result, err := service.ExecuteCommand(context.Background(), ExecuteCommandRequest{
		ID: "tools.hash",
		Payload: map[string]any{
			"algorithm": "sha256",
			"input":     "hello",
		},
	})
	if err != nil {
		t.Fatalf("ExecuteCommand returned error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
}
