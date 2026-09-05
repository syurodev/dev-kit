package bridge

import (
	"context"
	"devkit/internal/capabilities"
	"errors"

	"devkit/internal/agenthub"
	"devkit/internal/apperrors"
	"devkit/internal/cloudconfig"
	"devkit/internal/codegraph"
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
		return publicErrorForCode(existing.Code)
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
		case errors.Is(err, agenthub.ErrProcessFailed):
			return apperrors.New(codeConnectionFailed, "Agent process failed.", true)
		case errors.Is(err, agenthub.ErrNotDescendant):
			return apperrors.New(codePermissionDenied, "Agent session is outside the caller's tree.", false)
		case errors.Is(err, agenthub.ErrCwdOutsideParent):
			return apperrors.New(codeValidationFailed, "Agent cwd must be inside the parent session's project.", true)
		case errors.Is(err, agenthub.ErrInvalidCwd):
			return apperrors.New(codeValidationFailed, "Agent cwd is not a valid project directory.", true)
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
		case errors.Is(err, codegraph.ErrNodeNotFound):
			return apperrors.Wrap(codeValidationFailed, "CodeGraph node not found.", true, codegraph.ErrNodeNotFound)
		case errors.Is(err, codegraph.ErrAnalyzerNotConfigured):
			return apperrors.Wrap(codeConnectionFailed, "CodeGraph analyzer not configured.", true, codegraph.ErrAnalyzerNotConfigured)
		case errors.Is(err, codegraph.ErrNoPlan):
			return apperrors.Wrap(codeValidationFailed, "No active plan.", true, codegraph.ErrNoPlan)
		case errors.Is(err, codegraph.ErrPlanNodeNotFound):
			return apperrors.Wrap(codeValidationFailed, "Plan node not found.", true, codegraph.ErrPlanNodeNotFound)
		case errors.Is(err, codegraph.ErrInvalidPlan):
			return apperrors.Wrap(codeValidationFailed, "Invalid plan.", true, codegraph.ErrInvalidPlan)
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
	if subject == "builtin.database" || subject == "builtin.ssh" || subject == "builtin.kafka" {
		return apperrors.New(codeConnectionFailed, "Connection failed.", true)
	}
	return apperrors.New(codeInternalError, "An unexpected error occurred.", false)
}

func publicErrorForCode(code string) *apperrors.AppError {
	switch code {
	case codeValidationFailed:
		return apperrors.New(codeValidationFailed, "Validation failed.", true)
	case codePermissionDenied:
		return apperrors.New(codePermissionDenied, "Permission denied.", false)
	case codeConnectionFailed:
		return apperrors.New(codeConnectionFailed, "Connection failed.", true)
	default:
		return apperrors.New(codeInternalError, "An unexpected error occurred.", false)
	}
}
