package bridge

import (
	"context"
	"devkit/internal/capabilities"
	"errors"
	"log"
	"os"

	"devkit/internal/agenthub"
	"devkit/internal/agentruntime"
	"devkit/internal/apperrors"
	"devkit/internal/cloudconfig"
	"devkit/internal/codegraph"
	"devkit/internal/database"
	"devkit/internal/environments"
	"devkit/internal/interaction"
	"devkit/internal/kafka"
	"devkit/internal/mcp/install"
	"devkit/internal/preferences"
	"devkit/internal/syncclient"
	"devkit/internal/vault"
)

const (
	codeValidationFailed   = "validation_failed"
	codeVaultLocked        = "vault_locked"
	codePermissionDenied   = "permission_denied"
	codeConnectionFailed   = "connection_failed"
	codeOperationCancelled = "operation_cancelled"
	codeInternalError      = "internal_error"
	// codeUnsupportedProject is its own code rather than a validation failure
	// so the UI can tell "you picked the wrong folder" from "analysis broke".
	// The two need different offers: one is "pick another folder", the other is
	// "try again", and offering a retry on a folder that will never analyze is
	// a button that cannot work.
	codeUnsupportedProject = "unsupported_project"
	// A statement the engine refused. Not connection_failed: the connection is
	// fine, and the frontend tells the two apart to decide whether to offer a
	// reconnect or just show what the engine said.
	codeQueryFailed = "query_failed"
	// The three states of an agent runtime the UI answers with an action of
	// its own: offer the install, show what Node is missing, show the log.
	codeAgentNotInstalled  = "agent_not_installed"
	codeRuntimeRequirement = "runtime_requirement"
	codeInstallFailed      = "install_failed"
	codeAgentAuthRequired  = "agent_auth_required"
)

var safeValidationMessages = map[string]struct{}{
	"title is required":                                      {},
	"name is required":                                       {},
	"host is required":                                       {},
	"username is required":                                   {},
	"unsupported auth method":                                {},
	"master password too short":                              {},
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
}

func publicError(subject string, err error) *apperrors.AppError {
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
	if errors.Is(err, preferences.ErrInvalidTheme) {
		return apperrors.New(codeValidationFailed, preferences.ErrInvalidTheme.Error(), true)
	}
	// Authorization failures are classified before subject-specific adapters so
	// adding a new module cannot accidentally hide a denied capability as a
	// connection or service failure.
	if errors.Is(err, capabilities.ErrPermissionDenied) {
		return apperrors.New(codePermissionDenied, "Permission denied.", false)
	}
	var existing *apperrors.AppError
	if errors.As(err, &existing) {
		return publicErrorForCode(subject, err, existing.Code)
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
			return apperrors.New(codeAgentNotInstalled, "Claude Agent is not installed. Install it in Settings → Agents.", true)
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
		case errors.Is(err, errAgentHubUnavailable), errors.Is(err, agenthub.ErrUnavailable):
			return apperrors.New(codeInternalError, "Agent Hub is not available.", true)
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
		case errors.Is(err, agenthub.ErrAgentAuthRequired):
			// Pointed at Claude Code's own sign-in, never a form of ours: third-party
			// apps may not offer a Claude login.
			return apperrors.New(codeAgentAuthRequired, "Claude isn't signed in, or the sign-in expired. Sign in to Claude, or set ANTHROPIC_API_KEY.", true)
		case errors.Is(err, agenthub.ErrAuthMethodUnknown):
			return apperrors.New(codeValidationFailed, "That sign-in method isn't offered by this agent.", true)
		case errors.Is(err, agenthub.ErrProcessFailed):
			return apperrors.New(codeConnectionFailed, "Agent process failed.", true)
		case errors.Is(err, agenthub.ErrNotDescendant):
			return apperrors.New(codePermissionDenied, "Agent session is outside the caller's tree.", false)
		case errors.Is(err, agenthub.ErrCwdOutsideParent):
			return apperrors.New(codeValidationFailed, "Agent cwd must be inside the parent session's project.", true)
		case errors.Is(err, agenthub.ErrInvalidCwd):
			return apperrors.New(codeValidationFailed, "Agent cwd is not a valid project directory.", true)
		case errors.Is(err, agenthub.ErrInvalidConfigOption):
			return apperrors.New(codeValidationFailed, "Choose a valid agent setting.", true)
		case errors.Is(err, agenthub.ErrInvalidPrompt):
			return apperrors.New(codeValidationFailed, "The prompt is empty, or an attachment is unreadable, unsupported, or too large (images: 3.75 MB).", true)
		case errors.Is(err, agenthub.ErrCallerMismatch):
			return apperrors.New(codeValidationFailed, "callerSessionId does not match the calling agent.", true)
		case errors.Is(err, agenthub.ErrProxyNotFound):
			return apperrors.New(codeConnectionFailed, "DevKit MCP proxy (devkit-mcp) was not found; build it first.", true)
		}
	}
	if _, safe := safeValidationMessages[err.Error()]; safe {
		return apperrors.New(codeValidationFailed, err.Error(), true)
	}
	if subject == "builtin.codegraph" {
		switch {
		case errors.Is(err, codegraph.ErrNoActiveProject):
			return apperrors.Wrap(codeValidationFailed, "No active CodeGraph project.", true, codegraph.ErrNoActiveProject)
		case errors.Is(err, codegraph.ErrIndexNotReady):
			return apperrors.Wrap(codeValidationFailed, "CodeGraph index not ready. Wait for indexing to finish or reopen the project to retry.", true, err)
		case errors.Is(err, codegraph.ErrNodeNotFound):
			return apperrors.Wrap(codeValidationFailed, "CodeGraph node not found.", true, codegraph.ErrNodeNotFound)
		case errors.Is(err, codegraph.ErrAnalyzerNotConfigured):
			return apperrors.Wrap(codeConnectionFailed, "CodeGraph analyzer not configured.", true, codegraph.ErrAnalyzerNotConfigured)
		// The analyzer exists but the runtime that launches it does not — a
		// packaged install ships the artifact and not a JDK. CodeGraph's setup
		// panel says which program is missing and what to install; this is the
		// backstop for anything that reaches an index without passing it.
		// The folder holds nothing any analyzer recognises. Named separately
		// from a failed analyze because it is not a failure: the user picked a
		// folder, and the answer is which kinds of project CodeGraph reads.
		case errors.Is(err, codegraph.ErrUnsupportedProject):
			return apperrors.Wrap(codeUnsupportedProject, "No supported project found in that folder. CodeGraph reads Java projects — pick a folder holding a build file (pom.xml, build.gradle) or Java sources.", true, codegraph.ErrUnsupportedProject)
		case errors.Is(err, codegraph.ErrToolchainIncomplete):
			return apperrors.Wrap(codeConnectionFailed, "The CodeGraph analyzer cannot run: "+err.Error()+".", true, codegraph.ErrToolchainIncomplete)
		case errors.Is(err, codegraph.ErrNoPlan):
			return apperrors.Wrap(codeValidationFailed, "No active plan.", true, codegraph.ErrNoPlan)
		case errors.Is(err, codegraph.ErrPlanNodeNotFound):
			return apperrors.Wrap(codeValidationFailed, "Plan node not found.", true, codegraph.ErrPlanNodeNotFound)
		case errors.Is(err, codegraph.ErrInvalidPlan):
			return apperrors.Wrap(codeValidationFailed, "Invalid plan.", true, codegraph.ErrInvalidPlan)
		case errors.Is(err, codegraph.ErrPathOutsideProject):
			return apperrors.Wrap(codeValidationFailed, "File path must stay inside the active CodeGraph project.", true, err)
		case errors.Is(err, os.ErrNotExist):
			return apperrors.Wrap(codeValidationFailed, "CodeGraph file or directory not found.", true, err)
		case errors.Is(err, codegraph.ErrBinaryFile):
			return apperrors.Wrap(codeValidationFailed, "This file is not text and cannot be opened in the editor.", true, codegraph.ErrBinaryFile)
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
	// Everything else on this subject stays redacted below: connecting happens
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
	if subject == "builtin.database" || subject == "builtin.ssh" || subject == "builtin.kafka" {
		return apperrors.New(codeConnectionFailed, "Connection failed.", true)
	}
	return unexpectedError(subject, err)
}

func publicErrorForCode(subject string, err error, code string) *apperrors.AppError {
	switch code {
	case codeValidationFailed:
		return apperrors.New(codeValidationFailed, "Validation failed.", true)
	case codePermissionDenied:
		return apperrors.New(codePermissionDenied, "Permission denied.", false)
	case codeConnectionFailed:
		return apperrors.New(codeConnectionFailed, "Connection failed.", true)
	default:
		return unexpectedError(subject, err)
	}
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
