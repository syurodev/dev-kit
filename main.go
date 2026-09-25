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
	"devkit/internal/agentruntime"
	"devkit/internal/app"
	"devkit/internal/audit"
	"devkit/internal/auth"
	"devkit/internal/bridge"
	"devkit/internal/capabilities"
	"devkit/internal/checkpoint"
	"devkit/internal/cloudconfig"
	"devkit/internal/codegraph"
	"devkit/internal/database"
	"devkit/internal/environments"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/mcp"
	"devkit/internal/mcp/install"
	"devkit/internal/notes"
	"devkit/internal/notify"
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
	"devkit/internal/workfolder"
	"devkit/internal/workspace"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
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

// transcriptRetentionDays is how long agent transcript rows are kept.
const transcriptRetentionDays = 90

// agentCheckpointsOn is the one place the two conditions meet: the user's
// preference, and whether the machine has a git the engine can use. Read on
// every question rather than captured, so turning the setting off takes effect
// without a restart.
func agentCheckpointsOn(prefs *preferences.Service, engine *checkpoint.Engine) bool {
	if engine == nil || !engine.Available() {
		return false
	}
	settings, err := prefs.AgentCheckpoints()
	return err == nil && settings.Enabled
}

func main() {
	var mainWindow application.Window

	// Before anything can bring up GTK/WebKit: the webview process inherits
	// this environment, and WebKitGTK reads it once, at startup.
	configureWebviewEnv()

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
	container.Workspace = workspace.NewService(workspace.NewSQLRepository(db))
	// Query consoles are files, DataGrip-style: one .sql per console, with the
	// connection it belongs to on the first line. Not vault items — those are
	// the saved queries, which are named, deliberate and synced, and are
	// untouched by this. Wired after UIState because the folder is settable,
	// and the choice is remembered in a ui_state row.
	consoles := database.NewConsoleStore(filepath.Join(dataDir, "consoles"))
	if chosen, found, err := container.UIState.Get(bridge.ConsoleDirKey); err != nil {
		log.Printf("database: read console folder: %v", err)
	} else if found && chosen != "" {
		// A folder that has since been deleted, unmounted or made read-only
		// is not a reason to refuse to start: the consoles fall back to the
		// default and the setting still says what was asked for, which is the
		// state the settings panel can explain.
		if err := consoles.SetDir(chosen); err != nil {
			log.Printf("database: console folder %q unusable, using the default: %v", chosen, err)
		}
	}
	container.Database.SetConsoleStore(consoles)
	// Audit persists to SQLite (audit_events); the in-memory sink stays as the
	// diagnostic view and the fallback when a write fails. Rebuild the gateway
	// here, before RegisterBuiltins / NewAppService capture it.
	auditSQL := audit.NewSQLSink(db, storage.TableAuditEvents, container.Audit)
	defer auditSQL.Close()
	container.Capabilities = capabilities.NewGateway(container.Permissions, audit.NewTee(container.Audit, auditSQL))
	if err := auditSQL.Prune(context.Background()); err != nil {
		log.Printf("audit: prune: %v", err)
	}
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
	// Agent Hub: SQLite session/turn store + the Claude adapter. Compose here
	// so agenthub never imports internal/storage (migrations stay in storage).
	// The mock adapter is not registered: it is the test double the agenthub
	// and bridge tests drive, and offered in the app it was an agent that
	// answered nothing a user could want.
	agentReg := agenthub.NewRegistry()
	// See ClaudeOptions.OnUnknownMethod: this is the measurement that decides
	// whether fs/* and terminal/* are worth implementing. NFR-1 says the
	// Capability Gateway mediates the agent's file access there; today it
	// mediates none, and nobody knows whether that is because the agent never
	// asks or because DevKit never offered.
	reportUnknownMethod := func(method string, params json.RawMessage) {
		log.Printf("agenthub: agent called unimplemented ACP method %q (refused): %s",
			method, truncateForLog(params, 300))
	}
	// The Claude adapter is DevKit's to install (Settings → Agents), pinned and
	// run with an absolute node and entry the jail is told about. It is
	// resolved per session, not here, so an install needs no restart. PATH and
	// npx are never consulted: an app opened from the Dock cannot see an nvm
	// install, and npx dies on EPERM inside the jail.
	// File checkpoints: a shadow git per project under the data directory, used
	// to snapshot the project at the boundary of every turn. Locating git is a
	// few process spawns, done once here rather than per turn; a machine
	// without a usable one gets an engine that reports why and does nothing.
	checkpointGit := checkpoint.Locate(context.Background())
	if !checkpointGit.OK() {
		log.Printf("agenthub: file checkpoints are off: %s", checkpointGit.Reason)
	}
	checkpointEngine := checkpoint.New(filepath.Join(dataDir, "checkpoints"), checkpointGit, checkpoint.Limits{})

	runtimesDir := filepath.Join(dataDir, "runtimes")
	runtimes := agentruntime.New(runtimesDir)
	runtimes.OnOutput = func(agent, line string) {
		emitAppEvent("agenthub:runtimeInstallOutput", map[string]any{
			"agent": agent,
			"line":  line,
		})
	}
	// Before any session can start on a version it would delete.
	if err := runtimes.GCOnStartup(); err != nil {
		log.Printf("agenthub: runtime cleanup: %v", err)
	}
	claudeOpts := agenthub.ClaudeOptions{
		OnUnknownMethod: reportUnknownMethod,
		Runtime: func() (agentruntime.Resolved, error) {
			return runtimes.Resolve(agentruntime.ClaudeACP)
		},
		// Declaring agentFileChangeReport switches on Claude's own native
		// checkpointing for every session the process serves, which writes file
		// history under ~/.claude. So it is asked for only while DevKit's own
		// checkpoints are on — a user who turned them off gets no file history
		// from either side. Read per session, so the switch takes effect on the
		// next one rather than at the next restart.
		FileChangeReports: func() bool { return agentCheckpointsOn(container.Preferences, checkpointEngine) },
	}
	// Development and CI only: an absolute adapter that bypasses the
	// managed runtime entirely.
	if override := os.Getenv("DEVKIT_CLAUDE_ACP_COMMAND"); override != "" {
		if filepath.IsAbs(override) {
			claudeOpts.Command = override
		} else {
			log.Printf("agenthub: DEVKIT_CLAUDE_ACP_COMMAND must be absolute, ignoring %q", override)
		}
	}
	claudeAdapter := agenthub.NewClaudeAdapter(claudeOpts)
	codexOpts := agenthub.CodexOptions{
		// DevKit's own Codex home: the user's ~/.codex, its sign-in and its
		// rules, is never the session's (Codex spec §6).
		Home: filepath.Join(dataDir, "agent-homes", "codex"),
		Runtime: func() (agentruntime.Resolved, error) {
			return runtimes.Resolve(agentruntime.CodexCLI)
		},
		// An Accounts token chosen in Settings → Agents → Codex, resolved at
		// spawn; none means Codex's own ChatGPT sign-in. Read through the
		// container because Accounts is wired further down, once the database
		// is open.
		APIKey: app.CodexAPIKey{C: container}.Key,
		OnSignInCompleted: func(r agenthub.SignInResult) {
			if r.Success {
				// Accounts notes the sign-in so its list answers "what am I
				// signed in to". The credential stays in Codex's home.
				if err := app.RecordAgentSignIn(container, r.Agent); err != nil {
					log.Printf("agenthub: sign-in: accounts note: %v", err)
				}
			}
			emitAppEvent("agenthub:signInCompleted", map[string]any{
				"agent":   string(r.Agent),
				"loginId": r.LoginID,
				"success": r.Success,
				"error":   r.Error,
			})
		},
	}
	// Development and CI only, like DEVKIT_CLAUDE_ACP_COMMAND.
	if override := os.Getenv("DEVKIT_CODEX_COMMAND"); override != "" {
		if filepath.IsAbs(override) {
			codexOpts.Command = override
		} else {
			log.Printf("agenthub: DEVKIT_CODEX_COMMAND must be absolute, ignoring %q", override)
		}
	}
	codexAdapter := agenthub.NewCodexAdapter(codexOpts)
	runtimes.Verifiers = map[string]func(context.Context, agentruntime.Resolved) error{
		agentruntime.ClaudeACP: claudeAdapter.Probe,
		agentruntime.CodexCLI:  codexAdapter.Probe,
	}
	vendors := []agenthub.VendorAdapter{claudeAdapter, codexAdapter}
	if application.System.IsServer() {
		// Server mode serves the bridge over unauthenticated loopback HTTP and
		// the jail shares the host loopback: a session could approve its own
		// tool calls. Keep the types, refuse the sessions.
		vendors = agenthub.ForServerMode(vendors...)
	}
	for _, v := range vendors {
		agentReg.Register(v)
	}
	container.AgentRuntimes = runtimes
	// Desktop notifications for Agent Hub. Nil in server mode, where there is no
	// desktop to notify; Notify and Authorize are no-ops on nil. Nothing here
	// touches the OS yet — the backend starts in app.Run (notificationStartup)
	// and asks for permission on focus (notify.Notifier).
	var agentNotifier *notify.Notifier
	var desktopNotifications *wailsNotifications
	if !application.System.IsServer() {
		desktopNotifications = &wailsNotifications{service: notifications.New()}
		agentNotifier = &notify.Notifier{
			Desktop: desktopNotifications,
			Enabled: func() bool {
				enabled, err := container.Preferences.AgentNotifications()
				return err == nil && enabled
			},
			Focused: func() bool { return mainWindow != nil && mainWindow.IsFocused() },
		}
		desktopNotifications.service.OnNotificationResponse(func(result notifications.NotificationResult) {
			if result.Error != nil {
				log.Printf("agenthub: desktop notification response: %v", result.Error)
				return
			}
			// Only Agent Hub raises these today; a target from anywhere else
			// has no handler yet and is dropped rather than mis-routed.
			if target, ok := notificationTarget(result.Response); ok && target.Module == "agenthub" {
				emitAppEvent("agenthub:notificationAction", map[string]any{"sessionId": target.ID})
			}
		})
		container.AgentNotifications = agentNotifier
	}
	agentWaiter := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) {
		payload := map[string]any{
			"id":        p.ID,
			"sessionId": p.SessionID,
			"params":    rawJSON(p.Params),
			"caller":    identity.Agent(p.SessionID).String(),
			// The UI shows the deadline; it must not invent one. A countdown
			// started when a toast mounts is wrong whenever the toast mounts
			// late, which is the normal case for a request that arrives while
			// the rail is closed.
			"createdAt": p.CreatedAt.Format(time.RFC3339Nano),
			"expiresAt": p.ExpiresAt.Format(time.RFC3339Nano),
		}
		// Title/depth let the prompt say which agent is asking; the hub
		// service is assigned a few lines below and exists by prompt time.
		if container.AgentHub != nil {
			if sess, err := container.AgentHub.GetStatus(context.Background(), p.SessionID); err == nil {
				if sess.Title != nil {
					payload["title"] = *sess.Title
				}
				payload["depth"] = sess.Depth
			}
		}
		emitAppEvent("agenthub:permission", payload)
		// Queues and returns: the waiter starts waiting for the answer only
		// after this callback, so nothing here may wait on the OS.
		agentNotifier.Notify(agentTarget(p.SessionID), "Permission needed", "The agent is waiting for your approval.")
	})
	agentElicitations := agenthub.NewElicitationWaiter(func(e agenthub.PendingElicitation) {
		// Its own event, not agenthub:sessionUpdate: that channel runs
		// ParseSessionUpdate, and a question is not an update. No expiresAt —
		// a question has no deadline; the turn waits for as long as the user
		// takes, and Stop is the way out.
		payload := map[string]any{
			"id":        e.ID,
			"sessionId": e.SessionID,
			"mode":      e.Mode,
			"message":   e.Message,
			"params":    rawJSON(e.Params),
			"caller":    identity.Agent(e.SessionID).String(),
			"createdAt": e.CreatedAt.Format(time.RFC3339Nano),
		}
		if e.ToolCallID != "" {
			payload["toolCallId"] = e.ToolCallID
		}
		if container.AgentHub != nil {
			if sess, err := container.AgentHub.GetStatus(context.Background(), e.SessionID); err == nil {
				if sess.Title != nil {
					payload["title"] = *sess.Title
				}
				payload["depth"] = sess.Depth
			}
		}
		emitAppEvent("agenthub:elicitation", payload)
		agentNotifier.Notify(agentTarget(e.SessionID), "Question from the agent", "The agent is waiting for your answer.")
	}, func(c agenthub.ElicitationClosed) {
		// Answered here, answered elsewhere, taken back by the agent, or the
		// turn was stopped: every open card for this id goes away.
		emitAppEvent("agenthub:elicitationClosed", map[string]any{
			"id":        c.ID,
			"sessionId": c.SessionID,
			"reason":    c.Reason,
		})
	})
	if agentNotifier != nil {
		agentNotifier.Busy = func() bool {
			return agentWaiter.Pending() != nil || agentElicitations.Pending() != nil
		}
	}
	agentStore := agenthub.NewSQLStore(db)

	// Nothing had ever deleted from agent_transcript: it is append-plus-upsert,
	// no session delete cascades into it, and a long session writes a row per
	// streamed chunk. Prune once at startup rather than on a timer — the table
	// only grows while the app runs, and a boot is the one moment where a short
	// blocking sweep costs nothing a user can perceive.
	//
	// The rule is whole closed sessions: a session the user closed goes with
	// its turns and transcript once it has been closed for the window. Idle
	// sessions are never pruned — keying on "silent for the window" emptied
	// transcripts of sessions that still resume, because reading a session does
	// not touch updated_at. The first cut was row-wise on created_at, which
	// sawed the head off any conversation old enough to span it.
	//
	// POLICY, not a measurement: 90 days is a deliberately conservative default
	// so this cannot surprise anyone by eating recent work. Set it to whatever
	// the product actually wants closed sessions to live for.
	// Same shape as the audit prune above: best-effort, logged, never fatal.
	closedBefore := time.Now().AddDate(0, 0, -transcriptRetentionDays)
	if removed, err := agentStore.PruneClosedSessions(context.Background(), closedBefore); err != nil {
		log.Printf("agenthub: session prune: %v", err)
	} else if removed > 0 {
		log.Printf("agenthub: pruned %d sessions closed for more than %d days", removed, transcriptRetentionDays)
	}

	agentSvc := agenthub.NewService(agentReg, agentStore, func(sessionID string, update json.RawMessage) {
		// `parsed` is the one source the UI consumes: the normalized shape the
		// transcript renders from, whose Raw keeps the wire unchanged for
		// kinds this build does not model. It used to travel beside the raw
		// blob too, which carried every streamed field twice per event.
		emitAppEvent("agenthub:sessionUpdate", map[string]any{
			"sessionId": sessionID,
			"parsed":    agenthub.ParseSessionUpdate(update),
		})
	})
	agentSvc.ResolvePermission = agentWaiter.Resolve
	agentSvc.CancelPendingPermissions = agentWaiter.CancelSession
	agentSvc.ResolveElicitation = agentElicitations.Resolve
	agentSvc.CancelPendingElicitations = agentElicitations.CancelSession
	// Read fresh on every spawn/resume/fork (see Service.SandboxExtras), so a
	// change in Settings applies to the next session without a restart.
	agentSvc.SandboxExtras = func() preferences.AgentSandboxSettings {
		settings, err := container.Preferences.AgentSandbox()
		if err != nil {
			// Fail closed: the default shares home and Docker, and a read
			// that errors must not hand that out on a guess.
			return preferences.StrictAgentSandbox()
		}
		return settings
	}
	// Once home is shared, DevKit's own data directory is in it: the vault
	// database and every session's MCP socket. Hidden whatever the settings
	// say — all but the agent runtimes, which the jail has to run.
	agentSvc.ProtectedPaths = func() []string {
		return agenthub.ProtectDataDir(dataDir, filepath.Base(runtimesDir))
	}
	// A crash mid-turn leaves rows saying running/awaiting_permission, and
	// no ACP process survives the restart that could finish those turns —
	// without this reset, Invoke refuses the sessions with ErrSessionBusy
	// forever. Same startup contract as outbox.RecoverStaleSyncing above.
	// Logged, not fatal, like the prune above: a failed reset leaves some
	// sessions refusing prompts, which is no reason to refuse to start DevKit.
	if err := agentSvc.RecoverStaleSessions(context.Background()); err != nil {
		log.Printf("agenthub: recover stale sessions: %v", err)
	}
	// Keep the stamped deadline and the enforced one from drifting apart.
	agentWaiter.Timeout = agentSvc.PermissionTimeoutOrDefault()
	agentSvc.OnTurnFinished = func(sessionID string, status agenthub.SessionStatus, outcome string) {
		emitAppEvent("agenthub:turnFinished", map[string]any{
			"sessionId": sessionID,
			"status":    string(status),
			"outcome":   outcome,
		})
		if outcome == "completed" {
			// Runs in Invoke's deferred turn end; queues and returns.
			agentNotifier.Notify(agentTarget(sessionID), "Answer ready", "The agent finished its turn.")
		}
	}
	agentSvc.OnRecordError = func(sessionID string, err error) {
		log.Printf("agenthub: transcript write failed for session %s: %v", sessionID, err)
	}
	agentSvc.OnFilesRestored = func(sessionID string, result agenthub.RestoreResult) {
		// The project's files moved under whatever else is showing them. The
		// transcript is untouched: a restore undoes what an agent did on disk,
		// not what it said.
		emitAppEvent("agenthub:filesRestored", map[string]any{
			"sessionId":          sessionID,
			"written":            result.Written,
			"deleted":            result.Deleted,
			"safetyCheckpointId": result.SafetyCheckpointID,
		})
	}
	agentSvc.OnSessionRewound = func(sessionID, promptText string) {
		// The transcript on screen now holds messages the session no longer
		// has. No event the UI already listens to would explain that, so it
		// reloads from the store rather than trying to patch what it has.
		emitAppEvent("agenthub:sessionRewound", map[string]any{
			"sessionId":  sessionID,
			"promptText": promptText,
		})
	}
	agentCheckpoints := agenthub.NewEngineCheckpoints(checkpointEngine, agentStore, agentStore)
	agentCheckpoints.OnCheckpoint = func(sessionID string, record agenthub.CheckpointRecord) {
		// The turn header is drawn while the turn is still running, before
		// there is anything to count. This is what fills the number in.
		// The summary, not the record: the vendor's own changed-file report and
		// the skip list are read by nothing on the other side, and this fires
		// on every turn.
		emitAppEvent("agenthub:checkpoint", map[string]any{
			"sessionId":  sessionID,
			"checkpoint": record.Summary(),
		})
	}
	agentCheckpoints.OnError = func(sessionID string, err error) {
		log.Printf("agenthub: checkpoint failed for session %s: %v", sessionID, err)
	}
	container.AgentCheckpoints = agentCheckpoints
	// The stored preference, made true of the engine before any session runs.
	// The same two steps the Settings write takes, so startup and a change in
	// flight cannot disagree about what is applied.
	if settings, err := container.Preferences.AgentCheckpoints(); err == nil {
		agentCheckpoints.ApplySettings(settings.MaxFileSizeMiB, settings.RetentionDays, settings.ExtraExcludes)
		if settings.Enabled {
			agentSvc.SetCheckpoints(agentCheckpoints)
		}
	} else {
		log.Printf("agenthub: reading the checkpoint preference: %v", err)
	}
	// Retention, once at startup: 30 days, 200 per session, a soft 1 GiB per
	// project, and the shadow repositories of projects with nothing left. In
	// the background because it walks every project directory and nothing is
	// waiting on it.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		report, err := agentCheckpoints.Maintain(ctx)
		if err != nil {
			log.Printf("agenthub: checkpoint maintenance: %v", err)
			return
		}
		if len(report.Expired) > 0 || len(report.RemovedProjects) > 0 {
			log.Printf("agenthub: checkpoint maintenance removed %d checkpoints and %d project repositories",
				len(report.Expired), len(report.RemovedProjects))
		}
	}()
	agentSvc.OnUnknownUpdate = func(sessionID, wireKind string, raw json.RawMessage) {
		// Once per kind per session. claude-agent-acp 0.74 emitted sixteen
		// sessionUpdate kinds and this build once parsed six; the rest were
		// degrading to "unknown" and disappearing, so a vendor adding one
		// looked exactly like nothing happening. Logged with a slice of the
		// payload because the kind alone rarely says what it carries.
		log.Printf("agenthub: unmodelled session/update kind %q for session %s: %s",
			wireKind, sessionID, truncateForLog(raw, 300))
	}
	container.AgentHub = agentSvc
	container.AgentHubPermissions = agentWaiter
	container.AgentHubElicitations = agentElicitations

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
	// Agent Hub's sign-in waits on this: exit 0 means the login finished and
	// the failed action can be retried.
	terminalRegistry.OnExit = func(sessionID string, code int) {
		emitAppEvent("terminal:exit", map[string]any{
			"sessionId": sessionID,
			"code":      code,
		})
	}
	container.Terminal = terminalRegistry
	// A finished re-index used to tell the frontend nothing, so endpoint lists
	// and plan statuses silently went stale — see codegraph.mdx on the diff
	// recomputing itself after every incremental re-index.
	//
	// Every re-index names its service (workspace.mdx § "Bề mặt API"), the
	// focused one included: the frontend refetches only what it shows for
	// that folder.
	container.CodeGraph.OnIndexUpdated = func(version int) {
		emitAppEvent("codegraph:indexUpdated", map[string]any{
			"version":     version,
			"servicePath": container.CodeGraph.ProjectRoot(),
		})
	}
	container.CodeGraphRegistry.OnIndexUpdated = func(servicePath string, version int) {
		emitAppEvent("codegraph:indexUpdated", map[string]any{
			"version":     version,
			"servicePath": servicePath,
		})
	}
	// A resident service shut down to make room says so, rather than the
	// next call to it quietly paying for a reopen.
	container.CodeGraphRegistry.OnEvicted = func(servicePath string) {
		emitAppEvent("codegraph:serviceClosed", map[string]any{"servicePath": servicePath})
	}
	// The active project can change without the UI asking — an agent's
	// codegraph.openProject over MCP, or the restore at startup finishing after
	// the window is up — and an editor holding a buffer from the old folder has
	// to know before its next save lands in the new one.
	container.CodeGraph.OnProjectChanged = func(root string) {
		// The folder Code Graph opened is the folder the rest of the app works
		// in until someone points a module elsewhere.
		container.WorkFolder.SetCurrent(root)
		// "projectChanged" now means the focused service changed; root and
		// servicePath are the same folder, servicePath for the one name
		// every codegraph event carries.
		emitAppEvent("codegraph:projectChanged", map[string]any{"root": root, "servicePath": root})
	}
	// Git follows the work folder. Wired before the announcement below and
	// before the last project is restored, so neither finds it missing.
	app.WireWorkFolder(container)
	container.WorkFolder.Subscribe(func(state workfolder.State) {
		emitAppEvent("workfolder:changed", map[string]any{
			"current": state.Current,
			"pins":    state.Pins,
		})
	})
	container.CodeGraph.SetConfigDir(container.ConfigDir)
	container.CodeGraph.SetTerminalRegistry(terminalRegistry)
	defer terminalRegistry.CloseAll()

	// The analyzer toolchain registry: per language, what to build, what that
	// build needs, and every place a usable artifact may already be. It is what
	// lets the UI do something about a missing analyzer other than print
	// "analyzer not configured". One language today; the module is meant to
	// grow more, and adding one is a Toolchain value rather than a change here.
	//
	// "." is where the analyzer SOURCE lives for a process started from the
	// repository root, which is the dev-time assumption. A packaged install has
	// no source and finds its shipped artifact through the toolchain's own
	// search instead — see analyzerSearchPaths.
	toolchains := codegraph.DefaultToolchains(".")
	container.CodeGraph.SetToolchains(toolchains)
	container.CodeGraph.OnAnalyzerBuildOutput = func(language, line string) {
		emitAppEvent("codegraph:analyzerBuildOutput", map[string]any{
			"language": language,
			"line":     line,
		})
	}
	for _, status := range container.CodeGraph.AnalyzerStatus(context.Background()) {
		if status.Ready {
			continue
		}
		// Not fatal and no longer the end of the story: CodeGraph shows a setup
		// panel that can run this build itself.
		log.Printf("codegraph: %s analyzer not built — CodeGraph will offer to build it (%s in %s)",
			status.Language, status.BuildCommand, status.Artifact)
	}
	// No analyzer is wired here. Which one runs is a per-project answer now
	// that there is more than one language, and only the open itself knows it:
	// it detects what the folder holds and takes that language's analyzer out
	// of the registry above. Each still resolves its artifact and its runtime
	// per analysis, so a jar that appears later — because the user built it
	// from the setup panel — needs no restart and no re-wiring.

	// Returns once the project is marked as opening; the analysis runs in the
	// background and reports through the events above, so it never holds the
	// window back.
	if err := container.CodeGraph.RestoreLastProject(context.Background(), container.ConfigDir); err != nil {
		log.Printf("codegraph: could not restore last project: %v", err)
	}
	// Workspace services are indexed once when added, in the background, and
	// what a restart interrupted is queued again. After the restore above:
	// the queue shares the analyze slots, and the project the user left open
	// is the one they are waiting on.
	app.WireWorkspaceIndex(container, func(st codegraph.ServiceIndexStatus) {
		emitAppEvent("codegraph:indexStatus", map[string]any{
			"path":             st.Path,
			"state":            st.State,
			"error":            st.Error,
			"missingToolchain": st.MissingToolchain,
			"indexedAt":        st.IndexedAt,
		})
	})
	go app.QueueUnindexedServices(container)
	defer container.Database.Close()
	defer container.SSH.Close()
	defer container.Kafka.Close()
	defer container.CodeGraph.Close()
	// Runs before CodeGraph.Close (defers unwind in reverse): resident
	// services share its Host, and each shuts down its own store.
	defer func() { _ = container.CodeGraphRegistry.Shutdown() }()
	if err := app.RegisterBuiltins(container); err != nil {
		log.Fatal(err)
	}

	// MCP host: GUI process owns tools + interaction; cmd/devkit-mcp proxies stdio.
	// NoopPrompter is enough: InteractionHost polls InteractionPending every 500ms
	// and Completes via the bridge. Share one Hub between MCP and bridge.
	hub := interaction.NewHub(interaction.NoopPrompter{})
	// Server mode: every bridge method, InteractionComplete included, is on
	// unauthenticated loopback HTTP, so any local process — an agent in a
	// terminal pointed at this MCP server — could answer its own prompt.
	// Headless makes selection and confirmation fail fast instead, which is
	// what AGENTS.md always said server mode does.
	hub.Headless = application.System.IsServer()
	container.Interaction = hub
	agentSvc.OnSessionClosed = func(id string) { hub.ForgetCaller(identity.Agent(id).String()) }
	// Accounts, Tasks, Git and the API client need the database, the vault and
	// this hub — none of which an ephemeral container has. Wired in one place
	// so main does not have to know which module needs which collaborator.
	app.WireCredentialsAndWork(container, db, container.Vault, app.Tables{
		AccountSecrets:     storage.TableAccountSecrets,
		AccountHostChoices: storage.TableAccountHostChoices,
		APIRequestHistory:  storage.TableAPIRequestHistory,
		CodexHookEvents:    storage.TableCodexHookEvents,
		TaskVerifyRuns:     storage.TableTaskVerifyRuns,
	})
	// The board asks the hub whether a session is still alive before letting
	// another take over its claim. Separate because the hub is built above,
	// after the vendor registry and the sandbox runner.
	app.WireAgentLiveness(container)
	// A re-index that changed the index reconciles any board pointed at this
	// project: a plan node whose code now matches moves its task to review,
	// one that diverged moves it back. Chains the existing listener rather
	// than replacing it, so the frontend still hears about re-indexes.
	app.WirePlanReconcile(container)
	appService := bridge.NewAppService(container, func(theme preferences.Theme) error {
		return applyNativeTheme(mainWindow, theme)
	})
	mcpServer := mcp.NewServerFromContainer(mcp.Deps{
		Caller:            bridge.NewCaller(appService),
		Notes:             container.Notes,
		Vault:             container.Vault,
		Search:            mcp.NewSearchAPI(container.Items),
		Tools:             container.HashTools,
		Envs:              container.Environments,
		SSH:               container.SSH,
		DB:                container.Database,
		Kafka:             container.Kafka,
		CodeGraph:         container.CodeGraph,
		CodeGraphServices: mcp.RegistryServices(container.CodeGraphRegistry),
		AgentHub:          container.AgentHub,
		Tasks:             container.Tasks,
		Git:               container.Git,
		Accounts:          container.Accounts,
		// Wrapped so api.fromEndpoint can read the index: internal/mcp must
		// not import app, and apiclient must not import codegraph.
		API: app.NewAPIWithDrafts(container.APIClient, container.CodeGraph),
		// The write side of an external Codex CLI session's lifecycle hooks.
		// container.CodexSessions is set by WireCredentialsAndWork above, which
		// must run first: a nil *codexsessions.Service assigned into this
		// interface field would be the typed-nil trap — an interface that is
		// NOT == nil, so registerCodexHook's own nil check would not catch it,
		// and every hook call would panic on a nil db instead of the tool
		// simply never registering.
		CodexHook: container.CodexSessions,
		// Which branches an agent may push: the ones written on tasks it
		// holds, so the permission survives the session that created them.
		TaskBranches: app.NewTaskBranches(container.Tasks),
		// Turning an approved plan into a backlog is the join of two modules
		// that must not import each other, so it arrives as its own adapter.
		PlanBacklog: app.NewPlanBacklog(container),
		Hub:         hub,
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

	// Per-session identity-bound sockets for Agent Hub. A missing proxy is
	// logged here and surfaces as ErrProxyNotFound on CreateSession.
	agentsDir := mcp.AgentsDir(sockPath)
	mcp.SweepStaleSockets(agentsDir)
	proxyPath, err := install.ResolveCommand()
	if err != nil {
		log.Printf("agenthub: devkit-mcp proxy not found; agent sessions will fail to open until it is built (task mcp:install, or go build -o bin/devkit-mcp ./cmd/devkit-mcp): %v", err)
		proxyPath = ""
	}
	sessionHost := mcp.NewSessionHost(mcpCtx, mcpServer, agentsDir, proxyPath)
	defer sessionHost.Close()
	agentSvc.MCPEndpoints = sessionHost
	auditSQL.PruneLoop(mcpCtx, time.Hour)

	// The notification service itself is not a Service here on purpose: see
	// wailsNotifications for why a failing one must not fail app.Run.
	// notificationStartup starts it at the same point and never fails.
	services := []application.Service{application.NewService(appService)}
	if desktopNotifications != nil {
		services = append(services, application.NewService(&notificationStartup{desktop: desktopNotifications}))
	}
	wailsApp := application.New(application.Options{
		Name:        "DevKit",
		Description: "Local-first developer toolkit",
		Services:    services,
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
		// The transcript recorder batches writes; land every buffered tail
		// while the database is still open (it closes in main's defers,
		// after Run returns).
		OnShutdown: func() {
			if err := agentSvc.Shutdown(context.Background()); err != nil {
				log.Printf("agenthub: flush transcripts on quit: %v", err)
			}
			mcpCancel()
		},
		ShouldQuit: quitAfterFlush(),
	})
	openBrowser = wailsApp.Browser.OpenURL

	// The platform's default menus, plus DevKit's own — where the platform has
	// a bar to put them in. See app_menu.go for both halves of that.
	if setsApplicationMenu() {
		wailsApp.Menu.SetApplicationMenu(buildApplicationMenu(wailsApp))
	}

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
	// The window is SOLID by default. It used to be transparent so the native
	// backdrop showed through, which reads well on a plain desktop and badly on
	// a busy one: module content ends up sitting on the user's wallpaper, and
	// every surface colour is judged against whatever happens to be behind the
	// window rather than against the palette. The glass tiers still work — they
	// now tint over --background-app instead of the desktop, which is what they
	// are specified as relative to anyway.
	//
	// DEVKIT_TRANSLUCENT_WINDOW=1 restores the vibrancy path (macOS Mac.Backdrop,
	// Windows 11 22H2+ Mica). It is kept rather than deleted because
	// 01 Foundations § Glass materials still documents that behaviour and it is
	// the only way to compare the two side by side.
	translucentWindow := os.Getenv("DEVKIT_TRANSLUCENT_WINDOW") == "1"
	macBackdrop := application.MacBackdropNormal
	backgroundType := application.BackgroundTypeSolid
	if !translucentWindow {
		// Nothing to do: solid type, themed colour, normal backdrop.
	} else if runtime.GOOS == "darwin" {
		backgroundColour = application.NewRGBA(0, 0, 0, 0)
		macBackdrop = application.MacBackdropTranslucent
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

	// HiddenInset with a compact toolbar style. AppKit draws the traffic lights
	// against its own title bar and Wails v3 beta.15 exposes no way to move
	// them, so the preset is the only lever: plain Hidden put them above our
	// 40px bar's centre line, plain HiddenInset put them below it, and the
	// compact toolbar lands between the two. The frontend closes the last two
	// pixels with --titlebar-optical-offset.
	//
	// UseToolbar comes with HiddenInset and is what makes AppKit inset the
	// lights at all; the separator stays hidden. If the toolbar ever interferes
	// with dragging, MacTitleBarHidden is the fallback — drag behaviour lives in
	// the frontend (--wails-draggable) with InvisibleTitleBarHeight at zero.
	macTitleBar := application.MacTitleBarHiddenInset
	macTitleBar.ToolbarStyle = application.MacToolbarStyleUnifiedCompact

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
			Backdrop:                macBackdrop,
			// Match AppKit's vibrancy, rim and titlebar to the persisted web theme.
			Appearance: macAppearance,
			TitleBar:   macTitleBar,
		},
		Windows: application.WindowsWindow{
			Theme: windowsTheme,
			// Only takes effect when backgroundType resolved to Translucent
			// above (Windows 11 22H2+); Wails ignores it on a solid window.
			BackdropType: application.Mica,
		},
		// Linux only, and the whole reason is that GTK would otherwise stack its
		// own decoration on top of the app's title bar. See framelessWindow in
		// native_webview_linux.go.
		Frameless:        framelessWindow,
		BackgroundType:   backgroundType,
		BackgroundColour: backgroundColour,
		// "/" everywhere but a WebKitGTK window compositing on the CPU, which
		// carries a flag the frontend reads to flatten its glass.
		URL: webviewStartURL(),
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
	trackWindowShape(wailsApp, mainWindow)

	// Permission to notify is asked for when the user is here — the window
	// gaining focus (the first time, normally at launch) or the setting being
	// turned on — never when an agent first needs it. Authorize is a no-op once
	// granted, while turned off, or while a permission waits.
	if agentNotifier != nil {
		mainWindow.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
			// Off AppKit's main thread: Authorize reads the setting from SQLite.
			go agentNotifier.Authorize()
		})
	}

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
			// Frameless windows never get their Title applied; put it back
			// once the native window exists. No-op off Linux.
			applyWindowTitle(mainWindow, windowOptions.Title)
			// A trackpad pinch on Linux zooms WebKitGTK's own page scale
			// before any JS ever sees it. No-op off Linux.
			lockWebviewZoom(mainWindow)
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

	// The frontend's performance lines (lib/perfLog.ts), printed where a dev is
	// already looking. The frontend only sends them in dev or with
	// localStorage.devkitPerf set; the cap keeps a page from flooding the log.
	wailsApp.Event.On(perfEventName, func(event *application.CustomEvent) {
		log.Print(perfLogLine(event.Data))
	})

	if err := wailsApp.Run(); err != nil {
		log.Fatal(err)
	}
}

// perfEventName matches PERF_EVENT in frontend/src/lib/perfLog.ts.
const perfEventName = "devkit:perf"

func perfLogLine(data any) string {
	const maxLen = 300
	line, ok := data.(string)
	if !ok {
		return "[perf] event without a string payload"
	}
	// By rune: the lines carry → and ›, and a byte cut can split one.
	if runes := []rune(line); len(runes) > maxLen {
		line = string(runes[:maxLen]) + "…"
	}
	return line
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

// truncateForLog keeps a vendor payload readable in a log line. Agent output
// can be a whole file; a log that scrolls the terminal is a log nobody reads.
func truncateForLog(raw []byte, max int) string {
	if len(raw) <= max {
		return string(raw)
	}
	return string(raw[:max]) + "…"
}
