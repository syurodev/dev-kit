package apperrors

type AppError struct {
	Code        string         `json:"code"`
	Message     string         `json:"message"`
	Recoverable bool           `json:"recoverable"`
	Details     map[string]any `json:"details,omitempty"`
	cause       error
}

func New(code string, message string, recoverable bool) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Recoverable: recoverable,
	}
}

func NewWithDetails(code string, message string, recoverable bool, details map[string]any) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Recoverable: recoverable,
		Details:     details,
	}
}

func Wrap(code string, message string, recoverable bool, cause error) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Recoverable: recoverable,
		cause:       cause,
	}
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
