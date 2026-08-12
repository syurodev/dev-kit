package main

import (
	"context"
	"embed"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/app"
	"devkit/internal/auth"
	"devkit/internal/bridge"
	"devkit/internal/cloudconfig"
	"devkit/internal/environments"
	"devkit/internal/interaction"
	"devkit/internal/mcp"
	"devkit/internal/servermode"

	"devkit/internal/storage"
	"devkit/internal/syncclient"
	"devkit/internal/syncconflict"
	"devkit/internal/syncstore"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"

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
		// Production local writes use one transaction for the vault row, local
		// entity version and durable Phase 3C mutation intent. The raw repository
		// remains available for isolated persistence tests only.
		vaultitem.NewSQLMutationRepository(db),
		environments.NewSQLRepository(db),
	)
	// The callback/browser boundary is composed here rather than in auth so the
	// OIDC core remains independent of Wails and directly testable.
	var openBrowser auth.BrowserOpener
	var authMu sync.Mutex
	refreshStore := auth.NewSystemRefreshTokenStore()
	wireAuth := func(cfg auth.Config) error {
		authService, err := auth.New(auth.Dependencies{
			Config: cfg,
			Store:  refreshStore,
			OpenBrowser: func(url string) error {
				if openBrowser == nil {
					return auth.ErrAuthenticationFailed
				}
				return openBrowser(url)
			},
		})
		if err != nil {
			return err
		}
		authMu.Lock()
		container.Auth = authService
		authMu.Unlock()
		return nil
	}
	startupResult, err := cloudconfig.ResolveStartup(context.Background(), cloudconfig.StartupOptions{
		ConfigDir: configDir,
		Timeout:   5 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	container.SyncBlocked = startupResult.SyncBlocked()
	if startupResult.IssuerChanged {
		_ = refreshStore.Delete()
	}
	cloudconfig.StartRefresh(context.Background(), cloudconfig.RefreshOptions{
		ConfigDir:      configDir,
		FromEnv:        startupResult.FromEnv,
		StartupFetched: startupResult.StartupFetched,
		OnApply: func(update cloudconfig.ApplyUpdate) {
			container.SyncBlocked = cloudconfig.ApplySyncBlocked(container.SyncBlocked, update.Blocked)
			if update.IssuerChanged {
				_ = refreshStore.Delete()
			}
			if !startupResult.FromEnv && update.HasAuthConfig {
				authMu.Lock()
				needsAuth := container.Auth == nil
				authMu.Unlock()
				if needsAuth || update.IssuerChanged {
					if err := wireAuth(update.AuthConfig); err != nil {
						log.Printf("auth wiring failed: %v", err)
					}
				}
			}
		},
	})
	if startupResult.HasAuthConfig {
		if err := wireAuth(startupResult.AuthConfig); err != nil {
			log.Fatal(err) // keyring/deps failure only — not missing issuer
		}
	}
	outbox := syncstore.NewSQLOutbox(db)
	if err := outbox.RecoverStaleSyncing(time.Now().UTC()); err != nil {
		log.Fatal(err)
	}
	remoteStore := syncstore.NewSQLRemoteStore(db)
	syncService, err := syncclient.New(syncclient.Dependencies{
		DeviceIdentity: syncstore.NewSQLDeviceIdentityStore(db),
		Cursors:        syncstore.NewSQLCursorStore(db),
		Remote:         remoteStore,
		LocalMutations: syncstore.NewSQLLocalMutationStore(db),
		Outbox:         outbox,
		Vault:          container.Vault,
	})
	if err != nil {
		log.Fatal(err)
	}
	// Sync remains disconnected until the user explicitly starts the pinned
	// Keycloak/Gateway flow through the capability-gated bridge.
	container.Sync = syncService
	// Conflict resolution runs offline against the bound account, so it is wired
	// directly from durable stores rather than the live session.
	container.SyncConflict = syncconflict.New(syncconflict.Deps{
		Conflicts: syncstore.NewSQLConflictStore(db),
		Resolver:  syncstore.NewSQLConflictResolver(db),
		Accounts:  remoteStore,
		Items:     container.Items,
	})
	// Agent Hub: SQLite session/turn store + mock/claude adapters. Compose here
	// so agenthub never imports internal/storage (migrations stay in storage).
	agentReg := agenthub.NewRegistry()
	agentReg.Register(agenthub.NewMockAdapter())
	agentReg.Register(agenthub.NewClaudeAdapter(agenthub.ClaudeOptions{AllowNpx: true}))
	agentWaiter := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) {
		emitAgentHubEvent("agenthub:permission", map[string]any{
			"id":        p.ID,
			"sessionId": p.SessionID,
			"params":    rawJSON(p.Params),
		})
	})
	agentSvc := agenthub.NewService(agentReg, agenthub.NewSQLStore(db), func(sessionID string, update json.RawMessage) {
		emitAgentHubEvent("agenthub:sessionUpdate", map[string]any{
			"sessionId": sessionID,
			"update":    rawJSON(update),
		})
	})
	agentSvc.ResolvePermission = agentWaiter.Resolve
	container.AgentHub = agentSvc
	container.AgentHubPermissions = agentWaiter
	defer container.Database.Close()
	defer container.SSH.Close()
	defer container.Kafka.Close()
	if err := app.RegisterBuiltins(container); err != nil {
		log.Fatal(err)
	}

	// MCP host: GUI process owns tools + interaction; cmd/devkit-mcp proxies stdio.
	// NoopPrompter is enough: InteractionHost polls InteractionPending every 500ms
	// and Completes via the bridge. Share one Hub between MCP and bridge.
	hub := interaction.NewHub(interaction.NoopPrompter{})
	container.Interaction = hub
	appService := bridge.NewAppService(container)
	mcpServer := mcp.NewServerFromContainer(mcp.Deps{
		Caller: appService,
		Notes:  container.Notes,
		Vault:  container.Vault,
		Search: mcp.NewSearchAPI(container.Items),
		Tools:  container.HashTools,
		Envs:   container.Environments,
		SSH:    container.SSH,
		DB:     container.Database,
		Kafka:  container.Kafka,
		Hub:    hub,
	})
	mcpCtx, mcpCancel := context.WithCancel(context.Background())
	defer mcpCancel()
	sockPath, err := mcp.SocketPath()
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		if err := mcp.ServeUnix(mcpCtx, sockPath, mcpServer); err != nil {
			log.Printf("mcp host: %v", err)
		}
	}()

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
		OnShutdown: mcpCancel,
	})
	openBrowser = wailsApp.Browser.OpenURL

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

// emitAgentHubEvent forwards hub callbacks to the Wails/server event bus once
// the application singleton exists (Invoke/permission happen after Run starts).
func emitAgentHubEvent(name string, data map[string]any) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(name, data)
}

func rawJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}
