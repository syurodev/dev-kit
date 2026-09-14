package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"devkit/internal/agenthub"
	"devkit/internal/terminal"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// AgentHubAuthMethods lists the sign-ins an agent offers ("claude"), each with
// the command DevKit would run for it. No session is needed: the list comes
// from a sandboxed initialize, so it works for a user who has never signed in.
func (s *AppService) AgentHubAuthMethods(ctx context.Context, agent string) ([]agenthub.AuthMethod, error) {
	id := agentRuntimeID(agent)
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:read", "auth:"+id, map[string]any{
		"action": "auth_methods",
		"agent":  id,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.AuthMethods(callCtx, agenthub.AgentType(agent))
	})
	if err != nil {
		return nil, err
	}
	if methods, _ := result.([]agenthub.AuthMethod); methods != nil {
		return methods, nil
	}
	return []agenthub.AuthMethod{}, nil
}

// AgentHubStartSignIn runs one of an agent's sign-in methods in a new terminal
// session and returns its id, for the Terminal module's tab to attach to. The
// process's exit code arrives as a terminal:exit event.
//
// Only a method id crosses the bridge, never a command: the backend resolves it
// against what the agent offered (AuthMethods), so the UI cannot be used to run
// anything else. The login runs outside the sandbox, like the user typing it —
// it needs a browser, a localhost callback, the keychain and ~/.claude.
func (s *AppService) AgentHubStartSignIn(ctx context.Context, agent, methodID string, rows, cols int) (string, error) {
	id := agentRuntimeID(agent)
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "auth:"+id, map[string]any{
		"action": "start_sign_in",
		"agent":  id,
		"method": agentRuntimeID(methodID),
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		if s.container.Terminal == nil {
			return nil, errAgentHubUnavailable
		}
		cmd, err := svc.SignInCommand(callCtx, agenthub.AgentType(agent), methodID)
		if err != nil {
			return nil, err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("%w: sign-in: home directory: %v", agenthub.ErrProcessFailed, err)
		}
		pty, err := terminal.NewLocalCommandPTY(cmd.Path, cmd.Args, cmd.Env, home, rows, cols)
		if err != nil {
			return nil, fmt.Errorf("%w: sign-in: start %s: %v", agenthub.ErrProcessFailed, cmd.Label, err)
		}
		return s.container.Terminal.Register("builtin.agenthub", pty), nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

var errAgentHubUnavailable = errors.New("agenthub is not available")

func (s *AppService) agentHubService() (*agenthub.Service, error) {
	if s.container.AgentHub == nil {
		return nil, errAgentHubUnavailable
	}
	return s.container.AgentHub, nil
}

func (s *AppService) agentHubPermissions() (*agenthub.PermissionWaiter, error) {
	if s.container.AgentHubPermissions == nil {
		return nil, errAgentHubUnavailable
	}
	return s.container.AgentHubPermissions, nil
}

// hubSessionID validates a hub session UUID for capability scopes / audit metadata.
// Agent hub sessions use google/uuid strings, not vaultitem 24-char hex ids.
func hubSessionID(value string) string {
	if len(value) != 36 {
		return invalidOpaqueID
	}
	for i, char := range value {
		switch i {
		case 8, 13, 18, 23:
			if char != '-' {
				return invalidOpaqueID
			}
		default:
			if !strings.ContainsRune("0123456789abcdef", char) {
				return invalidOpaqueID
			}
		}
	}
	return value
}

func (s *AppService) AgentHubListAgents() ([]agenthub.AgentSession, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "session:list", map[string]any{
		"action": "list_agents",
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ListSessions(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.AgentSession), nil
}

func (s *AppService) AgentHubCreateSession(input agenthub.CreateInput) (agenthub.AgentSession, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "session:new", map[string]any{
		"action":     "create_session",
		"agent_type": string(input.AgentType),
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.CreateSession(ctx, input)
	})
	if err != nil {
		return agenthub.AgentSession{}, err
	}
	return result.(agenthub.AgentSession), nil
}

func (s *AppService) AgentHubGetStatus(id string) (agenthub.AgentSession, error) {
	opaqueID := hubSessionID(id)
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "session:"+opaqueID, map[string]any{
		"action":     "get_status",
		"session_id": opaqueID,
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.GetStatus(ctx, id)
	})
	if err != nil {
		return agenthub.AgentSession{}, err
	}
	return result.(agenthub.AgentSession), nil
}

// AgentHubTranscriptPage is the transcript read: the newest bounded window of
// a session, oldest first. It goes through the same capability gateway as
// every other read — an agent asking for another agent's transcript is
// refused by authorizeTarget, not by the UI. beforeSeq walks backwards from
// the page already held (0 starts from the end), limit is clamped
// server-side; the unbounded listing this replaces made a session with months
// of history pay for all of it on open.
func (s *AppService) AgentHubTranscriptPage(id string, beforeSeq int64, limit int) (agenthub.TranscriptPage, error) {
	opaqueID := hubSessionID(id)
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "session:"+opaqueID, map[string]any{
		"action":     "transcript_page",
		"session_id": opaqueID,
		"before_seq": beforeSeq,
		"limit":      limit,
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.TranscriptPage(ctx, id, beforeSeq, limit)
	})
	if err != nil {
		return agenthub.TranscriptPage{}, err
	}
	return result.(agenthub.TranscriptPage), nil
}

// AgentHubToolCallDetail returns one stored tool call, payload intact — the
// page reader shortens oversized raw fields to a preview, and this is the
// "show me the whole output" behind it.
func (s *AppService) AgentHubToolCallDetail(id string, toolCallID string) (agenthub.TranscriptEntry, error) {
	opaqueID := hubSessionID(id)
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "session:"+opaqueID, map[string]any{
		"action":       "tool_call_detail",
		"session_id":   opaqueID,
		"tool_call_id": toolCallID,
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ToolCallDetail(ctx, id, toolCallID)
	})
	if err != nil {
		return agenthub.TranscriptEntry{}, err
	}
	return result.(agenthub.TranscriptEntry), nil
}

func (s *AppService) AgentHubInvoke(ctx context.Context, input agenthub.InvokeInput) (agenthub.InvokeResult, error) {
	opaqueID := hubSessionID(input.SessionID)
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "invoke",
		"session_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.Invoke(callCtx, input)
	})
	if err != nil {
		return agenthub.InvokeResult{}, err
	}
	return result.(agenthub.InvokeResult), nil
}

func (s *AppService) AgentHubCancel(ctx context.Context, id string) error {
	opaqueID := hubSessionID(id)
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "cancel",
		"session_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.Cancel(callCtx, id)
	})
	return err
}

// AgentHubCloseSession shuts the live runtime down and marks the session
// closed. Distinct from AgentHubCancel, which stops the turn in flight and
// leaves the agent running: closing ends the process, releases its MCP
// endpoint and is not undoable — the session stays in the store as history.
func (s *AppService) AgentHubCloseSession(ctx context.Context, id string) error {
	opaqueID := hubSessionID(id)
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "close_session",
		"session_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.CloseSession(callCtx, id)
	})
	return err
}

// AgentHubDeleteSession permanently removes a session, its transcript and
// every descendant. Closing comes first inside the service, so this also ends
// whatever was still running.
func (s *AppService) AgentHubDeleteSession(ctx context.Context, id string) error {
	opaqueID := hubSessionID(id)
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "delete_session",
		"session_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.DeleteSession(callCtx, id)
	})
	return err
}

func (s *AppService) AgentHubListVendorTypes() ([]agenthub.AgentType, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "vendor:list", map[string]any{
		"action": "list_vendor_types",
	}, func(context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ListVendorTypes(), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.AgentType), nil
}

// AgentHubResolvePermission completes a pending ACP permission prompt from the UI.
func (s *AppService) AgentHubResolvePermission(id string, outcome json.RawMessage) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "permission:complete", map[string]any{
		"action": "resolve_permission",
		"id":     opaqueID,
	}, func(context.Context) (any, error) {
		waiter, err := s.agentHubPermissions()
		if err != nil {
			return nil, err
		}
		return nil, waiter.Complete(id, outcome)
	})
	return err
}

// AgentHubPendingPermissions lists every ACP permission prompt awaiting the
// user, oldest first. The frontend uses it to rebuild its queue on mount;
// live arrivals still come through the agenthub:permission event.
func (s *AppService) AgentHubPendingPermissions() ([]agenthub.PendingPermission, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "permission:list", map[string]any{
		"action": "pending_permissions",
	}, func(context.Context) (any, error) {
		waiter, err := s.agentHubPermissions()
		if err != nil {
			return nil, err
		}
		return waiter.PendingList(), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.PendingPermission), nil
}

// defaultOpenDirectoryDialog is the native folder picker. Same shape as
// defaultSaveFileDialog in database.go: the real dialog lives here, and the
// AppService field beside it lets tests substitute one without a window.
func defaultOpenDirectoryDialog(title, startIn string) (string, error) {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return "", errors.New("dialog service unavailable")
	}
	dialog := app.Dialog.OpenFile().
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true).
		SetTitle(title)
	if startIn != "" {
		dialog.SetDirectory(startIn)
	}
	return dialog.PromptForSingleSelection()
}

// AgentHubChooseFiles opens the native file picker and returns the chosen
// paths, empty when the user cancelled.
//
// Multi-select because attaching context is usually "these three files", and a
// picker that closes after one turns that into three trips through a modal.
func (s *AppService) AgentHubChooseFiles(startIn string) ([]string, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "session:new", map[string]any{
		"action": "choose_files",
	}, func(context.Context) (any, error) {
		dialogFunc := s.openFilesDialog
		if dialogFunc == nil {
			dialogFunc = defaultOpenFilesDialog
		}
		return dialogFunc("Attach files to the prompt", startIn)
	})
	if err != nil {
		return nil, err
	}
	// Normalised here rather than in the dialog: every implementation of that
	// hook — the native one, a test's, a future one — would otherwise have to
	// remember, and a nil slice marshals as null where the frontend spreads a
	// list. The boundary is the place that owns the contract.
	if paths, _ := result.([]string); paths != nil {
		return paths, nil
	}
	return []string{}, nil
}

// defaultOpenFilesDialog is the native file picker, sibling of
// defaultOpenDirectoryDialog.
func defaultOpenFilesDialog(title, startIn string) ([]string, error) {
	app := application.Get()
	if app == nil || app.Dialog == nil {
		return nil, errors.New("dialog service unavailable")
	}
	dialog := app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle(title)
	if startIn != "" {
		dialog.SetDirectory(startIn)
	}
	// Cancel comes back as no paths and no error; AgentHubChooseFiles turns
	// that into an empty list.
	return dialog.PromptForMultipleSelection()
}

// AgentHubChooseDirectory opens the native folder picker// AgentHubChooseDirectory opens the native folder picker and returns the chosen
// path, or "" when the user cancelled. Cancelling is not an error: the dialog
// that called this stays open with the field untouched, and an error there
// would render as a failure the user did not cause.
//
// startIn is a hint, not a constraint — the picker opens there when the path
// exists and falls back to the system default otherwise.
func (s *AppService) AgentHubChooseDirectory(startIn string) (string, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "session:new", map[string]any{
		"action": "choose_directory",
	}, func(context.Context) (any, error) {
		dialogFunc := s.openDirDialog
		if dialogFunc == nil {
			dialogFunc = defaultOpenDirectoryDialog
		}
		return dialogFunc("Choose a working directory", startIn)
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}
