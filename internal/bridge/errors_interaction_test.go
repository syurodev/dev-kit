package bridge

import (
	"errors"
	"testing"

	"devkit/internal/apperrors"
	"devkit/internal/interaction"
)

func TestPublicErrorInteractionTimeout(t *testing.T) {
	err := publicError("builtin.ssh", interaction.ErrInteractionTimeout)
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Code != "operation_cancelled" {
		t.Fatalf("%+v", err)
	}
	if app.Message != "Operation cancelled." {
		t.Fatalf("message = %q", app.Message)
	}
}

func TestPublicErrorInteractionConfirmationDenied(t *testing.T) {
	err := publicError("builtin.ssh", interaction.ErrConfirmationDenied)
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Code != "permission_denied" {
		t.Fatalf("%+v", err)
	}
}

func TestPublicErrorInteractionSelectionAndConfirmationRequired(t *testing.T) {
	err := publicError("builtin.database", interaction.ErrSelectionRequired)
	var app *apperrors.AppError
	if !errors.As(err, &app) || app.Code != "validation_failed" || app.Message != "Selection required." {
		t.Fatalf("%+v", err)
	}

	err = publicError("builtin.kafka", interaction.ErrConfirmationRequired)
	if !errors.As(err, &app) || app.Code != "validation_failed" || app.Message != "Confirmation required." {
		t.Fatalf("%+v", err)
	}
}
