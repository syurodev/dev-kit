package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"changeme/internal/app"
	"changeme/internal/bridge"

	"changeme/internal/services/notes"
	"changeme/internal/vault"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Persist the vault keyring so the Master Password set at first-run setup
	// survives restarts. Only wrapped key material is written to disk.
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	dataDir := filepath.Join(configDir, "DevKit")
	keyringPath := filepath.Join(dataDir, "keyring.json")
	notesPath := filepath.Join(dataDir, "notes.json")

	container := app.NewContainerWithStores(
		vault.NewFileKeyringStore(keyringPath),
		notes.NewFileRepository(notesPath),
	)
	if err := app.RegisterBuiltins(container); err != nil {
		log.Fatal(err)
	}

	appService := bridge.NewAppService(container)

	wailsApp := application.New(application.Options{
		Name:        "DevKit",
		Description: "Local-first developer toolkit",
		Services: []application.Service{
			application.NewService(appService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "DevKit",
		Width:  1200,
		Height: 760,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(16, 18, 20),
		URL:              "/",
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}
