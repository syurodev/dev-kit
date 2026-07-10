package main

import (
	"context"
	"embed"
	"log"
	"os"
	"path/filepath"

	"changeme/internal/app"
	"changeme/internal/bridge"
	"changeme/internal/servermode"

	"changeme/internal/services/database"
	"changeme/internal/services/notes"
	sshservice "changeme/internal/services/ssh"
	"changeme/internal/storage"
	"changeme/internal/vault"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if application.System.IsServer() {
		host, err := servermode.ValidateHost(os.Getenv("WAILS_SERVER_HOST"))
		if err != nil {
			log.Fatal(err)
		}
		if err := os.Setenv("WAILS_SERVER_HOST", host); err != nil {
			log.Fatal(err)
		}
	}

	// Persist the vault keyring so the Master Password set at first-run setup
	// survives restarts. Only wrapped key material is written to disk.
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	dataDir := filepath.Join(configDir, "DevKit")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(dataDir, "devkit.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(context.Background(), db); err != nil {
		log.Fatal(err)
	}

	container := app.NewContainerWithStores(
		vault.NewSQLKeyringStore(storage.NewKV(db, storage.TableVaultKeyring)),
		notes.NewSQLRepository(storage.NewKV(db, storage.TableNotes)),
		database.NewSQLRepository(storage.NewKV(db, storage.TableConnectionProfiles)),
		sshservice.NewSQLRepository(storage.NewKV(db, storage.TableSSHProfiles)),
	)
	defer container.Database.Close()
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
		Server: application.ServerOptions{
			Host: "127.0.0.1",
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
