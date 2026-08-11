package interaction

import "errors"

var (
	ErrSelectionRequired    = errors.New("selection required")
	ErrConfirmationRequired = errors.New("confirmation required")
	ErrConfirmationDenied   = errors.New("confirmation denied")
	ErrInteractionTimeout   = errors.New("interaction timeout")
	ErrNoPendingInteraction = errors.New("no pending interaction")
	ErrInvalidInteractionID = errors.New("invalid interaction id")
)
