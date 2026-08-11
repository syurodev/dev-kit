package bridge

import (
	"context"
	"errors"
	"strings"

	"devkit/internal/apperrors"
	"devkit/internal/cloudconfig"
	"devkit/internal/environments"
	"devkit/internal/kafka"
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
}

func publicError(subject string, err error) *apperrors.AppError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return apperrors.New(codeOperationCancelled, "Operation cancelled.", true)
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
	// Authorization failures are classified before subject-specific adapters so
	// adding a new module cannot accidentally hide a denied capability as a
	// connection or service failure.
	if strings.HasPrefix(err.Error(), "permission denied:") {
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
	if _, safe := safeValidationMessages[err.Error()]; safe {
		return apperrors.New(codeValidationFailed, err.Error(), true)
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
