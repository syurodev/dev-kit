package bridge

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"devkit/internal/app"
	"devkit/internal/preferences"
	"devkit/internal/tools"
)

type AppService struct {
	container       *app.Container
	applyTheme      func(preferences.Theme) error
	saveFileDialog  func(suggestedName string) (string, error)
	openDirDialog   func(title, startIn string) (string, error)
	openFilesDialog func(title, startIn string) ([]string, error)

	// sshKeys are the key files this window has been shown or has had chosen for
	// it, so reading one by path is limited to those.
	sshKeys sshKeyPaths
	// serverMode reports the unauthenticated HTTP build. Nil means a desktop one.
	serverMode       func() bool
	vaultLifecycleMu sync.Mutex
}

// errDialogUnavailable is what a native dialog answers when the shell supplied
// none (tests, a headless host).
var errDialogUnavailable = errors.New("dialog service unavailable")

// NativeHost is what the bridge needs from the window toolkit, handed in by the
// shell that owns the toolkit (the root package). The bridge itself imports no
// window toolkit, so the Go side can be hosted without one: a nil field means "not
// available here" and the method that needs it says so.
type NativeHost struct {
	// SaveFile asks the person where to save a file; the path, "" if cancelled.
	SaveFile func(suggestedName string) (string, error)
	// OpenFiles and OpenDirectory ask the person to choose files or a folder.
	OpenFiles     func(title, startIn string) ([]string, error)
	OpenDirectory func(title, startIn string) (string, error)
	// IsServer reports the unauthenticated HTTP build; nil means a desktop one.
	IsServer func() bool
}

// NewAppServiceWithHost is NewAppService for a shell that provides native
// dialogs and knows whether it is the server build.
func NewAppServiceWithHost(container *app.Container, themeApplier func(preferences.Theme) error, host NativeHost) *AppService {
	service := NewAppServiceWithMode(container, themeApplier, host.IsServer)
	service.saveFileDialog = host.SaveFile
	service.openFilesDialog = host.OpenFiles
	service.openDirDialog = host.OpenDirectory
	return service
}

func NewAppService(container *app.Container, themeApplier ...func(preferences.Theme) error) *AppService {
	service := &AppService{container: container}
	if len(themeApplier) > 0 {
		service.applyTheme = themeApplier[0]
	}
	return service
}

// NewAppServiceWithMode wires the trusted execution boundary used by methods
// whose desktop-only UI authority must stay unavailable in server mode.
func NewAppServiceWithMode(container *app.Container, themeApplier func(preferences.Theme) error, serverMode func() bool) *AppService {
	service := NewAppService(container, themeApplier)
	service.serverMode = serverMode
	return service
}

type ModuleDTO struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Version      string   `json:"version"`
	APIVersion   string   `json:"api_version"`
	Commands     []string `json:"commands"`
	Views        []string `json:"views"`
	Capabilities []string `json:"capabilities"`
}

type ExecuteCommandRequest struct {
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload"`
}

func (s *AppService) ListModules() []ModuleDTO {
	modules := s.container.Modules.List()
	result := make([]ModuleDTO, 0, len(modules))
	for _, module := range modules {
		result = append(result, ModuleDTO{
			ID:           module.ID,
			Title:        module.Title,
			Version:      module.Version,
			APIVersion:   module.APIVersion,
			Commands:     append([]string(nil), module.Commands...),
			Views:        append([]string(nil), module.Views...),
			Capabilities: append([]string(nil), module.Capabilities...),
		})
	}
	return result
}

func (s *AppService) ExecuteCommand(ctx context.Context, request ExecuteCommandRequest) (any, error) {
	// The capability comes from the command, not from a constant. Every command
	// used to be authorised as tools:hash, which was harmless while hash was
	// the only one and becomes an authorisation bug the moment it is not: a
	// command would run under a grant written for something else.
	capability, known := tools.CapabilityFor(request.ID)
	if !known {
		return nil, fmt.Errorf("command %q is not a tools command", request.ID)
	}
	payload := decodeCommandPayload(request)
	opaqueID := commandID(request.ID)
	return s.call(ctx, tools.SubjectID, capability, "command:"+opaqueID, map[string]any{
		"action":     "execute",
		"command_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		return s.container.Commands.Execute(callCtx, request.ID, payload)
	})
}

// decodeCommandPayload turns the untyped map the webview sends into the request
// shape the handler asserts. Missing keys read as empty rather than failing
// here: the service is what knows which of its fields are required, and it
// already reports that (an unsupported hash algorithm, an unsupported UUID
// version) in terms the caller can act on.
func decodeCommandPayload(request ExecuteCommandRequest) any {
	field := func(name string) string {
		value, ok := request.Payload[name]
		if !ok || value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
	if request.ID == tools.CommandHash {
		return tools.HashRequest{
			Algorithm: field("algorithm"),
			Input:     field("input"),
		}
	}
	return tools.UtilRequest{
		Input:   field("input"),
		Version: field("version"),
	}
}
