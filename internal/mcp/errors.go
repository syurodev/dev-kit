package mcp

import (
	"errors"

	"devkit/internal/apperrors"
	"devkit/internal/interaction"
)

// mapInteractErr maps interaction hub errors to stable public AppErrors for MCP tools.
func mapInteractErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, interaction.ErrInteractionTimeout):
		return apperrors.New("operation_cancelled", "Operation cancelled.", true)
	case errors.Is(err, interaction.ErrConfirmationDenied):
		return apperrors.New("permission_denied", "Permission denied.", false)
	case errors.Is(err, interaction.ErrSelectionRequired):
		return apperrors.New("validation_failed", "Selection required.", true)
	case errors.Is(err, interaction.ErrConfirmationRequired):
		return apperrors.New("validation_failed", "Confirmation required.", true)
	default:
		return err
	}
}
