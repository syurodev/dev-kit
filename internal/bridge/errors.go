package bridge

import (
	"context"
	"errors"
	"strings"

	"changeme/internal/apperrors"
	"changeme/internal/vault"
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
	"title is required":                       {},
	"name is required":                        {},
	"host is required":                        {},
	"username is required":                    {},
	"unsupported auth method":                 {},
	"master password too short":               {},
	"forward direction is required":           {},
	"unsupported forward direction":           {},
	"bind host must be loopback":              {},
	"bind port must be between 0 and 65535":   {},
	"target host is required":                 {},
	"target host is invalid":                  {},
	"target port must be between 1 and 65535": {},
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
	var existing *apperrors.AppError
	if errors.As(err, &existing) {
		return publicErrorForCode(existing.Code)
	}
	if strings.HasPrefix(err.Error(), "permission denied:") {
		return apperrors.New(codePermissionDenied, "Permission denied.", false)
	}
	if _, safe := safeValidationMessages[err.Error()]; safe {
		return apperrors.New(codeValidationFailed, err.Error(), true)
	}
	if subject == "builtin.database" || subject == "builtin.ssh" {
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
