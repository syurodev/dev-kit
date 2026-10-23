package bridge

import (
	"context"
	"devkit/internal/app"
	"devkit/internal/capabilities"
	"errors"
	"io/fs"
	"log"
	"net/url"
	"os"
	"strings"
	"unicode"

	"devkit/internal/accounts"
	"devkit/internal/agenthub"
	"devkit/internal/agentruntime"
	"devkit/internal/apiclient"
	"devkit/internal/apperrors"
	"devkit/internal/cloudconfig"
	"devkit/internal/codegraph"
	"devkit/internal/database"
	"devkit/internal/environments"
	gitservice "devkit/internal/git"
	"devkit/internal/gitssh"
	"devkit/internal/interaction"
	"devkit/internal/kafka"
	"devkit/internal/mcp/install"
	"devkit/internal/notes"
	"devkit/internal/pdfdoc"
	"devkit/internal/preferences"
	"devkit/internal/syncclient"
	"devkit/internal/tasks"
	"devkit/internal/telegramcontrol"
	"devkit/internal/vault"
	"devkit/internal/workspace"
	"devkit/internal/wssearch"
)

const (
	codeValidationFailed   = "validation_failed"
	codeVaultLocked        = "vault_locked"
	codePermissionDenied   = "permission_denied"
	codeConnectionFailed   = "connection_failed"
	codeOperationCancelled = "operation_cancelled"
	codeInternalError      = "internal_error"
	// codeGitAuthFailed: a remote turned away the identity git offered. Its own
	// code because the answer is about which account or key is in use — the
	// message names it — and a retry as it stands will be refused again.
	codeGitAuthFailed = "git_auth_failed"
	// codeUnsupportedProject is its own code rather than a validation failure
	// so the UI can tell "no analyzer reads this project" from "analysis
	// broke". The folder opens for its files either way; a graph query on it
	// answers with this code, and the UI shows "files only" where it would
	// otherwise offer a retry — on a folder that will never analyze, a button
	// that cannot work.
	codeUnsupportedProject = "unsupported_project"
	// codeProjectChanged: the CodeGraph project is not the one the caller
	// thinks is open. Its own code because the UI's answer is to re-read which
	// project is open (and keep the unsaved buffer), never to retry the call.
	codeProjectChanged = "project_changed"
	// codeFileConflict: a save was refused because the file on disk is not the
	// revision the buffer was read from. The edits are untouched; the UI's
	// answer is to compare, reload or overwrite on purpose — never to retry.
	codeFileConflict = "file_conflict"
	// codeFolderNotInWorkspace: the folder a call names is not (or is no longer)
	// one of the workspace's. Its own code so a save into a removed folder keeps
	// the buffer as a draft to copy out instead of reporting a failure to retry.
	codeFolderNotInWorkspace = "folder_not_in_workspace"
	// codeInvalidQuery: a search the engine cannot compile (a bad regex).
	codeInvalidQuery = "invalid_query"
	// A statement the engine refused. Not connection_failed: the connection is
	// fine, and the frontend tells the two apart to decide whether to offer a
	// reconnect or just show what the engine said.
	codeQueryFailed = "query_failed"
	// A task move that lost its race: the card was moved somewhere else
	// between being drawn and being dropped. Its own code because the board's
	// answer is to show where the task is now, not to report a failure.
	codeTaskStatusChanged = "task_status_changed"
	// The three states of an agent runtime the UI answers with an action of
	// its own: offer the install, show what Node is missing, show the log.
	codeAgentNotInstalled  = "agent_not_installed"
	codeRuntimeRequirement = "runtime_requirement"
	codeInstallFailed      = "install_failed"
	codeAgentAuthRequired  = "agent_auth_required"
	// codeAgentAPIKeyUnusable is a sign-in problem a sign-in cannot fix: the
	// API key chosen in Settings is gone or was refused, and it wins over
	// the vendor's own sign-in at every spawn. The UI points at Settings.
	codeAgentAPIKeyUnusable = "agent_api_key_unusable"
	// codeAgentUsageLimit is the vendor refusing a turn because the account's
	// allowance is spent — a wait or an upgrade, not a restart. The message
	// is the vendor's own, which says when the limit lifts.
	codeAgentUsageLimit = "agent_usage_limit"
	// codeAgentTurnFailed is a turn the vendor ended with an error of its own
	// while the agent stays up: the session is fine, the vendor's message says
	// why this turn did not finish.
	codeAgentTurnFailed = "agent_turn_failed"
)

var safeValidationMessages = map[string]struct{}{
	"title is required":         {},
	"name is required":          {},
	"host is required":          {},
	"username is required":      {},
	"unsupported auth method":   {},
	"master password too short": {},
	// A wrong password has to reach the UI as a sentence, or the unlock gate
	// and the reveal dialog can only say that something went wrong — which is
	// the one thing the user can do nothing about. It tells an attacker
	// nothing they could not learn by trying again.
	"invalid master password":                                {},
	"forward direction is required":                          {},
	"unsupported forward direction":                          {},
	"bind host must be loopback":                             {},
	"bind port must be between 0 and 65535":                  {},
	"target host is required":                                {},
	"target host is invalid":                                 {},
	"target port must be between 1 and 65535":                {},
	"environment tag is in use":                              {},
	"environment tag is locked":                              {},
	"bootstrap_servers is required":                          {},
	"env is required":                                        {},
	"username is required for SASL authentication":           {},
	"sasl_mechanism is required for SASL security protocols": {},
	"unsupported sasl mechanism":                             {},
	"unsupported security protocol":                          {},
	"agenthub: no pending permission":                        {},
	"agenthub: invalid permission id":                        {},
	"agenthub: no pending elicitation":                       {},
	"agenthub: invalid elicitation id":                       {},
}

func publicError(subject string, err error) *apperrors.AppError {
	var telegramError *telegramcontrol.SettingsError
	if errors.As(err, &telegramError) {
		return apperrors.New(codeValidationFailed, telegramError.Message, true)
	}
	if mapped := workspaceMemoryPublicError(err); mapped != nil {
		return mapped
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return apperrors.New(codeOperationCancelled, "Operation cancelled.", true)
	}
	if errors.Is(err, interaction.ErrInteractionTimeout) {
		return apperrors.New(codeOperationCancelled, "Operation cancelled.", true)
	}
	if errors.Is(err, interaction.ErrConfirmationDenied) {
		return apperrors.New(codePermissionDenied, "Permission denied.", false)
	}
	if subject == "builtin.git" && errors.Is(err, interaction.ErrSelectionRequired) {
		return apperrors.New(codeValidationFailed, "More than one account could authenticate this remote and nothing here can ask which. Bind an account to the remote, or run it from the DevKit window.", true)
	}
	if errors.Is(err, interaction.ErrSelectionRequired) {
		return apperrors.New(codeValidationFailed, "Selection required.", true)
	}
	if errors.Is(err, interaction.ErrConfirmationRequired) {
		return apperrors.New(codeValidationFailed, "Confirmation required.", true)
	}
	if errors.Is(err, interaction.ErrNoPendingInteraction) {
		return apperrors.New(codeValidationFailed, "No pending interaction.", true)
	}
	if errors.Is(err, interaction.ErrInvalidInteractionID) {
		return apperrors.New(codeValidationFailed, "Invalid interaction id.", true)
	}
	if errors.Is(err, errInteractionUnavailable) {
		return apperrors.New(codeInternalError, "Interaction is not available.", true)
	}
	if errors.Is(err, vault.ErrLocked) {
		return apperrors.New(codeVaultLocked, "Vault is locked or unavailable.", true)
	}
	if errors.Is(err, environments.ErrTagInUse) {
		return apperrors.New(codeValidationFailed, environments.ErrTagInUse.Error(), true)
	}
	if errors.Is(err, environments.ErrTagLocked) {
		return apperrors.New(codeValidationFailed, "environment tag is locked", true)
	}
	if errors.Is(err, workspace.ErrInvalidInstructions) {
		return apperrors.New(codeValidationFailed, "Workspace instructions must be valid UTF-8 and at most 32 KiB.", true)
	}
	if errors.Is(err, workspace.ErrNameRequired) {
		return apperrors.New(codeValidationFailed, "Give the project a name.", true)
	}
	if errors.Is(err, workspace.ErrWorkspaceNotFound) {
		return apperrors.New(codeValidationFailed, "That project no longer exists.", true)
	}
	if errors.Is(err, workspace.ErrServiceNotFound) {
		return apperrors.New(codeFolderNotInWorkspace, "That folder is no longer part of this workspace.", true)
	}
	// A board declared a verify command but this machine cannot resolve where
	// it runs: no workspace binding, or a binding whose service does not
	// resolve here. Falling back to asking a person would silently swap the
	// board's rule for a different one nobody agreed to, so the service
	// refuses instead — this is what tells the person why, and what to do.
	if errors.Is(err, tasks.ErrBoardUnbound) {
		return apperrors.New(codeValidationFailed, "This board isn't linked to a workspace on this machine. Link it to a workspace to run its verify command.", true)
	}
	// The rest of the tasks sentinels are refusals a person can act on. Left
	// to the fallback they all read "An unexpected error occurred.", which is
	// what a board said when two people moved the same card.
	if subject == "builtin.tasks" {
		switch {
		case errors.Is(err, tasks.ErrStatusChanged):
			return apperrors.New(codeTaskStatusChanged, "This task was moved somewhere else in the meantime.", true)
		case errors.Is(err, tasks.ErrNotFound):
			return apperrors.New(codeValidationFailed, "That task or board no longer exists.", true)
		case errors.Is(err, tasks.ErrDependencyCycle):
			return apperrors.New(codeValidationFailed, "That dependency would make the task wait on itself.", true)
		case errors.Is(err, tasks.ErrParentHasParent):
			return apperrors.New(codeValidationFailed, "A subtask cannot have subtasks of its own.", true)
		case errors.Is(err, tasks.ErrClaimed):
			return apperrors.New(codeValidationFailed, "Another agent session holds this task.", true)
		case errors.Is(err, tasks.ErrNotAssignable):
			return apperrors.New(codeValidationFailed, "That task is finished or archived, so it cannot be given to a session.", true)
		case errors.Is(err, tasks.ErrVerifying):
			return apperrors.New(codeValidationFailed, "This task is already being verified.", true)
		case errors.Is(err, tasks.ErrInvalid):
			return apperrors.New(codeValidationFailed, "That change is not valid for this task.", true)
		}
	}
	// Notes input an agent gets wrong over MCP as often as a person does in
	// the UI. The message names what was given and what is accepted; as
	// internal_error it said only that DevKit broke, which it had not.
	if errors.Is(err, notes.ErrInvalidColor) || errors.Is(err, notes.ErrTagTooLong) || errors.Is(err, notes.ErrTooManyTags) {
		return apperrors.New(codeValidationFailed, err.Error(), true)
	}
	if errors.Is(err, app.ErrCodexAPIKeyAccount) {
		return apperrors.New(codeValidationFailed, codexAPIKeyAccountMessage, true)
	}
	if errors.Is(err, errUpdateCheckFailed) {
		return apperrors.New(codeConnectionFailed, "Could not check for updates. Check your internet connection and try again.", true)
	}
	var invalidCustomization *agenthub.CustomizationError
	if errors.As(err, &invalidCustomization) {
		return apperrors.New(codeValidationFailed, invalidCustomization.Message, true)
	}
	if errors.Is(err, preferences.ErrCustomizationConflict) {
		return apperrors.New(codeValidationFailed, "Agent configuration changed. Refresh and apply your changes again.", true)
	}
	if errors.Is(err, preferences.ErrModelProfilesConflict) {
		return apperrors.New(codeValidationFailed, "The model profiles changed. Refresh and apply your changes again.", true)
	}
	if errors.Is(err, preferences.ErrInvalidModelProfiles) {
		// The validator's own sentence names the field and the limit; it holds
		// nothing the person did not type.
		return apperrors.New(codeValidationFailed, "Model profiles: "+strings.TrimPrefix(err.Error(), preferences.ErrInvalidModelProfiles.Error()+": "), true)
	}
	if errors.Is(err, preferences.ErrSubagentLimitsConflict) {
		return apperrors.New(codeValidationFailed, "The subagent limits changed. Refresh and apply your change again.", true)
	}
	if errors.Is(err, preferences.ErrInvalidSubagentLimits) {
		return apperrors.New(codeValidationFailed, "Subagent limits: "+strings.TrimPrefix(err.Error(), preferences.ErrInvalidSubagentLimits.Error()+": "), true)
	}
	if errors.Is(err, preferences.ErrAccountPolicyConflict) {
		return apperrors.New(codeValidationFailed, "The account switching rules changed. Refresh and apply your change again.", true)
	}
	if errors.Is(err, preferences.ErrInvalidAccountPolicy) {
		// The validator's own sentence names the vendor and the limit; it holds nothing the person did not type.
		return apperrors.New(codeValidationFailed, "Account switching rules: "+sentenceAfter(err, preferences.ErrInvalidAccountPolicy, "those rules cannot be saved"), true)
	}
	if errors.Is(err, preferences.ErrInvalidTheme) {
		return apperrors.New(codeValidationFailed, preferences.ErrInvalidTheme.Error(), true)
	}
	if errors.Is(err, preferences.ErrInvalidCallVisibility) {
		return apperrors.Wrap(codeValidationFailed, "That call cannot be marked.", true, preferences.ErrInvalidCallVisibility)
	}
	if errors.Is(err, preferences.ErrTooManyCallVisibility) {
		return apperrors.Wrap(codeValidationFailed, "You can keep at most 500 always show/hide choices.", true, preferences.ErrTooManyCallVisibility)
	}
	// Authorization failures are classified before subject-specific adapters so
	// adding a new module cannot accidentally hide a denied capability as a
	// connection or service failure.
	if errors.Is(err, capabilities.ErrPermissionDenied) {
		return apperrors.New(codePermissionDenied, "Permission denied.", false)
	}
	var existing *apperrors.AppError
	if errors.As(err, &existing) {
		return publicErrorForCode(subject, err, existing)
	}
	if subject == "builtin.sync" {
		switch {
		case errors.Is(err, cloudconfig.ErrUnavailable):
			return apperrors.New(codeConnectionFailed, "Cloud configuration is unavailable. Connect to the internet once or set DEVKIT_OIDC_ISSUER.", true)
		case errors.Is(err, cloudconfig.ErrAppBelowMinimum):
			return apperrors.New(codeConnectionFailed, "This DevKit version is too old for Sync. Please update.", true)
		case errors.Is(err, syncclient.ErrNotConnected):
			return apperrors.New(codeConnectionFailed, "Sync is not connected.", true)
		case errors.Is(err, syncclient.ErrSyncInProgress):
			return apperrors.New(codeConnectionFailed, "Sync is already in progress.", true)
		case errors.Is(err, syncclient.ErrInvalidDependencies):
			return apperrors.New(codeConnectionFailed, "Sync is unavailable.", true)
		case errors.Is(err, syncclient.ErrConnectAuthenticationRejected):
			return apperrors.New(codeConnectionFailed, "The sync gateway rejected the sign-in token.", true)
		case errors.Is(err, syncclient.ErrConnectServiceUnavailable):
			return apperrors.New(codeConnectionFailed, "The sync service is temporarily unavailable.", true)
		case errors.Is(err, syncclient.ErrLocalCaptureFailed):
			return apperrors.New(codeConnectionFailed, "Local changes could not be prepared for sync.", true)
		case errors.Is(err, syncclient.ErrPushFailed):
			return apperrors.New(codeConnectionFailed, "Sync upload failed.", true)
		case errors.Is(err, syncclient.ErrPullFailed):
			return apperrors.New(codeConnectionFailed, "Sync download failed.", true)
		case errors.Is(err, syncclient.ErrApplyFailed):
			return apperrors.New(codeConnectionFailed, "Downloaded sync data could not be applied.", true)
		default:
			// Never expose an upstream response, endpoint, or bearer token through
			// the public bridge error contract.
			return apperrors.New(codeConnectionFailed, "Sync operation failed.", true)
		}
	}
	if subject == "builtin.kafka" {
		switch {
		case errors.Is(err, errKafkaUnavailable):
			return apperrors.New(codeInternalError, "Kafka is not available.", true)
		case errors.Is(err, kafka.ErrNotConnected):
			return apperrors.New(codeConnectionFailed, "Kafka is not connected.", true)
		case errors.Is(err, kafka.ErrInsecureRemotePLAINTEXT):
			return apperrors.New(codeConnectionFailed, "Plaintext Kafka to a remote host is not allowed.", true)
		}
	}
	if subject == "builtin.agenthub" {
		// Runtime errors first: a session that cannot start because Node is
		// missing also carries ErrProcessFailed, and "Agent process failed."
		// would hide the one thing the user can fix.
		var runtimeErr *agentruntime.Error
		reason := ""
		if errors.As(err, &runtimeErr) {
			reason = runtimeErr.Reason
		}
		switch {
		case errors.Is(err, errAgentRuntimesUnavailable):
			return apperrors.New(codeInternalError, "Agent runtimes are not available.", true)
		case errors.Is(err, agenthub.ErrAgentNotInstalled):
			return apperrors.New(codeAgentNotInstalled, agentNotInstalledMessage(agenthub.ErrorVendor(err)), true)
		case errors.Is(err, agentruntime.ErrNotInstalled):
			return apperrors.New(codeValidationFailed, "That agent version is not installed.", true)
		case errors.Is(err, agentruntime.ErrRequirementNotMet):
			message := "A requirement for the agent is not met."
			if reason != "" {
				message = "A requirement for the agent is not met: " + reason
			}
			return apperrors.New(codeRuntimeRequirement, message, true)
		case errors.Is(err, agentruntime.ErrInstallFailed):
			message := "Installing the agent failed."
			if reason != "" {
				message = "Installing the agent failed: " + reason
			}
			return apperrors.New(codeInstallFailed, message, true)
		case errors.Is(err, agentruntime.ErrInstallAlreadyRunning):
			return apperrors.New(codeValidationFailed, "An install is already running for this agent.", true)
		case errors.Is(err, agentruntime.ErrVersionInUse):
			return apperrors.New(codeValidationFailed, "That version is in use. Switch to another version before removing it.", true)
		case errors.Is(err, agentruntime.ErrUnknownAgent):
			return apperrors.New(codeValidationFailed, "Unknown agent.", true)
		case errors.Is(err, agentruntime.ErrInvalidInput):
			message := "Invalid agent runtime option."
			if reason != "" {
				message = "Invalid agent runtime option: " + reason
			}
			return apperrors.New(codeValidationFailed, message, true)
		}
		switch {
		case errors.Is(err, agenthub.ErrUnavailableInServerMode):
			return apperrors.New(codeValidationFailed, "Agent Hub needs the desktop app. In server mode an agent could answer its own permission prompts.", false)
		case errors.Is(err, errAgentHubUnavailable), errors.Is(err, agenthub.ErrUnavailable):
			return apperrors.New(codeInternalError, "Agent Hub is not available.", true)
		case errors.Is(err, agenthub.ErrSessionClosed):
			return apperrors.New(codeValidationFailed, "This session is closed. Send a message to reopen it.", true)
		case errors.Is(err, agenthub.ErrSessionNotFound):
			return apperrors.New(codeValidationFailed, "Agent session not found.", true)
		case errors.Is(err, agenthub.ErrSessionBusy):
			return apperrors.New(codeValidationFailed, "Agent session is busy.", true)
		case errors.Is(err, agenthub.ErrDepthLimitExceeded):
			return apperrors.New(codeValidationFailed, "Agent session depth limit exceeded.", true)
		case errors.Is(err, agenthub.ErrCycleDetected):
			return apperrors.New(codeValidationFailed, "Agent session cycle detected.", true)
		case errors.Is(err, agenthub.ErrVendorUnknown):
			return apperrors.New(codeValidationFailed, "Unknown agent vendor.", true)
		case errors.Is(err, agenthub.ErrPermissionDenied):
			return apperrors.New(codePermissionDenied, "Permission denied.", false)
		case errors.Is(err, agenthub.ErrCancelled):
			return apperrors.New(codeOperationCancelled, "Operation cancelled.", true)
		case errors.Is(err, agenthub.ErrSandboxUnavailable):
			return apperrors.New(codeConnectionFailed, "Agent sandbox is unavailable.", true)
		// Before auth-required, which it always also is.
		case errors.Is(err, agenthub.ErrAgentAPIKeyUnusable):
			return apperrors.New(codeAgentAPIKeyUnusable, agentAPIKeyUnusableMessage(agenthub.ErrorVendor(err)), true)
		case errors.Is(err, agenthub.ErrAgentAuthRequired):
			return apperrors.New(codeAgentAuthRequired, agentAuthRequiredMessage(agenthub.ErrorVendor(err)), true)
		case errors.Is(err, agenthub.ErrAgentUsageLimit):
			return apperrors.New(codeAgentUsageLimit, agentUsageLimitMessage(agenthub.ErrorVendor(err), agenthub.ErrorVendorMessage(err)), true)
		case errors.Is(err, agenthub.ErrTurnFailed):
			return apperrors.New(codeAgentTurnFailed, agentTurnFailedMessage(agenthub.ErrorVendor(err), agenthub.ErrorVendorMessage(err)), true)
		case errors.Is(err, agenthub.ErrUnsupported):
			return apperrors.New(codeValidationFailed, "This agent doesn't support that.", true)
		case errors.Is(err, agenthub.ErrAuthMethodUnknown):
			return apperrors.New(codeValidationFailed, "That sign-in method isn't offered by this agent.", true)
		case errors.Is(err, agenthub.ErrModelsUnavailable):
			return apperrors.New(codeConnectionFailed, agentDisplayName(agenthub.ErrorVendor(err))+" didn't report which models it offers. Try Refresh models, or check that it is signed in.", true)
		// Vendor accounts. A home error wraps a path and the OS's words, a usage check the
		// CLI's: none of that reaches the window. ErrAccountHome comes before
		// ErrInvalidAccount, which it can also be wrapped by.
		case errors.Is(err, agenthub.ErrAllAccountsLimited):
			return apperrors.New(codeAgentUsageLimit, capitalised(sentenceAfter(err, agenthub.ErrAllAccountsLimited, "every account of this vendor has reached its limit")), true)
		case errors.Is(err, agenthub.ErrAccountNotFound):
			return apperrors.New(codeValidationFailed, "That account no longer exists.", true)
		case errors.Is(err, agenthub.ErrAccountHome):
			return apperrors.New(codeValidationFailed, "DevKit could not prepare a folder for that account. Nothing was changed.", true)
		case errors.Is(err, agenthub.ErrInvalidAccount):
			return apperrors.New(codeValidationFailed, "Account: "+sentenceAfter(err, agenthub.ErrInvalidAccount, "that is not a valid account"), true)
		case errors.Is(err, agenthub.ErrAccountLabelTaken):
			return apperrors.New(codeValidationFailed, "You already have an account with that name.", true)
		case errors.Is(err, agenthub.ErrAccountInUse):
			return apperrors.New(codeValidationFailed, "A session is using that account. Close it first.", true)
		case errors.Is(err, agenthub.ErrUsageNotCheckable):
			return apperrors.New(codeValidationFailed, "This account's usage cannot be read right now.", true)
		case errors.Is(err, agenthub.ErrAccountsUnavailable):
			return apperrors.New(codeValidationFailed, "Accounts are not available.", true)
		case errors.Is(err, agenthub.ErrProcessFailed):
			return apperrors.New(codeConnectionFailed, "Agent process failed.", true)
		case errors.Is(err, agenthub.ErrNotDescendant):
			return apperrors.New(codePermissionDenied, "Agent session is outside the caller's tree.", false)
		case errors.Is(err, agenthub.ErrCwdOutsideParent):
			return apperrors.New(codeValidationFailed, "Agent cwd must be inside the parent session's project.", true)
		case errors.Is(err, agenthub.ErrInvalidCwd):
			return apperrors.New(codeValidationFailed, "Agent cwd is not a valid project directory.", true)
		case errors.Is(err, agenthub.ErrWorkspaceAmbiguous):
			return apperrors.New(codeValidationFailed, "This folder belongs to several workspaces. Choose one for this session.", true)
		case errors.Is(err, agenthub.ErrWorkspaceMismatch):
			return apperrors.New(codeValidationFailed, "The chosen workspace does not contain this session's folder.", true)
		case errors.Is(err, agenthub.ErrInvalidConfigOption):
			// A model or effort that the vendor does not offer carries the
			// sentence that says what would work (agenthub.ModelChoiceError:
			// built from the vendor's list and the caller's own words). Any
			// other invalid setting stays the generic line.
			if detail, ok := agenthub.ModelChoiceDetail(err); ok && validModelChoiceDetail(detail) {
				return apperrors.New(codeValidationFailed, "Choose a valid agent setting: "+detail, true)
			}
			return apperrors.New(codeValidationFailed, "Choose a valid agent setting.", true)
		case errors.Is(err, agenthub.ErrInvalidPrompt):
			return apperrors.New(codeValidationFailed, invalidPromptMessage(err), true)
		case errors.Is(err, agenthub.ErrCallerMismatch):
			return apperrors.New(codeValidationFailed, "callerSessionId does not match the calling agent.", true)
		case errors.Is(err, agenthub.ErrProxyNotFound):
			return apperrors.New(codeConnectionFailed, "DevKit MCP proxy (devkit-mcp) was not found. Build it with: task mcp:install", true)
		}
	}
	if _, safe := safeValidationMessages[err.Error()]; safe {
		return apperrors.New(codeValidationFailed, err.Error(), true)
	}
	if subject == "builtin.codegraph" {
		switch {
		case errors.Is(err, codegraph.ErrNoActiveProject):
			return apperrors.Wrap(codeValidationFailed, "No active CodeGraph project.", true, codegraph.ErrNoActiveProject)
		// Before the generic "not ready": an index that failed because its
		// analyzer was never built needs a build, and "wait for indexing" would
		// send the person away from the setup panel that offers one.
		case errors.Is(err, codegraph.ErrAnalyzerNotConfigured):
			return apperrors.Wrap(codeConnectionFailed, "CodeGraph's analyzer for this project is not built, so it cannot be indexed. Build it from CodeGraph's setup panel, then reopen the project.", true, codegraph.ErrAnalyzerNotConfigured)
		case errors.Is(err, codegraph.ErrIndexNotReady):
			return apperrors.Wrap(codeValidationFailed, "CodeGraph index not ready. Wait for indexing to finish or reopen the project to retry.", true, err)
		case errors.Is(err, codegraph.ErrNodeNotFound):
			return apperrors.Wrap(codeValidationFailed, "CodeGraph node not found.", true, codegraph.ErrNodeNotFound)
		// The analyzer exists but the runtime that launches it does not — a
		// packaged install ships the artifact and not a JDK. CodeGraph's setup
		// panel says which program is missing and what to install; this is the
		// backstop for anything that reaches an index without passing it.
		// The folder holds nothing any analyzer recognises. Named separately
		// from a failed analyze because it is not a failure: the user picked a
		// folder, and the answer is which kinds of project CodeGraph reads.
		case errors.Is(err, codegraph.ErrUnsupportedProject):
			// No language names: the list grows with every analyzer, and a
			// message naming the ones that exist read as "this is a Java tool".
			// The folder itself is fine — it is open for its files.
			return apperrors.Wrap(codeUnsupportedProject, "CodeGraph has no analyzer for this project's language, so there is no graph to show. Its files, the editor and git status still work.", true, codegraph.ErrUnsupportedProject)
		case errors.Is(err, codegraph.ErrToolchainIncomplete):
			return apperrors.Wrap(codeConnectionFailed, "The CodeGraph analyzer cannot run: "+err.Error()+".", true, codegraph.ErrToolchainIncomplete)
		case errors.Is(err, codegraph.ErrNoPlan):
			return apperrors.Wrap(codeValidationFailed, "No active plan.", true, codegraph.ErrNoPlan)
		case errors.Is(err, codegraph.ErrPlanNodeNotFound):
			return apperrors.Wrap(codeValidationFailed, "Plan node not found.", true, codegraph.ErrPlanNodeNotFound)
		case errors.Is(err, codegraph.ErrInvalidPlan):
			return apperrors.Wrap(codeValidationFailed, "Invalid plan.", true, codegraph.ErrInvalidPlan)
		case errors.Is(err, codegraph.ErrTooManyNodes):
			return apperrors.Wrap(codeValidationFailed, "Too many CodeGraph nodes in one request.", true, codegraph.ErrTooManyNodes)
		case errors.Is(err, codegraph.ErrPathOutsideProject):
			return apperrors.Wrap(codeValidationFailed, "File path must stay inside the active CodeGraph project.", true, err)
		case errors.Is(err, os.ErrNotExist):
			return apperrors.Wrap(codeValidationFailed, codeGraphNotFoundMessage(err), true, err)
		case errors.Is(err, codegraph.ErrBinaryFile):
			return apperrors.Wrap(codeValidationFailed, "This file is not text and cannot be opened in the editor.", true, codegraph.ErrBinaryFile)
		case errors.Is(err, codegraph.ErrFileTooLarge):
			return apperrors.Wrap(codeValidationFailed, "This file is too large to open in the editor.", true, codegraph.ErrFileTooLarge)
		case errors.Is(err, codegraph.ErrFileConflict):
			return apperrors.Wrap(codeFileConflict, "The file changed on disk after it was opened. Nothing was saved; your edits are still in the editor.", true, codegraph.ErrFileConflict)
		case errors.Is(err, errRevealUnavailable):
			return apperrors.New(codeValidationFailed, "Showing a file in the file manager is only available in the desktop app.", true)
		case errors.Is(err, app.ErrPDFChanged):
			return apperrors.Wrap(codePDFChanged, "This PDF changed on disk. Open it again to see the new version.", true, app.ErrPDFChanged)
		case errors.Is(err, app.ErrPDFUnavailable), errors.Is(err, pdfdoc.ErrUnavailable):
			return apperrors.Wrap(codePDFUnavailable, "DevKit's PDF reader is not available.", true, err)
		case errors.Is(err, pdfdoc.ErrPasswordRequired):
			return apperrors.Wrap(codeValidationFailed, "This PDF is password-protected, and DevKit cannot open it.", true, pdfdoc.ErrPasswordRequired)
		case errors.Is(err, pdfdoc.ErrNotPDF):
			return apperrors.Wrap(codeValidationFailed, "This file is not a PDF.", true, pdfdoc.ErrNotPDF)
		case errors.Is(err, pdfdoc.ErrCorrupt):
			return apperrors.Wrap(codeValidationFailed, "This PDF is damaged and cannot be read.", true, pdfdoc.ErrCorrupt)
		case errors.Is(err, pdfdoc.ErrTooLarge), errors.Is(err, pdfdoc.ErrTooManyPages):
			return apperrors.Wrap(codeValidationFailed, "This PDF is too large to open.", true, err)
		case errors.Is(err, pdfdoc.ErrPageRange):
			return apperrors.Wrap(codeValidationFailed, "That page is not in this PDF.", true, pdfdoc.ErrPageRange)
		case errors.Is(err, wssearch.ErrEmptyQuery):
			return apperrors.Wrap(codeInvalidQuery, "Type something to search for.", true, wssearch.ErrEmptyQuery)
		case invalidQuery(err) != nil:
			return apperrors.Wrap(codeInvalidQuery, "Invalid search: "+invalidQuery(err).Reason+".", true, err)
		case errors.Is(err, codegraph.ErrProjectChanged):
			return apperrors.Wrap(codeProjectChanged, "The open CodeGraph project changed. Nothing was written; reopen the file from the current project.", true, codegraph.ErrProjectChanged)
		}
	}
	if subject == "builtin.mcpinstall" {
		switch {
		case errors.Is(err, install.ErrUnknownAgent),
			errors.Is(err, install.ErrUnknownScope),
			errors.Is(err, install.ErrProjectPathRequired),
			errors.Is(err, install.ErrProjectPathMissing),
			errors.Is(err, install.ErrCommandRequired),
			errors.Is(err, install.ErrCommandNotAbsolute),
			errors.Is(err, install.ErrProxyNotFound):
			return apperrors.New(codeValidationFailed, err.Error(), true)
		}
	}
	// The engine's own answer to the SQL the user typed, passed through whole.
	// Everything else on this subject has its secrets masked below: connecting happens
	// with a DSN in hand, so a dial or an auth failure can carry a host, a
	// username, and — if a driver echoes what it was given — a password. A
	// statement's error carries none of that, and it is the single most useful
	// sentence the console can show: "relation \"user\" does not exist" and
	// its HINT, the SQLSTATE a banner keys on, the constraint that was
	// violated. Redacting it to "Connection failed." was not redaction, it was
	// a wrong answer — the connection was fine.
	var statement *database.StatementError
	if subject == "builtin.database" && errors.As(err, &statement) {
		return apperrors.Wrap(codeQueryFailed, statement.Error(), true, err)
	}
	// A console is a file in the app's own directory. Failing to write one is
	// not a connection failing, and saying so sends the reader to the database
	// they were about to blame. The path stays out of the message; the cause
	// is in the wrapped error, which is logged.
	if subject == "builtin.database" && errors.Is(err, database.ErrConsoleStore) {
		return apperrors.Wrap(codeValidationFailed, "The query console could not be written to disk.", true, err)
	}
	if subject == "builtin.database" && errors.Is(err, database.ErrConsoleStoreUnavailable) {
		return apperrors.Wrap(codeValidationFailed, "Query consoles are not being stored in this build.", true, err)
	}
	if subject == "builtin.accounts" {
		switch {
		case errors.Is(err, accounts.ErrKeyInvalid):
			return apperrors.New(codeValidationFailed, "That is not a private key ssh can read. Paste the whole key, including the BEGIN and END lines.", true)
		case errors.Is(err, errSSHImportUnavailable):
			return apperrors.New(codeValidationFailed, "Importing a key from disk is only available in the desktop app: this window cannot be trusted to name a file.", true)
		case errors.Is(err, fs.ErrNotExist):
			return apperrors.New(codeValidationFailed, "That file no longer exists.", true)
		case errors.Is(err, errSSHKeyNotOffered):
			return apperrors.New(codeValidationFailed, "Choose the key from the list, or with Choose another file.", true)
		case errors.Is(err, accounts.ErrKeyExists):
			var exists *accounts.KeyExistsError
			if errors.As(err, &exists) {
				return apperrors.New(codeValidationFailed, "This key is already saved as \u201c"+exists.Label+"\u201d for that provider.", true)
			}
			return apperrors.New(codeValidationFailed, "This key is already saved for that provider.", true)
		case errors.Is(err, accounts.ErrLinkMismatch):
			return apperrors.New(codeValidationFailed, "A token can only go with an SSH key of the same provider, and neither can be a connection's password.", true)
		case errors.Is(err, accounts.ErrLabelInvalid):
			return apperrors.New(codeValidationFailed, "Give the account a name of up to 120 characters without line breaks. An external sign-in cannot be renamed.", true)
		case errors.Is(err, accounts.ErrIdentityInvalid):
			return apperrors.New(codeValidationFailed, "Enter both a name and an email address (like name@example.com) for commits, or clear both.", true)
		case errors.Is(err, accounts.ErrTokenInvalid):
			return apperrors.New(codeValidationFailed, "That does not look like a token: it is empty or has spaces or line breaks in it. Paste only the token.", true)
		case errors.Is(err, accounts.ErrUsernameRequired):
			return apperrors.New(codeValidationFailed, "This provider needs the username that goes with the token.", true)
		case errors.Is(err, accounts.ErrKeyPassphrase):
			return apperrors.New(codeValidationFailed, "This key is encrypted. Enter its passphrase, or check the one you entered.", true)
		}
	}
	if subject == "builtin.git" {
		// An SSH key that was meant to be used and could not be: the command
		// stopped rather than push as some other identity, and the person needs
		// to know that is why.
		switch {
		case errors.Is(err, gitssh.ErrUnsupported):
			return apperrors.New(codeValidationFailed, "SSH keys from Accounts are not supported on this platform yet, so nothing was sent. Remove the account from this remote to use git's own SSH configuration.", true)
		case errors.Is(err, gitssh.ErrNoSSH):
			return apperrors.New(codeValidationFailed, "There is no ssh program on this machine, which git needs to reach an SSH remote.", true)
		case errors.Is(err, gitssh.ErrNotAKey), errors.Is(err, gitssh.ErrPassphraseRequired), errors.Is(err, gitssh.ErrBadPassphrase):
			return apperrors.New(codeValidationFailed, "The SSH key of the account chosen for this remote can no longer be opened, so nothing was sent. Add the key again in Accounts.", true)
		}
		if refusal := gitRefusalError(err); refusal != nil {
			return refusal
		}
		var authErr *gitservice.AuthError
		if errors.As(err, &authErr) {
			// The cause is git's own stderr, which can quote the remote's
			// address; the public message names only the remote and account.
			return apperrors.Wrap(codeGitAuthFailed, gitAuthMessage(authErr), true, err)
		}
	}
	if subject == "builtin.apiclient" {
		if errors.Is(err, errNoSaveDialog) {
			return apperrors.Wrap(codeValidationFailed, "There is no save dialog here (a browser window has none) — use Copy instead.", true, err)
		}
		if message, ok := apiClientMessage(err); ok {
			return apperrors.Wrap(codeValidationFailed, message, true, err)
		}
		// A request that never got an answer: refused, timed out, no such
		// host, a certificate the system does not trust. The network's own
		// sentence is the whole point of an API client's error — but not the
		// *url.Error around it, which quotes the address, and an address with
		// variables filled in can carry a secret one.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			message := redactSecrets(urlErr.Err.Error())
			if urlErr.Timeout() {
				message = "The server did not answer within " + apiclient.DefaultTimeout.String() + "."
			}
			return apperrors.Wrap(codeConnectionFailed, message, true, err)
		}
	}
	// The driver's own sentence — refused, timed out, wrong password, bad
	// certificate — with anything that can hold a secret masked. A bare
	// "Connection failed." left nobody able to act on it.
	if subject == "builtin.database" || subject == "builtin.ssh" || subject == "builtin.kafka" {
		message := redactSecrets(err.Error())
		log.Printf("bridge: %s: connection error: %s", subject, message)
		if message == "" {
			message = "Connection failed."
		}
		return apperrors.Wrap(codeConnectionFailed, message, true, err)
	}
	return unexpectedError(subject, err)
}

// codeGraphNotFoundMessage names what was missing. A bare "not found" leaves
// the person guessing between the analyzer, the runtime that launches it and
// the project folder itself, and the *fs.PathError underneath already knows:
// its Path is the missing file and its Op is what was being done to it
// (fork/exec for a program that is not there, chdir for a missing working
// directory). The path is the user's own, on their own machine.
// invalidQuery is the search error that carries the person's own pattern's
// problem, or nil.
func invalidQuery(err error) *wssearch.InvalidQueryError {
	var q *wssearch.InvalidQueryError
	if errors.As(err, &q) {
		return q
	}
	return nil
}

func codeGraphNotFoundMessage(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && pathErr.Path != "" {
		return "CodeGraph file or directory not found: " + pathErr.Path + " (" + pathErr.Op + ")."
	}
	return "CodeGraph file or directory not found."
}

func publicErrorForCode(subject string, err error, existing *apperrors.AppError) *apperrors.AppError {
	code := existing.Code
	switch code {
	case codeValidationFailed:
		return apperrors.New(codeValidationFailed, "Validation failed.", true)
	case codePermissionDenied:
		return apperrors.New(codePermissionDenied, "Permission denied.", false)
	case codeConnectionFailed:
		return apperrors.New(codeConnectionFailed, "Connection failed.", true)
	case codeOperationCancelled:
		// internal/mcp mapInteractErr: a confirmation that timed out. The
		// original error is gone by now, so publicError's own check for it
		// no longer fires.
		return apperrors.New(codeOperationCancelled, "Operation cancelled.", true)
	case codePDFFailed:
		// Carries the store's or engine's own text (paths, wazero detail).
		logPDFCause(subject, existing)
		return apperrors.New(codePDFFailed, "Could not read the document.", existing.Recoverable)
	case codePDFUnavailable:
		logPDFCause(subject, existing)
		return apperrors.New(codePDFUnavailable, "DevKit's PDF reader is not available.", existing.Recoverable)
	}
	if keptCodes[code] {
		logPDFCause(subject, existing)
		return apperrors.New(code, existing.Message, existing.Recoverable)
	}
	return unexpectedError(subject, err)
}

const (
	codePDFFailed = "pdf.failed"
	// codePDFChanged: the Files viewer's PDF was replaced on disk after it was
	// opened. The answer is to open it again, never to retry the call.
	codePDFChanged     = "pdf_changed"
	codePDFUnavailable = "pdf.unavailable"
)

// logPDFCause logs what PDFium, wazero or the document store said behind a
// pdf.* error (internal/mcp pdfFailure): the published message is a fixed
// sentence, so this is the only place that text is not simply gone.
func logPDFCause(subject string, existing *apperrors.AppError) {
	if !strings.HasPrefix(existing.Code, "pdf.") {
		return
	}
	if cause := existing.Unwrap(); cause != nil {
		log.Printf("bridge: %s: %s: %v", subject, existing.Code, cause)
	}
}

// keptCodes are AppError codes published with their own message. Each is
// raised inside Caller.Call with a sentence written for the reader — fixed,
// or built only from numbers, limits, names the caller itself gave or the
// workspace lists, or (pr.*) a code host's message that has been bounded and
// marked as third-party text — never from an underlying error's text:
// internal/mcp/tools_pdf.go (pdfScope, pdfProblem, parsePageSpec),
// tools_workspace_memory.go, tools_pr.go (prError, hostSaid), git_repo.go and
// workspace_memory.go here. It is a list, not a prefix: a new code is
// redacted to internal_error until someone has read where its message comes
// from and added it. TestEveryMCPErrorCodeIsPublishedOrRedactedOnPurpose
// fails for a code in internal/mcp that is neither here, mapped above, nor on
// its explicit redacted-on-purpose list.
var keptCodes = map[string]bool{
	"pdf.notFound":            true,
	"pdf.sessionDenied":       true,
	"pdf.pageRange":           true,
	"pdf.timeout":             true,
	"pdf.unreadable":          true,
	"memory.notFound":         true,
	"memory.revisionConflict": true,
	"memory.workspaceDenied":  true,
	// internal/prwatch words these itself (account missing, token refused, the
	// host's own message); a token is never part of any of them.
	"pullrequests.attachFailed": true,
	"pullrequests.unavailable":  true,
	// internal/mcp/git_repo.go: which folder a `repo` argument meant, and why
	// it may not be used. Names are the caller's own input and the workspace's
	// folders — what workspace.list already tells that agent.
	"git.repoAmbiguous":        true,
	"git.repoOutsideWorkspace": true,
	"git.repoNotGranted":       true,
	"git.notARepository":       true,
	// internal/mcp/tools_pr.go prError. Fixed sentences, the platform's name
	// ("GitHub") or a time of day: the account/host/branch states.
	"pr.noAccount":        true,
	"pr.ambiguousAccount": true,
	"pr.unsupportedHost":  true,
	"pr.none":             true,
	"pr.detached":         true,
	"pr.protectedBranch":  true,
	"pr.repoRequired":     true,
	"pr.unauthorized":     true,
	"pr.rateLimited":      true,
	"pr.branchNotAllowed": true,
	"pr.notYours":         true,
	"pr.notMergeable":     true,
	"pr.notOpen":          true,
	// These quote the code host's own message, which prError passes through
	// hostSaid: folded to one line, control characters gone, at most 300
	// runes, said to be third-party text. forge builds none of them from a Go
	// error's text (github_http.go words its ErrInvalid cases itself).
	"pr.forbidden":   true,
	"pr.notFound":    true,
	"pr.rejected":    true,
	"pr.invalid":     true,
	"pr.unavailable": true,
	// internal/mcp/tools_agenthub.go codeResumeRefused: ResumeRefusedError.Sentence —
	// a fixed sentence per stop kind, the stop's time, and the vendor's text folded
	// to one line, clipped to 300 runes and said to be third-party.
	"agenthub.resumeRefused": true,
}

// unexpectedError is the bottom of publicError's funnel: every cause that
// reaches it is about to be redacted into "An unexpected error occurred.",
// the one public message with nothing — no code, no detail — for whoever
// hits it to act on. Logging the real cause here, once, is the only place
// left where it is not simply gone: without it, a real failure (an analyzer
// subprocess crash, a corrupt store) is invisible to the user AND to
// whoever has to debug their report afterward.
func unexpectedError(subject string, err error) *apperrors.AppError {
	log.Printf("bridge: %s: unclassified error: %v", subject, err)
	return apperrors.New(codeInternalError, "An unexpected error occurred.", false)
}

// agentNotInstalledMessage names the agent to install. An error that names
// none gets a sentence that does not guess: it used to say Claude whatever
// the session was.
func agentNotInstalledMessage(agent agenthub.AgentType) string {
	switch agent {
	case agenthub.AgentTypeClaude:
		return "Claude Agent is not installed. Install it in Settings → Agents."
	case agenthub.AgentTypeCodex:
		return "Codex is not installed. Install it in Settings → Agents."
	case agenthub.AgentTypeOpenCode:
		return "opencode is not installed. Install it in Settings → Agents."
	}
	return "The agent is not installed. Install it in Settings → Agents."
}

// agentAuthRequiredMessage says whose sign-in to fix and where. Claude's
// points at Claude Code's own login, never a form of ours: third-party apps
// may not offer a Claude login. Codex signs in inside DevKit, in its own
// home, so its message points at the card that does it.
func agentAuthRequiredMessage(agent agenthub.AgentType) string {
	switch agent {
	case agenthub.AgentTypeClaude:
		return "Claude isn't signed in, or the sign-in expired. Sign in to Claude, or set ANTHROPIC_API_KEY."
	case agenthub.AgentTypeCodex:
		return "Codex isn't signed in, or the sign-in expired. Sign in under Settings → Agents → Codex, or check the API key chosen there."
	case agenthub.AgentTypeOpenCode:
		return "opencode isn't signed in, or the sign-in expired. Sign in under Settings → Agents → opencode."
	}
	return "The agent isn't signed in, or the sign-in expired."
}

// agentAPIKeyUnusableMessage sends the person to the one place that fixes a
// failed key: choosing another, or none.
func agentAPIKeyUnusableMessage(agent agenthub.AgentType) string {
	if agent == agenthub.AgentTypeCodex {
		return "Codex can't use the API key chosen in Settings → Agents → Codex: it is gone from Accounts or was refused. Choose another, or go back to the ChatGPT sign-in."
	}
	return "The agent can't use the API key chosen for it in Settings → Agents. Choose another, or remove it."
}

// agentDisplayName is how a message names the vendor.
func agentDisplayName(agent agenthub.AgentType) string {
	switch agent {
	case agenthub.AgentTypeClaude:
		return "Claude"
	case agenthub.AgentTypeCodex:
		return "Codex"
	case agenthub.AgentTypeOpenCode:
		return "opencode"
	}
	return "The agent"
}

// agentUsageLimitMessage leads with what happened and then gives the
// vendor's own sentence, which is the only one that says when the limit
// lifts and what the plan offers.
func agentUsageLimitMessage(agent agenthub.AgentType, vendorMessage string) string {
	message := "Usage limit reached."
	if agent != "" {
		message = agentDisplayName(agent) + " usage limit reached."
	}
	if vendorMessage != "" {
		message += " " + vendorMessage
	}
	return message
}

// agentTurnFailedMessage says the turn ended, not the agent: the session is
// still there to prompt again.
func agentTurnFailedMessage(agent agenthub.AgentType, vendorMessage string) string {
	message := agentDisplayName(agent) + " couldn't finish the turn."
	if vendorMessage != "" {
		message += " " + vendorMessage
	}
	return message
}

// codexAPIKeyAccountMessage is what choosing an unusable account for Codex's
// API key says: what would have worked, since the account itself is fine for
// whatever else it is bound to.
const codexAPIKeyAccountMessage = "Choose an API key (a token account) that is not limited to an environment."

// gitAuthMessage says which identity a remote refused, and what the person can
// do about it. With one account that is obvious; with three it is the whole
// error, so it never falls back to git's "Permission denied".
func gitAuthMessage(e *gitservice.AuthError) string {
	remote := "Remote \u201c" + e.Remote + "\u201d"
	switch {
	case e.Transport == gitservice.TransportSSH && e.Account != "":
		return remote + " refused the SSH key of account \u201c" + e.Account + "\u201d. If the repository belongs to another GitHub or GitLab identity, choose the right account for this remote; if the key was removed from the account, add it again in the provider's settings."
	case e.Transport == gitservice.TransportSSH:
		return remote + " refused the SSH key git offered. No SSH-key account was used for it, so git used your own SSH configuration (ssh-agent or ~/.ssh/config). Add an SSH key for this host in Accounts, or check that the key it presents belongs to an account that can reach this repository."
	case e.Account != "":
		return remote + " refused the account \u201c" + e.Account + "\u201d. If the repository belongs to another GitHub or GitLab identity, bind the right account to this remote."
	case e.AccountID != "":
		return remote + " refused the account bound to it. If the repository belongs to another GitHub or GitLab identity, bind the right account to this remote."
	default:
		return remote + " refused the credentials git found on its own. Bind an account to this remote so DevKit authenticates it."
	}
}

// apiClientMessage words the API client's own refusals. Each is something the
// person can act on, and the fallback would have called every one of them "An
// unexpected error occurred."
func apiClientMessage(err error) (string, bool) {
	switch {
	case errors.Is(err, apiclient.ErrEnvRequired):
		return "Pick an environment first — its tag decides whether sending asks before it goes.", true
	case errors.Is(err, apiclient.ErrSchemeInvalid):
		return "Only http:// and https:// addresses can be sent.", true
	case errors.Is(err, apiclient.ErrPlainHTTP):
		return "Plain http:// is only allowed to this machine, or to a dev server when no account is bound and the environment does not ask before sending. Use https://.", true
	case errors.Is(err, apiclient.ErrNotAllowed):
		return "The request was not sent.", true
	case errors.Is(err, apiclient.ErrNotFound):
		return "That request, folder or environment no longer exists.", true
	case errors.Is(err, apiclient.ErrCollectionNotEmpty):
		return "This folder still holds " + strings.TrimPrefix(err.Error(), apiclient.ErrCollectionNotEmpty.Error()+": ") + ".", true
	case errors.Is(err, apiclient.ErrNotCurl):
		return "That does not read as a curl command.", true
	case errors.Is(err, apiclient.ErrUnknownFormat):
		return "DevKit does not recognise that file's format.", true
	case errors.Is(err, apiclient.ErrNotRecognised):
		return "That file is not one the chosen format can read.", true
	case errors.Is(err, apiclient.ErrFormatCannot):
		return "That format cannot do this — pick another.", true
	case errors.Is(err, apiclient.ErrTagRequired):
		return "The file holds an environment with no env tag. Pick the tag it should go under — it decides whether sending asks first.", true
	case errors.Is(err, apiclient.ErrInvalid):
		return "Some of the request is missing or malformed — check the name, the URL and the environment's tag.", true
	}
	return "", false
}

const invalidPromptGeneric = "The prompt is empty, or an attachment is unreadable, unsupported, or too large (images: 3.75 MB)."

// invalidPromptMessage says which attachment was refused and why when the
// error carries it ("report.pdf is not text or an image"). The generic line
// left a person staring at a prompt that would not send with no way to tell
// which of their files was the problem. Only the sentence DevKit itself wrote
// after the sentinel is shown, and only when it is short enough to be one.
// validModelChoiceDetail accepts the sentence of a ModelChoiceError when it is
// one bounded line; anything else falls back to the generic message.
func validModelChoiceDetail(detail string) bool {
	return detail != "" && len(detail) <= 900 && !strings.ContainsAny(detail, "\n\r")
}

func invalidPromptMessage(err error) string {
	detail := strings.TrimPrefix(err.Error(), agenthub.ErrInvalidPrompt.Error()+": ")
	if detail == err.Error() || detail == "" || len(detail) > 240 || strings.Contains(detail, "\n") {
		return invalidPromptGeneric
	}
	return "The prompt was not sent: " + detail + "."
}

// sentenceAfter is what a wrapped sentinel's own sentence says after its name:
// "invoke: <sentinel>: a name is at most 40 characters" reads "a name is at most 40
// characters". Only for sentinels whose wrapping sites are DevKit's own words (never
// an OS error or vendor output); fallback is used when nothing follows.
func sentenceAfter(err error, sentinel error, fallback string) string {
	text := err.Error()
	at := strings.Index(text, sentinel.Error()+": ")
	if at < 0 {
		return fallback
	}
	rest := strings.TrimSpace(text[at+len(sentinel.Error())+2:])
	if rest == "" {
		return fallback
	}
	return rest
}

// capitalised upper-cases the first letter of a sentence.
func capitalised(s string) string {
	for i, r := range s {
		return string(unicode.ToUpper(r)) + s[i+len(string(r)):]
	}
	return s
}
