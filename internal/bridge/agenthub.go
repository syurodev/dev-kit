package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"devkit/internal/app"

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
		owned := true
		defer func() {
			if owned && cmd.Release != nil {
				cmd.Release()
			}
		}()
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("%w: sign-in: home directory: %v", agenthub.ErrProcessFailed, err)
		}
		pty, err := terminal.NewLocalCommandPTY(cmd.Path, cmd.Args, cmd.Env, home, rows, cols)
		if err != nil {
			return nil, fmt.Errorf("%w: sign-in: start %s: %v", agenthub.ErrProcessFailed, cmd.Label, err)
		}
		owned = false
		if cmd.Release != nil {
			go func() { pty.Wait(); cmd.Release() }()
		}
		return s.container.Terminal.Register("builtin.agenthub", pty), nil
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// AgentHubSignInStatus reads an agent's in-app sign-in ("codex"): ChatGPT
// with its email and plan, an API key, or none. Claude signs in in a terminal
// (AgentHubAuthMethods) and answers "not supported" here.
//
// These five are the person's only. There is no MCP tool for any of them, and
// the service refuses a caller stamped as an agent or a terminal MCP client,
// so the capability below is not the only thing between an agent and the
// account every later session signs in with.
func (s *AppService) AgentHubSignInStatus(ctx context.Context, agent string) (agenthub.SignInStatus, error) {
	id := agentRuntimeID(agent)
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:read", "auth:"+id, map[string]any{
		"action": "sign_in_status",
		"agent":  id,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.SignInStatus(callCtx, agenthub.AgentType(agent))
	})
	if err != nil {
		return agenthub.SignInStatus{}, err
	}
	return result.(agenthub.SignInStatus), nil
}

// AgentHubStartBrowserSignIn starts the default sign-in: the vendor serves a
// login page and its callback from inside the jail, and the UI opens the page.
// How it ends arrives as an agenthub:signInCompleted event.
func (s *AppService) AgentHubStartBrowserSignIn(ctx context.Context, agent string) (agenthub.SignInFlow, error) {
	return s.agentHubStartInAppSignIn(ctx, agent, agenthub.SignInBrowser)
}

// AgentHubStartDeviceSignIn starts a sign-in by code, for a machine that
// cannot open a browser. Off by default per ChatGPT account; the UI says how
// to turn it on.
func (s *AppService) AgentHubStartDeviceSignIn(ctx context.Context, agent string) (agenthub.SignInFlow, error) {
	return s.agentHubStartInAppSignIn(ctx, agent, agenthub.SignInDeviceCode)
}

// agentHubStartInAppSignIn records how, never what: the page and the code in
// the answer are the person's to use once and stay out of the audit log.
func (s *AppService) agentHubStartInAppSignIn(ctx context.Context, agent, method string) (agenthub.SignInFlow, error) {
	id := agentRuntimeID(agent)
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "auth:"+id, map[string]any{
		"action": "start_in_app_sign_in",
		"agent":  id,
		"method": method,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.StartSignIn(callCtx, agenthub.AgentType(agent), method)
	})
	if err != nil {
		return agenthub.SignInFlow{}, err
	}
	return result.(agenthub.SignInFlow), nil
}

// AgentHubCancelSignIn abandons the sign-in in progress. A login id that is
// no longer current is already over, and cancelling it is not an error.
func (s *AppService) AgentHubCancelSignIn(ctx context.Context, agent, loginID string) error {
	id := agentRuntimeID(agent)
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "auth:"+id, map[string]any{
		"action": "cancel_in_app_sign_in",
		"agent":  id,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.CancelSignIn(callCtx, agenthub.AgentType(agent), loginID)
	})
	return err
}

// AgentHubSignOut removes the vendor's own sign-in from DevKit's home for it.
// An API key chosen in Settings is not signed out here; it is unlinked there.
func (s *AppService) AgentHubSignOut(ctx context.Context, agent string) error {
	id := agentRuntimeID(agent)
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "auth:"+id, map[string]any{
		"action": "sign_out",
		"agent":  id,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		if err := svc.SignOut(callCtx, agenthub.AgentType(agent)); err != nil {
			return nil, err
		}
		// The note in Accounts goes with the sign-in. Failing to remove it is
		// not a failed sign-out: the credential, the thing that matters, is
		// already gone from the vendor's home.
		if err := app.ForgetAgentSignIn(s.container, agenthub.AgentType(agent)); err != nil {
			log.Printf("agenthub: sign-out: accounts note: %v", err)
		}
		return nil, nil
	})
	return err
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

func (s *AppService) agentHubElicitations() (*agenthub.ElicitationWaiter, error) {
	if s.container.AgentHubElicitations == nil {
		return nil, errAgentHubUnavailable
	}
	return s.container.AgentHubElicitations, nil
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

// AgentHubSteer puts a message into the turn a session is running, instead of
// queueing it behind that turn. Returns the outcome: "injected", or
// "promptRequired" when nothing was running and the caller should send it as
// an ordinary prompt.
func (s *AppService) AgentHubSteer(ctx context.Context, id, text string) (string, error) {
	opaqueID := hubSessionID(id)
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "steer",
		"session_id": opaqueID,
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		outcome, err := svc.Steer(ctx, id, text)
		return string(outcome), err
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// AgentHubSetGoal sets what a session is working towards, or clears it when the
// objective is empty. The goal comes back on a session_info_update.
func (s *AppService) AgentHubSetGoal(ctx context.Context, id, objective string) error {
	opaqueID := hubSessionID(id)
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "set_goal",
		"session_id": opaqueID,
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.SetGoal(ctx, id, objective)
	})
	return err
}

// AgentHubSetMode changes the mode of a live agent session.
func (s *AppService) AgentHubSetMode(ctx context.Context, id, modeID string) error {
	opaqueID := hubSessionID(id)
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action": "set_mode", "session_id": opaqueID, "mode_id": modeID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.SetMode(callCtx, id, modeID)
	})
	return err
}

// AgentHubSetConfigOption changes one setting of a live agent session.
func (s *AppService) AgentHubSetConfigOption(ctx context.Context, id, optionID, value string) error {
	opaqueID := hubSessionID(id)
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action": "set_config_option", "session_id": opaqueID, "option_id": optionID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.SetConfigOption(callCtx, id, optionID, value)
	})
	return err
}

// AgentHubListContextFiles returns bounded prompt suggestions for one session:
// its folder and the workspace folders it was granted, filtered by query.
func (s *AppService) AgentHubListContextFiles(ctx context.Context, id, query string) ([]agenthub.ContextFile, error) {
	opaqueID := hubSessionID(id)
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:read", "session:"+opaqueID, map[string]any{
		"action": "list_context_files", "session_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ListContextFiles(callCtx, id, query)
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.ContextFile), nil
}

// AgentHubListDraftContextFiles is AgentHubListContextFiles for a draft, which
// has no session to read its folders from: the draft's own folder first, then
// the workspace's others.
func (s *AppService) AgentHubListDraftContextFiles(ctx context.Context, roots []string, query string) ([]agenthub.ContextFile, error) {
	result, err := s.call(ctx, "builtin.agenthub", "agenthub:read", "session:new", map[string]any{
		"action": "list_draft_context_files",
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ListDraftContextFiles(callCtx, roots, query)
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.ContextFile), nil
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

// AgentHubListCheckpoints returns a session's file checkpoints — one per turn,
// keyed on the prompt that started it, so the transcript can show a turn's
// "N files changed" without a call per turn.
func (s *AppService) AgentHubListCheckpoints(id string) ([]agenthub.CheckpointSummary, error) {
	opaqueID := hubSessionID(id)
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "session:"+opaqueID, map[string]any{
		"action":     "list_checkpoints",
		"session_id": opaqueID,
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ListCheckpoints(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	if records, _ := result.([]agenthub.CheckpointSummary); records != nil {
		return records, nil
	}
	return []agenthub.CheckpointSummary{}, nil
}

// AgentHubCheckpointFileDiff returns one changed file's two versions out of a
// checkpoint. The path is checked against the checkpoint's own list of changed
// files, so this cannot be used to read an arbitrary file out of a project.
func (s *AppService) AgentHubCheckpointFileDiff(checkpointID, path string) (agenthub.FileDiff, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "checkpoint:"+checkpointID, map[string]any{
		"action":        "checkpoint_file_diff",
		"checkpoint_id": checkpointID,
		"path":          path,
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.CheckpointFileDiff(ctx, checkpointID, path)
	})
	if err != nil {
		return agenthub.FileDiff{}, err
	}
	return result.(agenthub.FileDiff), nil
}

// AgentHubPlanRestore works out what putting a turn's files back would do,
// without touching anything. The returned token is what AgentHubApplyRestore
// takes; the plan itself stays on the backend, so a client cannot name a tree
// of its own.
func (s *AppService) AgentHubPlanRestore(req agenthub.RestoreRequest) (agenthub.RestorePreview, error) {
	opaqueID := hubSessionID(req.SessionID)
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "plan_restore",
		"session_id": opaqueID,
		"mode":       string(req.Mode),
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.PlanRestore(ctx, req)
	})
	if err != nil {
		return agenthub.RestorePreview{}, err
	}
	return result.(agenthub.RestorePreview), nil
}

// AgentHubApplyRestore carries out a plan. overwrite names the conflicts the
// user chose to force; the backend honours only the ones that can be forced.
func (s *AppService) AgentHubApplyRestore(token string, overwrite []string) (agenthub.RestoreResult, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "restore:"+token, map[string]any{
		"action":    "apply_restore",
		"overwrite": len(overwrite),
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ApplyRestore(ctx, token, overwrite)
	})
	if err != nil {
		return agenthub.RestoreResult{}, err
	}
	return result.(agenthub.RestoreResult), nil
}

// AgentHubPlanRewind works out what taking a session back to a message would
// do — to the conversation, the files, or both — without touching anything.
func (s *AppService) AgentHubPlanRewind(req agenthub.RewindRequest) (agenthub.RewindPreview, error) {
	opaqueID := hubSessionID(req.SessionID)
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action":     "plan_rewind",
		"session_id": opaqueID,
		"mode":       string(req.Mode),
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.PlanRewind(ctx, req)
	})
	if err != nil {
		return agenthub.RewindPreview{}, err
	}
	return result.(agenthub.RewindPreview), nil
}

// AgentHubApplyRewind carries out a rewind.
func (s *AppService) AgentHubApplyRewind(token string, overwrite []string) (agenthub.RewindResult, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "restore:"+token, map[string]any{
		"action":    "apply_rewind",
		"overwrite": len(overwrite),
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.ApplyRewind(ctx, token, overwrite)
	})
	if err != nil {
		return agenthub.RewindResult{}, err
	}
	return result.(agenthub.RewindResult), nil
}

// AgentHubInterruptedRewinds lists the rewinds that never finished, so the
// session can offer to put its files back.
func (s *AppService) AgentHubInterruptedRewinds() ([]agenthub.CheckpointSummary, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "session:list", map[string]any{
		"action": "interrupted_rewinds",
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return svc.InterruptedRewinds(ctx)
	})
	if err != nil {
		return nil, err
	}
	if records, _ := result.([]agenthub.CheckpointSummary); records != nil {
		return records, nil
	}
	return []agenthub.CheckpointSummary{}, nil
}

// AgentHubDismissInterruptedRewind closes the banner without touching files.
func (s *AppService) AgentHubDismissInterruptedRewind(checkpointID string) error {
	_, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "restore:"+checkpointID, map[string]any{
		"action":        "dismiss_interrupted_rewind",
		"checkpoint_id": checkpointID,
	}, func(ctx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.DismissInterruptedRewind(ctx, checkpointID)
	})
	return err
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

// AgentHubRenameSession changes the title shown for a session.
func (s *AppService) AgentHubRenameSession(ctx context.Context, id, title string) error {
	opaqueID := hubSessionID(id)
	_, err := s.call(ctx, "builtin.agenthub", "agenthub:execute", "session:"+opaqueID, map[string]any{
		"action": "rename_session", "session_id": opaqueID,
	}, func(callCtx context.Context) (any, error) {
		svc, err := s.agentHubService()
		if err != nil {
			return nil, err
		}
		return nil, svc.RenameSession(callCtx, id, title)
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

// AgentHubPermissionFileDiff previews what a pending permission would do to
// each file it edits: the file as it is now and as it would be once allowed.
// Empty for a request that edits no file, and for one no longer pending.
//
// Read-only, and only for a request the hub is already holding: the path comes
// from the agent's own request, not from the caller, so this cannot be pointed
// at an arbitrary file.
func (s *AppService) AgentHubPermissionFileDiff(id string) ([]agenthub.PermissionFileDiff, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "permission:list", map[string]any{
		"action": "permission_file_diff",
		"id":     opaqueID,
	}, func(ctx context.Context) (any, error) {
		waiter, err := s.agentHubPermissions()
		if err != nil {
			return nil, err
		}
		var pending *agenthub.PendingPermission
		for _, p := range waiter.PendingList() {
			if p.ID == id {
				pending = &p
				break
			}
		}
		if pending == nil {
			return []agenthub.PermissionFileDiff{}, nil
		}
		cwd := ""
		if svc, err := s.agentHubService(); err == nil {
			if session, err := svc.GetStatus(ctx, pending.SessionID); err == nil {
				cwd = session.ProjectPath
			}
		}
		diffs := agenthub.PermissionFileDiffs(pending.Params, cwd, os.ReadFile)
		if diffs == nil {
			diffs = []agenthub.PermissionFileDiff{}
		}
		return diffs, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.PermissionFileDiff), nil
}

// AgentHubRespondElicitation answers a question an agent put to the user
// (elicitation/create). response is the ACP body — {"action":"accept",
// "content":{...}}, {"action":"decline"} or {"action":"cancel"}; the hub keeps
// only what the question asked for before it reaches the agent.
func (s *AppService) AgentHubRespondElicitation(id string, response json.RawMessage) error {
	opaqueID := recordID(id)
	_, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "elicitation:respond", map[string]any{
		"action": "respond_elicitation",
		"id":     opaqueID,
	}, func(context.Context) (any, error) {
		waiter, err := s.agentHubElicitations()
		if err != nil {
			return nil, err
		}
		return nil, waiter.Complete(id, response)
	})
	return err
}

// AgentHubPendingElicitations lists every agent question awaiting the user,
// oldest first, so the frontend can rebuild its queue on mount; live arrivals
// come through the agenthub:elicitation event.
func (s *AppService) AgentHubPendingElicitations() ([]agenthub.PendingElicitation, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:read", "elicitation:list", map[string]any{
		"action": "pending_elicitations",
	}, func(context.Context) (any, error) {
		waiter, err := s.agentHubElicitations()
		if err != nil {
			return nil, err
		}
		return waiter.PendingList(), nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]agenthub.PendingElicitation), nil
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

// AgentHubChooseDirectory opens the native folder picker and returns the chosen
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

// agentScratchDir is the parent of the folders sessions get when they are not
// started in a project. Under DevKit's own config dir, next to codegraph and
// mcp.sock, so nothing lands in the user's home by surprise.
func agentScratchDir(configDir string) string {
	return filepath.Join(configDir, "DevKit", "agent-scratch")
}

// AgentHubScratchRoot is where "Don't work in a project" sessions live. The
// sidebar gathers every session under it into one group instead of drawing a
// project per timestamped folder.
func (s *AppService) AgentHubScratchRoot() string {
	return agentScratchDir(s.container.ConfigDir)
}

// AgentHubNewScratchDirectory makes an empty folder for one session that is
// not started in a project. An agent needs a working directory — the hub
// refuses / and the home folder — so "no project" is a fresh folder of its own
// rather than a shared one, where two sessions would see each other's files.
func (s *AppService) AgentHubNewScratchDirectory() (string, error) {
	result, err := s.call(context.Background(), "builtin.agenthub", "agenthub:execute", "session:new", map[string]any{
		"action": "scratch_directory",
	}, func(context.Context) (any, error) {
		root := agentScratchDir(s.container.ConfigDir)
		if err := os.MkdirAll(root, 0o755); err != nil {
			return "", err
		}
		return os.MkdirTemp(root, time.Now().Format("20060102-150405")+"-")
	})
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// GitCommitTurn stages exactly what one agent turn changed and drafts its
// message. It is the bridge between two histories of one work tree: the
// agent's per-turn checkpoints and the user's commits.
//
// It stages and drafts, and stops. The message and the decision to commit stay
// with the person — GitCommit is a separate call.
func (s *AppService) GitCommitTurn(sessionID, checkpointID string) (app.TurnCommit, error) {
	_ = recordID(sessionID)
	result, err := s.call(context.Background(), "builtin.git", "git:write", "repo:stage", map[string]any{
		"action":  "commit_turn",
		"session": recordID(sessionID),
	}, func(ctx context.Context) (any, error) {
		return app.CommitTurn(ctx, s.container, sessionID, checkpointID)
	})
	if err != nil {
		return app.TurnCommit{}, err
	}
	return result.(app.TurnCommit), nil
}
