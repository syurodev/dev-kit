package main

import (
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/app"
	"devkit/internal/auth"
	"devkit/internal/bridge"
	"devkit/internal/cloudconfig"
	"devkit/internal/codegraph"
	"devkit/internal/database"
	"devkit/internal/notes"
	"devkit/internal/environments"
	"devkit/internal/interaction"
	"devkit/internal/mcp"
	"devkit/internal/preferences"
	"devkit/internal/servermode"

	"devkit/internal/storage"
	"devkit/internal/syncclient"
	"devkit/internal/syncconflict"
	"devkit/internal/syncstore"
	"devkit/internal/terminal"
	"devkit/internal/uistate"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

// Floor derived from Sidebar (240px min) + its gutter (4px) + the content
// area's own hard floor (480px) + SplitPanel's gutter (4px) + SplitPanel
// (320px min), both open at once, plus the outer mx-1/mb-1 margins (8px) —
// see docs/superpowers/specs/2026-08-20-app-shell-sidebar-design.md § Resize.
// A restored window frame is rejected below this rather than being resized by
// the platform and then saved back at the wrong size.
const (
	minWindowWidth  = 1080
	minWindowHeight = 700
)

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
	container.ConfigDir = configDir
	// Query history is local by construction: its table sits outside
	// vault_items, which sync has no path to.
	container.Database.SetQueryLog(database.NewSQLQueryHistory(db), container.Vault)
	container.Database.SetSchemaCache(database.NewSQLSchemaCache(db))
	// Backlinks need a real table; without one the service still runs and
	// simply reports no links, which is what keeps the in-memory container in
	// tests working unchanged.
	container.Notes.SetLinkIndex(notes.NewSQLLinkIndex(db))
	container.Preferences = preferences.NewService(preferences.NewSQLRepository(db))
	container.UIState = uistate.NewService(uistate.NewSQLRepository(db))
	themePreference, err := container.Preferences.Theme()
	if err != nil {
		log.Fatal(err)
	}
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
		container.SetAuth(authService)
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
	container.SetSyncBlockedErr(startupResult.SyncBlocked())
	if startupResult.IssuerChanged {
		_ = refreshStore.Delete()
	}
	cloudconfig.StartRefresh(context.Background(), cloudconfig.RefreshOptions{
		ConfigDir:      configDir,
		FromEnv:        startupResult.FromEnv,
		StartupFetched: startupResult.StartupFetched,
		OnApply: func(update cloudconfig.ApplyUpdate) {
			container.SetSyncBlockedErr(cloudconfig.ApplySyncBlocked(container.SyncBlockedErr(), update.Blocked))
			if update.IssuerChanged {
				_ = refreshStore.Delete()
			}
			if !startupResult.FromEnv && update.HasAuthConfig {
				authMu.Lock()
				needsAuth := container.Auth() == nil
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
	conflictStore := syncstore.NewSQLConflictStore(db)
	syncService, err := syncclient.New(syncclient.Dependencies{
		DeviceIdentity: syncstore.NewSQLDeviceIdentityStore(db),
		Cursors:        syncstore.NewSQLCursorStore(db),
		Remote:         remoteStore,
		LocalMutations: syncstore.NewSQLLocalMutationStore(db),
		Outbox:         outbox,
		Conflicts:      conflictStore,
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
		Conflicts: conflictStore,
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
		emitAppEvent("agenthub:permission", map[string]any{
			"id":        p.ID,
			"sessionId": p.SessionID,
			"params":    rawJSON(p.Params),
		})
	})
	agentSvc := agenthub.NewService(agentReg, agenthub.NewSQLStore(db), func(sessionID string, update json.RawMessage) {
		emitAppEvent("agenthub:sessionUpdate", map[string]any{
			"sessionId": sessionID,
			"update":    rawJSON(update),
		})
	})
	agentSvc.ResolvePermission = agentWaiter.Resolve
	container.AgentHub = agentSvc
	container.AgentHubPermissions = agentWaiter

	terminalRegistry := terminal.NewRegistry(func(sessionID string, chunk []byte) {
		// Base64 explicitly rather than letting encoding/json decide what a
		// []byte becomes on the wire: PTY output is arbitrary bytes, a
		// multibyte rune can straddle two chunks, and a plain string() here
		// would corrupt it. The frontend decodes back to bytes and hands them
		// to xterm, which does its own UTF-8 reassembly across writes.
		//
		// Emit is non-blocking, which is what lets readLoop call it without a
		// slow consumer stalling the PTY reader.
		emitAppEvent("terminal:output", map[string]any{
			"sessionId": sessionID,
			"chunk":     base64.StdEncoding.EncodeToString(chunk),
		})
	})
	container.Terminal = terminalRegistry
	// A finished re-index used to tell the frontend nothing, so endpoint lists
	// and plan statuses silently went stale — see codegraph.mdx on the diff
	// recomputing itself after every incremental re-index.
	container.CodeGraph.OnIndexUpdated = func(version int) {
		emitAppEvent("codegraph:indexUpdated", map[string]any{"version": version})
	}
	container.CodeGraph.SetTerminalRegistry(terminalRegistry)
	defer terminalRegistry.CloseAll()

	// Dev-time wiring: resolve the Java analyzer jar relative to the working
	// directory (this binary is always run from the repo root — see
	// build/Taskfile.yml's run:server/run:desktop tasks). A real packaged
	// build needs a different resolution strategy (bundled resource path or
	// an env var override) — deliberately not decided here; see
	// analyzer_subprocess.go's own doc comment on NewSubprocessAnalyzerWithJar.
	const codegraphAnalyzerJarPath = "analyzer/target/codegraph-analyzer.jar"
	if _, err := os.Stat(codegraphAnalyzerJarPath); err == nil {
		container.CodeGraph.SetAnalyzer(codegraph.NewSubprocessAnalyzerWithJar(codegraphAnalyzerJarPath))
	} else {
		log.Printf("codegraph: analyzer jar not found at %q — CodeGraph indexing will report \"analyzer not configured\" until it is built (cd analyzer && mvn package)", codegraphAnalyzerJarPath)
	}

	defer container.Database.Close()
	defer container.SSH.Close()
	defer container.Kafka.Close()
	defer container.CodeGraph.Close()
	if err := app.RegisterBuiltins(container); err != nil {
		log.Fatal(err)
	}

	// MCP host: GUI process owns tools + interaction; cmd/devkit-mcp proxies stdio.
	// NoopPrompter is enough: InteractionHost polls InteractionPending every 500ms
	// and Completes via the bridge. Share one Hub between MCP and bridge.
	hub := interaction.NewHub(interaction.NoopPrompter{})
	container.Interaction = hub
	var mainWindow application.Window
	appService := bridge.NewAppService(container, func(theme preferences.Theme) error {
		return applyNativeTheme(mainWindow, theme)
	})
	mcpServer := mcp.NewServerFromContainer(mcp.Deps{
		Caller:    appService,
		Notes:     container.Notes,
		Vault:     container.Vault,
		Search:    mcp.NewSearchAPI(container.Items),
		Tools:     container.HashTools,
		Envs:      container.Environments,
		SSH:       container.SSH,
		DB:        container.Database,
		Kafka:     container.Kafka,
		CodeGraph: container.CodeGraph,
		AgentHub:  container.AgentHub,
		Hub:       hub,
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
		// One DevKit per user. A second launch would open a second window onto
		// the same devkit.db, the same vault session and the same MCP socket
		// (which internal/mcp already refuses to hand over, leaving the new
		// window's agent tools dead). Re-launching from the dock, a terminal or
		// a file association now raises the running window instead.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.devkit.app",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if mainWindow == nil {
					return
				}
				mainWindow.Restore()
				mainWindow.Show()
				mainWindow.Focus()
			},
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: mcpCancel,
	})
	openBrowser = wailsApp.Browser.OpenURL

	// A fully transparent window background is what actually lets a native
	// backdrop show through — an opaque colour paints over it regardless of
	// the backdrop option. Two platforms have one: macOS (Mac.Backdrop
	// vibrancy) and Windows 11 22H2+ (Mica, see native_backdrop_windows.go).
	// Everywhere else the window stays solid, painted with the same values as
	// the frontend's --background-app token so the transparent html/body the
	// frontend uses inside Wails (main.tsx's .native class) sits on an
	// identical colour instead of a slightly different one.
	resolvedDark := themePreference == preferences.ThemeDark ||
		(themePreference == preferences.ThemeSystem && wailsApp.Env.IsDarkMode())
	backgroundColour := application.NewRGB(233, 234, 238)
	if resolvedDark {
		backgroundColour = application.NewRGB(2, 2, 2)
	}
	backgroundType := application.BackgroundTypeSolid
	if runtime.GOOS == "darwin" {
		backgroundColour = application.NewRGBA(0, 0, 0, 0)
	} else if nativeBackdropTranslucent() {
		backgroundColour = application.NewRGBA(0, 0, 0, 0)
		backgroundType = application.BackgroundTypeTranslucent
	}

	macAppearance := application.DefaultAppearance
	windowsTheme := application.SystemDefault
	switch themePreference {
	case preferences.ThemeDark:
		macAppearance = application.NSAppearanceNameDarkAqua
		windowsTheme = application.Dark
	case preferences.ThemeLight:
		macAppearance = application.NSAppearanceNameAqua
		windowsTheme = application.Light
	}

	// Reopen where the user left the window. Falls back to the launch default
	// when nothing is stored yet or the stored frame is unusable.
	windowOptions := application.WebviewWindowOptions{
		Title:     "DevKit",
		Width:     1200,
		Height:    760,
		MinWidth:  minWindowWidth,
		MinHeight: minWindowHeight,
		Mac: application.MacWindow{
			// Do not use InvisibleTitleBarHeight here. In Wails beta.8 it is a
			// native AppKit hit zone: every first mouse-down in the top N pixels
			// calls performWindowDragWithEvent before the webview can honor a
			// no-drag action. The frontend marks the full header as draggable and
			// opts its interactive children out individually instead.
			InvisibleTitleBarHeight: 0,
			Backdrop:                application.MacBackdropTranslucent,
			// Match AppKit's vibrancy, rim and titlebar to the persisted web theme.
			Appearance: macAppearance,
			// Hidden avoids the additional NSToolbar used by HiddenInset and
			// matches the frontend's compact 30px titlebar.
			TitleBar: application.MacTitleBarHidden,
		},
		Windows: application.WindowsWindow{
			Theme: windowsTheme,
			// Only takes effect when backgroundType resolved to Translucent
			// above (Windows 11 22H2+); Wails ignores it on a solid window.
			BackdropType: application.Mica,
		},
		BackgroundType:   backgroundType,
		BackgroundColour: backgroundColour,
		URL:              "/",
	}
	if frame, ok := loadWindowFrame(container.UIState); ok {
		// X and Y are ignored unless InitialPosition opts out of the default,
		// which is WindowCentered — without this the size restores but the
		// window still jumps back to the middle of the primary display.
		windowOptions.InitialPosition = application.WindowXY
		windowOptions.X = frame.X
		windowOptions.Y = frame.Y
		windowOptions.Width = frame.Width
		windowOptions.Height = frame.Height
		if frame.Maximised {
			windowOptions.StartState = application.WindowStateMaximised
		}
	}
	mainWindow = wailsApp.Window.NewWithOptions(windowOptions)
	trackWindowFrame(mainWindow, container.UIState)

	// WKWebView currently caps requestAnimationFrame near 60Hz even on a
	// ProMotion display. Opt into its native refresh rate through a private
	// WebKit feature only after the webview exists. The native helper resolves
	// every private selector dynamically and is exception guarded: if Apple
	// removes or changes the API, this safely falls back to WKWebView's default
	// rendering policy instead of affecting startup or page rendering.
	mainWindow.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		// Window events may arrive on AppKit's main thread; hop out before the
		// helper synchronously dispatches its Objective-C work back to that thread.
		go func() {
			// Screens are only enumerable once the platform application is up,
			// so a restored frame is checked against them here rather than
			// before the window is created.
			recentreIfOffScreen(mainWindow, wailsApp.Screen)
			if enableNativeHighRefreshRate(mainWindow) {
				log.Printf("WKWebView native refresh rate enabled (private WebKit API)")
			} else if runtime.GOOS == "darwin" {
				log.Printf("WKWebView native refresh rate unavailable; using default frame rate")
			}
		}()
	})

	// A window created in an explicit light/dark mode does not ask every
	// platform backend to track later system changes. Once the user selects
	// System, reapply the resolved native theme whenever Wails reports one.
	wailsApp.Event.OnApplicationEvent(events.Common.ThemeChanged, func(*application.ApplicationEvent) {
		theme, themeErr := container.Preferences.Theme()
		if themeErr != nil || theme != preferences.ThemeSystem {
			return
		}
		go func() {
			if err := applyNativeTheme(mainWindow, theme); err != nil {
				log.Printf("apply system window theme: %v", err)
			}
		}()
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}

// emitAppEvent forwards a backend callback to the Wails/server event bus once
// the application singleton exists (these fire after Run starts). Non-blocking:
// app.Event.Emit does not wait for listeners, which is what lets the file
// watcher and the PTY reader call it without stalling.
func emitAppEvent(name string, data map[string]any) {
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
